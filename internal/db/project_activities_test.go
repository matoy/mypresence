package db

import (
	"testing"

	"github.com/matoy/mypresence/internal/models"
)

func seedBillableStatus(t *testing.T, d *DB, name string) int64 {
	t.Helper()
	id, err := d.CreateStatus(models.Status{Name: name, Color: "#22c55e", Billable: true, SortOrder: 1})
	if err != nil {
		t.Fatalf("CreateStatus: %v", err)
	}
	return id
}

// ─── GetUserBillableDatesForMonth / GetUserBillableWeightForDate ──────────────

func TestGetUserBillableDatesForMonth_FullAndHalfDays(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "billable1@test.com")
	statusID := seedBillableStatus(t, d, "Billable")

	if err := d.SetPresences(uid, []string{"2026-05-04"}, statusID, "full"); err != nil {
		t.Fatalf("SetPresences full: %v", err)
	}
	if err := d.SetPresences(uid, []string{"2026-05-05"}, statusID, "AM"); err != nil {
		t.Fatalf("SetPresences AM: %v", err)
	}

	weights, err := d.GetUserBillableDatesForMonth(uid, 2026, 5)
	if err != nil {
		t.Fatalf("GetUserBillableDatesForMonth: %v", err)
	}
	if weights["2026-05-04"] != 1.0 {
		t.Errorf("expected full day weight 1.0, got %v", weights["2026-05-04"])
	}
	if weights["2026-05-05"] != 0.5 {
		t.Errorf("expected half day weight 0.5, got %v", weights["2026-05-05"])
	}
	if _, ok := weights["2026-05-06"]; ok {
		t.Error("non-billable date should not appear in the map")
	}
}

func TestGetUserBillableDatesForMonth_BothHalvesBillable_SumsToFull(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "billable2@test.com")
	statusID := seedBillableStatus(t, d, "Billable2")

	if err := d.SetPresences(uid, []string{"2026-05-04"}, statusID, "AM"); err != nil {
		t.Fatalf("SetPresences AM: %v", err)
	}
	if err := d.SetPresences(uid, []string{"2026-05-04"}, statusID, "PM"); err != nil {
		t.Fatalf("SetPresences PM: %v", err)
	}

	weights, err := d.GetUserBillableDatesForMonth(uid, 2026, 5)
	if err != nil {
		t.Fatalf("GetUserBillableDatesForMonth: %v", err)
	}
	if weights["2026-05-04"] != 1.0 {
		t.Errorf("expected AM+PM billable to sum to 1.0, got %v", weights["2026-05-04"])
	}
}

func TestGetUserBillableWeightForDate(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "billable3@test.com")
	statusID := seedBillableStatus(t, d, "Billable3")

	if w, err := d.GetUserBillableWeightForDate(uid, "2026-05-04"); err != nil || w != 0 {
		t.Fatalf("expected 0 weight for unset date, got %v err=%v", w, err)
	}

	if err := d.SetPresences(uid, []string{"2026-05-04"}, statusID, "PM"); err != nil {
		t.Fatalf("SetPresences: %v", err)
	}
	w, err := d.GetUserBillableWeightForDate(uid, "2026-05-04")
	if err != nil {
		t.Fatalf("GetUserBillableWeightForDate: %v", err)
	}
	if w != 0.5 {
		t.Errorf("expected weight 0.5, got %v", w)
	}
}

// ─── Project activity CRUD ────────────────────────────────────────────────────

func TestCreateProjectActivity_And_GetProjectActivity(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "activity1@test.com")

	id, err := d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeJira, "PROJ-1", "Fix bug", "some notes", 60)
	if err != nil || id <= 0 {
		t.Fatalf("CreateProjectActivity: id=%d err=%v", id, err)
	}

	a, err := d.GetProjectActivity(id)
	if err != nil {
		t.Fatalf("GetProjectActivity: %v", err)
	}
	if a.UserID != uid || a.Date != "2026-05-04" || a.ActivityType != models.ActivityTypeJira {
		t.Errorf("unexpected activity: %+v", a)
	}
	if a.JiraKey != "PROJ-1" || a.JiraTitle != "Fix bug" || a.Comment != "some notes" || a.Percentage != 60 {
		t.Errorf("unexpected activity fields: %+v", a)
	}
}

func TestGetProjectActivity_NotFound(t *testing.T) {
	d := newTestDB(t)
	if _, err := d.GetProjectActivity(99999); err == nil {
		t.Error("expected error for non-existent activity")
	}
}

func TestUpdateProjectActivity_ChangesFields(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "activity2@test.com")
	id, _ := d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "old comment", 30)

	if err := d.UpdateProjectActivity(id, models.ActivityTypeServiceNow, "", "", "new comment", 70); err != nil {
		t.Fatalf("UpdateProjectActivity: %v", err)
	}
	a, _ := d.GetProjectActivity(id)
	if a.ActivityType != models.ActivityTypeServiceNow || a.Comment != "new comment" || a.Percentage != 70 {
		t.Errorf("unexpected updated activity: %+v", a)
	}
}

func TestDeleteProjectActivity_RemovesRow(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "activity3@test.com")
	id, _ := d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 50)

	if err := d.DeleteProjectActivity(id); err != nil {
		t.Fatalf("DeleteProjectActivity: %v", err)
	}
	if _, err := d.GetProjectActivity(id); err == nil {
		t.Error("expected error after deletion")
	}
}

func TestGetUserActivitiesTotalForDate_SumsExcludingGivenID(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "activity4@test.com")
	id1, _ := d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 40)
	id2, _ := d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 30)

	total, err := d.GetUserActivitiesTotalForDate(uid, "2026-05-04", 0)
	if err != nil {
		t.Fatalf("GetUserActivitiesTotalForDate: %v", err)
	}
	if total != 70 {
		t.Errorf("expected total 70, got %v", total)
	}

	totalExcl, err := d.GetUserActivitiesTotalForDate(uid, "2026-05-04", id1)
	if err != nil {
		t.Fatalf("GetUserActivitiesTotalForDate excl: %v", err)
	}
	if totalExcl != 30 {
		t.Errorf("expected total excluding id1 to be 30, got %v", totalExcl)
	}
	_ = id2
}

func TestListUserActivitiesForMonth_FiltersByMonth(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "activity5@test.com")
	d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 50) //nolint:errcheck
	d.CreateProjectActivity(uid, "2026-06-01", models.ActivityTypeOther, "", "", "", 50) //nolint:errcheck

	activities, err := d.ListUserActivitiesForMonth(uid, 2026, 5)
	if err != nil {
		t.Fatalf("ListUserActivitiesForMonth: %v", err)
	}
	if len(activities) != 1 || activities[0].Date != "2026-05-04" {
		t.Errorf("expected 1 activity in May, got %+v", activities)
	}
}

func TestGetActivitiesForUsersMonth_EmptyUserIDs(t *testing.T) {
	d := newTestDB(t)
	activities, err := d.GetActivitiesForUsersMonth(nil, 2026, 5)
	if err != nil {
		t.Fatalf("GetActivitiesForUsersMonth: %v", err)
	}
	if len(activities) != 0 {
		t.Errorf("expected no activities, got %d", len(activities))
	}
}

func TestGetActivitiesForUsersMonth_MultipleUsers(t *testing.T) {
	d := newTestDB(t)
	uid1 := seedUser(t, d, "activity6a@test.com")
	uid2 := seedUser(t, d, "activity6b@test.com")
	uid3 := seedUser(t, d, "activity6c@test.com")
	d.CreateProjectActivity(uid1, "2026-05-04", models.ActivityTypeOther, "", "", "", 50) //nolint:errcheck
	d.CreateProjectActivity(uid2, "2026-05-05", models.ActivityTypeOther, "", "", "", 50) //nolint:errcheck
	d.CreateProjectActivity(uid3, "2026-05-06", models.ActivityTypeOther, "", "", "", 50) //nolint:errcheck

	activities, err := d.GetActivitiesForUsersMonth([]int64{uid1, uid2}, 2026, 5)
	if err != nil {
		t.Fatalf("GetActivitiesForUsersMonth: %v", err)
	}
	if len(activities) != 2 {
		t.Fatalf("expected 2 activities, got %d", len(activities))
	}
	for _, a := range activities {
		if a.UserID == uid3 {
			t.Error("activity for uid3 should not be included")
		}
	}
}
