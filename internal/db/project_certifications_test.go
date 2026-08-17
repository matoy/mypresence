package db

import "testing"

// TestCertifyProjectMonth_And_IsProjectMonthCertified covers the basic
// certify + lookup flow for project declarations.
func TestCertifyProjectMonth_And_IsProjectMonthCertified(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "projcertify1@test.com")

	certified, err := d.IsProjectMonthCertified(uid, 2026, 6)
	if err != nil {
		t.Fatalf("IsProjectMonthCertified: %v", err)
	}
	if certified {
		t.Fatal("expected not certified before CertifyProjectMonth")
	}

	if err := d.CertifyProjectMonth(uid, 2026, 6, uid); err != nil {
		t.Fatalf("CertifyProjectMonth: %v", err)
	}

	certified, err = d.IsProjectMonthCertified(uid, 2026, 6)
	if err != nil {
		t.Fatalf("IsProjectMonthCertified: %v", err)
	}
	if !certified {
		t.Fatal("expected certified after CertifyProjectMonth")
	}

	// A different month for the same user must remain uncertified.
	certified, err = d.IsProjectMonthCertified(uid, 2026, 7)
	if err != nil {
		t.Fatalf("IsProjectMonthCertified other month: %v", err)
	}
	if certified {
		t.Fatal("expected other month to remain uncertified")
	}
}

// TestCertifyProjectMonth_Idempotent ensures re-certifying an already
// certified month does not error (insertOrIgnore semantics).
func TestCertifyProjectMonth_Idempotent(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "projcertify2@test.com")

	if err := d.CertifyProjectMonth(uid, 2026, 3, uid); err != nil {
		t.Fatalf("first CertifyProjectMonth: %v", err)
	}
	if err := d.CertifyProjectMonth(uid, 2026, 3, uid); err != nil {
		t.Fatalf("second CertifyProjectMonth (idempotent): %v", err)
	}

	certified, err := d.IsProjectMonthCertified(uid, 2026, 3)
	if err != nil {
		t.Fatalf("IsProjectMonthCertified: %v", err)
	}
	if !certified {
		t.Fatal("expected certified")
	}
}

// TestDecertifyProjectMonth ensures decertifying removes the certification
// and is a no-op (no error) when nothing was certified in the first place.
func TestDecertifyProjectMonth(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "projdecertify1@test.com")

	if err := d.CertifyProjectMonth(uid, 2026, 5, uid); err != nil {
		t.Fatalf("CertifyProjectMonth: %v", err)
	}
	if err := d.DecertifyProjectMonth(uid, 2026, 5); err != nil {
		t.Fatalf("DecertifyProjectMonth: %v", err)
	}

	certified, err := d.IsProjectMonthCertified(uid, 2026, 5)
	if err != nil {
		t.Fatalf("IsProjectMonthCertified: %v", err)
	}
	if certified {
		t.Fatal("expected uncertified after DecertifyProjectMonth")
	}

	// Decertifying a month that was never certified must not error.
	if err := d.DecertifyProjectMonth(uid, 2026, 9); err != nil {
		t.Fatalf("DecertifyProjectMonth on uncertified month: %v", err)
	}
}

// TestGetCertifiedProjectUserIDs covers batch lookups across multiple users,
// with an empty input slice and a mix of certified/uncertified/other-month
// users. It also confirms independence from the presence declaration
// certification table.
func TestGetCertifiedProjectUserIDs(t *testing.T) {
	d := newTestDB(t)
	u1 := seedUser(t, d, "projbatch1@test.com")
	u2 := seedUser(t, d, "projbatch2@test.com")
	u3 := seedUser(t, d, "projbatch3@test.com")

	// Empty input short-circuits without querying.
	result, err := d.GetCertifiedProjectUserIDs(nil, 2026, 6)
	if err != nil {
		t.Fatalf("GetCertifiedProjectUserIDs empty: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %v", result)
	}

	if err := d.CertifyProjectMonth(u1, 2026, 6, u1); err != nil {
		t.Fatalf("CertifyProjectMonth u1: %v", err)
	}
	// u2 certified for a different month — must not show up for June.
	if err := d.CertifyProjectMonth(u2, 2026, 7, u2); err != nil {
		t.Fatalf("CertifyProjectMonth u2: %v", err)
	}
	// u3's presence declaration is certified, but not their project one —
	// the two certifications are tracked independently.
	if err := d.CertifyMonth(u3, 2026, 6, u3); err != nil {
		t.Fatalf("CertifyMonth u3: %v", err)
	}

	result, err = d.GetCertifiedProjectUserIDs([]int64{u1, u2, u3}, 2026, 6)
	if err != nil {
		t.Fatalf("GetCertifiedProjectUserIDs: %v", err)
	}
	if len(result) != 1 || !result[u1] {
		t.Fatalf("expected only u1 project-certified for June, got %v", result)
	}
}
