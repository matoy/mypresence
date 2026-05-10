package db

import (
	"testing"
	"time"
)

// -----------------------------------------------------------------------
// SetPresences — invalid half value
// -----------------------------------------------------------------------

// TestSetPresences_InvalidHalf covers: return fmt.Errorf("invalid half value: %s", half)
func TestSetPresences_InvalidHalf(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "invalid_half@test.com")
	statusID := seedOnSiteStatus(t, d)
	err := d.SetPresences(uid, []string{"2026-06-01"}, statusID, "INVALID")
	if err == nil {
		t.Fatal("expected error for invalid half value")
	}
}

// -----------------------------------------------------------------------
// CreateSession — FK violation (non-existent user_id)
// -----------------------------------------------------------------------

// TestCreateSession_FKViolation covers: return "", err  (INSERT fails)
func TestCreateSession_FKViolation(t *testing.T) {
	d := newTestDB(t)
	_, err := d.CreateSession(999999) // user does not exist → FK violation
	if err == nil {
		t.Fatal("expected FK violation error for non-existent user")
	}
}

// -----------------------------------------------------------------------
// UpdateUserRoles — invalid role
// -----------------------------------------------------------------------

// TestUpdateUserRoles_InvalidRole covers: return fmt.Errorf("invalid role: %s", r)
func TestUpdateUserRoles_InvalidRole(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "badrole@test.com")
	err := d.UpdateUserRoles(uid, "superpowers")
	if err == nil {
		t.Fatal("expected error for invalid role")
	}
}

// -----------------------------------------------------------------------
// SeedDefaults — plain-text stored hash triggers UPDATE branch
// -----------------------------------------------------------------------

// TestSeedDefaults_PlainTextHash covers: the `if !strings.HasPrefix(stored, "$2")` true branch
// (UPDATE users SET role = 'global', password_hash = ? WHERE email = ?)
func TestSeedDefaults_PlainTextHash(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(4)

	// Pre-insert admin with a plain-text "hash" (does NOT start with "$2")
	d.core.Exec( //nolint:errcheck
		"INSERT INTO users (email, name, role, password_hash) VALUES ('admin_plain@test.com', 'Admin', 'global', 'plaintextpassword')",
	)

	// SeedDefaults will see stored="plaintextpassword" (no "$2" prefix) and update to bcrypt.
	if err := d.SeedDefaults("admin_plain@test.com", "newpassword"); err != nil {
		t.Fatalf("SeedDefaults with plain-text hash: %v", err)
	}
}

// -----------------------------------------------------------------------
// RevokePAT / AdminRevokePAT — exec error after closing core
// -----------------------------------------------------------------------

// TestRevokePAT_ExecError covers: return err (when Exec fails)
func TestRevokePAT_ExecError(t *testing.T) {
	d := newTestDB(t)
	d.core.Close() //nolint:errcheck
	err := d.RevokePAT(1, 1)
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}

// TestAdminRevokePAT_ExecError covers: return err (when Exec fails)
func TestAdminRevokePAT_ExecError(t *testing.T) {
	d := newTestDB(t)
	d.core.Close() //nolint:errcheck
	err := d.AdminRevokePAT(1)
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}

// -----------------------------------------------------------------------
// DeleteStatus — exec error after closing presence db
// -----------------------------------------------------------------------

// TestDeleteStatus_ExecError covers: return err (when Exec fails)
func TestDeleteStatus_ExecError(t *testing.T) {
	d := newTestDB(t)
	d.presence.Close() //nolint:errcheck
	err := d.DeleteStatus(1)
	if err == nil {
		t.Fatal("expected error after closing presence db")
	}
}

// -----------------------------------------------------------------------
// ListStatuses — query error after closing presence db
// -----------------------------------------------------------------------

// TestListStatuses_QueryError covers: return nil, err (when Query fails)
func TestListStatuses_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.presence.Close() //nolint:errcheck
	_, err := d.ListStatuses()
	if err == nil {
		t.Fatal("expected error after closing presence db")
	}
}

// -----------------------------------------------------------------------
// ClearPresences — invalid half value
// -----------------------------------------------------------------------

// TestClearPresences_InvalidHalf covers: return fmt.Errorf("invalid half value: %s", half)
func TestClearPresences_InvalidHalf(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "clear_invalid@test.com")
	err := d.ClearPresences(uid, []string{"2026-06-01"}, "WRONG")
	if err == nil {
		t.Fatal("expected error for invalid half value")
	}
}

// -----------------------------------------------------------------------
// SetPresences — tx.Begin error (presence DB closed)
// -----------------------------------------------------------------------

// TestSetPresences_TxBeginError covers: return err (when tx.Begin fails)
func TestSetPresences_TxBeginError(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "setpresence_err@test.com")
	d.presence.Close() //nolint:errcheck
	err := d.SetPresences(uid, []string{"2026-06-01"}, 1, "full")
	if err == nil {
		t.Fatal("expected error after closing presence db")
	}
}

// -----------------------------------------------------------------------
// CreatePasswordResetToken — INSERT error (core DB closed)
// -----------------------------------------------------------------------

// TestCreatePasswordResetToken_InsertError covers: return "", err (when INSERT fails)
func TestCreatePasswordResetToken_InsertError(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(4)
	uid, err := d.CreateLocalUser("resettoken_err@test.com", "Test", "password")
	if err != nil || uid <= 0 {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	// Drop the tokens table so the INSERT fails (GetUserByEmail still succeeds).
	d.core.Exec("DROP TABLE password_reset_tokens") //nolint:errcheck
	_, err = d.CreatePasswordResetToken("resettoken_err@test.com")
	if err == nil {
		t.Fatal("expected INSERT error after dropping tokens table")
	}
}

// -----------------------------------------------------------------------
// SetUserPassword — UPDATE error (core DB closed)
// -----------------------------------------------------------------------

// TestSetUserPassword_ExecError covers: return err (when UPDATE fails)
func TestSetUserPassword_ExecError(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(4)
	uid := seedUser(t, d, "setuserpass_err@test.com")
	d.core.Close() //nolint:errcheck
	err := d.SetUserPassword(uid, "newpassword")
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}

// -----------------------------------------------------------------------
// ListHolidays / GetHolidayMap — query error (presence DB closed)
// -----------------------------------------------------------------------

func TestListHolidays_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.presence.Close() //nolint:errcheck
	_, err := d.ListHolidays()
	if err == nil {
		t.Fatal("expected error after closing presence db")
	}
}

func TestGetHolidayMap_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.presence.Close() //nolint:errcheck
	_, err := d.GetHolidayMap("2026-01-01", "2026-12-31")
	if err == nil {
		t.Fatal("expected error after closing presence db")
	}
}

// -----------------------------------------------------------------------
// GetUserTeams — query error (core DB closed)
// -----------------------------------------------------------------------

func TestGetUserTeams_QueryError(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "getuserteams_err@test.com")
	d.core.Close() //nolint:errcheck
	_, err := d.GetUserTeams(uid)
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}

// -----------------------------------------------------------------------
// ListUserPATs / ListAllPATs — query error (core DB closed)
// -----------------------------------------------------------------------

func TestListUserPATs_QueryError(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "listupat_err@test.com")
	d.core.Close() //nolint:errcheck
	_, err := d.ListUserPATs(uid)
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}

func TestListAllPATs_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.core.Close() //nolint:errcheck
	_, err := d.ListAllPATs()
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}

// -----------------------------------------------------------------------
// ListActiveStatuses — query error (presence DB closed)
// -----------------------------------------------------------------------

func TestListActiveStatuses_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.presence.Close() //nolint:errcheck
	_, err := d.ListActiveStatuses()
	if err == nil {
		t.Fatal("expected error after closing presence db")
	}
}

// -----------------------------------------------------------------------
// GetTeamMembers / ListTeams — query error (core DB closed)
// -----------------------------------------------------------------------

func TestGetTeamMembers_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.core.Close() //nolint:errcheck
	_, err := d.GetTeamMembers(1)
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}

func TestListTeams_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.core.Close() //nolint:errcheck
	_, err := d.ListTeams()
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}

// -----------------------------------------------------------------------
// DeleteUserSessions — empty token (covers the early-return branch)
// -----------------------------------------------------------------------

func TestDeleteUserSessions_EmptyToken(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "del_all_sessions@test.com")
	// Insert a session directly
	d.core.Exec( //nolint:errcheck
		"INSERT INTO sessions (id, user_id, expires_at) VALUES ('testhash1', ?, datetime('now', '+1 hour'))",
		uid,
	)
	// Delete all sessions for user (empty exceptTokenRaw)
	d.DeleteUserSessions(uid, "")
	var count int
	d.core.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id = ?", uid).Scan(&count) //nolint:errcheck
	if count != 0 {
		t.Fatalf("expected 0 sessions after DeleteUserSessions(''), got %d", count)
	}
}

// -----------------------------------------------------------------------
// InsertGetID — Exec error (UNIQUE constraint violation)
// -----------------------------------------------------------------------

// TestInsertGetID_ExecError covers the: return 0, err path (Exec fails)
func TestInsertGetID_ExecError(t *testing.T) {
	d := newTestDB(t)
	// First insert succeeds
	_, err := d.core.InsertGetID("INSERT INTO users (email, name, role) VALUES (?, ?, 'basic')", "dup_err@test.com", "Dup")
	if err != nil {
		t.Fatalf("first InsertGetID: %v", err)
	}
	// Duplicate email → UNIQUE constraint violation → Exec returns error
	_, err = d.core.InsertGetID("INSERT INTO users (email, name, role) VALUES (?, ?, 'basic')", "dup_err@test.com", "Dup2")
	if err == nil {
		t.Fatal("expected UNIQUE constraint error on duplicate insert")
	}
}

// -----------------------------------------------------------------------
// ListFloorplans / GetFloorplan / ListSeats — query error (floorplan DB closed)
// -----------------------------------------------------------------------

func TestListFloorplans_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.floorplan.Close() //nolint:errcheck
	_, err := d.ListFloorplans()
	if err == nil {
		t.Fatal("expected error after closing floorplan db")
	}
}

func TestGetFloorplan_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.floorplan.Close() //nolint:errcheck
	_, err := d.GetFloorplan(1)
	if err == nil {
		t.Fatal("expected error after closing floorplan db")
	}
}

func TestListSeats_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.floorplan.Close() //nolint:errcheck
	_, err := d.ListSeats(1)
	if err == nil {
		t.Fatal("expected error after closing floorplan db")
	}
}

func TestGetSeatsWithStatus_ListSeatsError(t *testing.T) {
	d := newTestDB(t)
	d.floorplan.Close() //nolint:errcheck
	_, err := d.GetSeatsWithStatus(1, 1, "2026-06-01", "full")
	if err == nil {
		t.Fatal("expected error after closing floorplan db")
	}
}

func TestGetUserReservationDates_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.floorplan.Close() //nolint:errcheck
	_, err := d.GetUserReservationDates(1, "2026-06-01", "2026-06-30")
	if err == nil {
		t.Fatal("expected error after closing floorplan db")
	}
}

// -----------------------------------------------------------------------
// ListUsers — query error (core DB closed)
// -----------------------------------------------------------------------

func TestListUsers_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.core.Close() //nolint:errcheck
	_, err := d.ListUsers()
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}

// -----------------------------------------------------------------------
// GetPresences — query error (presence DB closed)
// -----------------------------------------------------------------------

func TestGetPresences_QueryError(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "getpres_err@test.com")
	d.presence.Close() //nolint:errcheck
	_, err := d.GetPresences([]int64{uid}, "2026-06-01", "2026-06-30")
	if err == nil {
		t.Fatal("expected error after closing presence db")
	}
}

// -----------------------------------------------------------------------
// ReserveSeat — exec error (floorplan DB closed)
// -----------------------------------------------------------------------

func TestReserveSeat_ExecError(t *testing.T) {
	d := newTestDB(t)
	d.floorplan.Close() //nolint:errcheck
	err := d.ReserveSeat(1, 1, "2026-06-01", "full")
	// QueryRow.Scan silently fails → count=0 → continues to Exec → Exec fails
	if err == nil {
		t.Fatal("expected error after closing floorplan db")
	}
}

// -----------------------------------------------------------------------
// LogPresenceAction — begin error (presence DB closed)
// -----------------------------------------------------------------------

func TestLogPresenceAction_BeginError(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "logaction_err@test.com")
	d.presence.Close() //nolint:errcheck
	err := d.LogPresenceAction(uid, uid, "set", []string{"2026-06-01"}, 1, "full")
	if err == nil {
		t.Fatal("expected error after closing presence db")
	}
}

// -----------------------------------------------------------------------
// GetUserLogs — query error (presence DB closed)
// -----------------------------------------------------------------------

func TestGetUserLogs_QueryError(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "getlogs_err@test.com")
	d.presence.Close() //nolint:errcheck
	_, err := d.GetUserLogs(uid, time.Time{})
	if err == nil {
		t.Fatal("expected error after closing presence db")
	}
}

// -----------------------------------------------------------------------
// ListProjects — query error (projects DB closed)
// -----------------------------------------------------------------------

func TestListProjects_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.projects.Close() //nolint:errcheck
	_, err := d.ListProjects()
	if err == nil {
		t.Fatal("expected error after closing projects db")
	}
}

// -----------------------------------------------------------------------
// GetAdminLogsByActor — query error (audit DB closed)
// -----------------------------------------------------------------------

func TestGetAdminLogsByActor_QueryError(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "getadmin_err@test.com")
	d.audit.Close() //nolint:errcheck
	_, err := d.GetAdminLogsByActor(uid, time.Time{})
	if err == nil {
		t.Fatal("expected error after closing audit db")
	}
}

// -----------------------------------------------------------------------
// projects.go — query errors when projects DB closed
// -----------------------------------------------------------------------

func TestGetProject_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.projects.Close() //nolint:errcheck
	_, err := d.GetProject(1)
	if err == nil {
		t.Fatal("expected error after closing projects db")
	}
}

func TestGetUserProjectEntriesForMonth_QueryError(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "projentries_err@test.com")
	d.projects.Close() //nolint:errcheck
	_, err := d.GetUserProjectEntriesForMonth(uid, 2026, 6)
	if err == nil {
		t.Fatal("expected error after closing projects db")
	}
}

func TestListActiveProjectsForMonth_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.projects.Close() //nolint:errcheck
	_, err := d.ListActiveProjectsForMonth(2026, 6)
	if err == nil {
		t.Fatal("expected error after closing projects db")
	}
}

func TestGetTeamIDsForUser_QueryError(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "teamids_err@test.com")
	d.core.Close() //nolint:errcheck
	_, err := d.GetTeamIDsForUser(uid)
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}
