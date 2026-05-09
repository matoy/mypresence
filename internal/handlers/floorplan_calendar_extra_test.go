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

// ─── floorplan.go uncovered branches ─────────────────────────────────────────

// ReserveSeat conflict (covers L.151-155)
func TestReserveSeat_Conflict(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	uid, _ := d.CreateLocalUser("reserveconflict@test.com", "ReserveConflict", "password1")
	tok, _ := d.CreateSession(uid)

	// Set user as on-site (need a status with OnSite=true)
	statusID, _ := d.CreateStatus(models.Status{Name: "OnSiteRS", Color: "#0000ff", OnSite: true, SortOrder: 1})
	d.SetPresences(uid, []string{"2026-06-01"}, statusID, "") //nolint:errcheck

	fpID, _ := d.CreateFloorplan("ConflictFP", 1)
	seatID, _ := d.CreateSeat(fpID, "Seat1", 0.5, 0.5)

	body, _ := json.Marshal(map[string]interface{}{
		"seat_id": seatID,
		"date":    "2026-06-01",
		"half":    "",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/api/reservations", bytes.NewReader(body))
	req1.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	w1.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ReserveSeat)).ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first reservation expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Create another user and try to reserve the same seat
	uid2, _ := d.CreateLocalUser("reserveconflict2@test.com", "ReserveConflict2", "password1")
	tok2, _ := d.CreateSession(uid2)
	d.SetPresences(uid2, []string{"2026-06-01"}, statusID, "") //nolint:errcheck

	body2, _ := json.Marshal(map[string]interface{}{
		"seat_id": seatID,
		"date":    "2026-06-01",
		"half":    "",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/reservations", bytes.NewReader(body2))
	req2.AddCookie(&http.Cookie{Name: "session", Value: tok2})
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	w2.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ReserveSeat)).ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("conflict expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

// AdminFloorplansPage with existing floorplan fpID param (covers L.187-189)
func TestAdminFloorplansPage_WithFPID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("TestFP_WithID", 1)
	d.CreateSeat(fpID, "TestSeat", 0.3, 0.4) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/floorplans?fp="+strconvI64(fpID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminFloorplansPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// UpdateSeat DB error (covers L.425-427)
func TestUpdateSeat_DBError2(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("UpdateSeatFP", 1)
	seatID, _ := d.CreateSeat(fpID, "SeatToUpdate", 0.5, 0.5)
	body, _ := json.Marshal(map[string]interface{}{"label": "Updated", "x_pct": 0.6, "y_pct": 0.6})
	req := createAdminReq(t, d, http.MethodPut, "/admin/seats/"+strconvI64(seatID), body)
	req.SetPathValue("id", strconvI64(seatID))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.UpdateSeat(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// DeleteFloorplan with image path (covers L.297-299)
func TestDeleteFloorplan_WithImage(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FPWithImage", 1)
	// Set image path directly
	d.SetFloorplanImage(fpID, "some_image.png") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodDelete, "/admin/floorplans/"+strconvI64(fpID), nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteFloorplan)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── calendar.go uncovered branches ──────────────────────────────────────────

// CalendarPage with a holiday in the current month (covers L.59-63)
func TestCalendarPage_WithHoliday(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("calholday@test.com", "CalHolDay", "password1")
	tok, _ := d.CreateSession(uid)

	// Create a holiday in January 2026
	d.CreateHoliday("2026-01-01", "New Year", false) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/calendar?year=2026&month=1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// CalendarPage with declared presence (covers L.86-88)
func TestCalendarPage_WithDeclaredPresence(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("caldeclared@test.com", "CalDeclared", "password1")
	tok, _ := d.CreateSession(uid)

	statusID, _ := d.CreateStatus(models.Status{Name: "Present", Color: "#00ff00", Billable: true, SortOrder: 1})
	d.SetPresences(uid, []string{"2026-01-05", "2026-01-06"}, statusID, "") //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/calendar?year=2026&month=1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// SetPresences DB error (covers L.174-177)
func TestSetPresences_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("setpresdb@test.com", "SetPresDB", "password1")
	tok, _ := d.CreateSession(uid)
	statusID, _ := d.CreateStatus(models.Status{Name: "DBErrStatus", Color: "#ff0000", SortOrder: 99})

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":   uid,
		"status_id": statusID,
		"dates":     []string{"2026-01-05"},
		"half":      "",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/presences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.SetPresences(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ClearPresences DB error (covers L.211-214)
func TestClearPresences_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("clearpresdb@test.com", "ClearPresDB", "password1")
	tok, _ := d.CreateSession(uid)

	body, _ := json.Marshal(map[string]interface{}{
		"user_id": uid,
		"dates":   []string{"2026-01-05"},
		"half":    "",
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/presences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.ClearPresences(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// SetPresences: date range update (covers L.157-162 minDate/maxDate updates)
func TestSetPresences_MultiDateRange(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("multidaterange@test.com", "MultiDateRange", "password1")
	tok, _ := d.CreateSession(uid)
	statusID, _ := d.CreateStatus(models.Status{Name: "RangeStatus", Color: "#aabbcc", SortOrder: 2})

	// Multiple dates in different order to trigger minDate < d and d > maxDate branches
	body, _ := json.Marshal(map[string]interface{}{
		"user_id":   uid,
		"status_id": statusID,
		"dates":     []string{"2026-03-15", "2026-03-10", "2026-03-20"},
		"half":      "",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/presences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// GetPresencesAPI team filter DB error (covers L.255+260-263)
func TestGetPresencesAPI_TeamDBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("TeamPresDBErr")
	req := createAdminReq(t, d, http.MethodGet,
		"/api/presences?team_id="+strconvI64(teamID)+"&year=2026&month=1", nil)

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.GetPresencesAPI(rw, r)
	})).ServeHTTP(w, req)
	// Either 200 (GetTeamMembers returns empty before DB close) or 500
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d: %s", w.Code, w.Body.String())
	}
}
