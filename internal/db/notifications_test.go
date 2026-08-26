package db

import (
	"testing"
	"time"
)

func TestNotifications_CRUDAndLifecycle(t *testing.T) {
	d := newTestDB(t)

	u1, err := d.CreateLocalUser("user1@example.com", "Alice", "password123")
	if err != nil {
		t.Fatalf("CreateLocalUser u1: %v", err)
	}
	admin, err := d.CreateLocalUser("admin@example.com", "Bob Admin", "password123")
	if err != nil {
		t.Fatalf("CreateLocalUser admin: %v", err)
	}

	// 1. Initially no notifications for u1
	unread, err := d.GetUnreadNotifications(u1)
	if err != nil {
		t.Fatalf("GetUnreadNotifications: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("expected 0 unread notifications, got %d", len(unread))
	}

	// 2. Create notification for u1
	notifID, err := d.CreateNotification(u1, admin, "team_added", "Ajout à une équipe", "Vous avez été ajouté à l'équipe Dev par Bob Admin.", "")
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if notifID == 0 {
		t.Fatal("expected non-zero notification ID")
	}

	// 3. GetUnreadNotifications should return 1 with ActorName populated
	unread, err = d.GetUnreadNotifications(u1)
	if err != nil {
		t.Fatalf("GetUnreadNotifications: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread notification, got %d", len(unread))
	}
	n := unread[0]
	if n.ID != notifID {
		t.Errorf("expected ID %d, got %d", notifID, n.ID)
	}
	if n.Type != "team_added" {
		t.Errorf("expected Type 'team_added', got %q", n.Type)
	}
	if n.ActorName != "Bob Admin" {
		t.Errorf("expected ActorName 'Bob Admin', got %q", n.ActorName)
	}
	if n.Acknowledged {
		t.Error("expected unacknowledged notification")
	}

	// 4. Acknowledge notification
	if err := d.AcknowledgeNotification(notifID, u1); err != nil {
		t.Fatalf("AcknowledgeNotification: %v", err)
	}

	// 5. Unread should now be 0
	unread, err = d.GetUnreadNotifications(u1)
	if err != nil {
		t.Fatalf("GetUnreadNotifications after ack: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("expected 0 unread notifications after ack, got %d", len(unread))
	}

	// 6. GetUserNotificationLogs should return 1 acknowledged notification
	logs, err := d.GetUserNotificationLogs(u1, time.Time{})
	if err != nil {
		t.Fatalf("GetUserNotificationLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	logEntry := logs[0]
	if !logEntry.Acknowledged {
		t.Error("expected log entry to be acknowledged")
	}
	if logEntry.AcknowledgedAt == nil {
		t.Error("expected non-nil AcknowledgedAt")
	}
	if logEntry.ActorName != "Bob Admin" {
		t.Errorf("expected ActorName 'Bob Admin', got %q", logEntry.ActorName)
	}

	// 7. Test since filter
	recentLogs, err := d.GetUserNotificationLogs(u1, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("GetUserNotificationLogs with since: %v", err)
	}
	if len(recentLogs) != 1 {
		t.Fatalf("expected 1 recent log entry, got %d", len(recentLogs))
	}

	futureLogs, err := d.GetUserNotificationLogs(u1, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("GetUserNotificationLogs with future since: %v", err)
	}
	if len(futureLogs) != 0 {
		t.Fatalf("expected 0 future log entries, got %d", len(futureLogs))
	}
}
