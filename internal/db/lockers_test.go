package db

import (
	"errors"
	"testing"

	"github.com/matoy/mypresence/internal/models"
)

func TestCreateAndListLockers(t *testing.T) {
	d := newTestDB(t)

	siteID, err := d.CreateSite(models.Site{Name: "Paris HQ", CountryCode: "FR"})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	uID := seedUser(t, d, "locker_user@test.com")

	l1ID, err := d.CreateLocker(models.Locker{
		SiteID: siteID,
		Number: "A-10",
		Zone:   "RDC",
		Status: "active",
	})
	if err != nil {
		t.Fatalf("CreateLocker 1: %v", err)
	}
	l2ID, err := d.CreateLocker(models.Locker{
		SiteID: siteID,
		Number: "A-2",
		Zone:   "RDC",
		Status: "active",
		UserID: uID,
		Code:   "1234",
		Notes:  "assigned on init",
	})
	if err != nil {
		t.Fatalf("CreateLocker 2: %v", err)
	}

	lockers, err := d.ListLockers(siteID)
	if err != nil {
		t.Fatalf("ListLockers: %v", err)
	}
	if len(lockers) != 2 {
		t.Fatalf("expected 2 lockers, got %d", len(lockers))
	}
	// Natural sort: A-2 must come before A-10
	if lockers[0].ID != l2ID || lockers[1].ID != l1ID {
		t.Fatalf("expected natural sort [A-2, A-10], got [%s, %s]", lockers[0].Number, lockers[1].Number)
	}
	if lockers[0].UserName == "" || lockers[0].Code != "1234" {
		t.Errorf("expected hydrated user and code, got user=%q code=%q", lockers[0].UserName, lockers[0].Code)
	}

	// Verify Site LockerCounts are populated
	site, err := d.GetSite(siteID)
	if err != nil {
		t.Fatalf("GetSite: %v", err)
	}
	if site.LockersCount != 2 || site.AvailableLockersCount != 1 {
		t.Errorf("expected LockersCount=2 AvailableLockersCount=1, got total=%d avail=%d", site.LockersCount, site.AvailableLockersCount)
	}
}

func TestBulkCreateLockers_PaddingAndSkipDuplicates(t *testing.T) {
	d := newTestDB(t)
	siteID, _ := d.CreateSite(models.Site{Name: "Lyon", CountryCode: "FR"})

	_, err := d.CreateLocker(models.Locker{SiteID: siteID, Number: "B-002", Zone: "2e"})
	if err != nil {
		t.Fatalf("seed locker: %v", err)
	}

	created, err := d.BulkCreateLockers(siteID, "B-", 1, 5, 3, "2e")
	if err != nil {
		t.Fatalf("BulkCreateLockers: %v", err)
	}
	if created != 4 {
		t.Fatalf("expected 4 new lockers (B-002 skipped), got %d", created)
	}

	list, err := d.ListLockers(siteID)
	if err != nil {
		t.Fatalf("ListLockers: %v", err)
	}
	if len(list) != 5 {
		t.Fatalf("expected 5 total lockers, got %d", len(list))
	}
	if list[0].Number != "B-001" || list[4].Number != "B-005" {
		t.Errorf("unexpected numbering endpoints: %q .. %q", list[0].Number, list[4].Number)
	}
}

func TestClaimLocker_UpdateCode_AndRelease(t *testing.T) {
	d := newTestDB(t)
	site1, _ := d.CreateSite(models.Site{Name: "Site1", CountryCode: "FR"})
	site2, _ := d.CreateSite(models.Site{Name: "Site2", CountryCode: "FR"})

	u1 := seedUser(t, d, "u1@test.com")
	u2 := seedUser(t, d, "u2@test.com")
	_ = d.UpdateUserSite(u1, site1)
	_ = d.UpdateUserSite(u2, site1)

	l1, _ := d.CreateLocker(models.Locker{SiteID: site1, Number: "01", Zone: "Z1"})
	l2, _ := d.CreateLocker(models.Locker{SiteID: site1, Number: "02", Zone: "Z1"})
	lMaint, _ := d.CreateLocker(models.Locker{SiteID: site1, Number: "03", Status: "maintenance"})
	lOtherSite, _ := d.CreateLocker(models.Locker{SiteID: site2, Number: "01", Zone: "Z2"})

	// Cannot claim maintenance locker
	if err := d.ClaimLocker(lMaint, u1, "9999"); !errors.Is(err, ErrLockerAlreadyTaken) {
		t.Errorf("expected ErrLockerAlreadyTaken for maintenance, got %v", err)
	}

	// u1 cannot claim a locker on site2 (wrong assigned site)
	if err := d.ClaimLocker(lOtherSite, u1, "5555"); !errors.Is(err, ErrLockerWrongSite) {
		t.Errorf("expected ErrLockerWrongSite when claiming on another site, got %v", err)
	}

	// u1 claims l1
	if err := d.ClaimLocker(l1, u1, "4321"); err != nil {
		t.Fatalf("ClaimLocker u1 -> l1: %v", err)
	}

	// u2 cannot claim l1 (already taken)
	if err := d.ClaimLocker(l1, u2, "1111"); !errors.Is(err, ErrLockerAlreadyTaken) {
		t.Errorf("expected ErrLockerAlreadyTaken for u2, got %v", err)
	}

	// u1 cannot claim second locker l2 on same site
	if err := d.ClaimLocker(l2, u1, "0000"); !errors.Is(err, ErrUserAlreadyHasLockerOnSite) {
		t.Errorf("expected ErrUserAlreadyHasLockerOnSite, got %v", err)
	}

	// ListAvailableLockersBySite on site1 only returns l2 and never leaks codes
	avail, err := d.ListAvailableLockersBySite(site1)
	if err != nil {
		t.Fatalf("ListAvailableLockersBySite: %v", err)
	}
	if len(avail) != 1 || avail[0].ID != l2 {
		t.Fatalf("expected only l2 available, got %+v", avail)
	}

	// Update code on l1
	if err := d.UpdateUserLockerCode(l1, u1, "8888"); err != nil {
		t.Fatalf("UpdateUserLockerCode: %v", err)
	}
	// Wrong user cannot update code
	if err := d.UpdateUserLockerCode(l1, u2, "0000"); !errors.Is(err, ErrLockerNotFound) {
		t.Errorf("expected ErrLockerNotFound when wrong user updates code, got %v", err)
	}

	got, _ := d.GetLocker(l1)
	if got.Code != "8888" {
		t.Errorf("expected updated code '8888', got %q", got.Code)
	}

	// Release clears user and assignment date while preserving physical code
	if err := d.ReleaseUserLocker(l1, u1); err != nil {
		t.Fatalf("ReleaseUserLocker: %v", err)
	}
	gotAfter, _ := d.GetLocker(l1)
	if gotAfter.UserID != 0 || gotAfter.AssignedAt != nil || gotAfter.Code != "8888" {
		t.Errorf("expected unassigned locker preserving code '8888', got %+v", gotAfter)
	}

	// u2 claims l1 without specifying a new code -> existing code '8888' is preserved
	if err := d.ClaimLocker(l1, u2, ""); err != nil {
		t.Fatalf("ClaimLocker u2 -> l1 with empty code: %v", err)
	}
	gotReclaimed, _ := d.GetLocker(l1)
	if gotReclaimed.UserID != u2 || gotReclaimed.Code != "8888" {
		t.Errorf("expected u2 holding l1 with preserved code '8888', got %+v", gotReclaimed)
	}
}

func TestFloorplanLockerRanges_CRUD(t *testing.T) {
	d := newTestDB(t)
	fpID, err := d.CreateFloorplan("2nd Floor", 0)
	if err != nil {
		t.Fatalf("CreateFloorplan: %v", err)
	}

	id, err := d.CreateFloorplanLockerRange(models.FloorplanLockerRange{
		FloorplanID: fpID,
		Label:       "001-050",
		Zone:        "North",
		XPct:        25.5,
		YPct:        40.2,
	})
	if err != nil {
		t.Fatalf("CreateFloorplanLockerRange: %v", err)
	}

	list, err := d.ListFloorplanLockerRanges(fpID)
	if err != nil {
		t.Fatalf("ListFloorplanLockerRanges: %v", err)
	}
	if len(list) != 1 || list[0].Label != "001-050" || list[0].Zone != "North" {
		t.Fatalf("unexpected ranges: %+v", list)
	}

	if err := d.UpdateFloorplanLockerRange(models.FloorplanLockerRange{
		ID:    id,
		Label: "001-060",
		Zone:  "North Wing",
		XPct:  30.0,
		YPct:  50.0,
	}); err != nil {
		t.Fatalf("UpdateFloorplanLockerRange: %v", err)
	}

	got, err := d.GetFloorplanLockerRange(id)
	if err != nil {
		t.Fatalf("GetFloorplanLockerRange: %v", err)
	}
	if got.Label != "001-060" || got.XPct != 30.0 {
		t.Errorf("unexpected updated range: %+v", got)
	}

	if err := d.DeleteFloorplanLockerRange(id); err != nil {
		t.Fatalf("DeleteFloorplanLockerRange: %v", err)
	}
	listAfter, _ := d.ListFloorplanLockerRanges(fpID)
	if len(listAfter) != 0 {
		t.Errorf("expected 0 ranges after delete, got %d", len(listAfter))
	}
}

func TestLockerFloorplanLinking_AndAvailabilityCounts(t *testing.T) {
	d := newTestDB(t)
	siteID, _ := d.CreateSite(models.Site{Name: "Paris", CountryCode: "FR"})
	fp1, _ := d.CreateFloorplanWithSite("Floor 1", siteID, 1)
	fp2, _ := d.CreateFloorplanWithSite("Floor 2", siteID, 2)
	uID := seedUser(t, d, "linked@test.com")

	// Floor 1: 001-025 (Vestiaire)
	// Floor 2: A-01..A-10 (Hall Nord)
	_, _ = d.CreateFloorplanLockerRange(models.FloorplanLockerRange{FloorplanID: fp1, Label: "001-025", Zone: "Vestiaire", XPct: 10, YPct: 10})
	_, _ = d.CreateFloorplanLockerRange(models.FloorplanLockerRange{FloorplanID: fp2, Label: "A-01..A-10", Zone: "Hall Nord", XPct: 20, YPct: 20})

	// Lockers on site
	l1, _ := d.CreateLocker(models.Locker{SiteID: siteID, Number: "010", Zone: "Vestiaire"})                   // matches fp1
	l2, _ := d.CreateLocker(models.Locker{SiteID: siteID, Number: "020", Zone: "Vestiaire", UserID: uID})      // assigned, matches fp1
	l3, _ := d.CreateLocker(models.Locker{SiteID: siteID, Number: "A-05", Zone: "Hall Nord"})                  // matches fp2
	_, _ = d.CreateLocker(models.Locker{SiteID: siteID, Number: "B-05", Zone: "Hall Nord"})                   // prefix B doesn't match A-01..A-10!
	_, _ = d.CreateLocker(models.Locker{SiteID: siteID, Number: "015", Zone: "Autre Zone"})                   // different zone doesn't match fp1

	// Verify unit helper cases
	r1 := models.FloorplanLockerRange{Label: "001-025", Zone: "Vestiaire"}
	r2 := models.FloorplanLockerRange{Label: "A-01..A-10", Zone: "Hall Nord"}
	if !LockerMatchesRange(models.Locker{Number: "010", Zone: "Vestiaire"}, r1) {
		t.Error("expected 010 Vestiaire to match r1")
	}
	if LockerMatchesRange(models.Locker{Number: "010", Zone: "Autre"}, r1) {
		t.Error("expected 010 in explicit other zone NOT to match r1")
	}
	if LockerMatchesRange(models.Locker{Number: "B-05", Zone: "Hall Nord"}, r2) {
		t.Error("expected B-05 NOT to match A-01..A-10")
	}
	if !LockerMatchesRange(models.Locker{Number: "A-05", Zone: ""}, r2) {
		t.Error("expected A-05 with empty zone to match A-01..A-10")
	}

	// Verify FloorplanID & FloorplanName hydration on GetLocker, GetUserLockers, ListLockers
	got1, _ := d.GetLocker(l1)
	if got1.FloorplanID != fp1 || got1.FloorplanName != "Floor 1" {
		t.Errorf("expected l1 linked to Floor 1 (%d), got fpID=%d fpName=%q", fp1, got1.FloorplanID, got1.FloorplanName)
	}
	got3, _ := d.GetLocker(l3)
	if got3.FloorplanID != fp2 || got3.FloorplanName != "Floor 2" {
		t.Errorf("expected l3 linked to Floor 2 (%d), got fpID=%d fpName=%q", fp2, got3.FloorplanID, got3.FloorplanName)
	}
	uLockers, _ := d.GetUserLockers(uID)
	if len(uLockers) != 1 || uLockers[0].ID != l2 || uLockers[0].FloorplanID != fp1 {
		t.Errorf("expected GetUserLockers to hydrate FloorplanID=%d, got %+v", fp1, uLockers)
	}

	// Verify ListFloorplanLockerRanges computes TotalCount and AvailableCount
	ranges1, err := d.ListFloorplanLockerRanges(fp1)
	if err != nil || len(ranges1) != 1 {
		t.Fatalf("ListFloorplanLockerRanges fp1: %v %+v", err, ranges1)
	}
	if ranges1[0].TotalCount != 2 || ranges1[0].AvailableCount != 1 || ranges1[0].SiteID != siteID {
		t.Errorf("expected fp1 range Total=2 Avail=1 Site=%d, got %+v", siteID, ranges1[0])
	}

	// Verify GetSiteFloorplanLockerZones returns zones per site
	zonesMap, err := d.GetSiteFloorplanLockerZones()
	if err != nil {
		t.Fatalf("GetSiteFloorplanLockerZones: %v", err)
	}
	if len(zonesMap[siteID]) != 2 {
		t.Errorf("expected 2 floorplan locker zones for site, got %+v", zonesMap[siteID])
	}
}

