package db

import (
	"fmt"
	"time"

	"github.com/matoy/mypresence/internal/models"
)

// migrateNews creates the news_messages table if it does not exist and applies
// any incremental column additions for existing deployments.
func (d *DB) migrateNews() error {
	dl := d.dialect
	ai := dl.autoincrement()
	dt := dl.datetimeType()

	stmt := dl.createTableIfNotExists("news_messages", fmt.Sprintf(`
  id         %s,
  title      %s NOT NULL,
  content    %s NOT NULL,
  start_date %s NOT NULL,
  end_date   %s NOT NULL,
  bg_color   %s NOT NULL DEFAULT '#dc2626',
  recurring  INTEGER NOT NULL DEFAULT 0,
  created_at %s DEFAULT CURRENT_TIMESTAMP
`, ai, dl.varcharType(200), dl.textType(), dl.varcharType(10), dl.varcharType(10), dl.varcharType(7), dt))

	if _, err := d.core.Exec(dl.rebind(stmt)); err != nil {
		return err
	}

	// Migration for existing deployments that pre-date the recurring column.
	d.core.Exec(`ALTER TABLE news_messages ADD COLUMN recurring INTEGER NOT NULL DEFAULT 0`) //nolint:errcheck
	return nil
}

// scanRecurring converts the SQLite INTEGER 0/1 to a Go bool.
func scanRecurring(v int) bool { return v != 0 }

// ListNewsMessages returns all news messages ordered by start_date desc.
func (d *DB) ListNewsMessages() ([]models.NewsMessage, error) {
	rows, err := d.core.Query(`
SELECT id, title, content, start_date, end_date, bg_color, recurring
FROM news_messages
ORDER BY start_date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var msgs []models.NewsMessage
	for rows.Next() {
		var m models.NewsMessage
		var rec int
		if err := rows.Scan(&m.ID, &m.Title, &m.Content, &m.StartDate, &m.EndDate, &m.BgColor, &rec); err != nil {
			return nil, err
		}
		m.Recurring = scanRecurring(rec)
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// GetActiveNewsMessages returns messages that are currently active.
// For non-recurring messages: active when start_date <= today <= end_date.
// For recurring messages: active when today's day-of-month is within
// [day(start_date), day(end_date)], repeating every month.
func (d *DB) GetActiveNewsMessages() ([]models.NewsMessage, error) {
	now := time.Now()
	todayStr := now.Format("2006-01-02")
	todayDay := now.Day()

	rows, err := d.core.Query(`
SELECT id, title, content, start_date, end_date, bg_color, recurring
FROM news_messages
ORDER BY recurring ASC, start_date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var msgs []models.NewsMessage
	for rows.Next() {
		var m models.NewsMessage
		var rec int
		if err := rows.Scan(&m.ID, &m.Title, &m.Content, &m.StartDate, &m.EndDate, &m.BgColor, &rec); err != nil {
			return nil, err
		}
		m.Recurring = scanRecurring(rec)

		if m.Recurring {
			startT, err1 := time.Parse("2006-01-02", m.StartDate)
			endT, err2 := time.Parse("2006-01-02", m.EndDate)
			if err1 == nil && err2 == nil && todayDay >= startT.Day() && todayDay <= endT.Day() {
				msgs = append(msgs, m)
			}
		} else {
			if m.StartDate <= todayStr && m.EndDate >= todayStr {
				msgs = append(msgs, m)
			}
		}
	}
	return msgs, rows.Err()
}

// CreateNewsMessage inserts a new news message and returns its ID.
func (d *DB) CreateNewsMessage(title, content, startDate, endDate, bgColor string, recurring bool) (int64, error) {
	dl := d.dialect
	rec := 0
	if recurring {
		rec = 1
	}
	var id int64
	if dl.isPostgres() {
		err := d.core.QueryRow(dl.rebind(`
INSERT INTO news_messages (title, content, start_date, end_date, bg_color, recurring)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id`), title, content, startDate, endDate, bgColor, rec).Scan(&id)
		return id, err
	}
	res, err := d.core.Exec(dl.rebind(`
INSERT INTO news_messages (title, content, start_date, end_date, bg_color, recurring)
VALUES (?, ?, ?, ?, ?, ?)`), title, content, startDate, endDate, bgColor, rec)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateNewsMessage updates an existing news message.
func (d *DB) UpdateNewsMessage(id int64, title, content, startDate, endDate, bgColor string, recurring bool) error {
	rec := 0
	if recurring {
		rec = 1
	}
	_, err := d.core.Exec(d.dialect.rebind(`
UPDATE news_messages
SET title = ?, content = ?, start_date = ?, end_date = ?, bg_color = ?, recurring = ?
WHERE id = ?`), title, content, startDate, endDate, bgColor, rec, id)
	return err
}

// DeleteNewsMessage removes a news message by ID.
func (d *DB) DeleteNewsMessage(id int64) error {
	_, err := d.core.Exec(d.dialect.rebind(`DELETE FROM news_messages WHERE id = ?`), id)
	return err
}

// GetNewsMessageTitle returns the title of a news message by ID (for audit logs).
func (d *DB) GetNewsMessageTitle(id int64) string {
	var title string
	d.core.QueryRow(d.dialect.rebind(`SELECT title FROM news_messages WHERE id = ?`), id).Scan(&title) //nolint:errcheck
	return title
}
