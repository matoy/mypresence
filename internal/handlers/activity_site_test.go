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

func TestActivityPage_SiteFiltering(t *testing.T) {
	d := newExtraTestDB(t)

	// Create sites
	siteParisID, err := d.CreateSite(models.Site{Name: "Paris HQ", CountryCode: "FR", Seats: 100})
	if err != nil {
		t.Fatalf("CreateSite Paris: %v", err)
	}
	siteLyonID, err := d.CreateSite(models.Site{Name: "Lyon Office", CountryCode: "FR", Seats: 50})
	if err != nil {
		t.Fatalf("CreateSite Lyon: %v", err)
	}

	// Create status
	statusID, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})

	// Create users
	u1, _ := d.CreateLocalUser("paris@test.com", "Alice Paris", "password1")
	d.UpdateUserSite(u1, siteParisID) //nolint:errcheck

	u2, _ := d.CreateLocalUser("lyon@test.com", "Bob Lyon", "password1")
	d.UpdateUserSite(u2, siteLyonID) //nolint:errcheck

	u3, _ := d.CreateLocalUser("none@test.com", "Charlie Unassigned", "password1")
	// u3 has no site (site_id = 0)

	// Create team and add users
	teamID, _ := d.CreateTeam("Engineering")
	d.AddTeamMember(teamID, u1) //nolint:errcheck
	d.AddTeamMember(teamID, u2) //nolint:errcheck
	d.AddTeamMember(teamID, u3) //nolint:errcheck

	// Presences in Jan 2026
	d.SetPresences(u1, []string{"2026-01-05"}, statusID, "") // 1 billable day
	d.SetPresences(u2, []string{"2026-01-06"}, statusID, "") // 1 billable day
	d.SetPresences(u3, []string{"2026-01-07"}, statusID, "") // 1 billable day

	var captured map[string]interface{}
	h := &ActivityHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			captured = data.(map[string]interface{})
		},
	}

	// 1. No site filter: should return all 3 users
	{
		req := createAdminReq(t, d, http.MethodGet,
			"/admin/activity?year=2026&month=1&team="+strconvI64(teamID), nil)
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		stats := captured["Stats"].([]models.UserStats)
		if len(stats) != 3 {
			t.Fatalf("expected 3 users without site filter, got %d", len(stats))
		}
		if siteVal := captured["SelectedSite"].(string); siteVal != "" {
			t.Errorf("expected empty SelectedSite, got %q", siteVal)
		}
		if totalBillable := captured["TotalBillable"].(float64); totalBillable != 3.0 {
			t.Errorf("expected total billable 3.0, got %f", totalBillable)
		}
	}

	// 2. Filter by Paris site: should return only Alice
	{
		req := createAdminReq(t, d, http.MethodGet,
			"/admin/activity?year=2026&month=1&team="+strconvI64(teamID)+"&site="+strconvI64(siteParisID), nil)
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		stats := captured["Stats"].([]models.UserStats)
		if len(stats) != 1 {
			t.Fatalf("expected 1 user for Paris, got %d", len(stats))
		}
		if stats[0].User.ID != u1 {
			t.Errorf("expected user Alice (id=%d), got id=%d", u1, stats[0].User.ID)
		}
		if siteVal := captured["SelectedSite"].(string); siteVal != strconvI64(siteParisID) {
			t.Errorf("expected SelectedSite %d, got %q", siteParisID, siteVal)
		}
		if totalBillable := captured["TotalBillable"].(float64); totalBillable != 1.0 {
			t.Errorf("expected total billable 1.0, got %f", totalBillable)
		}
	}

	// 3. Filter by unassigned site: should return only Charlie
	{
		req := createAdminReq(t, d, http.MethodGet,
			"/admin/activity?year=2026&month=1&team="+strconvI64(teamID)+"&site=unassigned", nil)
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		stats := captured["Stats"].([]models.UserStats)
		if len(stats) != 1 {
			t.Fatalf("expected 1 user for unassigned, got %d", len(stats))
		}
		if stats[0].User.ID != u3 {
			t.Errorf("expected user Charlie (id=%d), got id=%d", u3, stats[0].User.ID)
		}
		if siteVal := captured["SelectedSite"].(string); siteVal != "unassigned" {
			t.Errorf("expected SelectedSite 'unassigned', got %q", siteVal)
		}
		if totalBillable := captured["TotalBillable"].(float64); totalBillable != 1.0 {
			t.Errorf("expected total billable 1.0, got %f", totalBillable)
		}
	}

	// 4. Filter by non-existent site: should return 0 users
	{
		req := createAdminReq(t, d, http.MethodGet,
			"/admin/activity?year=2026&month=1&team="+strconvI64(teamID)+"&site=99999", nil)
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		stats := captured["Stats"].([]models.UserStats)
		if len(stats) != 0 {
			t.Fatalf("expected 0 users for site 99999, got %d", len(stats))
		}
		if totalBillable := captured["TotalBillable"].(float64); totalBillable != 0.0 {
			t.Errorf("expected total billable 0.0, got %f", totalBillable)
		}
	}
}

func TestActivityAPI_SiteFiltering(t *testing.T) {
	d := newExtraTestDB(t)

	siteParisID, _ := d.CreateSite(models.Site{Name: "Paris HQ", CountryCode: "FR", Seats: 100})
	siteLyonID, _ := d.CreateSite(models.Site{Name: "Lyon Office", CountryCode: "FR", Seats: 50})

	u1, _ := d.CreateLocalUser("paris_api@test.com", "Alice Paris", "password1")
	d.UpdateUserSite(u1, siteParisID) //nolint:errcheck

	u2, _ := d.CreateLocalUser("lyon_api@test.com", "Bob Lyon", "password1")
	d.UpdateUserSite(u2, siteLyonID) //nolint:errcheck

	u3, _ := d.CreateLocalUser("none_api@test.com", "Charlie Unassigned", "password1")

	teamID, _ := d.CreateTeam("APITeam")
	d.AddTeamMember(teamID, u1) //nolint:errcheck
	d.AddTeamMember(teamID, u2) //nolint:errcheck
	d.AddTeamMember(teamID, u3) //nolint:errcheck

	h := &ActivityHandler{DB: d, Render: noRender}

	// 1. site=Paris
	{
		req := createAdminReq(t, d, http.MethodGet,
			"/api/admin/activity?team_id="+strconvI64(teamID)+"&year=2026&month=1&site="+strconvI64(siteParisID), nil)
		w := httptest.NewRecorder()
		w.Body = new(bytes.Buffer)
		middleware.Auth(d, http.HandlerFunc(h.ActivityAPI)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var stats []models.UserStats
		_ = json.Unmarshal(w.Body.Bytes(), &stats)
		if len(stats) != 1 || stats[0].User.ID != u1 {
			t.Fatalf("expected only Alice, got: %v", stats)
		}
	}

	// 2. site=unassigned
	{
		req := createAdminReq(t, d, http.MethodGet,
			"/api/admin/activity?team_id="+strconvI64(teamID)+"&year=2026&month=1&site=unassigned", nil)
		w := httptest.NewRecorder()
		w.Body = new(bytes.Buffer)
		middleware.Auth(d, http.HandlerFunc(h.ActivityAPI)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var stats []models.UserStats
		_ = json.Unmarshal(w.Body.Bytes(), &stats)
		if len(stats) != 1 || stats[0].User.ID != u3 {
			t.Fatalf("expected only Charlie, got: %v", stats)
		}
	}
}

func TestActivityPage_SiteFiltering_DomainAndExecSummary(t *testing.T) {
	d := newExtraTestDB(t)

	siteParisID, _ := d.CreateSite(models.Site{Name: "Paris HQ", CountryCode: "FR", Seats: 100})
	siteLyonID, _ := d.CreateSite(models.Site{Name: "Lyon Office", CountryCode: "FR", Seats: 50})

	// Create domain
	domainID, _ := d.CreateDomain("Digital Operations")

	// Create team in domain
	teamID, _ := d.CreateTeam("Web Team")
	_ = d.UpdateTeamDomain(teamID, domainID)

	statusID, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})

	// Users: Alice (Paris), Bob (Lyon), Charlie (none)
	u1, _ := d.CreateLocalUser("alice_dom@test.com", "Alice Dom", "password1")
	d.UpdateUserSite(u1, siteParisID) //nolint:errcheck

	u2, _ := d.CreateLocalUser("bob_dom@test.com", "Bob Dom", "password1")
	d.UpdateUserSite(u2, siteLyonID) //nolint:errcheck

	u3, _ := d.CreateLocalUser("charlie_dom@test.com", "Charlie Dom", "password1")

	d.AddTeamMember(teamID, u1) //nolint:errcheck
	d.AddTeamMember(teamID, u2) //nolint:errcheck
	d.AddTeamMember(teamID, u3) //nolint:errcheck

	d.SetPresences(u1, []string{"2026-01-05"}, statusID, "")
	d.SetPresences(u2, []string{"2026-01-06"}, statusID, "")
	d.SetPresences(u3, []string{"2026-01-07"}, statusID, "")

	var captured map[string]interface{}
	h := &ActivityHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			captured = data.(map[string]interface{})
		},
	}

	// 1. Domain view with site=Paris
	{
		req := createAdminReq(t, d, http.MethodGet,
			"/admin/activity?year=2026&month=1&domain="+strconvI64(domainID)+"&site="+strconvI64(siteParisID), nil)
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		stats := captured["Stats"].([]models.UserStats)
		if len(stats) != 1 || stats[0].User.ID != u1 {
			t.Fatalf("expected only Alice for Paris domain view, got: %v", stats)
		}
		if execCount := captured["ExecUserCount"].(int); execCount != 1 {
			t.Errorf("expected ExecUserCount 1 for Paris, got %d", execCount)
		}
	}

	// 2. Domain view with site=unassigned
	{
		req := createAdminReq(t, d, http.MethodGet,
			"/admin/activity?year=2026&month=1&domain="+strconvI64(domainID)+"&site=unassigned", nil)
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		stats := captured["Stats"].([]models.UserStats)
		if len(stats) != 1 || stats[0].User.ID != u3 {
			t.Fatalf("expected only Charlie for unassigned domain view, got: %v", stats)
		}
		if execCount := captured["ExecUserCount"].(int); execCount != 1 {
			t.Errorf("expected ExecUserCount 1 for unassigned, got %d", execCount)
		}
	}
}

func TestActivityPage_DomainTeamGroupingAndDeduplication(t *testing.T) {
	d := newExtraTestDB(t)

	// Create domain
	domainID, _ := d.CreateDomain("Product Division")

	// Create 3 teams in domain
	teamAlphaID, _ := d.CreateTeam("Team Alpha")
	_ = d.UpdateTeamDomain(teamAlphaID, domainID)

	teamBetaID, _ := d.CreateTeam("Team Beta")
	_ = d.UpdateTeamDomain(teamBetaID, domainID)

	teamEmptyID, _ := d.CreateTeam("Team Empty")
	_ = d.UpdateTeamDomain(teamEmptyID, domainID)

	statusID, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})

	// Users: Alice (Alpha), Bob (Alpha & Beta), Charlie (Beta)
	uAlice, _ := d.CreateLocalUser("alice@product.com", "Alice Alpha", "password1")
	uBob, _ := d.CreateLocalUser("bob@product.com", "Bob Multi", "password1")
	uCharlie, _ := d.CreateLocalUser("charlie@product.com", "Charlie Beta", "password1")

	d.AddTeamMember(teamAlphaID, uAlice)   //nolint:errcheck
	d.AddTeamMember(teamAlphaID, uBob)     //nolint:errcheck
	d.AddTeamMember(teamBetaID, uBob)      //nolint:errcheck (multi-team user!)
	d.AddTeamMember(teamBetaID, uCharlie)  //nolint:errcheck

	// Presences:
	// Alice: 5 days
	d.SetPresences(uAlice, []string{"2026-01-05", "2026-01-06", "2026-01-07", "2026-01-08", "2026-01-09"}, statusID, "")
	// Bob: 10 days
	d.SetPresences(uBob, []string{
		"2026-01-05", "2026-01-06", "2026-01-07", "2026-01-08", "2026-01-09",
		"2026-01-12", "2026-01-13", "2026-01-14", "2026-01-15", "2026-01-16",
	}, statusID, "")
	// Charlie: 7 days
	d.SetPresences(uCharlie, []string{
		"2026-01-05", "2026-01-06", "2026-01-07", "2026-01-08", "2026-01-09",
		"2026-01-12", "2026-01-13",
	}, statusID, "")

	var captured map[string]interface{}
	h := &ActivityHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			captured = data.(map[string]interface{})
		},
	}

	req := createAuthedReq(t, d, http.MethodGet,
		"/admin/activity?year=2026&month=1&domain="+strconvI64(domainID),
		"mgr@product.com", "Manager", "password1", models.RoleGlobal, nil)
	mgrUser, _ := d.GetUserByEmail("mgr@product.com")
	_ = d.SetDomainManagers(domainID, []int64{mgrUser.ID})

	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// 1. Daily breakdown table is hidden for domain view
	if showDaily := captured["ShowDailyBreakdown"].(bool); showDaily {
		t.Fatal("expected ShowDailyBreakdown to be false for domain view")
	}

	// 2. Domain team groups: 3 teams (Alpha, Beta, Empty)
	teamGroups := captured["DomainTeamGroups"].([]DomainTeamGroup)
	if len(teamGroups) != 3 {
		t.Fatalf("expected 3 domain team groups, got %d", len(teamGroups))
	}

	// Find groups by name
	groupByName := make(map[string]DomainTeamGroup)
	for _, tg := range teamGroups {
		groupByName[tg.Team.Name] = tg
	}

	// Team Alpha: Alice (5) + Bob (10) = 15 billable
	alphaGroup, ok := groupByName["Team Alpha"]
	if !ok {
		t.Fatal("missing Team Alpha group")
	}
	if len(alphaGroup.Stats) != 2 {
		t.Fatalf("expected 2 members in Team Alpha, got %d", len(alphaGroup.Stats))
	}
	if alphaGroup.TotalBillable != 15.0 {
		t.Fatalf("expected Team Alpha TotalBillable=15, got %f", alphaGroup.TotalBillable)
	}

	// Team Beta: Bob (10) + Charlie (7) = 17 billable
	betaGroup, ok := groupByName["Team Beta"]
	if !ok {
		t.Fatal("missing Team Beta group")
	}
	if len(betaGroup.Stats) != 2 {
		t.Fatalf("expected 2 members in Team Beta, got %d", len(betaGroup.Stats))
	}
	if betaGroup.TotalBillable != 17.0 {
		t.Fatalf("expected Team Beta TotalBillable=17, got %f", betaGroup.TotalBillable)
	}

	// Team Empty: 0 members, 0 billable
	emptyGroup, ok := groupByName["Team Empty"]
	if !ok {
		t.Fatal("missing Team Empty group")
	}
	if len(emptyGroup.Stats) != 0 {
		t.Fatalf("expected 0 members in Team Empty, got %d", len(emptyGroup.Stats))
	}
	if emptyGroup.TotalBillable != 0.0 {
		t.Fatalf("expected Team Empty TotalBillable=0, got %f", emptyGroup.TotalBillable)
	}

	// 3. Domain Total Deduplication:
	// Bob is in both Alpha and Beta, but in the domain total (Stats and TotalBillable),
	// he must be counted only ONCE.
	// Expected total = Alice (5) + Bob (10) + Charlie (7) = 22.0 (NOT 15 + 17 = 32.0).
	domainStats := captured["Stats"].([]models.UserStats)
	if len(domainStats) != 3 {
		t.Fatalf("expected 3 unique users in domain stats (deduplicated), got %d", len(domainStats))
	}

	domainTotalBillable := captured["TotalBillable"].(float64)
	if domainTotalBillable != 22.0 {
		t.Fatalf("expected deduplicated TotalBillable=22.0, got %f (double counting occurred!)", domainTotalBillable)
	}
}
