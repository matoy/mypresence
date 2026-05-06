package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"presence-app/internal/config"
	"presence-app/internal/db"
	"presence-app/internal/middleware"
	"presence-app/internal/models"
)

func newHandlersTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestLoginPageRenderAndRedirect(t *testing.T) {
	database := newHandlersTestDB(t)

	var page string
	var data map[string]interface{}
	h := &AuthHandler{
		DB:     database,
		Config: &config.Config{},
		Render: func(w http.ResponseWriter, r *http.Request, p string, d interface{}) {
			page = p
			data = d.(map[string]interface{})
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/login?error=bad", nil)
	w := httptest.NewRecorder()
	h.LoginPage(w, req)
	if page != "login" {
		t.Fatalf("expected login page, got %q", page)
	}
	if data["Flash"] != "bad" {
		t.Fatalf("expected flash=bad, got %#v", data)
	}

	uid, err := database.CreateLocalUser("login@example.com", "Login", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := database.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	wrapped := middleware.Auth(database, http.HandlerFunc(h.LoginPage))
	req2 := httptest.NewRequest(http.MethodGet, "/login", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w2 := httptest.NewRecorder()
	wrapped.ServeHTTP(w2, req2)
	if w2.Code != http.StatusSeeOther || w2.Header().Get("Location") != "/" {
		t.Fatalf("expected redirect to /, got code=%d location=%q", w2.Code, w2.Header().Get("Location"))
	}
}

func TestLogoutClearsSession(t *testing.T) {
	database := newHandlersTestDB(t)
	uid, err := database.CreateLocalUser("logout@example.com", "Logout", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := database.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	h := &AuthHandler{DB: database}
	wrapped := middleware.Auth(database, http.HandlerFunc(h.Logout))
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("expected redirect to /login, got code=%d location=%q", w.Code, w.Header().Get("Location"))
	}
	if _, err := database.GetSessionUser(tok); err == nil {
		t.Fatal("expected session to be deleted")
	}
}

func TestResetPasswordPagesRender(t *testing.T) {
	database := newHandlersTestDB(t)
	var page string
	var data map[string]interface{}
	h := &ResetPasswordHandler{
		DB:     database,
		Config: &config.Config{},
		Render: func(w http.ResponseWriter, r *http.Request, p string, d interface{}) {
			page = p
			data = d.(map[string]interface{})
		},
	}

	h.ForgotPasswordPage(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/forgot-password", nil))
	if page != "forgot_password" || data["Sent"] != false {
		t.Fatalf("unexpected forgot page data: page=%q data=%#v", page, data)
	}

	req := httptest.NewRequest(http.MethodGet, "/reset-password?token=abc", nil)
	h.ResetPasswordPage(httptest.NewRecorder(), req)
	if page != "reset_password" || data["Token"] != "abc" || data["Done"] != false {
		t.Fatalf("unexpected reset page data: page=%q data=%#v", page, data)
	}
}

func TestSettingsPagesAndImpersonateExit(t *testing.T) {
	database := newHandlersTestDB(t)
	h := &SettingsHandler{DB: database, Render: func(http.ResponseWriter, *http.Request, string, interface{}) {}}

	uid, err := database.CreateLocalUser("settings@example.com", "Settings", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	localTok, err := database.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession local: %v", err)
	}
	nonLocal, err := database.UpsertUser("saml-settings@example.com", "SAML User")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	nonLocalTok, err := database.CreateSession(nonLocal.ID)
	if err != nil {
		t.Fatalf("CreateSession non-local: %v", err)
	}

	called := false
	h.Render = func(w http.ResponseWriter, r *http.Request, p string, d interface{}) {
		called = true
		if p != "settings_change_password" {
			t.Fatalf("unexpected page %q", p)
		}
	}
	wrappedChangePage := middleware.Auth(database, http.HandlerFunc(h.ChangePasswordPage))
	req := httptest.NewRequest(http.MethodGet, "/settings/change-password", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: localTok})
	wrappedChangePage.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Fatal("expected render for local user")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/settings/change-password", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: nonLocalTok})
	w2 := httptest.NewRecorder()
	wrappedChangePage.ServeHTTP(w2, req2)
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect for non-local user, got %d", w2.Code)
	}

	req3 := httptest.NewRequest(http.MethodPost, "/settings/impersonate/exit", nil)
	w3 := httptest.NewRecorder()
	h.ImpersonateExitPost(w3, req3)
	if w3.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect when real_session missing, got %d", w3.Code)
	}

	adminID, err := database.CreateLocalUser("admin-exit@example.com", "Admin Exit", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser admin: %v", err)
	}
	if err := database.UpdateUserRoles(adminID, models.RoleGlobal); err != nil {
		t.Fatalf("UpdateUserRoles: %v", err)
	}
	realTok, err := database.CreateSession(adminID)
	if err != nil {
		t.Fatalf("CreateSession real: %v", err)
	}
	impTok, err := database.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession imp: %v", err)
	}
	req4 := httptest.NewRequest(http.MethodPost, "/settings/impersonate/exit", nil)
	req4.AddCookie(&http.Cookie{Name: "real_session", Value: realTok})
	req4.AddCookie(&http.Cookie{Name: "session", Value: impTok})
	w4 := httptest.NewRecorder()
	h.ImpersonateExitPost(w4, req4)
	if w4.Code != http.StatusSeeOther || w4.Header().Get("Location") != "/" {
		t.Fatalf("expected success redirect to /, got code=%d location=%q", w4.Code, w4.Header().Get("Location"))
	}
}

func TestHealthEndpoint(t *testing.T) {
	database := newHandlersTestDB(t)
	h := &HealthHandler{DB: database, StartedAt: time.Now().Add(-5 * time.Minute)}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	h.Health(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("unexpected content-type: %q", ct)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("unexpected payload: %#v", payload)
	}

	database.Close()
	w2 := httptest.NewRecorder()
	h.Health(w2, req)
	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with closed DB, got %d", w2.Code)
	}
}
