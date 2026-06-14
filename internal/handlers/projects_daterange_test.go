package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
)

// ─── buildMonthKeysFromRange — unit tests ─────────────────────────────────────

func TestBuildMonthKeysFromRange_ValidRange(t *testing.T) {
	keys := buildMonthKeysFromRange("2026-01", "2026-03")
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d: %v", len(keys), keys)
	}
	want := []string{"2026-01", "2026-02", "2026-03"}
	for i, k := range want {
		if keys[i] != k {
			t.Fatalf("keys[%d]: got %q, want %q", i, keys[i], k)
		}
	}
}

func TestBuildMonthKeysFromRange_SingleMonth(t *testing.T) {
	keys := buildMonthKeysFromRange("2026-06", "2026-06")
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d: %v", len(keys), keys)
	}
	if keys[0] != "2026-06" {
		t.Fatalf("expected 2026-06, got %q", keys[0])
	}
}

func TestBuildMonthKeysFromRange_WithFullDates(t *testing.T) {
	// Accepts "YYYY-MM-DD" — only the first 7 chars are used.
	keys := buildMonthKeysFromRange("2026-01-15", "2026-03-20")
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d: %v", len(keys), keys)
	}
	if keys[0] != "2026-01" || keys[2] != "2026-03" {
		t.Fatalf("unexpected keys: %v", keys)
	}
}

func TestBuildMonthKeysFromRange_ReversedDates(t *testing.T) {
	keys := buildMonthKeysFromRange("2026-06", "2026-01")
	if keys != nil {
		t.Fatalf("expected nil for reversed range, got %v", keys)
	}
}

func TestBuildMonthKeysFromRange_EmptyFrom(t *testing.T) {
	keys := buildMonthKeysFromRange("", "2026-06")
	if keys != nil {
		t.Fatalf("expected nil when dateFrom is empty, got %v", keys)
	}
}

func TestBuildMonthKeysFromRange_EmptyTo(t *testing.T) {
	keys := buildMonthKeysFromRange("2026-01", "")
	if keys != nil {
		t.Fatalf("expected nil when dateTo is empty, got %v", keys)
	}
}

func TestBuildMonthKeysFromRange_InvalidFormat(t *testing.T) {
	keys := buildMonthKeysFromRange("not-a-date", "2026-06")
	if keys != nil {
		t.Fatalf("expected nil for invalid dateFrom, got %v", keys)
	}
}

func TestBuildMonthKeysFromRange_BothInvalid(t *testing.T) {
	keys := buildMonthKeysFromRange("bad", "worse")
	if keys != nil {
		t.Fatalf("expected nil for both invalid, got %v", keys)
	}
}

func TestBuildMonthKeysFromRange_CappedAt24(t *testing.T) {
	// A range of 30 months should be capped at 24.
	keys := buildMonthKeysFromRange("2024-01", "2026-06")
	if len(keys) != 24 {
		t.Fatalf("expected 24 keys (cap), got %d", len(keys))
	}
	if keys[0] != "2024-01" {
		t.Fatalf("expected first key 2024-01, got %q", keys[0])
	}
	if keys[23] != "2025-12" {
		t.Fatalf("expected last key 2025-12, got %q", keys[23])
	}
}

func TestBuildMonthKeysFromRange_CrossYear(t *testing.T) {
	keys := buildMonthKeysFromRange("2025-11", "2026-02")
	if len(keys) != 4 {
		t.Fatalf("expected 4 keys across year boundary, got %d: %v", len(keys), keys)
	}
	want := []string{"2025-11", "2025-12", "2026-01", "2026-02"}
	for i, k := range want {
		if keys[i] != k {
			t.Fatalf("keys[%d]: got %q, want %q", i, keys[i], k)
		}
	}
}

// ─── ProjectsReportPage — date range filter ───────────────────────────────────

func TestProjectsReportPage_ValidDateRange(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet,
		"/admin/projects-report?date_from=2026-01&date_to=2026-03", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjectsReportPage_SingleMonthRange(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet,
		"/admin/projects-report?date_from=2026-06&date_to=2026-06", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjectsReportPage_ReversedDateRange_FallsBackToDefault(t *testing.T) {
	// Reversed range → buildMonthKeysFromRange returns nil → default 3 months used.
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet,
		"/admin/projects-report?date_from=2026-06&date_to=2026-01", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjectsReportPage_OnlyDateFrom_FallsBackToDefault(t *testing.T) {
	// Only date_from without date_to → fallback to default 3 months.
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet,
		"/admin/projects-report?date_from=2026-01", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjectsReportPage_DateRangeWithProject(t *testing.T) {
	// Date range spanning project months correctly includes those months.
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("drpage@test.com", "DRPage", "password1")
	projID, _ := d.CreateProject("DRPageProj", "DRP", 0, true, "2026-01-01", "2026-12-31")
	d.SetProjectTimeEntry(uid, projID, 2026, 2, 3.0) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet,
		"/admin/projects-report?date_from=2026-01&date_to=2026-04", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── ProjectsReportAPI — date range filter ────────────────────────────────────

func TestProjectsReportAPI_ValidDateRange(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet,
		"/api/projects-report?date_from=2026-01&date_to=2026-04", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if resp["filter_date_from"] != "2026-01" {
		t.Fatalf("filter_date_from: got %v, want 2026-01", resp["filter_date_from"])
	}
	if resp["filter_date_to"] != "2026-04" {
		t.Fatalf("filter_date_to: got %v, want 2026-04", resp["filter_date_to"])
	}
	// month_keys should contain 4 entries
	monthKeys, ok := resp["month_keys"].([]interface{})
	if !ok {
		t.Fatalf("month_keys missing or wrong type: %v", resp["month_keys"])
	}
	if len(monthKeys) != 4 {
		t.Fatalf("expected 4 month_keys, got %d: %v", len(monthKeys), monthKeys)
	}
}

func TestProjectsReportAPI_SingleMonthRange(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet,
		"/api/projects-report?date_from=2026-05&date_to=2026-05", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	monthKeys, _ := resp["month_keys"].([]interface{})
	if len(monthKeys) != 1 {
		t.Fatalf("expected 1 month_key, got %d: %v", len(monthKeys), monthKeys)
	}
	if monthKeys[0] != "2026-05" {
		t.Fatalf("expected 2026-05, got %v", monthKeys[0])
	}
}

func TestProjectsReportAPI_ReversedDateRange_FallsBackToDefault(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet,
		"/api/projects-report?date_from=2026-06&date_to=2026-01", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	// Fallback → 3 default months
	monthKeys, _ := resp["month_keys"].([]interface{})
	if len(monthKeys) != 3 {
		t.Fatalf("expected 3 default month_keys on fallback, got %d: %v", len(monthKeys), monthKeys)
	}
	// date filters should be empty on fallback
	if resp["filter_date_from"] != "" {
		t.Fatalf("expected filter_date_from empty on fallback, got %v", resp["filter_date_from"])
	}
	if resp["filter_date_to"] != "" {
		t.Fatalf("expected filter_date_to empty on fallback, got %v", resp["filter_date_to"])
	}
}

func TestProjectsReportAPI_OnlyDateFrom_FallsBackToDefault(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet,
		"/api/projects-report?date_from=2026-03", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	monthKeys, _ := resp["month_keys"].([]interface{})
	if len(monthKeys) != 3 {
		t.Fatalf("expected 3 default month_keys, got %d", len(monthKeys))
	}
}

func TestProjectsReportAPI_DateRangeWithProject(t *testing.T) {
	// Verifies totals for a project with entries within the date range.
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("drapi@test.com", "DRAPI", "password1")
	projID, _ := d.CreateProject("DRAPIProj", "DRAP", 0, true, "2026-01-01", "2026-12-31")
	d.SetProjectTimeEntry(uid, projID, 2026, 2, 5.0) //nolint:errcheck
	d.SetProjectTimeEntry(uid, projID, 2026, 3, 2.0) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet,
		"/api/projects-report?date_from=2026-02&date_to=2026-03", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	monthKeys, _ := resp["month_keys"].([]interface{})
	if len(monthKeys) != 2 {
		t.Fatalf("expected 2 month_keys, got %d: %v", len(monthKeys), monthKeys)
	}
}

func TestProjectsReportAPI_InvalidDateFormat_FallsBackToDefault(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet,
		"/api/projects-report?date_from=not-a-date&date_to=also-bad", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	monthKeys, _ := resp["month_keys"].([]interface{})
	if len(monthKeys) != 3 {
		t.Fatalf("expected 3 default month_keys, got %d", len(monthKeys))
	}
}
