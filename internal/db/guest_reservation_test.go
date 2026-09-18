package db

import (
	"testing"
)

func TestGuestReservation_DBFlow(t *testing.T) {
	d := newTestDB(t)
	uid1 := seedUser(t, d, "host@test.com")
	uid2 := seedUser(t, d, "other@test.com")
	fpID, seat1 := seedFloorplanAndSeat(t, d, "S1")

	// Insert seat2 on the same floorplan
	res, err := d.floorplan.Exec("INSERT INTO seats (floorplan_id, label, x_pct, y_pct) VALUES (?, 'S2', 20, 20)", fpID)
	if err != nil {
		t.Fatalf("insert seat2: %v", err)
	}
	seat2, _ := res.LastInsertId()

	date := "2026-09-20"

	// 1. Host reserves seat1 for self
	if err := d.ReserveSeat(seat1, uid1, date, "full"); err != nil {
		t.Fatalf("reserve seat1 for self: %v", err)
	}

	// 2. Host also reserves seat2 for a guest "Alice Smith"
	if err := d.ReserveSeat(seat2, uid1, date, "full", "Alice Smith"); err != nil {
		t.Fatalf("reserve seat2 for guest: %v", err)
	}

	// 3. Check GetSeatsWithStatus from host's perspective (uid1)
	seatsHost, err := d.GetSeatsWithStatus(fpID, uid1, date, "full")
	if err != nil {
		t.Fatalf("GetSeatsWithStatus host: %v", err)
	}
	if len(seatsHost) != 2 {
		t.Fatalf("expected 2 seats, got %d", len(seatsHost))
	}
	for _, s := range seatsHost {
		switch s.ID {
		case seat1:
			if s.Status != "mine" {
				t.Errorf("seat1 expected status 'mine', got %q", s.Status)
			}
			if s.GuestName != "" {
				t.Errorf("seat1 expected empty GuestName, got %q", s.GuestName)
			}
		case seat2:
			if s.Status != "mine_guest" {
				t.Errorf("seat2 expected status 'mine_guest', got %q", s.Status)
			}
			if s.GuestName != "Alice Smith" {
				t.Errorf("seat2 expected GuestName 'Alice Smith', got %q", s.GuestName)
			}
		}
	}

	// 4. Check GetSeatsWithStatus from other user's perspective (uid2)
	seatsOther, err := d.GetSeatsWithStatus(fpID, uid2, date, "full")
	if err != nil {
		t.Fatalf("GetSeatsWithStatus other: %v", err)
	}
	for _, s := range seatsOther {
		if s.ID == seat2 {
			if s.Status != "taken" {
				t.Errorf("seat2 expected status 'taken' for other user, got %q", s.Status)
			}
			if s.OccupantName != "Alice Smith" {
				t.Errorf("seat2 expected OccupantName 'Alice Smith', got %q", s.OccupantName)
			}
		}
	}

	// 5. Check GetUserReservationDetails
	details, err := d.GetUserReservationDetails(uid1, date, date)
	if err != nil {
		t.Fatalf("GetUserReservationDetails: %v", err)
	}
	dayRes, exists := details[date]
	if !exists {
		t.Fatalf("expected details for date %s", date)
	}
	if !dayRes.HasSelf {
		t.Errorf("expected dayRes.HasSelf to be true")
	}
	if len(dayRes.GuestNames) != 1 || dayRes.GuestNames[0] != "Alice Smith" {
		t.Errorf("expected GuestNames ['Alice Smith'], got %v", dayRes.GuestNames)
	}

	// 6. Test targeted cancellation: cancel "guest"
	if err := d.CancelUserReservationsForDates(uid1, []string{date}, "guest"); err != nil {
		t.Fatalf("cancel guest reservation: %v", err)
	}

	detailsAfterGuestCancel, err := d.GetUserReservationDetails(uid1, date, date)
	if err != nil {
		t.Fatalf("GetUserReservationDetails after guest cancel: %v", err)
	}
	dayRes2 := detailsAfterGuestCancel[date]
	if !dayRes2.HasSelf {
		t.Errorf("expected self reservation to still exist")
	}
	if len(dayRes2.GuestNames) != 0 {
		t.Errorf("expected guest reservations to be cleared, got %v", dayRes2.GuestNames)
	}

	// 7. Re-reserve guest, then test cancel "self"
	if err := d.ReserveSeat(seat2, uid1, date, "full", "Bob Jones"); err != nil {
		t.Fatalf("re-reserve seat2 for guest Bob: %v", err)
	}
	if err := d.CancelUserReservationsForDates(uid1, []string{date}, "self"); err != nil {
		t.Fatalf("cancel self reservation: %v", err)
	}
	detailsAfterSelfCancel, err := d.GetUserReservationDetails(uid1, date, date)
	if err != nil {
		t.Fatalf("GetUserReservationDetails after self cancel: %v", err)
	}
	dayRes3 := detailsAfterSelfCancel[date]
	if dayRes3.HasSelf {
		t.Errorf("expected self reservation to be gone")
	}
	if len(dayRes3.GuestNames) != 1 || dayRes3.GuestNames[0] != "Bob Jones" {
		t.Errorf("expected Bob Jones guest reservation to remain, got %v", dayRes3.GuestNames)
	}

	// 8. Cancel "all"
	if err := d.CancelUserReservationsForDates(uid1, []string{date}, "all"); err != nil {
		t.Fatalf("cancel all reservations: %v", err)
	}
	detailsAfterAllCancel, err := d.GetUserReservationDetails(uid1, date, date)
	if err != nil {
		t.Fatalf("GetUserReservationDetails after all cancel: %v", err)
	}
	if len(detailsAfterAllCancel) != 0 {
		t.Errorf("expected no reservations left, got %v", detailsAfterAllCancel)
	}
}

func TestGuestReservation_BulkReserve(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "bulkhost@test.com")
	_, seatID := seedFloorplanAndSeat(t, d, "BulkSeat")

	dates := []string{"2026-09-21", "2026-09-22"}
	sid := seedOnSiteStatus(t, d)
	// Mark user on-site on those dates
	if err := d.SetPresences(uid, dates, sid, "full"); err != nil {
		t.Fatalf("SetPresences: %v", err)
	}

	count := d.BulkReserveSeat(seatID, uid, dates, "full", "Charlie Brown")
	if count != 2 {
		t.Fatalf("expected BulkReserveSeat count 2, got %d", count)
	}

	details, err := d.GetUserReservationDetails(uid, "2026-09-21", "2026-09-22")
	if err != nil {
		t.Fatalf("GetUserReservationDetails: %v", err)
	}
	for _, dt := range dates {
		rd := details[dt]
		if rd.HasSelf {
			t.Errorf("date %s expected HasSelf false", dt)
		}
		if len(rd.GuestNames) != 1 || rd.GuestNames[0] != "Charlie Brown" {
			t.Errorf("date %s expected guest 'Charlie Brown', got %v", dt, rd.GuestNames)
		}
	}
}
