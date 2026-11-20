package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

func newBenchDB(b *testing.B) *db.DB {
	b.Helper()
	d, err := db.Open(&config.Config{DBDriver: "sqlite", DataDir: b.TempDir()})
	if err != nil {
		b.Fatalf("db.Open: %v", err)
	}
	b.Cleanup(d.Close)
	d.SetBcryptCost(4)
	return d
}

func TestCalendarPage_BatchOptimization_Parity(t *testing.T) {
	d := newExtraTestDB(t)

	siteFR, _ := d.CreateSite(models.Site{Name: "Paris HQ", CountryCode: "FR", Seats: 30})
	siteMA, _ := d.CreateSite(models.Site{Name: "Casablanca", CountryCode: "MA", Seats: 20})
	siteOther, _ := d.CreateSite(models.Site{Name: "London", CountryCode: "GB", Seats: 10})

	fpFR, _ := d.CreateFloorplanWithSite("FR-F1", siteFR, 1)
	seatBob, _ := d.CreateSeat(fpFR, "A1", 10, 10)
	seatAliceGuest, _ := d.CreateSeat(fpFR, "A2", 20, 10)
	_, _ = d.CreateLocker(models.Locker{SiteID: siteFR, Number: "101", Zone: "North", Status: "active"})
	_, _ = d.CreateLocker(models.Locker{SiteID: siteOther, Number: "999", Zone: "West", Status: "active"})

	_, _ = d.CreateHoliday("2026-05-01", "Labour Day", false, "")
	_, _ = d.CreateHoliday("2026-05-08", "Victory Day", false, "FR")
	_, _ = d.CreateHoliday("2026-05-20", "London Special", false, "GB")

	stOffice, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})
	stRemote, _ := d.CreateStatus(models.Status{Name: "Remote", Color: "#3b82f6", Billable: true, OnSite: false, SortOrder: 2})

	aliceID, _ := d.CreateLocalUser("cal_alice@test.com", "Alice", "pass1")
	bobID, _ := d.CreateLocalUser("cal_bob@test.com", "Bob", "pass1")
	chloeID, _ := d.CreateLocalUser("cal_chloe@test.com", "Chloe", "pass1")
	goneID, _ := d.CreateLocalUser("cal_gone@test.com", "Gone", "pass1")

	_ = d.UpdateUserSite(aliceID, siteFR)
	_ = d.UpdateUserSite(bobID, siteMA)
	// Chloe stays unassigned; Gone left before May

	teamA, _ := d.CreateTeam("Team A")
	teamB, _ := d.CreateTeam("Team B")
	_ = d.AddTeamMember(teamA, aliceID)
	_ = d.AddTeamMember(teamA, bobID)
	_ = d.AddTeamMember(teamA, chloeID)
	_ = d.AddTeamMember(teamA, goneID)
	leftApr := "2026-04-30"
	leftMidMay := "2026-05-15"
	_ = d.SetTeamMemberLeftAt(teamA, goneID, &leftApr)
	_ = d.SetTeamMemberLeftAt(teamA, chloeID, &leftMidMay)
	_ = d.SetTeamLeaders(teamA, []int64{aliceID}) // Alice leads Team A only

	_ = d.AddTeamMember(teamB, aliceID)
	_ = d.AddTeamMember(teamB, bobID)
	_ = d.SetTeamLeaders(teamB, []int64{bobID})

	// Presences, third-party override, reservations, certifications, activities
	_ = d.SetPresences(aliceID, []string{"2026-05-04"}, stOffice, "full")
	_ = d.SetPresences(bobID, []string{"2026-05-04"}, stRemote, "full")
	_ = d.SetPresences(chloeID, []string{"2026-05-04"}, stOffice, "AM")
	_ = d.LogPresenceAction(aliceID, bobID, "set", []string{"2026-05-04"}, stRemote, "full")

	// Bob reserves own desk; Alice only books for a guest -> must NOT mark Alice's ReservationDates
	_ = d.ReserveSeat(seatBob, bobID, "2026-05-05", "full")
	_ = d.ReserveSeat(seatAliceGuest, aliceID, "2026-05-06", "full", "External Guest")

	_ = d.CertifyMonth(aliceID, 2026, 5, aliceID)
	_ = d.CertifyMonth(bobID, 2026, 5, bobID)

	if _, err := d.SetUserDayActivities(aliceID, "2026-05-04", []models.ProjectActivity{
		{ActivityType: models.ActivityTypeOther, Comment: "Dev", Percentage: 100},
	}); err != nil {
		t.Fatalf("SetUserDayActivities alice: %v", err)
	}
	if _, err := d.SetUserDayActivities(bobID, "2026-05-04", []models.ProjectActivity{
		{ActivityType: models.ActivityTypeOther, Comment: "Half", Percentage: 50},
	}); err != nil {
		t.Fatalf("SetUserDayActivities bob: %v", err)
	}
	if _, err := d.SetUserDayActivities(chloeID, "2026-05-04", []models.ProjectActivity{
		{ActivityType: models.ActivityTypeOther, Comment: "Full", Percentage: 100},
	}); err != nil {
		t.Fatalf("SetUserDayActivities chloe: %v", err)
	}

	var captured map[string]interface{}
	h := &CalendarHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			captured = data.(map[string]interface{})
		},
	}

	tok, _ := d.CreateSession(aliceID)
	req := httptest.NewRequest(http.MethodGet, "/?year=2026&month=5", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)

	if w.Code != http.StatusOK || captured == nil {
		t.Fatalf("CalendarPage failed: status=%d captured=%v", w.Code, captured != nil)
	}

	// Verify holidays matching Alice's FR site (not GB)
	days := captured["Days"].([]models.DayInfo)
	holCount := 0
	for _, day := range days {
		if day.IsHoliday {
			holCount++
			if day.Date != "2026-05-01" && day.Date != "2026-05-08" {
				t.Errorf("unexpected holiday on %s for FR user", day.Date)
			}
		}
	}
	if holCount != 2 {
		t.Errorf("expected 2 FR/global holidays, got %d", holCount)
	}

	if cert, _ := captured["Certified"].(bool); !cert {
		t.Error("expected Alice month to be Certified=true")
	}
	resDates := captured["ReservationDates"].(map[string]bool)
	if resDates["2026-05-06"] {
		t.Error("guest reservation should not appear in self ReservationDates")
	}
	ownActs := captured["ProjectActivities"].(map[string]bool)
	if !ownActs["2026-05-04"] {
		t.Error("expected Alice 100% project activity on 2026-05-04")
	}

	lockerSites := captured["LockerSites"].([]*models.Site)
	if len(lockerSites) != 1 || lockerSites[0].ID != siteFR || lockerSites[0].AvailableLockersCount != 1 {
		t.Fatalf("expected only FR site in LockerSites, got %+v", lockerSites)
	}

	teamViews := captured["TeamViews"].([]teamCalendarView)
	if len(teamViews) != 2 {
		t.Fatalf("expected 2 teamViews, got %d", len(teamViews))
	}

	tvA, tvB := teamViews[0], teamViews[1]
	if tvA.Team.ID == teamB {
		tvA, tvB = tvB, tvA
	}
	if !tvA.CanEdit || len(tvA.Members) != 3 || !tvA.HasUnassignedSite {
		t.Fatalf("unexpected Team A view: canEdit=%v members=%d unassigned=%v", tvA.CanEdit, len(tvA.Members), tvA.HasUnassignedSite)
	}
	if len(tvA.Sites) != 2 || tvA.Sites[0] != "Casablanca" || tvA.Sites[1] != "Paris HQ" {
		t.Fatalf("unexpected Team A sites: %v", tvA.Sites)
	}
	if tvA.ProjectActivities[bobID]["2026-05-04"] || !tvA.ProjectActivities[chloeID]["2026-05-04"] {
		t.Fatalf("unexpected Team A activity completion flags: %+v", tvA.ProjectActivities)
	}
	if !tvA.Reservations[bobID]["2026-05-05"] || !tvA.Certifications[bobID] {
		t.Fatalf("expected Bob reservation & certification in Team A view")
	}
	if tvA.Overrides[bobID]["2026-05-04"].ActorID != aliceID {
		t.Fatalf("expected Alice override for Bob on 2026-05-04, got %+v", tvA.Overrides[bobID])
	}

	if tvB.CanEdit || len(tvB.Members) != 2 {
		t.Fatalf("expected Team B read-only with 2 members, got canEdit=%v len=%d", tvB.CanEdit, len(tvB.Members))
	}
	if _, leaked := tvB.Certifications[chloeID]; leaked {
		t.Error("Team B should not contain Chloe's certification")
	}
	if _, leaked := tvB.ProjectActivities[chloeID]; leaked {
		t.Error("Team B should not contain Chloe's project activity")
	}
}

func TestProjectsReport_BatchOptimization_ParityAndDomainActivities(t *testing.T) {
	d := newExtraTestDB(t)

	domID, _ := d.CreateDomain("Product Domain")
	otherDomID, _ := d.CreateDomain("Other Domain")

	teamProj, _ := d.CreateTeamWithDetails("Delivery", "", false, false)
	teamManualA, _ := d.CreateTeamWithDetails("Support A", "SUP", true, false)
	teamManualB, _ := d.CreateTeamWithDetails("Support B", "SUP", true, false)
	teamOtherProj, _ := d.CreateTeamWithDetails("External", "", false, false)
	_ = d.UpdateTeamDomain(teamProj, domID)
	_ = d.UpdateTeamDomain(teamManualA, domID)
	_ = d.UpdateTeamDomain(teamManualB, domID)
	_ = d.UpdateTeamDomain(teamOtherProj, otherDomID)

	mgrID, _ := d.CreateLocalUser("dom_mgr@test.com", "Domain Mgr", "pass1")
	_ = d.SetDomainManagers(domID, []int64{mgrID})

	aliceID, _ := d.CreateLocalUser("pr_alice@test.com", "Alice", "pass1")
	bobID, _ := d.CreateLocalUser("pr_bob@test.com", "Bob", "pass1")
	_ = d.AddTeamMember(teamProj, aliceID)
	_ = d.AddTeamMember(teamManualA, aliceID)
	_ = d.AddTeamMember(teamManualB, aliceID) // Shared user across 2 manual teams in same domain
	_ = d.AddTeamMember(teamManualB, bobID)
	left := "2026-04-01"
	_ = d.SetTeamMemberLeftAt(teamManualB, bobID, &left) // Departed member still included in historical activities

	pidIn, _ := d.CreateProject("Core App", "CORE", teamProj, true, "2026-01-01", "2026-12-31")
	pidOut, _ := d.CreateProject("Secret", "SEC", teamOtherProj, true, "2026-01-01", "2026-12-31")
	_ = d.SetProjectTimeEntry(aliceID, pidIn, 2026, 5, 3.0)
	_ = d.SetProjectTimeEntry(aliceID, pidOut, 2026, 5, 5.0)

	_, _ = d.SetUserDayActivities(aliceID, "2026-05-04", []models.ProjectActivity{
		{ActivityType: models.ActivityTypeOther, Comment: "Ticket 1", Percentage: 100},
	})
	_, _ = d.SetUserDayActivities(bobID, "2026-05-05", []models.ProjectActivity{
		{ActivityType: models.ActivityTypeOther, Comment: "Ticket 2", Percentage: 50},
	})

	var captured map[string]interface{}
	h := &ProjectsHandler{
		DB:     d,
		Config: &config.Config{},
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			captured = data.(map[string]interface{})
		},
	}

	tok, _ := d.CreateSession(mgrID)

	// 1. Summary view scoped to domain
	reqSummary := httptest.NewRequest(http.MethodGet, "/admin/projects-report?date_from=2026-05&date_to=2026-05", nil)
	reqSummary.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w1 := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w1, reqSummary)
	if w1.Code != http.StatusOK || captured == nil {
		t.Fatalf("ProjectsReportPage summary failed: status=%d", w1.Code)
	}
	if !captured["ShowProjectsTab"].(bool) || !captured["ShowTasksTab"].(bool) {
		t.Fatalf("expected both tabs visible for domain manager with mixed teams")
	}
	rows := captured["Rows"].([]models.ProjectReportRow)
	if len(rows) != 1 || rows[0].Project.ID != pidIn || rows[0].TotalDays != 3.0 {
		t.Fatalf("expected 1 scoped project row (pid=%d days=3), got %+v", pidIn, rows)
	}

	// 2. Activities view via API (domain aggregation, deduplicated shared member)
	reqActs := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/projects-report?view=activities&domain=%d&year=2026&month=5", domID), nil)
	reqActs.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w2 := httptest.NewRecorder()
	w2.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w2, reqActs)
	if w2.Code != http.StatusOK {
		t.Fatalf("ProjectsReportAPI activities failed: status=%d body=%s", w2.Code, w2.Body.String())
	}
	var payload struct {
		Activities []models.ProjectActivity `json:"activities"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode activities API: %v", err)
	}
	if len(payload.Activities) != 2 {
		t.Fatalf("expected 2 deduplicated activities (Alice + Bob), got %d: %+v", len(payload.Activities), payload.Activities)
	}
}

func TestSitesReport_BatchOptimization_Parity(t *testing.T) {
	d := newExtraTestDB(t)

	siteFR1, _ := d.CreateSite(models.Site{Name: "Paris 1", CountryCode: "FR", Seats: 20})
	siteFR2, _ := d.CreateSite(models.Site{Name: "Paris 2", CountryCode: "FR", Seats: 10})
	siteDE, _ := d.CreateSite(models.Site{Name: "Berlin", CountryCode: "DE", Seats: 10})

	// May 2026: 21 weekdays total. Global holiday -> 20 working days for DE; +FR holiday -> 19 for Paris 1 & Paris 2.
	_, _ = d.CreateHoliday("2026-05-01", "Labour Day", false, "")
	_, _ = d.CreateHoliday("2026-05-08", "Victory Day", false, "FR")

	stOnSite, _ := d.CreateStatus(models.Status{Name: "Bureau", Color: "#10b981", Billable: true, OnSite: true, SortOrder: 1})

	domID, _ := d.CreateDomain("Ops")
	teamID, _ := d.CreateTeam("SRE")
	_ = d.UpdateTeamDomain(teamID, domID)

	u1, _ := d.CreateLocalUser("sr_u1@test.com", "SR U1", "pass1")
	u2, _ := d.CreateLocalUser("sr_u2@test.com", "SR U2", "pass1")
	uDisabled, _ := d.CreateLocalUser("sr_dis@test.com", "Disabled", "pass1")
	_ = d.UpdateUserSite(u1, siteFR1)
	_ = d.UpdateUserSite(u2, siteFR2)
	_ = d.UpdateUserSite(uDisabled, siteFR1)
	_ = d.SetUserDisabled(uDisabled, true)
	_ = d.AddTeamMember(teamID, u1)

	_ = d.SetPresences(u1, []string{"2026-05-04", "2026-05-05"}, stOnSite, "full")
	_ = d.SetPresences(u2, []string{"2026-05-04"}, stOnSite, "AM")

	var captured *SitesReportPageData
	h := &FloorplanHandler{
		DB:      d,
		DataDir: t.TempDir(),
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			captured = data.(*SitesReportPageData)
		},
	}

	req := createAdminReq(t, d, http.MethodGet, "/admin/sites-report?year=2026&month=5", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SitesReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK || captured == nil {
		t.Fatalf("SitesReportPage failed: status=%d", w.Code)
	}

	if len(captured.Summaries) != 3 {
		t.Fatalf("expected 3 site summaries, got %d", len(captured.Summaries))
	}
	bySite := make(map[int64]models.SiteReportSummary, 3)
	for _, s := range captured.Summaries {
		bySite[s.Site.ID] = s
	}
	if bySite[siteFR1].AttachedPeople != 1 || bySite[siteFR1].WorkingDays != 19 || !almostEqualFloat(bySite[siteFR1].TotalOnSiteDays, 2.0) {
		t.Fatalf("unexpected Paris 1 summary: %+v", bySite[siteFR1])
	}
	if bySite[siteFR2].AttachedPeople != 1 || bySite[siteFR2].WorkingDays != 19 || !almostEqualFloat(bySite[siteFR2].TotalOnSiteDays, 0.5) {
		t.Fatalf("unexpected Paris 2 summary: %+v", bySite[siteFR2])
	}
	if bySite[siteDE].WorkingDays != 20 {
		t.Fatalf("unexpected Berlin summary: %+v", bySite[siteDE])
	}
	if len(bySite[siteFR1].Domains) != 1 || len(bySite[siteFR1].Domains[0].Teams) != 1 {
		t.Fatalf("expected Ops/SRE breakdown on Paris 1, got %+v", bySite[siteFR1].Domains)
	}
}

func BenchmarkCalendarPage_MultiTeam(b *testing.B) {
	d := newBenchDB(b)
	siteFR, _ := d.CreateSite(models.Site{Name: "Paris HQ", CountryCode: "FR", Seats: 200})
	fpID, _ := d.CreateFloorplanWithSite("Main", siteFR, 1)
	seatID, _ := d.CreateSeat(fpID, "S-1", 10, 10)
	_, _ = d.CreateLocker(models.Locker{SiteID: siteFR, Number: "L1", Zone: "Z1", Status: "active"})
	stOffice, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})
	_, _ = d.CreateHoliday("2026-05-08", "Victory", false, "FR")

	userID, _ := d.CreateLocalUser("bench_cal_owner@test.com", "Owner", "p")
	_ = d.UpdateUserSite(userID, siteFR)
	tok, _ := d.CreateSession(userID)

	for tIdx := 0; tIdx < 5; tIdx++ {
		tid, _ := d.CreateTeam(fmt.Sprintf("Team %d", tIdx))
		_ = d.AddTeamMember(tid, userID)
		_ = d.SetTeamLeaders(tid, []int64{userID})
		for uIdx := 0; uIdx < 30; uIdx++ {
			uid, _ := d.CreateLocalUser(fmt.Sprintf("cal_m_%d_%d@test.com", tIdx, uIdx), fmt.Sprintf("Member %d-%d", tIdx, uIdx), "p")
			_ = d.UpdateUserSite(uid, siteFR)
			_ = d.AddTeamMember(tid, uid)
			_ = d.SetPresences(uid, []string{"2026-05-04", "2026-05-05", "2026-05-06"}, stOffice, "full")
			_ = d.CertifyMonth(uid, 2026, 5, uid)
			_, _ = d.SetUserDayActivities(uid, "2026-05-04", []models.ProjectActivity{
				{ActivityType: models.ActivityTypeOther, Comment: "Work", Percentage: 100},
			})
			if uIdx == 0 {
				_ = d.ReserveSeat(seatID, uid, "2026-05-04", "full")
			}
		}
	}

	h := &CalendarHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			_, _ = w.Write([]byte("ok"))
		},
	}

	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/?year=2026&month=5", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: tok})
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("status=%d", w.Code)
		}
	}
}

func BenchmarkProjectsReportPage(b *testing.B) {
	d := newBenchDB(b)
	adminID, _ := d.CreateLocalUser("bench_pr_admin@test.com", "Admin", "p")
	_ = d.UpdateUserRoles(adminID, models.RoleGlobal)
	tok, _ := d.CreateSession(adminID)

	userIDs := make([]int64, 50)
	for i := 0; i < 50; i++ {
		uid, _ := d.CreateLocalUser(fmt.Sprintf("pr_u_%d@test.com", i), fmt.Sprintf("User %d", i), "p")
		userIDs[i] = uid
	}
	teamID, _ := d.CreateTeam("Engineering")
	for i := 0; i < 30; i++ {
		pid, _ := d.CreateProject(fmt.Sprintf("Project %02d", i), fmt.Sprintf("P%02d", i), teamID, true, "2026-01-01", "2026-12-31")
		for _, uid := range userIDs[:10] {
			_ = d.SetProjectTimeEntry(uid, pid, 2026, 4, 2.0)
			_ = d.SetProjectTimeEntry(uid, pid, 2026, 5, 3.0)
		}
	}

	h := &ProjectsHandler{
		DB:     d,
		Config: &config.Config{},
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			_, _ = w.Write([]byte("ok"))
		},
	}

	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/admin/projects-report?date_from=2026-03&date_to=2026-05", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: tok})
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("status=%d", w.Code)
		}
	}
}
