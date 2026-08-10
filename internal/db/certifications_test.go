package db

import "testing"

// TestCertifyMonth_And_IsMonthCertified covers the basic certify + lookup flow.
func TestCertifyMonth_And_IsMonthCertified(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "certify1@test.com")

	certified, err := d.IsMonthCertified(uid, 2026, 6)
	if err != nil {
		t.Fatalf("IsMonthCertified: %v", err)
	}
	if certified {
		t.Fatal("expected not certified before CertifyMonth")
	}

	if err := d.CertifyMonth(uid, 2026, 6, uid); err != nil {
		t.Fatalf("CertifyMonth: %v", err)
	}

	certified, err = d.IsMonthCertified(uid, 2026, 6)
	if err != nil {
		t.Fatalf("IsMonthCertified: %v", err)
	}
	if !certified {
		t.Fatal("expected certified after CertifyMonth")
	}

	// A different month for the same user must remain uncertified.
	certified, err = d.IsMonthCertified(uid, 2026, 7)
	if err != nil {
		t.Fatalf("IsMonthCertified other month: %v", err)
	}
	if certified {
		t.Fatal("expected other month to remain uncertified")
	}
}

// TestCertifyMonth_Idempotent ensures re-certifying an already-certified month
// does not error (insertOrIgnore semantics).
func TestCertifyMonth_Idempotent(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "certify2@test.com")

	if err := d.CertifyMonth(uid, 2026, 3, uid); err != nil {
		t.Fatalf("first CertifyMonth: %v", err)
	}
	if err := d.CertifyMonth(uid, 2026, 3, uid); err != nil {
		t.Fatalf("second CertifyMonth (idempotent): %v", err)
	}

	certified, err := d.IsMonthCertified(uid, 2026, 3)
	if err != nil {
		t.Fatalf("IsMonthCertified: %v", err)
	}
	if !certified {
		t.Fatal("expected certified")
	}
}

// TestDecertifyMonth ensures decertifying removes the certification and is a
// no-op (no error) when nothing was certified in the first place.
func TestDecertifyMonth(t *testing.T) {
	d := newTestDB(t)
	uid := seedUser(t, d, "decertify1@test.com")

	if err := d.CertifyMonth(uid, 2026, 5, uid); err != nil {
		t.Fatalf("CertifyMonth: %v", err)
	}
	if err := d.DecertifyMonth(uid, 2026, 5); err != nil {
		t.Fatalf("DecertifyMonth: %v", err)
	}

	certified, err := d.IsMonthCertified(uid, 2026, 5)
	if err != nil {
		t.Fatalf("IsMonthCertified: %v", err)
	}
	if certified {
		t.Fatal("expected uncertified after DecertifyMonth")
	}

	// Decertifying a month that was never certified must not error.
	if err := d.DecertifyMonth(uid, 2026, 9); err != nil {
		t.Fatalf("DecertifyMonth on uncertified month: %v", err)
	}
}

// TestGetCertifiedUserIDs covers batch lookups across multiple users, with an
// empty input slice and a mix of certified/uncertified/other-month users.
func TestGetCertifiedUserIDs(t *testing.T) {
	d := newTestDB(t)
	u1 := seedUser(t, d, "batch1@test.com")
	u2 := seedUser(t, d, "batch2@test.com")
	u3 := seedUser(t, d, "batch3@test.com")

	// Empty input short-circuits without querying.
	result, err := d.GetCertifiedUserIDs(nil, 2026, 6)
	if err != nil {
		t.Fatalf("GetCertifiedUserIDs empty: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %v", result)
	}

	if err := d.CertifyMonth(u1, 2026, 6, u1); err != nil {
		t.Fatalf("CertifyMonth u1: %v", err)
	}
	// u2 certified for a different month — must not show up for June.
	if err := d.CertifyMonth(u2, 2026, 7, u2); err != nil {
		t.Fatalf("CertifyMonth u2: %v", err)
	}
	// u3 left uncertified entirely.

	result, err = d.GetCertifiedUserIDs([]int64{u1, u2, u3}, 2026, 6)
	if err != nil {
		t.Fatalf("GetCertifiedUserIDs: %v", err)
	}
	if len(result) != 1 || !result[u1] {
		t.Fatalf("expected only u1 certified for June, got %v", result)
	}
}
