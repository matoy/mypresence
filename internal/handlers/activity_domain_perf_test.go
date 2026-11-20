package handlers

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

func almostEqualFloat(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

// TestActivityPage_DomainOptimization_ParityAndEdgeCases thoroughly verifies that the
// batched SQL + in-memory calendar optimization computes exact numbers across multi-team
// domains (shared users, multi-country holidays, half-days, YTD, site filters, and exec summary).
func TestActivityPage_DomainOptimization_ParityAndEdgeCases(t *testing.T) {
	d := newExtraTestDB(t)

	siteFR, _ := d.CreateSite(models.Site{Name: "Paris HQ", CountryCode: "FR", Seats: 100})
	siteMA, _ := d.CreateSite(models.Site{Name: "Casablanca", CountryCode: "MA", Seats: 50})

	// May 2026: 21 weekdays total.
	// - 2026-05-01 (Fri): Global non-imputable -> deducts 1 day for FR, MA, and unassigned
	// - 2026-05-08 (Fri): FR-only non-imputable -> deducts 1 extra day ONLY for FR
	// - 2026-05-14 (Thu): FR imputable (allow_imputed=true) -> deducts 0 days
	_, _ = d.CreateHoliday("2026-05-01", "Labour Day", false, "")
	_, _ = d.CreateHoliday("2026-05-08", "Victory Day", false, "FR")
	_, _ = d.CreateHoliday("2026-05-14", "Ascension", true, "FR")

	stOffice, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})
	stRemote, _ := d.CreateStatus(models.Status{Name: "Remote", Color: "#3b82f6", Billable: true, OnSite: false, SortOrder: 2})
	stLeave, _ := d.CreateStatus(models.Status{Name: "Leave", Color: "#f59e0b", Billable: false, OnSite: false, SortOrder: 3})

	domainID, _ := d.CreateDomain("Engineering")
	teamA, _ := d.CreateTeam("Backend")
	teamB, _ := d.CreateTeam("Frontend")
	teamEmpty, _ := d.CreateTeam("Empty")
	teamExternal, _ := d.CreateTeam("Sales")
	_ = d.UpdateTeamDomain(teamA, domainID)
	_ = d.UpdateTeamDomain(teamB, domainID)
	_ = d.UpdateTeamDomain(teamEmpty, domainID)

	uAlice, _ := d.CreateLocalUser("alice_opt@test.com", "Alice Opt", "pass1") // FR, in Backend & Frontend
	uBob, _ := d.CreateLocalUser("bob_opt@test.com", "Bob Opt", "pass1")       // MA, in Backend
	uChlo, _ := d.CreateLocalUser("chlo_opt@test.com", "Chloe Opt", "pass1")   // No site, in Frontend
	uGone, _ := d.CreateLocalUser("gone_opt@test.com", "Gone Opt", "pass1")    // Departed before May
	uExt, _ := d.CreateLocalUser("ext_opt@test.com", "Ext Opt", "pass1")       // FR, in Sales (outside domain)

	_ = d.UpdateUserSite(uAlice, siteFR)
	_ = d.UpdateUserSite(uBob, siteMA)
	_ = d.UpdateUserSite(uExt, siteFR)

	_ = d.AddTeamMember(teamA, uAlice)
	_ = d.AddTeamMember(teamA, uBob)
	_ = d.AddTeamMember(teamA, uGone)
	_ = d.AddTeamMember(teamB, uAlice)
	_ = d.AddTeamMember(teamB, uChlo)
	_ = d.AddTeamMember(teamExternal, uExt)
	leftApr := "2026-04-30"
	_ = d.SetTeamMemberLeftAt(teamA, uGone, &leftApr)

	// Presences prior in 2026 (YTD only, should NOT count in May stats)
	_ = d.SetPresences(uAlice, []string{"2026-02-02", "2026-02-03"}, stOffice, "full") // +2.0 YTD
	_ = d.SetPresences(uBob, []string{"2026-03-10"}, stRemote, "AM")                   // +0.5 YTD

	// Presences in May 2026
	// Alice: 2 full Office + 1 AM Office + 1 PM Remote + 1 full Leave -> 3.0 billable (2.5 on-site), 4.0 set days
	_ = d.SetPresences(uAlice, []string{"2026-05-04", "2026-05-05"}, stOffice, "full")
	_ = d.SetPresences(uAlice, []string{"2026-05-06"}, stOffice, "AM")
	_ = d.SetPresences(uAlice, []string{"2026-05-06"}, stRemote, "PM")
	_ = d.SetPresences(uAlice, []string{"2026-05-07"}, stLeave, "full")

	// Bob: 1 full Remote -> 1.0 billable (0 on-site), 1.0 set day
	_ = d.SetPresences(uBob, []string{"2026-05-04"}, stRemote, "full")

	// Chloe: 0.5 Office (AM) -> 0.5 billable (0.5 on-site), 0.5 set day
	_ = d.SetPresences(uChlo, []string{"2026-05-05"}, stOffice, "AM")

	// External (Sales): 3.0 Office -> counted ONLY in ExecSummary
	_ = d.SetPresences(uExt, []string{"2026-05-04", "2026-05-05", "2026-05-06"}, stOffice, "full")

	_ = d.CertifyMonth(uAlice, 2026, 5, 1)

	var captured map[string]interface{}
	h := &ActivityHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			captured = data.(map[string]interface{})
		},
	}

	mgrID, _ := d.CreateLocalUser("mgr_opt@test.com", "Opt Mgr", "pass1")
	_ = d.UpdateUserRoles(mgrID, models.RoleGlobal)
	_ = d.SetDomainManagers(domainID, []int64{mgrID})
	mgrTok, _ := d.CreateSession(mgrID)

	// 1. Domain view as global admin + domain manager (with external Sales team included in ExecSummary)
	{
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/activity?year=2026&month=5&domain=%d", domainID), nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: mgrTok})
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d: %s", w.Code, w.Body.String())
		}

		// Domain deduplicated users: Alice, Bob, Chloe (Gone excluded, Ext excluded)
		stats := captured["Stats"].([]models.UserStats)
		if len(stats) != 3 {
			t.Fatalf("expected 3 domain stats, got %d", len(stats))
		}
		if got := captured["TotalBillable"].(float64); !almostEqualFloat(got, 4.5) {
			t.Errorf("TotalBillable got %v, want 4.5", got)
		}
		if got := captured["TotalOnSite"].(float64); !almostEqualFloat(got, 3.0) {
			t.Errorf("TotalOnSite got %v, want 3.0", got)
		}
		// Working days:
		// Alice (FR): 21 - 2 (May 1 global, May 8 FR) = 19
		// Bob (MA):   21 - 1 (May 1 global) = 20
		// Chloe ("" ): 21 - 1 (May 1 global) = 20
		// Sum = 59.0; totalSetDays = 4.0 + 1.0 + 0.5 = 5.5 -> TotalNotSet = 53.5
		if got := captured["TotalWorkingDays"].(float64); !almostEqualFloat(got, 59.0) {
			t.Errorf("TotalWorkingDays got %v, want 59.0", got)
		}
		if got := captured["TotalNotSet"].(float64); !almostEqualFloat(got, 53.5) {
			t.Errorf("TotalNotSet got %v, want 53.5", got)
		}
		// YTD billable across domain: Alice (2.0+3.0=5.0) + Bob (0.5+1.0=1.5) + Chloe (0.5) = 7.0
		if got := captured["TotalYTDBillable"].(float64); !almostEqualFloat(got, 7.0) {
			t.Errorf("TotalYTDBillable got %v, want 7.0", got)
		}
		ytdByUser := captured["YTDBillableByUser"].(map[int64]float64)
		if !almostEqualFloat(ytdByUser[uAlice], 5.0) || !almostEqualFloat(ytdByUser[uBob], 1.5) || !almostEqualFloat(ytdByUser[uChlo], 0.5) {
			t.Errorf("unexpected YTDBillableByUser: %+v", ytdByUser)
		}

		// ExecSummary includes Ext (3.0 billable, 3.0 on-site, 19 working days)
		if got := captured["ExecUserCount"].(int); got != 4 {
			t.Errorf("ExecUserCount got %d, want 4", got)
		}
		if got := captured["ExecTotalBillable"].(float64); !almostEqualFloat(got, 7.5) {
			t.Errorf("ExecTotalBillable got %v, want 7.5", got)
		}
		if got := captured["ExecTotalWorkingDays"].(float64); !almostEqualFloat(got, 78.0) {
			t.Errorf("ExecTotalWorkingDays got %v, want 78.0", got)
		}

		// Per-team groups sorted alphabetically: Backend, Empty, Frontend
		groups := captured["DomainTeamGroups"].([]DomainTeamGroup)
		if len(groups) != 3 {
			t.Fatalf("expected 3 domain team groups, got %d", len(groups))
		}
		// Backend: Alice + Bob -> billable=4.0, onsite=2.5, working=19+20=39, set=5.0, notSet=34, YTD=6.5
		// Team holidays for Backend [FR, MA]: only global May 1 applies to ALL countries -> WorkingDaysExcl = 20
		if groups[0].Team.Name != "Backend" || !almostEqualFloat(groups[0].TotalBillable, 4.0) ||
			!almostEqualFloat(groups[0].TotalOnSite, 2.5) || !almostEqualFloat(groups[0].TotalWorkingDays, 39.0) ||
			!almostEqualFloat(groups[0].TotalNotSet, 34.0) || !almostEqualFloat(groups[0].YTDBillable, 6.5) ||
			groups[0].WorkingDaysExcl != 20 {
			t.Errorf("unexpected Backend group: %+v", groups[0])
		}
		// Empty: 0 members
		if groups[1].Team.Name != "Empty" || len(groups[1].Stats) != 0 || groups[1].TotalBillable != 0 {
			t.Errorf("unexpected Empty group: %+v", groups[1])
		}
		// Frontend: Alice + Chloe -> billable=3.5, onsite=3.0, working=19+20=39, set=4.5, notSet=34.5, YTD=5.5
		if groups[2].Team.Name != "Frontend" || !almostEqualFloat(groups[2].TotalBillable, 3.5) ||
			!almostEqualFloat(groups[2].TotalOnSite, 3.0) || !almostEqualFloat(groups[2].TotalWorkingDays, 39.0) ||
			!almostEqualFloat(groups[2].TotalNotSet, 34.5) || !almostEqualFloat(groups[2].YTDBillable, 5.5) {
			t.Errorf("unexpected Frontend group: %+v", groups[2])
		}

		// Rendered HTML contract (compact DOM + shared lock symbol)
		rendered := renderAdminActivityContent(t, captured)
		if !strings.Contains(rendered, `href="#act-lock-icon"`) {
			t.Error("expected shared #act-lock-icon symbol usage in rendered template")
		}
		if !strings.Contains(rendered, fmt.Sprintf("team-member-row-%d hidden", teamA)) {
			t.Errorf("expected folded team row class for team %d", teamA)
		}
	}

	// 2. Site filter on domain view + sameTeamIDSet reuse check after attaching external team
	_ = d.UpdateTeamDomain(teamExternal, domainID)
	{
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/activity?year=2026&month=5&domain=%d&site=%d", domainID, siteFR), nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: mgrTok})
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("site filtered status=%d", w.Code)
		}
		// Only Alice & Ext are on siteFR -> 6.0 billable, 5.5 on-site
		if got := captured["TotalBillable"].(float64); !almostEqualFloat(got, 6.0) {
			t.Errorf("site-filtered TotalBillable got %v, want 6.0", got)
		}
		if got := captured["ExecTotalBillable"].(float64); !almostEqualFloat(got, 6.0) {
			t.Errorf("sameTeamIDSet ExecTotalBillable got %v, want 6.0", got)
		}
		if got := captured["ExecUserCount"].(int); got != 2 {
			t.Errorf("sameTeamIDSet ExecUserCount got %d, want 2", got)
		}
	}
}

// TestGetUsersSQLAggregations_LargeChunking verifies SQL chunking (>500 IDs) and exact
// equivalence across GetTeamsMemberGroupsAt, GetUsersStatusCounts, and GetUsersBillableDays.
func TestGetUsersSQLAggregations_LargeChunking(t *testing.T) {
	d := newExtraTestDB(t)
	stBill, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})
	stOff, _ := d.CreateStatus(models.Status{Name: "Leave", Color: "#f59e0b", Billable: false, OnSite: false, SortOrder: 2})

	teamID, _ := d.CreateTeam("MegaTeam")
	const totalUsers = 520 // > idBatchSize (500) to exercise multi-chunk SQL IN (...)
	userIDs := make([]int64, 0, totalUsers)
	for i := 0; i < totalUsers; i++ {
		uid, err := d.CreateLocalUser(fmt.Sprintf("chunk_%03d@test.com", i), fmt.Sprintf("User %03d", i), "pass")
		if err != nil {
			t.Fatalf("CreateLocalUser(%d): %v", i, err)
		}
		_ = d.AddTeamMember(teamID, uid)
		userIDs = append(userIDs, uid)
		switch i {
		case 0:
			_ = d.SetPresences(uid, []string{"2026-01-05"}, stBill, "AM")
			_ = d.SetPresences(uid, []string{"2026-01-05"}, stOff, "PM")
		case totalUsers - 1:
			_ = d.SetPresences(uid, []string{"2026-01-06", "2026-01-07"}, stBill, "full")
		}
	}

	deduped, byTeam, err := d.GetTeamsMemberGroupsAt([]int64{teamID}, "2026-01-01")
	if err != nil {
		t.Fatalf("GetTeamsMemberGroupsAt: %v", err)
	}
	if len(deduped) != totalUsers || len(byTeam[teamID]) != totalUsers {
		t.Fatalf("expected %d members, got deduped=%d byTeam=%d", totalUsers, len(deduped), len(byTeam[teamID]))
	}

	counts, err := d.GetUsersStatusCounts(userIDs, "2026-01-01", "2026-01-31")
	if err != nil {
		t.Fatalf("GetUsersStatusCounts: %v", err)
	}
	if !almostEqualFloat(counts[userIDs[0]][stBill], 0.5) || !almostEqualFloat(counts[userIDs[0]][stOff], 0.5) {
		t.Errorf("unexpected chunk-first user counts: %+v", counts[userIDs[0]])
	}
	if !almostEqualFloat(counts[userIDs[totalUsers-1]][stBill], 2.0) {
		t.Errorf("unexpected second-chunk user counts: %+v", counts[userIDs[totalUsers-1]])
	}

	billable, err := d.GetUsersBillableDays(userIDs, "2026-01-01", "2026-01-31")
	if err != nil {
		t.Fatalf("GetUsersBillableDays: %v", err)
	}
	if !almostEqualFloat(billable[userIDs[0]], 0.5) || !almostEqualFloat(billable[userIDs[totalUsers-1]], 2.0) {
		t.Errorf("unexpected billable totals across chunks: first=%v last=%v", billable[userIDs[0]], billable[userIDs[totalUsers-1]])
	}
	stats := db.BuildUsersStats(deduped, []models.Status{
		{ID: stBill, Billable: true, OnSite: true},
		{ID: stOff, Billable: false, OnSite: false},
	}, counts)
	if len(stats) != totalUsers {
		t.Fatalf("expected %d built stats, got %d", totalUsers, len(stats))
	}
}

// BenchmarkActivityPage_DomainManyTeams benchmarks /admin/activity?domain=...
// across 10 teams x 40 collaborators (400 users) and 6 months of YTD presences.
func BenchmarkActivityPage_DomainManyTeams(b *testing.B) {
	d, err := db.Open(&config.Config{DBDriver: "sqlite", DataDir: b.TempDir()})
	if err != nil {
		b.Fatalf("db.Open: %v", err)
	}
	defer d.Close() //nolint:errcheck
	d.SetBcryptCost(4)

	adminID, _ := d.CreateLocalUser("bench_admin@test.com", "Bench Admin", "pass123")
	_ = d.UpdateUserRoles(adminID, models.RoleGlobal)
	tok, _ := d.CreateSession(adminID)

	siteFR, _ := d.CreateSite(models.Site{Name: "Paris", CountryCode: "FR", Seats: 500})
	stOffice, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})
	stRemote, _ := d.CreateStatus(models.Status{Name: "Remote", Color: "#3b82f6", Billable: true, OnSite: false, SortOrder: 2})
	_, _ = d.CreateHoliday("2026-06-01", "Holiday 1", false, "FR")

	domainID, _ := d.CreateDomain("Big Domain")
	_ = d.SetDomainManagers(domainID, []int64{adminID})
	dates := []string{
		"2026-01-05", "2026-01-06", "2026-02-02", "2026-02-03",
		"2026-03-02", "2026-03-03", "2026-04-06", "2026-04-07",
		"2026-05-04", "2026-05-05", "2026-06-02", "2026-06-03",
		"2026-06-04", "2026-06-05", "2026-06-08", "2026-06-09",
	}

	const numTeams = 10
	const usersPerTeam = 40
	for tIdx := 0; tIdx < numTeams; tIdx++ {
		tid, _ := d.CreateTeam(fmt.Sprintf("Team %02d", tIdx))
		_ = d.UpdateTeamDomain(tid, domainID)
		for uIdx := 0; uIdx < usersPerTeam; uIdx++ {
			uid, _ := d.CreateLocalUser(fmt.Sprintf("u_%02d_%02d@bench.test", tIdx, uIdx), fmt.Sprintf("Collab %02d-%02d", tIdx, uIdx), "p")
			_ = d.UpdateUserSite(uid, siteFR)
			_ = d.AddTeamMember(tid, uid)
			_ = d.SetPresences(uid, dates[:10], stOffice, "full")
			_ = d.SetPresences(uid, dates[10:], stRemote, "full")
		}
	}

	var renderedBytes int
	h := &ActivityHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			renderedBytes = len(data.(map[string]interface{})["DomainTeamGroups"].([]DomainTeamGroup))
			_, _ = w.Write([]byte("ok"))
		},
	}

	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/activity?year=2026&month=6&domain=%d", domainID), nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: tok})
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
		if w.Code != http.StatusOK || renderedBytes != numTeams {
			b.Fatalf("unexpected result: status=%d teams=%d", w.Code, renderedBytes)
		}
	}
}
