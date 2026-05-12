package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// dialect abstracts SQL differences between supported database backends.
// Supported drivers: "sqlite", "postgres", "mysql", "sqlserver".
type dialect struct {
	driver string
}

// newDialect returns a dialect for the given driver name.
func newDialect(driver string) dialect {
	return dialect{driver: driver}
}

// isMySQL returns true for MySQL and MariaDB (same driver).
func (d dialect) isMySQL() bool { return d.driver == "mysql" }

// isPostgres returns true for PostgreSQL.
func (d dialect) isPostgres() bool { return d.driver == "postgres" }

// isSQLServer returns true for Microsoft SQL Server.
func (d dialect) isSQLServer() bool { return d.driver == "sqlserver" }

// isSQLite returns true for SQLite.
func (d dialect) isSQLite() bool { return d.driver == "sqlite" }

// now returns the SQL expression for the current timestamp.
func (d dialect) now() string {
	switch d.driver {
	case "sqlserver":
		return "GETDATE()"
	case "postgres", "mysql":
		return "NOW()"
	default: // sqlite
		return "datetime('now')"
	}
}

// autoincrement returns the SQL fragment for an auto-incrementing integer primary key column.
// Usage: fmt.Sprintf("id %s,", d.autoincrement())
func (d dialect) autoincrement() string {
	switch d.driver {
	case "postgres":
		return "BIGSERIAL PRIMARY KEY"
	case "mysql":
		return "BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY"
	case "sqlserver":
		return "BIGINT IDENTITY(1,1) PRIMARY KEY"
	default: // sqlite
		return "INTEGER PRIMARY KEY AUTOINCREMENT"
	}
}

// boolType returns the SQL column type for a boolean.
func (d dialect) boolType() string {
	if d.driver == "sqlserver" {
		return "BIT"
	}
	return "BOOLEAN"
}

// boolDefault returns the SQL literal for a boolean default value.
// PostgreSQL requires TRUE/FALSE; SQLite and MySQL accept 0/1; SQL Server uses 0/1 for BIT.
func (d dialect) boolDefault(v bool) string {
	if d.driver == "postgres" {
		if v {
			return "TRUE"
		}
		return "FALSE"
	}
	if v {
		return "1"
	}
	return "0"
}

// textType returns the SQL column type for unbounded text.
func (d dialect) textType() string {
	if d.driver == "sqlserver" {
		return "NVARCHAR(MAX)"
	}
	return "TEXT"
}

// varcharType returns a fixed-length VARCHAR (or NVARCHAR for MSSQL).
func (d dialect) varcharType(n int) string {
	if d.driver == "sqlserver" {
		return fmt.Sprintf("NVARCHAR(%d)", n)
	}
	return fmt.Sprintf("VARCHAR(%d)", n)
}

// datetimeType returns the SQL column type for a datetime.
func (d dialect) datetimeType() string {
	switch d.driver {
	case "sqlserver":
		return "DATETIME2"
	case "postgres":
		return "TIMESTAMPTZ"
	default:
		return "DATETIME"
	}
}

// realType returns the SQL column type for a floating-point number.
func (d dialect) realType() string {
	if d.driver == "sqlserver" {
		return "FLOAT"
	}
	return "REAL"
}

// insertIgnore returns the SQL verb/suffix to insert a row while ignoring
// unique-constraint conflicts.
// For SQLServer a different approach (MERGE) is needed; callers should use
// upsertOnConflict or insertIgnoreStmt for that driver.
func (d dialect) insertIgnoreVerb() string {
	switch d.driver {
	case "mysql":
		return "INSERT IGNORE INTO"
	default: // sqlite, postgres, sqlserver
		return "INSERT INTO"
	}
}

// onConflictDoNothing returns the SQL suffix appended after a VALUES clause
// to silently ignore duplicate-key conflicts (equivalent to INSERT OR IGNORE).
func (d dialect) onConflictDoNothing() string {
	switch d.driver {
	case "mysql":
		return "" // handled via INSERT IGNORE
	case "postgres":
		return " ON CONFLICT DO NOTHING"
	case "sqlserver":
		// MSSQL has no direct equivalent; callers must use a separate existence check
		// or MERGE. We return an empty string and rely on error-swallowing at the
		// application layer for idempotent seeds.
		return ""
	default: // sqlite
		return "" // handled via INSERT OR IGNORE keyword
	}
}

// insertOrIgnore builds a full "INSERT … OR IGNORE / INSERT IGNORE / INSERT … ON CONFLICT DO NOTHING"
// statement for the given table, columns and values placeholder.
// placeholders is e.g. "?, ?, ?" (always use ? — rebind will convert for postgres).
func (d dialect) insertOrIgnore(table string, cols []string, placeholders string) string {
	colList := strings.Join(cols, ", ")
	switch d.driver {
	case "mysql":
		return fmt.Sprintf("INSERT IGNORE INTO %s (%s) VALUES (%s)", table, colList, placeholders)
	case "postgres":
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING", table, colList, placeholders)
	case "sqlserver":
		// SQL Server has no INSERT … IF NOT EXISTS / INSERT OR IGNORE syntax.
		// Wrap the INSERT in a TRY/CATCH block so any UNIQUE / PRIMARY KEY violation
		// is silently swallowed, regardless of which columns form the key.
		return fmt.Sprintf(
			"BEGIN TRY INSERT INTO %s (%s) VALUES (%s) END TRY BEGIN CATCH END CATCH",
			table, colList, placeholders,
		)
	default: // sqlite
		return fmt.Sprintf("INSERT OR IGNORE INTO %s (%s) VALUES (%s)", table, colList, placeholders)
	}
}

// upsertOnConflict builds an INSERT … ON CONFLICT(conflictCol) DO UPDATE SET updateExpr statement.
// updateExpr is the "col = excluded.col" part (or "col = VALUES(col)" for MySQL).
// Example: d.upsertOnConflict("presences", cols, "?,...", "user_id, date, half", "status_id = excluded.status_id")
func (d dialect) upsertOnConflict(table string, cols []string, placeholders, conflictCols, updateExpr string) string {
	colList := strings.Join(cols, ", ")
	switch d.driver {
	case "mysql":
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s",
			table, colList, placeholders, mysqlValuesExpr(updateExpr))
	case "postgres":
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(%s) DO UPDATE SET %s",
			table, colList, placeholders, conflictCols, updateExpr)
	case "sqlserver":
		// SQL Server uses MERGE.  The source row is built once from named params in
		// a derived SELECT; INSERT VALUES and UPDATE SET both reference "source.col"
		// so we never duplicate the placeholders (which would require extra args).
		conflictColSlice := strings.Split(conflictCols, ", ")
		onClauses := make([]string, len(conflictColSlice))
		for i, c := range conflictColSlice {
			c = strings.TrimSpace(c)
			onClauses[i] = fmt.Sprintf("target.%s = source.%s", c, c)
		}
		// Build the source SELECT: "? AS col, ? AS col2, ..."
		srcPlaceholders := strings.Split(placeholders, ", ")
		srcCols := make([]string, len(cols))
		for i, c := range cols {
			ph := "?"
			if i < len(srcPlaceholders) {
				ph = strings.TrimSpace(srcPlaceholders[i])
			}
			srcCols[i] = fmt.Sprintf("%s AS %s", ph, c)
		}
		// INSERT VALUES references source columns (no extra placeholders).
		srcValCols := make([]string, len(cols))
		for i, c := range cols {
			srcValCols[i] = "source." + c
		}
		// Convert "col = excluded.col" → "target.col = source.col"
		mssqlUpdate := mssqlMergeUpdateExpr(updateExpr)
		return fmt.Sprintf(
			"MERGE INTO %s AS target USING (SELECT %s) AS source ON (%s) WHEN MATCHED THEN UPDATE SET %s WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s);",
			table,
			strings.Join(srcCols, ", "),
			strings.Join(onClauses, " AND "),
			mssqlUpdate,
			colList,
			strings.Join(srcValCols, ", "),
		)
	default: // sqlite
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(%s) DO UPDATE SET %s",
			table, colList, placeholders, conflictCols, updateExpr)
	}
}

// columnExists returns a SQL query that counts columns named colName in tableName.
// Result should be scanned into an int; > 0 means the column exists.
func (d dialect) columnExistsQuery(tableName, colName string) string {
	switch d.driver {
	case "postgres":
		return fmt.Sprintf(
			"SELECT COUNT(*) FROM information_schema.columns WHERE table_name = '%s' AND column_name = '%s'",
			tableName, colName,
		)
	case "mysql":
		return fmt.Sprintf(
			"SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_NAME = '%s' AND COLUMN_NAME = '%s' AND TABLE_SCHEMA = DATABASE()",
			tableName, colName,
		)
	case "sqlserver":
		return fmt.Sprintf(
			"SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = '%s' AND COLUMN_NAME = '%s'",
			tableName, colName,
		)
	default: // sqlite
		return fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='%s'", tableName, colName)
	}
}

// tableExistsQuery returns a query that counts tables with the given name.
func (d dialect) tableExistsQuery(tableName string) string {
	switch d.driver {
	case "postgres":
		return fmt.Sprintf(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_name = '%s' AND table_schema = 'public'",
			tableName,
		)
	case "mysql":
		return fmt.Sprintf(
			"SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_NAME = '%s' AND TABLE_SCHEMA = DATABASE()",
			tableName,
		)
	case "sqlserver":
		return fmt.Sprintf(
			"SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME = '%s'",
			tableName,
		)
	default: // sqlite
		return fmt.Sprintf(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'",
			tableName,
		)
	}
}

// createTableIfNotExists wraps a CREATE TABLE body in the correct idempotent
// form for the current driver.
// All drivers except SQL Server support CREATE TABLE IF NOT EXISTS natively.
// SQL Server uses: IF NOT EXISTS (SELECT 1 FROM INFORMATION_SCHEMA.TABLES …) CREATE TABLE …
func (d dialect) createTableIfNotExists(name, body string) string {
	if d.driver == "sqlserver" {
		return fmt.Sprintf(
			"IF NOT EXISTS (SELECT 1 FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME = '%s') CREATE TABLE %s (%s)",
			name, name, body,
		)
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", name, body)
}

// addColumnIfNotExists returns an ALTER TABLE … ADD COLUMN … statement that
// is safe to run even when the column already exists.
// PostgreSQL 9.6+, MariaDB 10.3+ support ADD COLUMN IF NOT EXISTS.
// SQLite supports it only in 3.37+; for portability we omit IF NOT EXISTS and
// rely on the caller ignoring "duplicate column name" errors.
// SQL Server has no such syntax; the statement is wrapped in a conditional.
func (d dialect) addColumnIfNotExists(table, col, colDef string) string {
	if d.driver == "sqlserver" {
		return fmt.Sprintf(
			"IF NOT EXISTS (SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME='%s' AND COLUMN_NAME='%s') ALTER TABLE %s ADD %s %s",
			table, col, table, col, colDef,
		)
	}
	if d.driver == "sqlite" {
		// Omit IF NOT EXISTS — SQLite < 3.37 does not support it.
		// Callers must ignore duplicate-column errors (//nolint:errcheck).
		return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, col, colDef)
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s", table, col, colDef)
}

// modifyColumnType returns an ALTER TABLE … statement that widens a column's
// type. oldType is only used by SQL Server (where the column must be re-declared
// with all its constraints). SQLite does not support ALTER COLUMN; we emit a
// no-op SELECT so the caller can safely ignore the error.
func (d dialect) modifyColumnType(table, col, newType, oldType string) string {
	switch d.driver {
	case "postgres":
		return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", table, col, newType)
	case "mysql":
		return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s NOT NULL DEFAULT 'basic'", table, col, newType)
	case "sqlserver":
		return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s NOT NULL", table, col, newType)
	default: // sqlite — column widening not supported; silently skip
		return "SELECT 1"
	}
}

// rebind converts a query using ? placeholders to the correct placeholder style
// for the target database driver.
// SQLite and MySQL use ?, PostgreSQL uses $1/$2/…, SQL Server uses @p1/@p2/…
func (d dialect) rebind(query string) string {
	switch d.driver {
	case "postgres":
		return rebindPositional(query, "$")
	case "sqlserver":
		return rebindPositional(query, "@p")
	default:
		return query // sqlite and mysql both use ?
	}
}

// rebindPositional replaces each ? in query with prefix+n (1-indexed).
func rebindPositional(query, prefix string) string {
	var b strings.Builder
	n := 0
	for _, ch := range query {
		if ch == '?' {
			n++
			fmt.Fprintf(&b, "%s%d", prefix, n)
		} else {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// mysqlValuesExpr converts PostgreSQL-style "col = excluded.col, col2 = excluded.col2"
// into MySQL-style "col = VALUES(col), col2 = VALUES(col2)".
func mysqlValuesExpr(updateExpr string) string {
	parts := strings.Split(updateExpr, ",")
	for i, p := range parts {
		// Replace "col = excluded.col" → "col = VALUES(col)"
		if eqIdx := strings.Index(p, "="); eqIdx >= 0 {
			lhs := strings.TrimSpace(p[:eqIdx])
			rhs := strings.TrimSpace(p[eqIdx+1:])
			// rhs might be "excluded.col" or already "VALUES(col)"
			if strings.HasPrefix(rhs, "excluded.") {
				col := strings.TrimPrefix(rhs, "excluded.")
				parts[i] = fmt.Sprintf(" %s = VALUES(%s)", lhs, col)
			} else {
				parts[i] = p
			}
		}
	}
	return strings.Join(parts, ",")
}

// mssqlMergeUpdateExpr converts a PostgreSQL-style updateExpr like
// "col = excluded.col, col2 = excluded.col2" into the SQL Server MERGE form
// "target.col = source.col, target.col2 = source.col2".
func mssqlMergeUpdateExpr(updateExpr string) string {
	parts := strings.Split(updateExpr, ",")
	for i, p := range parts {
		if eqIdx := strings.Index(p, "="); eqIdx >= 0 {
			lhs := strings.TrimSpace(p[:eqIdx])
			rhs := strings.TrimSpace(p[eqIdx+1:])
			// Strip existing "target." prefix on lhs if already present
			lhs = strings.TrimPrefix(lhs, "target.")
			// Convert "excluded.col" → "source.col"
			if strings.HasPrefix(rhs, "excluded.") {
				rhs = "source." + strings.TrimPrefix(rhs, "excluded.")
			}
			parts[i] = fmt.Sprintf(" target.%s = %s", lhs, rhs)
		}
	}
	return strings.Join(parts, ",")
}

// ─── Auto-rebind wrappers ─────────────────────────────────────────────────────
//
// rebindDB and rebindTx wrap *sql.DB / *sql.Tx respectively, automatically
// converting ? placeholders to the driver-correct form ($1/$2 for PostgreSQL,
// @p1/@p2 for SQL Server) on every Exec/Query/QueryRow/Prepare call.
// This eliminates the need to call dialect.rebind() at every call site.

// rebindDB wraps *sql.DB to automatically rebind ? placeholders.
type rebindDB struct {
	*sql.DB
	dl dialect
}

func newRebindDB(db *sql.DB, dl dialect) *rebindDB {
	return &rebindDB{DB: db, dl: dl}
}

func (r *rebindDB) Exec(query string, args ...any) (sql.Result, error) {
	return r.DB.Exec(r.dl.rebind(query), args...)
}

func (r *rebindDB) Query(query string, args ...any) (*sql.Rows, error) {
	return r.DB.Query(r.dl.rebind(query), args...)
}

func (r *rebindDB) QueryRow(query string, args ...any) *sql.Row {
	return r.DB.QueryRow(r.dl.rebind(query), args...)
}

func (r *rebindDB) Prepare(query string) (*sql.Stmt, error) {
	return r.DB.Prepare(r.dl.rebind(query))
}

func (r *rebindDB) Begin() (*rebindTx, error) {
	tx, err := r.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &rebindTx{Tx: tx, dl: r.dl}, nil
}

// InsertGetID executes an INSERT statement and returns the new row's ID.
// PostgreSQL does not support LastInsertId(); it appends "RETURNING id" instead.
// SQL Server does not support LastInsertId() either; it uses OUTPUT INSERTED.id.
// SQLite and MySQL use the standard LastInsertId().
func (r *rebindDB) InsertGetID(query string, args ...any) (int64, error) {
	if r.dl.driver == "postgres" {
		var id int64
		err := r.DB.QueryRow(r.dl.rebind(query+" RETURNING id"), args...).Scan(&id)
		return id, err
	}
	if r.dl.driver == "sqlserver" {
		// Inject OUTPUT INSERTED.id before VALUES so SQL Server returns the new PK.
		var id int64
		err := r.DB.QueryRow(r.dl.rebind(injectOutputInserted(query)), args...).Scan(&id)
		return id, err
	}
	result, err := r.DB.Exec(r.dl.rebind(query), args...)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// injectOutputInserted inserts "OUTPUT INSERTED.id" into an INSERT … VALUES …
// statement immediately before the VALUES keyword, producing the form:
//
//	INSERT INTO t (cols) OUTPUT INSERTED.id VALUES (…)
//
// which SQL Server uses to return the generated IDENTITY value.
func injectOutputInserted(query string) string {
	upper := strings.ToUpper(query)
	idx := strings.Index(upper, "VALUES")
	if idx < 0 {
		return query
	}
	return query[:idx] + "OUTPUT INSERTED.id " + query[idx:]
}

// rebindTx wraps *sql.Tx to automatically rebind ? placeholders.
type rebindTx struct {
	*sql.Tx
	dl dialect
}

func (r *rebindTx) Exec(query string, args ...any) (sql.Result, error) {
	return r.Tx.Exec(r.dl.rebind(query), args...)
}

func (r *rebindTx) Query(query string, args ...any) (*sql.Rows, error) {
	return r.Tx.Query(r.dl.rebind(query), args...)
}

func (r *rebindTx) QueryRow(query string, args ...any) *sql.Row {
	return r.Tx.QueryRow(r.dl.rebind(query), args...)
}

func (r *rebindTx) Prepare(query string) (*sql.Stmt, error) {
	return r.Tx.Prepare(r.dl.rebind(query))
}
