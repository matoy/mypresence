package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// TestProjectsReportPage_ActivitiesView_DomainAggregation verifies that a
// domain manager sees a domain-grouped team selector and that selecting the
// domain merges activities across all its manual-timesheet teams.
func TestProjectsReportPage_ActivitiesView_DomainAggregation(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	teamA, _ := d.CreateTeamWithDetails("Domain Team A", "", true, false)
	teamB, _ := d.CreateTeamWithDetails("Domain Team B", "", true, false)
	memberA, _ := d.CreateLocalUser("domactA@test.com", "Dom Act A", "password1")
	memberB, _ := d.CreateLocalUser("domactB@test.com", "Dom Act B", "password1")
	d.AddTeamMember(teamA, memberA)                                                          //nolint:errcheck
	d.AddTeamMember(teamB, memberB)                                                          //nolint:errcheck
	d.CreateProjectActivity(memberA, "2026-05-04", models.ActivityTypeOther, "", "", "", 40) //nolint:errcheck
	d.CreateProjectActivity(memberB, "2026-05-05", models.ActivityTypeOther, "", "", "", 60) //nolint:errcheck

	domainID, err := d.CreateDomain("Reporting Domain")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	d.UpdateTeamDomain(teamA, domainID) //nolint:errcheck
	d.UpdateTeamDomain(teamB, domainID) //nolint:errcheck

	dmID, _ := d.CreateLocalUser("reportdm@test.com", "Report DM", "password1")
	if err := d.SetDomainManagers(domainID, []int64{dmID}); err != nil {
		t.Fatalf("SetDomainManagers: %v", err)
	}
	tok, _ := d.CreateSession(dmID)

	var captured map[string]interface{}
	h.Render = func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		captured = data.(map[string]interface{})
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/projects-report?view=activities&domain="+strconvI64(domainID)+"&year=2026&month=5", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if isDM, _ := captured["IsDomainManager"].(bool); !isDM {
		t.Errorf("expected IsDomainManager=true, got %v", captured["IsDomainManager"])
	}
	activities, _ := captured["Activities"].([]models.ProjectActivity)
	if len(activities) != 2 {
		t.Fatalf("expected activities merged across both domain teams, got %d: %+v", len(activities), activities)
	}
}
