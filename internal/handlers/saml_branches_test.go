package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// minimalSAMLMetadataServer starts an httptest.Server that returns a minimal
// but syntactically valid SAML IdP EntityDescriptor, sufficient for
// samlsp.FetchMetadata to parse without errors.
func minimalSAMLMetadataServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.test">
  <IDPSSODescriptor WantAuthnRequestsSigned="false" protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="https://idp.test/sso"/>
  </IDPSSODescriptor>
</EntityDescriptor>`)
	}))
}

// ---- AuthHandler SAML nil-SP branches ----

func TestSAMLMetadata_SPNil(t *testing.T) {
	h := &AuthHandler{SP: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/saml/metadata", nil)
	h.SAMLMetadata(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestSAMLLogin_SPNil(t *testing.T) {
	h := &AuthHandler{SP: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/saml/login", nil)
	h.SAMLLogin(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestSAMLACS_SPNil(t *testing.T) {
	h := &AuthHandler{SP: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/saml/acs", nil)
	h.SAMLACS(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// ---- InitSAML invalid URL branches ----

func TestInitSAML_InvalidRootURL(t *testing.T) {
	h := &AuthHandler{Config: &config.Config{
		SAMLEnabled: true,
		SAMLRootURL: "://bad-url",
	}}
	err := h.InitSAML()
	if err == nil {
		t.Fatal("expected error for invalid root URL")
	}
}

func TestInitSAML_InvalidIDPURL(t *testing.T) {
	h := &AuthHandler{Config: &config.Config{
		SAMLEnabled:        true,
		SAMLRootURL:        "https://example.com",
		SAMLIDPMetadataURL: "://bad-url",
	}}
	err := h.InitSAML()
	if err == nil {
		t.Fatal("expected error for invalid IDP metadata URL")
	}
}

// TestInitSAML_FetchMetadataFails covers auth.go:58-70 — the httpClient + FetchMetadata
// call path. The IDP server is closed before the request so we get connection-refused.
func TestInitSAML_FetchMetadataFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close() // close immediately → connection refused on request

	h := &AuthHandler{Config: &config.Config{
		SAMLEnabled:        true,
		SAMLRootURL:        "http://sp.example.com",
		SAMLIDPMetadataURL: addr + "/metadata",
	}}
	err := h.InitSAML()
	if err == nil {
		t.Fatal("expected error when IDP metadata server is unreachable")
	}
}

// TestInitSAML_LoadX509KeyPairFails covers auth.go:73-78 — FetchMetadata succeeds
// but tls.LoadX509KeyPair fails because the cert files don't exist.
func TestInitSAML_LoadX509KeyPairFails(t *testing.T) {
	srv := minimalSAMLMetadataServer(t)
	defer srv.Close()

	h := &AuthHandler{Config: &config.Config{
		SAMLEnabled:        true,
		SAMLRootURL:        "http://sp.example.com",
		SAMLIDPMetadataURL: srv.URL,
		SAMLCertFile:       "/nonexistent-cert.pem",
		SAMLKeyFile:        "/nonexistent-key.pem",
	}}
	err := h.InitSAML()
	if err == nil {
		t.Fatal("expected error for nonexistent cert/key files")
	}
}

// TestInitSAML_SelfSignedSuccess covers auth.go:79-107 — FetchMetadata succeeds,
// no cert files configured so generateSelfSignedCert is called, and InitSAML
// completes successfully with h.SP set.
func TestInitSAML_SelfSignedSuccess(t *testing.T) {
	srv := minimalSAMLMetadataServer(t)
	defer srv.Close()

	h := &AuthHandler{Config: &config.Config{
		SAMLEnabled:        true,
		SAMLRootURL:        "http://sp.example.com",
		SAMLIDPMetadataURL: srv.URL,
		SAMLEntityID:       "http://sp.example.com/saml/metadata",
		// SAMLCertFile / SAMLKeyFile empty → generateSelfSignedCert path
	}}
	if err := h.InitSAML(); err != nil {
		t.Fatalf("InitSAML unexpected error: %v", err)
	}
	if h.SP == nil {
		t.Fatal("expected h.SP to be initialised after InitSAML")
	}
}

// ---- ChangePasswordPost missing branches ----

func TestChangePasswordPost_EmptyFields(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &SettingsHandler{DB: d}

	uid, err := d.CreateLocalUser("cpempty@example.com", "CPEmpty", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Missing current_password
	form := url.Values{}
	form.Set("current_password", "")
	form.Set("new_password", "newpassword")
	form.Set("confirm_password", "newpassword")
	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	handler := middleware.Auth(d, http.HandlerFunc(h.ChangePasswordPost))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Fatalf("expected error in redirect, got %q", w.Header().Get("Location"))
	}
}

func TestChangePasswordPost_PasswordMismatch(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &SettingsHandler{DB: d}

	uid, err := d.CreateLocalUser("cpmismatch@example.com", "CPMismatch", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	form := url.Values{}
	form.Set("current_password", "password1")
	form.Set("new_password", "newpassword")
	form.Set("confirm_password", "differentpassword")
	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	handler := middleware.Auth(d, http.HandlerFunc(h.ChangePasswordPost))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Fatalf("expected error in redirect, got %q", w.Header().Get("Location"))
	}
}

func TestChangePasswordPost_ShortPassword(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &SettingsHandler{DB: d}

	uid, err := d.CreateLocalUser("cpshort@example.com", "CPShort", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	form := url.Values{}
	form.Set("current_password", "password1")
	form.Set("new_password", "short")
	form.Set("confirm_password", "short")
	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	handler := middleware.Auth(d, http.HandlerFunc(h.ChangePasswordPost))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Fatalf("expected error in redirect, got %q", w.Header().Get("Location"))
	}
}

// ---- ImpersonatePage non-global branch ----

func TestImpersonatePage_NonGlobal2(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &SettingsHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		w.WriteHeader(http.StatusOK)
	}}

	uid, err := d.CreateLocalUser("basicuser2@example.com", "Basic", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/settings/impersonate", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	handler := middleware.Auth(d, http.HandlerFunc(h.ImpersonatePage))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

// ---- ImpersonatePost user not found branch ----

func TestImpersonatePost_UserNotFound(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &SettingsHandler{DB: d}

	// Create global admin
	uid, err := d.CreateLocalUser("admin2@example.com", "Admin2", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	if err := d.UpdateUserRoles(uid, models.RoleGlobal); err != nil {
		t.Fatalf("UpdateUserRoles: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	form := url.Values{}
	form.Set("login", "doesnotexist@example.com")
	req := httptest.NewRequest(http.MethodPost, "/settings/impersonate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	handler := middleware.Auth(d, http.HandlerFunc(h.ImpersonatePost))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: body=%s", w.Code, w.Body.String())
	}
}
