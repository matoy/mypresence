package db

import (
	"fmt"
	"strings"
)

// migrateCertifications creates the declaration_certifications table, used to
// track which users have certified (locked) their monthly presence
// declaration. Once certified, a month's declarations can no longer be
// modified until a global admin decertifies it.
func (d *DB) migrateCertifications() error {
	dl := d.dialect
	ai := dl.autoincrement()
	dt := dl.datetimeType()

	stmt := dl.createTableIfNotExists("declaration_certifications", fmt.Sprintf(`
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

// CertifyMonth marks a user's presence declaration as certified for the given
// month. Idempotent: certifying an already-certified month is a no-op.
func (d *DB) CertifyMonth(userID int64, year, month int, certifiedBy int64) error {
	_, err := d.presence.Exec(d.dialect.rebind(d.dialect.insertOrIgnore(
		"declaration_certifications",
		[]string{"user_id", "year", "month", "certified_by"},
		"?, ?, ?, ?",
	)), userID, year, month, certifiedBy)
	return err
}

// DecertifyMonth removes the certification for a user's month, if present,
// re-allowing edits to that month's presence declarations.
func (d *DB) DecertifyMonth(userID int64, year, month int) error {
	_, err := d.presence.Exec(
		"DELETE FROM declaration_certifications WHERE user_id = ? AND year = ? AND month = ?",
		userID, year, month,
	)
	return err
}

// IsMonthCertified reports whether a user's declaration for the given month
// is currently certified.
func (d *DB) IsMonthCertified(userID int64, year, month int) (bool, error) {
	var count int
	err := d.presence.QueryRow(
		"SELECT COUNT(*) FROM declaration_certifications WHERE user_id = ? AND year = ? AND month = ?",
		userID, year, month,
	).Scan(&count)
	return count > 0, err
}

// GetCertifiedUserIDs returns the subset of userIDs whose declaration is
// certified for the given year/month, for batch lookups (e.g. team views).
func (d *DB) GetCertifiedUserIDs(userIDs []int64, year, month int) (map[int64]bool, error) {
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
		"SELECT user_id FROM declaration_certifications WHERE user_id IN (%s) AND year = ? AND month = ?",
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
