package db

import (
	"testing"
	"time"
)

// TestCreateAndListNewsMessages covers basic CRUD for news messages.
func TestCreateAndListNewsMessages(t *testing.T) {
	d := newTestDB(t)

	// Initially empty.
	msgs, err := d.ListNewsMessages()
	if err != nil {
		t.Fatalf("ListNewsMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}

	// Create one.
	today := time.Now().Format("2006-01-02")
	id, err := d.CreateNewsMessage("Test Title", "Hello world", today, today, "#dc2626", false)
	if err != nil {
		t.Fatalf("CreateNewsMessage: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID")
	}

	// List should return 1.
	msgs, err = d.ListNewsMessages()
	if err != nil {
		t.Fatalf("ListNewsMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.Title != "Test Title" {
		t.Errorf("title: got %q, want %q", m.Title, "Test Title")
	}
	if m.Content != "Hello world" {
		t.Errorf("content: got %q", m.Content)
	}
	if m.BgColor != "#dc2626" {
		t.Errorf("bg_color: got %q", m.BgColor)
	}
}

// TestGetActiveNewsMessages checks that only in-range messages are returned.
func TestGetActiveNewsMessages(t *testing.T) {
	d := newTestDB(t)

	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	lastMonth := time.Now().AddDate(0, -1, 0).Format("2006-01-02")

	// Active: today is within range.
	_, err := d.CreateNewsMessage("Active", "content", yesterday, tomorrow, "#dc2626", false)
	if err != nil {
		t.Fatalf("CreateNewsMessage active: %v", err)
	}

	// Expired: ended yesterday.
	_, err = d.CreateNewsMessage("Expired", "content", lastMonth, yesterday, "#aabbcc", false)
	if err != nil {
		t.Fatalf("CreateNewsMessage expired: %v", err)
	}

	// Future: starts tomorrow.
	_, err = d.CreateNewsMessage("Future", "content", tomorrow, tomorrow, "#123456", false)
	if err != nil {
		t.Fatalf("CreateNewsMessage future: %v", err)
	}

	// Single-day active (starts and ends today).
	_, err = d.CreateNewsMessage("Today only", "content", today, today, "#ffffff", false)
	if err != nil {
		t.Fatalf("CreateNewsMessage today: %v", err)
	}

	active, err := d.GetActiveNewsMessages()
	if err != nil {
		t.Fatalf("GetActiveNewsMessages: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active messages, got %d", len(active))
	}
	for _, m := range active {
		if m.Title == "Expired" || m.Title == "Future" {
			t.Errorf("unexpected message in active set: %q", m.Title)
		}
	}
}

// TestUpdateNewsMessage verifies that updates are persisted.
func TestUpdateNewsMessage(t *testing.T) {
	d := newTestDB(t)
	today := time.Now().Format("2006-01-02")

	id, err := d.CreateNewsMessage("Original", "Original content", today, today, "#dc2626", false)
	if err != nil {
		t.Fatalf("CreateNewsMessage: %v", err)
	}

	err = d.UpdateNewsMessage(id, "Updated", "Updated content", today, today, "#aabbcc", false)
	if err != nil {
		t.Fatalf("UpdateNewsMessage: %v", err)
	}

	msgs, err := d.ListNewsMessages()
	if err != nil {
		t.Fatalf("ListNewsMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.Title != "Updated" {
		t.Errorf("title: got %q, want Updated", m.Title)
	}
	if m.Content != "Updated content" {
		t.Errorf("content: got %q", m.Content)
	}
	if m.BgColor != "#aabbcc" {
		t.Errorf("bg_color: got %q", m.BgColor)
	}
}

// TestDeleteNewsMessage verifies removal.
func TestDeleteNewsMessage(t *testing.T) {
	d := newTestDB(t)
	today := time.Now().Format("2006-01-02")

	id, err := d.CreateNewsMessage("To delete", "content", today, today, "#dc2626", false)
	if err != nil {
		t.Fatalf("CreateNewsMessage: %v", err)
	}

	if err := d.DeleteNewsMessage(id); err != nil {
		t.Fatalf("DeleteNewsMessage: %v", err)
	}

	msgs, err := d.ListNewsMessages()
	if err != nil {
		t.Fatalf("ListNewsMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after deletion, got %d", len(msgs))
	}
}

// TestGetNewsMessageTitle verifies the helper.
func TestGetNewsMessageTitle(t *testing.T) {
	d := newTestDB(t)
	today := time.Now().Format("2006-01-02")

	id, err := d.CreateNewsMessage("My Title", "content", today, today, "#dc2626", false)
	if err != nil {
		t.Fatalf("CreateNewsMessage: %v", err)
	}

	title := d.GetNewsMessageTitle(id)
	if title != "My Title" {
		t.Errorf("got %q, want %q", title, "My Title")
	}

	// Non-existent ID returns empty string.
	if got := d.GetNewsMessageTitle(99999); got != "" {
		t.Errorf("expected empty for unknown ID, got %q", got)
	}
}

// TestDeleteNewsMessage_NotFound ensures deleting a non-existent ID does not error.
func TestDeleteNewsMessage_NotFound(t *testing.T) {
	d := newTestDB(t)
	// Should not error even if ID doesn't exist.
	if err := d.DeleteNewsMessage(99999); err != nil {
		t.Fatalf("expected no error for missing ID, got: %v", err)
	}
}

// TestListNewsMessages_OrderedByStartDateDesc verifies ordering.
func TestListNewsMessages_OrderedByStartDateDesc(t *testing.T) {
	d := newTestDB(t)
	dates := []struct{ start, end string }{
		{"2026-01-01", "2026-01-31"},
		{"2026-03-01", "2026-03-31"},
		{"2026-02-01", "2026-02-28"},
	}
	for _, dt := range dates {
		if _, err := d.CreateNewsMessage("msg "+dt.start, "c", dt.start, dt.end, "#000000", false); err != nil {
			t.Fatalf("CreateNewsMessage: %v", err)
		}
	}
	msgs, err := d.ListNewsMessages()
	if err != nil {
		t.Fatalf("ListNewsMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	// Most recent start_date first.
	if msgs[0].StartDate != "2026-03-01" {
		t.Errorf("first message should be march, got %q", msgs[0].StartDate)
	}
	if msgs[2].StartDate != "2026-01-01" {
		t.Errorf("last message should be january, got %q", msgs[2].StartDate)
	}
}

// TestCreateNewsMessage_Recurring verifies that the recurring flag is persisted.
func TestCreateNewsMessage_Recurring(t *testing.T) {
	d := newTestDB(t)
	today := time.Now().Format("2006-01-02")

	id, err := d.CreateNewsMessage("Monthly", "content", today, today, "#7c3aed", true)
	if err != nil {
		t.Fatalf("CreateNewsMessage recurring: %v", err)
	}
	msgs, err := d.ListNewsMessages()
	if err != nil {
		t.Fatalf("ListNewsMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !msgs[0].Recurring {
		t.Error("expected recurring=true, got false")
	}

	// UpdateNewsMessage can toggle recurring off.
	if err := d.UpdateNewsMessage(id, "Monthly", "content", today, today, "#7c3aed", false); err != nil {
		t.Fatalf("UpdateNewsMessage: %v", err)
	}
	msgs, _ = d.ListNewsMessages()
	if msgs[0].Recurring {
		t.Error("expected recurring=false after update, got true")
	}
}

// TestGetActiveNewsMessages_Recurring verifies that a recurring message is active
// when today's day-of-month falls within the start/end day range.
func TestGetActiveNewsMessages_Recurring(t *testing.T) {
	d := newTestDB(t)
	now := time.Now()
	todayDay := now.Day()

	// Build start/end dates that bracket today's day-of-month.
	// Use two days before and two days after today to ensure we're in range,
	// regardless of the current month/year.
	startDay := todayDay - 2
	if startDay < 1 {
		startDay = 1
	}
	endDay := todayDay + 2
	if endDay > 28 {
		endDay = 28
	}

	startDate := time.Date(2026, 1, startDay, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	endDate := time.Date(2026, 1, endDay, 0, 0, 0, 0, time.UTC).Format("2006-01-02")

	// Recurring, active (today in range).
	if _, err := d.CreateNewsMessage("Monthly active", "content", startDate, endDate, "#7c3aed", true); err != nil {
		t.Fatalf("CreateNewsMessage: %v", err)
	}

	// Recurring, inactive (today NOT in range): use day 1–2 of month, unless today is 1 or 2.
	inactiveStart := "2026-01-01"
	inactiveEnd := "2026-01-02"
	if todayDay <= 2 {
		inactiveStart = "2026-01-27"
		inactiveEnd = "2026-01-28"
	}
	if _, err := d.CreateNewsMessage("Monthly inactive", "content", inactiveStart, inactiveEnd, "#dc2626", true); err != nil {
		t.Fatalf("CreateNewsMessage: %v", err)
	}

	// Non-recurring, expired — should not appear.
	past := time.Now().AddDate(0, -1, 0).Format("2006-01-02")
	if _, err := d.CreateNewsMessage("Expired", "content", past, past, "#dc2626", false); err != nil {
		t.Fatalf("CreateNewsMessage: %v", err)
	}

	active, err := d.GetActiveNewsMessages()
	if err != nil {
		t.Fatalf("GetActiveNewsMessages: %v", err)
	}
	found := false
	for _, m := range active {
		if m.Title == "Monthly active" {
			found = true
			if !m.Recurring {
				t.Error("expected Recurring=true on active recurring message")
			}
		}
		if m.Title == "Monthly inactive" {
			t.Error("Monthly inactive should not appear in active set")
		}
		if m.Title == "Expired" {
			t.Error("Expired non-recurring should not appear")
		}
	}
	if !found {
		t.Error("expected 'Monthly active' to appear in active set")
	}
}
