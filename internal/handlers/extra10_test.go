package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"presence-app/internal/middleware"
	"presence-app/internal/models"
)

// -----------------------------------------------------------------------
// CreatePAT — description too long and invalid expires_in
// -----------------------------------------------------------------------

func TestCreatePAT_DescriptionTooLong(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("longtokdesc@test.com", "LongDesc", "password1")
	d.UpdateUserRoles(uid, string(models.RoleGlobal)) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	longDesc := make([]byte, 201)
	for i := range longDesc {
		longDesc[i] = 'x'
	}
	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"description": string(longDesc),
		"expires_in":  30,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tokens", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreatePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreatePAT_InvalidExpiresIn(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("badexpiry@test.com", "BadExpiry", "password1")
	d.UpdateUserRoles(uid, string(models.RoleGlobal)) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"description": "valid desc",
		"expires_in":  -1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tokens", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreatePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CreatePAT — with valid expires_in (covers the expiresAt branch)
// -----------------------------------------------------------------------

func TestCreatePAT_WithExpiresIn(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("expiresin@test.com", "ExpiresIn", "password1")
	d.UpdateUserRoles(uid, string(models.RoleGlobal)) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"description": "expires in 30 days",
		"expires_in":  30,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tokens", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreatePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// SetDisabled — self-disable attempt (covers self-disable check)
// -----------------------------------------------------------------------

func TestSetDisabled_SelfDisable(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("selfdisable@test.com", "SelfDisable", "password1")
	d.UpdateUserRoles(uid, string(models.RoleGlobal)) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	bodyBytes, _ := json.Marshal(map[string]interface{}{"disabled": true})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/"+strconvI64(uid)+"/disabled",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetDisabled)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for self-disable, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// DeleteUser — self-delete attempt
// -----------------------------------------------------------------------

func TestDeleteUser_SelfDelete(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("selfdelete@test.com", "SelfDelete", "password1")
	d.UpdateUserRoles(uid, string(models.RoleGlobal)) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/users/"+strconvI64(uid), nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteUser)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for self-delete, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// UserLogsPage — all days filter (days=0)
// -----------------------------------------------------------------------

func TestUserLogsPage_AllHistory(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("logsalldays@test.com", "LogsAllDays", "password1")

	req := createAdminReq(t, d, http.MethodGet, "/admin/users/"+strconvI64(uid)+"/logs?days=0", nil)
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UserLogsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// UpdateHoliday — invalid ID
// -----------------------------------------------------------------------

func TestUpdateHoliday_BadID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &HolidaysHandler{DB: d, Render: noRender}

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"date": "2026-06-01", "name": "Test",
	})
	req := createAdminReq(t, d, http.MethodPut, "/admin/holidays/notanid", bodyBytes)
	req.SetPathValue("id", "notanid")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ImpersonatePost — non-global user gets 403
// -----------------------------------------------------------------------

func TestImpersonatePost_NonGlobal(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &SettingsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("nonglobal2@test.com", "NonGlobal2", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodPost, "/impersonate",
		bytes.NewReader([]byte("login=target@test.com")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ImpersonatePost)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// RevokePAT — invalid id
// -----------------------------------------------------------------------

func TestRevokePAT_BadID2(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("revokebadid@test.com", "RevokeBadID", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodDelete, "/api/tokens/notanumber", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req.SetPathValue("id", "notanumber")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.RevokePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
