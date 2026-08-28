package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/crewjam/saml"
	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/models"
)

const minimalIDPMetadata = `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="http://idp.example.com">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="http://idp.example.com/sso"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

func TestSAML_SP_Lifecycle_And_Handlers(t *testing.T) {
	d := newCRUDTestDB(t)

	idpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(minimalIDPMetadata))
	}))
	defer idpServer.Close()

	cfg := &config.Config{
		SAMLEnabled:        true,
		SAMLEntityID:       "http://localhost:8080/saml/metadata",
		SAMLIDPMetadataURL: idpServer.URL,
	}

	h := &AuthHandler{
		DB:     d,
		Config: cfg,
	}

	// 1. InitSAML
	if err := h.InitSAML(); err != nil {
		t.Fatalf("InitSAML failed: %v", err)
	}
	if h.SP == nil {
		t.Fatalf("expected h.SP non-nil")
	}

	// 2. SAMLMetadata
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/saml/metadata", nil)
	h.SAMLMetadata(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on SAMLMetadata, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "EntityDescriptor") {
		t.Errorf("expected XML EntityDescriptor in metadata body")
	}

	// 3. SAMLLogin
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/saml/login", nil)
	h.SAMLLogin(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 on SAMLLogin, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "http://idp.example.com/sso") {
		t.Errorf("expected redirect to IDP SSO, got %s", loc)
	}

	// 4. SAMLACS with bad form data
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/saml/acs", strings.NewReader("bad-form-content"))
	req.Header.Set("Content-Type", "invalid-content-type")
	h.SAMLACS(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect on ACS bad form, got %d", rec.Code)
	}

	// 5. SAMLACS with invalid SAMLResponse
	form := url.Values{"SAMLResponse": {"invalid-base64-xml"}}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/saml/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.SAMLACS(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect on ACS invalid response, got %d", rec.Code)
	}
}

func TestSAML_NilSP_Handlers(t *testing.T) {
	h := &AuthHandler{SP: nil}

	// 1. SAMLMetadata
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/saml/metadata", nil)
	h.SAMLMetadata(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 on SAMLMetadata with nil SP, got %d", rec.Code)
	}

	// 2. SAMLLogin
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/saml/login", nil)
	h.SAMLLogin(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 on SAMLLogin with nil SP, got %d", rec.Code)
	}

	// 3. SAMLACS
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/saml/acs", nil)
	h.SAMLACS(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 on SAMLACS with nil SP, got %d", rec.Code)
	}
}

func TestSAML_SyncGroupRoles_And_EntraOverage(t *testing.T) {
	d := newCRUDTestDB(t)

	cfg := &config.Config{
		SAMLGroupsClaim:         "groups",
		SAMLGroupGlobal:         "group-admin-id",
		SAMLGroupTeamLeader:     "group-tl-id",
		SAMLGroupActivityViewer: "group-viewer-id",
	}

	h := &AuthHandler{DB: d, Config: cfg}

	uID, _ := d.CreateLocalUser("saml_user@example.com", "SAML User", "pass")
	user, _ := d.GetUserByID(uID)

	// 1. Assertion with matching groups
	assertion := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{
			{
				Attributes: []saml.Attribute{
					{
						Name: "groups",
						Values: []saml.AttributeValue{
							{Value: "group-admin-id"},
							{Value: "group-tl-id"},
							{Value: "group-other-id"},
						},
					},
				},
			},
		},
	}

	h.syncSAMLGroupRoles(user, assertion, user.Email)
	updatedUser, _ := d.GetUserByID(uID)
	if !updatedUser.HasRole(models.RoleGlobal) || !updatedUser.HasRole(models.RoleTeamLeader) {
		t.Errorf("expected roles global and team_leader, got %q", updatedUser.Roles)
	}

	// 2. Assertion without matching groups -> falls back to basic
	assertionEmpty := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{
			{
				Attributes: []saml.Attribute{
					{
						Name:   "groups",
						Values: []saml.AttributeValue{{Value: "unrelated-group"}},
					},
				},
			},
		},
	}
	h.syncSAMLGroupRoles(user, assertionEmpty, user.Email)
	updatedUser2, _ := d.GetUserByID(uID)
	if updatedUser2.Roles != models.RoleBasic {
		t.Errorf("expected role %q, got %q", models.RoleBasic, updatedUser2.Roles)
	}

	// 3. hasPossibleEntraGroupOverage tests
	overageAssertion := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{
			{
				Attributes: []saml.Attribute{
					{Name: "http://schemas.microsoft.com/claims/hasgroups"},
				},
			},
		},
	}
	if !hasPossibleEntraGroupOverage(overageAssertion) {
		t.Errorf("expected true for hasgroups claim")
	}

	claimNamesAssertion := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{
			{
				Attributes: []saml.Attribute{
					{Name: "_claim_names"},
				},
			},
		},
	}
	if !hasPossibleEntraGroupOverage(claimNamesAssertion) {
		t.Errorf("expected true for _claim_names claim")
	}

	groupsLinkAssertion := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{
			{
				Attributes: []saml.Attribute{
					{Name: "groups.link"},
				},
			},
		},
	}
	if !hasPossibleEntraGroupOverage(groupsLinkAssertion) {
		t.Errorf("expected true for groups.link claim")
	}

	cleanAssertion := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{
			{
				Attributes: []saml.Attribute{
					{Name: "email"},
				},
			},
		},
	}
	if hasPossibleEntraGroupOverage(cleanAssertion) {
		t.Errorf("expected false for clean assertion")
	}

	// 4. hasConfiguredSAMLGroupMappings
	if !hasConfiguredSAMLGroupMappings(cfg) {
		t.Errorf("expected true when mappings are configured")
	}
	if hasConfiguredSAMLGroupMappings(&config.Config{}) {
		t.Errorf("expected false when no mappings configured")
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	if ip := clientIP(req); ip != "192.168.1.1:1234" {
		t.Errorf("expected remote addr %q, got %q", "192.168.1.1:1234", ip)
	}

	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	if ip := clientIP(req); ip != "10.0.0.1" {
		t.Errorf("expected first forwarded IP 10.0.0.1, got %q", ip)
	}
}

func TestSAML_AutoLogin_And_Bypass(t *testing.T) {
	d := newCRUDTestDB(t)

	var renderedPage string
	renderFn := func(w http.ResponseWriter, r *http.Request, p string, d interface{}) {
		renderedPage = p
	}

	cfg := &config.Config{
		SAMLEnabled:   true,
		SAMLAutoLogin: true,
	}
	h := &AuthHandler{
		DB:     d,
		Config: cfg,
		Render: renderFn,
	}

	// 1. Default request to /login should auto-redirect to /saml/login
	renderedPage = ""
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	h.LoginPage(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/saml/login" {
		t.Fatalf("expected 303 redirect to /saml/login, got code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
	if renderedPage != "" {
		t.Errorf("expected no render on auto-redirect, got page=%q", renderedPage)
	}

	// 2. Request with return_to should auto-redirect to /saml/login?return_to=...
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/login?return_to=%2Fprojects", nil)
	h.LoginPage(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/saml/login?return_to=%2Fprojects" {
		t.Fatalf("expected 303 redirect to /saml/login?return_to=%%2Fprojects, got code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	// 3. Request with ?local=1 bypasses auto-redirect and renders login page
	renderedPage = ""
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/login?local=1", nil)
	h.LoginPage(rec, req)
	if rec.Code != http.StatusOK && renderedPage != "login" {
		t.Fatalf("expected render login page on ?local=1, got code=%d page=%q", rec.Code, renderedPage)
	}

	// 4. Request with ?logged_out=1 bypasses auto-redirect and renders login page
	renderedPage = ""
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/login?logged_out=1", nil)
	h.LoginPage(rec, req)
	if renderedPage != "login" {
		t.Fatalf("expected render login page on ?logged_out=1, got page=%q", renderedPage)
	}

	// 5. Request with ?error=... bypasses auto-redirect and renders login page
	renderedPage = ""
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/login?error=Failed", nil)
	h.LoginPage(rec, req)
	if renderedPage != "login" {
		t.Fatalf("expected render login page on ?error=..., got page=%q", renderedPage)
	}

	// 6. When SAMLAutoLogin is false, renders login page
	cfg.SAMLAutoLogin = false
	renderedPage = ""
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/login", nil)
	h.LoginPage(rec, req)
	if renderedPage != "login" {
		t.Fatalf("expected render login page when SAMLAutoLogin=false, got page=%q", renderedPage)
	}
}
