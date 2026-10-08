package o365

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/testhelper"
)

func TestStateGenerationAndValidation(t *testing.T) {
	cfg := &Config{
		SecretKey: "secret-key-32-chars-long-minimum-!",
		ClientID:  "test-client-id",
	}
	c := NewClient(cfg, nil)

	state := c.GenerateState(42, "/?month=5")
	userID, returnTo, err := c.ValidateState(state)
	if err != nil {
		t.Fatalf("ValidateState failed: %v", err)
	}
	if userID != 42 {
		t.Errorf("Expected userID 42, got %d", userID)
	}
	if returnTo != "/?month=5" {
		t.Errorf("Expected returnTo /?month=5, got %s", returnTo)
	}

	// Tampered state
	tampered := state + "extra"
	_, _, err = c.ValidateState(tampered)
	if err == nil {
		t.Fatal("Expected error on tampered state")
	}

	// AuthURL
	authURL := c.AuthURL(42, "/test")
	if !strings.Contains(authURL, "client_id=test-client-id") {
		t.Errorf("AuthURL missing client_id: %s", authURL)
	}
}

func TestExchangeCodeAndGraphOperations(t *testing.T) {
	env := testhelper.NewEnv(t)
	uid, err := env.DB.CreateLocalUser("user@example.com", "Alice", "password")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}

	var createdEvents []map[string]interface{}
	var updatedEvents []string
	var deletedEvents []string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Token endpoint
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"token_type":    "Bearer",
				"access_token":  "mock-access-token",
				"refresh_token": "mock-refresh-token",
				"expires_in":    3600,
			})
			return
		}

		// /me profile endpoint
		if r.URL.Path == "/me" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"mail":                "alice.work@company.com",
				"userPrincipalName":   "alice.work@company.com",
			})
			return
		}

		// /me/calendar/events
		if r.URL.Path == "/me/calendar/events" && r.Method == http.MethodPost {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			createdEvents = append(createdEvents, body)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "graph-event-id-123",
			})
			return
		}

		// /me/calendar/events/{id} PATCH
		if strings.HasPrefix(r.URL.Path, "/me/calendar/events/") && r.Method == http.MethodPatch {
			id := strings.TrimPrefix(r.URL.Path, "/me/calendar/events/")
			updatedEvents = append(updatedEvents, id)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
			return
		}

		// /me/calendar/events/{id} DELETE
		if strings.HasPrefix(r.URL.Path, "/me/calendar/events/") && r.Method == http.MethodDelete {
			id := strings.TrimPrefix(r.URL.Path, "/me/calendar/events/")
			deletedEvents = append(deletedEvents, id)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	cfg := &Config{
		TenantID:     "common",
		ClientID:     "mock-client-id",
		ClientSecret: "mock-secret",
		RedirectURL:  "http://localhost:8080/auth/o365/callback",
		SecretKey:    "test-secret-32-chars-long-must-be-ok",
		AppName:      "MyPresence",
		GraphBaseURL: mockServer.URL,
		LoginBaseURL: mockServer.URL,
	}

	client := NewClient(cfg, env.DB)

	state := client.GenerateState(uid, "/calendar")
	gotUID, returnTo, tokenRec, err := client.ExchangeCode("mock-auth-code", state)
	if err != nil {
		t.Fatalf("ExchangeCode failed: %v", err)
	}
	if gotUID != uid || returnTo != "/calendar" {
		t.Errorf("Unexpected UID or returnTo: %d, %s", gotUID, returnTo)
	}
	if tokenRec.MicrosoftEmail != "alice.work@company.com" {
		t.Errorf("Expected microsoft email alice.work@company.com, got %s", tokenRec.MicrosoftEmail)
	}

	// GetValidAccessToken
	token, err := client.GetValidAccessToken(uid)
	if err != nil {
		t.Fatalf("GetValidAccessToken failed: %v", err)
	}
	if token != "mock-access-token" {
		t.Errorf("Expected mock-access-token, got %s", token)
	}

	// CreateEvent
	evt := Event{
		Subject:     "[MyPresence] Remote work",
		Date:        "2026-06-02",
		Half:        "full",
		ShowAs:      "workingElsewhere",
		Description: "Sync test",
	}
	eventID, err := client.CreateEvent(token, evt)
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}
	if eventID != "graph-event-id-123" {
		t.Errorf("Unexpected event ID: %s", eventID)
	}
	if len(createdEvents) != 1 {
		t.Fatalf("Expected 1 created event, got %d", len(createdEvents))
	}
	if createdEvents[0]["showAs"] != "workingElsewhere" {
		t.Errorf("Expected showAs workingElsewhere, got %v", createdEvents[0]["showAs"])
	}

	// UpdateEvent
	evt.ShowAs = "oof"
	if err := client.UpdateEvent(token, eventID, evt); err != nil {
		t.Fatalf("UpdateEvent failed: %v", err)
	}
	if len(updatedEvents) != 1 || updatedEvents[0] != eventID {
		t.Errorf("Expected updated event %s, got %v", eventID, updatedEvents)
	}

	// DeleteEvent
	if err := client.DeleteEvent(token, eventID); err != nil {
		t.Fatalf("DeleteEvent failed: %v", err)
	}
	if len(deletedEvents) != 1 || deletedEvents[0] != eventID {
		t.Errorf("Expected deleted event %s, got %v", eventID, deletedEvents)
	}
}
