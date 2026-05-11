package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
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
