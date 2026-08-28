package jira

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchRecentTickets_LegacyBasicAuth_Success(t *testing.T) {
	var gotAuth, gotJQL, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotJQL = r.URL.Query().Get("jql")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"issues": []map[string]interface{}{
				{"key": "PROJ-1", "fields": map[string]string{"summary": "Fix login bug"}},
				{"key": "PROJ-2", "fields": map[string]string{"summary": "Improve performance"}},
			},
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "bot@example.com", "", "tok-123")
	tickets, err := c.SearchRecentTickets("PROJ")
	if err != nil {
		t.Fatalf("SearchRecentTickets: %v", err)
	}
	if len(tickets) != 2 {
		t.Fatalf("expected 2 tickets, got %d", len(tickets))
	}
	if tickets[0].Key != "PROJ-1" || tickets[0].Title != "Fix login bug" {
		t.Errorf("unexpected ticket: %+v", tickets[0])
	}
	if gotPath != "/rest/api/3/search/jql" {
		t.Errorf("expected path /rest/api/3/search/jql, got %q", gotPath)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("bot@example.com:tok-123"))
	if gotAuth != wantAuth {
		t.Errorf("Authorization header: want %q, got %q", wantAuth, gotAuth)
	}
	if !strings.Contains(gotJQL, "project = \"PROJ\"") || !strings.Contains(gotJQL, "updated >= -30d") {
		t.Errorf("unexpected JQL: %q", gotJQL)
	}
}

func TestSearchRecentTickets_ScopedBearerAuth_Success(t *testing.T) {
	var gotAuth, gotJQL, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotJQL = r.URL.Query().Get("jql")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"issues": []map[string]interface{}{
				{"key": "PROJ-10", "fields": map[string]string{"summary": "Scoped ticket"}},
			},
		})
	}))
	defer server.Close()

	// In scoped mode, server.URL is passed to mock the cloud gateway URL
	c := NewClient(server.URL, "", "cloud-id-12345", "tok-scoped-abc")
	tickets, err := c.SearchRecentTickets("PROJ")
	if err != nil {
		t.Fatalf("SearchRecentTickets: %v", err)
	}
	if len(tickets) != 1 || tickets[0].Key != "PROJ-10" {
		t.Fatalf("unexpected tickets: %+v", tickets)
	}
	if gotPath != "/ex/jira/cloud-id-12345/rest/api/3/search/jql" {
		t.Errorf("expected path /ex/jira/cloud-id-12345/rest/api/3/search/jql, got %q", gotPath)
	}
	wantAuth := "Bearer tok-scoped-abc"
	if gotAuth != wantAuth {
		t.Errorf("Authorization header: want %q, got %q", wantAuth, gotAuth)
	}
	if !strings.Contains(gotJQL, "project = \"PROJ\"") {
		t.Errorf("unexpected JQL: %q", gotJQL)
	}
}

func TestSearchRecentTickets_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("invalid credentials")) //nolint:errcheck
	}))
	defer server.Close()

	c := NewClient(server.URL, "bot@example.com", "", "bad-token")
	if _, err := c.SearchRecentTickets("PROJ"); err == nil {
		t.Error("expected error on non-200 response")
	}
}

func TestSearchRecentTickets_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{not json")) //nolint:errcheck
	}))
	defer server.Close()

	c := NewClient(server.URL, "bot@example.com", "", "tok-123")
	if _, err := c.SearchRecentTickets("PROJ"); err == nil {
		t.Error("expected error on malformed JSON response")
	}
}

func TestSearchRecentTickets_EmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"issues": []interface{}{}}) //nolint:errcheck
	}))
	defer server.Close()

	c := NewClient(server.URL, "bot@example.com", "", "tok-123")
	tickets, err := c.SearchRecentTickets("PROJ")
	if err != nil {
		t.Fatalf("SearchRecentTickets: %v", err)
	}
	if len(tickets) != 0 {
		t.Errorf("expected 0 tickets, got %d", len(tickets))
	}
}

func TestNewClient_TrimsTrailingSlashAndSpaces(t *testing.T) {
	c := NewClient("https://acme.atlassian.net/", "e@e.com", "  cloud-id  ", "t")
	if c.BaseURL != "https://acme.atlassian.net" {
		t.Errorf("expected trailing slash to be trimmed, got %q", c.BaseURL)
	}
	if c.CloudID != "cloud-id" {
		t.Errorf("expected cloudID whitespace to be trimmed, got %q", c.CloudID)
	}
}
