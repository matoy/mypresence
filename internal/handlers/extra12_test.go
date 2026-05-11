package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// ActivityPage — DisableProjects=true path
// -----------------------------------------------------------------------

func TestActivityPage_DisableProjects(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ActivityHandler{DB: d, Render: noRender, DisableProjects: true}

	req := createAdminReq(t, d, http.MethodGet, "/admin/activity", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ActivityPage — team leader accessing different team (teamID override)
// -----------------------------------------------------------------------

func TestActivityPage_TeamLeaderWrongTeam(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ActivityHandler{DB: d, Render: noRender}

	// Create team leader
	uid, _ := d.CreateLocalUser("tlwrongteam@test.com", "TLWrongTeam", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck
	myTeamID, _ := d.CreateTeam("MyTeam")
	d.AddTeamMember(myTeamID, uid) //nolint:errcheck

	otherTeamID, _ := d.CreateTeam("OtherTeam")

	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet,
		"/admin/activity?team="+strconvI64(otherTeamID), nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	_ = myTeamID
	_ = otherTeamID
}

// -----------------------------------------------------------------------
// ActivityPage — team leader with no teams (teamID = 0 fallback)
// -----------------------------------------------------------------------

func TestActivityPage_TeamLeaderNoTeams(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ActivityHandler{DB: d, Render: noRender}

	// Team leader not in any team
	uid, _ := d.CreateLocalUser("tlnoteams@test.com", "TLNoTeams", "password1")
	d.UpdateUserRoles(uid, string(models.RoleTeamLeader)) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	// Request with a specific team they don't belong to
	req := httptest.NewRequest(http.MethodGet, "/admin/activity?team=9999", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ActivityPage — with week view mode
// -----------------------------------------------------------------------

func TestActivityPage_WeekView(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ActivityHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/admin/activity?view=week&year=2026&month=6", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
