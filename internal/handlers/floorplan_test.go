package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"presence-app/internal/middleware"
)

// -----------------------------------------------------------------------
// FloorplanHandler — Admin CRUD
// -----------------------------------------------------------------------
func TestFloorplanAdminPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	var rendered string
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}
	req := createAdminReq(t, d, http.MethodGet, "/admin/floorplans", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.AdminFloorplansPage)).ServeHTTP(w, req)
	if rendered != "admin_floorplans" {
		t.Errorf("expected admin_floorplans, got %q", rendered)
	}
}

func TestFloorplanCreate_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	body, _ := json.Marshal(map[string]string{"name": "Floor 1"})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/floorplans", body)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateFloorplan)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFloorplanCreate_ValidationError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	body, _ := json.Marshal(map[string]string{"name": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/floorplans", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateFloorplan(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestFloorplanCreate_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/floorplans", bytes.NewReader([]byte("{")))
	w := httptest.NewRecorder()
	h.CreateFloorplan(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestFloorplanGetAdmin_NotFound(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/floorplans/999", nil)
	req.SetPathValue("id", "999")
	w := httptest.NewRecorder()
	h.AdminGetFloorplan(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestFloorplanGetAdmin_Found(t *testing.T) {
	d := newExtraTestDB(t)
	fpID, _ := d.CreateFloorplan("Test FP", 0)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/floorplans/"+strconvI64(fpID), nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	h.AdminGetFloorplan(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestFloorplanUpdate_Success(t *testing.T) {
	d := newExtraTestDB(t)
	fpID, _ := d.CreateFloorplan("Old FP", 0)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	body, _ := json.Marshal(map[string]interface{}{"name": "New FP", "sort_order": 1})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/floorplans/"+strconvI64(fpID), body)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateFloorplan)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFloorplanUpdate_EmptyName(t *testing.T) {
	d := newExtraTestDB(t)
	fpID, _ := d.CreateFloorplan("FP", 0)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	body, _ := json.Marshal(map[string]interface{}{"name": "", "sort_order": 0})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/floorplans/"+strconvI64(fpID), bytes.NewReader(body))
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	h.UpdateFloorplan(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestFloorplanDelete_Success(t *testing.T) {
	d := newExtraTestDB(t)
	fpID, _ := d.CreateFloorplan("Del FP", 0)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/floorplans/"+strconvI64(fpID), nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DeleteFloorplan)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestFloorplanUploadImage_NoFile(t *testing.T) {
	d := newExtraTestDB(t)
	fpID, _ := d.CreateFloorplan("IMG FP", 0)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/floorplans/"+strconvI64(fpID)+"/image", nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	h.UploadFloorplanImage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no file, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// Seat CRUD
// -----------------------------------------------------------------------

func TestSeatCreate_Success(t *testing.T) {
	d := newExtraTestDB(t)
	fpID, _ := d.CreateFloorplan("Seat FP", 0)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	body, _ := json.Marshal(map[string]interface{}{"label": "A1", "x_pct": 10.0, "y_pct": 20.0})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/floorplans/"+strconvI64(fpID)+"/seats", body)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateSeat)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSeatCreate_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/floorplans/1/seats", bytes.NewReader([]byte("{")))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	h.CreateSeat(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSeatUpdate_Success(t *testing.T) {
	d := newExtraTestDB(t)
	fpID, _ := d.CreateFloorplan("SeatUp FP", 0)
	seatID, _ := d.CreateSeat(fpID, "B1", 10.0, 20.0)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	body, _ := json.Marshal(map[string]interface{}{"label": "B2", "x_pct": 30.0, "y_pct": 40.0})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/seats/"+strconvI64(seatID), body)
	req.SetPathValue("id", strconvI64(seatID))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateSeat)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSeatDelete_Success(t *testing.T) {
	d := newExtraTestDB(t)
	fpID, _ := d.CreateFloorplan("SeatDel FP", 0)
	seatID, _ := d.CreateSeat(fpID, "C1", 10.0, 20.0)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/seats/"+strconvI64(seatID), nil)
	req.SetPathValue("id", strconvI64(seatID))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DeleteSeat)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestListSeatsForFloorplanAPI_Empty(t *testing.T) {
	d := newExtraTestDB(t)
	fpID, _ := d.CreateFloorplan("List FP", 0)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	req := httptest.NewRequest(http.MethodGet, "/api/floorplans/"+strconvI64(fpID)+"/seats", nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	h.ListSeatsForFloorplanAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// CancelReservation handler
// -----------------------------------------------------------------------

func TestCancelReservationHandler_NotFound(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	uid, _ := d.CreateLocalUser("fpcancel@test.com", "FPCancel", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodDelete, "/api/reservations/99999", nil)
	req.SetPathValue("id", "99999")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CancelReservation)).ServeHTTP(w, req)
	// DB returns nil (silent success) on not-found reservation — handler returns 200
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on not-found reservation, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// BulkReserveSeats handler
// -----------------------------------------------------------------------

func TestBulkReserveSeats_MissingFP(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	uid, _ := d.CreateLocalUser("bulk@test.com", "Bulk", "password1")
	tok, _ := d.CreateSession(uid)

	body, _ := json.Marshal(map[string]interface{}{"floorplan_id": 0, "seat_id": 1, "dates": []string{"2026-05-10"}})
	req := httptest.NewRequest(http.MethodPost, "/api/reservations/bulk", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.BulkReserveSeats)).ServeHTTP(w, req)
	// Handler doesn't validate floorplan_id=0 — returns 200 with {"booked":0}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CalendarPage handler
// -----------------------------------------------------------------------

func TestCalendarPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	var rendered string
	h := &CalendarHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}, DisableFloorplans: true}

	uid, _ := d.CreateLocalUser("cal@test.com", "Cal", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)
	if rendered != "calendar" {
		t.Errorf("expected calendar page, got %q", rendered)
	}
}

func TestCalendarPage_WithYearMonth(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	var renderedData interface{}
	h := &CalendarHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		renderedData = data
	}, DisableFloorplans: true}

	uid, _ := d.CreateLocalUser("cal2@test.com", "Cal2", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/?year=2026&month=3", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)

	if m, ok := renderedData.(map[string]interface{}); ok {
		if m["Year"] != 2026 || m["Month"] != 3 {
			t.Errorf("expected year=2026 month=3, got year=%v month=%v", m["Year"], m["Month"])
		}
	}
}

// -----------------------------------------------------------------------
// ProjectsPage handler
// -----------------------------------------------------------------------

func TestProjectsPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	var rendered string
	h := &ProjectsHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}

	uid, _ := d.CreateLocalUser("proj@test.com", "Proj", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ProjectsPage)).ServeHTTP(w, req)
	if rendered != "projects" {
		t.Errorf("expected projects, got %q", rendered)
	}
}

// -----------------------------------------------------------------------
// AdminProjectsPage handler
// -----------------------------------------------------------------------

func TestAdminProjectsPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	var rendered string
	h := &ProjectsHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}
	req := createAdminReq(t, d, http.MethodGet, "/admin/projects", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsPage)).ServeHTTP(w, req)
	if rendered != "admin_projects" {
		t.Errorf("expected admin_projects, got %q", rendered)
	}
}

// -----------------------------------------------------------------------
// ActivityPage / ActivityAPI
// -----------------------------------------------------------------------

func TestActivityPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	var rendered string
	h := &ActivityHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}
	req := createAdminReq(t, d, http.MethodGet, "/admin/activity", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if rendered != "admin_activity" {
		t.Errorf("expected admin_activity, got %q", rendered)
	}
}

func TestActivityAPI_EmptyTeam(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ActivityHandler{DB: d, Render: noRender}
	tid, _ := d.CreateTeam("Activity Team")

	req := createAdminReq(t, d, http.MethodGet,
		"/api/admin/activity?team_id="+strconvI64(tid)+"&year=2026&month=1", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ImpersonatePage handler
// -----------------------------------------------------------------------

func TestImpersonatePage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	var rendered string
	h := &SettingsHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}
	req := createAdminReq(t, d, http.MethodGet, "/admin/impersonate", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ImpersonatePage)).ServeHTTP(w, req)
	if rendered != "impersonate" {
		t.Errorf("expected impersonate, got %q", rendered)
	}
}

// -----------------------------------------------------------------------
// ProjectsReportPage + ProjectsReportAPI
// -----------------------------------------------------------------------

func TestProjectsReportPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	var rendered string
	h := &ProjectsHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}
	req := createAdminReq(t, d, http.MethodGet, "/admin/projects/report", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if rendered != "admin_projects_report" {
		t.Errorf("expected admin_projects_report, got %q", rendered)
	}
}

func TestProjectsReportAPI_Empty(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}
	req := createAdminReq(t, d, http.MethodGet, "/api/admin/projects/report?year=2026&month=5", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// FloorplanPage (user-facing)
// -----------------------------------------------------------------------

func TestFloorplanPage_NoFloorplans(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	var rendered string
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}

	uid, _ := d.CreateLocalUser("fpuser@test.com", "FPUser", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/floorplan", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.FloorplanPage)).ServeHTTP(w, req)
	if rendered != "floorplan" {
		t.Errorf("expected floorplan, got %q", rendered)
	}
}
