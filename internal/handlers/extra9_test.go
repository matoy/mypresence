package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
)

// -----------------------------------------------------------------------
// ProjectsReportPage — filter branches
// -----------------------------------------------------------------------

func TestProjectsReportPage_FilterInactive(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProject("Active P", "AP", 0, true, "2026-01-01", "2030-12-31")    //nolint:errcheck
	d.CreateProject("Inactive P", "IP", 0, false, "2026-01-01", "2030-12-31") //nolint:errcheck

	// active=0 → show inactive only
	req := createAdminReq(t, d, http.MethodGet, "/admin/projects-report?active=0", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjectsReportPage_FilterText(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProject("SearchableProj", "SRCH", 0, true, "2026-01-01", "2030-12-31") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects-report?q=searchable", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjectsReportPage_FilterTeam(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProject("TeamP", "TP", 0, true, "2026-01-01", "2030-12-31") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects-report?team=99&active=", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ProjectsReportAPI — filter branches
// -----------------------------------------------------------------------

func TestProjectsReportAPI_FilterActive0(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProject("InactiveAPI", "IAPI", 0, false, "2026-01-01", "2030-12-31") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/projects-report?active=0", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjectsReportAPI_FilterTeamAndText(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProject("TextProj", "TXTP", 0, true, "2026-01-01", "2030-12-31") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/projects-report?q=nothing&team=5&active=", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjectsReportAPI_FilterMiniNo_ExcludesMiniProjects(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProjectWithDetails("RegularProj", "REG", 0, true, "2026-01-01", "2030-12-31", false, "2030-12-31") //nolint:errcheck
	d.CreateProjectWithDetails("MiniProj", "MIN", 0, true, "2026-01-01", "2030-12-31", true, "2026-06-30")     //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/projects-report?mini=0&active=", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	rows := out["rows"].([]interface{})
	for _, r := range rows {
		row := r.(map[string]interface{})
		proj := row["Project"].(map[string]interface{})
		if proj["code"] == "MIN" {
			t.Error("mini-project should be excluded when mini=0")
		}
	}
	if out["filter_mini"] != "0" {
		t.Errorf("expected filter_mini=0, got %v", out["filter_mini"])
	}
}

func TestProjectsReportAPI_FilterMiniYes_OnlyMiniProjects(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProjectWithDetails("RegularProj2", "REG2", 0, true, "2026-01-01", "2030-12-31", false, "2030-12-31") //nolint:errcheck
	d.CreateProjectWithDetails("MiniProj2", "MIN2", 0, true, "2026-01-01", "2030-12-31", true, "2026-06-30")     //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/projects-report?mini=1&active=", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	rows := out["rows"].([]interface{})
	for _, r := range rows {
		row := r.(map[string]interface{})
		proj := row["Project"].(map[string]interface{})
		if proj["code"] == "REG2" {
			t.Error("non-mini project should be excluded when mini=1")
		}
	}
	if out["filter_mini"] != "1" {
		t.Errorf("expected filter_mini=1, got %v", out["filter_mini"])
	}
}

func TestProjectsReportAPI_FilterMiniAll_IncludesMiniProjectsByDefault(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProjectWithDetails("MiniProjVisible", "MINV", 0, true, "2026-01-01", "2030-12-31", true, "2026-06-30") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/projects-report?active=", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	rows := out["rows"].([]interface{})
	var found bool
	for _, r := range rows {
		row := r.(map[string]interface{})
		proj := row["Project"].(map[string]interface{})
		if proj["code"] == "MINV" {
			found = true
		}
	}
	if !found {
		t.Error("mini-project should be included by default (mini filter not set)")
	}
	if out["filter_mini"] != "" {
		t.Errorf("expected filter_mini='' by default, got %v", out["filter_mini"])
	}
}

func TestProjectsReportPage_FilterMini_FiltersMiniProjects(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProjectWithDetails("PageMiniProj", "PGMIN", 0, true, "2026-01-01", "2030-12-31", true, "2026-06-30") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects-report?mini=0&active=", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// HolidaysPage — current year
// -----------------------------------------------------------------------

func TestHolidaysPage_NoYear(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &HolidaysHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/admin/holidays", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.HolidaysPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ImpersonatePage — non-global user gets 403
// -----------------------------------------------------------------------

func TestImpersonatePage_NonGlobalUser(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &SettingsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("nonglobal@test.com", "NonGlobal", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/impersonate", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ImpersonatePage)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CancelReservationsByDates — invalid date
// -----------------------------------------------------------------------

func TestCancelReservationsByDates_InvalidDate(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	bodyBytes, _ := json.Marshal(map[string]interface{}{"dates": []string{"not-a-date"}})
	req := createAdminReq(t, d, http.MethodDelete, "/api/reservations/bulk", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CancelReservationsByDates)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCancelReservationsByDates_EmptyDates(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	bodyBytes, _ := json.Marshal(map[string]interface{}{"dates": []string{}})
	req := createAdminReq(t, d, http.MethodDelete, "/api/reservations/bulk", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CancelReservationsByDates)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ListSeatsForFloorplanAPI — missing id (0)
// -----------------------------------------------------------------------

func TestListSeatsForFloorplanAPI_MissingID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodGet, "/api/floorplans/0/seats", nil)
	req.SetPathValue("id", "0")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListSeatsForFloorplanAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// BulkReserveSeats — invalid date
// -----------------------------------------------------------------------

func TestBulkReserveSeats_InvalidDate(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP BulkInv", 0)
	seatID, _ := d.CreateSeat(fpID, "S1", 0.3, 0.3)

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"seat_id": seatID,
		"dates":   []string{"not-a-date"},
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/reservations/bulk", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.BulkReserveSeats)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// SeatsAPI — with floorplan ID
// -----------------------------------------------------------------------

func TestSeatsAPI_WithFloorplanID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP SeatsAPI", 0)
	d.CreateSeat(fpID, "SA1", 0.2, 0.4) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet,
		"/api/seats?floorplan_id="+strconvI64(fpID)+"&date=2026-06-01", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SeatsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CreatePAT — no description
// -----------------------------------------------------------------------

func TestCreatePAT_NoDescription(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("nodescrpat@test.com", "NoDescr", "password1")
	tok, _ := d.CreateSession(uid)

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"description": "",
		"expires_at":  "2030-12-31",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/pat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreatePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusForbidden {
		t.Fatalf("expected 400 or 403, got %d: %s", w.Code, w.Body.String())
	}
}
