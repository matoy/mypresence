package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeadersHTTPAndHTTPS(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := SecurityHeaders(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS should be empty on plain HTTP, got %q", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Forwarded-Proto", "https")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if got := w2.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("expected HSTS on https")
	}
}

func TestTrimSpace(t *testing.T) {
	if got := trimSpace("\t  abc  \t"); got != "abc" {
		t.Fatalf("trimSpace = %q", got)
	}
	if got := trimSpace("   "); got != "" {
		t.Fatalf("trimSpace all spaces = %q", got)
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.1")
	if got := clientIP(req); got != "203.0.113.10" {
		t.Fatalf("clientIP XFF = %q", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "10.0.0.2:5678"
	if got := clientIP(req2); got != "10.0.0.2" {
		t.Fatalf("clientIP remote addr = %q", got)
	}
}

func TestLoginRateLimiterFlow(t *testing.T) {
	l := &LoginRateLimiter{attempts: make(map[string]*loginAttempt)}
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "10.1.1.1:1234"

	if !l.Allow(req) {
		t.Fatal("fresh IP should be allowed")
	}
	for i := 0; i < loginMaxFailures; i++ {
		l.RecordFailure(req)
	}
	if l.Allow(req) {
		t.Fatal("expected blocked IP after max failures")
	}
	l.Reset(req)
	if !l.Allow(req) {
		t.Fatal("expected allowed after reset")
	}
}
