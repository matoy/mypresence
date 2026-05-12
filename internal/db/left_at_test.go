package db

import (
	"testing"
)

// -----------------------------------------------------------------------
// GetTeamMembers — active-only filter
// -----------------------------------------------------------------------

func TestGetTeamMembers_ExcludesDeparted(t *testing.T) {
	d := newTestDB(t)
	active := seedUser(t, d, "active_member@test.com")
	departed := seedUser(t, d, "departed_member@test.com")
	teamID, _ := d.CreateTeam("DepartureTeam1")
	d.AddTeamMember(teamID, active)   //nolint:errcheck
	d.AddTeamMember(teamID, departed) //nolint:errcheck

	leftAt := "2026-04-30"
	if err := d.SetTeamMemberLeftAt(teamID, departed, &leftAt); err != nil {
		t.Fatalf("SetTeamMemberLeftAt: %v", err)
	}

	members, err := d.GetTeamMembers(teamID)
	if err != nil {
		t.Fatalf("GetTeamMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 active member, got %d", len(members))
	}
	if members[0].ID != active {
		t.Errorf("expected active member ID %d, got %d", active, members[0].ID)
	}
}

// -----------------------------------------------------------------------
// GetAllTeamMembers — returns active + departed, active first
// -----------------------------------------------------------------------

func TestGetAllTeamMembers_ReturnsAll(t *testing.T) {
	d := newTestDB(t)
	active := seedUser(t, d, "all_active@test.com")
	departed := seedUser(t, d, "all_departed@test.com")
	teamID, _ := d.CreateTeam("AllMembersTeam")
	d.AddTeamMember(teamID, active)   //nolint:errcheck
	d.AddTeamMember(teamID, departed) //nolint:errcheck

	leftAt := "2026-03-31"
	d.SetTeamMemberLeftAt(teamID, departed, &leftAt) //nolint:errcheck

	members, err := d.GetAllTeamMembers(teamID)
	if err != nil {
		t.Fatalf("GetAllTeamMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members (active + departed), got %d", len(members))
	}
	// Active member should come first (ORDER BY left_at IS NULL DESC)
	if members[0].LeftAt != nil {
		t.Errorf("first member should be active (LeftAt=nil), got %v", members[0].LeftAt)
	}
	if members[1].LeftAt == nil {
		t.Errorf("second member should be departed (LeftAt non-nil)")
	}
	if *members[1].LeftAt != "2026-03-31" {
		t.Errorf("expected LeftAt=2026-03-31, got %q", *members[1].LeftAt)
	}
}

func TestGetAllTeamMembers_EmptyTeam(t *testing.T) {
	d := newTestDB(t)
	teamID, _ := d.CreateTeam("EmptyTeam")
	members, err := d.GetAllTeamMembers(teamID)
	if err != nil {
		t.Fatalf("GetAllTeamMembers empty: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("expected 0 members, got %d", len(members))
	}
}

// -----------------------------------------------------------------------
// GetTeamMembersAt — includes member who left during or after startDate
// -----------------------------------------------------------------------

func TestGetTeamMembersAt_IncludesDepartureMonth(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "departing@test.com")
	teamID, _ := d.CreateTeam("DepartureMonthTeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck

	// Left on May 15 — should be visible for May (startDate = 2026-05-01)
	leftAt := "2026-05-15"
	d.SetTeamMemberLeftAt(teamID, uid, &leftAt) //nolint:errcheck

	members, err := d.GetTeamMembersAt(teamID, "2026-05-01")
	if err != nil {
		t.Fatalf("GetTeamMembersAt: %v", err)
	}
	if len(members) != 1 || members[0].ID != uid {
		t.Errorf("expected member to appear in departure month, got %v", members)
	}
}

func TestGetTeamMembersAt_ExcludesBeforeDeparture(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "gone@test.com")
	teamID, _ := d.CreateTeam("GoneTeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck

	// Left in April — should NOT appear in June (startDate = 2026-06-01)
	leftAt := "2026-04-30"
	d.SetTeamMemberLeftAt(teamID, uid, &leftAt) //nolint:errcheck

	members, err := d.GetTeamMembersAt(teamID, "2026-06-01")
	if err != nil {
		t.Fatalf("GetTeamMembersAt: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("expected 0 members for month after departure, got %d", len(members))
	}
}

func TestGetTeamMembersAt_ActiveMemberAlwaysIncluded(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "stillhere@test.com")
	teamID, _ := d.CreateTeam("StillHereTeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck
	// No left_at set → always included

	for _, startDate := range []string{"2025-01-01", "2026-01-01", "2027-01-01"} {
		members, err := d.GetTeamMembersAt(teamID, startDate)
		if err != nil {
			t.Fatalf("GetTeamMembersAt(%s): %v", startDate, err)
		}
		if len(members) != 1 {
			t.Errorf("active member should appear for startDate=%s, got %d", startDate, len(members))
		}
	}
}

func TestGetTeamMembersAt_DepartureDateIsInclusive(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "exactdate@test.com")
	teamID, _ := d.CreateTeam("ExactDateTeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck

	leftAt := "2026-05-31"
	d.SetTeamMemberLeftAt(teamID, uid, &leftAt) //nolint:errcheck

	// startDate == left_at: member left on last day of month, so still visible
	members, err := d.GetTeamMembersAt(teamID, "2026-05-31")
	if err != nil {
		t.Fatalf("GetTeamMembersAt: %v", err)
	}
	if len(members) != 1 {
		t.Errorf("member should be included when startDate == left_at, got %d", len(members))
	}
}

// -----------------------------------------------------------------------
// SetTeamMemberLeftAt — set and clear
// -----------------------------------------------------------------------

func TestSetTeamMemberLeftAt_SetAndClear(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "setclear@test.com")
	teamID, _ := d.CreateTeam("SetClearTeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck

	// Set departure date
	leftAt := "2026-06-30"
	if err := d.SetTeamMemberLeftAt(teamID, uid, &leftAt); err != nil {
		t.Fatalf("SetTeamMemberLeftAt set: %v", err)
	}

	// Departed member should not appear in GetTeamMembers (active only)
	members, _ := d.GetTeamMembers(teamID)
	if len(members) != 0 {
		t.Errorf("departed member should not appear in active list, got %d", len(members))
	}

	// Clear departure date (reinstate)
	if err := d.SetTeamMemberLeftAt(teamID, uid, nil); err != nil {
		t.Fatalf("SetTeamMemberLeftAt clear: %v", err)
	}

	// Reinstated member should appear again
	members, _ = d.GetTeamMembers(teamID)
	if len(members) != 1 || members[0].ID != uid {
		t.Errorf("reinstated member should appear in active list, got %v", members)
	}
}

// -----------------------------------------------------------------------
// AddTeamMember — resets left_at when re-adding a departed member
// -----------------------------------------------------------------------

func TestAddTeamMember_ResetsDepartureDate(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "readd@test.com")
	teamID, _ := d.CreateTeam("ReAddTeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck

	// Record departure
	leftAt := "2026-03-01"
	d.SetTeamMemberLeftAt(teamID, uid, &leftAt) //nolint:errcheck

	// Departed member should not appear
	members, _ := d.GetTeamMembers(teamID)
	if len(members) != 0 {
		t.Fatal("member should be gone after departure")
	}

	// Re-add (AddTeamMember should reset left_at)
	if err := d.AddTeamMember(teamID, uid); err != nil {
		t.Fatalf("AddTeamMember re-add: %v", err)
	}

	members, _ = d.GetTeamMembers(teamID)
	if len(members) != 1 || members[0].ID != uid {
		t.Errorf("re-added member should be active, got %v", members)
	}
}

// -----------------------------------------------------------------------
// GetUserTeams — only active memberships (left_at IS NULL)
// -----------------------------------------------------------------------

func TestGetUserTeams_ExcludesDepartedTeams(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "userteams_depart@test.com")
	activeTeamID, _ := d.CreateTeam("ActiveTeamUT")
	departedTeamID, _ := d.CreateTeam("DepartedTeamUT")
	d.AddTeamMember(activeTeamID, uid)   //nolint:errcheck
	d.AddTeamMember(departedTeamID, uid) //nolint:errcheck

	leftAt := "2026-04-01"
	d.SetTeamMemberLeftAt(departedTeamID, uid, &leftAt) //nolint:errcheck

	teams, err := d.GetUserTeams(uid)
	if err != nil {
		t.Fatalf("GetUserTeams: %v", err)
	}
	if len(teams) != 1 {
		t.Fatalf("expected 1 active team, got %d: %v", len(teams), teams)
	}
	if teams[0].ID != activeTeamID {
		t.Errorf("expected active team ID %d, got %d", activeTeamID, teams[0].ID)
	}
}

// -----------------------------------------------------------------------
// GetTeamStats — uses GetTeamMembersAt so departed members are included
// -----------------------------------------------------------------------

func TestGetTeamStats_IncludesHistoricalMember(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "hist_member@test.com")
	sid := seedOnSiteStatus(t, d)
	teamID, _ := d.CreateTeam("HistoricalStatsTeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck

	// Declare presence in May
	d.presence.Exec("INSERT INTO presences (user_id, date, half, status_id) VALUES (?, '2026-05-15', 'full', ?)", uid, sid) //nolint:errcheck

	// Record departure at end of May
	leftAt := "2026-05-31"
	d.SetTeamMemberLeftAt(teamID, uid, &leftAt) //nolint:errcheck

	// Stats for May should include the departed member's presence
	stats, err := d.GetTeamStats(teamID, "2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatalf("GetTeamStats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat row for historical member, got %d", len(stats))
	}
	if stats[0].User.ID != uid {
		t.Errorf("expected stat for user %d, got %d", uid, stats[0].User.ID)
	}

	// Stats for June should NOT include this member (left before June starts)
	statsJune, err := d.GetTeamStats(teamID, "2026-06-01", "2026-06-30")
	if err != nil {
		t.Fatalf("GetTeamStats June: %v", err)
	}
	if len(statsJune) != 0 {
		t.Errorf("expected 0 stats for month after departure, got %d", len(statsJune))
	}
}

// -----------------------------------------------------------------------
// GetSeatsWithStatusForDates — multi-date status aggregation
// -----------------------------------------------------------------------

func TestGetSeatsWithStatusForDates_EmptyDates(t *testing.T) {
	d := newTestDB(t)
	fpID, _ := seedFloorplanAndSeat(t, d, "SFD1")

	seats, err := d.GetSeatsWithStatusForDates(fpID, 1, []string{}, "full")
	if err != nil {
		t.Fatalf("GetSeatsWithStatusForDates empty dates: %v", err)
	}
	if len(seats) != 1 {
		t.Fatalf("expected 1 seat, got %d", len(seats))
	}
	if seats[0].Status != "free" {
		t.Errorf("expected free status with no dates, got %q", seats[0].Status)
	}
}

func TestGetSeatsWithStatusForDates_Free(t *testing.T) {
	d := newTestDB(t)
	fpID, _ := seedFloorplanAndSeat(t, d, "SFD2")
	uid := seedUser(t, d, "sfd_free@test.com")

	seats, err := d.GetSeatsWithStatusForDates(fpID, uid, []string{"2026-05-01", "2026-05-02"}, "full")
	if err != nil {
		t.Fatalf("GetSeatsWithStatusForDates: %v", err)
	}
	if len(seats) != 1 || seats[0].Status != "free" {
		t.Errorf("expected free seat, got %v", seats)
	}
}

func TestGetSeatsWithStatusForDates_Mine(t *testing.T) {
	d := newTestDB(t)
	fpID, seatID := seedFloorplanAndSeat(t, d, "SFD3")
	uid := seedUser(t, d, "sfd_mine@test.com")

	d.floorplan.Exec("INSERT INTO seat_reservations (seat_id, user_id, date, half) VALUES (?, ?, '2026-05-01', 'full')", seatID, uid) //nolint:errcheck

	seats, err := d.GetSeatsWithStatusForDates(fpID, uid, []string{"2026-05-01", "2026-05-02"}, "full")
	if err != nil {
		t.Fatalf("GetSeatsWithStatusForDates: %v", err)
	}
	if len(seats) != 1 || seats[0].Status != "mine" {
		t.Errorf("expected mine status, got %v", seats)
	}
	// Note: GetSeatsWithStatusForDates does not populate ReservationID (multi-date aggregation).
}

func TestGetSeatsWithStatusForDates_Taken(t *testing.T) {
	d := newTestDB(t)
	fpID, seatID := seedFloorplanAndSeat(t, d, "SFD4")
	alice := seedUser(t, d, "sfd_alice@test.com")
	bob := seedUser(t, d, "sfd_bob@test.com")

	// Alice reserves the seat
	d.floorplan.Exec("INSERT INTO seat_reservations (seat_id, user_id, date, half) VALUES (?, ?, '2026-05-01', 'full')", seatID, alice) //nolint:errcheck

	// Bob queries: seat should be "taken"
	seats, err := d.GetSeatsWithStatusForDates(fpID, bob, []string{"2026-05-01", "2026-05-02"}, "full")
	if err != nil {
		t.Fatalf("GetSeatsWithStatusForDates: %v", err)
	}
	if len(seats) != 1 || seats[0].Status != "taken" {
		t.Errorf("expected taken status, got %v", seats)
	}
	if seats[0].ReservationID != 0 {
		t.Errorf("reservation_id should be 0 for taken-by-someone-else")
	}
}

func TestGetSeatsWithStatusForDates_MineBeatsEmpty(t *testing.T) {
	d := newTestDB(t)
	fpID, seatID := seedFloorplanAndSeat(t, d, "SFD5")
	uid := seedUser(t, d, "sfd_mineempty@test.com")

	// Reserved on date 1 only, date 2 is free
	d.floorplan.Exec("INSERT INTO seat_reservations (seat_id, user_id, date, half) VALUES (?, ?, '2026-05-01', 'full')", seatID, uid) //nolint:errcheck

	seats, err := d.GetSeatsWithStatusForDates(fpID, uid, []string{"2026-05-01", "2026-05-02"}, "full")
	if err != nil {
		t.Fatalf("GetSeatsWithStatusForDates: %v", err)
	}
	// Status should be "mine" (has a reservation on at least one date)
	if len(seats) != 1 || seats[0].Status != "mine" {
		t.Errorf("expected mine status when reserved on at least one date, got %v", seats)
	}
}
