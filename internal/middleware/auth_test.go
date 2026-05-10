package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/myPresence/internal/config"
	"github.com/matoy/myPresence/internal/db"
)

// newMiddlewareTestDB opens an isolated SQLite DB for middleware tests.
func newMiddlewareTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	d.SetBcryptCost(4)
	return d
}

// -----------------------------------------------------------------------
// Auth / AuthWithOptions — session cookie
// -----------------------------------------------------------------------

func TestAuth_ValidSession_InjectsUser(t *testing.T) {
	d := newMiddlewareTestDB(t)
	uid, err := d.CreateLocalUser("auth@example.com", "Auth User", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var gotUser bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := GetUser(r); u != nil {
			gotUser = true
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := Auth(d, inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !gotUser {
		t.Error("expected user to be injected in context")
	}
}

func TestAuth_NoCookie_RedirectsToLogin(t *testing.T) {
	d := newMiddlewareTestDB(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := Auth(d, inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("expected /login, got %q", loc)
	}
}

func TestAuth_InvalidCookie_RedirectsToLogin(t *testing.T) {
	d := newMiddlewareTestDB(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := Auth(d, inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "invalid-token-xyz"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", rec.Code)
	}
}

// -----------------------------------------------------------------------
// Auth / AuthWithOptions — Bearer PAT
// -----------------------------------------------------------------------

func TestAuthWithOptions_BearerToken_Valid(t *testing.T) {
	d := newMiddlewareTestDB(t)
	uid, err := d.CreateLocalUser("pat@example.com", "PAT User", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	raw, _, err := d.CreatePAT(uid, "test pat", nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	var gotUser bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetUser(r) != nil {
			gotUser = true
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := AuthWithOptions(d, true, inner)
	req := httptest.NewRequest(http.MethodGet, "/api/something", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !gotUser {
		t.Error("expected user from PAT to be in context")
	}
}

func TestAuthWithOptions_BearerToken_Invalid_Returns401(t *testing.T) {
	d := newMiddlewareTestDB(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := AuthWithOptions(d, true, inner)
	req := httptest.NewRequest(http.MethodGet, "/api/something", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-xyz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthWithOptions_BearerDisabled_FallsBackToSession(t *testing.T) {
	d := newMiddlewareTestDB(t)
	uid, _ := d.CreateLocalUser("sess@example.com", "Sess", "password1")
	tok, _ := d.CreateSession(uid)

	var gotUser bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetUser(r) != nil {
			gotUser = true
		}
		w.WriteHeader(http.StatusOK)
	})

	// bearerEnabled=false: Bearer header is ignored, session cookie used
	handler := AuthWithOptions(d, false, inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !gotUser {
		t.Error("expected user from session cookie")
	}
}

// -----------------------------------------------------------------------
// OptionalAuth
// -----------------------------------------------------------------------

func TestOptionalAuth_WithValidSession_InjectsUser(t *testing.T) {
	d := newMiddlewareTestDB(t)
	uid, _ := d.CreateLocalUser("opt@example.com", "Opt", "password1")
	tok, _ := d.CreateSession(uid)

	var gotUser bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetUser(r) != nil {
			gotUser = true
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := OptionalAuth(d, inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !gotUser {
		t.Error("expected user in context")
	}
}

func TestOptionalAuth_WithoutSession_StillCallsNext(t *testing.T) {
	d := newMiddlewareTestDB(t)
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := OptionalAuth(d, inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("expected next handler to be called even without session")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// -----------------------------------------------------------------------
// NewLoginRateLimiter / Close
// -----------------------------------------------------------------------

func TestNewLoginRateLimiter_StartAndClose(t *testing.T) {
	l := NewLoginRateLimiter()
	if l == nil {
		t.Fatal("expected non-nil limiter")
	}
	// Close should not panic and should be idempotent
	l.Close()
	l.Close() // idempotent
}

func TestLoginRateLimiter_CloseNil(t *testing.T) {
	var l *LoginRateLimiter
	// Close on nil should be a no-op
	l.Close()
}
