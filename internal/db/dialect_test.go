package db

import (
	"strings"
	"testing"
)

// -----------------------------------------------------------------------
// rebind
// -----------------------------------------------------------------------

func TestRebind_SQLite_Unchanged(t *testing.T) {
	dl := newDialect("sqlite")
	q := "SELECT * FROM users WHERE id = ? AND email = ?"
	if got := dl.rebind(q); got != q {
		t.Errorf("sqlite rebind should be identity, got %q", got)
	}
}

func TestRebind_MySQL_Unchanged(t *testing.T) {
	dl := newDialect("mysql")
	q := "SELECT * FROM users WHERE id = ? AND email = ?"
	if got := dl.rebind(q); got != q {
		t.Errorf("mysql rebind should be identity, got %q", got)
	}
}

func TestRebind_Postgres_Positional(t *testing.T) {
	dl := newDialect("postgres")
	got := dl.rebind("SELECT * FROM users WHERE id = ? AND email = ?")
	want := "SELECT * FROM users WHERE id = $1 AND email = $2"
	if got != want {
		t.Errorf("postgres rebind: got %q, want %q", got, want)
	}
}

func TestRebind_SQLServer_Positional(t *testing.T) {
	dl := newDialect("sqlserver")
	got := dl.rebind("SELECT * FROM users WHERE id = ? AND email = ?")
	want := "SELECT * FROM users WHERE id = @p1 AND email = @p2"
	if got != want {
		t.Errorf("sqlserver rebind: got %q, want %q", got, want)
	}
}

func TestRebind_NoPlaceholders_Unchanged(t *testing.T) {
	for _, driver := range []string{"sqlite", "mysql", "postgres", "sqlserver"} {
		dl := newDialect(driver)
		q := "SELECT COUNT(*) FROM users"
		if got := dl.rebind(q); got != q {
			t.Errorf("[%s] rebind with no placeholders changed query: %q", driver, got)
		}
	}
}

func TestRebindPositional_CountsCorrectly(t *testing.T) {
	got := rebindPositional("INSERT INTO t (a,b,c) VALUES (?,?,?)", "$")
	want := "INSERT INTO t (a,b,c) VALUES ($1,$2,$3)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// -----------------------------------------------------------------------
// now()
// -----------------------------------------------------------------------

func TestNow_Drivers(t *testing.T) {
	cases := []struct {
		driver string
		want   string
	}{
		{"sqlite", "datetime('now')"},
		{"mysql", "NOW()"},
		{"postgres", "NOW()"},
		{"sqlserver", "GETDATE()"},
	}
	for _, tc := range cases {
		dl := newDialect(tc.driver)
		if got := dl.now(); got != tc.want {
			t.Errorf("[%s] now() = %q, want %q", tc.driver, got, tc.want)
		}
	}
}

// -----------------------------------------------------------------------
// autoincrement()
// -----------------------------------------------------------------------

func TestAutoincrement_ContainsKeyword(t *testing.T) {
	cases := map[string]string{
		"sqlite":    "AUTOINCREMENT",
		"postgres":  "BIGSERIAL",
		"mysql":     "AUTO_INCREMENT",
		"sqlserver": "IDENTITY",
	}
	for driver, substr := range cases {
		dl := newDialect(driver)
		got := dl.autoincrement()
		if !strings.Contains(got, substr) {
			t.Errorf("[%s] autoincrement() = %q, want to contain %q", driver, got, substr)
		}
	}
}

// -----------------------------------------------------------------------
// boolType / datetimeType / realType / textType
// -----------------------------------------------------------------------

func TestBoolType(t *testing.T) {
	if got := newDialect("sqlserver").boolType(); got != "BIT" {
		t.Errorf("sqlserver boolType = %q, want BIT", got)
	}
	for _, d := range []string{"sqlite", "mysql", "postgres"} {
		if got := newDialect(d).boolType(); got != "BOOLEAN" {
			t.Errorf("[%s] boolType = %q, want BOOLEAN", d, got)
		}
	}
}

func TestBoolDefault(t *testing.T) {
	cases := []struct {
		driver    string
		value     bool
		wantFalse string
		wantTrue  string
	}{
		{"sqlite", false, "0", "1"},
		{"mysql", false, "0", "1"},
		{"sqlserver", false, "0", "1"},
		{"postgres", false, "FALSE", "TRUE"},
	}
	for _, tc := range cases {
		dl := newDialect(tc.driver)
		if got := dl.boolDefault(false); got != tc.wantFalse {
			t.Errorf("[%s] boolDefault(false) = %q, want %q", tc.driver, got, tc.wantFalse)
		}
		if got := dl.boolDefault(true); got != tc.wantTrue {
			t.Errorf("[%s] boolDefault(true) = %q, want %q", tc.driver, got, tc.wantTrue)
		}
	}
}

func TestDatetimeType(t *testing.T) {
	cases := map[string]string{
		"sqlite":    "DATETIME",
		"mysql":     "DATETIME",
		"postgres":  "TIMESTAMPTZ",
		"sqlserver": "DATETIME2",
	}
	for driver, want := range cases {
		got := newDialect(driver).datetimeType()
		if got != want {
			t.Errorf("[%s] datetimeType = %q, want %q", driver, got, want)
		}
	}
}

func TestRealType(t *testing.T) {
	if got := newDialect("sqlserver").realType(); got != "FLOAT" {
		t.Errorf("sqlserver realType = %q, want FLOAT", got)
	}
	for _, d := range []string{"sqlite", "mysql", "postgres"} {
		if got := newDialect(d).realType(); got != "REAL" {
			t.Errorf("[%s] realType = %q, want REAL", d, got)
		}
	}
}

func TestTextType(t *testing.T) {
	if got := newDialect("sqlserver").textType(); !strings.Contains(got, "NVARCHAR") {
		t.Errorf("sqlserver textType = %q, want NVARCHAR", got)
	}
	for _, d := range []string{"sqlite", "mysql", "postgres"} {
		if got := newDialect(d).textType(); got != "TEXT" {
			t.Errorf("[%s] textType = %q, want TEXT", d, got)
		}
	}
}

// -----------------------------------------------------------------------
// insertOrIgnore()
// -----------------------------------------------------------------------

func TestInsertOrIgnore_SQLite(t *testing.T) {
	dl := newDialect("sqlite")
	got := dl.insertOrIgnore("users", []string{"email", "name"}, "?, ?")
	if !strings.HasPrefix(got, "INSERT OR IGNORE INTO users") {
		t.Errorf("sqlite insertOrIgnore = %q", got)
	}
}

func TestInsertOrIgnore_MySQL(t *testing.T) {
	dl := newDialect("mysql")
	got := dl.insertOrIgnore("users", []string{"email", "name"}, "?, ?")
	if !strings.HasPrefix(got, "INSERT IGNORE INTO users") {
		t.Errorf("mysql insertOrIgnore = %q", got)
	}
}

func TestInsertOrIgnore_Postgres(t *testing.T) {
	dl := newDialect("postgres")
	got := dl.insertOrIgnore("users", []string{"email", "name"}, "?, ?")
	if !strings.Contains(got, "ON CONFLICT DO NOTHING") {
		t.Errorf("postgres insertOrIgnore = %q, want ON CONFLICT DO NOTHING", got)
	}
}

func TestInsertOrIgnore_SQLServer_TryCatch(t *testing.T) {
	dl := newDialect("sqlserver")
	got := dl.insertOrIgnore("users", []string{"email", "name"}, "?, ?")
	if !strings.Contains(got, "BEGIN TRY") || !strings.Contains(got, "BEGIN CATCH") {
		t.Errorf("sqlserver insertOrIgnore = %q, want TRY/CATCH form", got)
	}
}

func TestInsertOrIgnore_AllDrivers_ContainTableName(t *testing.T) {
	for _, driver := range []string{"sqlite", "mysql", "postgres", "sqlserver"} {
		dl := newDialect(driver)
		got := dl.insertOrIgnore("mytable", []string{"col"}, "?")
		if !strings.Contains(got, "mytable") {
			t.Errorf("[%s] insertOrIgnore missing table name: %q", driver, got)
		}
	}
}

// -----------------------------------------------------------------------
// upsertOnConflict()
// -----------------------------------------------------------------------

func TestUpsertOnConflict_SQLite(t *testing.T) {
	dl := newDialect("sqlite")
	got := dl.upsertOnConflict("presences", []string{"user_id", "date", "status_id"}, "?, ?, ?", "user_id, date", "status_id = excluded.status_id")
	if !strings.Contains(got, "ON CONFLICT(user_id, date) DO UPDATE SET") {
		t.Errorf("sqlite upsert = %q", got)
	}
}

func TestUpsertOnConflict_Postgres(t *testing.T) {
	dl := newDialect("postgres")
	got := dl.upsertOnConflict("presences", []string{"user_id", "date", "status_id"}, "?, ?, ?", "user_id, date", "status_id = excluded.status_id")
	if !strings.Contains(got, "ON CONFLICT(user_id, date) DO UPDATE SET") {
		t.Errorf("postgres upsert = %q", got)
	}
	if !strings.Contains(got, "excluded.status_id") {
		t.Errorf("postgres upsert missing excluded ref: %q", got)
	}
}

func TestUpsertOnConflict_MySQL_DuplicateKey(t *testing.T) {
	dl := newDialect("mysql")
	got := dl.upsertOnConflict("presences", []string{"user_id", "date", "status_id"}, "?, ?, ?", "user_id, date", "status_id = excluded.status_id")
	if !strings.Contains(got, "ON DUPLICATE KEY UPDATE") {
		t.Errorf("mysql upsert = %q, want ON DUPLICATE KEY UPDATE", got)
	}
	// MySQL uses VALUES(col) syntax, not excluded.col
	if !strings.Contains(got, "VALUES(status_id)") {
		t.Errorf("mysql upsert should use VALUES() syntax: %q", got)
	}
}

func TestUpsertOnConflict_SQLServer_Merge(t *testing.T) {
	dl := newDialect("sqlserver")
	got := dl.upsertOnConflict("presences", []string{"user_id", "date", "status_id"}, "?, ?, ?", "user_id, date", "status_id = excluded.status_id")
	if !strings.Contains(got, "MERGE INTO presences") {
		t.Errorf("sqlserver upsert = %q, want MERGE INTO", got)
	}
	if !strings.Contains(got, "WHEN MATCHED") {
		t.Errorf("sqlserver upsert missing WHEN MATCHED: %q", got)
	}
	if !strings.Contains(got, "WHEN NOT MATCHED") {
		t.Errorf("sqlserver upsert missing WHEN NOT MATCHED: %q", got)
	}
}

// -----------------------------------------------------------------------
// columnExistsQuery / tableExistsQuery
// -----------------------------------------------------------------------

func TestColumnExistsQuery_ContainsTableAndColumn(t *testing.T) {
	for _, driver := range []string{"sqlite", "mysql", "postgres", "sqlserver"} {
		dl := newDialect(driver)
		got := dl.columnExistsQuery("users", "disabled")
		if !strings.Contains(got, "users") {
			t.Errorf("[%s] columnExistsQuery missing table name: %q", driver, got)
		}
		if !strings.Contains(got, "disabled") {
			t.Errorf("[%s] columnExistsQuery missing column name: %q", driver, got)
		}
	}
}

func TestTableExistsQuery_ContainsTableName(t *testing.T) {
	for _, driver := range []string{"sqlite", "mysql", "postgres", "sqlserver"} {
		dl := newDialect(driver)
		got := dl.tableExistsQuery("users")
		if !strings.Contains(got, "users") {
			t.Errorf("[%s] tableExistsQuery missing table name: %q", driver, got)
		}
	}
}

func TestColumnExistsQuery_SQLiteUsesPragma(t *testing.T) {
	dl := newDialect("sqlite")
	got := dl.columnExistsQuery("users", "email")
	if !strings.Contains(got, "pragma_table_info") {
		t.Errorf("sqlite columnExistsQuery should use pragma_table_info: %q", got)
	}
}

func TestColumnExistsQuery_NetworkUsesInformationSchema(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres", "sqlserver"} {
		dl := newDialect(driver)
		got := dl.columnExistsQuery("users", "email")
		if !strings.Contains(strings.ToUpper(got), "INFORMATION_SCHEMA") {
			t.Errorf("[%s] columnExistsQuery should use INFORMATION_SCHEMA: %q", driver, got)
		}
	}
}

// -----------------------------------------------------------------------
// mysqlValuesExpr
// -----------------------------------------------------------------------

func TestMysqlValuesExpr_ConvertsExcluded(t *testing.T) {
	got := mysqlValuesExpr("status_id = excluded.status_id")
	if !strings.Contains(got, "VALUES(status_id)") {
		t.Errorf("mysqlValuesExpr = %q, want VALUES(status_id)", got)
	}
	if strings.Contains(got, "excluded.") {
		t.Errorf("mysqlValuesExpr should not contain excluded.: %q", got)
	}
}

func TestMysqlValuesExpr_MultipleColumns(t *testing.T) {
	got := mysqlValuesExpr("name = excluded.name, role = excluded.role")
	if !strings.Contains(got, "VALUES(name)") || !strings.Contains(got, "VALUES(role)") {
		t.Errorf("mysqlValuesExpr multiple cols = %q", got)
	}
}

func TestMysqlValuesExpr_AlreadyValues_Unchanged(t *testing.T) {
	// If the expression doesn't use excluded.* style, it should pass through unchanged
	input := "days = VALUES(days)"
	got := mysqlValuesExpr(input)
	if !strings.Contains(got, "VALUES(days)") {
		t.Errorf("mysqlValuesExpr should preserve VALUES() expr: %q", got)
	}
}

// -----------------------------------------------------------------------
// isSQLite / isMySQL / isPostgres / isSQLServer helpers
// -----------------------------------------------------------------------

func TestDialectHelpers(t *testing.T) {
	if !newDialect("sqlite").isSQLite() {
		t.Error("sqlite should be isSQLite")
	}
	if !newDialect("mysql").isMySQL() {
		t.Error("mysql should be isMySQL")
	}
	if !newDialect("postgres").isPostgres() {
		t.Error("postgres should be isPostgres")
	}
	if !newDialect("sqlserver").isSQLServer() {
		t.Error("sqlserver should be isSQLServer")
	}
	// Cross-checks
	if newDialect("sqlite").isMySQL() || newDialect("sqlite").isPostgres() || newDialect("sqlite").isSQLServer() {
		t.Error("sqlite should not match other dialects")
	}
}

// -----------------------------------------------------------------------
// insertIgnoreVerb / onConflictDoNothing
// -----------------------------------------------------------------------

func TestInsertIgnoreVerb(t *testing.T) {
	cases := map[string]string{
		"mysql":     "INSERT IGNORE INTO",
		"sqlite":    "INSERT INTO",
		"postgres":  "INSERT INTO",
		"sqlserver": "INSERT INTO",
	}
	for driver, want := range cases {
		got := newDialect(driver).insertIgnoreVerb()
		if got != want {
			t.Errorf("[%s] insertIgnoreVerb = %q, want %q", driver, got, want)
		}
	}
}

func TestOnConflictDoNothing(t *testing.T) {
	// Only PostgreSQL returns a non-empty suffix; all others handle it differently.
	dl := newDialect("postgres")
	if got := dl.onConflictDoNothing(); got != " ON CONFLICT DO NOTHING" {
		t.Errorf("postgres onConflictDoNothing = %q, want \" ON CONFLICT DO NOTHING\"", got)
	}
	for _, driver := range []string{"mysql", "sqlite", "sqlserver"} {
		if got := newDialect(driver).onConflictDoNothing(); got != "" {
			t.Errorf("[%s] onConflictDoNothing = %q, want empty string", driver, got)
		}
	}
}

// -----------------------------------------------------------------------
// createTableIfNotExists per driver
// -----------------------------------------------------------------------

func TestCreateTableIfNotExists_SQLServer_ConditionalForm(t *testing.T) {
	dl := newDialect("sqlserver")
	got := dl.createTableIfNotExists("mytable", "id BIGINT PRIMARY KEY")
	if !strings.Contains(got, "IF NOT EXISTS") {
		t.Errorf("sqlserver createTableIfNotExists should use IF NOT EXISTS form: %q", got)
	}
	if !strings.Contains(strings.ToUpper(got), "INFORMATION_SCHEMA") {
		t.Errorf("sqlserver createTableIfNotExists should check INFORMATION_SCHEMA: %q", got)
	}
	if !strings.Contains(got, "mytable") {
		t.Errorf("sqlserver createTableIfNotExists missing table name: %q", got)
	}
}

func TestCreateTableIfNotExists_NonSQLServer_StandardForm(t *testing.T) {
	for _, driver := range []string{"sqlite", "mysql", "postgres"} {
		dl := newDialect(driver)
		got := dl.createTableIfNotExists("mytable", "id BIGINT PRIMARY KEY")
		if !strings.HasPrefix(got, "CREATE TABLE IF NOT EXISTS mytable") {
			t.Errorf("[%s] createTableIfNotExists = %q, want CREATE TABLE IF NOT EXISTS mytable ...", driver, got)
		}
	}
}

// -----------------------------------------------------------------------
// addColumnIfNotExists per driver
// -----------------------------------------------------------------------

func TestAddColumnIfNotExists_SQLServer_ConditionalForm(t *testing.T) {
	dl := newDialect("sqlserver")
	got := dl.addColumnIfNotExists("users", "disabled", "BIT NOT NULL DEFAULT 0")
	if !strings.Contains(got, "IF NOT EXISTS") {
		t.Errorf("sqlserver addColumnIfNotExists should use IF NOT EXISTS form: %q", got)
	}
	if !strings.Contains(strings.ToUpper(got), "INFORMATION_SCHEMA") {
		t.Errorf("sqlserver addColumnIfNotExists should check INFORMATION_SCHEMA.COLUMNS: %q", got)
	}
	if !strings.Contains(got, "users") || !strings.Contains(got, "disabled") {
		t.Errorf("sqlserver addColumnIfNotExists missing table/column names: %q", got)
	}
}

func TestAddColumnIfNotExists_NonSQLServer_StandardForm(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		dl := newDialect(driver)
		got := dl.addColumnIfNotExists("users", "disabled", "BOOLEAN DEFAULT 0")
		if !strings.Contains(got, "ADD COLUMN IF NOT EXISTS") {
			t.Errorf("[%s] addColumnIfNotExists = %q, want ADD COLUMN IF NOT EXISTS", driver, got)
		}
		if !strings.Contains(got, "users") || !strings.Contains(got, "disabled") {
			t.Errorf("[%s] addColumnIfNotExists missing table/column names: %q", driver, got)
		}
	}
}

func TestAddColumnIfNotExists_SQLite_PlainForm(t *testing.T) {
	dl := newDialect("sqlite")
	got := dl.addColumnIfNotExists("users", "disabled", "BOOLEAN DEFAULT 0")
	if !strings.Contains(got, "ADD COLUMN") {
		t.Errorf("sqlite addColumnIfNotExists = %q, want ADD COLUMN", got)
	}
	if strings.Contains(got, "IF NOT EXISTS") {
		t.Errorf("sqlite addColumnIfNotExists should not use IF NOT EXISTS: %q", got)
	}
	if !strings.Contains(got, "users") || !strings.Contains(got, "disabled") {
		t.Errorf("sqlite addColumnIfNotExists missing table/column names: %q", got)
	}
}

// -----------------------------------------------------------------------
// varcharType per driver
// -----------------------------------------------------------------------

func TestVarcharType_AllDrivers(t *testing.T) {
	for _, driver := range []string{"sqlite", "mysql", "postgres"} {
		got := newDialect(driver).varcharType(128)
		if got != "VARCHAR(128)" {
			t.Errorf("[%s] varcharType(128) = %q, want VARCHAR(128)", driver, got)
		}
	}
	if got := newDialect("sqlserver").varcharType(64); got != "NVARCHAR(64)" {
		t.Errorf("sqlserver varcharType(64) = %q, want NVARCHAR(64)", got)
	}
}
