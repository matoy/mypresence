package o365

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/matoy/mypresence/internal/models"
)

// SyncReport summarizes the results of a month calendar synchronization.
type SyncReport struct {
	Year      int      `json:"year"`
	Month     int      `json:"month"`
	Created   int      `json:"created"`
	Updated   int      `json:"updated"`
	Deleted   int      `json:"deleted"`
	Unchanged int      `json:"unchanged"`
	Errors    []string `json:"errors,omitempty"`
}

// SyncMonth synchronizes a user's presence declarations for a given month with Office 365.
// It creates missing events, updates changed events, and removes cancelled or on-site declarations.
func (c *Client) SyncMonth(userID int64, year int, month int) (*SyncReport, error) {
	report := &SyncReport{
		Year:   year,
		Month:  month,
		Errors: make([]string, 0),
	}

	token, err := c.GetValidAccessToken(userID)
	if err != nil {
		return nil, fmt.Errorf("o365 access token: %w", err)
	}

	// Calculate date range for the requested month
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	lastDay := firstDay.AddDate(0, 1, -1)
	startDate := firstDay.Format("2006-01-02")
	endDate := lastDay.Format("2006-01-02")

	// 1. Fetch current presences from DB
	allPresences, err := c.db.GetPresences([]int64{userID}, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("get presences: %w", err)
	}
	userPresences := allPresences[userID]
	if userPresences == nil {
		userPresences = make(map[string]map[string]int64)
	}

	// 2. Fetch statuses map to look up showAs and names
	statusList, err := c.db.ListStatuses()
	if err != nil {
		return nil, fmt.Errorf("list statuses: %w", err)
	}
	statusMap := make(map[int64]models.Status, len(statusList))
	for _, s := range statusList {
		statusMap[s.ID] = s
	}

	// 3. Fetch existing calendar sync records for this month
	existingSyncList, err := c.db.GetCalendarSyncEvents(userID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("get sync events: %w", err)
	}
	existingMap := make(map[string]models.CalendarSyncEvent, len(existingSyncList))
	for _, e := range existingSyncList {
		existingMap[e.Date+"_"+e.Half] = e
	}

	// 4. Iterate over declared presences and synchronize
	for date, halves := range userPresences {
		for half, statusID := range halves {
			status, ok := statusMap[statusID]
			desiredShowAs := ""
			if ok {
				desiredShowAs = status.O365ShowAs()
			}

			key := date + "_" + half
			existing, hasExisting := existingMap[key]

			if desiredShowAs != "" {
				// Eligible for Office 365 calendar synchronization (remote work, business trip, leave, etc.)
				subject := fmt.Sprintf("[%s] %s", c.cfg.AppName, status.Name)
				desc := fmt.Sprintf("Synchronisé automatiquement depuis %s le %s.", c.cfg.AppName, time.Now().Format("02/01/2006 15:04"))
				evt := Event{
					Subject:     subject,
					Date:        date,
					Half:        half,
					ShowAs:      desiredShowAs,
					Description: desc,
				}

				if !hasExisting {
					// Create event in Graph
					eventID, err := c.CreateEvent(token, evt)
					if err != nil {
						slog.Error("o365.sync: failed to create event", "user_id", userID, "date", date, "half", half, "err", err)
						report.Errors = append(report.Errors, fmt.Sprintf("%s (%s): %v", date, half, err))
						continue
					}
					syncRec := models.CalendarSyncEvent{
						UserID:      userID,
						Date:        date,
						Half:        half,
						StatusID:    status.ID,
						O365EventID: eventID,
						O365ShowAs:  desiredShowAs,
					}
					if err := c.db.SaveCalendarSyncEvent(syncRec); err != nil {
						slog.Error("o365.sync: failed to save sync event record", "user_id", userID, "date", date, "err", err)
					}
					report.Created++
				} else {
					// Event already synchronized: check if modified
					if existing.StatusID != status.ID || existing.O365ShowAs != desiredShowAs {
						err := c.UpdateEvent(token, existing.O365EventID, evt)
						if errors.Is(err, sql.ErrNoRows) {
							// Event was manually deleted in Outlook -> recreate it
							eventID, err := c.CreateEvent(token, evt)
							if err != nil {
								slog.Error("o365.sync: failed to recreate event after 404", "user_id", userID, "date", date, "err", err)
								report.Errors = append(report.Errors, fmt.Sprintf("%s (%s): %v", date, half, err))
								continue
							}
							existing.O365EventID = eventID
						} else if err != nil {
							slog.Error("o365.sync: failed to update event", "user_id", userID, "date", date, "err", err)
							report.Errors = append(report.Errors, fmt.Sprintf("%s (%s): %v", date, half, err))
							continue
						}

						existing.StatusID = status.ID
						existing.O365ShowAs = desiredShowAs
						_ = c.db.SaveCalendarSyncEvent(existing)
						report.Updated++
					} else {
						report.Unchanged++
					}
				}
				delete(existingMap, key)
			} else {
				// Not eligible (e.g. On-site presence): if previously synced, delete it from calendar
				if hasExisting {
					if err := c.DeleteEvent(token, existing.O365EventID); err != nil {
						slog.Warn("o365.sync: failed to delete event", "user_id", userID, "event_id", existing.O365EventID, "err", err)
					}
					_ = c.db.DeleteCalendarSyncEvent(userID, date, half)
					report.Deleted++
					delete(existingMap, key)
				}
			}
		}
	}

	// 5. Any remaining items in existingMap are presences that were cleared in MyPresence -> delete them
	for _, existing := range existingMap {
		if err := c.DeleteEvent(token, existing.O365EventID); err != nil {
			slog.Warn("o365.sync: failed to delete cleared event", "user_id", userID, "event_id", existing.O365EventID, "err", err)
		}
		_ = c.db.DeleteCalendarSyncEvent(userID, existing.Date, existing.Half)
		report.Deleted++
	}

	slog.Info("o365.sync completed",
		"user_id", userID,
		"year", year,
		"month", month,
		"created", report.Created,
		"updated", report.Updated,
		"deleted", report.Deleted,
		"unchanged", report.Unchanged,
		"errors", len(report.Errors),
	)

	return report, nil
}
