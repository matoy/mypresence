package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matoy/myPresence/internal/config"

	_ "modernc.org/sqlite"
)

// -----------------------------------------------------------------------
// Open — routing and defaults
// -----------------------------------------------------------------------

func TestOpen_EmptyDriver_DefaultsToSQLite(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(&config.Config{DBDriver: "", DataDir: dir})
	if err != nil {
		t.Fatalf("Open with empty driver: %v", err)
	}
	defer d.Close()
	if d.driver != "sqlite" {
		t.Errorf("expected driver=sqlite, got %q", d.driver)
	}
}

func TestOpen_SQLiteDriver_Explicit(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("Open sqlite: %v", err)
	}
	defer d.Close()
	if d.driver != "sqlite" {
		t.Errorf("expected sqlite driver, got %q", d.driver)
	}
}

func TestOpen_SQLite_SeparateConnections(t *testing.T) {
	// For SQLite the 5 domain connections must be distinct objects.
	dir := t.TempDir()
	d, err := Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	if d.shared != nil {
		t.Error("SQLite mode should not set shared connection")
	}
	// All domain pointers must be non-nil
	for name, conn := range map[string]interface{}{
		"core": d.core, "presence": d.presence, "floorplan": d.floorplan,
		"audit": d.audit, "projects": d.projects,
	} {
		if conn == nil {
			t.Errorf("SQLite domain %q connection is nil", name)
		}
	}
}

func TestOpen_InvalidDriver_ReturnsError(t *testing.T) {
	_, err := Open(&config.Config{
		DBDriver: "oracle", // unsupported
		DBHost:   "localhost",
		DBName:   "test",
		DBUser:   "test",
	})
	if err == nil {
		t.Error("expected error for unsupported driver")
	}
}

func TestOpen_SQLite_MigratesSchema(t *testing.T) {
	// After Open the core tables must exist.
	dir := t.TempDir()
	d, err := Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	assertTablesExist(t, d.core, []string{"users", "teams", "user_teams", "sessions", "personal_access_tokens", "password_reset_tokens"}, "core")
	assertTablesExist(t, d.presence, []string{"statuses", "presences", "holidays", "presence_logs"}, "presence")
	assertTablesExist(t, d.floorplan, []string{"floorplans", "seats", "seat_reservations"}, "floorplan")
	assertTablesExist(t, d.audit, []string{"admin_logs"}, "audit")
	assertTablesExist(t, d.projects, []string{"projects", "project_time_entries"}, "projects")
}

// assertTablesExist checks that each named table is present in the given DB.
func assertTablesExist(t *testing.T, db *rebindDB, tables []string, label string) {
	t.Helper()
	for _, tbl := range tables {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&n); err != nil || n == 0 {
			t.Errorf("%s table %q not found after migration (count=%d, err=%v)", label, tbl, n, err)
		}
	}
}

func TestOpen_SQLite_Idempotent(t *testing.T) {
	// Opening the same directory twice should not fail (migrations are idempotent).
	dir := t.TempDir()
	d1, err := Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	d1.Close()

	d2, err := Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("second Open (should be idempotent): %v", err)
	}
	d2.Close()
}

func TestOpen_SQLite_Ping(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	if err := d.Ping(); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

// -----------------------------------------------------------------------
// buildDSN
// -----------------------------------------------------------------------

func TestBuildDSN_Postgres_Defaults(t *testing.T) {
	cfg := &config.Config{
		DBDriver:   "postgres",
		DBHost:     "dbhost",
		DBName:     "mydb",
		DBUser:     "myuser",
		DBPassword: "mypass",
	}
	dsn, err := buildDSN(cfg, "postgres")
	if err != nil {
		t.Fatalf("buildDSN postgres: %v", err)
	}
	if !strings.Contains(dsn, "host=dbhost") {
		t.Errorf("postgres DSN missing host: %q", dsn)
	}
	if !strings.Contains(dsn, "port=5432") {
		t.Errorf("postgres DSN should default to port 5432: %q", dsn)
	}
	if !strings.Contains(dsn, "dbname=mydb") {
		t.Errorf("postgres DSN missing dbname: %q", dsn)
	}
	if !strings.Contains(dsn, "user=myuser") {
		t.Errorf("postgres DSN missing user: %q", dsn)
	}
	if !strings.Contains(dsn, "password=mypass") {
		t.Errorf("postgres DSN missing password: %q", dsn)
	}
	if !strings.Contains(dsn, "sslmode=disable") {
		t.Errorf("postgres DSN should default sslmode=disable: %q", dsn)
	}
}

func TestBuildDSN_Postgres_CustomPort(t *testing.T) {
	cfg := &config.Config{
		DBDriver: "postgres",
		DBHost:   "localhost",
		DBPort:   "5433",
		DBName:   "db",
		DBUser:   "u",
	}
	dsn, _ := buildDSN(cfg, "postgres")
	if !strings.Contains(dsn, "port=5433") {
		t.Errorf("postgres DSN should use custom port 5433: %q", dsn)
	}
}

func TestBuildDSN_Postgres_SSLMode(t *testing.T) {
	cfg := &config.Config{
		DBDriver:  "postgres",
		DBHost:    "h",
		DBName:    "n",
		DBUser:    "u",
		DBSSLMode: "require",
	}
	dsn, _ := buildDSN(cfg, "postgres")
	if !strings.Contains(dsn, "sslmode=require") {
		t.Errorf("postgres DSN should propagate sslmode: %q", dsn)
	}
}

func TestBuildDSN_MySQL_Defaults(t *testing.T) {
	cfg := &config.Config{
		DBDriver:   "mysql",
		DBHost:     "mariadb",
		DBName:     "mydb",
		DBUser:     "myuser",
		DBPassword: "mypass",
	}
	dsn, err := buildDSN(cfg, "mysql")
	if err != nil {
		t.Fatalf("buildDSN mysql: %v", err)
	}
	if !strings.Contains(dsn, "tcp(mariadb:3306)") {
		t.Errorf("mysql DSN missing host:port: %q", dsn)
	}
	if !strings.Contains(dsn, "mydb") {
		t.Errorf("mysql DSN missing dbname: %q", dsn)
	}
	if !strings.Contains(dsn, "myuser:mypass") {
		t.Errorf("mysql DSN missing credentials: %q", dsn)
	}
	if !strings.Contains(dsn, "parseTime=true") {
		t.Errorf("mysql DSN should always include parseTime=true: %q", dsn)
	}
	if !strings.Contains(dsn, "charset=utf8mb4") {
		t.Errorf("mysql DSN should enforce utf8mb4: %q", dsn)
	}
}

func TestBuildDSN_MySQL_CustomPort(t *testing.T) {
	cfg := &config.Config{
		DBDriver: "mysql",
		DBHost:   "localhost",
		DBPort:   "3307",
		DBName:   "db",
		DBUser:   "u",
	}
	dsn, _ := buildDSN(cfg, "mysql")
	if !strings.Contains(dsn, "tcp(localhost:3307)") {
		t.Errorf("mysql DSN should use custom port 3307: %q", dsn)
	}
}

func TestBuildDSN_MySQL_SSLRequire(t *testing.T) {
	cfg := &config.Config{
		DBDriver:  "mysql",
		DBHost:    "h",
		DBName:    "db",
		DBUser:    "u",
		DBSSLMode: "require",
	}
	dsn, _ := buildDSN(cfg, "mysql")
	if !strings.Contains(dsn, "tls=true") {
		t.Errorf("mysql DSN should set tls=true for SSLMode=require: %q", dsn)
	}
}

func TestBuildDSN_MySQL_SSLSkipVerify(t *testing.T) {
	cfg := &config.Config{
		DBDriver:  "mysql",
		DBHost:    "h",
		DBName:    "db",
		DBUser:    "u",
		DBSSLMode: "skip-verify",
	}
	dsn, _ := buildDSN(cfg, "mysql")
	if !strings.Contains(dsn, "tls=skip-verify") {
		t.Errorf("mysql DSN should set tls=skip-verify: %q", dsn)
	}
}

func TestBuildDSN_SQLServer_Defaults(t *testing.T) {
	cfg := &config.Config{
		DBDriver:   "sqlserver",
		DBHost:     "sqlsrv",
		DBName:     "mydb",
		DBUser:     "myuser",
		DBPassword: "mypass",
	}
	dsn, err := buildDSN(cfg, "sqlserver")
	if err != nil {
		t.Fatalf("buildDSN sqlserver: %v", err)
	}
	if !strings.Contains(dsn, "sqlsrv:1433") {
		t.Errorf("sqlserver DSN missing host:defaultport: %q", dsn)
	}
	if !strings.Contains(dsn, "database=mydb") {
		t.Errorf("sqlserver DSN missing database: %q", dsn)
	}
	if !strings.Contains(dsn, "myuser") {
		t.Errorf("sqlserver DSN missing user: %q", dsn)
	}
}

func TestBuildDSN_SQLServer_CustomPort(t *testing.T) {
	cfg := &config.Config{
		DBDriver: "sqlserver",
		DBHost:   "host",
		DBPort:   "1434",
		DBName:   "db",
		DBUser:   "u",
	}
	dsn, _ := buildDSN(cfg, "sqlserver")
	if !strings.Contains(dsn, "host:1434") {
		t.Errorf("sqlserver DSN should use custom port 1434: %q", dsn)
	}
}

func TestBuildDSN_UnknownDriver_ReturnsError(t *testing.T) {
	_, err := buildDSN(&config.Config{}, "oracle")
	if err == nil {
		t.Error("expected error for unknown driver")
	}
}

// -----------------------------------------------------------------------
// SeedDefaults — idempotency (insertOrIgnore path)
// -----------------------------------------------------------------------

func TestSeedDefaults_Idempotent(t *testing.T) {
	d := newTestDB(t)

	if err := d.SeedDefaults("admin@test.com", "password1"); err != nil {
		t.Fatalf("first SeedDefaults: %v", err)
	}
	// Second call must not fail (INSERT OR IGNORE / ON CONFLICT DO NOTHING)
	if err := d.SeedDefaults("admin@test.com", "password1"); err != nil {
		t.Fatalf("second SeedDefaults (idempotency): %v", err)
	}

	// User must exist exactly once
	var count int
	d.core.QueryRow("SELECT COUNT(*) FROM users WHERE email=?", "admin@test.com").Scan(&count) //nolint:errcheck
	if count != 1 {
		t.Errorf("expected exactly 1 admin user, got %d", count)
	}
}

func TestSeedDefaults_StatusesCreatedOnce(t *testing.T) {
	d := newTestDB(t)
	d.SeedDefaults("admin@test.com", "pass") //nolint:errcheck

	var before int
	d.presence.QueryRow("SELECT COUNT(*) FROM statuses").Scan(&before) //nolint:errcheck

	// Second seed must not duplicate statuses
	d.SeedDefaults("admin@test.com", "pass") //nolint:errcheck

	var after int
	d.presence.QueryRow("SELECT COUNT(*) FROM statuses").Scan(&after) //nolint:errcheck
	if before != after {
		t.Errorf("SeedDefaults duplicated statuses: before=%d after=%d", before, after)
	}
}

// -----------------------------------------------------------------------
// UpsertUser — ON CONFLICT path
// -----------------------------------------------------------------------

func TestUpsertUser_CreatesUser(t *testing.T) {
	d := newTestDB(t)
	u, err := d.UpsertUser("upsert@test.com", "Alice")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if u.Email != "upsert@test.com" {
		t.Errorf("unexpected email: %q", u.Email)
	}
}

func TestUpsertUser_UpdatesName(t *testing.T) {
	d := newTestDB(t)
	d.UpsertUser("upsert2@test.com", "Alice") //nolint:errcheck
	u, err := d.UpsertUser("upsert2@test.com", "Alice Updated")
	if err != nil {
		t.Fatalf("UpsertUser update: %v", err)
	}
	if u.Name != "Alice Updated" {
		t.Errorf("expected updated name, got %q", u.Name)
	}
	// Only one row should exist
	var count int
	d.core.QueryRow("SELECT COUNT(*) FROM users WHERE email=?", "upsert2@test.com").Scan(&count) //nolint:errcheck
	if count != 1 {
		t.Errorf("UpsertUser should not duplicate rows, got %d", count)
	}
}

// -----------------------------------------------------------------------
// AddTeamMember — insertOrIgnore idempotency
// -----------------------------------------------------------------------

func TestAddTeamMember_Idempotent(t *testing.T) {
	d := newTestDB(t)
	userID := seedUser(t, d, "member@test.com")
	res, err := d.core.Exec("INSERT INTO teams (name) VALUES ('TestTeam')")
	if err != nil {
		t.Fatalf("insert team: %v", err)
	}
	teamID, _ := res.LastInsertId()

	if err := d.AddTeamMember(teamID, userID); err != nil {
		t.Fatalf("first AddTeamMember: %v", err)
	}
	// Second call must not error (INSERT OR IGNORE semantics)
	if err := d.AddTeamMember(teamID, userID); err != nil {
		t.Fatalf("second AddTeamMember (idempotency): %v", err)
	}

	var count int
	d.core.QueryRow("SELECT COUNT(*) FROM user_teams WHERE team_id=? AND user_id=?", teamID, userID).Scan(&count) //nolint:errcheck
	if count != 1 {
		t.Errorf("AddTeamMember should not duplicate rows, got %d", count)
	}
}

// -----------------------------------------------------------------------
// migrateLegacy — copies rows from a legacy app.db into domain DBs
// -----------------------------------------------------------------------

func TestMigrateLegacy_CopiesData(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "app.db")

	// Build a minimal legacy SQLite database that mirrors the old single-file schema.
	legacy, err := sql.Open("sqlite", legacyPath)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT UNIQUE NOT NULL, name TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'basic', password_hash TEXT, disabled BOOLEAN NOT NULL DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE teams (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT UNIQUE NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE user_teams (user_id BIGINT NOT NULL, team_id BIGINT NOT NULL)`,
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id BIGINT NOT NULL, expires_at DATETIME NOT NULL)`,
		`CREATE TABLE personal_access_tokens (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id BIGINT NOT NULL, description TEXT NOT NULL DEFAULT '', token_hash TEXT NOT NULL UNIQUE, token_prefix TEXT NOT NULL, expires_at DATETIME, last_used_at DATETIME, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE statuses (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, color TEXT NOT NULL DEFAULT '#3b82f6', billable BOOLEAN DEFAULT 0, on_site BOOLEAN DEFAULT 0, sort_order INTEGER DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE presences (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL, date TEXT NOT NULL, half TEXT NOT NULL DEFAULT 'full', status_id INTEGER NOT NULL)`,
		`CREATE TABLE holidays (id INTEGER PRIMARY KEY AUTOINCREMENT, date TEXT UNIQUE NOT NULL, name TEXT NOT NULL, allow_imputed BOOLEAN DEFAULT 0)`,
		`CREATE TABLE presence_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id BIGINT NOT NULL, actor_id BIGINT NOT NULL, action TEXT NOT NULL, date TEXT NOT NULL, half TEXT NOT NULL DEFAULT 'full', status_id BIGINT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE admin_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, actor_id BIGINT NOT NULL, entity_type TEXT NOT NULL, entity_id BIGINT NOT NULL DEFAULT 0, action TEXT NOT NULL, details TEXT NOT NULL DEFAULT '', created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE floorplans (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, image_path TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE seats (id INTEGER PRIMARY KEY AUTOINCREMENT, floorplan_id BIGINT NOT NULL, label TEXT NOT NULL, x_pct REAL NOT NULL DEFAULT 0, y_pct REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE seat_reservations (id INTEGER PRIMARY KEY AUTOINCREMENT, seat_id BIGINT NOT NULL, user_id BIGINT NOT NULL, date TEXT NOT NULL, half TEXT NOT NULL DEFAULT 'full', created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			_ = legacy.Close()
			t.Fatalf("create legacy table: %v", err)
		}
	}
	legacy.Exec(`INSERT INTO users (email, name, role) VALUES ('migrated@test.com', 'Migrated User', 'basic')`) //nolint:errcheck
	legacy.Exec(`INSERT INTO statuses (name, color) VALUES ('Legacy Status', '#ff0000')`)                       //nolint:errcheck
	_ = legacy.Close()

	// Open opens the new multi-file layout; finding app.db triggers migrateLegacy.
	d, err := Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("Open with legacy db: %v", err)
	}
	defer d.Close()

	// Migrated user must appear in core.db.
	var userCount int
	d.core.QueryRow("SELECT COUNT(*) FROM users WHERE email='migrated@test.com'").Scan(&userCount) //nolint:errcheck
	if userCount != 1 {
		t.Errorf("expected migrated user in core.db, got count=%d", userCount)
	}

	// Migrated status must appear in presence.db.
	var statusCount int
	d.presence.QueryRow("SELECT COUNT(*) FROM statuses WHERE name='Legacy Status'").Scan(&statusCount) //nolint:errcheck
	if statusCount != 1 {
		t.Errorf("expected migrated status in presence.db, got count=%d", statusCount)
	}

	// app.db should be renamed to app.db.bak.
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Error("expected app.db to be renamed, but it still exists")
	}
	if _, err := os.Stat(legacyPath + ".bak"); os.IsNotExist(err) {
		t.Error("expected app.db.bak to exist after migration")
	}
}

// Silence unused-import warning for the sqlite driver blank import.
var _ = strings.Contains
