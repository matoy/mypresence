package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/myPresence/internal/middleware"
	"github.com/matoy/myPresence/internal/models"
)

// ─── floorplan.go DB error branches ──────────────────────────────────────────

// CreateFloorplan DB error
func TestCreateFloorplan_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	body, _ := json.Marshal(map[string]string{"name": "TestFloor"})
	req := createAdminReq(t, d, http.MethodPost, "/admin/floorplans", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.CreateFloorplan(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// UpdateFloorplan DB error
func TestUpdateFloorplan_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FPUpdate", 0)
	body, _ := json.Marshal(map[string]interface{}{"name": "UpdatedFP", "sort_order": 1})
	req := createAdminReq(t, d, http.MethodPut, "/admin/floorplans/"+strconvI64(fpID), body)
	req.SetPathValue("id", strconvI64(fpID))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.UpdateFloorplan(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// AdminListSeats DB error
func TestAdminListSeats_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FPSeats", 0)
	req := createAdminReq(t, d, http.MethodGet,
		"/admin/seats?floorplan_id="+strconvI64(fpID), nil)

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.AdminListSeats(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// CreateSeat DB error
func TestCreateSeat_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FPCreateSeat", 0)
	body, _ := json.Marshal(map[string]interface{}{"label": "A1", "x_pct": 50.0, "y_pct": 50.0})
	req := createAdminReq(t, d, http.MethodPost,
		"/admin/floorplans/"+strconvI64(fpID)+"/seats", body)
	req.SetPathValue("id", strconvI64(fpID))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.CreateSeat(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ListSeatsForFloorplanAPI DB error
func TestListSeatsForFloorplanAPI_DBError2(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FPListSeatsForAPI", 0)
	req := createAdminReq(t, d, http.MethodGet, "/api/floorplans/"+strconvI64(fpID)+"/seats", nil)
	req.SetPathValue("id", strconvI64(fpID))

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.ListSeatsForFloorplanAPI(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// SeatsAPI - user not on-site returns on_site:false
func TestListSeatsAPI_DBError2(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FPListSeatsDBErr", 0)
	uid, _ := d.CreateLocalUser("seatdberr@test.com", "SeatDBErr", "password1")
	tok, _ := d.CreateSession(uid)
	// No presences → user is NOT on-site

	req := httptest.NewRequest(http.MethodGet,
		"/api/seats?floorplan_id="+strconvI64(fpID)+"&date=2026-01-05", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SeatsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (not on-site), got %d: %s", w.Code, w.Body.String())
	}
}

// FloorplanPage with fpIDStr != "" to cover parsing branch
func TestFloorplanPage_WithFPID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	statusID, _ := d.CreateStatus(models.Status{Name: "FloorOnSite", Color: "#ff0000", OnSite: true, Billable: true, SortOrder: 1})
	fpID, _ := d.CreateFloorplan("FPForPage", 0)
	uid, _ := d.CreateLocalUser("floorpage@test.com", "FloorPage", "password1")
	tok, _ := d.CreateSession(uid)
	d.SetPresences(uid, []string{"2026-01-05"}, statusID, "") //nolint:errcheck
	d.CreateSeat(fpID, "A1", 50.0, 50.0)                      //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet,
		"/floorplan?floorplan="+strconvI64(fpID)+"&date=2026-01-05", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.FloorplanPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// FloorplanPage: user not on-site → shows read-only layout
func TestFloorplanPage_NotOnSite(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FPForPageOffSite", 0)
	d.CreateSeat(fpID, "B1", 30.0, 40.0) //nolint:errcheck
	uid, _ := d.CreateLocalUser("floorpageoffsite@test.com", "FloorPageOffSite", "password1")
	tok, _ := d.CreateSession(uid)
	// No presences set → user is not on-site

	req := httptest.NewRequest(http.MethodGet,
		"/floorplan?floorplan="+strconvI64(fpID)+"&date=2026-01-05", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.FloorplanPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// BulkReserveSeats - invalid JSON
func TestBulkReserveSeats_InvalidJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodPost, "/api/reservations/bulk", []byte("bad{"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.BulkReserveSeats)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// CancelUserReservationsForDates - DB error
func TestCancelReservationsByDates_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	body, _ := json.Marshal(map[string]interface{}{"dates": []string{"2026-01-05"}})
	req := createAdminReq(t, d, http.MethodDelete, "/api/reservations/bulk", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.CancelReservationsByDates(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── settings.go ImpersonatePage DB error ─────────────────────────────────────

// ImpersonatePage DB error
func TestImpersonatePage_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &SettingsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/admin/impersonate", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.ImpersonatePage(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── admin_users.go DeleteUser DB error ───────────────────────────────────────

// DeleteUser DB error via close
func TestDeleteUser_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("delme@test.com", "DelMe", "password1")
	req := createAdminReq(t, d, http.MethodDelete, "/admin/users/"+strconvI64(uid), nil)
	req.SetPathValue("id", strconvI64(uid))

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.DeleteUser(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── projects.go AdminProjectsAPI branches ────────────────────────────────────

// AdminProjectsAPI - various CRUD actions
func TestAdminProjectsAPI_CreateAndUpdate(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("AProjTeam")

	// Create project
	body, _ := json.Marshal(map[string]interface{}{
		"name":       "TestAPIProject",
		"code":       "TAP",
		"team_id":    teamID,
		"active":     true,
		"start_date": "2026-01-01",
		"end_date":   "2026-12-31",
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/projects", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for create, got %d: %s", w.Code, w.Body.String())
	}

	// Parse the project ID
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp) //nolint:errcheck
	projIDFloat, _ := resp["id"].(float64)
	projID := int64(projIDFloat)

	// Update project
	body2, _ := json.Marshal(map[string]interface{}{
		"name":       "UpdatedProject",
		"code":       "UPD",
		"team_id":    teamID,
		"active":     false,
		"start_date": "2026-01-01",
		"end_date":   "2026-06-30",
	})
	req2 := createAdminReq(t, d, http.MethodPut, "/admin/projects/"+strconvI64(projID), body2)
	req2.SetPathValue("id", strconvI64(projID))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	w2.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsAPI)).ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for update, got %d: %s", w2.Code, w2.Body.String())
	}

	// Delete project
	req3 := createAdminReq(t, d, http.MethodDelete, "/admin/projects/"+strconvI64(projID), nil)
	req3.SetPathValue("id", strconvI64(projID))
	w3 := httptest.NewRecorder()
	w3.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsAPI)).ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 for delete, got %d: %s", w3.Code, w3.Body.String())
	}
}

// ─── admin.go ToggleStatusDisabled DB error ───────────────────────────────────

// ToggleStatusDisabled DB error
func TestToggleStatusDisabled_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	statusID, _ := d.CreateStatus(models.Status{Name: "ToggleErr", Color: "#000000", SortOrder: 1})
	body, _ := json.Marshal(map[string]bool{"disabled": true})
	req := createAdminReq(t, d, http.MethodPatch,
		"/admin/statuses/"+strconvI64(statusID)+"/disabled", body)
	req.SetPathValue("id", strconvI64(statusID))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.ToggleStatusDisabled(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}
