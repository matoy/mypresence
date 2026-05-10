package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matoy/myPresence/internal/middleware"
	"github.com/matoy/myPresence/internal/models"
)

// ─── admin.go branches ────────────────────────────────────────────────────────

// TeamsPage for team-leader filters out teams they don't belong to
func TestTeamsPage_TeamLeaderFiltered(t *testing.T) {
	d := newExtraTestDB(t)

	uid, _ := d.CreateLocalUser("tlfilter@test.com", "TLFilter", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck
	myTeam, _ := d.CreateTeam("TLMyTeam")
	d.AddTeamMember(myTeam, uid) //nolint:errcheck
	d.CreateTeam("OtherTeam")    //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	h := &AdminHandler{DB: d, Render: noRender}
	req := httptest.NewRequest(http.MethodGet, "/admin/teams", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.TeamsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// RemoveTeamMember with valid user ID → covers memberName = u.Name branch
func TestRemoveTeamMember_ValidUserID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("removeme@test.com", "RemoveMe", "password1")
	teamID, _ := d.CreateTeam("RemoveMeTeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck

	body, _ := json.Marshal(map[string]interface{}{"user_id": uid})
	req := httptest.NewRequest(http.MethodDelete, "/admin/teams/"+strconvI64(teamID)+"/members/"+strconvI64(uid),
		bytes.NewReader(body))
	req.SetPathValue("id", strconvI64(teamID))
	req.SetPathValue("userId", strconvI64(uid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	adminReq := createAdminReq(t, d, http.MethodDelete,
		"/admin/teams/"+strconvI64(teamID)+"/members/"+strconvI64(uid), nil)
	adminReq.SetPathValue("id", strconvI64(teamID))
	adminReq.SetPathValue("userId", strconvI64(uid))
	middleware.Auth(d, http.HandlerFunc(h.RemoveTeamMember)).ServeHTTP(w, adminReq)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// CreateTeam DB error (close after auth)
func TestCreateTeam_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	body, _ := json.Marshal(map[string]string{"name": "NewTeam"})
	req := createAdminReq(t, d, http.MethodPost, "/admin/teams", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.CreateTeam(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ToggleStatusDisabled invalid JSON
func TestToggleStatusDisabled_InvalidJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	statusID, _ := d.CreateStatus(models.Status{Name: "ToggleStatus", Color: "#000000", SortOrder: 1})
	req := createAdminReq(t, d, http.MethodPatch,
		"/admin/statuses/"+strconvI64(statusID)+"/disabled",
		[]byte("invalid json{"))
	req.SetPathValue("id", strconvI64(statusID))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ToggleStatusDisabled)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// DeleteStatus when status is in use → covers "status_in_use" error path
func TestDeleteStatus_InUse(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	// Create a status and a user with presences using it
	statusID, _ := d.CreateStatus(models.Status{Name: "InUseStatus", Color: "#ff0000", SortOrder: 1})
	uid, _ := d.CreateLocalUser("inuse@test.com", "InUse", "password1")
	d.SetPresences(uid, []string{"2026-01-05"}, statusID, "") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodDelete, "/admin/statuses/"+strconvI64(statusID), nil)
	req.SetPathValue("id", strconvI64(statusID))

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteStatus)).ServeHTTP(w, req)
	// status_in_use returns 409 Conflict
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── admin_holidays.go branches ───────────────────────────────────────────────

// UpdateHoliday with invalid ID
func TestUpdateHoliday_InvalidID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}

	body, _ := json.Marshal(map[string]string{"date": "2026-01-01", "name": "Test"})
	req := createAdminReq(t, d, http.MethodPut, "/admin/holidays/notanumber", body)
	req.SetPathValue("id", "notanumber")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// UpdateHoliday with invalid JSON
func TestUpdateHoliday_InvalidJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodPut, "/admin/holidays/1", []byte("invalid{"))
	req.SetPathValue("id", "1")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// UpdateHoliday with empty fields
func TestUpdateHoliday_EmptyFields(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}

	body, _ := json.Marshal(map[string]string{"date": "", "name": ""})
	req := createAdminReq(t, d, http.MethodPut, "/admin/holidays/1", body)
	req.SetPathValue("id", "1")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// DeleteHoliday with invalid ID
func TestDeleteHoliday_InvalidID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodDelete, "/admin/holidays/notanumber", nil)
	req.SetPathValue("id", "notanumber")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── calendar.go branches ─────────────────────────────────────────────────────

// CalendarPage db error via close-after-auth
func TestCalendarPage_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/calendar?year=2026&month=1", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.CalendarPage(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// SetPresences — empty dates (different from NoDates in calendar_extra_test.go - uses admin user)
func TestSetPresences_EmptyDates2(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	statusID, _ := d.CreateStatus(models.Status{Name: "Office2", Color: "#ff0000", SortOrder: 1})
	// Make user_id match the admin
	uid, _ := d.CreateLocalUser("emptydate2@test.com", "Empty2", "password1")
	tok, _ := d.CreateSession(uid)
	bodyMap := map[string]interface{}{
		"user_id":   uid,
		"dates":     []string{},
		"status_id": statusID,
		"half":      "",
	}
	body2, _ := json.Marshal(bodyMap)
	req2 := httptest.NewRequest(http.MethodPost, "/api/presences", bytes.NewReader(body2))
	req2.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req2.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req2)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// SetPresences — non-imputable holiday
func TestSetPresences_NonImputableHoliday(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	statusID, _ := d.CreateStatus(models.Status{Name: "Office4", Color: "#ff0000", SortOrder: 1})
	d.CreateHoliday("2026-01-01", "NewYear", false) //nolint:errcheck
	uid, _ := d.CreateLocalUser("nonimputable@test.com", "NonImputable", "password1")
	tok, _ := d.CreateSession(uid)

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":   uid,
		"dates":     []string{"2026-01-01"},
		"status_id": statusID,
		"half":      "",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/presences", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

// GetPresencesAPI — DB error
func TestGetPresencesAPI_DBError2(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("PresAPITeam")
	req := createAdminReq(t, d, http.MethodGet,
		"/api/presences?team_id="+strconvI64(teamID)+"&year=2026&month=1", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.GetPresencesAPI(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── settings.go branches ─────────────────────────────────────────────────────

// ChangePasswordPage for non-local user → redirect
func TestChangePasswordPage_NonLocalUser(t *testing.T) {
	d := newExtraTestDB(t)
	h := &SettingsHandler{DB: d, Render: noRender}

	// Create a non-local user (SSO user - no password hash) via UpsertUser
	u, _ := d.UpsertUser("ssouser@test.com", "SSOUser")
	tok, _ := d.CreateSession(u.ID)

	req := httptest.NewRequest(http.MethodGet, "/settings/change-password", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ChangePasswordPage)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", w.Code, w.Body.String())
	}
}

// ImpersonatePost — target is disabled → redirect
func TestImpersonatePost_DisabledTarget(t *testing.T) {
	d := newExtraTestDB(t)
	h := &SettingsHandler{DB: d, Render: noRender}

	targetUID, _ := d.CreateLocalUser("disabledtarget@test.com", "DisabledTarget", "password1")
	d.SetUserDisabled(targetUID, true) //nolint:errcheck

	body := []byte("login=disabledtarget%40test.com")
	req := createAdminReq(t, d, http.MethodPost, "/admin/impersonate", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ImpersonatePost)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 for disabled target, got %d: %s", w.Code, w.Body.String())
	}
}

// ImpersonatePost — target is the admin themselves → redirect
func TestImpersonatePost_SelfImpersonate(t *testing.T) {
	d := newExtraTestDB(t)
	h := &SettingsHandler{DB: d, Render: noRender}

	// Admin impersonating themselves
	adminUID, _ := d.CreateLocalUser("selfimper@test.com", "SelfImper", "password1")
	d.UpdateUserRoles(adminUID, string(models.RoleGlobal)) //nolint:errcheck
	tok, _ := d.CreateSession(adminUID)

	req := httptest.NewRequest(http.MethodPost, "/admin/impersonate",
		strings.NewReader("login=selfimper%40test.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ImpersonatePost)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 for self-impersonate, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── projects.go branches ─────────────────────────────────────────────────────

// ProjectsAPI — invalid month
func TestProjectsAPI_InvalidMonth(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/api/projects?year=2026&month=13", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ProjectTimeAPI — invalid month
func TestProjectTimeAPI_InvalidMonth(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/api/project-time?year=2026&month=13", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectTimeAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// SetProjectTime — project not found/inactive
func TestSetProjectTime_InactiveProject(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	// Create an inactive project
	projID, _ := d.CreateProject("InactiveProj", "INV", 0, false, "2026-01-01", "2026-12-31")
	body, _ := json.Marshal(map[string]interface{}{
		"project_id": projID,
		"year":       2026,
		"month":      1,
		"days":       5.0,
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/project-time", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// SetProjectTime — project ended before this month
func TestSetProjectTime_ProjectEnded(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	// Project that ended in 2025
	projID, _ := d.CreateProject("EndedProj", "END", 0, true, "2025-01-01", "2025-12-31")
	body, _ := json.Marshal(map[string]interface{}{
		"project_id": projID,
		"year":       2026,
		"month":      1,
		"days":       5.0,
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/project-time", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// SetProjectTime — exceeds billable cap
func TestSetProjectTime_ExceedsCap(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	statusID, _ := d.CreateStatus(models.Status{Name: "BillableStatus", Color: "#00ff00", Billable: true, SortOrder: 1})

	// Create user and add presences for billable days
	uid, _ := d.CreateLocalUser("cappeduser@test.com", "Capped", "password1")
	tok, _ := d.CreateSession(uid)

	// Give user only 1 billable day
	d.SetPresences(uid, []string{"2026-01-05"}, statusID, "") //nolint:errcheck

	projID, _ := d.CreateProject("CappedProj", "CAP", 0, true, "2026-01-01", "2026-12-31")

	body, _ := json.Marshal(map[string]interface{}{
		"project_id": projID,
		"year":       2026,
		"month":      1,
		"days":       10.0, // more than 1 billable day
	})
	req := httptest.NewRequest(http.MethodPost, "/api/project-time", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

// AdminProjectsPage — filter by active=0, text, team
func TestAdminProjectsPage_Filters(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("FilteredTeam")
	d.CreateProject("ActiveProject", "ACTP", teamID, true, "2026-01-01", "2026-12-31")     //nolint:errcheck
	d.CreateProject("InactiveProject", "INACT", teamID, false, "2026-01-01", "2026-12-31") //nolint:errcheck

	// Test filter by active=0 (show only inactive)
	req := createAdminReq(t, d, http.MethodGet,
		"/admin/projects?active=0&q=Inactive&team="+strconvI64(teamID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// AdminProjectsPage — filter active=1 (default)
func TestAdminProjectsPage_FilterActive1(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("FilteredTeam2")
	d.CreateProject("ActiveProject2", "ACTP2", teamID, true, "2026-01-01", "2026-12-31")     //nolint:errcheck
	d.CreateProject("InactiveProject2", "INACT2", teamID, false, "2026-01-01", "2026-12-31") //nolint:errcheck

	// Test filter active=1 (only active projects)
	req := createAdminReq(t, d, http.MethodGet,
		"/admin/projects?active=1", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
