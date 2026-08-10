package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// newUserSessionReq builds an authenticated request for an existing user id.
func newUserSessionReq(t *testing.T, d *db.DB, uid int64, method, path string, body []byte) *http.Request {
	t.Helper()
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	return req
}

// weekdayDatesInMonth returns every Mon-Fri date (YYYY-MM-DD) in the month,
// used to fully declare a month so it becomes eligible for certification.
func weekdayDatesInMonth(year, month int) []string {
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	var dates []string
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			dates = append(dates, d.Format("2006-01-02"))
		}
	}
	return dates
}

// -----------------------------------------------------------------------
// CertifyMonth handler
// -----------------------------------------------------------------------

func TestCertifyMonth_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("cert-bad-json-%d@test.com", nextID()), "U", "password1")
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/certify", []byte("not-json"))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CertifyMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCertifyMonth_InvalidRange(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("cert-range-%d@test.com", nextID()), "U", "password1")
	body, _ := json.Marshal(map[string]int{"year": 1999, "month": 6})
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/certify", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CertifyMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range year, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCertifyMonth_IncompleteMonth(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("cert-incomplete-%d@test.com", nextID()), "U", "password1")
	// No presences declared at all for the month.
	body, _ := json.Marshal(map[string]int{"year": 2026, "month": 6})
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/certify", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CertifyMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for incomplete month, got %d: %s", w.Code, w.Body.String())
	}
	certified, err := d.IsMonthCertified(uid, 2026, 6)
	if err != nil {
		t.Fatalf("IsMonthCertified: %v", err)
	}
	if certified {
		t.Fatal("month must not be certified when rejected as incomplete")
	}
}

func TestCertifyMonth_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("cert-ok-%d@test.com", nextID()), "U", "password1")
	statusID, err := d.CreateStatus(models.Status{Name: "Présent", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})
	if err != nil {
		t.Fatalf("CreateStatus: %v", err)
	}
	if err := d.SetPresences(uid, weekdayDatesInMonth(2026, 6), statusID, ""); err != nil {
		t.Fatalf("SetPresences: %v", err)
	}

	body, _ := json.Marshal(map[string]int{"year": 2026, "month": 6})
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/certify", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CertifyMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	certified, err := d.IsMonthCertified(uid, 2026, 6)
	if err != nil {
		t.Fatalf("IsMonthCertified: %v", err)
	}
	if !certified {
		t.Fatal("expected month to be certified after successful call")
	}
}

func TestCertifyMonth_Idempotent(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("cert-idem-%d@test.com", nextID()), "U", "password1")
	statusID, _ := d.CreateStatus(models.Status{Name: "Présent", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})
	if err := d.SetPresences(uid, weekdayDatesInMonth(2026, 6), statusID, ""); err != nil {
		t.Fatalf("SetPresences: %v", err)
	}

	body, _ := json.Marshal(map[string]int{"year": 2026, "month": 6})
	for i := 0; i < 2; i++ {
		req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/certify", body)
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.CertifyMonth)).ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}
}

// -----------------------------------------------------------------------
// DecertifyMonth handler
// -----------------------------------------------------------------------

func TestDecertifyMonth_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}
	req := createAdminReq(t, d, http.MethodPost, "/api/decertify", []byte("not-json"))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DecertifyMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDecertifyMonth_InvalidRange(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}
	body, _ := json.Marshal(map[string]int{"user_id": 1, "year": 1999, "month": 6})
	req := createAdminReq(t, d, http.MethodPost, "/api/decertify", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DecertifyMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range year, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDecertifyMonth_Forbidden(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("decert-basic-%d@test.com", nextID()), "U", "password1")
	body, _ := json.Marshal(map[string]int64{"user_id": uid, "year": 2026, "month": 6})
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/decertify", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DecertifyMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-global-admin, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDecertifyMonth_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("decert-ok-%d@test.com", nextID()), "U", "password1")
	if err := d.CertifyMonth(uid, 2026, 6, uid); err != nil {
		t.Fatalf("CertifyMonth setup: %v", err)
	}

	body, _ := json.Marshal(map[string]int64{"user_id": uid, "year": 2026, "month": 6})
	req := createAdminReq(t, d, http.MethodPost, "/api/decertify", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DecertifyMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	certified, err := d.IsMonthCertified(uid, 2026, 6)
	if err != nil {
		t.Fatalf("IsMonthCertified: %v", err)
	}
	if certified {
		t.Fatal("expected month to be uncertified after decertify")
	}
}

// -----------------------------------------------------------------------
// Edit lock enforcement on SetPresences / ClearPresences
// -----------------------------------------------------------------------

func TestSetPresences_LockedWhenCertified(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("lock-set-%d@test.com", nextID()), "U", "password1")
	statusID, _ := d.CreateStatus(models.Status{Name: "Présent", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})
	if err := d.CertifyMonth(uid, 2026, 6, uid); err != nil {
		t.Fatalf("CertifyMonth setup: %v", err)
	}

	body, _ := json.Marshal(map[string]interface{}{"user_id": uid, "dates": []string{"2026-06-10"}, "status_id": statusID})
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/presences", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusLocked {
		t.Fatalf("expected 423 Locked, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClearPresences_LockedWhenCertified(t *testing.T) {
	d := newExtraTestDB(t)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("lock-clear-%d@test.com", nextID()), "U", "password1")
	statusID, _ := d.CreateStatus(models.Status{Name: "Présent", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})
	if err := d.SetPresences(uid, []string{"2026-06-10"}, statusID, ""); err != nil {
		t.Fatalf("SetPresences setup: %v", err)
	}
	if err := d.CertifyMonth(uid, 2026, 6, uid); err != nil {
		t.Fatalf("CertifyMonth setup: %v", err)
	}

	body, _ := json.Marshal(map[string]interface{}{"user_id": uid, "dates": []string{"2026-06-10"}})
	req := newUserSessionReq(t, d, uid, http.MethodDelete, "/api/presences", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ClearPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusLocked {
		t.Fatalf("expected 423 Locked, got %d: %s", w.Code, w.Body.String())
	}
}
