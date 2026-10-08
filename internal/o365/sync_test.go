package o365

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/models"
	"github.com/matoy/mypresence/internal/testhelper"
)

func TestSyncMonth_ComprehensiveReconciliation(t *testing.T) {
	env := testhelper.NewEnv(t)
	uid, err := env.DB.CreateLocalUser("bob@example.com", "Bob", "pass")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}

	var createCalls int32
	var updateCalls int32
	var deleteCalls int32
	var eventCounter int64

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/me/calendar/events" && r.Method == http.MethodPost {
			atomic.AddInt32(&createCalls, 1)
			id := fmt.Sprintf("evt-%d", atomic.AddInt64(&eventCounter, 1))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
			return
		}

		if strings.HasPrefix(r.URL.Path, "/me/calendar/events/") && r.Method == http.MethodPatch {
			atomic.AddInt32(&updateCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "updated"})
			return
		}

		if strings.HasPrefix(r.URL.Path, "/me/calendar/events/") && r.Method == http.MethodDelete {
			atomic.AddInt32(&deleteCalls, 1)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	cfg := &Config{
		TenantID:     "common",
		ClientID:     "mock-client-id",
		SecretKey:    "test-secret-32-chars-long-must-be-ok",
		AppName:      "MyPresence",
		GraphBaseURL: mockServer.URL,
		LoginBaseURL: mockServer.URL,
	}
	client := NewClient(cfg, env.DB)

	// Save pre-existing token for Bob
	encAccess, _ := EncryptToken("valid-token", cfg.SecretKey)
	encRefresh, _ := EncryptToken("refresh-token", cfg.SecretKey)
	_ = env.DB.SaveUserO365Token(uid, "bob@company.com", encAccess, encRefresh, time.Now().Add(1*time.Hour), false)

	// Fetch default statuses: Remote work (billable, !on_site), Leave (!billable, !on_site), On site (billable, on_site)
	statuses, err := env.DB.ListStatuses()
	if err != nil {
		t.Fatalf("ListStatuses: %v", err)
	}
	var remoteStatus, leaveStatus, onSiteStatus models.Status
	for _, s := range statuses {
		if strings.Contains(strings.ToLower(s.Name), "remote") {
			remoteStatus = s
		} else if strings.Contains(strings.ToLower(s.Name), "leave") {
			leaveStatus = s
		} else if strings.Contains(strings.ToLower(s.Name), "site") {
			onSiteStatus = s
		}
	}
	if remoteStatus.ID == 0 || leaveStatus.ID == 0 || onSiteStatus.ID == 0 {
		t.Fatalf("Missing expected default statuses: remote=%d, leave=%d, onSite=%d", remoteStatus.ID, leaveStatus.ID, onSiteStatus.ID)
	}

	// 1. Set presences for June 2026:
	// - 2026-06-01: Remote work (full day) -> should create event with workingElsewhere
	// - 2026-06-02: Leave (full day) -> should create event with oof
	// - 2026-06-03: On site (full day) -> should NOT create event
	_ = env.DB.SetPresences(uid, []string{"2026-06-01"}, remoteStatus.ID, "full")
	_ = env.DB.SetPresences(uid, []string{"2026-06-02"}, leaveStatus.ID, "full")
	_ = env.DB.SetPresences(uid, []string{"2026-06-03"}, onSiteStatus.ID, "full")

	report, err := client.SyncMonth(uid, 2026, 6)
	if err != nil {
		t.Fatalf("First SyncMonth failed: %v", err)
	}
	if report.Created != 2 || report.Updated != 0 || report.Deleted != 0 {
		t.Fatalf("Expected 2 created, got %+v", report)
	}
	if atomic.LoadInt32(&createCalls) != 2 {
		t.Errorf("Expected 2 Graph POST calls, got %d", atomic.LoadInt32(&createCalls))
	}

	// 2. Second sync without modifications -> should be completely idempotent (0 created, 0 updated, 0 deleted, 2 unchanged)
	report2, err := client.SyncMonth(uid, 2026, 6)
	if err != nil {
		t.Fatalf("Second SyncMonth failed: %v", err)
	}
	if report2.Created != 0 || report2.Updated != 0 || report2.Deleted != 0 || report2.Unchanged != 2 {
		t.Fatalf("Expected 2 unchanged, got %+v", report2)
	}

	// 3. Modify 2026-06-01 from Remote work to Leave -> should update existing event
	_ = env.DB.SetPresences(uid, []string{"2026-06-01"}, leaveStatus.ID, "full")
	report3, err := client.SyncMonth(uid, 2026, 6)
	if err != nil {
		t.Fatalf("Third SyncMonth failed: %v", err)
	}
	if report3.Updated != 1 || report3.Unchanged != 1 {
		t.Fatalf("Expected 1 updated, 1 unchanged, got %+v", report3)
	}
	if atomic.LoadInt32(&updateCalls) != 1 {
		t.Errorf("Expected 1 Graph PATCH call, got %d", atomic.LoadInt32(&updateCalls))
	}

	// 4. Change 2026-06-02 to On site -> should delete event from Outlook
	_ = env.DB.SetPresences(uid, []string{"2026-06-02"}, onSiteStatus.ID, "full")
	report4, err := client.SyncMonth(uid, 2026, 6)
	if err != nil {
		t.Fatalf("Fourth SyncMonth failed: %v", err)
	}
	if report4.Deleted != 1 {
		t.Fatalf("Expected 1 deleted, got %+v", report4)
	}
	if atomic.LoadInt32(&deleteCalls) != 1 {
		t.Errorf("Expected 1 Graph DELETE call, got %d", atomic.LoadInt32(&deleteCalls))
	}

	// 5. Clear presence on 2026-06-01 -> should delete remaining event from Outlook
	_ = env.DB.ClearPresences(uid, []string{"2026-06-01"}, "")
	report5, err := client.SyncMonth(uid, 2026, 6)
	if err != nil {
		t.Fatalf("Fifth SyncMonth failed: %v", err)
	}
	if report5.Deleted != 1 {
		t.Fatalf("Expected 1 deleted, got %+v", report5)
	}
	if atomic.LoadInt32(&deleteCalls) != 2 {
		t.Errorf("Expected 2 total Graph DELETE calls, got %d", atomic.LoadInt32(&deleteCalls))
	}
}
