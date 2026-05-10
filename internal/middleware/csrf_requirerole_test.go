package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matoy/myPresence/internal/models"
)

// ─── GenerateCSRFToken ────────────────────────────────────────────────────────

func TestGenerateCSRFToken_Deterministic(t *testing.T) {
	tok1 := GenerateCSRFToken("secret", "session123")
	tok2 := GenerateCSRFToken("secret", "session123")
	if tok1 != tok2 {
		t.Errorf("same inputs should always produce same token: %q != %q", tok1, tok2)
	}
}

func TestGenerateCSRFToken_DiffersWithDifferentSession(t *testing.T) {
	tok1 := GenerateCSRFToken("secret", "sessionA")
	tok2 := GenerateCSRFToken("secret", "sessionB")
	if tok1 == tok2 {
		t.Error("different session tokens should produce different CSRF tokens")
	}
}

func TestGenerateCSRFToken_DiffersWithDifferentKey(t *testing.T) {
	tok1 := GenerateCSRFToken("keyA", "session")
	tok2 := GenerateCSRFToken("keyB", "session")
	if tok1 == tok2 {
		t.Error("different secret keys should produce different CSRF tokens")
	}
}

// ─── ValidateCSRF ─────────────────────────────────────────────────────────────

func TestValidateCSRF_GETPassesThrough(t *testing.T) {
	called := false
	handler := ValidateCSRF("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("GET should pass through CSRF validation without calling next")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestValidateCSRF_JSONPostPassesThrough(t *testing.T) {
	called := false
	handler := ValidateCSRF("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/something", strings.NewReader(`{"foo":"bar"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("JSON POST should pass through (not form-encoded)")
	}
}

func TestValidateCSRF_FormPostWithoutCookieForbidden(t *testing.T) {
	handler := ValidateCSRF("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := "csrf_token=anything&foo=bar"
	req := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// No session cookie set
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 without session cookie, got %d", rec.Code)
	}
}

func TestValidateCSRF_FormPostWithWrongTokenForbidden(t *testing.T) {
	handler := ValidateCSRF("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := "csrf_token=wrong-token&foo=bar"
	req := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: "my-session-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 with wrong CSRF token, got %d", rec.Code)
	}
}

func TestValidateCSRF_FormPostWithValidTokenAllowed(t *testing.T) {
	const secret = "my-test-secret"
	const sessionToken = "my-session-token"
	validCSRF := GenerateCSRFToken(secret, sessionToken)

	called := false
	handler := ValidateCSRF(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	body := "csrf_token=" + validCSRF + "&foo=bar"
	req := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should be called with valid CSRF token")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestValidateCSRF_MultipartFormPostChecked(t *testing.T) {
	handler := ValidateCSRF("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(""))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	req.AddCookie(&http.Cookie{Name: "session", Value: "tok"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// wrong token (empty) → 403
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for multipart with wrong token, got %d", rec.Code)
	}
}

// ─── RequireRole ──────────────────────────────────────────────────────────────

func userInCtx(r *http.Request, user *models.User) *http.Request {
	ctx := context.WithValue(r.Context(), userContextKey, user)
	return r.WithContext(ctx)
}

func TestRequireRole_NoUserForbidden(t *testing.T) {
	handler := RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 with no user, got %d", rec.Code)
	}
}

func TestRequireRole_WrongRoleForbidden(t *testing.T) {
	handler := RequireRole(models.RoleGlobal)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = userInCtx(req, &models.User{ID: 1, Roles: models.RoleBasic})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for basic user on global route, got %d", rec.Code)
	}
}

func TestRequireRole_CorrectRoleAllowed(t *testing.T) {
	called := false
	handler := RequireRole(models.RoleTeamManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/teams", nil)
	req = userInCtx(req, &models.User{ID: 1, Roles: models.RoleTeamManager})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should be called for user with matching role")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRequireRole_GlobalUserBypassesAllRoles(t *testing.T) {
	called := false
	handler := RequireRole(models.RoleTeamManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/teams", nil)
	req = userInCtx(req, &models.User{ID: 1, Roles: models.RoleGlobal})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("global user should bypass role check")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRequireRole_MultipleRolesAnyMatch(t *testing.T) {
	handler := RequireRole(models.RoleProjectsAdmin, models.RoleProjectsViewer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, role := range []string{models.RoleProjectsAdmin, models.RoleProjectsViewer} {
		req := httptest.NewRequest(http.MethodGet, "/admin/projects-report", nil)
		req = userInCtx(req, &models.User{ID: 1, Roles: role})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("role %q: expected 200, got %d", role, rec.Code)
		}
	}
}

func TestRequireRole_MultipleRolesNoneMatchForbidden(t *testing.T) {
	handler := RequireRole(models.RoleProjectsAdmin, models.RoleProjectsViewer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/projects-report", nil)
	req = userInCtx(req, &models.User{ID: 1, Roles: models.RoleBasic})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for basic user, got %d", rec.Code)
	}
}
