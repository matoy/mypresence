package db

import (
	"testing"
	"time"

	"presence-app/internal/config"
)

// newTestDB opens an isolated in-memory-style SQLite DB in a temp directory.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedUser inserts a minimal user and returns its id.
func seedUser(t *testing.T, db *DB, email string) int64 {
	t.Helper()
	res, err := db.core.Exec(
		"INSERT INTO users (email, name, role) VALUES (?, ?, 'basic')", email, email,
	)
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedOnSiteStatus inserts a status with on_site=1 and returns its id.
func seedOnSiteStatus(t *testing.T, db *DB) int64 {
	t.Helper()
	res, err := db.presence.Exec(
		"INSERT INTO statuses (name, color, billable, on_site, sort_order) VALUES ('Présent', '#22c55e', 1, 1, 1)",
	)
	if err != nil {
		t.Fatalf("seedOnSiteStatus: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedFloorplanAndSeat inserts a floorplan + one seat, returns (floorplanID, seatID).
func seedFloorplanAndSeat(t *testing.T, db *DB, label string) (int64, int64) {
	t.Helper()
	fpRes, err := db.floorplan.Exec("INSERT INTO floorplans (name) VALUES ('Test FP')")
	if err != nil {
		t.Fatalf("seedFloorplan: %v", err)
	}
	fpID, _ := fpRes.LastInsertId()

	sRes, err := db.floorplan.Exec(
		"INSERT INTO seats (floorplan_id, label, x_pct, y_pct) VALUES (?, ?, 50, 50)", fpID, label,
	)
	if err != nil {
		t.Fatalf("seedSeat: %v", err)
	}
	seatID, _ := sRes.LastInsertId()
	return fpID, seatID
}

// -----------------------------------------------------------------------
// GetUserReservationDates
// -----------------------------------------------------------------------

func TestGetUserReservationDates_Empty(t *testing.T) {
	db := newTestDB(t)
	m, err := db.GetUserReservationDates(1, "2026-04-01", "2026-04-30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestGetUserReservationDates_WithReservations(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "alice@test.com")
	_, seatID := seedFloorplanAndSeat(t, d, "A1")

	dates := []string{"2026-04-14", "2026-04-15", "2026-04-16"}
	for _, date := range dates {
		_, err := d.floorplan.Exec(
			"INSERT INTO seat_reservations (seat_id, user_id, date, half) VALUES (?, ?, ?, 'full')",
			seatID, userID, date,
		)
		if err != nil {
			t.Fatalf("insert reservation %s: %v", date, err)
		}
	}

	// Full range — should find all 3
	m, err := d.GetUserReservationDates(userID, "2026-04-01", "2026-04-30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range dates {
		if !m[want] {
			t.Errorf("expected date %s in result, got %v", want, m)
		}
	}

	// Narrower range — only first two
	m2, err := d.GetUserReservationDates(userID, "2026-04-14", "2026-04-15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m2) != 2 {
		t.Errorf("expected 2 dates in narrow range, got %d: %v", len(m2), m2)
	}
	if m2["2026-04-16"] {
		t.Error("2026-04-16 should be outside the query range")
	}
}

func TestGetUserReservationDates_OtherUserIsolation(t *testing.T) {
	d := newTestDB(t)
	alice := seedUser(t, d, "alice@test.com")
	bob := seedUser(t, d, "bob@test.com")
	_, seatID := seedFloorplanAndSeat(t, d, "B1")

	// Only Bob has a reservation
	d.floorplan.Exec("INSERT INTO seat_reservations (seat_id, user_id, date, half) VALUES (?, ?, '2026-04-14', 'full')", seatID, bob) //nolint:errcheck

	m, err := d.GetUserReservationDates(alice, "2026-04-01", "2026-04-30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("Alice should have no reservations, got %v", m)
	}
}

// -----------------------------------------------------------------------
// BulkReserveSeat
// -----------------------------------------------------------------------

func TestBulkReserveSeat_SkipsWhenNotOnSite(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "user@test.com")
	_, seatID := seedFloorplanAndSeat(t, d, "C1")

	// No on-site presence → should book 0
	count := d.BulkReserveSeat(seatID, userID, []string{"2026-04-14", "2026-04-15"}, "full")
	if count != 0 {
		t.Errorf("expected 0 bookings (no on-site presence), got %d", count)
	}
}

func TestBulkReserveSeat_SuccessWhenOnSite(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "user2@test.com")
	statusID := seedOnSiteStatus(t, d)
	_, seatID := seedFloorplanAndSeat(t, d, "D1")

	// Declare on-site on two dates
	d.presence.Exec("INSERT INTO presences (user_id, date, half, status_id) VALUES (?, '2026-04-14', 'full', ?)", userID, statusID) //nolint:errcheck
	d.presence.Exec("INSERT INTO presences (user_id, date, half, status_id) VALUES (?, '2026-04-15', 'full', ?)", userID, statusID) //nolint:errcheck

	count := d.BulkReserveSeat(seatID, userID, []string{"2026-04-14", "2026-04-15", "2026-04-16"}, "full")
	if count != 2 {
		t.Errorf("expected 2 bookings, got %d", count)
	}
}

func TestBulkReserveSeat_SkipsTakenSeat(t *testing.T) {
	d := newTestDB(t)
	alice := seedUser(t, d, "alice2@test.com")
	bob := seedUser(t, d, "bob2@test.com")
	statusID := seedOnSiteStatus(t, d)
	_, seatID := seedFloorplanAndSeat(t, d, "E1")

	// Both on site
	d.presence.Exec("INSERT INTO presences (user_id, date, half, status_id) VALUES (?, '2026-04-14', 'full', ?)", alice, statusID) //nolint:errcheck
	d.presence.Exec("INSERT INTO presences (user_id, date, half, status_id) VALUES (?, '2026-04-14', 'full', ?)", bob, statusID)   //nolint:errcheck

	// Alice books first
	d.floorplan.Exec("INSERT INTO seat_reservations (seat_id, user_id, date, half) VALUES (?, ?, '2026-04-14', 'full')", seatID, alice) //nolint:errcheck

	// Bob tries to bulk-reserve the same seat/date — should be skipped (conflict)
	count := d.BulkReserveSeat(seatID, bob, []string{"2026-04-14"}, "full")
	if count != 0 {
		t.Errorf("expected 0 (seat taken), got %d", count)
	}
}

// -----------------------------------------------------------------------
// CancelUserReservationsForDates
// -----------------------------------------------------------------------

func TestCancelUserReservationsForDates_Empty(t *testing.T) {
	d := newTestDB(t)
	// Should be a no-op, not an error
	if err := d.CancelUserReservationsForDates(1, []string{}); err != nil {
		t.Errorf("unexpected error for empty dates: %v", err)
	}
}

func TestCancelUserReservationsForDates_RemovesOwn(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "usr@test.com")
	_, seatID := seedFloorplanAndSeat(t, d, "F1")

	dates := []string{"2026-04-14", "2026-04-15"}
	for _, date := range dates {
		d.floorplan.Exec("INSERT INTO seat_reservations (seat_id, user_id, date, half) VALUES (?, ?, ?, 'full')", seatID, userID, date) //nolint:errcheck
	}

	if err := d.CancelUserReservationsForDates(userID, dates); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, _ := d.GetUserReservationDates(userID, "2026-04-01", "2026-04-30")
	if len(m) != 0 {
		t.Errorf("expected reservations to be deleted, still have %v", m)
	}
}

func TestCancelUserReservationsForDates_PreservesOtherUser(t *testing.T) {
	d := newTestDB(t)
	alice := seedUser(t, d, "alice3@test.com")
	bob := seedUser(t, d, "bob3@test.com")
	_, seatID := seedFloorplanAndSeat(t, d, "G1")

	d.floorplan.Exec("INSERT INTO seat_reservations (seat_id, user_id, date, half) VALUES (?, ?, '2026-04-14', 'full')", seatID, alice) //nolint:errcheck
	d.floorplan.Exec("INSERT INTO seat_reservations (seat_id, user_id, date, half) VALUES (?, ?, '2026-04-15', 'full')", seatID, bob)   //nolint:errcheck

	// Cancel only alice's dates
	d.CancelUserReservationsForDates(alice, []string{"2026-04-14"}) //nolint:errcheck

	// Bob's reservation on 2026-04-15 should remain
	m, _ := d.GetUserReservationDates(bob, "2026-04-01", "2026-04-30")
	if !m["2026-04-15"] {
		t.Error("bob's reservation should not have been deleted")
	}
}

// -----------------------------------------------------------------------
// Counts
// -----------------------------------------------------------------------

func TestCounts_ReturnsNonZeroAfterSeed(t *testing.T) {
	d := newTestDB(t)

	// Seed explicit data so Counts is non-zero
	seedUser(t, d, "count_user@test.com")
	seedOnSiteStatus(t, d)

	c := d.Counts()
	if c.Users == 0 {
		t.Error("expected at least 1 user")
	}
	if c.Statuses == 0 {
		t.Error("expected at least 1 status")
	}
}

func TestCounts_IncrementsOnInsert(t *testing.T) {
	d := newTestDB(t)
	before := d.Counts()

	seedUser(t, d, "extra@test.com")
	after := d.Counts()

	if after.Users <= before.Users {
		t.Errorf("Users count should increase after insert: before=%d after=%d", before.Users, after.Users)
	}
}

func TestCounts_FloorplansAndSeats(t *testing.T) {
	d := newTestDB(t)
	seedFloorplanAndSeat(t, d, "Z1")
	c := d.Counts()
	if c.Floorplans == 0 {
		t.Error("expected at least 1 floorplan")
	}
	if c.Seats == 0 {
		t.Error("expected at least 1 seat")
	}
}

// -----------------------------------------------------------------------
// CleanExpiredSessions
// -----------------------------------------------------------------------

func TestCleanExpiredSessions_RemovesExpired(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "session@test.com")

	// Insert a session that expired in the past
	_, err := d.core.Exec(
		`INSERT INTO sessions (id, user_id, expires_at) VALUES ('deadbeef', ?, '2000-01-01 00:00:00')`,
		userID,
	)
	if err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	var before int
	d.core.QueryRow("SELECT COUNT(*) FROM sessions WHERE id='deadbeef'").Scan(&before) //nolint:errcheck
	if before != 1 {
		t.Fatal("expired session not inserted correctly")
	}

	d.CleanExpiredSessions()

	var after int
	d.core.QueryRow("SELECT COUNT(*) FROM sessions WHERE id='deadbeef'").Scan(&after) //nolint:errcheck
	if after != 0 {
		t.Error("expired session should have been deleted")
	}
}

func TestCleanExpiredSessions_PreservesValid(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "valid@test.com")

	// Create a normal (valid) session via the API
	tok, err := d.CreateSession(userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	d.CleanExpiredSessions()

	// Session should still exist
	u, err := d.GetSessionUser(tok)
	if err != nil {
		t.Errorf("valid session should survive CleanExpiredSessions: %v", err)
	}
	if u.ID != userID {
		t.Errorf("got wrong user after clean: %d", u.ID)
	}
}

// -----------------------------------------------------------------------
// CleanExpiredResetTokens
// -----------------------------------------------------------------------

func TestCleanExpiredResetTokens_RemovesExpired(t *testing.T) {
	d := newTestDB(t)
	_, err := d.CreateLocalUser("reset@test.com", "Reset", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}

	// Create a token, then artificially expire it
	rawToken, err := d.CreatePasswordResetToken("reset@test.com")
	if err != nil || rawToken == "" {
		t.Fatalf("CreatePasswordResetToken: err=%v token=%q", err, rawToken)
	}

	// Expire it
	d.core.Exec(`UPDATE password_reset_tokens SET expires_at = '2000-01-01 00:00:00'`) //nolint:errcheck

	var before int
	d.core.QueryRow("SELECT COUNT(*) FROM password_reset_tokens").Scan(&before) //nolint:errcheck
	if before == 0 {
		t.Fatal("token should be present before clean")
	}

	d.CleanExpiredResetTokens()

	var after int
	d.core.QueryRow("SELECT COUNT(*) FROM password_reset_tokens").Scan(&after) //nolint:errcheck
	if after != 0 {
		t.Errorf("expired reset token should have been deleted, count=%d", after)
	}
}

func TestCleanExpiredResetTokens_PreservesValid(t *testing.T) {
	d := newTestDB(t)
	_, err := d.CreateLocalUser("resetvalid@test.com", "ResetValid", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}

	rawToken, _ := d.CreatePasswordResetToken("resetvalid@test.com")
	if rawToken == "" {
		t.Fatal("expected non-empty reset token")
	}

	d.CleanExpiredResetTokens()

	// Token should still be valid
	u, err := d.UsePasswordResetToken(rawToken)
	if err != nil {
		t.Errorf("valid token should survive CleanExpiredResetTokens: %v", err)
	}
	if u == nil || u.Email != "resetvalid@test.com" {
		t.Error("UsePasswordResetToken returned wrong user")
	}
}

// -----------------------------------------------------------------------
// AdminRevokePAT / ListAllPATs
// -----------------------------------------------------------------------

func TestAdminRevokePAT_RevokesAnyToken(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "patowner@test.com")
	// Give user a non-basic role so PAT creation is possible
	d.core.Exec("UPDATE users SET role='global' WHERE id=?", userID) //nolint:errcheck

	_, pat, err := d.CreatePAT(userID, "admin-revoke-test", nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	if err := d.AdminRevokePAT(pat.ID); err != nil {
		t.Errorf("AdminRevokePAT: %v", err)
	}

	// Should be gone
	pats, _ := d.ListUserPATs(userID)
	for _, p := range pats {
		if p.ID == pat.ID {
			t.Error("PAT should have been deleted by AdminRevokePAT")
		}
	}
}

func TestAdminRevokePAT_NotFound_ReturnsError(t *testing.T) {
	d := newTestDB(t)
	if err := d.AdminRevokePAT(99999); err == nil {
		t.Error("expected error for non-existent PAT ID")
	}
}

func TestDeleteStatus_FreeStatus_Succeeds(t *testing.T) {
	d := newTestDB(t)
	sid := seedOnSiteStatus(t, d)

	if err := d.DeleteStatus(sid); err != nil {
		t.Fatalf("expected no error deleting unused status, got: %v", err)
	}
}

func TestDeleteStatus_InUseReturnsError(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "u@test.com")
	sid := seedOnSiteStatus(t, d)

	// Attach a presence so the status is in use.
	if err := d.SetPresences(uid, []string{"2026-05-05"}, sid, "full"); err != nil {
		t.Fatalf("SetPresences: %v", err)
	}

	err := d.DeleteStatus(sid)
	if err == nil {
		t.Fatal("expected an error deleting a status with linked presences, got nil")
	}
	if err.Error() != "status_in_use" {
		t.Errorf("expected sentinel error \"status_in_use\", got %q", err.Error())
	}
}

func TestSetStatusDisabled_TogglesCorrectly(t *testing.T) {
	d := newTestDB(t)
	sid := seedOnSiteStatus(t, d)

	// Initially the status must be active.
	statuses, err := d.ListStatuses()
	if err != nil {
		t.Fatalf("ListStatuses: %v", err)
	}
	var found bool
	for _, s := range statuses {
		if s.ID == sid {
			found = true
			if s.Disabled {
				t.Fatalf("expected status to be enabled on creation, got disabled=true")
			}
		}
	}
	if !found {
		t.Fatalf("seeded status id=%d not found in ListStatuses", sid)
	}

	// Disable it.
	if err := d.SetStatusDisabled(sid, true); err != nil {
		t.Fatalf("SetStatusDisabled(true): %v", err)
	}
	statuses, _ = d.ListStatuses()
	for _, s := range statuses {
		if s.ID == sid && !s.Disabled {
			t.Fatalf("expected status to be disabled after SetStatusDisabled(true)")
		}
	}

	// Re-enable it.
	if err := d.SetStatusDisabled(sid, false); err != nil {
		t.Fatalf("SetStatusDisabled(false): %v", err)
	}
	statuses, _ = d.ListStatuses()
	for _, s := range statuses {
		if s.ID == sid && s.Disabled {
			t.Fatalf("expected status to be active again after SetStatusDisabled(false)")
		}
	}
}

func TestListActiveStatuses_ExcludesDisabled(t *testing.T) {
	d := newTestDB(t)
	sid := seedOnSiteStatus(t, d)

	// Before disabling: must appear in ListActiveStatuses.
	active, err := d.ListActiveStatuses()
	if err != nil {
		t.Fatalf("ListActiveStatuses: %v", err)
	}
	var found bool
	for _, s := range active {
		if s.ID == sid {
			found = true
		}
	}
	if !found {
		t.Fatalf("enabled status id=%d not found in ListActiveStatuses", sid)
	}

	// Disable the status.
	if err := d.SetStatusDisabled(sid, true); err != nil {
		t.Fatalf("SetStatusDisabled: %v", err)
	}

	// After disabling: must NOT appear in ListActiveStatuses.
	active, _ = d.ListActiveStatuses()
	for _, s := range active {
		if s.ID == sid {
			t.Fatalf("disabled status id=%d should not appear in ListActiveStatuses", sid)
		}
	}

	// But must still appear in ListStatuses (full admin view).
	all, _ := d.ListStatuses()
	found = false
	for _, s := range all {
		if s.ID == sid {
			found = true
		}
	}
	if !found {
		t.Fatalf("disabled status id=%d should still appear in ListStatuses", sid)
	}
}

// TestListStatuses_NullDisabledColumn verifies that both ListStatuses and
// ListActiveStatuses handle a NULL disabled value gracefully (via COALESCE).
// This reproduces the production scenario where an existing database had rows
// before the disabled column was added: the addColumnIfNotExists migration adds
// the column as nullable so that ALTER TABLE never fails on older SQLite builds,
// leaving pre-existing rows with disabled = NULL until the next UPDATE.
func TestListStatuses_NullDisabledColumn(t *testing.T) {
	d := newTestDB(t)

	// Recreate the statuses table without NOT NULL on disabled so we can
	// insert a row that simulates a pre-migration record (disabled = NULL).
	d.presence.Exec("DROP TABLE IF EXISTS statuses") //nolint:errcheck
	d.presence.Exec(`CREATE TABLE statuses (                                                   
id   INTEGER PRIMARY KEY AUTOINCREMENT,
name TEXT    NOT NULL,
color TEXT   NOT NULL DEFAULT '#3b82f6',
billable BOOLEAN NOT NULL DEFAULT 0,
on_site  BOOLEAN NOT NULL DEFAULT 0,
sort_order INTEGER NOT NULL DEFAULT 0,
disabled BOOLEAN,
created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)`) //nolint:errcheck

	res, err := d.presence.Exec(
		"INSERT INTO statuses (name, color, billable, on_site, sort_order, disabled) VALUES ('Legacy', '#123456', 0, 0, 99, NULL)",
	)
	if err != nil {
		t.Fatalf("insert legacy status: %v", err)
	}
	id, _ := res.LastInsertId()

	// ListStatuses must succeed and return the row with Disabled=false.
	all, err := d.ListStatuses()
	if err != nil {
		t.Fatalf("ListStatuses with NULL disabled: %v", err)
	}
	var found bool
	for _, s := range all {
		if s.ID == id {
			found = true
			if s.Disabled {
				t.Errorf("NULL disabled should be treated as false via COALESCE, got Disabled=true")
			}
		}
	}
	if !found {
		t.Fatalf("legacy status id=%d not returned by ListStatuses", id)
	}

	// ListActiveStatuses must also include the row (NULL → treated as active).
	active, err := d.ListActiveStatuses()
	if err != nil {
		t.Fatalf("ListActiveStatuses with NULL disabled: %v", err)
	}
	found = false
	for _, s := range active {
		if s.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("legacy status id=%d (disabled=NULL) should appear in ListActiveStatuses", id)
	}
}

func TestListAllPATs_ReturnsAllUsers(t *testing.T) {
	d := newTestDB(t)
	u1 := seedUser(t, d, "p1@test.com")
	u2 := seedUser(t, d, "p2@test.com")

	expires := time.Now().Add(24 * time.Hour)
	d.CreatePAT(u1, "token1", &expires) //nolint:errcheck
	d.CreatePAT(u2, "token2", &expires) //nolint:errcheck

	all, err := d.ListAllPATs()
	if err != nil {
		t.Fatalf("ListAllPATs: %v", err)
	}
	if len(all) < 2 {
		t.Errorf("expected at least 2 tokens, got %d", len(all))
	}
	// Verify UserName is populated
	for _, p := range all {
		if p.UserName == "" {
			t.Errorf("PAT ID=%d has empty UserName", p.ID)
		}
	}
}
