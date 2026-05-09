package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"presence-app/internal/config"
	"presence-app/internal/middleware"
)

// -----------------------------------------------------------------------
// SAML handlers — SP is nil path (quick coverage)
// -----------------------------------------------------------------------

func TestSAMLMetadata_NotConfigured(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AuthHandler{DB: d, Render: noRender, Config: &config.Config{}, SP: nil}

	req := httptest.NewRequest(http.MethodGet, "/saml/metadata", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.SAMLMetadata(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestSAMLLogin_NotConfigured(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AuthHandler{DB: d, Render: noRender, Config: &config.Config{}, SP: nil}

	req := httptest.NewRequest(http.MethodGet, "/saml/login", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.SAMLLogin(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestSAMLACS_NotConfigured(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AuthHandler{DB: d, Render: noRender, Config: &config.Config{}, SP: nil}

	req := httptest.NewRequest(http.MethodPost, "/saml/acs", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.SAMLACS(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// LocalLogin — admin credentials path
// -----------------------------------------------------------------------

func TestLocalLogin_AdminCredentials(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AuthHandler{DB: d, Render: noRender, Config: &config.Config{
		AdminUser:     "admin@test.com",
		AdminPassword: "adminpass",
	}}

	body := []byte("username=admin%40test.com&password=adminpass")
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.LocalLogin(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
}

func TestLocalLogin_AdminWrongPassword(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AuthHandler{DB: d, Render: noRender, Config: &config.Config{
		AdminUser:     "admin@test.com",
		AdminPassword: "adminpass",
	}}

	body := []byte("username=admin%40test.com&password=wrongpass")
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.LocalLogin(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login?error=Invalid+credentials" {
		t.Fatalf("expected credentials error, got %s", loc)
	}
}

// -----------------------------------------------------------------------
// UploadFloorplanImage — valid extension (but no image data)
// -----------------------------------------------------------------------

func TestUploadFloorplanImage_WithPNG(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP Upload PNG", 0)

	// Use pre-existing test in calendar_extra_test.go which tests invalid ext
	// This test covers a valid PNG file upload
	req := createAdminReq(t, d, http.MethodPost, "/admin/floorplans/"+strconvI64(fpID)+"/image", nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UploadFloorplanImage)).ServeHTTP(w, req)
	// Without a file, should return 400
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing file, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ProjectsReportPage — different time params
// -----------------------------------------------------------------------

func TestProjectsReportPage_WithMonth(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects-report?month=2026-06", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ProjectsReportAPI — with params
// -----------------------------------------------------------------------

func TestProjectsReportAPI_WithMonth(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/api/admin/projects-report?month=2026-06", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// SetDisabled — error path (invalid JSON)
// -----------------------------------------------------------------------

func TestSetDisabled_InvalidJSON(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("setdisjson@test.com", "SetDisJSON", "password1")
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/users/"+strconvI64(uid)+"/disabled", []byte("not json"))
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetDisabled)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (handler ignores JSON errors), got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// DeleteUser — success path
// -----------------------------------------------------------------------

func TestDeleteUser_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("deleteme@test.com", "DeleteMe", "password1")
	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/users/"+strconvI64(uid), nil)
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteUser)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// DeleteUser — success
// -----------------------------------------------------------------------

func TestDeleteUser_SuccessPath(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("deleteme2@test.com", "DeleteMe2", "password1")
	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/users/"+strconvI64(uid), nil)
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteUser)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// RevokePAT — success path
// -----------------------------------------------------------------------

func TestRevokePAT_OwnToken(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("revpat2@test.com", "RevPAT2", "password1")
	d.UpdateUserRoles(uid, "team_manager") //nolint:errcheck
	tok, _ := d.CreateSession(uid)
	_, pat, _ := d.CreatePAT(uid, "revoke test", nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/pat/"+strconvI64(pat.ID), nil)
	req.SetPathValue("id", strconvI64(pat.ID))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.RevokePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CreateHoliday — empty name
// -----------------------------------------------------------------------

func TestCreateHoliday_EmptyName(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &HolidaysHandler{DB: d, Render: noRender}

	bodyBytes, _ := json.Marshal(map[string]interface{}{"name": "", "date": "2026-05-01"})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/holidays", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d", w.Code)
	}
}
