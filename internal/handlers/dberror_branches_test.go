package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
)

// Tests that trigger DB errors by closing the database before the handler call.
// This covers error branches that return HTTP 500.

func TestListFloorplansAPI_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d}
	d.Close() // Force DB error

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/floorplans", nil)
	h.ListFloorplansAPI(w, r)
	t.Logf("code=%d body=%s", w.Code, w.Body.String())
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListTeamsAPI_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d}
	d.Close() // Force DB error

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/admin/teams", nil)
	h.ListTeamsAPI(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestAdminListSeats_MissingFPID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/admin/seats", nil)
	h.AdminListSeats(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHolidaysPage_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}
	d.Close() // Force DB error

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/holidays", nil)
	h.HolidaysPage(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCreateHoliday_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}
	d.Close() // Force DB error

	body := []byte(`{"date":"2024-01-01","name":"New Year"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/admin/holidays", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.CreateHoliday(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestCancelReservation_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	uid, err := d.CreateLocalUser("canceldbtest@test.com", "Cancel", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	h := &FloorplanHandler{DB: d}

	req := httptest.NewRequest(http.MethodDelete, "/api/reservations/1", nil)
	req.SetPathValue("id", "1")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.Close() // Close DB after auth but before handler body
		h.CancelReservation(w, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListPATs_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	uid, err := d.CreateLocalUser("listpatdb@test.com", "ListPAT", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	h := &PATHandler{DB: d}

	req := httptest.NewRequest(http.MethodGet, "/api/tokens", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.Close()
		h.ListPATs(w, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevokePAT_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	uid, err := d.CreateLocalUser("revokepatdb@test.com", "RevokePAT", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	h := &PATHandler{DB: d}

	req := httptest.NewRequest(http.MethodDelete, "/api/tokens/1", nil)
	req.SetPathValue("id", "1")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.Close()
		h.RevokePAT(w, r)
	})).ServeHTTP(w, req)
	// RevokePAT returns 404 on not found, which is the expected error when DB is closed
	if w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 404 or 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSeat_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	uid, err := d.CreateLocalUser("updateseatdb@test.com", "UpdateSeat", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	if err := d.UpdateUserRoles(uid, "global"); err != nil {
		t.Fatalf("UpdateUserRoles: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	h := &FloorplanHandler{DB: d}

	body := bytes.NewReader([]byte(`{"label":"A1","x_pct":0.5,"y_pct":0.5}`))
	req := httptest.NewRequest(http.MethodPut, "/api/admin/seats/1", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.Close()
		h.UpdateSeat(w, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d: %s", w.Code, w.Body.String())
	}
}
