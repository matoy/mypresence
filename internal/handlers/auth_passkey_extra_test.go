package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/config"
)

func TestPasskey_Registration_Ceremony_Branches(t *testing.T) {
	d := newCRUDTestDB(t)

	cfg := &config.Config{
		AppName:         "MyPresence",
		EnablePasskeys:  true,
		PasskeyRPID:     "localhost",
		PasskeyRPOrigin: "http://localhost:8080",
	}

	h := &AuthHandler{
		DB:     d,
		Config: cfg,
	}

	uID, _ := d.CreateLocalUser("pk_reg@example.com", "PK Reg", "pass")
	localUser, _ := d.GetUserByID(uID)

	ssoUser, _ := d.UpsertUser("sso_user@example.com", "SSO User")
	ssoID := ssoUser.ID

	// 1. PasskeyRegisterBegin with nil WebAuthn -> 404
	rec := httptest.NewRecorder()
	req := reqWithUser(d, localUser, http.MethodPost, "/webauthn/register/begin", nil)
	h.PasskeyRegisterBegin(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when WebAuthn is nil, got %d", rec.Code)
	}

	// Initialize WebAuthn
	_ = h.InitWebAuthn()

	// 2. PasskeyRegisterBegin without user -> 401
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/register/begin", nil)
	h.PasskeyRegisterBegin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on missing user, got %d", rec.Code)
	}

	// 3. PasskeyRegisterBegin for non-local user -> 403
	rec = httptest.NewRecorder()
	req = reqWithUser(d, ssoUser, http.MethodPost, "/webauthn/register/begin", nil)
	h.PasskeyRegisterBegin(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 on non-local user, got %d", rec.Code)
	}

	// 4. PasskeyRegisterBegin success -> 200 JSON
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodPost, "/webauthn/register/begin", nil)
	h.PasskeyRegisterBegin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on PasskeyRegisterBegin, got %d: %s", rec.Code, rec.Body.String())
	}

	// 5. PasskeyRegisterFinish without user or non-local -> 401
	rec = httptest.NewRecorder()
	req = reqWithUser(d, ssoUser, http.MethodPost, "/webauthn/register/finish", nil)
	h.PasskeyRegisterFinish(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on non-local user register finish, got %d", rec.Code)
	}

	// 6. PasskeyRegisterFinish with missing token -> 400
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodPost, "/webauthn/register/finish", nil)
	h.PasskeyRegisterFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on missing token, got %d", rec.Code)
	}

	// 7. PasskeyRegisterFinish with wrong user in session -> 400
	h.passkeySessions.Store("reg-other-user", &passkeyChallengeEntry{
		UserID:    ssoID,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodPost, "/webauthn/register/finish?token=reg-other-user", nil)
	h.PasskeyRegisterFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on wrong user session in register finish, got %d", rec.Code)
	}

	// 8. PasskeyRegisterFinish with invalid payload -> 400
	h.passkeySessions.Store("reg-valid-tok", &passkeyChallengeEntry{
		UserID:    localUser.ID,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	rec = httptest.NewRecorder()
	req = reqWithUser(d, localUser, http.MethodPost, "/webauthn/register/finish?token=reg-valid-tok", strings.NewReader("bad-assertion"))
	h.PasskeyRegisterFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad payload in register finish, got %d", rec.Code)
	}
}

func TestPasskey_LoginCeremony_Branches(t *testing.T) {
	d := newCRUDTestDB(t)

	cfg := &config.Config{
		AppName:         "MyPresence",
		EnablePasskeys:  true,
		PasskeyRPID:     "localhost",
		PasskeyRPOrigin: "http://localhost:8080",
	}

	h := &AuthHandler{
		DB:     d,
		Config: cfg,
	}

	// 1. PasskeyLoginBegin with nil WebAuthn -> 404
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/begin", nil)
	h.PasskeyLoginBegin(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when WebAuthn is nil, got %d", rec.Code)
	}

	// Initialize WebAuthn
	if err := h.InitWebAuthn(); err != nil {
		t.Fatalf("InitWebAuthn: %v", err)
	}

	// 2. PasskeyLoginBegin with valid WebAuthn -> 200 JSON
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/login/begin", nil)
	h.PasskeyLoginBegin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on PasskeyLoginBegin, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "sessionToken") {
		t.Errorf("expected sessionToken in JSON response")
	}

	// 3. PasskeyLoginFinish with missing token -> 400
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader("{}"))
	h.PasskeyLoginFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on missing token, got %d", rec.Code)
	}

	// 4. PasskeyLoginFinish with unknown token -> 400
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/login/finish?token=unknown-token", strings.NewReader("{}"))
	h.PasskeyLoginFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on unknown token, got %d", rec.Code)
	}

	// 5. PasskeyLoginFinish with expired token -> 400
	h.passkeySessions.Store("expired-tok", &passkeyChallengeEntry{
		ExpiresAt: time.Now().Add(-10 * time.Minute),
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/login/finish?token=expired-tok", strings.NewReader("{}"))
	h.PasskeyLoginFinish(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on expired token, got %d", rec.Code)
	}

	// 6. PasskeyLoginFinish with invalid request payload -> 401
	h.passkeySessions.Store("valid-tok", &passkeyChallengeEntry{
		ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/webauthn/login/finish?token=valid-tok", strings.NewReader("invalid-assertion"))
	h.PasskeyLoginFinish(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on invalid assertion payload, got %d", rec.Code)
	}
}

func TestPasskey_DeletePost_Branches(t *testing.T) {
	d := newCRUDTestDB(t)

	cfg := &config.Config{
		AppName:         "MyPresence",
		EnablePasskeys:  true,
		PasskeyRPID:     "localhost",
		PasskeyRPOrigin: "http://localhost:8080",
	}

	uID1, _ := d.CreateLocalUser("pk_del_user1@example.com", "PK User 1", "pass")
	user1, _ := d.GetUserByID(uID1)

	h := &AuthHandler{
		DB:     d,
		Config: cfg,
	}
	_ = h.InitWebAuthn()

	// 1. Delete with missing id form field -> redirect with error
	form := url.Values{}
	rec := httptest.NewRecorder()
	req := reqWithUser(d, user1, http.MethodPost, "/settings/passkeys/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.PasskeyDeletePost(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303 on missing id, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error=") {
		t.Errorf("expected error in location query: %s", loc)
	}

	// 2. Delete non-existent credential -> still redirects with success
	form = url.Values{"id": {"non-existent-cred-id"}}
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodPost, "/settings/passkeys/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.PasskeyDeletePost(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "success=") {
		t.Errorf("expected success in location query: %s", loc)
	}
}

func TestPasskey_SettingsPage_Rendering(t *testing.T) {
	d := newCRUDTestDB(t)

	cfg := &config.Config{
		AppName:         "MyPresence",
		EnablePasskeys:  true,
		PasskeyRPID:     "localhost",
		PasskeyRPOrigin: "http://localhost:8080",
	}

	uID, _ := d.CreateLocalUser("pk_settings@example.com", "PK Settings User", "pass")
	user, _ := d.GetUserByID(uID)

	var renderedPage string
	var renderedData interface{}
	h := &AuthHandler{
		DB:     d,
		Config: cfg,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			renderedPage = page
			renderedData = data
		},
	}
	_ = h.InitWebAuthn()

	rec := httptest.NewRecorder()
	req := reqWithUser(d, user, http.MethodGet, "/settings/passkeys?success=ok", nil)
	h.PasskeysPage(rec, req)
	if renderedPage != "settings_passkeys" {
		t.Errorf("expected renderedPage settings_passkeys, got %q", renderedPage)
	}
	m, ok := renderedData.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{} data, got %+v", renderedData)
	}
	if m["Success"] != "ok" {
		t.Errorf("expected Success 'ok' in template data, got %v", m["Success"])
	}
}
