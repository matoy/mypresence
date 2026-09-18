package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

func TestGuestReservation_HandlerFlow(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	uid, _ := d.CreateLocalUser("hosthandler@test.com", "HostHandler", "password1")
	tok, _ := d.CreateSession(uid)

	statusID, _ := d.CreateStatus(models.Status{Name: "OnSiteH", Color: "#0000ff", OnSite: true, SortOrder: 1})
	date := "2026-09-22"
	d.SetPresences(uid, []string{date}, statusID, "full") //nolint:errcheck

	fpID, _ := d.CreateFloorplan("HostFP", 1)
	seat1, _ := d.CreateSeat(fpID, "SeatH1", 0.1, 0.1)
	seat2, _ := d.CreateSeat(fpID, "SeatH2", 0.2, 0.2)

	// 1. Reserve seat 1 for self
	body1, _ := json.Marshal(map[string]interface{}{
		"seat_id": seat1,
		"date":    date,
		"half":    "full",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/api/reservations", bytes.NewReader(body1))
	req1.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ReserveSeat)).ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 for self reservation, got %d: %s", w1.Code, w1.Body.String())
	}

	// 2. Reserve seat 2 for a guest
	body2, _ := json.Marshal(map[string]interface{}{
		"seat_id":    seat2,
		"date":       date,
		"half":       "full",
		"guest_name": "Dave Miller",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/reservations", bytes.NewReader(body2))
	req2.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ReserveSeat)).ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for guest reservation, got %d: %s", w2.Code, w2.Body.String())
	}

	// Verify DB state
	seats, err := d.GetSeatsWithStatus(fpID, uid, date, "full")
	if err != nil {
		t.Fatalf("GetSeatsWithStatus: %v", err)
	}
	var foundSelf, foundGuest bool
	for _, s := range seats {
		if s.ID == seat1 && s.Status == "mine" {
			foundSelf = true
		}
		if s.ID == seat2 && s.Status == "mine_guest" && s.GuestName == "Dave Miller" {
			foundGuest = true
		}
	}
	if !foundSelf {
		t.Errorf("expected seat1 to have status 'mine'")
	}
	if !foundGuest {
		t.Errorf("expected seat2 to have status 'mine_guest' and guest_name 'Dave Miller'")
	}

	// 3. Cancel targeted "guest"
	cancelBody, _ := json.Marshal(map[string]interface{}{
		"dates": []string{date},
		"type":  "guest",
	})
	cancelReq := httptest.NewRequest(http.MethodDelete, "/api/reservations/bulk", bytes.NewReader(cancelBody))
	cancelReq.AddCookie(&http.Cookie{Name: "session", Value: tok})
	cancelReq.Header.Set("Content-Type", "application/json")
	wCancel := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CancelReservationsByDates)).ServeHTTP(wCancel, cancelReq)
	if wCancel.Code != http.StatusOK {
		t.Fatalf("expected 200 for cancel guest, got %d: %s", wCancel.Code, wCancel.Body.String())
	}

	// Verify details: self remains, guest removed
	details, _ := d.GetUserReservationDetails(uid, date, date)
	if !details[date].HasSelf {
		t.Errorf("expected self reservation to remain")
	}
	if len(details[date].GuestNames) != 0 {
		t.Errorf("expected guest reservations to be removed")
	}

	// 4. Bulk reserve for guest
	bulkDates := []string{"2026-09-23", "2026-09-24"}
	d.SetPresences(uid, bulkDates, statusID, "full") //nolint:errcheck
	bulkBody, _ := json.Marshal(map[string]interface{}{
		"seat_id":    seat2,
		"dates":      bulkDates,
		"half":       "full",
		"guest_name": "Emma Watson",
	})
	bulkReq := httptest.NewRequest(http.MethodPost, "/api/reservations/bulk", bytes.NewReader(bulkBody))
	bulkReq.AddCookie(&http.Cookie{Name: "session", Value: tok})
	bulkReq.Header.Set("Content-Type", "application/json")
	wBulk := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.BulkReserveSeats)).ServeHTTP(wBulk, bulkReq)
	if wBulk.Code != http.StatusOK {
		t.Fatalf("expected 200 for bulk reservation, got %d: %s", wBulk.Code, wBulk.Body.String())
	}
	var bulkRes map[string]interface{}
	json.Unmarshal(wBulk.Body.Bytes(), &bulkRes) //nolint:errcheck
	if booked, ok := bulkRes["booked"].(float64); !ok || booked != 2 {
		t.Errorf("expected booked 2, got %v", bulkRes["booked"])
	}

	// 5. Cancel "all" for bulk dates
	cancelAllBody, _ := json.Marshal(map[string]interface{}{
		"dates": bulkDates,
		"type":  "all",
	})
	cancelAllReq := httptest.NewRequest(http.MethodDelete, "/api/reservations/bulk", bytes.NewReader(cancelAllBody))
	cancelAllReq.AddCookie(&http.Cookie{Name: "session", Value: tok})
	cancelAllReq.Header.Set("Content-Type", "application/json")
	wCancelAll := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CancelReservationsByDates)).ServeHTTP(wCancelAll, cancelAllReq)
	if wCancelAll.Code != http.StatusOK {
		t.Fatalf("expected 200 for cancel all, got %d: %s", wCancelAll.Code, wCancelAll.Body.String())
	}
	detailsAll, _ := d.GetUserReservationDetails(uid, "2026-09-23", "2026-09-24")
	if len(detailsAll) != 0 {
		t.Errorf("expected no reservations left after cancel all, got %v", detailsAll)
	}
}
