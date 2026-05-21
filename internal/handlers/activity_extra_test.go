package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// TestActivityPage_WithTeamData tests ActivityPage with a team that has members,
// presences and project data — covering the teamID > 0 branches.
func TestActivityPage_WithTeamData(t *testing.T) {
	d := newExtraTestDB(t)

	// Create a billable+onsite status
	statusID, _ := d.CreateStatus(models.Status{Name: "Télétravail", Color: "#ff0000", Billable: true, OnSite: true, SortOrder: 1})

	// Create a team member
	memberID, _ := d.CreateLocalUser("actmember@test.com", "ActMember", "password1")

	// Create a team and add the member
	teamID, _ := d.CreateTeam("ActivityTeam")
	d.AddTeamMember(teamID, memberID) //nolint:errcheck

	// Set presences for the member on a specific date
	d.SetPresences(memberID, []string{"2026-01-05"}, statusID, "")   //nolint:errcheck
	d.SetPresences(memberID, []string{"2026-01-06"}, statusID, "AM") //nolint:errcheck
	d.SetPresences(memberID, []string{"2026-01-06"}, statusID, "PM") //nolint:errcheck

	// Create a project and add a time entry for the member
	projectID, _ := d.CreateProject("ActivityProject", "ACT", teamID, true, "2026-01-01", "2026-12-31")
	d.SetProjectTimeEntry(memberID, projectID, 2026, 1, 5.0) //nolint:errcheck

	// Create a holiday in January that is NOT allow-imputed
	d.CreateHoliday("2026-01-01", "NewYear", false) //nolint:errcheck
	// Create another holiday on a weekday with AllowImputed = true (won't increase holidayCount)
	d.CreateHoliday("2026-01-02", "NewYear2", true) //nolint:errcheck

	h := &ActivityHandler{DB: d, Render: noRender, DisableProjects: false}

	req := createAdminReq(t, d, http.MethodGet,
		"/admin/activity?year=2026&month=1&team="+strconvI64(teamID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestActivityPage_WithTeamDataTotalNotSetNeg covers totalNotSet < 0 clamping.
func TestActivityPage_WithTeamDataTotalNotSetNeg(t *testing.T) {
	d := newExtraTestDB(t)

	// Create a billable+onsite status
	statusID, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#00ff00", Billable: true, OnSite: true, SortOrder: 1})

	// Create team members to ensure lots of presences set
	var memberIDs []int64
	for i := 0; i < 3; i++ {
		uid, _ := d.CreateLocalUser("actmember2"+strconvI64(int64(i))+"@test.com",
			"Member2"+strconvI64(int64(i)), "password1")
		memberIDs = append(memberIDs, uid)
	}

	teamID, _ := d.CreateTeam("ActivityTeam2")
	for _, mid := range memberIDs {
		d.AddTeamMember(teamID, mid) //nolint:errcheck
	}

	// Fill every working day of January 2026 for all members
	workdays := []string{
		"2026-01-02", "2026-01-05", "2026-01-06", "2026-01-07", "2026-01-08", "2026-01-09",
		"2026-01-12", "2026-01-13", "2026-01-14", "2026-01-15", "2026-01-16",
		"2026-01-19", "2026-01-20", "2026-01-21", "2026-01-22", "2026-01-23",
		"2026-01-26", "2026-01-27", "2026-01-28", "2026-01-29", "2026-01-30",
	}
	for _, mid := range memberIDs {
		d.SetPresences(mid, workdays, statusID, "") //nolint:errcheck
	}

	h := &ActivityHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet,
		"/admin/activity?year=2026&month=1&team="+strconvI64(teamID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestActivityAPI_TeamLeaderOwnTeam tests ActivityAPI for team-leader on own team.
func TestActivityAPI_TeamLeaderOwnTeam(t *testing.T) {
	d := newExtraTestDB(t)

	uid, _ := d.CreateLocalUser("tlapi@test.com", "TLApi", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck
	teamID, _ := d.CreateTeam("TLApiTeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	h := &ActivityHandler{DB: d, Render: noRender}

	req := httptest.NewRequest(http.MethodGet,
		"/api/activity?team_id="+strconvI64(teamID)+"&year=2026&month=1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestActivityAPI_TeamLeaderForbiddenTeam tests ActivityAPI for team-leader on foreign team.
func TestActivityAPI_TeamLeaderForbiddenTeam(t *testing.T) {
	d := newExtraTestDB(t)

	uid, _ := d.CreateLocalUser("tlapiforbid@test.com", "TLApiForbid", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck
	teamID, _ := d.CreateTeam("ForeignTeam")
	tok, _ := d.CreateSession(uid)

	h := &ActivityHandler{DB: d, Render: noRender}

	req := httptest.NewRequest(http.MethodGet,
		"/api/activity?team_id="+strconvI64(teamID)+"&year=2026&month=1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestActivityAPI_MissingParams tests ActivityAPI when params are missing.
func TestActivityAPI_MissingParams(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ActivityHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/api/activity", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestActivityPage_ActivityViewerGetsExecSummary verifies that the ActivityPage
// handler passes ShowExecSummary=true when the authenticated user has the
// activity_viewer role.
func TestActivityPage_ActivityViewerGetsExecSummary(t *testing.T) {
	d := newExtraTestDB(t)

	// Create a second team and a member so exec summary aggregates across teams.
	statusID, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#00ff00", Billable: true, OnSite: true, SortOrder: 1})
	memberID, _ := d.CreateLocalUser("execmember@test.com", "ExecMember", "password1")
	teamID, _ := d.CreateTeam("ExecTeam")
	d.AddTeamMember(teamID, memberID)                              //nolint:errcheck
	d.SetPresences(memberID, []string{"2026-03-02"}, statusID, "") //nolint:errcheck

	var showExecSummaryGot interface{}
	captureRender := func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		if m, ok := data.(map[string]interface{}); ok {
			showExecSummaryGot = m["ShowExecSummary"]
		}
	}

	h := &ActivityHandler{DB: d, Render: captureRender, DisableProjects: false}

	req := createAuthedReq(t, d, http.MethodGet,
		"/admin/activity?year=2026&month=3&team="+strconvI64(teamID),
		"viewer@exec.com", "ExecViewer", "password1",
		string(models.RoleActivityViewer), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if showExecSummaryGot != true {
		t.Fatalf("ShowExecSummary should be true for activity_viewer, got %v", showExecSummaryGot)
	}
}
