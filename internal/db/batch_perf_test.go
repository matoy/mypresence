package db

import (
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/models"
)

func TestBatchDBOptimizations_ParityAndChunking(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(4)

	siteFR, err := d.CreateSite(models.Site{Name: "Paris HQ", CountryCode: "FR", Seats: 50})
	if err != nil {
		t.Fatalf("CreateSite FR: %v", err)
	}
	siteEmpty, err := d.CreateSite(models.Site{Name: "Empty Site", CountryCode: "DE", Seats: 10})
	if err != nil {
		t.Fatalf("CreateSite Empty: %v", err)
	}
	fpID, err := d.CreateFloorplanWithSite("Floor 1", siteFR, 1)
	if err != nil {
		t.Fatalf("CreateFloorplanWithSite: %v", err)
	}
	seatID, err := d.CreateSeat(fpID, "S-01", 10, 10)
	if err != nil {
		t.Fatalf("CreateSeat: %v", err)
	}

	lk1, err := d.CreateLocker(models.Locker{SiteID: siteFR, Number: "L1", Zone: "A", Status: "active"})
	if err != nil {
		t.Fatalf("CreateLocker L1: %v", err)
	}
	_, err = d.CreateLocker(models.Locker{SiteID: siteFR, Number: "L2", Zone: "A", Status: "maintenance"})
	if err != nil {
		t.Fatalf("CreateLocker L2: %v", err)
	}

	// GetSiteWithLockers checks: zero, unknown, no-lockers, active/maint/claimed
	if ls, err := d.GetSiteWithLockers(0); err != nil || ls != nil {
		t.Fatalf("expected nil for site 0, got (%v, %v)", ls, err)
	}
	if ls, err := d.GetSiteWithLockers(999999); err != nil || ls != nil {
		t.Fatalf("expected nil for unknown site, got (%v, %v)", ls, err)
	}
	if ls, err := d.GetSiteWithLockers(siteEmpty); err != nil || ls != nil {
		t.Fatalf("expected nil for site without lockers, got (%v, %v)", ls, err)
	}
	ls, err := d.GetSiteWithLockers(siteFR)
	if err != nil || ls == nil || ls.LockersCount != 2 || ls.AvailableLockersCount != 1 {
		t.Fatalf("unexpected GetSiteWithLockers result: %+v, err=%v", ls, err)
	}

	uid1 := seedUser(t, d, "batch1@test.com")
	uid2 := seedUser(t, d, "batch2@test.com")
	if err := d.UpdateUserSite(uid1, siteFR); err != nil {
		t.Fatalf("UpdateUserSite: %v", err)
	}
	if err := d.UpdateUserSite(uid2, siteFR); err != nil {
		t.Fatalf("UpdateUserSite 2: %v", err)
	}

	if err := d.ClaimLocker(lk1, uid2, "1234"); err != nil {
		t.Fatalf("ClaimLocker: %v", err)
	}
	lsClaimed, err := d.GetSiteWithLockers(siteFR)
	if err != nil || lsClaimed == nil || lsClaimed.AvailableLockersCount != 0 {
		t.Fatalf("expected 0 available lockers after claim, got %+v (err=%v)", lsClaimed, err)
	}

	// GetSessionUser hydrates SiteName & SiteCountryCode and clears cache on UpdateSite/DeleteSite
	tok, err := d.CreateSession(uid1)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	su, err := d.GetSessionUser(tok)
	if err != nil {
		t.Fatalf("GetSessionUser: %v", err)
	}
	if su.SiteName != "Paris HQ" || su.SiteCountryCode != "FR" {
		t.Fatalf("expected session user site Paris HQ / FR, got %q / %q", su.SiteName, su.SiteCountryCode)
	}
	if err := d.UpdateSite(models.Site{ID: siteFR, Name: "Paris Updated", CountryCode: "ES", Seats: 60}); err != nil {
		t.Fatalf("UpdateSite: %v", err)
	}
	suUpdated, err := d.GetSessionUser(tok)
	if err != nil {
		t.Fatalf("GetSessionUser after UpdateSite: %v", err)
	}
	if suUpdated.SiteName != "Paris Updated" || suUpdated.SiteCountryCode != "ES" {
		t.Fatalf("expected refreshed session cache after UpdateSite, got %q / %q", suUpdated.SiteName, suUpdated.SiteCountryCode)
	}

	// Holidays: GetHolidayMapForCountry vs GetUserHolidayMap + DeleteSite invalidates cache
	if _, err := d.CreateHoliday("2026-07-14", "Bastille", false, "FR"); err != nil {
		t.Fatalf("CreateHoliday FR: %v", err)
	}
	if _, err := d.CreateHoliday("2026-10-12", "Hispanic Day", false, "ES"); err != nil {
		t.Fatalf("CreateHoliday ES: %v", err)
	}
	uHol, err := d.GetUserHolidayMap(uid1, "2026-07-01", "2026-10-31")
	if err != nil {
		t.Fatalf("GetUserHolidayMap: %v", err)
	}
	cHol, err := d.GetHolidayMapForCountry("ES", "2026-07-01", "2026-10-31")
	if err != nil {
		t.Fatalf("GetHolidayMapForCountry: %v", err)
	}
	if len(uHol) != len(cHol) || uHol["2026-10-12"].Name != "Hispanic Day" {
		t.Fatalf("holiday parity mismatch: user=%+v country=%+v", uHol, cHol)
	}
	if _, err := d.GetUserHolidayMap(999999, "2026-07-01", "2026-07-31"); err == nil {
		t.Fatal("expected error for unknown user in GetUserHolidayMap")
	}

	// Teams, ListUsersBasic vs ListUsers, GetAllTeamMembersByTeams (active > departed dedup)
	teamA, _ := d.CreateTeam("Team Alpha")
	teamB, _ := d.CreateTeam("Team Beta")
	_ = d.AddTeamMember(teamA, uid1)
	_ = d.AddTeamMember(teamA, uid2)
	_ = d.AddTeamMember(teamB, uid1)
	_ = d.AddTeamMember(teamB, uid2)
	left := "2026-06-15"
	_ = d.SetTeamMemberLeftAt(teamA, uid2, &left) // Departed in A, still active in B
	_ = d.SetTeamLeaders(teamA, []int64{uid1})

	basicUsers, err := d.ListUsersBasic()
	if err != nil {
		t.Fatalf("ListUsersBasic: %v", err)
	}
	fullUsers, err := d.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(basicUsers) != len(fullUsers) || len(basicUsers) < 2 {
		t.Fatalf("unexpected user counts basic=%d full=%d", len(basicUsers), len(fullUsers))
	}
	for _, bu := range basicUsers {
		if bu.ID == uid1 {
			if len(bu.Teams) != 0 || bu.SiteName != "Paris Updated" {
				t.Fatalf("ListUsersBasic should skip Teams and keep SiteName: %+v", bu)
			}
		}
	}
	for _, fu := range fullUsers {
		if fu.ID == uid1 && len(fu.Teams) != 2 {
			t.Fatalf("ListUsers should keep Teams hydrated: %+v", fu)
		}
	}

	domainMembers, err := d.GetAllTeamMembersByTeams([]int64{teamA, teamB, teamA})
	if err != nil {
		t.Fatalf("GetAllTeamMembersByTeams: %v", err)
	}
	if len(domainMembers) != 2 {
		t.Fatalf("expected 2 deduplicated members across teams, got %d", len(domainMembers))
	}
	for _, m := range domainMembers {
		if m.ID == uid2 && m.LeftAt != nil {
			t.Fatalf("expected active membership in Team Beta to win over departed in Team Alpha, got left_at=%v", *m.LeftAt)
		}
	}

	leaders, leaderIDs, err := d.GetAllTeamsLeaders()
	if err != nil {
		t.Fatalf("GetAllTeamsLeaders: %v", err)
	}
	if len(leaderIDs[teamA]) != 1 || leaders[teamA][0].SiteName != "Paris Updated" || leaders[teamA][0].SiteCountryCode != "ES" {
		t.Fatalf("unexpected GetAllTeamsLeaders result: %+v", leaders[teamA])
	}

	// BuildProjectsReport & GetProjectsReport & GetProjectsByIDs
	pid1, _ := d.CreateProject("Proj One", "P1", teamA, true, "2026-01-01", "2026-12-31")
	pid2, _ := d.CreateProject("Proj Two", "P2", teamB, true, "2026-01-01", "2026-12-31")
	_ = d.SetProjectTimeEntry(uid1, pid1, 2026, 5, 4.0)
	_ = d.SetProjectTimeEntry(uid2, pid2, 2026, 6, 2.5)

	userMap := map[int64]models.User{
		uid1: {ID: uid1, Name: "U1"},
		uid2: {ID: uid2, Name: "U2"},
	}
	monthKeys := []string{"2026-05", "2026-06"}
	byTeamProjs, err := d.ListProjectsByTeams([]int64{teamA, teamB})
	if err != nil || len(byTeamProjs) != 2 {
		t.Fatalf("ListProjectsByTeams: len=%d err=%v", len(byTeamProjs), err)
	}
	builtRows, err := d.BuildProjectsReport(byTeamProjs, monthKeys, userMap)
	if err != nil {
		t.Fatalf("BuildProjectsReport: %v", err)
	}
	reportRows, err := d.GetProjectsReport([]int64{pid1, 999999, pid2}, monthKeys, userMap)
	if err != nil {
		t.Fatalf("GetProjectsReport: %v", err)
	}
	if len(builtRows) != 2 || len(reportRows) != 2 {
		t.Fatalf("expected 2 built and 2 report rows, got %d / %d", len(builtRows), len(reportRows))
	}
	if builtRows[0].TotalDays != reportRows[0].TotalDays || builtRows[1].TotalDays != reportRows[1].TotalDays {
		t.Fatalf("BuildProjectsReport vs GetProjectsReport mismatch: built=%+v report=%+v", builtRows, reportRows)
	}

	// DeleteSite clears session cache as well
	if err := d.DeleteSite(siteFR); err != nil {
		t.Fatalf("DeleteSite: %v", err)
	}
	suDeleted, err := d.GetSessionUser(tok)
	if err != nil {
		t.Fatalf("GetSessionUser after DeleteSite: %v", err)
	}
	if suDeleted.SiteID != 0 || suDeleted.SiteCountryCode != "" {
		t.Fatalf("expected site cleared in session cache after DeleteSite, got siteID=%d cc=%q", suDeleted.SiteID, suDeleted.SiteCountryCode)
	}

	// Verify >idBatchSize (500+) chunked queries find real records sitting past index 500
	stBillable, _ := d.CreateStatus(models.Status{Name: "Bill", Color: "#00ff00", Billable: true, SortOrder: 1})
	_ = d.SetPresences(uid1, []string{"2026-05-04"}, stBillable, "full")
	_ = d.LogPresenceAction(uid2, uid1, "set", []string{"2026-05-04"}, stBillable, "full")
	_ = d.ReserveSeat(seatID, uid1, "2026-05-04", "full")
	if _, err := d.SetUserDayActivities(uid1, "2026-05-04", []models.ProjectActivity{{
		ActivityType: models.ActivityTypeOther, Comment: "chunk test", Percentage: 100,
	}}); err != nil {
		t.Fatalf("SetUserDayActivities: %v", err)
	}

	largeUserBatch := make([]int64, 0, idBatchSize+15)
	for i := int64(0); i < int64(idBatchSize)+10; i++ {
		largeUserBatch = append(largeUserBatch, 100000+i)
	}
	largeUserBatch = append(largeUserBatch, uid1, uid1) // triggers chunk #2 AND tests dedup

	ovrMap, err := d.GetPresenceOverrides(largeUserBatch, "2026-05-01", "2026-05-31")
	if err != nil || ovrMap[uid1]["2026-05-04"].ActorID != uid2 {
		t.Fatalf("chunked GetPresenceOverrides failed: err=%v map=%+v", err, ovrMap[uid1])
	}
	resMap, err := d.GetUsersReservationDates(largeUserBatch, "2026-05-01", "2026-05-31")
	if err != nil || !resMap[uid1]["2026-05-04"] {
		t.Fatalf("chunked GetUsersReservationDates failed: err=%v map=%+v", err, resMap[uid1])
	}
	acts, err := d.GetActivitiesForUsersMonth(largeUserBatch, 2026, 5)
	if err != nil || len(acts) != 1 || acts[0].UserID != uid1 {
		t.Fatalf("chunked GetActivitiesForUsersMonth failed: err=%v acts=%+v", err, acts)
	}
	actsRange, err := d.GetActivitiesForUsersRange(largeUserBatch, "2026-05-01", "2026-05-31")
	if err != nil || len(actsRange[uid1]) != 1 {
		t.Fatalf("chunked GetActivitiesForUsersRange failed: err=%v map=%+v", err, actsRange[uid1])
	}
	billMap, err := d.GetUsersBillableDatesForRange(largeUserBatch, "2026-05-01", "2026-05-31")
	if err != nil || billMap[uid1]["2026-05-04"] != 1.0 {
		t.Fatalf("chunked GetUsersBillableDatesForRange failed: err=%v map=%+v", err, billMap[uid1])
	}
	declMap, err := d.GetUsersTotalDeclaredForMonths(largeUserBatch, []string{"2026-05"})
	if err != nil || declMap[uid1]["2026-05"] != 4.0 {
		t.Fatalf("chunked GetUsersTotalDeclaredForMonths failed: err=%v map=%+v", err, declMap[uid1])
	}

	largeProjBatch := make([]int64, 0, idBatchSize+15)
	for i := int64(0); i < int64(idBatchSize)+10; i++ {
		largeProjBatch = append(largeProjBatch, 200000+i)
	}
	largeProjBatch = append(largeProjBatch, pid1, pid2)
	projMap, err := d.GetProjectsByIDs(largeProjBatch)
	if err != nil || len(projMap) != 2 || !strings.Contains(projMap[pid1].TeamName, "Alpha") {
		t.Fatalf("chunked GetProjectsByIDs failed: err=%v map=%+v", err, projMap)
	}
}
