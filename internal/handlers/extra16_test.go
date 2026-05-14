package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// SetProjectTime — invalid year → 400
// -----------------------------------------------------------------------

func TestSetProjectTime_ZeroYear(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	body, _ := json.Marshal(map[string]interface{}{
		"project_id": 1,
		"year":       0, // zero year → "Invalid parameters"
		"month":      6,
		"days":       1.0,
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/project-time", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for zero year, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ImpersonatePost — DB closed after auth (GetUserByEmail fails → redirect)
// -----------------------------------------------------------------------

func TestImpersonatePost_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &SettingsHandler{DB: d, Render: noRender}

	_, _ = d.CreateLocalUser("imptarget_err@test.com", "ImpTargetErr", "password1")

	adminUID, _ := d.CreateLocalUser("impadmin_err@test.com", "ImpAdminErr", "password1")
	d.UpdateUserRoles(adminUID, "global") //nolint:errcheck
	tok, _ := d.CreateSession(adminUID)

	body := []byte("login=imptarget_err%40test.com")
	req := httptest.NewRequest(http.MethodPost, "/settings/impersonate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)

	// Close all DB after auth so GetUserByEmail fails → redirects to /.
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.ImpersonatePost(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected redirect or 500, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ImpersonateExitPost — non-global real session → redirects to /login
// -----------------------------------------------------------------------

func TestImpersonateExitPost_NonGlobalRealSession(t *testing.T) {
	d := newExtraTestDB(t)
	h := &SettingsHandler{DB: d, Render: noRender}

	normalUID, _ := d.CreateLocalUser("impexitnonglobal@test.com", "NonGlobal", "password1")
	normalTok, _ := d.CreateSession(normalUID)

	impUID, _ := d.CreateLocalUser("impexitimp@test.com", "Imp", "password1")
	impTok, _ := d.CreateSession(impUID)

	req := httptest.NewRequest(http.MethodPost, "/settings/impersonate/exit", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: impTok})
	req.AddCookie(&http.Cookie{Name: "real_session", Value: normalTok})

	w := httptest.NewRecorder()
	h.ImpersonateExitPost(w, req)

	// Non-global real session → clears cookies and redirects to /login.
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Fatalf("expected /login, got %q", loc)
	}
}

// ChangePasswordPost — GetUserByID fails after DB close → redirect with error
func TestChangePasswordPost_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &SettingsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("changepwd_dberr@test.com", "ChangePwdErr", "oldpass12")
	tok, _ := d.CreateSession(uid)

	body := []byte("current_password=oldpass12&new_password=newpass99&confirm_password=newpass99")
	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)

	// Close DB after auth so GetUserByID fails → handler redirects with error message.
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.ChangePasswordPost(rw, r)
	})).ServeHTTP(w, req)

	// GetUserByID error → CheckPassword fails → redirect with "Mot de passe actuel incorrect".
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect on DB error, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// computeWorkingDays — weekday holiday with AllowImputed=false increments holidayCount
// -----------------------------------------------------------------------

func TestComputeWorkingDays_WeekdayHoliday(t *testing.T) {
	// June 2026: Mon June 1 is a weekday. Use it as a holiday with AllowImputed=false.
	holidays := []models.Holiday{
		{Date: "2026-06-01", AllowImputed: false},
	}
	working, holidayCount := computeWorkingDays(2026, 6, holidays)
	if working == 0 {
		t.Fatal("expected working days > 0")
	}
	if holidayCount != 1 {
		t.Fatalf("expected holidayCount=1, got %d", holidayCount)
	}
}

func TestComputeWorkingDays_WeekendHoliday(t *testing.T) {
	// June 7 2026 is a Sunday — should be skipped, holidayCount stays 0.
	holidays := []models.Holiday{
		{Date: "2026-06-07", AllowImputed: false},
	}
	_, holidayCount := computeWorkingDays(2026, 6, holidays)
	if holidayCount != 0 {
		t.Fatalf("expected holidayCount=0 for weekend holiday, got %d", holidayCount)
	}
}

func TestComputeWorkingDays_HolidayWithAllowImputed(t *testing.T) {
	// Weekday holiday but AllowImputed=true → not counted.
	holidays := []models.Holiday{
		{Date: "2026-06-01", AllowImputed: true},
	}
	_, holidayCount := computeWorkingDays(2026, 6, holidays)
	if holidayCount != 0 {
		t.Fatalf("expected holidayCount=0 when AllowImputed=true, got %d", holidayCount)
	}
}

// -----------------------------------------------------------------------
// LocalLogin — disabled user account → redirect with "Account disabled"
// -----------------------------------------------------------------------

func TestLocalLogin_DisabledUser3(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	uid, err := d.CreateLocalUser("disabled@test.com", "DisabledUser", "password123")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	if err := d.SetUserDisabled(uid, true); err != nil {
		t.Fatalf("SetUserDisabled: %v", err)
	}

	h := &AuthHandler{DB: d, Config: &config.Config{}, Render: noRender}

	body := []byte("username=disabled%40test.com&password=password123")
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	h.LocalLogin(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect for disabled user, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/login?error=Account+disabled" {
		t.Fatalf("expected Account+disabled redirect, got %q", loc)
	}
}

// -----------------------------------------------------------------------
// isTeamLeaderOf — covers the `return true` branch when leader and target share a team
// -----------------------------------------------------------------------

func TestIsTeamLeaderOf_SharedTeam(t *testing.T) {
	d := newExtraTestDB(t)

	teamID, err := d.CreateTeam("SharedTeam")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	leaderID, _ := d.CreateLocalUser("leader_shared@test.com", "Leader", "pass1234")
	targetID, _ := d.CreateLocalUser("target_shared@test.com", "Target", "pass1234")

	d.AddTeamMember(teamID, leaderID) //nolint:errcheck
	d.AddTeamMember(teamID, targetID) //nolint:errcheck

	if !isTeamLeaderOf(d, leaderID, targetID) {
		t.Fatal("expected isTeamLeaderOf to return true when they share a team")
	}
}

func TestIsTeamLeaderOf_NoSharedTeam(t *testing.T) {
	d := newExtraTestDB(t)

	t1, _ := d.CreateTeam("Team1NoShar")
	t2, _ := d.CreateTeam("Team2NoShar")

	leaderID, _ := d.CreateLocalUser("leader_noshar@test.com", "LeaderNoShar", "pass1234")
	targetID, _ := d.CreateLocalUser("target_noshar@test.com", "TargetNoShar", "pass1234")

	d.AddTeamMember(t1, leaderID) //nolint:errcheck
	d.AddTeamMember(t2, targetID) //nolint:errcheck

	if isTeamLeaderOf(d, leaderID, targetID) {
		t.Fatal("expected isTeamLeaderOf to return false when no shared team")
	}
}

// -----------------------------------------------------------------------

func TestLocalLogin_RateLimited2(t *testing.T) {
	d := newExtraTestDB(t)

	rl := middleware.NewLoginRateLimiter()
	defer rl.Close()
	h := &AuthHandler{DB: d, Config: &config.Config{}, RateLimiter: rl}

	const blockedIP = "10.0.0.1"

	// Saturate rate limiter for this IP (5 failures trigger block).
	for i := 0; i < 5; i++ {
		fakeReq := httptest.NewRequest(http.MethodPost, "/login", nil)
		fakeReq.RemoteAddr = blockedIP + ":1234"
		rl.RecordFailure(fakeReq)
	}

	body := []byte("username=admin&password=wrong")
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = blockedIP + ":5678"

	w := httptest.NewRecorder()
	h.LocalLogin(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect when rate-limited, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ForgotPasswordPost — rate limiter blocks
// -----------------------------------------------------------------------

func TestForgotPasswordPost_RateLimited2(t *testing.T) {
	d := newExtraTestDB(t)

	rl := middleware.NewLoginRateLimiter()
	defer rl.Close()
	h := &ResetPasswordHandler{DB: d, Config: &config.Config{}, Render: noRender, RateLimiter: rl}

	const blockedIP = "10.0.0.2"

	for i := 0; i < 5; i++ {
		fakeReq := httptest.NewRequest(http.MethodPost, "/forgot-password", nil)
		fakeReq.RemoteAddr = blockedIP + ":1234"
		rl.RecordFailure(fakeReq)
	}

	req := httptest.NewRequest(http.MethodPost, "/forgot-password", bytes.NewReader([]byte("email=x@x.com")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = blockedIP + ":5678"

	w := httptest.NewRecorder()
	h.ForgotPasswordPost(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when rate-limited, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// ResetPasswordPost — rate limiter blocks
// -----------------------------------------------------------------------

func TestResetPasswordPost_RateLimited2(t *testing.T) {
	d := newExtraTestDB(t)

	rl := middleware.NewLoginRateLimiter()
	defer rl.Close()
	h := &ResetPasswordHandler{DB: d, Config: &config.Config{}, Render: noRender, RateLimiter: rl}

	const blockedIP = "10.0.0.3"

	for i := 0; i < 5; i++ {
		fakeReq := httptest.NewRequest(http.MethodPost, "/reset-password", nil)
		fakeReq.RemoteAddr = blockedIP + ":1234"
		rl.RecordFailure(fakeReq)
	}

	req := httptest.NewRequest(http.MethodPost, "/reset-password", bytes.NewReader([]byte("token=abc&password=new&confirm=new")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = blockedIP + ":5678"

	w := httptest.NewRecorder()
	h.ResetPasswordPost(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when rate-limited, got %d", w.Code)
	}
}
