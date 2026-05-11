package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

func newSettingsTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func newAuthedRequest(t *testing.T, database *db.DB, email, name, password string) (*http.Request, *models.User) {
	t.Helper()
	uid, err := database.CreateLocalUser(email, name, password)
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	u, err := database.GetUserByID(uid)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	tok, err := database.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	return req, u
}

func TestChangePasswordPostSuccess(t *testing.T) {
	database := newSettingsTestDB(t)
	h := &SettingsHandler{DB: database}

	baseReq, user := newAuthedRequest(t, database, "me@example.com", "Me", "oldpassword")
	form := url.Values{}
	form.Set("current_password", "oldpassword")
	form.Set("new_password", "newpassword")
	form.Set("confirm_password", "newpassword")
	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range baseReq.Cookies() {
		req.AddCookie(c)
	}

	handler := middleware.Auth(database, http.HandlerFunc(h.ChangePasswordPost))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "success=") {
		t.Fatalf("expected success redirect, got %q", loc)
	}

	dbUser, err := database.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if !database.CheckPassword(dbUser.ID, dbUser.PasswordHash, "newpassword") {
		t.Fatal("expected password to be changed")
	}
}

func TestChangePasswordPostWrongCurrent(t *testing.T) {
	database := newSettingsTestDB(t)
	h := &SettingsHandler{DB: database}

	baseReq, _ := newAuthedRequest(t, database, "me2@example.com", "Me2", "oldpassword")
	form := url.Values{}
	form.Set("current_password", "wrong")
	form.Set("new_password", "newpassword")
	form.Set("confirm_password", "newpassword")
	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range baseReq.Cookies() {
		req.AddCookie(c)
	}

	handler := middleware.Auth(database, http.HandlerFunc(h.ChangePasswordPost))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Fatalf("expected error redirect, got %q", loc)
	}
}

func TestImpersonatePostForbiddenForNonGlobal(t *testing.T) {
	database := newSettingsTestDB(t)
	h := &SettingsHandler{DB: database}

	baseReq, _ := newAuthedRequest(t, database, "basic@example.com", "Basic", "password1")
	form := url.Values{}
	form.Set("login", "target@example.com")
	req := httptest.NewRequest(http.MethodPost, "/settings/impersonate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range baseReq.Cookies() {
		req.AddCookie(c)
	}

	handler := middleware.Auth(database, http.HandlerFunc(h.ImpersonatePost))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestImpersonatePostSuccessForGlobal(t *testing.T) {
	database := newSettingsTestDB(t)
	h := &SettingsHandler{DB: database}

	adminReq, admin := newAuthedRequest(t, database, "admin@example.com", "Admin", "password1")
	if err := database.UpdateUserRoles(admin.ID, models.RoleGlobal); err != nil {
		t.Fatalf("UpdateUserRoles: %v", err)
	}
	_, err := database.CreateLocalUser("target@example.com", "Target", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser target: %v", err)
	}

	form := url.Values{}
	form.Set("login", "target@example.com")
	req := httptest.NewRequest(http.MethodPost, "/settings/impersonate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range adminReq.Cookies() {
		req.AddCookie(c)
	}

	handler := middleware.Auth(database, http.HandlerFunc(h.ImpersonatePost))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	cookies := w.Result().Cookies()
	var hasSession, hasRealSession bool
	for _, c := range cookies {
		if c.Name == "session" && c.Value != "" {
			hasSession = true
		}
		if c.Name == "real_session" && c.Value != "" {
			hasRealSession = true
		}
	}
	if !hasSession || !hasRealSession {
		t.Fatalf("expected session + real_session cookies, got %#v", cookies)
	}
}
