package db

import (
	"database/sql"
	"errors"
	"time"

	"github.com/matoy/mypresence/internal/models"
)

// SaveUserO365Token inserts or updates Microsoft Office 365 tokens for a user.
func (d *DB) SaveUserO365Token(userID int64, email, accessToken, refreshToken string, expiry time.Time, autoSync bool) error {
	dl := d.dialect
	var count int
	err := d.core.QueryRow(dl.rebind(`SELECT COUNT(*) FROM user_o365_tokens WHERE user_id = ?`), userID).Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		query := `UPDATE user_o365_tokens SET microsoft_email = ?, access_token = ?, refresh_token = ?, token_expiry = ?, auto_sync = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`
		_, err = d.core.Exec(dl.rebind(query), email, accessToken, refreshToken, expiry, autoSync, userID)
		return err
	}

	query := `INSERT INTO user_o365_tokens (user_id, microsoft_email, access_token, refresh_token, token_expiry, auto_sync, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	_, err = d.core.Exec(dl.rebind(query), userID, email, accessToken, refreshToken, expiry, autoSync)
	return err
}

// GetUserO365Token retrieves the Office 365 token record for a user.
// Returns nil, nil if no token exists for the user.
func (d *DB) GetUserO365Token(userID int64) (*models.UserO365Token, error) {
	dl := d.dialect
	query := `SELECT id, user_id, microsoft_email, access_token, refresh_token, token_expiry, auto_sync, created_at, updated_at FROM user_o365_tokens WHERE user_id = ?`
	row := d.core.QueryRow(dl.rebind(query), userID)

	var t models.UserO365Token
	var expiry, createdAt, updatedAt time.Time
	err := row.Scan(&t.ID, &t.UserID, &t.MicrosoftEmail, &t.AccessToken, &t.RefreshToken, &expiry, &t.AutoSync, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.TokenExpiry = expiry
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	return &t, nil
}

// DeleteUserO365Token removes the Office 365 token record for a user (disconnecting O365).
func (d *DB) DeleteUserO365Token(userID int64) error {
	dl := d.dialect
	_, err := d.core.Exec(dl.rebind(`DELETE FROM user_o365_tokens WHERE user_id = ?`), userID)
	return err
}

// SetUserO365AutoSync updates the auto_sync preference for a user.
func (d *DB) SetUserO365AutoSync(userID int64, autoSync bool) error {
	dl := d.dialect
	_, err := d.core.Exec(dl.rebind(`UPDATE user_o365_tokens SET auto_sync = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`), autoSync, userID)
	return err
}

// GetCalendarSyncEvents returns all external calendar events synchronized for a user in a date range.
func (d *DB) GetCalendarSyncEvents(userID int64, startDate, endDate string) ([]models.CalendarSyncEvent, error) {
	dl := d.dialect
	query := `SELECT id, user_id, date, half, status_id, o365_event_id, o365_show_as, synced_at FROM calendar_sync_events WHERE user_id = ? AND date >= ? AND date <= ? ORDER BY date ASC`
	rows, err := d.presence.Query(dl.rebind(query), userID, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var events []models.CalendarSyncEvent
	for rows.Next() {
		var e models.CalendarSyncEvent
		var syncedAt time.Time
		if err := rows.Scan(&e.ID, &e.UserID, &e.Date, &e.Half, &e.StatusID, &e.O365EventID, &e.O365ShowAs, &syncedAt); err != nil {
			return nil, err
		}
		e.SyncedAt = syncedAt
		events = append(events, e)
	}
	return events, rows.Err()
}

// SaveCalendarSyncEvent inserts or updates a synchronized calendar event record.
func (d *DB) SaveCalendarSyncEvent(event models.CalendarSyncEvent) error {
	dl := d.dialect
	var count int
	err := d.presence.QueryRow(dl.rebind(`SELECT COUNT(*) FROM calendar_sync_events WHERE user_id = ? AND date = ? AND half = ?`), event.UserID, event.Date, event.Half).Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		query := `UPDATE calendar_sync_events SET status_id = ?, o365_event_id = ?, o365_show_as = ?, synced_at = CURRENT_TIMESTAMP WHERE user_id = ? AND date = ? AND half = ?`
		_, err = d.presence.Exec(dl.rebind(query), event.StatusID, event.O365EventID, event.O365ShowAs, event.UserID, event.Date, event.Half)
		return err
	}

	query := `INSERT INTO calendar_sync_events (user_id, date, half, status_id, o365_event_id, o365_show_as, synced_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`
	_, err = d.presence.Exec(dl.rebind(query), event.UserID, event.Date, event.Half, event.StatusID, event.O365EventID, event.O365ShowAs)
	return err
}

// DeleteCalendarSyncEvent removes a single sync event record by user, date and half.
func (d *DB) DeleteCalendarSyncEvent(userID int64, date, half string) error {
	dl := d.dialect
	_, err := d.presence.Exec(dl.rebind(`DELETE FROM calendar_sync_events WHERE user_id = ? AND date = ? AND half = ?`), userID, date, half)
	return err
}

// DeleteAllCalendarSyncEventsForUser removes all sync events for a user (e.g. on disconnect).
func (d *DB) DeleteAllCalendarSyncEventsForUser(userID int64) error {
	dl := d.dialect
	_, err := d.presence.Exec(dl.rebind(`DELETE FROM calendar_sync_events WHERE user_id = ?`), userID)
	return err
}
