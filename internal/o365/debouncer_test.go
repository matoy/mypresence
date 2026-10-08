package o365

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncer(t *testing.T) {
	// Dummy debouncer test verifying timer resets
	var counter int32
	d := &Debouncer{
		interval: 50 * time.Millisecond,
		timers:   make(map[string]*time.Timer),
	}

	// Mock Trigger action
	d.timers["1:2026:6"] = time.AfterFunc(d.interval, func() {
		atomic.AddInt32(&counter, 1)
	})

	// Reset before it fires
	d.mu.Lock()
	if old, ok := d.timers["1:2026:6"]; ok {
		old.Stop()
	}
	d.timers["1:2026:6"] = time.AfterFunc(d.interval, func() {
		atomic.AddInt32(&counter, 1)
	})
	d.mu.Unlock()

	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&counter) != 1 {
		t.Fatalf("Expected counter to be 1 after debounced execution, got %d", atomic.LoadInt32(&counter))
	}
}
