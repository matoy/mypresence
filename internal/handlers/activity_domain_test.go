package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// TestActivityPage_DomainManagerSeesDomainGroupedTeams verifies that a user who
// only manages a domain (no team_leader/activity_viewer/global role) gets a
// domain-grouped team list restricted to their domain's teams, and that
// selecting the domain aggregates stats across all its teams.
func TestActivityPage_DomainManagerSeesDomainGroupedTeams(t *testing.T) {
	d := newExtraTestDB(t)

	statusID, _ := d.CreateStatus(models.Status{Name: "On site", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})

	member1, _ := d.CreateLocalUser("member1@test.com", "Member One", "password1")
	member2, _ := d.CreateLocalUser("member2@test.com", "Member Two", "password1")

	teamA, _ := d.CreateTeam("Team A")
	teamB, _ := d.CreateTeam("Team B")
	otherTeam, _ := d.CreateTeam("Other Team")
	d.AddTeamMember(teamA, member1) //nolint:errcheck
	d.AddTeamMember(teamB, member2) //nolint:errcheck

	d.SetPresences(member1, []string{"2026-03-05"}, statusID, "") //nolint:errcheck
	d.SetPresences(member2, []string{"2026-03-06"}, statusID, "") //nolint:errcheck

	domainID, err := d.CreateDomain("Engineering")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if err := d.UpdateTeamDomain(teamA, domainID); err != nil {
		t.Fatalf("UpdateTeamDomain teamA: %v", err)
	}
	if err := d.UpdateTeamDomain(teamB, domainID); err != nil {
		t.Fatalf("UpdateTeamDomain teamB: %v", err)
	}
	_ = otherTeam

	dmID, err := d.CreateLocalUser("domainmgr@test.com", "Domain Mgr", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	if err := d.SetDomainManagers(domainID, []int64{dmID}); err != nil {
		t.Fatalf("SetDomainManagers: %v", err)
	}
	tok, err := d.CreateSession(dmID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var captured map[string]interface{}
	h := &ActivityHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		captured = data.(map[string]interface{})
	}, DisableProjects: true}

	req := httptest.NewRequest(http.MethodGet, "/admin/activity?year=2026&month=3&domain="+strconvI64(domainID), nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if captured == nil {
		t.Fatal("render was not called")
	}
	if isDM, _ := captured["IsDomainManager"].(bool); !isDM {
		t.Errorf("expected IsDomainManager=true, got %v", captured["IsDomainManager"])
	}
	teams, _ := captured["Teams"].([]models.Team)
	for _, tm := range teams {
		if tm.ID == otherTeam {
			t.Errorf("domain manager should not see teams outside their domain, got %+v", teams)
		}
	}
	stats, _ := captured["Stats"].([]models.UserStats)
	if len(stats) != 2 {
		t.Fatalf("expected aggregated stats for both domain members, got %d entries: %+v", len(stats), stats)
	}
	if show, _ := captured["ShowDailyBreakdown"].(bool); show {
		t.Error("daily breakdown should be hidden when a domain is selected")
	}
}

// TestActivityPage_DomainManagerCannotSelectForeignDomain ensures a domain
// query param the user doesn't manage is ignored (falls back to team mode).
func TestActivityPage_DomainManagerCannotSelectForeignDomain(t *testing.T) {
	d := newExtraTestDB(t)

	teamA, _ := d.CreateTeam("Team A")
	otherDomainID, _ := d.CreateDomain("Other Domain")
	d.UpdateTeamDomain(teamA, otherDomainID) //nolint:errcheck

	// dmID manages no domain at all, and has no elevated role.
	dmID, _ := d.CreateLocalUser("plain@test.com", "Plain User", "password1")
	tok, _ := d.CreateSession(dmID)

	var captured map[string]interface{}
	h := &ActivityHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		captured = data.(map[string]interface{})
	}, DisableProjects: true}

	req := httptest.NewRequest(http.MethodGet, "/admin/activity?year=2026&month=3&domain="+strconvI64(otherDomainID), nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if selDomain, _ := captured["SelectedDomainID"].(int64); selDomain != 0 {
		t.Errorf("expected SelectedDomainID=0 for a domain the user doesn't manage, got %v", selDomain)
	}
}

// TestActivityPage_TeamLeaderDomainManagerCanSelectAnyDomainTeam reproduces the
// reported bug: a domain manager who also has the team_leader role could only
// browse the team(s) they personally lead — selecting any other team of a
// domain they manage silently fell back to one of their own led teams.
func TestActivityPage_TeamLeaderDomainManagerCanSelectAnyDomainTeam(t *testing.T) {
	d := newExtraTestDB(t)

	statusID, _ := d.CreateStatus(models.Status{Name: "On site", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})

	leaderMember, _ := d.CreateLocalUser("leader@test.com", "Leader", "password1")
	otherMember, _ := d.CreateLocalUser("othermember@test.com", "Other Member", "password1")

	leaderTeam, _ := d.CreateTeam("Leader Team")                      // the team the user personally leads
	otherDomainTeam, _ := d.CreateTeam("Other Team")                  // another team in the same domain, not led by the user
	d.AddTeamMember(leaderTeam, leaderMember)                         //nolint:errcheck
	d.AddTeamMember(otherDomainTeam, otherMember)                     //nolint:errcheck
	d.SetPresences(otherMember, []string{"2026-03-05"}, statusID, "") //nolint:errcheck

	domainID, err := d.CreateDomain("Shared Domain")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	d.UpdateTeamDomain(leaderTeam, domainID)      //nolint:errcheck
	d.UpdateTeamDomain(otherDomainTeam, domainID) //nolint:errcheck

	// leaderMember is both leader of leaderTeam AND a manager of the domain.
	if err := d.SetTeamLeaders(leaderTeam, []int64{leaderMember}); err != nil {
		t.Fatalf("SetTeamLeaders: %v", err)
	}
	d.AddTeamMember(leaderTeam, leaderMember) //nolint:errcheck
	if err := d.SetDomainManagers(domainID, []int64{leaderMember}); err != nil {
		t.Fatalf("SetDomainManagers: %v", err)
	}
	tok, err := d.CreateSession(leaderMember)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var captured map[string]interface{}
	h := &ActivityHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		captured = data.(map[string]interface{})
	}, DisableProjects: true}

	// Select the OTHER domain team, which the user does not personally lead.
	req := httptest.NewRequest(http.MethodGet, "/admin/activity?year=2026&month=3&team="+strconvI64(otherDomainTeam), nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if selTeam, _ := captured["SelectedTeamID"].(int64); selTeam != otherDomainTeam {
		t.Fatalf("expected the requested team %d to remain selected, got %v", otherDomainTeam, captured["SelectedTeamID"])
	}
	if selDomain, _ := captured["SelectedDomainID"].(int64); selDomain != 0 {
		t.Errorf("expected no domain selected when a team is explicitly requested, got %v", selDomain)
	}

	// The API endpoint used for drilldowns must allow it too.
	apiReq := httptest.NewRequest(http.MethodGet, "/api/activity?team_id="+strconvI64(otherDomainTeam)+"&year=2026&month=3", nil)
	apiReq.AddCookie(&http.Cookie{Name: "session", Value: tok})
	apiW := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ActivityAPI)).ServeHTTP(apiW, apiReq)
	if apiW.Code != http.StatusOK {
		t.Fatalf("expected 200 from ActivityAPI, got %d: %s", apiW.Code, apiW.Body.String())
	}
}
