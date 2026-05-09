package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"presence-app/internal/middleware"
	"presence-app/internal/models"
)

// ─── reset_password.go branches ───────────────────────────────────────────────

// ForgotPasswordPost DB error (covers L.51-55)
func TestForgotPasswordPost_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ResetPasswordHandler{DB: d, Render: noRender}

	// Create a user with a local account (so token creation is attempted)
	d.CreateLocalUser("fwdpw@test.com", "FwdPw", "password1") //nolint:errcheck

	form := url.Values{"email": {"fwdpw@test.com"}}
	req := httptest.NewRequest(http.MethodPost, "/forgot-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Close DB to trigger CreatePasswordResetToken error
	d.Close()

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.ForgotPasswordPost(w, req)
	// Even on error, renders "sent" silently (200)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (silent), got %d: %s", w.Code, w.Body.String())
	}
}

// ResetPasswordPost rate limited (covers L.92-95)
func TestResetPasswordPost_RateLimited(t *testing.T) {
	d := newExtraTestDB(t)
	rl := middleware.NewLoginRateLimiter()
	defer rl.Close()
	h := &ResetPasswordHandler{DB: d, Render: noRender, RateLimiter: rl}

	dummy := httptest.NewRequest(http.MethodPost, "/reset-password", nil)
	dummy.RemoteAddr = "192.168.50.2:12345"
	for i := 0; i < 5; i++ {
		rl.RecordFailure(dummy)
	}

	form := url.Values{"token": {"sometoken"}, "password": {"newpassword1"}, "confirm": {"newpassword1"}}
	req := httptest.NewRequest(http.MethodPost, "/reset-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.168.50.2:12345"

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.ResetPasswordPost(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", w.Code, w.Body.String())
	}
}

// ResetPasswordPost missing fields (covers L.112-115)
func TestResetPasswordPost_MissingFields(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ResetPasswordHandler{DB: d, Render: noRender}

	form := url.Values{"token": {"sometoken"}, "password": {""}, "confirm": {""}}
	req := httptest.NewRequest(http.MethodPost, "/reset-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.ResetPasswordPost(w, req)
	if w.Code != http.StatusOK { // renders error page
		t.Fatalf("expected 200 (error page), got %d: %s", w.Code, w.Body.String())
	}
}

// ResetPasswordPost SetUserPassword DB error (covers L.131-134)
func TestResetPasswordPost_SetPasswordDBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ResetPasswordHandler{DB: d, Render: noRender}

	// Create a user and a reset token
	d.CreateLocalUser("resetpw@test.com", "ResetPw", "oldpassword1") //nolint:errcheck
	rawToken, err := d.CreatePasswordResetToken("resetpw@test.com")
	if err != nil || rawToken == "" {
		t.Skip("could not create reset token")
	}

	// Close the DB so SetUserPassword fails
	d.Close()

	form := url.Values{
		"token":    {rawToken},
		"password": {"newpassword1"},
		"confirm":  {"newpassword1"},
	}
	req := httptest.NewRequest(http.MethodPost, "/reset-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.ResetPasswordPost(w, req)
	// renders error page (200 with error) or could be 500
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── settings.go branches ────────────────────────────────────────────────────

// UserLogsPage with days query param (covers L.26-29)
func TestUserLogsPage_DaysParam(t *testing.T) {
	d := newExtraTestDB(t)
	h := &SettingsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("logsdays@test.com", "LogsDays", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/settings/logs?days=30", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.MyLogsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ChangePasswordPost SetUserPassword DB error (covers L.108-111)
func TestChangePasswordPost_SetPasswordDBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &SettingsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("changepwdb@test.com", "ChangePwDB", "oldpassword1")
	tok, _ := d.CreateSession(uid)

	form := url.Values{
		"current":  {"oldpassword1"},
		"password": {"newpassword1"},
		"confirm":  {"newpassword1"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/change-password",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.ChangePasswordPost(rw, r)
	})).ServeHTTP(w, req)
	// Should redirect to error page
	if w.Code != http.StatusSeeOther && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 303 or 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ImpersonatePage — only the admin themselves is in the list (filtered out by L.136)
func TestImpersonatePage_SelfOnly(t *testing.T) {
	d := newExtraTestDB(t)
	h := &SettingsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("selfonly@test.com", "SelfOnly", "password1")
	d.UpdateUserRoles(uid, "global") //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/impersonate", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ImpersonatePage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── admin.go CreateStatus DB error ──────────────────────────────────────────

// CreateStatus DB error (covers L.239-243)
func TestCreateStatus_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	body := `{"name":"TestStatus","color":"#ff0000","billable":false,"on_site":false,"sort_order":1}`
	req := createAdminReq(t, d, http.MethodPost, "/admin/statuses", []byte(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.CreateStatus(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// DeleteStatus with non-conflict DB error (covers L.287-289)
func TestDeleteStatus_OtherDBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	// Create a status first
	sid, _ := d.CreateStatus(models.Status{Name: "DelStatusOther", Color: "#cccccc", Billable: false, SortOrder: 99})

	req := createAdminReq(t, d, http.MethodDelete, "/admin/statuses/"+strconvI64(sid), nil)
	req.SetPathValue("id", strconvI64(sid))

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.DeleteStatus(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusConflict && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 409 or 500, got %d: %s", w.Code, w.Body.String())
	}
}

// pat.go invalid JSON (covers L.63-67)
func TestCreatePAT_InvalidJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &PATHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodPost, "/api/tokens", []byte("not json"))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreatePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// AdminRevokePAT non-admin (covers L.137-141)
func TestAdminRevokePAT_NonAdmin(t *testing.T) {
	d := newExtraTestDB(t)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("nonadminpat@test.com", "NonAdmin", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/tokens/1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminRevokePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
