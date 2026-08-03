package db

import (
	"testing"
)

// ─── project_favorites ────────────────────────────────────────────────────────

func TestGetUserFavoriteProjectIDs_EmptyByDefault(t *testing.T) {
	d := newTestDB(t)
	uid, _ := d.CreateLocalUser("fav1@test.com", "Fav1", "pass")

	ids, err := d.GetUserFavoriteProjectIDs(uid)
	if err != nil {
		t.Fatalf("GetUserFavoriteProjectIDs: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 favorites, got %d", len(ids))
	}
}

func TestToggleProjectFavorite_AddsFavorite(t *testing.T) {
	d := newTestDB(t)
	pid := seedProject(t, d, "FavProj", "FAVP")
	uid, _ := d.CreateLocalUser("fav2@test.com", "Fav2", "pass")

	isFav, err := d.ToggleProjectFavorite(uid, pid)
	if err != nil {
		t.Fatalf("ToggleProjectFavorite: %v", err)
	}
	if !isFav {
		t.Fatal("expected isFav=true after first toggle")
	}
	ids, _ := d.GetUserFavoriteProjectIDs(uid)
	if len(ids) != 1 || ids[0] != pid {
		t.Fatalf("expected [%d], got %v", pid, ids)
	}
}

func TestToggleProjectFavorite_RemovesFavorite(t *testing.T) {
	d := newTestDB(t)
	pid := seedProject(t, d, "FavProj2", "FVP2")
	uid, _ := d.CreateLocalUser("fav3@test.com", "Fav3", "pass")

	_, _ = d.ToggleProjectFavorite(uid, pid) // add
	isFav, err := d.ToggleProjectFavorite(uid, pid)
	if err != nil {
		t.Fatalf("ToggleProjectFavorite: %v", err)
	}
	if isFav {
		t.Fatal("expected isFav=false after second toggle")
	}
	ids, _ := d.GetUserFavoriteProjectIDs(uid)
	if len(ids) != 0 {
		t.Fatalf("expected empty favorites after removal, got %v", ids)
	}
}

func TestGetUserFavoriteProjectIDs_MultipleProjects(t *testing.T) {
	d := newTestDB(t)
	p1 := seedProject(t, d, "ProjA", "PA")
	p2 := seedProject(t, d, "ProjB", "PB")
	p3 := seedProject(t, d, "ProjC", "PC")
	uid, _ := d.CreateLocalUser("fav4@test.com", "Fav4", "pass")

	_, _ = d.ToggleProjectFavorite(uid, p1)
	_, _ = d.ToggleProjectFavorite(uid, p3)

	ids, err := d.GetUserFavoriteProjectIDs(uid)
	if err != nil {
		t.Fatalf("GetUserFavoriteProjectIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 favorites, got %d", len(ids))
	}
	set := map[int64]bool{ids[0]: true, ids[1]: true}
	if !set[p1] || !set[p3] || set[p2] {
		t.Fatalf("expected {p1, p3}, got %v", ids)
	}
}

func TestGetUserFavoriteProjectIDs_IsolatedPerUser(t *testing.T) {
	d := newTestDB(t)
	pid := seedProject(t, d, "Shared", "SHRD")
	uid1, _ := d.CreateLocalUser("fav5a@test.com", "Fav5a", "pass")
	uid2, _ := d.CreateLocalUser("fav5b@test.com", "Fav5b", "pass")

	_, _ = d.ToggleProjectFavorite(uid1, pid)

	ids1, _ := d.GetUserFavoriteProjectIDs(uid1)
	ids2, _ := d.GetUserFavoriteProjectIDs(uid2)
	if len(ids1) != 1 {
		t.Fatalf("user1: expected 1 favorite, got %d", len(ids1))
	}
	if len(ids2) != 0 {
		t.Fatalf("user2: expected 0 favorites, got %d", len(ids2))
	}
}
