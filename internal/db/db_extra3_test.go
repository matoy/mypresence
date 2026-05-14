package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// CreatePAT — InsertGetID error (core DB closed)
// -----------------------------------------------------------------------

func TestCreatePAT_CoreDBError(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "pat_core_err@test.com")
	d.core.Close() //nolint:errcheck
	_, _, err := d.CreatePAT(uid, "test token", nil)
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}

// -----------------------------------------------------------------------
// CreateLocalUser — UNIQUE constraint (covers InsertGetID error return)
// -----------------------------------------------------------------------

func TestCreateLocalUser_DuplicateEmail(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(4)
	_, err := d.CreateLocalUser("dup_local@test.com", "Test", "password1")
	if err != nil {
		t.Fatalf("first CreateLocalUser: %v", err)
	}
	_, err = d.CreateLocalUser("dup_local@test.com", "Test2", "password2")
	if err == nil {
		t.Fatal("expected UNIQUE constraint error on duplicate email")
	}
}

// -----------------------------------------------------------------------
// GetTeamStats — ListStatuses error (presence DB closed, core open)
// GetTeamMembersAt uses d.core → succeeds.
// ListStatuses uses d.presence → fails.
// -----------------------------------------------------------------------

func TestGetTeamStats_ListStatusesError(t *testing.T) {
	d := newTestDB(t)
	// Close only presence.db; core.db stays open so GetTeamMembersAt succeeds.
	d.presence.Close() //nolint:errcheck
	_, err := d.GetTeamStats(1, "2026-06-01", "2026-06-30")
	if err == nil {
		t.Fatal("expected error: ListStatuses should fail with presence db closed")
	}
}

// -----------------------------------------------------------------------
// UsePasswordResetToken — actual expired token path
// (existing test covers "invalid/unknown" token; this one covers "token expired")
// -----------------------------------------------------------------------

func TestUsePasswordResetToken_ActuallyExpired(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(4)

	// Create a real local user.
	_, err := d.CreateLocalUser("expire_real@test.com", "Expire Real", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}

	// Generate a real reset token.
	rawToken, err := d.CreatePasswordResetToken("expire_real@test.com")
	if err != nil || rawToken == "" {
		t.Fatalf("CreatePasswordResetToken: err=%v token=%q", err, rawToken)
	}

	// Backdate the token's expiry to 1 hour ago.
	_, err = d.core.Exec(
		"UPDATE password_reset_tokens SET expires_at = datetime('now', '-1 hour')",
	)
	if err != nil {
		t.Fatalf("UPDATE expires_at: %v", err)
	}

	// Now UsePasswordResetToken should find the token but report "token expired".
	_, err = d.UsePasswordResetToken(rawToken)
	if err == nil {
		t.Fatal("expected 'token expired' error")
	}
	if err.Error() != "token expired" {
		t.Fatalf("expected 'token expired', got %q", err.Error())
	}
}

// -----------------------------------------------------------------------
// openSQLiteMulti — presence.db is a directory (covers error cleanup path)
// -----------------------------------------------------------------------

func TestOpenSQLiteMulti_PresenceIsDir(t *testing.T) {
	dir := t.TempDir()
	// Create a directory named presence.db so SQLite cannot open it as a file.
	if err := os.Mkdir(filepath.Join(dir, "presence.db"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	_, err := Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err == nil {
		t.Fatal("expected Open to fail when presence.db is a directory")
	}
}

// -----------------------------------------------------------------------
// joinStrings — multiple elements (covers the `result += sep` when i > 0)
// -----------------------------------------------------------------------

func TestJoinStrings_MultipleElements(t *testing.T) {
	result := joinStrings([]string{"a", "b", "c"}, ",")
	if result != "a,b,c" {
		t.Fatalf("expected 'a,b,c', got %q", result)
	}
}

// -----------------------------------------------------------------------
// teamNameMap — query error (core DB closed)
// -----------------------------------------------------------------------

func TestTeamNameMap_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.core.Close() //nolint:errcheck
	_, err := d.teamNameMap()
	if err == nil {
		t.Fatal("expected error after closing core db")
	}
}

// -----------------------------------------------------------------------
// ReserveSeat — user already has a reservation for that day
// Covers: return fmt.Errorf("vous avez déjà réservé un siège pour cette journée")
// -----------------------------------------------------------------------

func TestReserveSeat_UserAlreadyHasReservation(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "already_rsv@test.com")
	fpID, seatID1 := seedFloorplanAndSeat(t, d, "Seat A")

	// Create a second seat on the same floorplan.
	seatRes, err := d.floorplan.Exec(
		"INSERT INTO seats (floorplan_id, label, x_pct, y_pct) VALUES (?, 'Seat B', 60, 60)", fpID,
	)
	if err != nil {
		t.Fatalf("insert seat B: %v", err)
	}
	seatID2, _ := seatRes.LastInsertId()

	// Reserve seat A for the user.
	if err := d.ReserveSeat(seatID1, uid, "2026-07-01", "full"); err != nil {
		t.Fatalf("first ReserveSeat: %v", err)
	}

	// Try to reserve seat B for the same user on the same day → should fail.
	err = d.ReserveSeat(seatID2, uid, "2026-07-01", "full")
	if err == nil {
		t.Fatal("expected error: user already has a reservation for this day")
	}
}

// -----------------------------------------------------------------------
// GetProjectsReport — user not in userMap (covers buildProjectReportRow !ok branch)
// -----------------------------------------------------------------------

func TestGetProjectsReport_UserNotInMap(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "notinmap@test.com")

	// Create a project.
	projectID, err := d.CreateProject("NIM Project", "NIM", 0, true, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Add a time entry for that user on that project.
	if err := d.SetProjectTimeEntry(uid, projectID, 2026, 6, 3.0); err != nil {
		t.Fatalf("SetProjectTimeEntry: %v", err)
	}

	// Call GetProjectsReport with an empty userMap — user is in DB but not in map.
	// buildProjectReportRow's `if !ok { continue }` branch is hit.
	result, err := d.GetProjectsReport(
		[]int64{projectID},
		[]string{"2026-06"},
		map[int64]models.User{}, // empty map: user not found
	)
	if err != nil {
		t.Fatalf("GetProjectsReport: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected at least one report row")
	}
	// The UserRows should be empty because the user was skipped.
	if len(result[0].UserRows) != 0 {
		t.Fatalf("expected 0 user rows when user not in map, got %d", len(result[0].UserRows))
	}
}

// -----------------------------------------------------------------------
// ListProjectsByTeams — query error (projects DB closed)
// -----------------------------------------------------------------------

func TestListProjectsByTeams_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.projects.Close() //nolint:errcheck
	_, err := d.ListProjectsByTeams([]int64{1})
	if err == nil {
		t.Fatal("expected error after closing projects db")
	}
}

// -----------------------------------------------------------------------
// GetProjectsReport — query error (projects DB closed)
// -----------------------------------------------------------------------

func TestGetProjectsReport_QueryError(t *testing.T) {
	d := newTestDB(t)
	d.projects.Close() //nolint:errcheck
	_, err := d.GetProjectsReport([]int64{1}, []string{"2026-06"}, map[int64]models.User{})
	if err == nil {
		t.Fatal("expected error after closing projects db")
	}
}

// -----------------------------------------------------------------------
// GetAdminLogsByActor — non-zero since (covers the `if !since.IsZero()` branch)
// -----------------------------------------------------------------------

func TestGetAdminLogsByActor_WithSince(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "logsince@test.com")
	d.LogAdminAction(uid, "team", 1, "create", "test") //nolint:errcheck

	// Use a non-zero time to exercise the `if !since.IsZero()` branch.
	// We pass time.Unix(1, 0) — older than any record — to cover the branch without needing results.
	since := time.Unix(1, 0)
	_, err := d.GetAdminLogsByActor(uid, since)
	if err != nil {
		t.Fatalf("GetAdminLogsByActor with since: %v", err)
	}
}

// -----------------------------------------------------------------------
// GetUserLogs — non-zero since (covers the `if !since.IsZero()` branch)
// -----------------------------------------------------------------------

func TestGetUserLogs_WithNonZeroSince(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "logssince@test.com")
	statusID := seedOnSiteStatus(t, d)
	d.SetPresences(uid, []string{"2026-06-01"}, statusID, "") //nolint:errcheck
	d.LogPresenceAction(uid, uid, "set", []string{"2026-06-01"}, statusID, "") //nolint:errcheck

	// Use a non-zero time to exercise the `if !since.IsZero()` branch.
	// time.Unix(1, 0) is older than any record, so results are returned.
	since := time.Unix(1, 0)
	_, err := d.GetUserLogs(uid, since)
	if err != nil {
		t.Fatalf("GetUserLogs with since: %v", err)
	}
}

// -----------------------------------------------------------------------
// BulkReserveSeat — user is on-site and reservation succeeds (covers count++ branch)
// -----------------------------------------------------------------------

func TestBulkReserveSeat_Success(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "bulkrsv_ok@test.com")
	statusID := seedOnSiteStatus(t, d)
	_, seatID := seedFloorplanAndSeat(t, d, "BulkOK")

	d.SetPresences(uid, []string{"2026-07-01", "2026-07-02"}, statusID, "") //nolint:errcheck

	count := d.BulkReserveSeat(seatID, uid, []string{"2026-07-01", "2026-07-02"}, "full")
	if count != 2 {
		t.Fatalf("expected 2 successful reservations, got %d", count)
	}
}

// TestBulkReserveSeat_EmptyHalf covers the `if half == ""` default branch.
func TestBulkReserveSeat_EmptyHalf(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "bulkrsv_empty@test.com")
	statusID := seedOnSiteStatus(t, d)
	_, seatID := seedFloorplanAndSeat(t, d, "BulkEmpty")

	d.SetPresences(uid, []string{"2026-07-03"}, statusID, "") //nolint:errcheck

	// Pass empty half string → defaults to "full"
	count := d.BulkReserveSeat(seatID, uid, []string{"2026-07-03"}, "")
	if count != 1 {
		t.Fatalf("expected 1 successful reservation with empty half, got %d", count)
	}
}

// -----------------------------------------------------------------------
// ListProjects — project with a real team_id covers the `if n, ok` true branch
// -----------------------------------------------------------------------

func TestListProjects_WithTeamID(t *testing.T) {
	d := newTestDB(t)
	teamID, err := d.CreateTeam("TestTeam")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	_, err = d.CreateProject("TeamProject", "TP", teamID, true, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	projects, err := d.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].TeamName == "" {
		t.Fatal("expected TeamName to be populated for project with valid team_id")
	}
}

// -----------------------------------------------------------------------
// ListActiveProjectsForMonth — project with team_id covers TeamName assignment
// -----------------------------------------------------------------------

func TestListActiveProjectsForMonth_WithTeamID(t *testing.T) {
	d := newTestDB(t)
	teamID, _ := d.CreateTeam("ActiveTeam")
	d.CreateProject("ActiveProject", "AP", teamID, true, "2026-01-01", "2026-12-31") //nolint:errcheck

	projects, err := d.ListActiveProjectsForMonth(2026, 6)
	if err != nil {
		t.Fatalf("ListActiveProjectsForMonth: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("expected at least 1 active project")
	}
	if projects[0].TeamName == "" {
		t.Fatal("expected TeamName to be populated")
	}
}

// -----------------------------------------------------------------------
// GetProject — with team_id covers TeamName assignment branch
// -----------------------------------------------------------------------

func TestGetProject_WithTeamID(t *testing.T) {
	d := newTestDB(t)
	teamID, _ := d.CreateTeam("ProjectTeam")
	pid, _ := d.CreateProject("ProjectWithTeam", "PWT", teamID, true, "2026-01-01", "2026-12-31")

	p, err := d.GetProject(pid)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.TeamName == "" {
		t.Fatal("expected TeamName to be populated for project with valid team_id")
	}
}

// -----------------------------------------------------------------------
// ListProjectsByTeams — with valid team_id covers TeamName assignment branch
// -----------------------------------------------------------------------

func TestListProjectsByTeams_WithTeamID(t *testing.T) {
	d := newTestDB(t)
	teamID, _ := d.CreateTeam("ByTeam")
	d.CreateProject("TeamProj", "TBT", teamID, true, "2026-01-01", "2026-12-31") //nolint:errcheck

	projects, err := d.ListProjectsByTeams([]int64{teamID})
	if err != nil {
		t.Fatalf("ListProjectsByTeams: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("expected at least 1 project")
	}
	if projects[0].TeamName == "" {
		t.Fatal("expected TeamName to be populated")
	}
}

// -----------------------------------------------------------------------
// GetProjectsReport — project with real team_id covers teamMap[proj.TeamID] ok=true
// -----------------------------------------------------------------------

func TestGetProjectsReport_WithTeamID(t *testing.T) {
	d := newTestDB(t)
	teamID, _ := d.CreateTeam("ReportTeam")
	pid, _ := d.CreateProject("ReportProj", "RP", teamID, true, "2026-01-01", "2026-12-31")

	uid := seedUser(t, d, "reportteam@test.com")
	d.SetProjectTimeEntry(uid, pid, 2026, 6, 2.5) //nolint:errcheck

	users, _ := d.ListUsers()
	userMap := make(map[int64]models.User, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}

	rows, err := d.GetProjectsReport([]int64{pid}, []string{"2026-06"}, userMap)
	if err != nil {
		t.Fatalf("GetProjectsReport: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected at least 1 report row")
	}
}
