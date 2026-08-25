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

// CreateHoliday via Auth (covers L.54-57 currentUser != nil log)
func TestCreateHoliday_WithAuth(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}

	body, _ := json.Marshal(map[string]interface{}{
		"date":          "2026-07-14",
		"name":          "Bastille Day",
		"allow_imputed": false,
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/holidays", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// UpdateHoliday DB error (covers L.85-88)
func TestUpdateHoliday_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}

	id, _ := d.CreateHoliday("2026-08-01", "Test Holiday", false)
	body, _ := json.Marshal(map[string]interface{}{
		"date":          "2026-08-01",
		"name":          "Updated Holiday",
		"allow_imputed": true,
	})
	req := createAdminReq(t, d, http.MethodPut, "/admin/holidays/"+strconvI64(id), body)
	req.SetPathValue("id", strconvI64(id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.UpdateHoliday(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// DeleteHoliday via Auth (covers L.109-112 currentUser != nil log)
func TestDeleteHoliday_WithAuth(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}

	id, _ := d.CreateHoliday("2026-09-01", "Auth Delete Holiday", false)
	req := createAdminReq(t, d, http.MethodDelete, "/admin/holidays/"+strconvI64(id), nil)
	req.SetPathValue("id", strconvI64(id))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ImpersonatePost without session cookie (covers L.167-170 in settings.go)
// When the request has no session cookie, adminCookie lookup fails → redirect
func TestImpersonatePost_NoCookie(t *testing.T) {
	d := newExtraTestDB(t)
	h := &SettingsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("nocookie@test.com", "NoCookie", "password1")
	d.UpdateUserRoles(uid, "global") //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	// Create a non-admin target to impersonate
	d.CreateLocalUser("nctarget@test.com", "NCTarget", "password1") //nolint:errcheck

	// Auth middleware needs session cookie to set user in context, then we clear it
	body := bytes.NewBufferString("login=nctarget@test.com")
	req := httptest.NewRequest(http.MethodPost, "/impersonate", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// Remove all cookies so adminCookie fetch fails → L.167-170 covered
		r.Header.Del("Cookie")
		h.ImpersonatePost(rw, r)
	})).ServeHTTP(w, req)
	// Should redirect (302) when no session cookie
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateHoliday_WithCountryCode(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}

	body, _ := json.Marshal(map[string]interface{}{
		"date":          "2026-07-30",
		"name":          "Throne Day",
		"allow_imputed": false,
		"country_code":  "MA",
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/holidays", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateHoliday)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.ID == 0 {
		t.Fatalf("expected holiday id > 0")
	}

	holidays, _ := d.ListHolidays()
	found := false
	for _, hol := range holidays {
		if hol.ID == resp.ID {
			found = true
			if hol.CountryCode != "MA" {
				t.Fatalf("expected country_code MA, got %q", hol.CountryCode)
			}
		}
	}
	if !found {
		t.Fatalf("holiday not found in list")
	}

	// Update country code to multiple: "FR, MA"
	updateBody, _ := json.Marshal(map[string]interface{}{
		"date":          "2026-07-30",
		"name":          "Throne Day Updated",
		"allow_imputed": true,
		"country_code":  "fr, ma",
	})
	upReq := createAdminReq(t, d, http.MethodPut, "/admin/holidays/"+strconvI64(resp.ID), updateBody)
	upReq.SetPathValue("id", strconvI64(resp.ID))
	upReq.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	w2.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateHoliday)).ServeHTTP(w2, upReq)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	holidaysAfter, _ := d.ListHolidays()
	for _, hol := range holidaysAfter {
		if hol.ID == resp.ID {
			if hol.CountryCode != "FR,MA" {
				t.Fatalf("expected country_code FR,MA, got %q", hol.CountryCode)
			}
			if !hol.AllowImputed {
				t.Fatalf("expected allow_imputed true")
			}
		}
	}
}

func TestCalendar_MultiCountryValidation(t *testing.T) {
	d := newExtraTestDB(t)
	calH := &CalendarHandler{DB: d, Render: noRender}

	// User FR
	uFrID, _ := d.CreateLocalUser("user.fr@example.com", "User FR", "pass1234")
	tFrID, _ := d.CreateTeamWithDetails("Team FR", "", false, false, "FR")
	d.AddTeamMember(tFrID, uFrID) //nolint:errcheck

	// User MA
	uMaID, _ := d.CreateLocalUser("user.ma@example.com", "User MA", "pass1234")
	tMaID, _ := d.CreateTeamWithDetails("Team MA", "", false, false, "MA")
	d.AddTeamMember(tMaID, uMaID) //nolint:errcheck

	// User CZ
	uCzID, _ := d.CreateLocalUser("user.cz@example.com", "User CZ", "pass1234")
	tCzID, _ := d.CreateTeamWithDetails("Team CZ", "", false, false, "CZ")
	d.AddTeamMember(tCzID, uCzID) //nolint:errcheck

	// French holiday on 2026-07-14 (non-imputable)
	d.CreateHoliday("2026-07-14", "Bastille Day", false, "FR") //nolint:errcheck
	// Moroccan holiday on 2026-07-30 (non-imputable)
	d.CreateHoliday("2026-07-30", "Throne Day", false, "MA") //nolint:errcheck
	// Shared holiday (FR + MA) on 2026-05-08 (non-imputable)
	d.CreateHoliday("2026-05-08", "Shared Victory Day", false, "FR, MA") //nolint:errcheck

	// Status: On-site
	statusID, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#00ff00", Billable: true, OnSite: true, SortOrder: 1})

	// FR user tries to impute on French holiday 2026-07-14 -> should fail (422)
	frReqBody, _ := json.Marshal(map[string]interface{}{
		"user_id":   uFrID,
		"dates":     []string{"2026-07-14"},
		"status_id": statusID,
		"half":      "full",
	})
	r1 := createAdminReq(t, d, http.MethodPost, "/api/presences", frReqBody)
	r1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	w1.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(calH.SetPresences)).ServeHTTP(w1, r1)
	if w1.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for FR user on French holiday, got %d: %s", w1.Code, w1.Body.String())
	}

	// FR user tries to impute on Moroccan holiday 2026-07-30 -> should SUCCEED (200)
	frReqBody2, _ := json.Marshal(map[string]interface{}{
		"user_id":   uFrID,
		"dates":     []string{"2026-07-30"},
		"status_id": statusID,
		"half":      "full",
	})
	r2 := createAdminReq(t, d, http.MethodPost, "/api/presences", frReqBody2)
	r2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	w2.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(calH.SetPresences)).ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for FR user on Moroccan holiday, got %d: %s", w2.Code, w2.Body.String())
	}

	// MA user tries to impute on Moroccan holiday 2026-07-30 -> should fail (422)
	maReqBody, _ := json.Marshal(map[string]interface{}{
		"user_id":   uMaID,
		"dates":     []string{"2026-07-30"},
		"status_id": statusID,
		"half":      "full",
	})
	r3 := createAdminReq(t, d, http.MethodPost, "/api/presences", maReqBody)
	r3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	w3.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(calH.SetPresences)).ServeHTTP(w3, r3)
	if w3.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for MA user on Moroccan holiday, got %d: %s", w3.Code, w3.Body.String())
	}

	// MA user tries to impute on French holiday 2026-07-14 -> should SUCCEED (200)
	maReqBody2, _ := json.Marshal(map[string]interface{}{
		"user_id":   uMaID,
		"dates":     []string{"2026-07-14"},
		"status_id": statusID,
		"half":      "full",
	})
	r4 := createAdminReq(t, d, http.MethodPost, "/api/presences", maReqBody2)
	r4.Header.Set("Content-Type", "application/json")
	w4 := httptest.NewRecorder()
	w4.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(calH.SetPresences)).ServeHTTP(w4, r4)
	if w4.Code != http.StatusOK {
		t.Fatalf("expected 200 for MA user on French holiday, got %d: %s", w4.Code, w4.Body.String())
	}

	// Shared holiday 2026-05-08 (FR, MA):
	// FR user -> fails (422)
	frReqBodyShared, _ := json.Marshal(map[string]interface{}{
		"user_id":   uFrID,
		"dates":     []string{"2026-05-08"},
		"status_id": statusID,
		"half":      "full",
	})
	r5 := createAdminReq(t, d, http.MethodPost, "/api/presences", frReqBodyShared)
	r5.Header.Set("Content-Type", "application/json")
	w5 := httptest.NewRecorder()
	w5.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(calH.SetPresences)).ServeHTTP(w5, r5)
	if w5.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for FR user on shared holiday, got %d: %s", w5.Code, w5.Body.String())
	}

	// MA user -> fails (422)
	maReqBodyShared, _ := json.Marshal(map[string]interface{}{
		"user_id":   uMaID,
		"dates":     []string{"2026-05-08"},
		"status_id": statusID,
		"half":      "full",
	})
	r6 := createAdminReq(t, d, http.MethodPost, "/api/presences", maReqBodyShared)
	r6.Header.Set("Content-Type", "application/json")
	w6 := httptest.NewRecorder()
	w6.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(calH.SetPresences)).ServeHTTP(w6, r6)
	if w6.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for MA user on shared holiday, got %d: %s", w6.Code, w6.Body.String())
	}

	// CZ user -> SUCCEEDS (200)
	czReqBodyShared, _ := json.Marshal(map[string]interface{}{
		"user_id":   uCzID,
		"dates":     []string{"2026-05-08"},
		"status_id": statusID,
		"half":      "full",
	})
	r7 := createAdminReq(t, d, http.MethodPost, "/api/presences", czReqBodyShared)
	r7.Header.Set("Content-Type", "application/json")
	w7 := httptest.NewRecorder()
	w7.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(calH.SetPresences)).ServeHTTP(w7, r7)
	if w7.Code != http.StatusOK {
		t.Fatalf("expected 200 for CZ user on shared FR+MA holiday, got %d: %s", w7.Code, w7.Body.String())
	}

	// Mixed team (FR, MA):
	uMixedID, _ := d.CreateLocalUser("user.mixed@example.com", "User Mixed", "pass1234")
	tMixedID, _ := d.CreateTeamWithDetails("Team Mixed", "", false, false, "FR, MA")
	d.AddTeamMember(tMixedID, uMixedID) //nolint:errcheck

	// Mixed team on FR-only holiday (2026-07-14) -> SUCCEEDS (200) because not all team countries are on holiday
	mixedReqFR, _ := json.Marshal(map[string]interface{}{
		"user_id":   uMixedID,
		"dates":     []string{"2026-07-14"},
		"status_id": statusID,
		"half":      "full",
	})
	r8 := createAdminReq(t, d, http.MethodPost, "/api/presences", mixedReqFR)
	r8.Header.Set("Content-Type", "application/json")
	w8 := httptest.NewRecorder()
	w8.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(calH.SetPresences)).ServeHTTP(w8, r8)
	if w8.Code != http.StatusOK {
		t.Fatalf("expected 200 for Mixed team user on FR-only holiday, got %d: %s", w8.Code, w8.Body.String())
	}

	// Mixed team on MA-only holiday (2026-07-30) -> SUCCEEDS (200) because not all team countries are on holiday
	mixedReqMA, _ := json.Marshal(map[string]interface{}{
		"user_id":   uMixedID,
		"dates":     []string{"2026-07-30"},
		"status_id": statusID,
		"half":      "full",
	})
	r9 := createAdminReq(t, d, http.MethodPost, "/api/presences", mixedReqMA)
	r9.Header.Set("Content-Type", "application/json")
	w9 := httptest.NewRecorder()
	w9.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(calH.SetPresences)).ServeHTTP(w9, r9)
	if w9.Code != http.StatusOK {
		t.Fatalf("expected 200 for Mixed team user on MA-only holiday, got %d: %s", w9.Code, w9.Body.String())
	}

	// Mixed team on shared FR+MA holiday (2026-05-08) -> FAILS (422) because all team countries are on holiday
	mixedReqShared, _ := json.Marshal(map[string]interface{}{
		"user_id":   uMixedID,
		"dates":     []string{"2026-05-08"},
		"status_id": statusID,
		"half":      "full",
	})
	r10 := createAdminReq(t, d, http.MethodPost, "/api/presences", mixedReqShared)
	r10.Header.Set("Content-Type", "application/json")
	w10 := httptest.NewRecorder()
	w10.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(calH.SetPresences)).ServeHTTP(w10, r10)
	if w10.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for Mixed team user on shared FR+MA holiday, got %d: %s", w10.Code, w10.Body.String())
	}
}
