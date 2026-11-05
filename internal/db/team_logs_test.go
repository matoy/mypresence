package db

import (
	"testing"
	"time"
)

func TestGetTeamLogs_EmptyOrNoMembers(t *testing.T) {
	d := newTestDB(t)

	// Non-existent team
	logs, err := d.GetTeamLogs(9999, 0, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error for non-existent team: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected 0 logs, got %d", len(logs))
	}

	// Empty team (no members)
	teamID, err := d.CreateTeamWithDetails("Empty Team", "", false, false)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	logs, err = d.GetTeamLogs(teamID, 0, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error for empty team: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected 0 logs, got %d", len(logs))
	}
}

func TestGetTeamLogs_WithMembersAndActions(t *testing.T) {
	d := newTestDB(t)

	leaderID, _ := d.CreateLocalUser("leader@example.com", "Leader Lois", "pass")
	member1ID, _ := d.CreateLocalUser("member1@example.com", "Member One", "pass")
	member2ID, _ := d.CreateLocalUser("member2@example.com", "Member Two", "pass")
	outsiderID, _ := d.CreateLocalUser("outsider@example.com", "Outsider", "pass")

	teamID, _ := d.CreateTeamWithDetails("Dev Team", "", false, false)
	_ = d.AddTeamMember(teamID, leaderID)
	_ = d.SetTeamLeaders(teamID, []int64{leaderID})
	_ = d.AddTeamMember(teamID, member1ID)
	_ = d.AddTeamMember(teamID, member2ID)

	statusID := seedOnSiteStatus(t, d)

	// Member 1 self-declares
	_ = d.LogPresenceAction(member1ID, member1ID, "set", []string{"2026-06-01"}, statusID, "full")
	// Leader declares on behalf of Member 2
	_ = d.LogPresenceAction(leaderID, member2ID, "set", []string{"2026-06-02"}, statusID, "AM")
	// Outsider logs presence (not in team)
	_ = d.LogPresenceAction(outsiderID, outsiderID, "set", []string{"2026-06-03"}, statusID, "full")

	// Get all team logs
	logs, err := d.GetTeamLogs(teamID, 0, time.Time{})
	if err != nil {
		t.Fatalf("GetTeamLogs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs for team, got %d", len(logs))
	}

	// Verify user names and actor names are populated
	userNames := map[int64]string{}
	actorNames := map[int64]string{}
	for _, l := range logs {
		userNames[l.UserID] = l.UserName
		actorNames[l.ActorID] = l.ActorName
	}
	if userNames[member1ID] != "Member One" {
		t.Errorf("expected Member One, got %q", userNames[member1ID])
	}
	if userNames[member2ID] != "Member Two" {
		t.Errorf("expected Member Two, got %q", userNames[member2ID])
	}
	if actorNames[leaderID] != "Leader Lois" {
		t.Errorf("expected Leader Lois, got %q", actorNames[leaderID])
	}

	// Filter by member 1
	m1Logs, err := d.GetTeamLogs(teamID, member1ID, time.Time{})
	if err != nil {
		t.Fatalf("GetTeamLogs with memberID: %v", err)
	}
	if len(m1Logs) != 1 || m1Logs[0].UserID != member1ID {
		t.Fatalf("expected 1 log for member 1, got %d", len(m1Logs))
	}

	// Filter with since in future -> 0 logs
	futureLogs, err := d.GetTeamLogs(teamID, 0, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("GetTeamLogs with future since: %v", err)
	}
	if len(futureLogs) != 0 {
		t.Fatalf("expected 0 logs with future since, got %d", len(futureLogs))
	}
}

