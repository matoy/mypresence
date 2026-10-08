package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/o365"
	"github.com/matoy/mypresence/internal/testhelper"
)

func setupTestO365(t *testing.T) (*testhelper.Env, *O365Handler, int64, *httptest.Server) {
	env := testhelper.NewEnv(t)
	uid, err := env.DB.CreateLocalUser("carol@example.com", "Carol", "password")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"token_type":    "Bearer",
				"access_token":  "mock-access-token",
				"refresh_token": "mock-refresh-token",
				"expires_in":    3600,
			})
			return
		}
		if r.URL.Path == "/me" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"mail": "carol@office.com",
			})
			return
		}
		if r.URL.Path == "/me/calendar/events" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "new-evt-id"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/me/calendar/events/") && r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))

	cfg := &config.Config{
		O365SyncEnabled: true,
		O365TenantID:    "common",
		O365ClientID:    "test-client-id",
		O365RedirectURL: "http://localhost:8080/auth/o365/callback",
		SecretKey:       "32-chars-long-secret-key-for-test!!",
		AppName:         "MyPresence",
	}

	o365Cfg := &o365.Config{
		TenantID:     cfg.O365TenantID,
		ClientID:     cfg.O365ClientID,
		RedirectURL:  cfg.O365RedirectURL,
		SecretKey:    cfg.SecretKey,
		AppName:      cfg.AppName,
		GraphBaseURL: mockServer.URL,
		LoginBaseURL: mockServer.URL,
	}

	client := o365.NewClient(o365Cfg, env.DB)
	debouncer := o365.NewDebouncer(client, 100*time.Millisecond)
	handler := NewO365Handler(env.DB, cfg, client, debouncer)

	return env, handler, uid, mockServer
}

func TestO365Handler_ConnectAndCallback(t *testing.T) {
	env, handler, uid, mockServer := setupTestO365(t)
	defer mockServer.Close()

	user, err := env.DB.GetUserByID(uid)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}

	// 1. Connect endpoint redirects to Microsoft OAuth
	req := httptest.NewRequest(http.MethodGet, "/auth/o365/connect?return_to=/?month=6", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	w := httptest.NewRecorder()

	handler.Connect(w, req)
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("Expected 307 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "client_id=test-client-id") || !strings.Contains(loc, "state=") {
		t.Fatalf("Unexpected redirect location: %s", loc)
	}

	// Extract state from location
	state := loc[strings.Index(loc, "state=")+6:]
	if ampersand := strings.Index(state, "&"); ampersand != -1 {
		state = state[:ampersand]
	}

	// 2. Callback endpoint exchanges code
	cbReq := httptest.NewRequest(http.MethodGet, "/auth/o365/callback?code=mock-code&state="+state, nil)
	cbW := httptest.NewRecorder()

	handler.Callback(cbW, cbReq)
	if cbW.Code != http.StatusSeeOther {
		t.Fatalf("Expected 303 redirect, got %d", cbW.Code)
	}
	cbLoc := cbW.Header().Get("Location")
	if !strings.Contains(cbLoc, "o365_connected=1") {
		t.Errorf("Expected o365_connected=1 in location, got %s", cbLoc)
	}

	// 3. Status endpoint shows connected
	statusReq := httptest.NewRequest(http.MethodGet, "/api/o365/status", nil)
	statusReq = statusReq.WithContext(middleware.WithUser(statusReq.Context(), user))
	statusW := httptest.NewRecorder()

	handler.Status(statusW, statusReq)
	if statusW.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", statusW.Code)
	}

	var statusResp map[string]interface{}
	_ = json.NewDecoder(statusW.Body).Decode(&statusResp)
	if statusResp["connected"] != true || statusResp["microsoft_email"] != "carol@office.com" {
		t.Errorf("Unexpected status response: %+v", statusResp)
	}

	// 4. Toggle auto-sync
	toggleBody, _ := json.Marshal(map[string]bool{"auto_sync": true})
	toggleReq := httptest.NewRequest(http.MethodPost, "/api/o365/toggle-auto-sync", bytes.NewReader(toggleBody))
	toggleReq = toggleReq.WithContext(middleware.WithUser(toggleReq.Context(), user))
	toggleW := httptest.NewRecorder()

	handler.ToggleAutoSync(toggleW, toggleReq)
	if toggleW.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on toggle, got %d", toggleW.Code)
	}

	// Verify in DB
	tok, _ := env.DB.GetUserO365Token(uid)
	if tok == nil || !tok.AutoSync {
		t.Fatalf("Expected auto_sync=true in DB, got %+v", tok)
	}

	// 5. Sync endpoint
	syncBody, _ := json.Marshal(map[string]int{"year": 2026, "month": 6})
	syncReq := httptest.NewRequest(http.MethodPost, "/api/o365/sync", bytes.NewReader(syncBody))
	syncReq = syncReq.WithContext(middleware.WithUser(syncReq.Context(), user))
	syncW := httptest.NewRecorder()

	handler.Sync(syncW, syncReq)
	if syncW.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on sync, got %d", syncW.Code)
	}

	// 6. Disconnect endpoint
	disReq := httptest.NewRequest(http.MethodPost, "/api/o365/disconnect", nil)
	disReq = disReq.WithContext(middleware.WithUser(disReq.Context(), user))
	disW := httptest.NewRecorder()

	handler.Disconnect(disW, disReq)
	if disW.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on disconnect, got %d", disW.Code)
	}

	tokAfter, _ := env.DB.GetUserO365Token(uid)
	if tokAfter != nil {
		t.Fatalf("Expected token to be deleted, got %+v", tokAfter)
	}
}
