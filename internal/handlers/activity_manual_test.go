package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

func TestTeamHasManualTimesheets(t *testing.T) {
	teams := []models.Team{
		{ID: 1, Name: "Regular", TimesheetsManagedManually: false},
		{ID: 2, Name: "Manual", TimesheetsManagedManually: true},
	}
	if teamHasManualTimesheets(teams, 1) {
		t.Error("team 1 should not be manual")
	}
	if !teamHasManualTimesheets(teams, 2) {
		t.Error("team 2 should be manual")
	}
	if teamHasManualTimesheets(teams, 999) {
		t.Error("unknown team should not be manual")
	}
}

func TestComputeManualProjectActivity_FullyCompleteDayCountsFullWeight(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ActivityHandler{DB: d}

	uid, _ := d.CreateLocalUser("manualact1@test.com", "ManualAct1", "password1")
	statusID, _ := d.CreateStatus(models.Status{Name: "Billable", Color: "#22c55e", Billable: true, SortOrder: 1})
	d.SetPresences(uid, []string{"2026-05-04"}, statusID, "full")                         //nolint:errcheck
	d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 100) //nolint:errcheck

	stats := []models.UserStats{{User: models.User{ID: uid}, BillableDays: 1.0}}
	byUser, total := h.computeManualProjectActivity(stats, 2026, 5)
	if byUser[uid] != 100.0 {
		t.Errorf("expected 100%% activity, got %v", byUser[uid])
	}
	if total != 1.0 {
		t.Errorf("expected total declared 1.0, got %v", total)
	}
}

func TestComputeManualProjectActivity_IncompleteDayNotCounted(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ActivityHandler{DB: d}

	uid, _ := d.CreateLocalUser("manualact2@test.com", "ManualAct2", "password1")
	statusID, _ := d.CreateStatus(models.Status{Name: "Billable", Color: "#22c55e", Billable: true, SortOrder: 1})
	d.SetPresences(uid, []string{"2026-05-04"}, statusID, "full")                        //nolint:errcheck
	d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 40) //nolint:errcheck

	stats := []models.UserStats{{User: models.User{ID: uid}, BillableDays: 1.0}}
	byUser, total := h.computeManualProjectActivity(stats, 2026, 5)
	if byUser[uid] != 0.0 {
		t.Errorf("expected 0%% activity for incomplete day, got %v", byUser[uid])
	}
	if total != 0.0 {
		t.Errorf("expected total declared 0, got %v", total)
	}
}

func TestComputeManualProjectActivity_HalfDayCompletesAt50Percent(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ActivityHandler{DB: d}

	uid, _ := d.CreateLocalUser("manualact3@test.com", "ManualAct3", "password1")
	statusID, _ := d.CreateStatus(models.Status{Name: "Billable", Color: "#22c55e", Billable: true, SortOrder: 1})
	d.SetPresences(uid, []string{"2026-05-04"}, statusID, "AM")                          //nolint:errcheck
	d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 50) //nolint:errcheck

	stats := []models.UserStats{{User: models.User{ID: uid}, BillableDays: 0.5}}
	byUser, total := h.computeManualProjectActivity(stats, 2026, 5)
	if byUser[uid] != 100.0 {
		t.Errorf("expected 100%% activity (0.5/0.5), got %v", byUser[uid])
	}
	if total != 0.5 {
		t.Errorf("expected total declared 0.5, got %v", total)
	}
}

// TestActivityPage_ManualTeam_UsesManualFormula verifies that when the
// selected team has "Timesheets managed manually" enabled, the Team Summary's
// project-activity column uses the completed-days formula instead of the
// declared project-time-entry days.
func TestActivityPage_ManualTeam_UsesManualFormula(t *testing.T) {
	d := newExtraTestDB(t)

	statusID, _ := d.CreateStatus(models.Status{Name: "Billable", Color: "#22c55e", Billable: true, SortOrder: 1})
	memberID, _ := d.CreateLocalUser("manualteammember@test.com", "ManualTeamMember", "password1")
	teamID, _ := d.CreateTeamWithDetails("Manual Activity Team", "MAT", true)
	d.AddTeamMember(teamID, memberID) //nolint:errcheck

	// Two billable days: one fully declared via activities, one not declared at all.
	d.SetPresences(memberID, []string{"2026-05-04"}, statusID, "full")                         //nolint:errcheck
	d.SetPresences(memberID, []string{"2026-05-05"}, statusID, "full")                         //nolint:errcheck
	d.CreateProjectActivity(memberID, "2026-05-04", models.ActivityTypeOther, "", "", "", 100) //nolint:errcheck

	// Also declare classic project time entries — these must be ignored for a manual team.
	projectID, _ := d.CreateProject("ClassicProject", "CLS", teamID, true, "2026-01-01", "2026-12-31")
	d.SetProjectTimeEntry(memberID, projectID, 2026, 5, 5.0) //nolint:errcheck

	var rendered map[string]interface{}
	h := &ActivityHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = data.(map[string]interface{})
	}}

	req := createAdminReq(t, d, http.MethodGet, "/admin/activity?year=2026&month=5&team="+strconvI64(teamID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	byUser := rendered["ProjectActivityByUser"].(map[int64]float64)
	// 1 complete day out of 2 billable days = 50%, NOT the 5-day project-time-entry-based value.
	if got := byUser[memberID]; got != 50.0 {
		t.Errorf("expected 50%% project activity for manual team, got %v", got)
	}
}
