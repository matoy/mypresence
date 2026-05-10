package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/myPresence/internal/config"
	"github.com/matoy/myPresence/internal/middleware"
	"github.com/matoy/myPresence/internal/models"
)

// -----------------------------------------------------------------------
// SeatsAPI — user is on-site (covers GetSeatsWithStatus path)
// -----------------------------------------------------------------------

func TestSeatsAPI_UserOnSite(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FP OnSite", 0)
	d.CreateSeat(fpID, "OnSite1", 0.3, 0.3) //nolint:errcheck

	uid, _ := d.CreateLocalUser("onsite@test.com", "OnSite", "password1")
	tok, _ := d.CreateSession(uid)

	// Create an on-site status and set presence for user
	status, _ := d.CreateStatus(models.Status{Name: "OnSite Status", Color: "#abc", OnSite: true})
	d.SetPresences(uid, []string{"2026-06-15"}, status, "") //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet,
		"/api/seats?floorplan_id="+strconvI64(fpID)+"&date=2026-06-15", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SeatsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ListFloorplansAPI — floorplans list is nil (empty DB)
// -----------------------------------------------------------------------

func TestListFloorplansAPI_Empty(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodGet, "/api/floorplans", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListFloorplansAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// LocalLogin — admin user in config matches DB user
// -----------------------------------------------------------------------

func TestLocalLogin_AdminConfigMatch(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	uid, _ := d.CreateLocalUser("configadmin@test.com", "ConfigAdmin", "password1")
	_ = uid

	h := &AuthHandler{DB: d, Config: &config.Config{
		AppName:       "Test",
		AdminUser:     "configadmin@test.com",
		AdminPassword: "adminpass123",
	}, Render: noRender}

	form := bytes.NewReader([]byte("username=configadmin@test.com&password=adminpass123"))
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.LocalLogin(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", w.Code, w.Body.String())
	}
}
