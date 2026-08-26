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

// ─── ToggleFloorplanFavoriteAPI ───────────────────────────────────────────────

func TestToggleFloorplanFavoriteAPI_InvalidID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodPost, "/api/floorplan-favorite/notanid", nil)
	req.SetPathValue("id", "notanid")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ToggleFloorplanFavoriteAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestToggleFloorplanFavoriteAPI_AddsAndRemovesFavorite(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("FavFloor", 0)
	uid, _ := d.CreateLocalUser("fpfavtoggle@test.com", "FpFavToggle", "password1")
	tok, _ := d.CreateSession(uid)

	makeFavReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/floorplan-favorite/"+strconvI64(fpID), nil)
		req.SetPathValue("id", strconvI64(fpID))
		req.AddCookie(&http.Cookie{Name: "session", Value: tok})
		return req
	}

	// First toggle: should add (favorite=true)
	w1 := httptest.NewRecorder()
	w1.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ToggleFloorplanFavoriteAPI)).ServeHTTP(w1, makeFavReq())
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
	middleware.Auth(d, http.HandlerFunc(h.ToggleFloorplanFavoriteAPI)).ServeHTTP(w2, makeFavReq())
	if w2.Code != http.StatusOK {
		t.Fatalf("remove: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var r2 map[string]bool
	json.Unmarshal(w2.Body.Bytes(), &r2) //nolint:errcheck
	if r2["favorite"] {
		t.Fatal("expected favorite=false after second toggle")
	}
}

func TestToggleFloorplanFavoriteAPI_IsolatedPerUser(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("IsoFloor", 0)

	uidA, _ := d.CreateLocalUser("fpfaviso_a@test.com", "IsoA", "password1")
	tokA, _ := d.CreateSession(uidA)
	reqA := httptest.NewRequest(http.MethodPost, "/api/floorplan-favorite/"+strconvI64(fpID), nil)
	reqA.SetPathValue("id", strconvI64(fpID))
	reqA.AddCookie(&http.Cookie{Name: "session", Value: tokA})
	wA := httptest.NewRecorder()
	wA.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ToggleFloorplanFavoriteAPI)).ServeHTTP(wA, reqA)
	if wA.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wA.Code)
	}

	// User B has NOT toggled: their favorites remain empty
	uidB, _ := d.CreateLocalUser("fpfaviso_b@test.com", "IsoB", "password1")
	idsB, _ := d.GetUserFavoriteFloorplanIDs(uidB)
	if len(idsB) != 0 {
		t.Fatalf("user B should have no favorites, got %v", idsB)
	}
	idsA, _ := d.GetUserFavoriteFloorplanIDs(uidA)
	if len(idsA) != 1 || idsA[0] != fpID {
		t.Fatalf("user A should have [%d], got %v", fpID, idsA)
	}
}

// ─── ListFloorplansAPI favorites sorting ──────────────────────────────────────

func TestListFloorplansAPI_FavoritesFirst(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fp1, _ := d.CreateFloorplan("Floor 1", 0)
	fp2, _ := d.CreateFloorplan("Floor 2", 1)
	fp3, _ := d.CreateFloorplan("Floor 3", 2)

	uid, _ := d.CreateLocalUser("fp_sort@test.com", "Fpsort", "password1")
	tok, _ := d.CreateSession(uid)

	// User favorites Floor 3
	_, _ = d.ToggleFloorplanFavorite(uid, fp3)

	req := httptest.NewRequest(http.MethodGet, "/api/floorplans", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListFloorplansAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var fps []models.Floorplan
	if err := json.Unmarshal(w.Body.Bytes(), &fps); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(fps) != 3 {
		t.Fatalf("expected 3 floorplans, got %d", len(fps))
	}
	// First one must be Floor 3 and have IsFavorite=true
	if fps[0].ID != fp3 || !fps[0].IsFavorite {
		t.Errorf("expected fps[0] to be fp3 (favorite), got ID=%d, fav=%v", fps[0].ID, fps[0].IsFavorite)
	}
	if fps[1].ID != fp1 || fps[1].IsFavorite {
		t.Errorf("expected fps[1] to be fp1, got ID=%d, fav=%v", fps[1].ID, fps[1].IsFavorite)
	}
	if fps[2].ID != fp2 || fps[2].IsFavorite {
		t.Errorf("expected fps[2] to be fp2, got ID=%d, fav=%v", fps[2].ID, fps[2].IsFavorite)
	}
}

// ─── FloorplanPage favorites handling ─────────────────────────────────────────

func TestFloorplanPage_FavoritesReflectsUserState(t *testing.T) {
	d := newExtraTestDB(t)
	var renderedPage string
	var renderedData map[string]interface{}
	h := &FloorplanHandler{
		DB:      d,
		DataDir: t.TempDir(),
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			renderedPage = page
			renderedData, _ = data.(map[string]interface{})
		},
	}

	fp1, _ := d.CreateFloorplan("Floor A", 0)
	fp2, _ := d.CreateFloorplan("Floor B", 1)

	uid, _ := d.CreateLocalUser("fp_page_fav@test.com", "FpPageFav", "password1")
	tok, _ := d.CreateSession(uid)

	// User favorites Floor B
	_, _ = d.ToggleFloorplanFavorite(uid, fp2)

	// Visiting /floorplan without floorplan query param: should default to Floor B
	req := httptest.NewRequest(http.MethodGet, "/floorplan", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.FloorplanPage)).ServeHTTP(w, req)

	if renderedPage != "floorplan" {
		t.Fatalf("expected rendered page 'floorplan', got %q", renderedPage)
	}
	currentFP, ok := renderedData["CurrentFP"].(*models.Floorplan)
	if !ok || currentFP == nil {
		t.Fatal("expected non-nil CurrentFP in template data")
	}
	if currentFP.ID != fp2 {
		t.Errorf("expected default floor to be favorite fp2 (%d), got %d", fp2, currentFP.ID)
	}
	if isFav, _ := renderedData["IsFavorite"].(bool); !isFav {
		t.Error("expected IsFavorite to be true for Floor B")
	}

	fps, ok := renderedData["Floorplans"].([]models.Floorplan)
	if !ok || len(fps) != 2 {
		t.Fatalf("expected 2 floorplans in data, got %v", fps)
	}
	if fps[0].ID != fp2 || !fps[0].IsFavorite {
		t.Errorf("expected first floorplan to be fp2 (favorite), got %v", fps[0])
	}
	if fps[1].ID != fp1 || fps[1].IsFavorite {
		t.Errorf("expected second floorplan to be fp1, got %v", fps[1])
	}
}
