package db

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/models"
)

func TestGetTeamsMembersAt_And_GetTeamsStats(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(4)

	// Test with empty slice
	emptyMembers, err := d.GetTeamsMembersAt(nil, "2026-05-01")
	if err != nil {
		t.Fatalf("GetTeamsMembersAt(nil): %v", err)
	}
	if len(emptyMembers) != 0 {
		t.Errorf("expected 0 members for empty teamIDs, got %d", len(emptyMembers))
	}

	emptyStats, err := d.GetTeamsStats(nil, "2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatalf("GetTeamsStats(nil): %v", err)
	}
	if len(emptyStats) != 0 {
		t.Errorf("expected 0 stats for empty teamIDs, got %d", len(emptyStats))
	}

	// Create two teams and users
	teamA, err := d.CreateTeam("Team Alpha")
	if err != nil {
		t.Fatalf("CreateTeam Alpha: %v", err)
	}
	teamB, err := d.CreateTeam("Team Beta")
	if err != nil {
		t.Fatalf("CreateTeam Beta: %v", err)
	}

	u1 := seedUser(t, d, "alice@test.com")
	u2 := seedUser(t, d, "bob@test.com")
	u3 := seedUser(t, d, "charlie@test.com")

	// Alice in Team A and Team B (shared member)
	if err := d.AddTeamMember(teamA, u1); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	if err := d.AddTeamMember(teamB, u1); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}

	// Bob only in Team A
	if err := d.AddTeamMember(teamA, u2); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}

	// Charlie in Team B but left on 2026-04-15
	if err := d.AddTeamMember(teamB, u3); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	leftDate := "2026-04-15"
	if err := d.SetTeamMemberLeftAt(teamB, u3, &leftDate); err != nil {
		t.Fatalf("SetTeamMemberLeftAt: %v", err)
	}

	// For start date 2026-05-01, Charlie should NOT be returned
	membersMay, err := d.GetTeamsMembersAt([]int64{teamA, teamB}, "2026-05-01")
	if err != nil {
		t.Fatalf("GetTeamsMembersAt May: %v", err)
	}
	// Alice and Bob (Alice deduplicated!)
	if len(membersMay) != 2 {
		t.Fatalf("expected 2 distinct members for May, got %d", len(membersMay))
	}

	// Seed presences
	statusID := seedOnSiteStatus(t, d)
	if err := d.SetPresences(u1, []string{"2026-05-10"}, statusID, "full"); err != nil {
		t.Fatalf("SetPresences u1: %v", err)
	}
	if err := d.SetPresences(u2, []string{"2026-05-11"}, statusID, "AM"); err != nil {
		t.Fatalf("SetPresences u2: %v", err)
	}

	stats, err := d.GetTeamsStats([]int64{teamA, teamB}, "2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatalf("GetTeamsStats: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 user stats, got %d", len(stats))
	}

	// Check Alice's stats (1 full day)
	var aliceStats, bobStats *models.UserStats
	for i := range stats {
		switch stats[i].User.ID {
		case u1:
			aliceStats = &stats[i]
		case u2:
			bobStats = &stats[i]
		}
	}
	if aliceStats == nil || aliceStats.BillableDays != 1.0 || aliceStats.OnSiteDays != 1.0 {
		t.Errorf("unexpected alice stats: %+v", aliceStats)
	}
	if bobStats == nil || bobStats.BillableDays != 0.5 || bobStats.OnSiteDays != 0.5 {
		t.Errorf("unexpected bob stats: %+v", bobStats)
	}
}

func TestSessionCache_TTL_And_Invalidation(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(4)

	uid := seedUser(t, d, "session_cache@test.com")
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// 1. Initial retrieval populates cache
	u1, err := d.GetSessionUser(tok)
	if err != nil {
		t.Fatalf("GetSessionUser 1: %v", err)
	}
	if u1.ID != uid {
		t.Fatalf("expected user ID %d, got %d", uid, u1.ID)
	}

	// 2. Cache hit returns same user
	u2, err := d.GetSessionUser(tok)
	if err != nil {
		t.Fatalf("GetSessionUser 2 (cache hit): %v", err)
	}
	if u2.ID != uid {
		t.Fatalf("expected user ID %d from cache, got %d", uid, u2.ID)
	}

	// 3. ClearSessionCache clears cache, re-fetches from DB
	d.ClearSessionCache()
	u3, err := d.GetSessionUser(tok)
	if err != nil {
		t.Fatalf("GetSessionUser 3 after ClearSessionCache: %v", err)
	}
	if u3.ID != uid {
		t.Fatalf("expected user ID %d, got %d", uid, u3.ID)
	}

	// 4. UpdateUserRoles invalidates cache
	if err := d.UpdateUserRoles(uid, models.RoleGlobal); err != nil {
		t.Fatalf("UpdateUserRoles: %v", err)
	}
	uRoles, err := d.GetSessionUser(tok)
	if err != nil {
		t.Fatalf("GetSessionUser after UpdateUserRoles: %v", err)
	}
	if uRoles.Roles != models.RoleGlobal {
		t.Errorf("expected role %q, got %q", models.RoleGlobal, uRoles.Roles)
	}

	// 5. UpdateLocalUser invalidates cache
	if err := d.UpdateLocalUser(uid, "session_updated@test.com", "Updated Name"); err != nil {
		t.Fatalf("UpdateLocalUser: %v", err)
	}
	uUpdated, err := d.GetSessionUser(tok)
	if err != nil {
		t.Fatalf("GetSessionUser after UpdateLocalUser: %v", err)
	}
	if uUpdated.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %q", uUpdated.Name)
	}

	// 6. SetUserDisabled invalidates cache and blocks session
	if err := d.SetUserDisabled(uid, true); err != nil {
		t.Fatalf("SetUserDisabled(true): %v", err)
	}
	if _, err := d.GetSessionUser(tok); err == nil {
		t.Errorf("expected error for disabled user, got nil")
	}

	// Re-enable user
	if err := d.SetUserDisabled(uid, false); err != nil {
		t.Fatalf("SetUserDisabled(false): %v", err)
	}
	if _, err := d.GetSessionUser(tok); err != nil {
		t.Fatalf("GetSessionUser after re-enable: %v", err)
	}

	// 7. CleanExpiredSessions triggers cache clear
	d.CleanExpiredSessions()
	if _, err := d.GetSessionUser(tok); err != nil {
		t.Fatalf("GetSessionUser after CleanExpiredSessions: %v", err)
	}

	// 8. DeleteSession removes session and invalidates cache
	if err := d.DeleteSession(tok); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := d.GetSessionUser(tok); err == nil {
		t.Errorf("expected error for deleted session, got nil")
	}
}

func TestSessionCache_TTL_Expiry(t *testing.T) {
	d := newTestDB(t)
	d.SetBcryptCost(4)

	uid := seedUser(t, d, "ttl_test@test.com")
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	u, err := d.GetSessionUser(tok)
	if err != nil {
		t.Fatalf("GetSessionUser: %v", err)
	}

	sum := sha256.Sum256([]byte(tok))
	tokenHash := hex.EncodeToString(sum[:])
	d.sessionCache.Store(tokenHash, cachedSessionUser{
		user:     *u,
		cachedAt: time.Now().Add(-20 * time.Second),
	})

	// Subsequent GetSessionUser should detect expired TTL, delete it, and query DB cleanly
	u2, err := d.GetSessionUser(tok)
	if err != nil {
		t.Fatalf("GetSessionUser after expired entry: %v", err)
	}
	if u2.ID != uid {
		t.Fatalf("expected user ID %d, got %d", uid, u2.ID)
	}
}
