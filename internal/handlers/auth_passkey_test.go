package handlers

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

func reqWithUser(d *db.DB, u *models.User, method, path string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, path, body)
	if u != nil {
		sessionTok, _ := d.CreateSession(u.ID)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionTok})
		var rOut *http.Request
		middleware.Auth(d, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rOut = r
		})).ServeHTTP(httptest.NewRecorder(), req)
		return rOut
	}
	return req
}

func TestWebAuthnUser_Methods(t *testing.T) {
	u := &models.User{
		ID:    12345,
		Email: "alice@example.com",
		Name:  "Alice Wonderland",
	}
	creds := []webauthn.Credential{
		{ID: []byte("cred-1")},
	}
	wUser := newWebAuthnUser(u, creds)

	idBytes := wUser.WebAuthnID()
	if len(idBytes) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(idBytes))
	}
	if binary.BigEndian.Uint64(idBytes) != 12345 {
		t.Errorf("expected ID 12345, got %d", binary.BigEndian.Uint64(idBytes))
	}

	if wUser.WebAuthnName() != "alice@example.com" {
		t.Errorf("expected WebAuthnName %q, got %q", "alice@example.com", wUser.WebAuthnName())
	}
	if wUser.WebAuthnDisplayName() != "Alice Wonderland" {
		t.Errorf("expected WebAuthnDisplayName %q, got %q", "Alice Wonderland", wUser.WebAuthnDisplayName())
	}
	if len(wUser.WebAuthnCredentials()) != 1 {
		t.Errorf("expected 1 credential, got %d", len(wUser.WebAuthnCredentials()))
	}
}

func TestGeneratePasskeyToken(t *testing.T) {
	tok1, err1 := generatePasskeyToken()
	tok2, err2 := generatePasskeyToken()
	if err1 != nil || err2 != nil {
		t.Fatalf("generatePasskeyToken failed: %v, %v", err1, err2)
	}
	if len(tok1) != 64 || len(tok2) != 64 {
		t.Fatalf("expected 64 char hex string, got %d / %d", len(tok1), len(tok2))
	}
	if tok1 == tok2 {
		t.Errorf("expected random tokens to be distinct")
	}
}

func TestInitWebAuthn(t *testing.T) {
	// Disabled
	h1 := &AuthHandler{Config: &config.Config{EnablePasskeys: false}}
	if err := h1.InitWebAuthn(); err != nil || h1.WebAuthn != nil {
		t.Errorf("expected nil error and nil WebAuthn when disabled")
	}

	// Enabled without RPID
	h2 := &AuthHandler{Config: &config.Config{EnablePasskeys: true, PasskeyRPID: "", PasskeyRPOrigin: ""}}
	if err := h2.InitWebAuthn(); err != nil || h2.WebAuthn != nil || h2.Config.EnablePasskeys {
		t.Errorf("expected disabled passkeys when config missing")
	}

	// Enabled valid
	h4 := &AuthHandler{Config: &config.Config{
		EnablePasskeys:  true,
		PasskeyRPID:     "localhost",
		PasskeyRPOrigin: "http://localhost:8080",
		AppName:         "MyPresence",
	}}
	if err := h4.InitWebAuthn(); err != nil || h4.WebAuthn == nil {
		t.Fatalf("InitWebAuthn valid failed: %v", err)
	}
}

func TestPasskeyRegister_Ceremony(t *testing.T) {
	d := newCRUDTestDB(t)
	cfg := &config.Config{
		EnablePasskeys:  true,
		PasskeyRPID:     "localhost",
		PasskeyRPOrigin: "http://localhost:8080",
		AppName:         "MyPresence",
	}
	h := &AuthHandler{
		DB:     d,
		Config: cfg,
	}
	_ = h.InitWebAuthn()

	uid, _ := d.CreateLocalUser("local@example.com", "Local User", "password123")
	localUser, _ := d.GetUserByID(uid)

	ssoUser, _ := d.UpsertUser("sso@example.com", "SSO User")

	// 1. PasskeyRegisterBegin - WebAuthn nil
	hNil := &AuthHandler{DB: d, Config: cfg}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webauthn/register/begin", nil)
	hNil.PasskeyRegisterBegin(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when WebAuthn is nil, got %d", rec.Code)
	}

	// 2. PasskeyRegisterBegin - unauthorized
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/register/begin", nil)
	h.PasskeyRegisterBegin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when user is nil, got %d", rec.Code)
	}

	// 3. PasskeyRegisterBegin - non-local user
	rec = httptest.NewRecorder()
	req = reqWithUser(d, ssoUser, http.MethodPost, "/webauthn/register/begin", nil)
	h.PasskeyRegisterBegin(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-local user, got %d", rec.Code)
	}

	// 4. PasskeyRegisterBegin - success
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodPost, "/webauthn/register/begin", nil)
	h.PasskeyRegisterBegin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on register begin, got %d: %s", rec.Code, rec.Body.String())
	}
	var beginResp struct {
		SessionToken string                 `json:"sessionToken"`
		PublicKey    map[string]interface{} `json:"publicKey"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &beginResp); err != nil || beginResp.SessionToken == "" {
		t.Fatalf("failed to decode begin response: %v, body: %s", err, rec.Body.String())
	}

	// 5. PasskeyRegisterFinish - WebAuthn nil
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/register/finish", nil)
	hNil.PasskeyRegisterFinish(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when WebAuthn is nil, got %d", rec.Code)
	}

	// 6. PasskeyRegisterFinish - unauthorized / non-local
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/register/finish", nil)
	h.PasskeyRegisterFinish(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when not authed, got %d", rec.Code)
	}

	// 7. PasskeyRegisterFinish - missing token
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodPost, "/webauthn/register/finish", nil)
	h.PasskeyRegisterFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on missing token, got %d", rec.Code)
	}

	// 8. PasskeyRegisterFinish - unknown token
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodPost, "/webauthn/register/finish?token=unknown-token", nil)
	h.PasskeyRegisterFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on unknown token, got %d", rec.Code)
	}

	// 9. PasskeyRegisterFinish - wrong user or expired
	h.passkeySessions.Store("expired-token", &passkeyChallengeEntry{
		UserID:    localUser.ID,
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	})
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodPost, "/webauthn/register/finish?token=expired-token", nil)
	h.PasskeyRegisterFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on expired token, got %d", rec.Code)
	}

	// 10. PasskeyRegisterFinish - invalid body
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodPost, "/webauthn/register/finish?token="+beginResp.SessionToken+"&name=MacBook", strings.NewReader("invalid-body"))
	h.PasskeyRegisterFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on invalid credential response, got %d", rec.Code)
	}
}

func TestPasskeyLogin_Ceremony(t *testing.T) {
	d := newCRUDTestDB(t)
	cfg := &config.Config{
		EnablePasskeys:  true,
		PasskeyRPID:     "localhost",
		PasskeyRPOrigin: "http://localhost:8080",
		AppName:         "MyPresence",
	}
	h := &AuthHandler{
		DB:     d,
		Config: cfg,
	}
	_ = h.InitWebAuthn()

	uid, _ := d.CreateLocalUser("local2@example.com", "Local Two", "password123")
	_ = uid

	// 1. PasskeyLoginBegin - nil WebAuthn
	hNil := &AuthHandler{DB: d, Config: cfg}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/begin", nil)
	hNil.PasskeyLoginBegin(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 on nil WebAuthn, got %d", rec.Code)
	}

	// 2. PasskeyLoginBegin - success
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/login/begin", nil)
	h.PasskeyLoginBegin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on login begin, got %d: %s", rec.Code, rec.Body.String())
	}
	var loginBeginResp struct {
		SessionToken string                 `json:"sessionToken"`
		PublicKey    map[string]interface{} `json:"publicKey"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loginBeginResp); err != nil || loginBeginResp.SessionToken == "" {
		t.Fatalf("failed to decode login begin response: %v, body: %s", err, rec.Body.String())
	}

	// 3. PasskeyLoginFinish - nil WebAuthn
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", nil)
	hNil.PasskeyLoginFinish(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 on nil WebAuthn, got %d", rec.Code)
	}

	// 4. PasskeyLoginFinish - missing token
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", nil)
	h.PasskeyLoginFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on missing token, got %d", rec.Code)
	}

	// 5. PasskeyLoginFinish - invalid token
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/login/finish?token=not-exists", nil)
	h.PasskeyLoginFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on not found token, got %d", rec.Code)
	}

	// 6. PasskeyLoginFinish - expired token
	h.passkeySessions.Store("expired-login-token", &passkeyChallengeEntry{
		UserID:    0,
		ExpiresAt: time.Now().Add(-5 * time.Second),
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/login/finish?token=expired-login-token", nil)
	h.PasskeyLoginFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on expired session, got %d", rec.Code)
	}

	// 7. PasskeyLoginFinish - invalid assertion body
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/login/finish?token="+loginBeginResp.SessionToken, strings.NewReader("bad-assertion"))
	h.PasskeyLoginFinish(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on bad assertion body, got %d", rec.Code)
	}
}

func TestPasskeysPage_And_Delete(t *testing.T) {
	d := newCRUDTestDB(t)
	cfg := &config.Config{
		EnablePasskeys:  true,
		PasskeyRPID:     "localhost",
		PasskeyRPOrigin: "http://localhost:8080",
		AppName:         "MyPresence",
	}

	renderedPage := ""
	var renderedData interface{}
	renderMock := func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		renderedPage = page
		renderedData = data
	}

	h := &AuthHandler{
		DB:     d,
		Config: cfg,
		Render: renderMock,
	}
	_ = h.InitWebAuthn()

	uid, _ := d.CreateLocalUser("passkey_mgmt@example.com", "Passkey User", "password")
	localUser, _ := d.GetUserByID(uid)
	ssoUser, _ := d.UpsertUser("sso_mgmt@example.com", "SSO User")

	// PasskeysPage - WebAuthn nil -> redirect
	hNil := &AuthHandler{DB: d, Config: cfg, Render: renderMock}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/settings/passkeys", nil)
	hNil.PasskeysPage(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect when WebAuthn nil, got %d", rec.Code)
	}

	// PasskeysPage - unauthenticated -> redirect
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/settings/passkeys", nil)
	h.PasskeysPage(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect when unauthed, got %d", rec.Code)
	}

	// PasskeysPage - SSO user -> redirect
	rec = httptest.NewRecorder()
	req = reqWithUser(d, ssoUser, http.MethodGet, "/settings/passkeys", nil)
	h.PasskeysPage(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for SSO user, got %d", rec.Code)
	}

	// Add credential to DB
	_ = d.CreateWebAuthnCredential(uid, "YubiKey 5", &webauthn.Credential{
		ID:              []byte("key-id-123"),
		PublicKey:       []byte("pubkey-bytes"),
		AttestationType: "none",
	})

	// PasskeysPage - local user -> renders page
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodGet, "/settings/passkeys?success=Passkey+added", nil)
	h.PasskeysPage(rec, req)
	if renderedPage != "settings_passkeys" {
		t.Errorf("expected rendered page 'settings_passkeys', got %q", renderedPage)
	}
	m, ok := renderedData.(map[string]interface{})
	if !ok || m["Success"] != "Passkey added" {
		t.Errorf("expected Success message, got %+v", renderedData)
	}
	credsList, ok := m["Passkeys"].([]db.WebAuthnCredentialInfo)
	if !ok || len(credsList) != 1 || credsList[0].Name != "YubiKey 5" {
		t.Fatalf("expected 1 passkey 'YubiKey 5', got %+v", m["Passkeys"])
	}

	// PasskeyDeletePost - nil WebAuthn -> redirect
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/settings/passkeys/delete", nil)
	hNil.PasskeyDeletePost(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect on nil WebAuthn, got %d", rec.Code)
	}

	// PasskeyDeletePost - unauthenticated / SSO -> redirect
	rec = httptest.NewRecorder()
	req = reqWithUser(d, ssoUser, http.MethodPost, "/settings/passkeys/delete", nil)
	h.PasskeyDeletePost(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for SSO user, got %d", rec.Code)
	}

	// PasskeyDeletePost - missing ID
	form := url.Values{}
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodPost, "/settings/passkeys/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.PasskeyDeletePost(rec, req)
	if !strings.Contains(rec.Header().Get("Location"), "error=Invalid+request") {
		t.Errorf("expected error=Invalid+request redirect, got %s", rec.Header().Get("Location"))
	}

	// PasskeyDeletePost - valid ID
	form = url.Values{"id": {credsList[0].IDBase64}}
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodPost, "/settings/passkeys/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.PasskeyDeletePost(rec, req)
	if !strings.Contains(rec.Header().Get("Location"), "success=Passkey+deleted") {
		t.Errorf("expected success=Passkey+deleted redirect, got %s", rec.Header().Get("Location"))
	}

	// Verify deleted
	afterCreds, _ := d.ListWebAuthnCredentials(uid)
	if len(afterCreds) != 0 {
		t.Errorf("expected 0 credentials after deletion, got %d", len(afterCreds))
	}
}
