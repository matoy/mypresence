package db

import (
	"fmt"
	"strings"
)

// migrateProjectCertifications creates the project_certifications table, used
// to track which users have certified (locked) their monthly project time
// declaration (percentage-based or "Timesheets managed manually"). Once
// certified, a month's project declarations can no longer be modified until
// a global admin, activity viewer, or team leader decertifies it.
func (d *DB) migrateProjectCertifications() error {
	dl := d.dialect
	ai := dl.autoincrement()
	dt := dl.datetimeType()

	stmt := dl.createTableIfNotExists("project_certifications", fmt.Sprintf(`
id %s,
user_id BIGINT NOT NULL,
year INTEGER NOT NULL,
month INTEGER NOT NULL,
certified_by BIGINT NOT NULL,
certified_at %s DEFAULT CURRENT_TIMESTAMP,
UNIQUE(user_id, year, month)
`, ai, dt))

	_, err := d.presence.Exec(dl.rebind(stmt))
	return err
}

// CertifyProjectMonth marks a user's project time declaration as certified
// for the given month. Idempotent: certifying an already-certified month is
// a no-op.
func (d *DB) CertifyProjectMonth(userID int64, year, month int, certifiedBy int64) error {
	_, err := d.presence.Exec(d.dialect.rebind(d.dialect.insertOrIgnore(
		"project_certifications",
		[]string{"user_id", "year", "month", "certified_by"},
		"?, ?, ?, ?",
	)), userID, year, month, certifiedBy)
	return err
}

// DecertifyProjectMonth removes the project certification for a user's
// month, if present, re-allowing edits to that month's project declarations.
func (d *DB) DecertifyProjectMonth(userID int64, year, month int) error {
	_, err := d.presence.Exec(
		"DELETE FROM project_certifications WHERE user_id = ? AND year = ? AND month = ?",
		userID, year, month,
	)
	return err
}

// IsProjectMonthCertified reports whether a user's project declaration for
// the given month is currently certified.
func (d *DB) IsProjectMonthCertified(userID int64, year, month int) (bool, error) {
	var count int
	err := d.presence.QueryRow(
		"SELECT COUNT(*) FROM project_certifications WHERE user_id = ? AND year = ? AND month = ?",
		userID, year, month,
	).Scan(&count)
	return count > 0, err
}

// GetCertifiedProjectUserIDs returns the subset of userIDs whose project
// declaration is certified for the given year/month, for batch lookups (e.g.
// the activity report).
func (d *DB) GetCertifiedProjectUserIDs(userIDs []int64, year, month int) (map[int64]bool, error) {
	result := make(map[int64]bool)
	if len(userIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(userIDs))
	args := make([]interface{}, 0, len(userIDs)+2)
	for i, id := range userIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, year, month)

	query := fmt.Sprintf(
		"SELECT user_id FROM project_certifications WHERE user_id IN (%s) AND year = ? AND month = ?",
		strings.Join(placeholders, ","),
	)

	rows, err := d.presence.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		result[uid] = true
	}
	return result, rows.Err()
}
