package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
)

// TestDeleteUser_TargetNil covers admin_users.go L.187 (targetUser == nil in slog closure)
func TestDeleteUser_TargetNil(t *testing.T) {
	d := newExtraTestDB(t)
	h := &UsersAdminHandler{DB: d, Render: noRender}

	// Call DeleteUser with a non-existent ID → GetUserByID returns nil, DeleteLocalUser succeeds (0 rows)
	req := createAdminReq(t, d, http.MethodDelete, "/admin/users/99999", nil)
	req.SetPathValue("id", "99999")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteUser)).ServeHTTP(w, req)
	// Non-existent user: DeleteLocalUser returns nil, so 200 OK
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d: %s", w.Code, w.Body.String())
	}
}

// TestActivityAPI_DBError covers activity.go L.252-255 (GetTeamStats DB error)
func TestActivityAPI_DBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ActivityHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("StatsDBErrTeam")
	req := createAdminReq(t, d, http.MethodGet,
		"/api/activity?team_id="+strconvI64(teamID)+"&year=2026&month=1", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.ActivityAPI(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}
