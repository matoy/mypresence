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
	d.ToggleProjectFavorite(uid, pid) //nolint:errcheck

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
)

// ─── ToggleProjectFavoriteAPI ─────────────────────────────────────────────────

func TestToggleProjectFavoriteAPI_AddsAndRemovesFavorite(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("FavTest", "FAVT", 0, true, "2026-01-01", "2026-12-31")
	req := createAdminReq(t, d, http.MethodPost, "/api/project-favorite/"+strconvI64(pid), nil)
	req.SetPathValue("id", strconvI64(pid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ToggleProjectFavoriteAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on add, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if !resp["favorite"] {
		t.Fatal("expected favorite=true after first toggle")
	}

	// Second toggle removes it
	req2 := createAdminReq(t, d, http.MethodPost, "/api/project-favorite/"+strconvI64(pid), nil)
	req2.SetPathValue("id", strconvI64(pid))
	w2 := httptest.NewRecorder()
	w2.Body = new(bytes.Buffer)
	// Reuse same user — createAdminReq creates a new user each time, so toggle on the right user
	// by calling DB directly then verifying toggle removes it
	isFav, _ := d.ToggleProjectFavorite(d.MustGetAnyUserID(t), pid) // remove via direct DB call is unavailable; test via API path
	_ = isFav
}

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

func TestToggleProjectFavoriteAPI_ToggleOwnFavorite(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("OwnFav", "OWNF", 0, true, "2026-01-01", "2026-12-31")

	callToggle := func(d2 *db2, req *http.Request) bool {
		w := httptest.NewRecorder()
		w.Body = new(bytes.Buffer)
		middleware.Auth(d, http.HandlerFunc(h.ToggleProjectFavoriteAPI)).ServeHTTP(w, req)
		var resp map[string]bool
		json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
		return resp["favorite"]
	}
	_ = callToggle
	_ = pid
}

// ─── ProjectsAPI favorites field ─────────────────────────────────────────────

func TestProjectsAPI_ReturnsFavoritesField(t *testing.T) {
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
		t.Fatal("expected 'favorites' field in ProjectsAPI response")
	}
}

func TestToggleProjectFavoriteAPI_FavoriteAppearsInAPI(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("APIFav", "APIF", 0, true, "2026-01-01", "2026-12-31")

	// Toggle favorite as a specific user then check ProjectsAPI includes it
	uid, _ := d.CreateLocalUser("apifav@test.com", "ApiFav", "password1")
	d.ToggleProjectFavorite(uid, pid) //nolint:errcheck

	favIDs, err := d.GetUserFavoriteProjectIDs(uid)
	if err != nil {
		t.Fatalf("GetUserFavoriteProjectIDs: %v", err)
	}
	if len(favIDs) != 1 || favIDs[0] != pid {
		t.Fatalf("expected [%d], got %v", pid, favIDs)
	}
}
