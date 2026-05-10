package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// corruptFile writes invalid bytes to path so SQLite PRAGMA execution fails.
func corruptFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("not a sqlite database\x00\x01\x02\x03"), 0644); err != nil {
		t.Fatalf("corruptFile: %v", err)
	}
}

// -----------------------------------------------------------------------
// openSQLiteMulti — per-file error paths
// -----------------------------------------------------------------------

// TestOpenSQLiteMulti_PresenceFails covers:
//   - openSQLiteConn PRAGMA error path (_ = db.Close(); return nil, err)
//   - openSQLiteMulti: _ = coreDB.Close(); return nil, fmt.Errorf("open presence.db: %w", err)
func TestOpenSQLiteMulti_PresenceFails(t *testing.T) {
	dir := t.TempDir()
	corruptFile(t, filepath.Join(dir, "presence.db"))
	_, err := openSQLiteMulti(dir, newDialect("sqlite"))
	if err == nil {
		t.Fatal("expected error for corrupt presence.db")
	}
}

// TestOpenSQLiteMulti_FloorplanFails covers:
//   - openSQLiteMulti: _ = coreDB.Close(); _ = presenceDB.Close(); return nil, fmt.Errorf("open floorplan.db: %w", err)
func TestOpenSQLiteMulti_FloorplanFails(t *testing.T) {
	dir := t.TempDir()
	corruptFile(t, filepath.Join(dir, "floorplan.db"))
	_, err := openSQLiteMulti(dir, newDialect("sqlite"))
	if err == nil {
		t.Fatal("expected error for corrupt floorplan.db")
	}
}

// TestOpenSQLiteMulti_AuditFails covers the audit cleanup path.
func TestOpenSQLiteMulti_AuditFails(t *testing.T) {
	dir := t.TempDir()
	corruptFile(t, filepath.Join(dir, "audit.db"))
	_, err := openSQLiteMulti(dir, newDialect("sqlite"))
	if err == nil {
		t.Fatal("expected error for corrupt audit.db")
	}
}

// TestOpenSQLiteMulti_ProjectsFails covers the projects cleanup path.
func TestOpenSQLiteMulti_ProjectsFails(t *testing.T) {
	dir := t.TempDir()
	corruptFile(t, filepath.Join(dir, "projects.db"))
	_, err := openSQLiteMulti(dir, newDialect("sqlite"))
	if err == nil {
		t.Fatal("expected error for corrupt projects.db")
	}
}

// TestOpenSQLiteMulti_CorruptLegacyAppDB covers:
//   - openSQLiteMulti: log.Printf("WARNING: legacy migration from app.db failed: %v", err)
//   - migrateLegacy: return fmt.Errorf("open legacy: %w", err)  [via sql.Open path]
//
// A corrupt app.db makes migrateLegacy fail, but openSQLiteMulti treats it as non-fatal.
func TestOpenSQLiteMulti_CorruptLegacyAppDB(t *testing.T) {
	dir := t.TempDir()
	corruptFile(t, filepath.Join(dir, "app.db"))
	d, err := openSQLiteMulti(dir, newDialect("sqlite"))
	if err != nil {
		t.Fatalf("expected success despite corrupt app.db (non-fatal): %v", err)
	}
	d.Close()
}

// -----------------------------------------------------------------------
// Ping — per-sub-db error paths
// -----------------------------------------------------------------------

// TestPing_SharedMode covers the d.shared != nil branch: return d.shared.Ping()
func TestPing_SharedMode(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck
	d := &DB{shared: conn}
	if err := d.Ping(); err != nil {
		t.Fatalf("Ping shared mode: %v", err)
	}
}

// TestPing_CoreError covers: return fmt.Errorf("core.db: %w", err)
func TestPing_CoreError(t *testing.T) {
	d := newTestDB(t)
	d.core.Close() //nolint:errcheck
	if err := d.Ping(); err == nil {
		t.Fatal("expected error after closing core")
	}
}

// TestPing_PresenceError covers: return fmt.Errorf("presence.db: %w", err)
func TestPing_PresenceError(t *testing.T) {
	d := newTestDB(t)
	d.presence.Close() //nolint:errcheck
	if err := d.Ping(); err == nil {
		t.Fatal("expected error after closing presence")
	}
}

// TestPing_FloorplanError covers: return fmt.Errorf("floorplan.db: %w", err)
func TestPing_FloorplanError(t *testing.T) {
	d := newTestDB(t)
	d.floorplan.Close() //nolint:errcheck
	if err := d.Ping(); err == nil {
		t.Fatal("expected error after closing floorplan")
	}
}

// TestPing_AuditError covers: return fmt.Errorf("audit.db: %w", err)
func TestPing_AuditError(t *testing.T) {
	d := newTestDB(t)
	d.audit.Close() //nolint:errcheck
	if err := d.Ping(); err == nil {
		t.Fatal("expected error after closing audit")
	}
}

// TestPing_ProjectsError covers: return fmt.Errorf("projects.db: %w", err)
func TestPing_ProjectsError(t *testing.T) {
	d := newTestDB(t)
	d.projects.Close() //nolint:errcheck
	if err := d.Ping(); err == nil {
		t.Fatal("expected error after closing projects")
	}
}

// -----------------------------------------------------------------------
// Close — shared mode
// -----------------------------------------------------------------------

// TestClose_SharedMode covers the d.shared != nil branch in Close().
func TestClose_SharedMode(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	dl := newDialect("sqlite")
	wrapped := newRebindDB(conn, dl)
	d := &DB{
		shared:    conn,
		core:      wrapped,
		presence:  wrapped,
		floorplan: wrapped,
		audit:     wrapped,
		projects:  wrapped,
	}
	d.Close() // covers: _ = d.shared.Close(); return
}

// -----------------------------------------------------------------------
// migrateLegacy / copyLegacyRows — valid empty legacy app.db
// -----------------------------------------------------------------------

// TestOpenSQLiteMulti_WithValidEmptyLegacyAppDB creates a valid but empty SQLite
// app.db so that migrateLegacy runs its jobs loop and copyLegacyRows is called.
// Since the legacy tables don't exist, copyLegacyRows returns nil (table-not-found path).
func TestOpenSQLiteMulti_WithValidEmptyLegacyAppDB(t *testing.T) {
	dir := t.TempDir()

	// Create a minimal valid SQLite file as app.db.
	legacyConn, err := sql.Open("sqlite", filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Force the connection open so the file is created.
	if _, err := legacyConn.Exec("SELECT 1"); err != nil {
		legacyConn.Close() //nolint:errcheck
		t.Fatalf("init legacy: %v", err)
	}
	legacyConn.Close() //nolint:errcheck

	// openSQLiteMulti should succeed: legacy migration runs but tables are missing → non-fatal.
	d, err := openSQLiteMulti(dir, newDialect("sqlite"))
	if err != nil {
		t.Fatalf("expected success with valid empty app.db: %v", err)
	}
	d.Close()
}
