package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"presence-app/internal/middleware"
	"presence-app/internal/models"

	"presence-app/internal/config"
)

// -----------------------------------------------------------------------
// ActivityAPI — team leader forbidden path & filterTeamsForUser
// -----------------------------------------------------------------------

func TestActivityAPI_TeamLeaderForbidden(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ActivityHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("tleader@test.com", "Team Leader", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	otherTid, _ := d.CreateTeam("Other Team For Activity")

	req := httptest.NewRequest(http.MethodGet, "/api/activity?team_id="+strconvI64(otherTid)+"&year=2026&month=1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for team leader outside their team, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActivityAPI_TeamLeaderAllowed(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ActivityHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("tleader2@test.com", "Team Leader 2", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck
	tid, _ := d.CreateTeam("Leader's Team 2")
	d.AddTeamMember(tid, uid) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/api/activity?team_id="+strconvI64(tid)+"&year=2026&month=1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// AdminFloorplansPage — extra path (floorplan with seats)
// -----------------------------------------------------------------------

func TestAdminFloorplansPage_WithFloorplans(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	d.CreateFloorplan("FP A", 0) //nolint:errcheck
	req := createAdminReq(t, d, http.MethodGet, "/admin/floorplans", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminFloorplansPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// BulkReserveSeats — missing body/seat_id
// -----------------------------------------------------------------------

func TestBulkReserveSeats_MissingSeatID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	bodyBytes, _ := json.Marshal(map[string]interface{}{"seat_id": 0, "dates": []string{"2026-06-01"}})
	req := createAdminReq(t, d, http.MethodPost, "/api/floorplans/reserve-bulk", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.BulkReserveSeats)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// AdminRevokePAT — more paths
// -----------------------------------------------------------------------

func TestAdminRevokePAT_SuccessPath(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("adminrevoke@test.com", "AdminRevoke", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamManager)) //nolint:errcheck
	_, pat, _ := d.CreatePAT(uid, "test token", nil)

	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/pats/"+strconvI64(pat.ID), nil)
	req.SetPathValue("id", strconvI64(pat.ID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminRevokePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminRevokePAT_BadID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/pats/notanumber", nil)
	req.SetPathValue("id", "notanumber")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminRevokePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CreateProject — error paths
// -----------------------------------------------------------------------

func TestCreateProjectAPI_EmptyName(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"name":       "",
		"code":       "EP01",
		"active":     true,
		"start_date": "2026-01-01",
		"end_date":   "2030-12-31",
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/projects", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateProject)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProjectAPI_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"name":       "API Project",
		"code":       "AP01",
		"active":     true,
		"start_date": "2026-01-01",
		"end_date":   "2030-12-31",
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/projects", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateProject)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// UpdateProject — success and error paths
// -----------------------------------------------------------------------

func TestUpdateProject_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("OldName", "OLD1", 0, true, "2026-01-01", "2030-12-31")
	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"name":       "NewName",
		"code":       "NEW1",
		"active":     true,
		"start_date": "2026-01-01",
		"end_date":   "2030-12-31",
	})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/projects/"+strconvI64(pid), bodyBytes)
	req.SetPathValue("id", strconvI64(pid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateProject)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateProject_EmptyName(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("ToUpdate", "TU01", 0, true, "2026-01-01", "2030-12-31")
	bodyBytes, _ := json.Marshal(map[string]interface{}{"name": "", "code": "TU01"})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/projects/"+strconvI64(pid), bodyBytes)
	req.SetPathValue("id", strconvI64(pid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateProject)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// UpdateHoliday — success path
// -----------------------------------------------------------------------

func TestUpdateHoliday_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &HolidaysHandler{DB: d, Render: noRender}

	hid, _ := d.CreateHoliday("2026-12-25", "Christmas", false)
	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"name": "Christmas Day",
		"date": "2026-12-25",
	})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/holidays/"+strconvI64(hid), bodyBytes)
	req.SetPathValue("id", strconvI64(hid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateHoliday_EmptyName(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &HolidaysHandler{DB: d, Render: noRender}

	hid, _ := d.CreateHoliday("2026-01-01", "NewYear", false)
	bodyBytes, _ := json.Marshal(map[string]interface{}{"name": "", "date": "2026-01-01"})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/holidays/"+strconvI64(hid), bodyBytes)
	req.SetPathValue("id", strconvI64(hid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// DeleteHoliday — success
// -----------------------------------------------------------------------

func TestDeleteHoliday_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &HolidaysHandler{DB: d, Render: noRender}

	hid, _ := d.CreateHoliday("2026-04-05", "EasterDel", false)
	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/holidays/"+strconvI64(hid), nil)
	req.SetPathValue("id", strconvI64(hid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// LocalLogin — extra paths (no SAML context needed)
// -----------------------------------------------------------------------

func TestLocalLogin_InvalidPassword(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AuthHandler{DB: d, Render: noRender, Config: &config.Config{AppName: "Test"}}

	d.CreateLocalUser("loginbad@test.com", "LoginBad", "correctpassword") //nolint:errcheck

	body := []byte("login=loginbad%40test.com&password=wrongpassword")
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

// -----------------------------------------------------------------------
// CancelReservationsByDates — success path
// -----------------------------------------------------------------------

func TestCancelReservationsByDates_WithDates(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"dates": []string{"2026-06-01", "2026-06-02"},
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/floorplans/cancel-by-dates", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CancelReservationsByDates)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CreatePAT — missing description
// -----------------------------------------------------------------------

func TestCreatePAT_MissingDescription(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("patmiss@test.com", "PATMiss", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamManager)) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	bodyBytes, _ := json.Marshal(map[string]interface{}{"description": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/pat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreatePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty description, got %d: %s", w.Code, w.Body.String())
	}
}
