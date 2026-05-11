package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// TeamsPage and ListTeamsAPI
// -----------------------------------------------------------------------

func TestTeamsPage_AsTeamLeader(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("teamleader@test.com", "Team Leader", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck
	tid, _ := d.CreateTeam("Leader's Team")
	d.AddTeamMember(tid, uid) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/admin/teams", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.TeamsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestListTeamsAPI_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/api/teams", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListTeamsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// isUserInTeam — triggered via AddTeamMember with team leader not in team
// -----------------------------------------------------------------------

func TestIsUserInTeam_Forbidden(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	// Create team leader not in any team
	uid, _ := d.CreateLocalUser("notinteam@test.com", "Not In Team", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	tid, _ := d.CreateTeam("Other Team")
	target, _ := d.CreateLocalUser("target@test.com", "Target", "password1")

	bodyBytes, _ := json.Marshal(map[string]interface{}{"user_id": target})
	req := httptest.NewRequest(http.MethodPost, "/admin/teams/"+strconvI64(tid)+"/members", bytes.NewReader(bodyBytes))
	req.SetPathValue("id", strconvI64(tid))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AddTeamMember)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestIsUserInTeam_AllowedWhenInTeam(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("inteam@test.com", "In Team", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck
	tid, _ := d.CreateTeam("My Team")
	d.AddTeamMember(tid, uid) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	target, _ := d.CreateLocalUser("target2@test.com", "Target2", "password2")
	bodyBytes, _ := json.Marshal(map[string]interface{}{"user_id": target})
	req := httptest.NewRequest(http.MethodPost, "/admin/teams/"+strconvI64(tid)+"/members", bytes.NewReader(bodyBytes))
	req.SetPathValue("id", strconvI64(tid))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AddTeamMember)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// RemoveTeamMember — team leader not in team (forbidden)
// -----------------------------------------------------------------------

func TestRemoveTeamMember_Forbidden(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("notinteam2@test.com", "Not In Team 2", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	tid, _ := d.CreateTeam("Other Team 2")
	memberUID, _ := d.CreateLocalUser("member2@test.com", "Member2", "password1")
	d.AddTeamMember(tid, memberUID) //nolint:errcheck

	req := httptest.NewRequest(http.MethodDelete, "/admin/teams/"+strconvI64(tid)+"/members/"+strconvI64(memberUID), nil)
	req.SetPathValue("id", strconvI64(tid))
	req.SetPathValue("userId", strconvI64(memberUID))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.RemoveTeamMember)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// CreateStatus — error and success paths
// -----------------------------------------------------------------------

func TestCreateStatus_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"name":  "New Status",
		"color": "#ff0000",
		"icon":  "🏠",
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/statuses", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateStatus)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateStatus_EmptyName(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"name":  "",
		"color": "#ff0000",
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/statuses", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateStatus)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// DeleteStatus — success
// -----------------------------------------------------------------------

func TestDeleteStatus_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	sid, _ := d.CreateStatus(models.Status{Name: "DeleteMe", Color: "#abc"})

	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/statuses/"+strconvI64(sid), nil)
	req.SetPathValue("id", strconvI64(sid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteStatus)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// AdminListSeats
// -----------------------------------------------------------------------

func TestAdminListSeats_MissingFloorplanID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodGet, "/api/admin/seats", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminListSeats)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAdminListSeats_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP Admin", 0)

	req := createAdminReq(t, d, http.MethodGet, "/api/admin/seats?floorplan_id="+strconvI64(fpID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminListSeats)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ListFloorplansAPI
// -----------------------------------------------------------------------

func TestListFloorplansAPI_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodGet, "/api/floorplans", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListFloorplansAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ListSeatsForFloorplanAPI
// -----------------------------------------------------------------------

func TestListSeatsForFloorplanAPI_MissingDate(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP ForSeats", 0)
	req := createAdminReq(t, d, http.MethodGet, "/api/floorplans/"+strconvI64(fpID)+"/seats", nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListSeatsForFloorplanAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without date, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListSeatsForFloorplanAPI_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP ForSeats2", 0)
	req := createAdminReq(t, d, http.MethodGet, "/api/floorplans/"+strconvI64(fpID)+"/seats?date=2026-06-01", nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListSeatsForFloorplanAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// BulkReserveSeats — success with valid seat
// -----------------------------------------------------------------------

func TestBulkReserveSeats_WithSeat(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP Bulk2", 0)
	seatID, _ := d.CreateSeat(fpID, "A1", 0.5, 0.5)

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"seat_id": seatID,
		"dates":   []string{"2026-06-01", "2026-06-02"},
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/floorplans/reserve-bulk", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.BulkReserveSeats)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// HolidaysPage — test with extra data
// -----------------------------------------------------------------------

func TestHolidaysPage_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &HolidaysHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/admin/holidays?year=2026", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.HolidaysPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// SetPassword — error paths
// -----------------------------------------------------------------------

func TestSetPassword_EmptyPassword(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("setpwderr@test.com", "PwdErr", "oldpass1")
	bodyBytes, _ := json.Marshal(map[string]interface{}{"password": ""})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/users/"+strconvI64(uid)+"/password", bodyBytes)
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPassword)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty password, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ImpersonateExitPost — no real_session cookie
// -----------------------------------------------------------------------

func TestImpersonateExitPost_NoCookie(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &SettingsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodPost, "/settings/impersonate/exit", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ImpersonateExitPost)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
}

func TestImpersonateExitPost_InvalidRealSession(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &SettingsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodPost, "/settings/impersonate/exit", nil)
	req.AddCookie(&http.Cookie{Name: "real_session", Value: "invalidtoken"})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ImpersonateExitPost)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// AdminProjectsPage — no team filter (bare page)
// -----------------------------------------------------------------------

func TestAdminProjectsPage_NoFilter(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ActivityAPI — extra params
// -----------------------------------------------------------------------

func TestActivityAPI_WithTeam(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ActivityHandler{DB: d, Render: noRender}

	tid, _ := d.CreateTeam("ActivityAPITeam")
	req := createAdminReq(t, d, http.MethodGet, "/api/activity?team_id="+strconvI64(tid)+"&year=2026&month=1", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// UpdateStatus
// -----------------------------------------------------------------------

func TestUpdateStatus_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	sid, _ := d.CreateStatus(models.Status{Name: "UpdateMe", Color: "#abc"})
	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"name":  "Updated Status",
		"color": "#00ff00",
		"icon":  "🏠",
	})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/statuses/"+strconvI64(sid), bodyBytes)
	req.SetPathValue("id", strconvI64(sid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateStatus)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// SetProjectTime — missing project
// -----------------------------------------------------------------------

func TestSetProjectTime_MissingProject(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	bodyBytes, _ := json.Marshal(map[string]interface{}{"project_id": 9999999, "year": 2026, "month": 6, "days": 0.0})
	req := createAdminReq(t, d, http.MethodPost, "/api/project-time", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing project, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ChangePasswordPost — redirects to /settings/change-password
// -----------------------------------------------------------------------

func TestChangePasswordPost_Redirect(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &SettingsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("redirect@test.com", "Redirect", "pass1234")
	tok, _ := d.CreateSession(uid)

	body := []byte("current_password=pass1234&new_password=newpass99&confirm_password=newpass99")
	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ChangePasswordPost)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", w.Code, w.Body.String())
	}
}
