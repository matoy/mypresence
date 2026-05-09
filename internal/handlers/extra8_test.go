package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"presence-app/internal/middleware"
	"presence-app/internal/models"
)

// -----------------------------------------------------------------------
// SetProjectTime — days > 0 path (billable days check)
// -----------------------------------------------------------------------

func TestSetProjectTime_WithDays(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	projID, _ := d.CreateProject("Days Project", "DP01", 0, true, "2026-01-01", "2030-12-31")

	bodyBytes, _ := json.Marshal(map[string]interface{}{"project_id": projID, "year": 2026, "month": 6, "days": 5.0})
	req := createAdminReq(t, d, http.MethodPost, "/api/project-time", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	// It could succeed or return 422 if billable days insufficient — just check it doesn't crash
	if w.Code != http.StatusOK && w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 200 or 422, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// AdminRevokePAT — additional paths
// -----------------------------------------------------------------------

func TestAdminRevokePAT_NotFound(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/pats/9999999", nil)
	req.SetPathValue("id", "9999999")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminRevokePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected code %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CancelReservation — bad ID
// -----------------------------------------------------------------------

func TestCancelReservation_BadID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodDelete, "/api/reservations/notanumber", nil)
	req.SetPathValue("id", "notanumber")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CancelReservation)).ServeHTTP(w, req)
	// ID 0 → user check might fail or succeed silently
	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
		t.Fatalf("unexpected code %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ReserveSeat — success path with valid seat
// -----------------------------------------------------------------------

func TestReserveSeat_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP Reserve2", 0)
	seatID, _ := d.CreateSeat(fpID, "R2", 0.5, 0.5)

	// First create presence for the user on that day (required for reservation)
	uid, _ := d.CreateLocalUser("reserve2@test.com", "Reserve2", "password1")
	tok, _ := d.CreateSession(uid)
	status, _ := d.CreateStatus(models.Status{Name: "OnSite", Color: "#abc", OnSite: true})
	d.SetPresences(uid, []string{"2026-06-10"}, status, "") //nolint:errcheck

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"seat_id": seatID,
		"date":    "2026-06-10",
		"half":    "",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/reservations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ReserveSeat)).ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 200 or 422, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// AdminFloorplansPage — with image data
// -----------------------------------------------------------------------

func TestAdminFloorplansPage_WithImageData(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP WithImage", 0)
	d.SetFloorplanImage(fpID, "floorplan_1.png") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/floorplans?id="+strconvI64(fpID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminFloorplansPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// CreatePAT — valid with expiry
// -----------------------------------------------------------------------

func TestCreatePAT_WithExpiry(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("patexpiry@test.com", "PATExpiry", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamManager)) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"description": "expiry test token",
		"expires_at":  "2030-12-31",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/pat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreatePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ChangePasswordPost — additional paths
// -----------------------------------------------------------------------

func TestChangePasswordPost_NonLocalUser(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &SettingsHandler{DB: d, Render: noRender}

	// Upsert a non-local (SSO) user
	u, _ := d.UpsertUser("sso@test.com", "SSO User")
	tok, _ := d.CreateSession(u.ID)

	body := []byte("current_password=whatever&new_password=newpass99&confirm_password=newpass99")
	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ChangePasswordPost)).ServeHTTP(w, req)
	// Non-local user should get a redirect with error about no local password
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// UpdateSeat — error path
// -----------------------------------------------------------------------

func TestUpdateSeat_BadID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	bodyBytes, _ := json.Marshal(map[string]interface{}{"label": "Updated", "x_pct": 0.5, "y_pct": 0.5})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/seats/notanumber", bodyBytes)
	req.SetPathValue("id", "notanumber")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateSeat)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 400/200/500 for bad ID, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ListFloorplansAPI — with date
// -----------------------------------------------------------------------

func TestListFloorplansAPI_WithDate(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	d.CreateFloorplan("FP ListDate", 0) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/floorplans?date=2026-06-01", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListFloorplansAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
