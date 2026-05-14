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
// CalendarPage — error and DisableFloorplans branch
// -----------------------------------------------------------------------

func TestCalendarPage_GetPresencesError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("calpagedberr@test.com", "CalPageErr", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/calendar", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.Close() // Close DB so GetPresences fails
		h.CalendarPage(w, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on presence DB error, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCalendarPage_DisableFloorplans(t *testing.T) {
	d := newExtraTestDB(t)
	var renderCalled bool
	h := &CalendarHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		renderCalled = true
	}, DisableFloorplans: true}
	req := createAdminReq(t, d, http.MethodGet, "/calendar", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)
	if !renderCalled {
		t.Error("expected render to be called")
	}
}

func TestCalendarPage_WithTeam_DisableFloorplans(t *testing.T) {
	d := newExtraTestDB(t)
	memberID, _ := d.CreateLocalUser("calmem@test.com", "CalMem", "password1")
	teamID, _ := d.CreateTeam("CalendarTeam")
	d.AddTeamMember(teamID, memberID) //nolint:errcheck

	adminID := seedUserInHandlers(t, d, "caladmin@test.com")
	d.UpdateUserRoles(adminID, string(models.RoleGlobal)) //nolint:errcheck
	d.AddTeamMember(teamID, adminID)                      //nolint:errcheck

	var renderCalled bool
	h := &CalendarHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		renderCalled = true
	}, DisableFloorplans: true}

	tok, _ := d.CreateSession(adminID)
	req := httptest.NewRequest(http.MethodGet, "/calendar", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)
	if !renderCalled {
		t.Error("expected render to be called with team view and DisableFloorplans=true")
	}
}

// -----------------------------------------------------------------------
// GetPresencesAPI — error paths
// -----------------------------------------------------------------------

func TestGetPresencesAPI_GetTeamMembersError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("APIErrTeam1")
	uid, _ := d.CreateLocalUser("presapi_err1@test.com", "PresAPIErr1", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet,
		"/api/presences?team_id="+strconvI64(teamID)+"&year=2026&month=5", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.Close() // Close DB so GetTeamMembersAt fails
		h.GetPresencesAPI(w, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetPresencesAPI_GetPresencesError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	memberID, _ := d.CreateLocalUser("presapierr2@test.com", "PresAPIErr2", "password1")
	teamID, _ := d.CreateTeam("APIErrTeam2")
	d.AddTeamMember(teamID, memberID) //nolint:errcheck
	uid, _ := d.CreateLocalUser("presapi_caller2@test.com", "PresAPICaller2", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet,
		"/api/presences?team_id="+strconvI64(teamID)+"&year=2026&month=5", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.Close()
		h.GetPresencesAPI(w, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// isTeamLeaderOf — missing branches
// -----------------------------------------------------------------------

// Leader in team A, target in team B → returns false (no intersection)
func TestIsTeamLeaderOf_NoMatchingTeam(t *testing.T) {
	d := newExtraTestDB(t)

	leaderID := seedUserInHandlers(t, d, "leader_nomatch@test.com")
	targetID := seedUserInHandlers(t, d, "target_nomatch@test.com")

	teamA, _ := d.CreateTeam("TeamA_nomatch")
	teamB, _ := d.CreateTeam("TeamB_nomatch")
	d.AddTeamMember(teamA, leaderID) //nolint:errcheck
	d.AddTeamMember(teamB, targetID) //nolint:errcheck

	h := &CalendarHandler{DB: d, Render: noRender}

	// Build a SetPresences request as leaderID (team leader) editing targetID
	d.UpdateUserRoles(leaderID, string(models.RoleTeamLeader)) //nolint:errcheck
	tok, _ := d.CreateSession(leaderID)

	statusID, _ := d.CreateStatus(models.Status{Name: "ILTF", Color: "#abc", OnSite: true})
	d.SetPresences(targetID, []string{"2026-06-01"}, statusID, "") //nolint:errcheck

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":   targetID,
		"dates":     []string{"2026-06-02"},
		"status_id": statusID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/presences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("leader not in target's team should get 403, got %d: %s", w.Code, w.Body.String())
	}
}

// Leader with no teams → GetUserTeams returns empty → isTeamLeaderOf returns false immediately
func TestIsTeamLeaderOf_LeaderNoTeams(t *testing.T) {
	d := newExtraTestDB(t)

	leaderID := seedUserInHandlers(t, d, "leader_noteams@test.com")
	targetID := seedUserInHandlers(t, d, "target_noteams@test.com")

	d.UpdateUserRoles(leaderID, string(models.RoleTeamLeader)) //nolint:errcheck
	tok, _ := d.CreateSession(leaderID)

	body, _ := json.Marshal(map[string]interface{}{
		"user_id": targetID,
		"dates":   []string{"2026-06-01"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/presences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h := &CalendarHandler{DB: d, Render: noRender}
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("leader with no teams should get 403, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// SetTeamMemberLeftAt — DB error → 500
// -----------------------------------------------------------------------

func TestSetTeamMemberLeftAt_DBError_Returns500(t *testing.T) {
	d := newExtraTestDB(t)
	teamID, _ := d.CreateTeam("LeftAtErrTeam")
	targetID := seedUserInHandlers(t, d, "leftat_err@test.com")
	d.AddTeamMember(teamID, targetID) //nolint:errcheck

	adminUID, _ := d.CreateLocalUser("leftat_admin_err@test.com", "Admin", "password1")
	d.UpdateUserRoles(adminUID, string(models.RoleGlobal)) //nolint:errcheck
	tok, _ := d.CreateSession(adminUID)

	body, _ := json.Marshal(map[string]interface{}{"left_at": "2026-06-30"})
	req := httptest.NewRequest(http.MethodPatch,
		"/admin/teams/"+strconvI64(teamID)+"/members/"+strconvI64(targetID)+"/left-at",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", strconvI64(teamID))
	req.SetPathValue("userId", strconvI64(targetID))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	h := &AdminHandler{DB: d, Render: noRender}
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.Close() // Close DB after auth so SetTeamMemberLeftAt fails
		h.SetTeamMemberLeftAt(w, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ListSeatsWithStatusForDatesAPI — DB error → 500
// -----------------------------------------------------------------------

func TestListSeatsWithStatusForDatesAPI_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	fpID, _ := d.CreateFloorplan("StatusDBErrFP", 0)
	uid, _ := d.CreateLocalUser("seatsdbErr@test.com", "SeatsErr", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet,
		"/api/floorplans/"+strconvI64(fpID)+"/seats/status?dates=2026-05-01", nil)
	req.SetPathValue("id", strconvI64(fpID))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.Close() // Close DB so GetSeatsWithStatusForDates fails
		h.ListSeatsWithStatusForDatesAPI(w, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// computeWorkingDays — invalid holiday date (continue branch)
// -----------------------------------------------------------------------

func TestComputeWorkingDays_InvalidHolidayDate(t *testing.T) {
	holidays := []models.Holiday{
		{Date: "invalid-date", Name: "Bad Holiday", AllowImputed: false},
		{Date: "2026-05-01", Name: "Labour Day", AllowImputed: false},
	}
	working, hCount := computeWorkingDays(2026, 5, holidays)
	if working == 0 {
		t.Error("expected non-zero working days")
	}
	// Only 2026-05-01 (Friday) is a valid non-imputable holiday
	if hCount != 1 {
		t.Errorf("expected 1 valid holiday, got %d", hCount)
	}
}

// -----------------------------------------------------------------------
// computeProjectActivity — DB error path (continue branch)
// -----------------------------------------------------------------------

func TestComputeProjectActivity_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ActivityHandler{DB: d, DisableProjects: false}

	uid := seedUserInHandlers(t, d, "projact_err@test.com")
	stats := []models.UserStats{
		{User: models.User{ID: uid}, BillableDays: 5},
	}

	// Close DB so GetUserTotalDeclaredForMonth fails → continue branch
	d.Close()

	result, total := h.computeProjectActivity(stats, 2026, 5)
	if len(result) != 0 {
		t.Errorf("expected empty result on error, got %v", result)
	}
	if total != 0 {
		t.Errorf("expected zero total on error, got %v", total)
	}
}
