package db

import (
	"testing"

	"github.com/matoy/mypresence/internal/models"
)

func TestGetPresenceOverrides_Scenarios(t *testing.T) {
	d := newTestDB(t)

	managerID, _ := d.CreateLocalUser("manager@test.com", "Manager Bob", "pass12345")
	userID, _ := d.CreateLocalUser("user@test.com", "User Alice", "pass12345")
	statusID, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#3b82f6", OnSite: true})

	// 1. Empty user IDs
	emptyOverrides, err := d.GetPresenceOverrides([]int64{}, "2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(emptyOverrides) != 0 {
		t.Fatalf("expected empty map, got %v", emptyOverrides)
	}

	// 2. Manager sets presence for user on 2026-05-10
	_ = d.SetPresences(userID, []string{"2026-05-10"}, statusID, "full")
	_ = d.LogPresenceAction(managerID, userID, "set", []string{"2026-05-10"}, statusID, "full")

	// 3. Manager clears presence for user on 2026-05-11
	_ = d.LogPresenceAction(managerID, userID, "clear", []string{"2026-05-11"}, 0, "full")

	// 4. User sets presence for themselves on 2026-05-12
	_ = d.SetPresences(userID, []string{"2026-05-12"}, statusID, "full")
	_ = d.LogPresenceAction(userID, userID, "set", []string{"2026-05-12"}, statusID, "full")

	// 5. Manager sets presence on 2026-05-13, but user subsequently updates it themselves
	_ = d.LogPresenceAction(managerID, userID, "set", []string{"2026-05-13"}, statusID, "full")
	_ = d.LogPresenceAction(userID, userID, "set", []string{"2026-05-13"}, statusID, "full")

	overrides, err := d.GetPresenceOverrides([]int64{userID}, "2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatalf("GetPresenceOverrides: %v", err)
	}

	userMap := overrides[userID]
	if userMap == nil {
		t.Fatal("expected user map in overrides")
	}

	// Check 2026-05-10: should be overridden with "set" by Manager Bob
	if ov, ok := userMap["2026-05-10"]; !ok {
		t.Error("expected override on 2026-05-10")
	} else {
		if ov.ActorID != managerID {
			t.Errorf("expected ActorID %d, got %d", managerID, ov.ActorID)
		}
		if ov.ActorName != "Manager Bob" {
			t.Errorf("expected ActorName 'Manager Bob', got '%s'", ov.ActorName)
		}
		if ov.Action != "set" {
			t.Errorf("expected Action 'set', got '%s'", ov.Action)
		}
	}

	// Check 2026-05-11: should be overridden with "clear" by Manager Bob
	if ov, ok := userMap["2026-05-11"]; !ok {
		t.Error("expected override on 2026-05-11")
	} else {
		if ov.ActorID != managerID {
			t.Errorf("expected ActorID %d, got %d", managerID, ov.ActorID)
		}
		if ov.Action != "clear" {
			t.Errorf("expected Action 'clear', got '%s'", ov.Action)
		}
	}

	// Check 2026-05-12: self-action -> should NOT be in overrides
	if _, ok := userMap["2026-05-12"]; ok {
		t.Error("2026-05-12 should NOT be in overrides (self-action)")
	}

	// Check 2026-05-13: subsequent self-action -> should NOT be in overrides
	if _, ok := userMap["2026-05-13"]; ok {
		t.Error("2026-05-13 should NOT be in overrides (subsequent self-action cancelled override)")
	}
}
