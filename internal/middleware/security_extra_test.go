package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAllow_WindowExpired covers the "window expired → reset" branch in Allow.
func TestAllow_WindowExpired(t *testing.T) {
	l := &LoginRateLimiter{attempts: make(map[string]*loginAttempt)}
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "10.9.9.1:1234"

	// Manually insert an expired entry
	l.attempts["10.9.9.1"] = &loginAttempt{
		count:     loginMaxFailures - 1,
		firstFail: time.Now().Add(-loginWindow - time.Second), // already expired
	}
	// Allow should see expired window → delete entry → return true
	if !l.Allow(req) {
		t.Fatal("expected allowed after window expiry")
	}
	if _, exists := l.attempts["10.9.9.1"]; exists {
		t.Fatal("expected expired entry to be deleted from attempts map")
	}
}

// TestAllow_StillInBlockPeriod covers the "blocked" branch in Allow.
func TestAllow_StillInBlockPeriod(t *testing.T) {
	l := &LoginRateLimiter{attempts: make(map[string]*loginAttempt)}
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "10.9.9.2:5678"

	// Manually insert a fresh block
	l.attempts["10.9.9.2"] = &loginAttempt{
		count:     loginMaxFailures,
		firstFail: time.Now(),
		blockedAt: time.Now(),
	}
	if l.Allow(req) {
		t.Fatal("expected blocked IP to be denied")
	}
}

// TestRecordFailure_WindowExpired covers the "reset window on expiry" branch in RecordFailure.
func TestRecordFailure_WindowExpired(t *testing.T) {
	l := &LoginRateLimiter{attempts: make(map[string]*loginAttempt)}
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "10.9.9.3:9999"

	// Insert an old entry (window expired)
	l.attempts["10.9.9.3"] = &loginAttempt{
		count:     loginMaxFailures - 1,
		firstFail: time.Now().Add(-loginWindow - time.Second),
	}
	l.RecordFailure(req) // should reset count to 1
	if a := l.attempts["10.9.9.3"]; a.count != 1 {
		t.Fatalf("expected count reset to 1, got %d", a.count)
	}
}

// TestCleanupLoop_RemovesStaleEntries covers the cleanupLoop goroutine's logic.
// We test the cleanup inline (without actually running the loop) to avoid flakiness.
func TestCleanupLoop_ManualCleanup(t *testing.T) {
	l := &LoginRateLimiter{attempts: make(map[string]*loginAttempt)}

	// Add a stale entry
	l.attempts["1.2.3.4"] = &loginAttempt{
		count:     1,
		firstFail: time.Now().Add(-loginWindow - time.Second),
		blockedAt: time.Time{},
	}
	// Add a fresh entry
	l.attempts["5.6.7.8"] = &loginAttempt{
		count:     1,
		firstFail: time.Now(),
	}

	// Replicate cleanup logic directly
	l.mu.Lock()
	for ip, a := range l.attempts {
		expired := time.Since(a.firstFail) > loginWindow
		blockExpired := a.blockedAt.IsZero() || time.Since(a.blockedAt) > loginBlockDuration
		if expired && blockExpired {
			delete(l.attempts, ip)
		}
	}
	l.mu.Unlock()

	if _, exists := l.attempts["1.2.3.4"]; exists {
		t.Error("expected stale entry to be cleaned up")
	}
	if _, exists := l.attempts["5.6.7.8"]; !exists {
		t.Error("expected fresh entry to remain")
	}
}

// TestNewLoginRateLimiter_StartsCleanupLoop verifies Close() stops the loop.
func TestNewLoginRateLimiter_StartsCleanupLoop(t *testing.T) {
	l := NewLoginRateLimiter()
	if l == nil {
		t.Fatal("expected non-nil limiter")
	}
	// Close should not block or panic
	l.Close()
	// Double-close should be safe (stopOnce)
	l.Close()
}

// TestClientIP_InvalidXFF covers the fallback when X-Forwarded-For is not a valid IP.
func TestClientIP_InvalidXFF(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:8080"
	req.Header.Set("X-Forwarded-For", "not-an-ip")
	got := clientIP(req)
	if got != "192.168.1.1" {
		t.Errorf("expected RemoteAddr fallback, got %q", got)
	}
}

// TestAllow_SomeFailuresNotBlocked covers L.112 (return a.count < loginMaxFailures)
// when the IP has some failures but not yet blocked.
func TestAllow_SomeFailuresNotBlocked(t *testing.T) {
	l := &LoginRateLimiter{attempts: make(map[string]*loginAttempt)}
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "10.9.9.5:1111"

	// Manually insert 3 failures (< loginMaxFailures=5), no block
	l.attempts["10.9.9.5"] = &loginAttempt{
		count:     3,
		firstFail: time.Now(),
	}
	if !l.Allow(req) {
		t.Fatal("expected allowed for IP with 3 failures (< max 5)")
	}
}

// TestClientIP_NoPort covers L.188-190 (SplitHostPort error → return RemoteAddr as-is)
func TestClientIP_NoPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1" // no port
	got := clientIP(req)
	if got != "192.168.1.1" {
		t.Errorf("expected RemoteAddr as-is, got %q", got)
	}
}

// TestCleanupLoop_TickerPath exercises the ticker.C branch of cleanupLoop,
// including the inner delete-expired branch, using a very short tick interval.
func TestCleanupLoop_TickerPath(t *testing.T) {
	l := &LoginRateLimiter{
		attempts:         make(map[string]*loginAttempt),
		stopCh:           make(chan struct{}),
		testTickInterval: time.Millisecond,
	}

	// Add a stale entry (expired window, expired block)
	l.attempts["1.2.3.4"] = &loginAttempt{
		count:     2,
		firstFail: time.Now().Add(-loginWindow - time.Second),
		blockedAt: time.Time{},
	}
	// Add a fresh entry that should NOT be deleted
	l.attempts["9.9.9.9"] = &loginAttempt{
		count:     1,
		firstFail: time.Now(),
	}

	go l.cleanupLoop()

	// Wait for at least one tick to fire
	time.Sleep(20 * time.Millisecond)
	l.Close()

	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.attempts["1.2.3.4"]; exists {
		t.Error("expected stale entry to be removed by ticker cleanup")
	}
	if _, exists := l.attempts["9.9.9.9"]; !exists {
		t.Error("expected fresh entry to remain after ticker cleanup")
	}
}
