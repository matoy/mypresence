package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"presence-app/internal/config"
	"presence-app/internal/middleware"
)

// -----------------------------------------------------------------------
// InitSAML — disabled path (0 config)
// -----------------------------------------------------------------------

func TestInitSAML_Disabled(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AuthHandler{DB: d, Render: noRender, Config: &config.Config{SAMLEnabled: false}}
	if err := h.InitSAML(); err != nil {
		t.Fatalf("expected nil error when SAML disabled, got %v", err)
	}
}

// -----------------------------------------------------------------------
// generateSelfSignedCert
// -----------------------------------------------------------------------

func TestGenerateSelfSignedCertExtra(t *testing.T) {
	cert, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("expected non-empty certificate")
	}
}

// -----------------------------------------------------------------------
// LocalLogin — blocked by rate limiter path
// -----------------------------------------------------------------------

func TestLocalLogin_UserNotFound(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AuthHandler{DB: d, Render: noRender, Config: &config.Config{AppName: "Test"}}

	// Login with email that doesn't exist
	body := []byte("username=nonexistent%40test.com&password=somepassword")
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.LocalLogin(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ActivityPage — as global admin (default path)
// -----------------------------------------------------------------------

func TestActivityPage_AsAdmin(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ActivityHandler{DB: d, Render: noRender}

	d.CreateTeam("ActivityAdminTeam") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/activity", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// FloorplanPage — with date param
// -----------------------------------------------------------------------

func TestFloorplanPage_WithDate(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP PageDate", 0)
	req := createAdminReq(t, d, http.MethodGet, "/floorplan/"+strconvI64(fpID)+"?date=2026-06-01", nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.FloorplanPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// CancelReservation — with ID
// -----------------------------------------------------------------------

func TestCancelReservation_WithID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodDelete, "/api/reservations/42", nil)
	req.SetPathValue("id", "42")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CancelReservation)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ForgotPasswordPost — empty email
// -----------------------------------------------------------------------

func TestForgotPasswordPost_EmptyEmail(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	cfg := &config.Config{AppName: "Test"}
	var rendered string
	h := &ResetPasswordHandler{DB: d, Config: cfg, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}

	body := []byte("email=")
	req := httptest.NewRequest(http.MethodPost, "/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.ForgotPasswordPost(w, req)
	if rendered != "forgot_password" {
		t.Fatalf("expected forgot_password render, got %q", rendered)
	}
}

// -----------------------------------------------------------------------
// UserLogsPage — with valid ID
// -----------------------------------------------------------------------

func TestUserLogsPage_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("userlogs@test.com", "UserLogs", "password1")
	req := createAdminReq(t, d, http.MethodGet, "/admin/users/"+strconvI64(uid)+"/logs", nil)
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UserLogsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// Logout handler
// -----------------------------------------------------------------------

func TestLogout_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AuthHandler{DB: d, Render: noRender, Config: &config.Config{}}

	uid, _ := d.CreateLocalUser("logout@test.com", "Logout", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.Logout(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// SeatsAPI — missing floorplan
// -----------------------------------------------------------------------

func TestSeatsAPI_MissingFloorplanID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodGet, "/api/seats?date=2026-06-01", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SeatsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing floorplan_id, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// AdminProjectsAPI — with team filter
// -----------------------------------------------------------------------

func TestAdminProjectsAPI_WithTeam(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	tid, _ := d.CreateTeam("API Team Filter")
	req := createAdminReq(t, d, http.MethodGet, "/api/admin/projects?team_id="+strconvI64(tid), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
