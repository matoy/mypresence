package db

import (
	"testing"
)

// ─── project_members ──────────────────────────────────────────────────────────

func TestGetProjectMembers_EmptyByDefault(t *testing.T) {
	d := newTestDB(t)
	pid := seedProject(t, d, "Open", "OPEN")

	ids, err := d.GetProjectMembers(pid)
	if err != nil {
		t.Fatalf("GetProjectMembers: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 members, got %d", len(ids))
	}
}

func TestSetAndGetProjectMembers(t *testing.T) {
	d := newTestDB(t)
	pid := seedProject(t, d, "Restricted", "RESTR")
	uid1, _ := d.CreateLocalUser("m1@test.com", "M1", "pass")
	uid2, _ := d.CreateLocalUser("m2@test.com", "M2", "pass")

	if err := d.SetProjectMembers(pid, []int64{uid1, uid2}); err != nil {
		t.Fatalf("SetProjectMembers: %v", err)
	}
	ids, err := d.GetProjectMembers(pid)
	if err != nil {
		t.Fatalf("GetProjectMembers: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 members, got %d", len(ids))
	}
}

func TestSetProjectMembers_ClearsExisting(t *testing.T) {
	d := newTestDB(t)
	pid := seedProject(t, d, "ClearTest", "CLR")
	uid1, _ := d.CreateLocalUser("clr1@test.com", "Clr1", "pass")
	uid2, _ := d.CreateLocalUser("clr2@test.com", "Clr2", "pass")

	d.SetProjectMembers(pid, []int64{uid1, uid2}) //nolint:errcheck
	// Replace with only uid1
	if err := d.SetProjectMembers(pid, []int64{uid1}); err != nil {
		t.Fatalf("SetProjectMembers: %v", err)
	}
	ids, _ := d.GetProjectMembers(pid)
	if len(ids) != 1 || ids[0] != uid1 {
		t.Fatalf("expected [%d], got %v", uid1, ids)
	}
}

func TestSetProjectMembers_EmptyRemovesAll(t *testing.T) {
	d := newTestDB(t)
	pid := seedProject(t, d, "EmptyMem", "EMPM")
	uid, _ := d.CreateLocalUser("empm@test.com", "Empm", "pass")

	d.SetProjectMembers(pid, []int64{uid}) //nolint:errcheck
	if err := d.SetProjectMembers(pid, []int64{}); err != nil {
		t.Fatalf("SetProjectMembers empty: %v", err)
	}
	ids, _ := d.GetProjectMembers(pid)
	if len(ids) != 0 {
		t.Fatalf("expected 0 members after clearing, got %d", len(ids))
	}
}

// ─── IsUserAssignedToProject ──────────────────────────────────────────────────

func TestIsUserAssignedToProject_OpenProject(t *testing.T) {
	d := newTestDB(t)
	pid := seedProject(t, d, "Open2", "OPN2")
	uid, _ := d.CreateLocalUser("open2u@test.com", "Open2U", "pass")

	ok, err := d.IsUserAssignedToProject(uid, pid)
	if err != nil {
		t.Fatalf("IsUserAssignedToProject: %v", err)
	}
	if !ok {
		t.Error("expected open project to be accessible to any user")
	}
}

func TestIsUserAssignedToProject_AssignedUser(t *testing.T) {
	d := newTestDB(t)
	pid := seedProject(t, d, "Restricted2", "RST2")
	uid, _ := d.CreateLocalUser("rst2u@test.com", "Rst2U", "pass")

	d.SetProjectMembers(pid, []int64{uid}) //nolint:errcheck

	ok, err := d.IsUserAssignedToProject(uid, pid)
	if err != nil || !ok {
		t.Fatalf("expected assigned user to have access, err=%v ok=%v", err, ok)
	}
}

func TestIsUserAssignedToProject_NotAssigned(t *testing.T) {
	d := newTestDB(t)
	pid := seedProject(t, d, "Restricted3", "RST3")
	uid1, _ := d.CreateLocalUser("rst3a@test.com", "Rst3A", "pass")
	uid2, _ := d.CreateLocalUser("rst3b@test.com", "Rst3B", "pass")

	d.SetProjectMembers(pid, []int64{uid1}) //nolint:errcheck

	ok, err := d.IsUserAssignedToProject(uid2, pid)
	if err != nil {
		t.Fatalf("IsUserAssignedToProject: %v", err)
	}
	if ok {
		t.Error("expected non-member to have no access")
	}
}

// ─── ListActiveProjectsForMonthAndUser ────────────────────────────────────────

func TestListActiveProjectsForMonthAndUser_OpenProject(t *testing.T) {
	d := newTestDB(t)
	seedProject(t, d, "AnyUser", "ANYU")
	uid, _ := d.CreateLocalUser("anyu@test.com", "AnyU", "pass")

	projects, err := d.ListActiveProjectsForMonthAndUser(2026, 5, uid)
	if err != nil {
		t.Fatalf("ListActiveProjectsForMonthAndUser: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 open project, got %d", len(projects))
	}
}

func TestListActiveProjectsForMonthAndUser_FiltersUnassigned(t *testing.T) {
	d := newTestDB(t)
	pid := seedProject(t, d, "MembOnly", "MBRO")
	uid1, _ := d.CreateLocalUser("mbr1@test.com", "Mbr1", "pass")
	uid2, _ := d.CreateLocalUser("mbr2@test.com", "Mbr2", "pass")

	d.SetProjectMembers(pid, []int64{uid1}) //nolint:errcheck

	// uid1 should see the project
	p1, _ := d.ListActiveProjectsForMonthAndUser(2026, 5, uid1)
	if len(p1) != 1 {
		t.Fatalf("uid1 expected 1 project, got %d", len(p1))
	}
	// uid2 should not see it
	p2, _ := d.ListActiveProjectsForMonthAndUser(2026, 5, uid2)
	if len(p2) != 0 {
		t.Fatalf("uid2 expected 0 projects, got %d", len(p2))
	}
}

func TestListActiveProjectsForMonthAndUser_MixedOpenAndRestricted(t *testing.T) {
	d := newTestDB(t)
	pid1 := seedProject(t, d, "OpenProj", "OPP")
	pid2 := seedProject(t, d, "ClosedProj", "CLP")
	uid, _ := d.CreateLocalUser("mixed@test.com", "Mixed", "pass")
	other, _ := d.CreateLocalUser("other@test.com", "Other", "pass")

	d.SetProjectMembers(pid2, []int64{other}) //nolint:errcheck

	projects, _ := d.ListActiveProjectsForMonthAndUser(2026, 5, uid)
	// uid should see open project (pid1) but not closed one (pid2)
	if len(projects) != 1 {
		t.Fatalf("expected 1 project for uid, got %d", len(projects))
	}
	if projects[0].ID != pid1 {
		t.Fatalf("expected pid1 (%d), got %d", pid1, projects[0].ID)
	}

	// `other` is assigned to pid2 and pid1 is open → should see both
	projectsOther, _ := d.ListActiveProjectsForMonthAndUser(2026, 5, other)
	if len(projectsOther) != 2 {
		t.Fatalf("expected 2 projects for other, got %d", len(projectsOther))
	}
}
