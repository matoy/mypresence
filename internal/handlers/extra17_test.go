package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// ActivityPage — totalNotSet < 0 clamped to 0 (activity.go:64)
// -----------------------------------------------------------------------

// TestActivityPage_TotalNotSetNegativeClamped sets up more presence days than
// working days so totalNotSet = totalWorkingDays - totalSetDays < 0, covering
// the `if totalNotSet < 0 { totalNotSet = 0 }` branch.
func TestActivityPage_TotalNotSetNegativeClamped(t *testing.T) {
	d := newExtraTestDB(t)

	// Create on-site status.
	statusID, _ := d.CreateStatus(models.Status{
		Name: "OnSite17", Color: "#111111", Billable: true, OnSite: true, SortOrder: 9,
	})

	// Create team member (not the admin).
	memberID, _ := d.CreateLocalUser("totalnotset_member@test.com", "NotSetMember", "password1")
	teamID, _ := d.CreateTeam("NotSetTeam")
	d.AddTeamMember(teamID, memberID) //nolint:errcheck

	// Set presences for ALL 30 days of June 2026 (weekdays + weekends).
	// June 2026 has 22 working days; setting 30 days makes totalSetDays > totalWorkingDays.
	var allJune []string
	for day := 1; day <= 30; day++ {
		allJune = append(allJune, "2026-06-"+twoDigit(day))
	}
	d.SetPresences(memberID, allJune, statusID, "") //nolint:errcheck

	h := &ActivityHandler{DB: d, Render: noRender, DisableProjects: true}

	req := createAdminReq(t, d, http.MethodGet,
		"/admin/activity?year=2026&month=6&team="+strconvI64(teamID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + strconvI64(int64(n))
	}
	return strconvI64(int64(n))
}

// -----------------------------------------------------------------------
// CalendarPage — team has 0 members at old startDate → continue (calendar.go:98)
// -----------------------------------------------------------------------

// TestCalendarPage_TeamNoMembersAtOldDate uses year=2020&month=1 so that
// GetTeamMembersAt(teamID, "2020-01-01") returns [] (the user joined in 2026),
// triggering the `if len(members) == 0 { continue }` branch.
func TestCalendarPage_TeamNoMembersAtOldDate(t *testing.T) {
	d := newExtraTestDB(t)

	// Create a user who will be authenticated and is in a team.
	memberID, _ := d.CreateLocalUser("calpage_olddate@test.com", "OldDateMember", "password1")
	d.UpdateUserRoles(memberID, string(models.RoleGlobal)) //nolint:errcheck
	teamID, _ := d.CreateTeam("OldDateTeam")
	d.AddTeamMember(teamID, memberID) //nolint:errcheck

	tok, _ := d.CreateSession(memberID)

	h := &CalendarHandler{DB: d, Render: noRender, DisableFloorplans: true}

	// Request year=2020 (joined_at = 2026) → GetTeamMembersAt returns [] → continue.
	req := httptest.NewRequest(http.MethodGet, "/calendar?year=2020&month=1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// GetPresencesAPI — DB error → 500 (calendar.go:297)
// -----------------------------------------------------------------------

// TestGetPresencesAPI_DBError closes the DB inside the auth handler so
// GetTeamMembersAt fails, covering the jsonError at line 297.
func TestGetPresencesAPI_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	teamID, _ := d.CreateTeam("PresencesAPIErrTeam")
	h := &CalendarHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet,
		"/api/presences?team_id="+strconvI64(teamID)+"&year=2026&month=6", nil)

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

// -----------------------------------------------------------------------
// ListSeatsWithStatusForDatesAPI — DB error → 500 (floorplan.go:505)
// -----------------------------------------------------------------------

// TestListSeatsWithStatusForDatesAPI_DBError closes the DB inside the auth handler
// so GetSeatsWithStatusForDates fails, covering the jsonError at line 505.
func TestListSeatsWithStatusForDatesAPI_DBError2(t *testing.T) {
	d := newExtraTestDB(t)
	fpID, _ := d.CreateFloorplan("APIErrFP", 0)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodGet,
		"/api/floorplans/"+strconvI64(fpID)+"/seats-status?dates=2026-06-01", nil)
	req.SetPathValue("id", strconvI64(fpID))

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.ListSeatsWithStatusForDatesAPI(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}
