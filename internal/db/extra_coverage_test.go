package db

import (
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/models"
)

func TestExtraDB_Domains_Coverage(t *testing.T) {
	d := newTestDB(t)

	u1 := seedUser(t, d, "domain_user1@example.com")
	u2 := seedUser(t, d, "domain_user2@example.com")

	// Create domain
	domID, err := d.CreateDomain("Product & Tech")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}

	// GetDomain & GetDomainName
	dm, err := d.GetDomain(domID)
	if err != nil || dm.Name != "Product & Tech" {
		t.Fatalf("GetDomain: %v, %+v", err, dm)
	}
	if name := d.GetDomainName(domID); name != "Product & Tech" {
		t.Errorf("GetDomainName: %q", name)
	}
	if name := d.GetDomainName(99999); name != "" {
		t.Errorf("GetDomainName for unknown ID expected empty, got %q", name)
	}

	// SetDomainManagers
	if err := d.SetDomainManagers(domID, []int64{u1, u2}); err != nil {
		t.Fatalf("SetDomainManagers: %v", err)
	}
	managers, err := d.ListDomainManagers(domID)
	if err != nil || len(managers) != 2 {
		t.Fatalf("ListDomainManagers: %v, count %d", err, len(managers))
	}

	// IsDomainManager
	isMgr1, err := d.IsDomainManager(u1)
	if err != nil || !isMgr1 {
		t.Errorf("expected u1 to be domain manager, got %v, err %v", isMgr1, err)
	}
	u3 := seedUser(t, d, "non_manager@example.com")
	isMgr3, err := d.IsDomainManager(u3)
	if err != nil || isMgr3 {
		t.Errorf("expected u3 not to be domain manager, got %v, err %v", isMgr3, err)
	}

	// GetUserDomains
	userDoms, err := d.GetUserDomains(u1)
	if err != nil || len(userDoms) != 1 || userDoms[0].ID != domID {
		t.Fatalf("GetUserDomains: %v, %+v", err, userDoms)
	}

	// Create Team & Assign to domain
	tID, err := d.CreateTeamWithDetails("Web Team", "", false)
	if err != nil {
		t.Fatalf("CreateTeamWithDetails: %v", err)
	}
	if err := d.UpdateTeamDomain(tID, domID); err != nil {
		t.Fatalf("UpdateTeamDomain: %v", err)
	}

	// ListTeamsForDomain
	domTeams, err := d.ListTeamsForDomain(domID)
	if err != nil || len(domTeams) != 1 || domTeams[0].ID != tID {
		t.Fatalf("ListTeamsForDomain: %v, %+v", err, domTeams)
	}

	// ListDomainsWithTeams
	domsWithTeams, err := d.ListDomainsWithTeams()
	if err != nil || len(domsWithTeams) != 1 || len(domsWithTeams[0].Teams) != 1 {
		t.Fatalf("ListDomainsWithTeams: %v, %+v", err, domsWithTeams)
	}

	// DeleteDomain
	if err := d.DeleteDomain(domID); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}
	domTeamsAfter, _ := d.ListTeamsForDomain(domID)
	if len(domTeamsAfter) != 0 {
		t.Errorf("expected 0 teams after domain delete, got %d", len(domTeamsAfter))
	}
}

func TestExtraDB_News_Coverage(t *testing.T) {
	d := newTestDB(t)

	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")

	// Create active non-recurring news
	nID1, err := d.CreateNewsMessage("Notice 1", "Message body", yesterday, tomorrow, "#3b82f6", false)
	if err != nil {
		t.Fatalf("CreateNewsMessage: %v", err)
	}

	// Create active recurring news
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	endOfMonth := time.Date(now.Year(), now.Month(), 28, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	nID2, err := d.CreateNewsMessage("Recurring Notice", "Recurring body", startOfMonth, endOfMonth, "#dc2626", true)
	if err != nil {
		t.Fatalf("CreateNewsMessage recurring: %v", err)
	}

	// UpdateNewsMessage
	if err := d.UpdateNewsMessage(nID1, "Updated Notice", "Updated body", today, tomorrow, "#10b981", false); err != nil {
		t.Fatalf("UpdateNewsMessage: %v", err)
	}

	// ListNewsMessages
	allNews, err := d.ListNewsMessages()
	if err != nil || len(allNews) != 2 {
		t.Fatalf("ListNewsMessages: %v, count %d", err, len(allNews))
	}

	// GetActiveNewsMessages
	activeNews, err := d.GetActiveNewsMessages()
	if err != nil || len(activeNews) < 1 {
		t.Fatalf("GetActiveNewsMessages: %v, count %d", err, len(activeNews))
	}

	// DeleteNewsMessage
	if err := d.DeleteNewsMessage(nID1); err != nil {
		t.Fatalf("DeleteNewsMessage: %v", err)
	}
	if err := d.DeleteNewsMessage(nID2); err != nil {
		t.Fatalf("DeleteNewsMessage: %v", err)
	}
}

func TestExtraDB_ProjectsAndActivities_Coverage(t *testing.T) {
	d := newTestDB(t)
	uID := seedUser(t, d, "proj_act_user@example.com")

	pID, err := d.CreateProject("Alpha Project", "ALPHA", 0, true, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// UpdateProjectWithDetails
	if err := d.UpdateProjectWithDetails(pID, "Alpha Plus", "AP", 0, true, "2026-01-01", "2026-12-31", false, "2026-12-31"); err != nil {
		t.Fatalf("UpdateProjectWithDetails: %v", err)
	}

	// SetProjectMembers & GetProjectMembers & IsUserAssignedToProject
	if err := d.SetProjectMembers(pID, []int64{uID}); err != nil {
		t.Fatalf("SetProjectMembers: %v", err)
	}
	members, err := d.GetProjectMembers(pID)
	if err != nil || len(members) != 1 || members[0] != uID {
		t.Fatalf("GetProjectMembers: %v, %+v", err, members)
	}
	assigned, err := d.IsUserAssignedToProject(pID, uID)
	if err != nil || !assigned {
		t.Errorf("expected user to be assigned to project: %v, %v", assigned, err)
	}

	// ToggleProjectFavorite & GetUserFavoriteProjectIDs
	if _, err := d.ToggleProjectFavorite(uID, pID); err != nil {
		t.Fatalf("ToggleProjectFavorite: %v", err)
	}
	favs, err := d.GetUserFavoriteProjectIDs(uID)
	if err != nil || len(favs) != 1 || favs[0] != pID {
		t.Fatalf("GetUserFavoriteProjectIDs: %v, %+v", err, favs)
	}
	// Untoggle
	if _, err := d.ToggleProjectFavorite(uID, pID); err != nil {
		t.Fatalf("ToggleProjectFavorite untoggle: %v", err)
	}

	// ListActiveProjectsForMonthAndUser
	activeProjs, err := d.ListActiveProjectsForMonthAndUser(2026, 8, uID)
	if err != nil || len(activeProjs) != 1 {
		t.Fatalf("ListActiveProjectsForMonthAndUser: %v, count %d", err, len(activeProjs))
	}

	// Project Activities
	sID := seedOnSiteStatus(t, d)
	date := "2026-08-18"
	_ = d.SetPresences(uID, []string{date}, sID, "full")

	weight, err := d.GetUserBillableWeightForDate(uID, date)
	if err != nil || weight != 1.0 {
		t.Fatalf("GetUserBillableWeightForDate: %v, weight %v", err, weight)
	}

	actID, err := d.CreateProjectActivity(uID, date, models.ActivityTypeOther, "", "", "Backend API", 60)
	if err != nil {
		t.Fatalf("CreateProjectActivity: %v", err)
	}

	tot, err := d.GetUserActivitiesTotalForDate(uID, date, 0)
	if err != nil || tot != 60 {
		t.Fatalf("GetUserActivitiesTotalForDate: %v, tot %v", err, tot)
	}

	// Total excluding this ID
	totEx, err := d.GetUserActivitiesTotalForDate(uID, date, actID)
	if err != nil || totEx != 0 {
		t.Fatalf("GetUserActivitiesTotalForDate excluding actID: %v, tot %v", err, totEx)
	}

	billableDates, err := d.GetUserBillableDatesForMonth(uID, 2026, 8)
	if err != nil || len(billableDates) != 1 || billableDates[date] != 1.0 {
		t.Fatalf("GetUserBillableDatesForMonth: %v, %+v", err, billableDates)
	}
}

func TestExtraDB_PresencesAndUser_Branches(t *testing.T) {
	d := newTestDB(t)

	uID := seedUser(t, d, "pres_user@example.com")
	sID := seedOnSiteStatus(t, d)

	// 1. SetPresences invalid half
	err := d.SetPresences(uID, []string{"2026-08-01"}, sID, "invalid")
	if err == nil {
		t.Errorf("expected error on invalid half")
	}

	// 2. SetPresences half == "" (defaults to full)
	if err := d.SetPresences(uID, []string{"2026-08-01"}, sID, ""); err != nil {
		t.Errorf("SetPresences empty half: %v", err)
	}

	// 3. SetPresences half == "AM" and "PM"
	if err := d.SetPresences(uID, []string{"2026-08-01"}, sID, "AM"); err != nil {
		t.Errorf("SetPresences AM: %v", err)
	}
	if err := d.SetPresences(uID, []string{"2026-08-01"}, sID, "PM"); err != nil {
		t.Errorf("SetPresences PM: %v", err)
	}

	// 4. LogPresenceAction with set and clear
	if err := d.LogPresenceAction(uID, uID, "set", []string{"2026-08-01"}, sID, ""); err != nil {
		t.Errorf("LogPresenceAction set: %v", err)
	}
	if err := d.LogPresenceAction(uID, uID, "clear", []string{"2026-08-01"}, 0, "AM"); err != nil {
		t.Errorf("LogPresenceAction clear AM: %v", err)
	}

	// 5. UpsertUser update path
	user1, err := d.UpsertUser("pres_user@example.com", "Pres User Updated")
	if err != nil || user1.Name != "Pres User Updated" {
		t.Fatalf("UpsertUser existing: %v, %+v", err, user1)
	}

	// 6. SetUserPassword & CheckPassword
	if err := d.SetUserPassword(uID, "new-secret-password"); err != nil {
		t.Fatalf("SetUserPassword: %v", err)
	}
	u, _ := d.GetUserByID(uID)
	if !d.CheckPassword(uID, u.PasswordHash, "new-secret-password") {
		t.Errorf("CheckPassword expected true for correct password")
	}
	if d.CheckPassword(uID, u.PasswordHash, "wrong-password") {
		t.Errorf("CheckPassword expected false for wrong password")
	}

	// 7. SeedDefaults idempotency
	if err := d.SeedDefaults("admin@test.com", "adminpass"); err != nil {
		t.Errorf("SeedDefaults second call: %v", err)
	}
}
