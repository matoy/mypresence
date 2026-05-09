package db

import (
	"testing"
	"time"

	"presence-app/internal/models"
)

// -----------------------------------------------------------------------
// GetTeamStats
// -----------------------------------------------------------------------

func TestGetTeamStats_EmptyTeam(t *testing.T) {
	d := newTestDB(t)
	teamID, err := d.CreateTeam("Stats Team")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	stats, err := d.GetTeamStats(teamID, "2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatalf("GetTeamStats: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %d", len(stats))
	}
}

func TestGetTeamStats_WithPresences(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(4)

	teamID, _ := d.CreateTeam("Stats Team 2")
	userID := seedUser(t, d, "stat@test.com")
	_ = d.AddTeamMember(teamID, userID)

	statusID := seedOnSiteStatus(t, d)
	_ = d.SetPresences(userID, []string{"2026-05-05"}, statusID, "full")

	stats, err := d.GetTeamStats(teamID, "2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatalf("GetTeamStats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat row, got %d", len(stats))
	}
	if stats[0].BillableDays != 1 {
		t.Errorf("expected 1 billable day, got %v", stats[0].BillableDays)
	}
}

// -----------------------------------------------------------------------
// LogPresenceAction / GetUserLogs
// -----------------------------------------------------------------------

func TestLogPresenceAction_SetAndClear(t *testing.T) {
	d := newTestDB(t)
	actorID := seedUser(t, d, "actor@test.com")
	userID := seedUser(t, d, "user@test.com")
	statusID := seedOnSiteStatus(t, d)

	err := d.LogPresenceAction(actorID, userID, "set", []string{"2026-05-05", "2026-05-06"}, statusID, "full")
	if err != nil {
		t.Fatalf("LogPresenceAction set: %v", err)
	}

	err = d.LogPresenceAction(actorID, userID, "clear", []string{"2026-05-05"}, 0, "AM")
	if err != nil {
		t.Fatalf("LogPresenceAction clear: %v", err)
	}
}

func TestGetUserLogs_Empty(t *testing.T) {
	d := newTestDB(t)
	logs, err := d.GetUserLogs(999, time.Time{})
	if err != nil {
		t.Fatalf("GetUserLogs: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("expected empty logs, got %d", len(logs))
	}
}

func TestGetUserLogs_WithData(t *testing.T) {
	d := newTestDB(t)
	actorID := seedUser(t, d, "actor2@test.com")
	userID := seedUser(t, d, "user2@test.com")
	statusID := seedOnSiteStatus(t, d)

	_ = d.LogPresenceAction(actorID, userID, "set", []string{"2026-05-07"}, statusID, "full")

	logs, err := d.GetUserLogs(userID, time.Time{})
	if err != nil {
		t.Fatalf("GetUserLogs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("expected at least 1 log entry")
	}
	if logs[0].Date != "2026-05-07" {
		t.Errorf("expected date 2026-05-07, got %s", logs[0].Date)
	}
}

func TestGetUserLogs_WithSince(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "usersince@test.com")

	// Filter with a future since → no results
	logs, err := d.GetUserLogs(userID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("GetUserLogs with since: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("expected empty after since filter, got %d", len(logs))
	}
}

// -----------------------------------------------------------------------
// GetTeamName / GetStatusName / GetHolidayName
// -----------------------------------------------------------------------

func TestGetTeamName(t *testing.T) {
	d := newTestDB(t)
	id, _ := d.CreateTeam("My Team Name")
	name := d.GetTeamName(id)
	if name != "My Team Name" {
		t.Errorf("GetTeamName = %q, want %q", name, "My Team Name")
	}
	if got := d.GetTeamName(99999); got != "" {
		t.Errorf("GetTeamName(unknown) = %q, want empty", got)
	}
}

func TestGetStatusName(t *testing.T) {
	d := newTestDB(t)
	id, _ := d.CreateStatus(models.Status{Name: "Télétravail", Color: "#ff0000", Billable: true, SortOrder: 1})
	name := d.GetStatusName(id)
	if name != "Télétravail" {
		t.Errorf("GetStatusName = %q, want %q", name, "Télétravail")
	}
	if got := d.GetStatusName(99999); got != "" {
		t.Errorf("GetStatusName(unknown) = %q, want empty", got)
	}
}

func TestGetHolidayName(t *testing.T) {
	d := newTestDB(t)
	id, _ := d.CreateHoliday("2026-07-14", "Fête nationale", false)
	name := d.GetHolidayName(id)
	if name != "Fête nationale" {
		t.Errorf("GetHolidayName = %q, want %q", name, "Fête nationale")
	}
	if got := d.GetHolidayName(99999); got != "" {
		t.Errorf("GetHolidayName(unknown) = %q, want empty", got)
	}
}

// -----------------------------------------------------------------------
// Floorplan CRUD
// -----------------------------------------------------------------------

func TestListFloorplans(t *testing.T) {
	d := newTestDB(t)
	fps, err := d.ListFloorplans()
	if err != nil {
		t.Fatalf("ListFloorplans: %v", err)
	}
	if len(fps) != 0 {
		t.Errorf("expected empty, got %d", len(fps))
	}
}

func TestCreateFloorplan(t *testing.T) {
	d := newTestDB(t)
	id, err := d.CreateFloorplan("Floor 1", 0)
	if err != nil {
		t.Fatalf("CreateFloorplan: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero ID")
	}
	fps, _ := d.ListFloorplans()
	if len(fps) != 1 || fps[0].Name != "Floor 1" {
		t.Errorf("unexpected floorplans: %v", fps)
	}
}

func TestUpdateFloorplan(t *testing.T) {
	d := newTestDB(t)
	id, _ := d.CreateFloorplan("Old Name", 0)
	if err := d.UpdateFloorplan(id, "New Name", 1); err != nil {
		t.Fatalf("UpdateFloorplan: %v", err)
	}
	fps, _ := d.ListFloorplans()
	if fps[0].Name != "New Name" {
		t.Errorf("expected New Name, got %s", fps[0].Name)
	}
}

func TestDeleteFloorplan(t *testing.T) {
	d := newTestDB(t)
	id, _ := d.CreateFloorplan("To Delete", 0)
	if err := d.DeleteFloorplan(id); err != nil {
		t.Fatalf("DeleteFloorplan: %v", err)
	}
	fps, _ := d.ListFloorplans()
	if len(fps) != 0 {
		t.Errorf("expected empty after delete, got %d", len(fps))
	}
}

// -----------------------------------------------------------------------
// Seat CRUD
// -----------------------------------------------------------------------

func TestCreateSeat(t *testing.T) {
	d := newTestDB(t)
	fpID, _ := d.CreateFloorplan("FP for seat", 0)
	id, err := d.CreateSeat(fpID, "A1", 10.0, 20.0)
	if err != nil {
		t.Fatalf("CreateSeat: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero seat ID")
	}
}

func TestUpdateSeat(t *testing.T) {
	d := newTestDB(t)
	fpID, seatID := seedFloorplanAndSeat(t, d, "B1")
	if err := d.UpdateSeat(seatID, "B2", 30.0, 40.0); err != nil {
		t.Fatalf("UpdateSeat: %v", err)
	}
	seats, _ := d.ListSeats(fpID)
	if len(seats) == 0 || seats[0].Label != "B2" {
		t.Errorf("expected label B2, got %v", seats)
	}
}

func TestDeleteSeat(t *testing.T) {
	d := newTestDB(t)
	fpID, seatID := seedFloorplanAndSeat(t, d, "C1")
	if err := d.DeleteSeat(seatID); err != nil {
		t.Fatalf("DeleteSeat: %v", err)
	}
	seats, _ := d.ListSeats(fpID)
	if len(seats) != 0 {
		t.Errorf("expected no seats after delete, got %d", len(seats))
	}
}

// -----------------------------------------------------------------------
// CancelReservation
// -----------------------------------------------------------------------

func TestCancelReservation(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "cancel@test.com")
	_, seatID := seedFloorplanAndSeat(t, d, "D1")

	// Reserve first
	if err := d.ReserveSeat(seatID, userID, "2026-05-10", "full"); err != nil {
		t.Fatalf("ReserveSeat: %v", err)
	}
	// Fetch the reservation ID
	var resID int64
	d.floorplan.QueryRow("SELECT id FROM seat_reservations WHERE seat_id = ? AND user_id = ?", seatID, userID).Scan(&resID) //nolint:errcheck
	if resID == 0 {
		t.Fatal("expected non-zero reservation ID")
	}
	// Then cancel
	if err := d.CancelReservation(resID, userID); err != nil {
		t.Fatalf("CancelReservation: %v", err)
	}
}

// -----------------------------------------------------------------------
// ListUserPATs (37.5% → cover more branches)
// -----------------------------------------------------------------------

func TestListUserPATs_WithExpiry(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(4)
	userID := seedUser(t, d, "pat@test.com")

	// PAT with expiry
	expires := time.Now().Add(time.Hour)
	_, _, err := d.CreatePAT(userID, "test pat", &expires)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	// PAT without expiry
	_, _, err = d.CreatePAT(userID, "no-expiry", nil)
	if err != nil {
		t.Fatalf("CreatePAT no-expiry: %v", err)
	}

	pats, err := d.ListUserPATs(userID)
	if err != nil {
		t.Fatalf("ListUserPATs: %v", err)
	}
	if len(pats) != 2 {
		t.Errorf("expected 2 PATs, got %d", len(pats))
	}
	// Verify expiry is populated for the first
	var withExpiry, withoutExpiry int
	for _, p := range pats {
		if p.ExpiresAt != nil {
			withExpiry++
		} else {
			withoutExpiry++
		}
	}
	if withExpiry != 1 || withoutExpiry != 1 {
		t.Errorf("expected 1 with expiry and 1 without, got %d/%d", withExpiry, withoutExpiry)
	}
}

// -----------------------------------------------------------------------
// GetUserBillableDaysForMonth (from projects.go)
// -----------------------------------------------------------------------

func TestGetUserBillableDaysForMonth(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "billable@test.com")
	statusID := seedOnSiteStatus(t, d)

	_ = d.SetPresences(userID, []string{"2026-05-05", "2026-05-06"}, statusID, "full")

	days, err := d.GetUserBillableDaysForMonth(userID, 2026, 5)
	if err != nil {
		t.Fatalf("GetUserBillableDaysForMonth: %v", err)
	}
	if days != 2.0 {
		t.Errorf("expected 2.0 billable days, got %v", days)
	}
}

// -----------------------------------------------------------------------
// GetTeamIDsForUser (from projects.go)
// -----------------------------------------------------------------------

func TestGetTeamIDsForUser(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "teamids@test.com")

	// No teams yet
	ids, err := d.GetTeamIDsForUser(userID)
	if err != nil {
		t.Fatalf("GetTeamIDsForUser: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 team IDs, got %d", len(ids))
	}

	// Add to a team
	teamID, _ := d.CreateTeam("ID Team")
	_ = d.AddTeamMember(teamID, userID)

	ids, err = d.GetTeamIDsForUser(userID)
	if err != nil {
		t.Fatalf("GetTeamIDsForUser with team: %v", err)
	}
	if len(ids) != 1 || ids[0] != teamID {
		t.Errorf("expected [%d], got %v", teamID, ids)
	}
}
