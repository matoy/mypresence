package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"presence-app/internal/config"
	"presence-app/internal/db"
)

func newResetTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestResetBaseURL(t *testing.T) {
	h := &ResetPasswordHandler{Config: &config.Config{AppURL: "https://example.test/"}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "ignored.local"
	if got := h.baseURL(req); got != "https://example.test" {
		t.Fatalf("baseURL from config = %q", got)
	}

	h2 := &ResetPasswordHandler{Config: &config.Config{}}
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Host = "app.local"
	req2.Header.Set("X-Forwarded-Proto", "https")
	if got := h2.baseURL(req2); got != "https://app.local" {
		t.Fatalf("baseURL from request = %q", got)
	}
}

func TestForgotPasswordPostAlwaysRendersSent(t *testing.T) {
	database := newResetTestDB(t)
	cfg := &config.Config{AppName: "myPresence"}

	var data map[string]interface{}
	h := &ResetPasswordHandler{
		DB:     database,
		Config: cfg,
		Render: func(w http.ResponseWriter, r *http.Request, page string, d interface{}) {
			if page != "forgot_password" {
				t.Fatalf("unexpected page: %s", page)
			}
			data = d.(map[string]interface{})
		},
	}

	form := url.Values{}
	form.Set("email", "")
	req := httptest.NewRequest(http.MethodPost, "/forgot-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ForgotPasswordPost(w, req)
	if sent, _ := data["Sent"].(bool); !sent {
		t.Fatalf("expected Sent=true for empty email, got %#v", data)
	}

	form2 := url.Values{}
	form2.Set("email", "unknown@example.com")
	req2 := httptest.NewRequest(http.MethodPost, "/forgot-password", strings.NewReader(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w2 := httptest.NewRecorder()
	h.ForgotPasswordPost(w2, req2)
	if sent, _ := data["Sent"].(bool); !sent {
		t.Fatalf("expected Sent=true for unknown email, got %#v", data)
	}
}

func TestResetPasswordPostValidationAndSuccess(t *testing.T) {
	database := newResetTestDB(t)
	cfg := &config.Config{}
	uid, err := database.CreateLocalUser("local@example.com", "Local", "oldpassword")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	if uid <= 0 {
		t.Fatalf("invalid user id: %d", uid)
	}

	rawToken, err := database.CreatePasswordResetToken("local@example.com")
	if err != nil || rawToken == "" {
		t.Fatalf("CreatePasswordResetToken: err=%v token=%q", err, rawToken)
	}

	var data map[string]interface{}
	h := &ResetPasswordHandler{
		DB:     database,
		Config: cfg,
		Render: func(w http.ResponseWriter, r *http.Request, page string, d interface{}) {
			if page != "reset_password" {
				t.Fatalf("unexpected page: %s", page)
			}
			data = d.(map[string]interface{})
		},
	}

	bad := url.Values{}
	bad.Set("token", "")
	bad.Set("password", "12345678")
	bad.Set("confirm", "12345678")
	reqBad := httptest.NewRequest(http.MethodPost, "/reset-password", strings.NewReader(bad.Encode()))
	reqBad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ResetPasswordPost(httptest.NewRecorder(), reqBad)
	if got, _ := data["Error"].(string); got != "invalid_token" {
		t.Fatalf("expected invalid_token, got %#v", data)
	}

	good := url.Values{}
	good.Set("token", rawToken)
	good.Set("password", "newpassword")
	good.Set("confirm", "newpassword")
	reqGood := httptest.NewRequest(http.MethodPost, "/reset-password", strings.NewReader(good.Encode()))
	reqGood.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ResetPasswordPost(httptest.NewRecorder(), reqGood)
	if done, _ := data["Done"].(bool); !done {
		t.Fatalf("expected Done=true, got %#v", data)
	}

	u, err := database.GetUserByEmail("local@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if !database.CheckPassword(u.ID, u.PasswordHash, "newpassword") {
		t.Fatal("expected password to be updated")
	}
}
