package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// TestGetPresencesAPI_WithMembers covers calendar.go L.255-257 (loop body when members non-empty)
func TestGetPresencesAPI_WithMembers(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("TeamPresWithMembers")
	uid, _ := d.CreateLocalUser("pres_member@test.com", "PresMember", "password1")
	d.AddTeamMember(teamID, uid) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet,
		"/api/presences?team_id="+strconvI64(teamID)+"&year=2026&month=1", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.GetPresencesAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAdminProjectsAPI_FilterActive_Inactive covers projects.go L.283 (filterActive="1" but project inactive in AdminProjectsAPI)
func TestAdminProjectsAPI_FilterActive_Inactive(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	// Create an inactive project
	d.CreateProject("InactiveProjAPI", "IAPI", 0, false, "2025-01-01", "2025-12-31") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/admin/projects?active=1", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteHoliday_DBError2 covers admin_holidays.go L.109-112 (DeleteHoliday DB error with auth)
func TestDeleteHoliday_DBError2(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}

	id, _ := d.CreateHoliday("2026-10-01", "DB Error Holiday", false)
	req := createAdminReq(t, d, http.MethodDelete, "/admin/holidays/"+strconvI64(id), nil)
	req.SetPathValue("id", strconvI64(id))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.DeleteHoliday(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// TestFloorplan_CreateSeatDBError covers floorplan.go L.398-404 (CreateSeat DB error)
func TestFloorplan_CreateSeatDBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d}

	fpID, _ := d.CreateFloorplan("FPCreateSeatErr", 0)
	body := []byte(`{"label":"","x_pct":10.0,"y_pct":20.0}`)
	req := createAdminReq(t, d, http.MethodPost, "/admin/floorplans/"+strconvI64(fpID)+"/seats", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", strconvI64(fpID))
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

// TestFloorplan_UpdateSeatDBError covers floorplan.go L.425-431 (UpdateSeat DB error via auth)
func TestFloorplan_UpdateSeatDBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d}

	fpID, _ := d.CreateFloorplan("FPUpdateSeatErr", 0)
	seatID, _ := d.CreateSeat(fpID, "DeskU", 5, 5)
	body := []byte(`{"label":"","x_pct":15.0,"y_pct":25.0}`)
	req := createAdminReq(t, d, http.MethodPut, "/admin/seats/"+strconvI64(seatID), body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", strconvI64(seatID))
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

func TestFloorplan_Reservations_Branches(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d}

	uID, _ := d.CreateLocalUser("seat_res_user@test.com", "Seat User", "pass")
	user, _ := d.GetUserByID(uID)

	sID, _ := d.CreateStatus(models.Status{Name: "Work", Color: "#000000", OnSite: true, Billable: true})
	fpID, _ := d.CreateFloorplan("Floor 1", 0)
	seatID, _ := d.CreateSeat(fpID, "Desk 1", 10, 10)

	// 1. Bad JSON
	rec := httptest.NewRecorder()
	req := reqWithUser(d, user, http.MethodPost, "/api/reservations", strings.NewReader("bad-json"))
	h.ReserveSeat(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad json ReserveSeat, got %d", rec.Code)
	}

	// 2. Missing params (seat_id == 0)
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user, http.MethodPost, "/api/reservations", strings.NewReader(`{"seat_id":0,"date":"2026-08-20"}`))
	h.ReserveSeat(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on missing seat_id, got %d", rec.Code)
	}

	// 3. User not on-site -> 403
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user, http.MethodPost, "/api/reservations", strings.NewReader(fmt.Sprintf(`{"seat_id":%d,"date":"2026-08-20"}`, seatID)))
	h.ReserveSeat(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 when user not declared on site, got %d", rec.Code)
	}

	// 4. User on-site -> reserve success (200)
	_ = d.SetPresences(uID, []string{"2026-08-20"}, sID, "full")
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user, http.MethodPost, "/api/reservations", strings.NewReader(fmt.Sprintf(`{"seat_id":%d,"date":"2026-08-20"}`, seatID)))
	h.ReserveSeat(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on ReserveSeat success, got %d: %s", rec.Code, rec.Body.String())
	}

	// 5. Conflict: duplicate reservation on same seat & date -> 409
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user, http.MethodPost, "/api/reservations", strings.NewReader(fmt.Sprintf(`{"seat_id":%d,"date":"2026-08-20"}`, seatID)))
	h.ReserveSeat(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 on duplicate reservation, got %d", rec.Code)
	}

	// 6. CancelReservation
	resDates, _ := d.GetUserReservationDates(uID, "2026-08-01", "2026-08-31")
	_ = resDates
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user, http.MethodDelete, "/api/reservations/1", nil)
	req.SetPathValue("id", "1")
	h.CancelReservation(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on CancelReservation, got %d", rec.Code)
	}
}
