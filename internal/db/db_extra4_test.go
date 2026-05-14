package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// openSQLiteMulti cleanup paths — dirs trigger error on specific sub-DB opens
// -----------------------------------------------------------------------

// TestOpenSQLiteMulti_FloorplanIsDir covers the coreDB+presenceDB cleanup path
// (lines 127-130) executed when floorplan.db cannot be opened.
func TestOpenSQLiteMulti_FloorplanIsDir(t *testing.T) {
	dir := t.TempDir()
	// Create valid core.db and presence.db so the first two opens succeed.
	for _, name := range []string{"core.db", "presence.db"} {
		db, err := sql.Open("sqlite", filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("pre-create %s: %v", name, err)
		}
		db.Close()
	}
	// Make floorplan.db a directory — openSQLiteConn will fail on it.
	if err := os.Mkdir(filepath.Join(dir, "floorplan.db"), 0o755); err != nil {
		t.Fatalf("mkdir floorplan.db: %v", err)
	}

	_, err := openSQLiteMulti(dir, newDialect("sqlite"))
	if err == nil {
		t.Fatal("expected error when floorplan.db is a directory")
	}
}

// TestOpenSQLiteMulti_AuditIsDir covers the core+presence+floorplan cleanup path
// (lines 135-137) executed when audit.db cannot be opened.
func TestOpenSQLiteMulti_AuditIsDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"core.db", "presence.db", "floorplan.db"} {
		db, err := sql.Open("sqlite", filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("pre-create %s: %v", name, err)
		}
		db.Close()
	}
	if err := os.Mkdir(filepath.Join(dir, "audit.db"), 0o755); err != nil {
		t.Fatalf("mkdir audit.db: %v", err)
	}

	_, err := openSQLiteMulti(dir, newDialect("sqlite"))
	if err == nil {
		t.Fatal("expected error when audit.db is a directory")
	}
}

// TestOpenSQLiteMulti_ProjectsIsDir covers the full cleanup path (lines 138-140)
// executed when projects.db cannot be opened.
func TestOpenSQLiteMulti_ProjectsIsDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"core.db", "presence.db", "floorplan.db", "audit.db"} {
		db, err := sql.Open("sqlite", filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("pre-create %s: %v", name, err)
		}
		db.Close()
	}
	if err := os.Mkdir(filepath.Join(dir, "projects.db"), 0o755); err != nil {
		t.Fatalf("mkdir projects.db: %v", err)
	}

	_, err := openSQLiteMulti(dir, newDialect("sqlite"))
	if err == nil {
		t.Fatal("expected error when projects.db is a directory")
	}
}

// -----------------------------------------------------------------------
// migrateLegacy — PRAGMA failure when app.db is a directory (lines 575-577)
// -----------------------------------------------------------------------

// TestOpenSQLiteMulti_AppDbIsDir creates app.db as a directory so that
// migrateLegacy is called and fails on the PRAGMA, logging a warning but
// not failing Open().
func TestOpenSQLiteMulti_AppDbIsDir(t *testing.T) {
	dir := t.TempDir()
	// Create app.db as a directory — migrateLegacy will try to open it.
	if err := os.Mkdir(filepath.Join(dir, "app.db"), 0o755); err != nil {
		t.Fatalf("mkdir app.db: %v", err)
	}

	dl := newDialect("sqlite")
	d, err := openSQLiteMulti(dir, dl)
	if err != nil {
		t.Fatalf("expected Open to succeed despite bad app.db, got: %v", err)
	}
	defer d.Close()
}

// -----------------------------------------------------------------------
// CheckPassword — legacy non-matching plaintext (line 1134 return false)
// -----------------------------------------------------------------------

// TestCheckPassword_LegacyNonMatching covers the `return false` at the bottom
// of CheckPassword when storedHash doesn't start with "$2" and doesn't equal
// the plain password.
func TestCheckPassword_LegacyNonMatching(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "legacynomatch@test.com")

	// Force a plaintext-style hash.
	d.core.Exec("UPDATE users SET password_hash = 'storedplain' WHERE id = ?", uid) //nolint:errcheck

	result := d.CheckPassword(uid, "storedplain", "differentpassword")
	if result {
		t.Fatal("expected false when plaintext hash doesn't match password")
	}
}

// -----------------------------------------------------------------------
// ClearPresences — empty dates → early return nil (line 1486)
// -----------------------------------------------------------------------

func TestClearPresences_EmptyDates(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "clearpresencesempty@test.com")
	if err := d.ClearPresences(uid, []string{}, "full"); err != nil {
		t.Fatalf("expected nil for empty dates, got: %v", err)
	}
}

// -----------------------------------------------------------------------
// ReserveSeat — empty half defaults to "full" (lines 2168-2170)
// -----------------------------------------------------------------------

func TestReserveSeat_EmptyHalf(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "reserveemptyhalf@test.com")
	statusID := seedOnSiteStatus(t, d)
	_, seatID := seedFloorplanAndSeat(t, d, "EmptyHalf")

	d.SetPresences(uid, []string{"2026-08-01"}, statusID, "") //nolint:errcheck

	// Passing empty half should default to "full" internally.
	if err := d.ReserveSeat(seatID, uid, "2026-08-01", ""); err != nil {
		t.Fatalf("ReserveSeat with empty half: %v", err)
	}
}

// -----------------------------------------------------------------------
// GetSeatsWithStatus — non-conflicting half → conflicts=false → continue
// (line 2078-2079)
// -----------------------------------------------------------------------

// TestGetSeatsWithStatus_NonConflictingHalf covers the `if !conflicts { continue }`
// branch when an existing "AM" reservation doesn't conflict with a "PM" query.
func TestGetSeatsWithStatus_NonConflictingHalf(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "gsws_noc@test.com")
	uid2 := seedUser(t, d, "gsws_noc2@test.com")
	statusID := seedOnSiteStatus(t, d)
	fpID, seatID := seedFloorplanAndSeat(t, d, "NonConflict")

	// uid2 reserves the seat for AM.
	d.SetPresences(uid2, []string{"2026-08-03"}, statusID, "") //nolint:errcheck
	if err := d.ReserveSeat(seatID, uid2, "2026-08-03", "AM"); err != nil {
		t.Fatalf("setup ReserveSeat AM: %v", err)
	}

	// uid queries with PM — the AM reservation of uid2 doesn't conflict.
	d.SetPresences(uid, []string{"2026-08-03"}, statusID, "") //nolint:errcheck
	seats, err := d.GetSeatsWithStatus(fpID, uid, "2026-08-03", "PM")
	if err != nil {
		t.Fatalf("GetSeatsWithStatus: %v", err)
	}
	// Seat should be free for PM since only AM is taken by uid2.
	if len(seats) == 0 {
		t.Fatal("expected at least one seat")
	}
	if seats[0].Status != "free" {
		t.Fatalf("expected seat to be free for PM, got %q", seats[0].Status)
	}
}

// -----------------------------------------------------------------------
// GetSeatsWithStatusForDates — non-overlapping half → halfOverlaps=false → continue
// (line 2139)
// -----------------------------------------------------------------------

// TestGetSeatsWithStatusForDates_NonOverlappingHalf covers the `continue` when
// the reservation half ("AM") doesn't overlap with the query half ("PM").
func TestGetSeatsWithStatusForDates_NonOverlappingHalf(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "gswsfd_noh@test.com")
	uid2 := seedUser(t, d, "gswsfd_noh2@test.com")
	statusID := seedOnSiteStatus(t, d)
	fpID, seatID := seedFloorplanAndSeat(t, d, "NoOverlap")

	// uid2 reserves the seat for AM.
	d.SetPresences(uid2, []string{"2026-08-05"}, statusID, "") //nolint:errcheck
	if err := d.ReserveSeat(seatID, uid2, "2026-08-05", "AM"); err != nil {
		t.Fatalf("setup ReserveSeat AM: %v", err)
	}

	// uid queries with PM — halfOverlaps("AM","PM") = false → continue.
	seats, err := d.GetSeatsWithStatusForDates(fpID, uid, []string{"2026-08-05"}, "PM")
	if err != nil {
		t.Fatalf("GetSeatsWithStatusForDates: %v", err)
	}
	if len(seats) == 0 {
		t.Fatal("expected at least one seat")
	}
	// The AM reservation doesn't overlap with PM — seat should be free.
	if seats[0].Status != "free" {
		t.Fatalf("expected free for PM query, got %q", seats[0].Status)
	}
}

// -----------------------------------------------------------------------
// GetProjectsReport — non-existent projectID → GetProject fails → continue
// (projects.go line 271-272)
// -----------------------------------------------------------------------

// TestGetProjectsReport_NonExistentProject covers the `continue` when GetProject
// returns an error for a project ID that doesn't exist in the DB.
func TestGetProjectsReport_NonExistentProject(t *testing.T) {
	d := newTestDB(t)
	// Pass a non-existent project ID — time entries query returns 0 rows,
	// then GetProject(99999) fails → continue.
	rows, err := d.GetProjectsReport([]int64{99999}, []string{"2026-06"}, map[int64]models.User{})
	if err != nil {
		t.Fatalf("GetProjectsReport: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows for non-existent project, got %d", len(rows))
	}
}
