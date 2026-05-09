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

// ─── admin_users.go DB error branches ─────────────────────────────────────────

// CreateUser missing fields (covers L.53-57)
func TestCreateUser_MissingFields(t *testing.T) {
	d := newExtraTestDB(t)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	body, _ := json.Marshal(map[string]interface{}{
		"email": "someuser@test.com",
		// name and password missing
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/users", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateUser)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// UpdateUser missing fields (covers L.83-87) + DB error (covers L.88-92)
func TestUpdateUser_MissingFields(t *testing.T) {
	d := newExtraTestDB(t)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("updateme@test.com", "UpdateMe", "password1")
	body, _ := json.Marshal(map[string]interface{}{
		"email": "", // missing
	})
	req := createAdminReq(t, d, http.MethodPut, "/admin/users/"+strconvI64(uid), body)
	req.SetPathValue("id", strconvI64(uid))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateUser)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// UpdateUser DB error (covers L.88-92)
func TestUpdateUser_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("updateme2@test.com", "UpdateMe2", "password1")
	body, _ := json.Marshal(map[string]interface{}{
		"email": "updateme2@test.com",
		"name":  "UpdatedName",
	})
	req := createAdminReq(t, d, http.MethodPut, "/admin/users/"+strconvI64(uid), body)
	req.SetPathValue("id", strconvI64(uid))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.UpdateUser(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusConflict && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 409 or 500, got %d: %s", w.Code, w.Body.String())
	}
}

// SetPassword DB error
func TestSetPassword_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("setpwdme@test.com", "SetPwdMe", "password1")
	body, _ := json.Marshal(map[string]interface{}{
		"password": "newpassword1",
	})
	req := createAdminReq(t, d, http.MethodPut, "/admin/users/"+strconvI64(uid)+"/password", body)
	req.SetPathValue("id", strconvI64(uid))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.SetPassword(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// SetDisabled DB error
func TestSetDisabled_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("setdisabledme@test.com", "SetDisabledMe", "password1")
	body, _ := json.Marshal(map[string]bool{"disabled": true})
	req := createAdminReq(t, d, http.MethodPatch, "/admin/users/"+strconvI64(uid)+"/disabled", body)
	req.SetPathValue("id", strconvI64(uid))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.SetDisabled(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── projects.go report branches ──────────────────────────────────────────────

// ProjectsReportPage for team leader (covers teamIDFilter branch)
func TestProjectsReportPage_TeamLeader(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("tlreport@test.com", "TLReport", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck
	teamID, _ := d.CreateTeam("TLReportTeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	// Create some projects for filtering
	d.CreateProject("TLProj", "TLP", teamID, true, "2026-01-01", "2026-12-31") //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/admin/projects-report?active=0&q=TLProj", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ProjectsReportAPI for team leader (covers teamIDFilter branch)
func TestProjectsReportAPI_TeamLeader(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("tlreportapi@test.com", "TLReportAPI", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck
	teamID, _ := d.CreateTeam("TLReportAPITeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	d.CreateProject("TLAPIProj", "TLAP", teamID, true, "2026-01-01", "2026-12-31") //nolint:errcheck

	// filter with text, active=0, team
	req := httptest.NewRequest(http.MethodGet,
		"/api/projects-report?active=0&q=TLAPIProj&team="+strconvI64(teamID), nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// SetProjectTime — re-entry updating existing (covers existing = e.Days branch)
func TestSetProjectTime_UpdateExisting(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	statusID, _ := d.CreateStatus(models.Status{Name: "BillableOK", Color: "#00ff00", Billable: true, SortOrder: 1})
	uid, _ := d.CreateLocalUser("updateentry@test.com", "UpdateEntry", "password1")
	tok, _ := d.CreateSession(uid)

	// Give user 10 billable days
	dates := []string{"2026-01-05", "2026-01-06", "2026-01-07", "2026-01-08", "2026-01-09",
		"2026-01-12", "2026-01-13", "2026-01-14", "2026-01-15", "2026-01-16"}
	d.SetPresences(uid, dates, statusID, "") //nolint:errcheck

	projID, _ := d.CreateProject("UpdateEntryProj", "UEP", 0, true, "2026-01-01", "2026-12-31")

	// First submission: 5 days
	body1, _ := json.Marshal(map[string]interface{}{"project_id": projID, "year": 2026, "month": 1, "days": 5.0})
	req1 := httptest.NewRequest(http.MethodPost, "/api/project-time", bytes.NewReader(body1))
	req1.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	w1.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 for first entry, got %d: %s", w1.Code, w1.Body.String())
	}

	// Second submission: update to 8 days (should pass cap since existing=5, new=8, billable=10)
	body2, _ := json.Marshal(map[string]interface{}{"project_id": projID, "year": 2026, "month": 1, "days": 8.0})
	req2 := httptest.NewRequest(http.MethodPost, "/api/project-time", bytes.NewReader(body2))
	req2.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	w2.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for update, got %d: %s", w2.Code, w2.Body.String())
	}
}

// ─── pat.go branches ──────────────────────────────────────────────────────────

// CreatePAT DB error (covers L.93-96)
func TestCreatePAT_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &PATHandler{DB: d, Render: noRender}

	body, _ := json.Marshal(map[string]interface{}{"description": "My test token", "expires_in": 30})
	req := createAdminReq(t, d, http.MethodPost, "/api/tokens", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.CreatePAT(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// RevokePAT not found
func TestRevokePAT_NotFound(t *testing.T) {
	d := newExtraTestDB(t)
	h := &PATHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodDelete, "/settings/pat/9999", nil)
	req.SetPathValue("id", "9999")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.RevokePAT)).ServeHTTP(w, req)
	// RevokePAT with non-existent PAT should return 404 or error
	if w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 404 or 500, got %d: %s", w.Code, w.Body.String())
	}
}
