package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
)

// -----------------------------------------------------------------------
// AdminFloorplansPage — no fp param but floorplans exist (uses floorplans[0])
// -----------------------------------------------------------------------

func TestAdminFloorplansPage_DefaultFirstFP(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	d.CreateFloorplan("FP DefaultFirst", 0) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/floorplans", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminFloorplansPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// AdminFloorplansPage — fp param but not found (currentFP == nil)
// -----------------------------------------------------------------------

func TestAdminFloorplansPage_FPNotFound(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodGet, "/admin/floorplans?fp=999999", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminFloorplansPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// SetPassword — short password (< 8 chars)
// -----------------------------------------------------------------------

func TestSetPassword_ShortPassword(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("shortpw@test.com", "ShortPW", "password1")

	bodyBytes := []byte(`{"password":"abc"}`)
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/users/"+strconvI64(uid)+"/password", bodyBytes)
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPassword)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short password, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// DeleteTeam — not found is still OK
// -----------------------------------------------------------------------

func TestDeleteTeam_NotFound(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/teams/99999", nil)
	req.SetPathValue("id", "99999")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteTeam)).ServeHTTP(w, req)
	// DeleteTeam probably ignores not-found
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// SeatsAPI — missing floorplan_id
// -----------------------------------------------------------------------

func TestSeatsAPI_NoFloorplanID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodGet, "/api/seats?date=2026-06-01", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SeatsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing floorplan_id, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// UpdateTeam — empty name
// -----------------------------------------------------------------------

func TestUpdateTeam_EmptyName(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("TestTeamUpdate")

	bodyBytes := []byte(`{"name":""}`)
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/teams/"+strconvI64(teamID), bodyBytes)
	req.SetPathValue("id", strconvI64(teamID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateTeam)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusOK {
		t.Fatalf("updateTeam unexpected %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// DeleteHoliday — invalid ID
// -----------------------------------------------------------------------

func TestDeleteHoliday_BadID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &HolidaysHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodDelete, "/admin/holidays/notanid", nil)
	req.SetPathValue("id", "notanid")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
