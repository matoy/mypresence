package o365

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Debouncer coordinates debounced background synchronization calls.
type Debouncer struct {
	client   *Client
	interval time.Duration
	mu       sync.Mutex
	timers   map[string]*time.Timer
}

// NewDebouncer creates a Debouncer with the given delay (default 3 seconds if 0).
func NewDebouncer(client *Client, interval time.Duration) *Debouncer {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	return &Debouncer{
		client:   client,
		interval: interval,
		timers:   make(map[string]*time.Timer),
	}
}

// Trigger schedules a debounced sync for the given user, year, and month.
// If another call arrives before the timer fires, the timer is reset.
func (d *Debouncer) Trigger(userID int64, year, month int) {
	if d == nil || d.client == nil {
		return
	}

	key := fmt.Sprintf("%d:%d:%d", userID, year, month)
	d.mu.Lock()
	defer d.mu.Unlock()

	if t, exists := d.timers[key]; exists {
		t.Stop()
	}

	d.timers[key] = time.AfterFunc(d.interval, func() {
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()

		rep, err := d.client.SyncMonth(userID, year, month)
		if err != nil {
			slog.Warn("o365.debouncer: background sync failed", "user_id", userID, "year", year, "month", month, "err", err)
			return
		}
		slog.Debug("o365.debouncer: background sync finished", "user_id", userID, "year", year, "month", month, "created", rep.Created, "updated", rep.Updated, "deleted", rep.Deleted)
	})
}
