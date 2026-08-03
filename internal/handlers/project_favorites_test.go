package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
)

// ─── ToggleProjectFavoriteAPI ─────────────────────────────────────────────────

func TestToggleProjectFavoriteAPI_InvalidID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodPost, "/api/project-favorite/notanid", nil)
	req.SetPathValue("id", "notanid")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ToggleProjectFavoriteAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestToggleProjectFavoriteAPI_AddsAndRemovesFavorite(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("FavTest", "FAVT", 0, true, "2026-01-01", "2026-12-31")
	uid, _ := d.CreateLocalUser("favtoggle@test.com", "FavToggle", "password1")
	tok, _ := d.CreateSession(uid)

	makeFavReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/project-favorite/"+strconvI64(pid), nil)
		req.SetPathValue("id", strconvI64(pid))
		req.AddCookie(&http.Cookie{Name: "session", Value: tok})
		return req
	}

	// First toggle: should add (favorite=true)
	w1 := httptest.NewRecorder()
	w1.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ToggleProjectFavoriteAPI)).ServeHTTP(w1, makeFavReq())
	if w1.Code != http.StatusOK {
		t.Fatalf("add: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}
	var r1 map[string]bool
	json.Unmarshal(w1.Body.Bytes(), &r1) //nolint:errcheck
	if !r1["favorite"] {
		t.Fatal("expected favorite=true after first toggle")
	}

	// Second toggle: should remove (favorite=false)
	w2 := httptest.NewRecorder()
	w2.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ToggleProjectFavoriteAPI)).ServeHTTP(w2, makeFavReq())
	if w2.Code != http.StatusOK {
		t.Fatalf("remove: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var r2 map[string]bool
	json.Unmarshal(w2.Body.Bytes(), &r2) //nolint:errcheck
	if r2["favorite"] {
		t.Fatal("expected favorite=false after second toggle")
	}
}

func TestToggleProjectFavoriteAPI_IsolatedPerUser(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("IsoFav", "ISOF", 0, true, "2026-01-01", "2026-12-31")

	uidA, _ := d.CreateLocalUser("faviso_a@test.com", "IsoA", "password1")
	tokA, _ := d.CreateSession(uidA)
	reqA := httptest.NewRequest(http.MethodPost, "/api/project-favorite/"+strconvI64(pid), nil)
	reqA.SetPathValue("id", strconvI64(pid))
	reqA.AddCookie(&http.Cookie{Name: "session", Value: tokA})
	wA := httptest.NewRecorder()
	wA.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ToggleProjectFavoriteAPI)).ServeHTTP(wA, reqA)
	if wA.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wA.Code)
	}

	// User B has NOT toggled: their favorites remain empty
	uidB, _ := d.CreateLocalUser("faviso_b@test.com", "IsoB", "password1")
	idsB, _ := d.GetUserFavoriteProjectIDs(uidB)
	if len(idsB) != 0 {
		t.Fatalf("user B should have no favorites, got %v", idsB)
	}
	idsA, _ := d.GetUserFavoriteProjectIDs(uidA)
	if len(idsA) != 1 || idsA[0] != pid {
		t.Fatalf("user A should have [%d], got %v", pid, idsA)
	}
}

// ─── ProjectsAPI favorites field ─────────────────────────────────────────────

func TestProjectsAPI_IncludesFavoritesField(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/api/projects?year=2026&month=8", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := resp["favorites"]; !ok {
		t.Fatal("expected 'favorites' key in ProjectsAPI response")
	}
}

func TestProjectsAPI_FavoritesReflectsUserState(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("APIFav", "APIF", 0, true, "2026-01-01", "2026-12-31")
	uid, _ := d.CreateLocalUser("apifav@test.com", "ApiFav", "password1")
	tok, _ := d.CreateSession(uid)
	_, _ = d.ToggleProjectFavorite(uid, pid)

	req := httptest.NewRequest(http.MethodGet, "/api/projects?year=2026&month=8", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	favs, _ := resp["favorites"].([]interface{})
	if len(favs) != 1 {
		t.Fatalf("expected 1 favorite in response, got %v", favs)
	}
}
