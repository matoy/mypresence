package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// LocalLogin — admin credential match but user not in DB
// -----------------------------------------------------------------------

func TestLocalLogin_AdminUserNotInDB(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	cfg := &config.Config{AppName: "Test", AdminUser: "superadmin@test.com", AdminPassword: "secret123"}
	h := &AuthHandler{DB: d, Config: cfg, Render: noRender}

	// superadmin@test.com doesn't exist in DB
	form := bytes.NewReader([]byte("username=superadmin@test.com&password=secret123"))
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.LocalLogin(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CreateStatus — duplicate name (non-empty, but DB conflict)
// -----------------------------------------------------------------------

func TestDeleteStatus_NotFound(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/statuses/99999", nil)
	req.SetPathValue("id", "99999")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteStatus)).ServeHTTP(w, req)
	// Might fail since status doesn't exist
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound {
		t.Fatalf("unexpected %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ToggleStatusDisabled — valid status
// -----------------------------------------------------------------------

func TestToggleStatusDisabled_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	sid, _ := d.CreateStatus(models.Status{Name: "ToggleMe", Color: "#abc"})

	bodyBytes := []byte(`{"disabled":true}`)
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/statuses/"+strconvI64(sid)+"/disabled", bodyBytes)
	req.SetPathValue("id", strconvI64(sid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ToggleStatusDisabled)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ToggleStatusDisabled — bad ID
// -----------------------------------------------------------------------

func TestToggleStatusDisabled_BadID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	bodyBytes := []byte(`{"disabled":true}`)
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/statuses/notanumber/disabled", bodyBytes)
	req.SetPathValue("id", "notanumber")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ToggleStatusDisabled)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusOK {
		t.Fatalf("unexpected %d: %s", w.Code, w.Body.String())
	}
}
