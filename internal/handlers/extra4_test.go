package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"

	"github.com/matoy/mypresence/internal/config"
)

// -----------------------------------------------------------------------
// CreateProject — additional paths
// -----------------------------------------------------------------------

func TestCreateProjectAPI_EmptyDates(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"name":       "NoDate Project",
		"code":       "ND01",
		"active":     true,
		"start_date": "",
		"end_date":   "",
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/projects", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateProject)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty dates, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProjectAPI_DateOrderWrong(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"name":       "BadDate Project",
		"code":       "BD01",
		"active":     true,
		"start_date": "2030-01-01",
		"end_date":   "2026-01-01",
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/projects", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateProject)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong date order, got %d", w.Code)
	}
}

func TestCreateProjectAPI_InvalidJSON(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodPost, "/api/admin/projects", []byte("not json"))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateProject)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// UpdateProject — additional paths
// -----------------------------------------------------------------------

func TestUpdateProject_InvalidID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	bodyBytes, _ := json.Marshal(map[string]interface{}{"name": "X", "code": "X"})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/projects/notanumber", bodyBytes)
	req.SetPathValue("id", "notanumber")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateProject)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid ID, got %d", w.Code)
	}
}

func TestUpdateProject_InvalidJSON(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("UpdateMe2", "UM02", 0, true, "2026-01-01", "2030-12-31")
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/projects/"+strconvI64(pid), []byte("bad json"))
	req.SetPathValue("id", strconvI64(pid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateProject)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", w.Code)
	}
}

func TestUpdateProject_DateOrderWrong(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("UpdateDate", "UD01", 0, true, "2026-01-01", "2030-12-31")
	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"name":       "UpdateDate",
		"code":       "UD01",
		"start_date": "2030-01-01",
		"end_date":   "2026-01-01",
	})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/projects/"+strconvI64(pid), bodyBytes)
	req.SetPathValue("id", strconvI64(pid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateProject)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for date order, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// SetProjectTime — extra paths
// -----------------------------------------------------------------------

func TestSetProjectTime_InvalidJSON(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodPost, "/api/project-time", []byte("not json"))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// AdminProjectsPage — report view
// -----------------------------------------------------------------------

func TestAdminProjectsPage_BadTeamID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects?team_id=notanumber", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (bad team_id is silently ignored), got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ChangePasswordPost — missing field errors
// -----------------------------------------------------------------------

func TestChangePasswordPost_MismatchNew(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &SettingsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("mismatch@test.com", "Mismatch", "pass1234")
	tok, _ := d.CreateSession(uid)

	body := []byte("current_password=pass1234&new_password=newpass99&confirm_password=different99")
	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ChangePasswordPost)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ImpersonatePage — as global admin
// -----------------------------------------------------------------------

func TestImpersonatePage_AsAdmin(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &SettingsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/admin/impersonate", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ImpersonatePage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ResetPasswordPost — short password
// -----------------------------------------------------------------------

func TestResetPasswordPost_ShortPassword(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	cfg := &config.Config{AppName: "Test"}
	var rendered string
	h := &ResetPasswordHandler{DB: d, Config: cfg, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}
	_, _ = d.CreateLocalUser("shortpwd@test.com", "Short", "oldpass12")
	rawToken, _ := d.CreatePasswordResetToken("shortpwd@test.com")
	if rawToken == "" {
		t.Skip("could not generate reset token")
	}
	// Password < 8 chars should fail
	body := []byte("token=" + rawToken + "&password=short&confirm=short")
	req := httptest.NewRequest(http.MethodPost, "/reset-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.ResetPasswordPost(w, req)
	if rendered != "reset_password" {
		t.Fatalf("expected reset_password render, got %q", rendered)
	}
}

// -----------------------------------------------------------------------
// PATPage — render
// -----------------------------------------------------------------------

func TestPATPage_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/settings/pat", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.PATPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// SeatsAPI — with date (success path)
// -----------------------------------------------------------------------

func TestSeatsAPI_WithDate(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP SeatsDate", 0)
	d.CreateSeat(fpID, "B1", 0.3, 0.4) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/seats?floorplan_id="+strconvI64(fpID)+"&date=2026-06-01", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SeatsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ListPATs
// -----------------------------------------------------------------------

func TestListPATs_AsAdmin2(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/api/admin/pats", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListPATs)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
