package db

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"presence-app/internal/config"
	"presence-app/internal/models"
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
	_, _ = d.presence.Exec("DROP TABLE IF EXISTS statuses")
	_, _ = d.presence.Exec(`CREATE TABLE statuses (                                                   
id   INTEGER PRIMARY KEY AUTOINCREMENT,
name TEXT    NOT NULL,
color TEXT   NOT NULL DEFAULT '#3b82f6',
billable BOOLEAN NOT NULL DEFAULT 0,
on_site  BOOLEAN NOT NULL DEFAULT 0,
sort_order INTEGER NOT NULL DEFAULT 0,
disabled BOOLEAN,
created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)`)

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

// -----------------------------------------------------------------------
// CreateLocalUser / CheckPassword / SetUserPassword
// -----------------------------------------------------------------------

func TestCreateLocalUser_StoresHashedPassword(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(bcrypt.MinCost)

	id, err := d.CreateLocalUser("local@test.com", "Local User", "secret")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}
	var hash string
	d.core.QueryRow("SELECT COALESCE(password_hash,'') FROM users WHERE id=?", id).Scan(&hash) //nolint:errcheck
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("expected bcrypt hash, got %q", hash)
	}
}

func TestCheckPassword_BcryptHash_Correct(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(bcrypt.MinCost)

	id, _ := d.CreateLocalUser("pass@test.com", "Pass User", "correct")
	var hash string
	d.core.QueryRow("SELECT COALESCE(password_hash,'') FROM users WHERE id=?", id).Scan(&hash) //nolint:errcheck

	if !d.CheckPassword(id, hash, "correct") {
		t.Error("CheckPassword should return true for correct password")
	}
	if d.CheckPassword(id, hash, "wrong") {
		t.Error("CheckPassword should return false for wrong password")
	}
}

func TestCheckPassword_EmptyHash_ReturnsFalse(t *testing.T) {
	d := newTestDB(t)
	if d.CheckPassword(1, "", "password") {
		t.Error("empty hash should return false")
	}
	if d.CheckPassword(1, "$2y$...", "") {
		t.Error("empty password should return false")
	}
}

func TestCheckPassword_LegacyPlaintext_Rehashes(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(bcrypt.MinCost)

	uid := seedUser(t, d, "legacy@test.com")
	// Set a plaintext "hash" (legacy migration scenario)
	d.core.Exec("UPDATE users SET password_hash = 'plaintextpass' WHERE id = ?", uid) //nolint:errcheck

	if !d.CheckPassword(uid, "plaintextpass", "plaintextpass") {
		t.Error("legacy plaintext match should return true")
	}
	// After a successful match the hash must be upgraded to bcrypt
	var newHash string
	d.core.QueryRow("SELECT COALESCE(password_hash,'') FROM users WHERE id=?", uid).Scan(&newHash) //nolint:errcheck
	if !strings.HasPrefix(newHash, "$2") {
		t.Error("plaintext hash should be auto-rehashed to bcrypt after a successful match")
	}
}

func TestSetUserPassword_ChangesPassword(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(bcrypt.MinCost)

	id, _ := d.CreateLocalUser("pwdchange@test.com", "Pwd", "oldpass")
	if err := d.SetUserPassword(id, "newpass"); err != nil {
		t.Fatalf("SetUserPassword: %v", err)
	}
	var hash string
	d.core.QueryRow("SELECT COALESCE(password_hash,'') FROM users WHERE id=?", id).Scan(&hash) //nolint:errcheck
	if !d.CheckPassword(id, hash, "newpass") {
		t.Error("new password should be accepted after SetUserPassword")
	}
	if d.CheckPassword(id, hash, "oldpass") {
		t.Error("old password should be rejected after SetUserPassword")
	}
}

// -----------------------------------------------------------------------
// SetUserDisabled / GetUserByID / GetUserByEmail / ListUsers
// -----------------------------------------------------------------------

func TestSetUserDisabled_TogglesFlag(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "disable@test.com")

	if err := d.SetUserDisabled(uid, true); err != nil {
		t.Fatalf("SetUserDisabled(true): %v", err)
	}
	u, err := d.GetUserByID(uid)
	if err != nil {
		t.Fatalf("GetUserByID after disable: %v", err)
	}
	if !u.Disabled {
		t.Error("user should be disabled")
	}

	if err := d.SetUserDisabled(uid, false); err != nil {
		t.Fatalf("SetUserDisabled(false): %v", err)
	}
	u, err = d.GetUserByID(uid)
	if err != nil {
		t.Fatalf("GetUserByID after re-enable: %v", err)
	}
	if u.Disabled {
		t.Error("user should be re-enabled")
	}
}

func TestGetUserByEmail_ReturnsUser(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "byemail@test.com")

	u, err := d.GetUserByEmail("byemail@test.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if u.ID != uid {
		t.Errorf("got wrong user ID %d, want %d", u.ID, uid)
	}
}

func TestGetUserByEmail_NotFound_ReturnsError(t *testing.T) {
	d := newTestDB(t)
	if _, err := d.GetUserByEmail("nobody@nowhere.com"); err == nil {
		t.Error("expected error for missing email")
	}
}

func TestGetUserByID_NotFound_ReturnsError(t *testing.T) {
	d := newTestDB(t)
	if _, err := d.GetUserByID(99999); err == nil {
		t.Error("expected error for missing user ID")
	}
}

func TestListUsers_ReturnsAll(t *testing.T) {
	d := newTestDB(t)
	seedUser(t, d, "u1@test.com")
	seedUser(t, d, "u2@test.com")

	users, err := d.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) < 2 {
		t.Errorf("expected at least 2 users, got %d", len(users))
	}
}

func TestUpdateUserRoles_ChangesRole(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "roles@test.com")

	if err := d.UpdateUserRoles(uid, "global,status_manager"); err != nil {
		t.Fatalf("UpdateUserRoles: %v", err)
	}
	u, err := d.GetUserByID(uid)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if u.Roles != "global,status_manager" {
		t.Errorf("expected updated roles, got %q", u.Roles)
	}
}

func TestUpdateLocalUser_ChangesEmailAndName(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(bcrypt.MinCost)

	uid, _ := d.CreateLocalUser("before@test.com", "Before", "pass")
	if err := d.UpdateLocalUser(uid, "after@test.com", "After"); err != nil {
		t.Fatalf("UpdateLocalUser: %v", err)
	}
	u, err := d.GetUserByID(uid)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if u.Email != "after@test.com" || u.Name != "After" {
		t.Errorf("expected email=after@test.com name=After, got %q / %q", u.Email, u.Name)
	}
}

func TestDeleteLocalUser_RemovesUser(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "del@test.com")

	if err := d.DeleteLocalUser(uid); err != nil {
		t.Fatalf("DeleteLocalUser: %v", err)
	}
	if _, err := d.GetUserByID(uid); err == nil {
		t.Error("user should have been deleted")
	}
}

// -----------------------------------------------------------------------
// Session lifecycle: DeleteSession / DeleteUserSessions
// -----------------------------------------------------------------------

func TestDeleteSession_RemovesSession(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "delsess@test.com")

	tok, _ := d.CreateSession(uid)
	if err := d.DeleteSession(tok); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := d.GetSessionUser(tok); err == nil {
		t.Error("session should be gone after DeleteSession")
	}
}

func TestDeleteSession_UnknownToken_IsNoOp(t *testing.T) {
	d := newTestDB(t)
	// Deleting a non-existent session should succeed silently (DELETE with 0 rows affected).
	if err := d.DeleteSession("no-such-token"); err != nil {
		t.Errorf("DeleteSession on unknown token should not error, got: %v", err)
	}
}

func TestDeleteUserSessions_RemovesAllExceptOne(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "multisess@test.com")

	tok1, _ := d.CreateSession(uid)
	tok2, _ := d.CreateSession(uid)

	d.DeleteUserSessions(uid, tok1)

	if _, err := d.GetSessionUser(tok1); err != nil {
		t.Error("tok1 should still be valid (was the excepted token)")
	}
	if _, err := d.GetSessionUser(tok2); err == nil {
		t.Error("tok2 should have been deleted")
	}
}

// -----------------------------------------------------------------------
// GetUserByPAT / RevokePAT
// -----------------------------------------------------------------------

func TestGetUserByPAT_ValidToken(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "patauth@test.com")
	d.core.Exec("UPDATE users SET role='global' WHERE id=?", uid) //nolint:errcheck

	rawToken, _, err := d.CreatePAT(uid, "auth-test", nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	u, err := d.GetUserByPAT(rawToken)
	if err != nil {
		t.Fatalf("GetUserByPAT: %v", err)
	}
	if u.ID != uid {
		t.Errorf("expected userID %d, got %d", uid, u.ID)
	}
}

func TestGetUserByPAT_InvalidToken_ReturnsError(t *testing.T) {
	d := newTestDB(t)
	if _, err := d.GetUserByPAT("invalid-token"); err == nil {
		t.Error("expected error for invalid PAT")
	}
}

func TestRevokePAT_OwnerCanRevoke(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "patrevoke@test.com")
	d.core.Exec("UPDATE users SET role='global' WHERE id=?", uid) //nolint:errcheck

	_, pat, _ := d.CreatePAT(uid, "revoke-test", nil)
	if err := d.RevokePAT(pat.ID, uid); err != nil {
		t.Fatalf("RevokePAT: %v", err)
	}
	pats, _ := d.ListUserPATs(uid)
	for _, p := range pats {
		if p.ID == pat.ID {
			t.Error("PAT should have been revoked by its owner")
		}
	}
}

func TestRevokePAT_OtherUserCannotRevoke(t *testing.T) {
	d := newTestDB(t)
	owner := seedUser(t, d, "patowner2@test.com")
	other := seedUser(t, d, "patother@test.com")
	d.core.Exec("UPDATE users SET role='global' WHERE id=?", owner) //nolint:errcheck

	_, pat, _ := d.CreatePAT(owner, "cannot-revoke", nil)
	if err := d.RevokePAT(pat.ID, other); err == nil {
		t.Error("another user should not be able to revoke someone else's PAT")
	}
}

// -----------------------------------------------------------------------
// CreateStatus / UpdateStatus
// -----------------------------------------------------------------------

func TestCreateStatus_ReturnsPositiveID(t *testing.T) {
	d := newTestDB(t)
	id, err := d.CreateStatus(models.Status{Name: "Test Status", Color: "#ffffff", Billable: false, OnSite: false, SortOrder: 1})
	if err != nil {
		t.Fatalf("CreateStatus: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}
}

func TestUpdateStatus_ChangesFields(t *testing.T) {
	d := newTestDB(t)
	sid := seedOnSiteStatus(t, d)

	err := d.UpdateStatus(models.Status{ID: sid, Name: "Updated Name", Color: "#000000", Billable: true, OnSite: false, SortOrder: 99})
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	statuses, err := d.ListStatuses()
	if err != nil {
		t.Fatalf("ListStatuses: %v", err)
	}
	var found bool
	for _, s := range statuses {
		if s.ID == sid {
			found = true
			if s.Name != "Updated Name" {
				t.Errorf("expected Name=Updated Name, got %q", s.Name)
			}
			if s.SortOrder != 99 {
				t.Errorf("expected SortOrder=99, got %d", s.SortOrder)
			}
			if !s.Billable {
				t.Error("expected Billable=true after update")
			}
		}
	}
	if !found {
		t.Error("updated status not found in ListStatuses")
	}
}

// -----------------------------------------------------------------------
// GetPresences / SetPresences / ClearPresences round-trip
// -----------------------------------------------------------------------

func TestSetPresences_GetPresences_FullDay(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "pres@test.com")
	sid := seedOnSiteStatus(t, d)

	dates := []string{"2026-05-01", "2026-05-02"}
	if err := d.SetPresences(uid, dates, sid, "full"); err != nil {
		t.Fatalf("SetPresences: %v", err)
	}
	result, err := d.GetPresences([]int64{uid}, "2026-05-01", "2026-05-02")
	if err != nil {
		t.Fatalf("GetPresences: %v", err)
	}
	for _, date := range dates {
		if result[uid][date]["full"] != sid {
			t.Errorf("expected presence on %s with status %d, got %v", date, sid, result[uid][date])
		}
	}
}

func TestSetPresences_HalfDay_AM(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "halfday@test.com")
	sid := seedOnSiteStatus(t, d)

	if err := d.SetPresences(uid, []string{"2026-05-03"}, sid, "AM"); err != nil {
		t.Fatalf("SetPresences AM: %v", err)
	}
	result, _ := d.GetPresences([]int64{uid}, "2026-05-03", "2026-05-03")
	if result[uid]["2026-05-03"]["AM"] != sid {
		t.Errorf("expected AM presence, got %v", result[uid]["2026-05-03"])
	}
	if _, hasFullDay := result[uid]["2026-05-03"]["full"]; hasFullDay {
		t.Error("should not have full-day presence when AM is set")
	}
}

func TestSetPresences_FullReplacesPreviousHalves(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "fullreplace@test.com")
	sid := seedOnSiteStatus(t, d)

	d.SetPresences(uid, []string{"2026-05-10"}, sid, "AM") //nolint:errcheck
	d.SetPresences(uid, []string{"2026-05-10"}, sid, "PM") //nolint:errcheck

	// Setting full-day should remove the AM/PM records
	if err := d.SetPresences(uid, []string{"2026-05-10"}, sid, "full"); err != nil {
		t.Fatalf("SetPresences full: %v", err)
	}
	result, _ := d.GetPresences([]int64{uid}, "2026-05-10", "2026-05-10")
	day := result[uid]["2026-05-10"]
	if _, hasAM := day["AM"]; hasAM {
		t.Error("AM should have been removed when full-day is set")
	}
	if day["full"] != sid {
		t.Errorf("expected full-day presence, got %v", day)
	}
}

func TestClearPresences_RemovesAll(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "clearall@test.com")
	sid := seedOnSiteStatus(t, d)

	d.SetPresences(uid, []string{"2026-05-04", "2026-05-05"}, sid, "full") //nolint:errcheck
	if err := d.ClearPresences(uid, []string{"2026-05-04", "2026-05-05"}, ""); err != nil {
		t.Fatalf("ClearPresences: %v", err)
	}
	result, _ := d.GetPresences([]int64{uid}, "2026-05-01", "2026-05-31")
	if len(result[uid]) != 0 {
		t.Errorf("expected no presences after clear, got %v", result[uid])
	}
}

func TestClearPresences_SpecificHalf(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "clearhalf@test.com")
	sid := seedOnSiteStatus(t, d)

	d.SetPresences(uid, []string{"2026-05-06"}, sid, "AM") //nolint:errcheck
	d.SetPresences(uid, []string{"2026-05-06"}, sid, "PM") //nolint:errcheck

	if err := d.ClearPresences(uid, []string{"2026-05-06"}, "AM"); err != nil {
		t.Fatalf("ClearPresences AM: %v", err)
	}
	result, _ := d.GetPresences([]int64{uid}, "2026-05-06", "2026-05-06")
	if _, hasAM := result[uid]["2026-05-06"]["AM"]; hasAM {
		t.Error("AM presence should have been cleared")
	}
	if _, hasPM := result[uid]["2026-05-06"]["PM"]; !hasPM {
		t.Error("PM presence should remain after clearing only AM")
	}
}

func TestGetPresences_EmptyUserIDs_ReturnsEmpty(t *testing.T) {
	d := newTestDB(t)
	result, err := d.GetPresences([]int64{}, "2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatalf("GetPresences empty: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result for no user IDs, got %v", result)
	}
}

// -----------------------------------------------------------------------
// Team CRUD: ListTeams / CreateTeam / UpdateTeam / DeleteTeam
// -----------------------------------------------------------------------

func TestListTeams_Empty(t *testing.T) {
	d := newTestDB(t)
	teams, err := d.ListTeams()
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if len(teams) != 0 {
		t.Errorf("expected 0 teams, got %d", len(teams))
	}
}

func TestCreateTeam_And_UpdateTeam(t *testing.T) {
	d := newTestDB(t)
	id, err := d.CreateTeam("Alpha Team")
	if err != nil || id <= 0 {
		t.Fatalf("CreateTeam: id=%d err=%v", id, err)
	}
	if err := d.UpdateTeam(id, "Alpha Team Renamed"); err != nil {
		t.Fatalf("UpdateTeam: %v", err)
	}
	teams, _ := d.ListTeams()
	var found bool
	for _, tm := range teams {
		if tm.ID == id && tm.Name == "Alpha Team Renamed" {
			found = true
		}
	}
	if !found {
		t.Error("renamed team not found in ListTeams")
	}
}

func TestDeleteTeam_RemovesTeam(t *testing.T) {
	d := newTestDB(t)
	id, _ := d.CreateTeam("ToDelete")
	if err := d.DeleteTeam(id); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}
	teams, _ := d.ListTeams()
	for _, tm := range teams {
		if tm.ID == id {
			t.Error("team should have been deleted")
		}
	}
}

func TestGetTeamMembers_And_RemoveTeamMember(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "member2@test.com")
	id, _ := d.CreateTeam("MemberTeam")
	d.AddTeamMember(id, uid) //nolint:errcheck

	members, err := d.GetTeamMembers(id)
	if err != nil {
		t.Fatalf("GetTeamMembers: %v", err)
	}
	if len(members) != 1 || members[0].ID != uid {
		t.Errorf("expected 1 member with ID %d, got %v", uid, members)
	}

	if err := d.RemoveTeamMember(id, uid); err != nil {
		t.Fatalf("RemoveTeamMember: %v", err)
	}
	members, _ = d.GetTeamMembers(id)
	if len(members) != 0 {
		t.Errorf("expected 0 members after removal, got %d", len(members))
	}
}

func TestGetUserTeams_ReturnsUserMemberships(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "userteams@test.com")
	id1, _ := d.CreateTeam("TeamA")
	id2, _ := d.CreateTeam("TeamB")
	d.AddTeamMember(id1, uid) //nolint:errcheck
	d.AddTeamMember(id2, uid) //nolint:errcheck

	teams, err := d.GetUserTeams(uid)
	if err != nil {
		t.Fatalf("GetUserTeams: %v", err)
	}
	if len(teams) != 2 {
		t.Errorf("expected 2 teams for user, got %d", len(teams))
	}
}

// -----------------------------------------------------------------------
// Holiday CRUD: CreateHoliday / ListHolidays / GetHolidayMap / UpdateHoliday / DeleteHoliday
// -----------------------------------------------------------------------

func TestCreateHoliday_And_ListHolidays(t *testing.T) {
	d := newTestDB(t)
	id, err := d.CreateHoliday("2026-07-14", "Bastille Day", false)
	if err != nil || id <= 0 {
		t.Fatalf("CreateHoliday: id=%d err=%v", id, err)
	}
	holidays, err := d.ListHolidays()
	if err != nil {
		t.Fatalf("ListHolidays: %v", err)
	}
	var found bool
	for _, h := range holidays {
		if h.ID == id && h.Name == "Bastille Day" && h.Date == "2026-07-14" {
			found = true
		}
	}
	if !found {
		t.Error("created holiday not found in ListHolidays")
	}
}

func TestGetHolidayMap_WithinRange(t *testing.T) {
	d := newTestDB(t)
	d.CreateHoliday("2026-08-15", "Assumption", false) //nolint:errcheck
	d.CreateHoliday("2026-11-11", "Armistice", true)   //nolint:errcheck

	m, err := d.GetHolidayMap("2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("GetHolidayMap: %v", err)
	}
	if _, ok := m["2026-08-15"]; !ok {
		t.Error("expected 2026-08-15 in holiday map")
	}
	if _, ok := m["2026-11-11"]; ok {
		t.Error("2026-11-11 should be outside the query range")
	}
}

func TestUpdateHoliday_ChangesFields(t *testing.T) {
	d := newTestDB(t)
	id, _ := d.CreateHoliday("2026-12-25", "Christmas", false)
	if err := d.UpdateHoliday(id, "2026-12-25", "Christmas Day", true); err != nil {
		t.Fatalf("UpdateHoliday: %v", err)
	}
	holidays, _ := d.ListHolidays()
	for _, h := range holidays {
		if h.ID == id {
			if h.Name != "Christmas Day" {
				t.Errorf("expected updated name, got %q", h.Name)
			}
			if !h.AllowImputed {
				t.Error("expected allow_imputed=true after update")
			}
		}
	}
}

func TestDeleteHoliday_RemovesHoliday(t *testing.T) {
	d := newTestDB(t)
	id, _ := d.CreateHoliday("2026-01-01", "New Year", false)
	if err := d.DeleteHoliday(id); err != nil {
		t.Fatalf("DeleteHoliday: %v", err)
	}
	holidays, _ := d.ListHolidays()
	for _, h := range holidays {
		if h.ID == id {
			t.Error("holiday should have been deleted")
		}
	}
}

// ── SetFloorplanImage ─────────────────────────────────────────────────────────

func TestSetFloorplanImage_UpdatesPath(t *testing.T) {
	d := newTestDB(t)
	fpID, _ := seedFloorplanAndSeat(t, d, "A1")

	if err := d.SetFloorplanImage(fpID, "/images/map.png"); err != nil {
		t.Fatalf("SetFloorplanImage: %v", err)
	}

	fp, err := d.GetFloorplan(fpID)
	if err != nil {
		t.Fatalf("GetFloorplan: %v", err)
	}
	if fp.ImagePath != "/images/map.png" {
		t.Fatalf("expected image_path '/images/map.png', got %q", fp.ImagePath)
	}
}

// ── GetSeatsWithStatus ────────────────────────────────────────────────────────

func TestGetSeatsWithStatus_Free(t *testing.T) {
	d := newTestDB(t)
	fpID, _ := seedFloorplanAndSeat(t, d, "B1")
	uid := seedUser(t, d, "free@test.com")

	seats, err := d.GetSeatsWithStatus(fpID, uid, "2026-06-01", "full")
	if err != nil {
		t.Fatalf("GetSeatsWithStatus: %v", err)
	}
	if len(seats) != 1 {
		t.Fatalf("expected 1 seat, got %d", len(seats))
	}
	if seats[0].Status != "free" {
		t.Fatalf("expected status 'free', got %q", seats[0].Status)
	}
}

func TestGetSeatsWithStatus_Mine(t *testing.T) {
	d := newTestDB(t)
	fpID, seatID := seedFloorplanAndSeat(t, d, "C1")
	uid := seedUser(t, d, "mine@test.com")

	// Reserve the seat for this user.
	_, err := d.floorplan.Exec(
		"INSERT INTO seat_reservations (seat_id, user_id, date, half) VALUES (?, ?, '2026-06-02', 'full')",
		seatID, uid,
	)
	if err != nil {
		t.Fatalf("insert reservation: %v", err)
	}

	seats, err := d.GetSeatsWithStatus(fpID, uid, "2026-06-02", "full")
	if err != nil {
		t.Fatalf("GetSeatsWithStatus: %v", err)
	}
	if len(seats) != 1 || seats[0].Status != "mine" {
		t.Fatalf("expected status 'mine', got %q", seats[0].Status)
	}
	if seats[0].ReservationID == 0 {
		t.Fatal("expected non-zero ReservationID for 'mine' seat")
	}
}

func TestGetSeatsWithStatus_Taken(t *testing.T) {
	d := newTestDB(t)
	fpID, seatID := seedFloorplanAndSeat(t, d, "D1")
	uid := seedUser(t, d, "owner@test.com")
	otherUID := seedUser(t, d, "other@test.com")

	// Reserve the seat for another user.
	_, err := d.floorplan.Exec(
		"INSERT INTO seat_reservations (seat_id, user_id, date, half) VALUES (?, ?, '2026-06-03', 'full')",
		seatID, otherUID,
	)
	if err != nil {
		t.Fatalf("insert reservation: %v", err)
	}

	seats, err := d.GetSeatsWithStatus(fpID, uid, "2026-06-03", "full")
	if err != nil {
		t.Fatalf("GetSeatsWithStatus: %v", err)
	}
	if len(seats) != 1 || seats[0].Status != "taken" {
		t.Fatalf("expected status 'taken', got %q", seats[0].Status)
	}
}

// ── GetAdminLogsByActor / fetchTeamNames / fetchStatusNames / fetchHolidayNames ──

func TestGetAdminLogsByActor_AllEntityTypes(t *testing.T) {
	d := newTestDB(t)

	actorID := seedUser(t, d, "actor@audit.com")

	// Create one entity of each type.
	teamID, _ := d.CreateTeam("Audit Team")
	statusID, _ := d.CreateStatus(models.Status{Name: "AuditStatus", Color: "#aabbcc", SortOrder: 1})
	holidayID, _ := d.CreateHoliday("2026-07-14", "Bastille Day", false)
	targetUID := seedUser(t, d, "target@audit.com")

	// Log one action per entity type.
	d.LogAdminAction(actorID, "team", teamID, "create", "Audit Team")
	d.LogAdminAction(actorID, "status", statusID, "create", "AuditStatus")
	d.LogAdminAction(actorID, "holiday", holidayID, "create", "Bastille Day")
	d.LogAdminAction(actorID, "user", targetUID, "create", "target@audit.com")

	logs, err := d.GetAdminLogsByActor(actorID, time.Time{})
	if err != nil {
		t.Fatalf("GetAdminLogsByActor: %v", err)
	}
	if len(logs) != 4 {
		t.Fatalf("expected 4 log entries, got %d", len(logs))
	}

	entityNames := make(map[string]string)
	for _, l := range logs {
		entityNames[l.EntityType] = l.EntityName
	}

	if entityNames["team"] == "" {
		t.Error("expected EntityName for team type, got empty")
	}
	if entityNames["status"] == "" {
		t.Error("expected EntityName for status type, got empty")
	}
	if entityNames["holiday"] == "" {
		t.Error("expected EntityName for holiday type, got empty")
	}
	if entityNames["user"] == "" {
		t.Error("expected EntityName for user type, got empty")
	}
}

// Ensure the import of "time" is used (kept for existing TestListAllPATs_ReturnsAllUsers).
var _ = time.Now
