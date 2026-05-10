package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/matoy/myPresence/internal/config"
)

// -----------------------------------------------------------------------
// LocalLogin — admin credentials valid but user not in DB
// Covers: recordFailure(); http.Redirect(w, r, "/login?error=Internal+error", …)
// -----------------------------------------------------------------------

func TestLocalLogin_AdminNotInDB(t *testing.T) {
	d := newResetTestDB(t)
	d.SetBcryptCost(4)

	h := &AuthHandler{
		DB:     d,
		Config: &config.Config{AdminUser: "notindb@test.com", AdminPassword: "adminpass"},
	}

	form := url.Values{}
	form.Set("username", "notindb@test.com")
	form.Set("password", "adminpass") // correct password, but user not in DB
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	h.LocalLogin(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Fatalf("expected error redirect, got %q", w.Header().Get("Location"))
	}
}

// -----------------------------------------------------------------------
// ForgotPasswordPost — valid local user (rawToken != "")
// Covers: resetURL assignment, body/subject build, go func() launch, renderSent()
// -----------------------------------------------------------------------

func TestForgotPasswordPost_ValidLocalUser(t *testing.T) {
	database := newResetTestDB(t)
	database.SetBcryptCost(4)

	if _, err := database.CreateLocalUser("fpvalid@test.com", "FP Valid", "password1"); err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}

	var rendered bool
	h := &ResetPasswordHandler{
		DB: database,
		Config: &config.Config{
			AppName:  "myPresence",
			SMTPURL:  "smtp://127.0.0.1:1", // will fail in goroutine — that's fine
			SMTPFrom: "noreply@test.com",
		},
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			rendered = true
		},
	}

	form := url.Values{}
	form.Set("email", "fpvalid@test.com")
	req := httptest.NewRequest(http.MethodPost, "/forgot-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	h.ForgotPasswordPost(w, req)

	if !rendered {
		t.Fatal("expected Render to be called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ImpersonateExitPost — full success path (rotates session)
// -----------------------------------------------------------------------

func TestImpersonateExitPost_Success(t *testing.T) {
	d := newResetTestDB(t)
	d.SetBcryptCost(4)

	// Create an admin and a regular user
	adminUID, err := d.CreateLocalUser("impexit_admin@test.com", "Admin", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser admin: %v", err)
	}
	d.UpdateUserRoles(adminUID, "global") //nolint:errcheck

	normalUID, err := d.CreateLocalUser("impexit_user@test.com", "User", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser user: %v", err)
	}

	// Create impersonation sessions: admin's real session + impersonated session
	adminToken, err := d.CreateSession(adminUID)
	if err != nil {
		t.Fatalf("CreateSession admin: %v", err)
	}
	impToken, err := d.CreateSession(normalUID)
	if err != nil {
		t.Fatalf("CreateSession imp: %v", err)
	}

	h := &SettingsHandler{DB: d, Render: noRender}

	req := httptest.NewRequest(http.MethodPost, "/settings/impersonate/exit", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: impToken})
	req.AddCookie(&http.Cookie{Name: "real_session", Value: adminToken})

	w := httptest.NewRecorder()
	h.ImpersonateExitPost(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	// Should redirect to /
	if w.Header().Get("Location") != "/" {
		t.Fatalf("expected redirect to /, got %q", w.Header().Get("Location"))
	}
}
