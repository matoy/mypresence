package db

import (
	"testing"

	"github.com/matoy/myPresence/internal/models"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func seedProject(t *testing.T, d *DB, name, code string) int64 {
	t.Helper()
	id, err := d.CreateProject(name, code, 0, true, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("seedProject: %v", err)
	}
	return id
}

// ─── ListProjects ─────────────────────────────────────────────────────────────

func TestListProjects_EmptyDB_ReturnsNil(t *testing.T) {
	d := newTestDB(t)
	projects, err := d.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}

func TestListProjects_ReturnsAllProjects(t *testing.T) {
	d := newTestDB(t)
	seedProject(t, d, "Alpha", "ALPHA")
	seedProject(t, d, "Beta", "BETA")

	projects, err := d.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

// ─── GetProject ───────────────────────────────────────────────────────────────

func TestGetProject_ReturnsCorrectProject(t *testing.T) {
	d := newTestDB(t)
	id := seedProject(t, d, "Gamma", "GAMMA")

	p, err := d.GetProject(id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.Name != "Gamma" || p.Code != "GAMMA" {
		t.Errorf("unexpected project: %+v", p)
	}
	if !p.Active {
		t.Error("expected project to be active")
	}
}

func TestGetProject_NotFound_ReturnsError(t *testing.T) {
	d := newTestDB(t)
	_, err := d.GetProject(9999)
	if err == nil {
		t.Error("expected error for non-existent project")
	}
}

// ─── CreateProject ────────────────────────────────────────────────────────────

func TestCreateProject_ReturnsPositiveID(t *testing.T) {
	d := newTestDB(t)
	id, err := d.CreateProject("Delta", "DELTA", 0, true, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}
}

func TestCreateProject_InactiveProject(t *testing.T) {
	d := newTestDB(t)
	id, err := d.CreateProject("Epsilon", "EPS", 0, false, "2025-01-01", "2025-12-31")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	p, err := d.GetProject(id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.Active {
		t.Error("expected project to be inactive")
	}
}

// ─── UpdateProject ────────────────────────────────────────────────────────────

func TestUpdateProject_ChangesFields(t *testing.T) {
	d := newTestDB(t)
	id := seedProject(t, d, "Zeta", "ZETA")

	err := d.UpdateProject(id, "Zeta Updated", "ZETA2", 0, false, "2026-06-01", "2026-12-31")
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	p, _ := d.GetProject(id)
	if p.Name != "Zeta Updated" {
		t.Errorf("expected updated name, got %q", p.Name)
	}
	if p.Code != "ZETA2" {
		t.Errorf("expected updated code, got %q", p.Code)
	}
	if p.Active {
		t.Error("expected project to be inactive after update")
	}
}

// ─── ListActiveProjectsForMonth ───────────────────────────────────────────────

func TestListActiveProjectsForMonth_FiltersInactive(t *testing.T) {
	d := newTestDB(t)
	// Active and within range
	d.CreateProject("Active", "ACT", 0, true, "2026-01-01", "2026-12-31") //nolint:errcheck
	// Active but already ended before May 2026
	d.CreateProject("Ended", "END", 0, true, "2025-01-01", "2026-04-30") //nolint:errcheck
	// Inactive
	d.CreateProject("Inactive", "INA", 0, false, "2026-01-01", "2026-12-31") //nolint:errcheck

	projects, err := d.ListActiveProjectsForMonth(2026, 5)
	if err != nil {
		t.Fatalf("ListActiveProjectsForMonth: %v", err)
	}
	if len(projects) != 1 {
		t.Errorf("expected 1 active project for May 2026, got %d", len(projects))
	}
	if projects[0].Name != "Active" {
		t.Errorf("unexpected project: %q", projects[0].Name)
	}
}

// ─── SetProjectTimeEntry / GetUserTotalDeclaredForMonth ──────────────────────

func TestSetProjectTimeEntry_InsertsEntry(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "user@test.com")
	projID := seedProject(t, d, "Project X", "PX")

	err := d.SetProjectTimeEntry(userID, projID, 2026, 5, 3.5)
	if err != nil {
		t.Fatalf("SetProjectTimeEntry: %v", err)
	}

	total, err := d.GetUserTotalDeclaredForMonth(userID, 2026, 5)
	if err != nil {
		t.Fatalf("GetUserTotalDeclaredForMonth: %v", err)
	}
	if total != 3.5 {
		t.Errorf("expected 3.5 declared days, got %v", total)
	}
}

func TestSetProjectTimeEntry_UpdatesExisting(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "user2@test.com")
	projID := seedProject(t, d, "Project Y", "PY")

	d.SetProjectTimeEntry(userID, projID, 2026, 5, 2.0) //nolint:errcheck
	d.SetProjectTimeEntry(userID, projID, 2026, 5, 4.0) //nolint:errcheck

	total, _ := d.GetUserTotalDeclaredForMonth(userID, 2026, 5)
	if total != 4.0 {
		t.Errorf("expected 4.0 after update, got %v", total)
	}
}

func TestSetProjectTimeEntry_ZeroDeletesEntry(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "user3@test.com")
	projID := seedProject(t, d, "Project Z", "PZ")

	d.SetProjectTimeEntry(userID, projID, 2026, 5, 5.0) //nolint:errcheck
	d.SetProjectTimeEntry(userID, projID, 2026, 5, 0)   //nolint:errcheck

	total, _ := d.GetUserTotalDeclaredForMonth(userID, 2026, 5)
	if total != 0 {
		t.Errorf("expected 0 after deletion, got %v", total)
	}
}

func TestGetUserTotalDeclaredForMonth_SumsAcrossProjects(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "user4@test.com")
	projID1 := seedProject(t, d, "Proj 1", "P1")
	projID2 := seedProject(t, d, "Proj 2", "P2")

	d.SetProjectTimeEntry(userID, projID1, 2026, 5, 3.0) //nolint:errcheck
	d.SetProjectTimeEntry(userID, projID2, 2026, 5, 2.5) //nolint:errcheck

	total, err := d.GetUserTotalDeclaredForMonth(userID, 2026, 5)
	if err != nil {
		t.Fatalf("GetUserTotalDeclaredForMonth: %v", err)
	}
	if total != 5.5 {
		t.Errorf("expected 5.5, got %v", total)
	}
}

func TestGetUserTotalDeclaredForMonth_EmptyReturnsZero(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "user5@test.com")

	total, err := d.GetUserTotalDeclaredForMonth(userID, 2026, 5)
	if err != nil {
		t.Fatalf("GetUserTotalDeclaredForMonth: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0, got %v", total)
	}
}

// ─── GetUserProjectEntriesForMonth ────────────────────────────────────────────

func TestGetUserProjectEntriesForMonth_ReturnsEntries(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "user6@test.com")
	projID := seedProject(t, d, "Proj A", "PA")

	d.SetProjectTimeEntry(userID, projID, 2026, 4, 1.0) //nolint:errcheck
	d.SetProjectTimeEntry(userID, projID, 2026, 5, 2.0) //nolint:errcheck

	entries, err := d.GetUserProjectEntriesForMonth(userID, 2026, 5)
	if err != nil {
		t.Fatalf("GetUserProjectEntriesForMonth: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for May, got %d", len(entries))
	}
	if entries[0].Days != 2.0 {
		t.Errorf("expected 2.0 days, got %v", entries[0].Days)
	}
}

func TestGetUserProjectEntriesForMonth_Empty(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "user7@test.com")

	entries, err := d.GetUserProjectEntriesForMonth(userID, 2026, 5)
	if err != nil {
		t.Fatalf("GetUserProjectEntriesForMonth: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// ─── ListProjectsByTeams ────────────────────────────────────────────────────

func TestListProjectsByTeams_NilReturnsAll(t *testing.T) {
	d := newTestDB(t)
	seedProject(t, d, "All 1", "A1")
	seedProject(t, d, "All 2", "A2")

	projects, err := d.ListProjectsByTeams(nil)
	if err != nil {
		t.Fatalf("ListProjectsByTeams(nil): %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

func TestListProjectsByTeams_EmptyReturnsNil(t *testing.T) {
	d := newTestDB(t)
	projects, err := d.ListProjectsByTeams([]int64{})
	if err != nil {
		t.Fatalf("ListProjectsByTeams(empty): %v", err)
	}
	if projects != nil {
		t.Fatalf("expected nil projects for empty team list, got %#v", projects)
	}
}

func TestListProjectsByTeams_FiltersByTeam(t *testing.T) {
	d := newTestDB(t)
	resA, err := d.core.Exec("INSERT INTO teams (name) VALUES ('Team A')")
	if err != nil {
		t.Fatalf("insert team A: %v", err)
	}
	teamA, _ := resA.LastInsertId()
	resB, err := d.core.Exec("INSERT INTO teams (name) VALUES ('Team B')")
	if err != nil {
		t.Fatalf("insert team B: %v", err)
	}
	teamB, _ := resB.LastInsertId()

	if _, err := d.CreateProject("P A", "PA", teamA, true, "2026-01-01", "2026-12-31"); err != nil {
		t.Fatalf("CreateProject A: %v", err)
	}
	if _, err := d.CreateProject("P B", "PB", teamB, true, "2026-01-01", "2026-12-31"); err != nil {
		t.Fatalf("CreateProject B: %v", err)
	}

	projects, err := d.ListProjectsByTeams([]int64{teamA})
	if err != nil {
		t.Fatalf("ListProjectsByTeams(filter): %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].Code != "PA" || projects[0].TeamID != teamA {
		t.Fatalf("unexpected project: %+v", projects[0])
	}
}

// ─── GetProjectsReport ──────────────────────────────────────────────────────

func TestGetProjectsReport_EmptyProjectIDs(t *testing.T) {
	d := newTestDB(t)
	rows, err := d.GetProjectsReport(nil, []string{"2026-05"}, map[int64]models.User{})
	if err != nil {
		t.Fatalf("GetProjectsReport(nil): %v", err)
	}
	if rows != nil {
		t.Fatalf("expected nil rows, got %#v", rows)
	}
}

func TestGetProjectsReport_AggregatesMonthlyAndTotal(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "report-user@test.com")
	pid := seedProject(t, d, "Report Project", "RP")

	if err := d.SetProjectTimeEntry(uid, pid, 2026, 5, 2.5); err != nil {
		t.Fatalf("SetProjectTimeEntry 05: %v", err)
	}
	if err := d.SetProjectTimeEntry(uid, pid, 2026, 6, 1.0); err != nil {
		t.Fatalf("SetProjectTimeEntry 06: %v", err)
	}

	monthKeys := []string{"2026-05", "2026-06"}
	userMap := map[int64]models.User{uid: {ID: uid, Email: "report-user@test.com", Name: "Report User"}}
	rows, err := d.GetProjectsReport([]int64{pid}, monthKeys, userMap)
	if err != nil {
		t.Fatalf("GetProjectsReport: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].TotalDays != 3.5 {
		t.Fatalf("expected TotalDays=3.5, got %v", rows[0].TotalDays)
	}
	if rows[0].MonthTotals["2026-05"] != 2.5 || rows[0].MonthTotals["2026-06"] != 1.0 {
		t.Fatalf("unexpected month totals: %#v", rows[0].MonthTotals)
	}
	if len(rows[0].UserRows) != 1 || rows[0].UserRows[0].TotalDays != 3.5 {
		t.Fatalf("unexpected user rows: %#v", rows[0].UserRows)
	}
}
