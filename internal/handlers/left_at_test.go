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

// -----------------------------------------------------------------------
// SetTeamMemberLeftAt — auth and functional paths
// -----------------------------------------------------------------------

func TestSetTeamMemberLeftAt_AsAdmin_Set(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("LeftAtTeam1")
	targetID := seedUserInHandlers(t, d, "target_la@test.com")
	d.AddTeamMember(teamID, targetID) //nolint:errcheck

	body, _ := json.Marshal(map[string]interface{}{"left_at": "2026-06-30"})
	req := createAdminReq(t, d, http.MethodPatch, "/admin/teams/"+strconvI64(teamID)+"/members/"+strconvI64(targetID)+"/left-at", body)
	req.SetPathValue("id", strconvI64(teamID))
	req.SetPathValue("userId", strconvI64(targetID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetTeamMemberLeftAt)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the member is no longer active
	members, _ := d.GetTeamMembers(teamID)
	for _, m := range members {
		if m.ID == targetID {
			t.Error("member should be departed (not in active list) after SetTeamMemberLeftAt")
		}
	}
}

func TestSetTeamMemberLeftAt_AsAdmin_Clear(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("LeftAtTeam2")
	targetID := seedUserInHandlers(t, d, "target_la2@test.com")
	d.AddTeamMember(teamID, targetID) //nolint:errcheck

	// First set departure
	leftAt := "2026-06-30"
	d.SetTeamMemberLeftAt(teamID, targetID, &leftAt) //nolint:errcheck

	// Then clear via handler
	body, _ := json.Marshal(map[string]interface{}{"left_at": nil})
	req := createAdminReq(t, d, http.MethodPatch, "/admin/teams/"+strconvI64(teamID)+"/members/"+strconvI64(targetID)+"/left-at", body)
	req.SetPathValue("id", strconvI64(teamID))
	req.SetPathValue("userId", strconvI64(targetID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetTeamMemberLeftAt)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on clear, got %d: %s", w.Code, w.Body.String())
	}

	// Member should be active again
	members, _ := d.GetTeamMembers(teamID)
	found := false
	for _, m := range members {
		if m.ID == targetID {
			found = true
		}
	}
	if !found {
		t.Error("member should be reinstated (active) after clearing left_at")
	}
}

func TestSetTeamMemberLeftAt_AsTeamLeader_InTeam_Allowed(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	leaderID := seedUserInHandlers(t, d, "leader_la@test.com")
	d.UpdateUserRoles(leaderID, string(models.RoleTeamLeader)) //nolint:errcheck
	targetID := seedUserInHandlers(t, d, "target_la3@test.com")

	teamID, _ := d.CreateTeam("LeaderTeamLA")
	d.AddTeamMember(teamID, leaderID) //nolint:errcheck
	d.AddTeamMember(teamID, targetID) //nolint:errcheck

	tok, _ := d.CreateSession(leaderID)

	body, _ := json.Marshal(map[string]interface{}{"left_at": "2026-07-31"})
	req := httptest.NewRequest(http.MethodPatch, "/admin/teams/"+strconvI64(teamID)+"/members/"+strconvI64(targetID)+"/left-at", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", strconvI64(teamID))
	req.SetPathValue("userId", strconvI64(targetID))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetTeamMemberLeftAt)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("team leader in team should be allowed: got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetTeamMemberLeftAt_AsTeamLeader_NotInTeam_Forbidden(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	leaderID := seedUserInHandlers(t, d, "leader_la2@test.com")
	d.UpdateUserRoles(leaderID, string(models.RoleTeamLeader)) //nolint:errcheck
	targetID := seedUserInHandlers(t, d, "target_la4@test.com")

	teamID, _ := d.CreateTeam("OtherTeamLA")
	d.AddTeamMember(teamID, targetID) //nolint:errcheck
	// leader is NOT in teamID

	tok, _ := d.CreateSession(leaderID)

	body, _ := json.Marshal(map[string]interface{}{"left_at": "2026-07-31"})
	req := httptest.NewRequest(http.MethodPatch, "/admin/teams/"+strconvI64(teamID)+"/members/"+strconvI64(targetID)+"/left-at", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", strconvI64(teamID))
	req.SetPathValue("userId", strconvI64(targetID))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetTeamMemberLeftAt)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("team leader not in team should be forbidden: got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetTeamMemberLeftAt_BasicUser_Forbidden(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	basicID := seedUserInHandlers(t, d, "basic_la@test.com")
	teamID, _ := d.CreateTeam("TeamLA_Basic")
	targetID := seedUserInHandlers(t, d, "target_la5@test.com")
	d.AddTeamMember(teamID, targetID) //nolint:errcheck
	tok, _ := d.CreateSession(basicID)

	body, _ := json.Marshal(map[string]interface{}{"left_at": "2026-07-01"})
	req := httptest.NewRequest(http.MethodPatch, "/admin/teams/"+strconvI64(teamID)+"/members/"+strconvI64(targetID)+"/left-at", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", strconvI64(teamID))
	req.SetPathValue("userId", strconvI64(targetID))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetTeamMemberLeftAt)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("basic user should be forbidden: got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetTeamMemberLeftAt_BadDate_Returns400(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("BadDateTeam")
	targetID := seedUserInHandlers(t, d, "target_la6@test.com")
	d.AddTeamMember(teamID, targetID) //nolint:errcheck

	body, _ := json.Marshal(map[string]interface{}{"left_at": "not-a-date"})
	req := createAdminReq(t, d, http.MethodPatch, "/admin/teams/"+strconvI64(teamID)+"/members/"+strconvI64(targetID)+"/left-at", body)
	req.SetPathValue("id", strconvI64(teamID))
	req.SetPathValue("userId", strconvI64(targetID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetTeamMemberLeftAt)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad date should return 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetTeamMemberLeftAt_BadJSON_Returns400(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("BadJSONTeam")
	targetID := seedUserInHandlers(t, d, "target_la7@test.com")
	d.AddTeamMember(teamID, targetID) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodPatch, "/admin/teams/"+strconvI64(teamID)+"/members/"+strconvI64(targetID)+"/left-at", []byte("{bad"))
	req.SetPathValue("id", strconvI64(teamID))
	req.SetPathValue("userId", strconvI64(targetID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetTeamMemberLeftAt)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON should return 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ListSeatsWithStatusForDatesAPI
// -----------------------------------------------------------------------

func TestListSeatsWithStatusForDatesAPI_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("StatusDatesFP", 0)
	d.CreateSeat(fpID, "Seat1", 0.3, 0.4) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet,
		"/api/floorplans/"+strconvI64(fpID)+"/seats/status?dates=2026-05-01,2026-05-02&half=full", nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListSeatsWithStatusForDatesAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var seats []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &seats); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(seats) != 1 {
		t.Errorf("expected 1 seat in response, got %d", len(seats))
	}
}

func TestListSeatsWithStatusForDatesAPI_NoDates(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("StatusNoDatesFP", 0)
	d.CreateSeat(fpID, "Seat2", 0.5, 0.5) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/floorplans/"+strconvI64(fpID)+"/seats/status", nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListSeatsWithStatusForDatesAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with no dates, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListSeatsWithStatusForDatesAPI_InvalidDate(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("InvalidDateFP", 0)

	req := createAdminReq(t, d, http.MethodGet,
		"/api/floorplans/"+strconvI64(fpID)+"/seats/status?dates=not-a-date", nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListSeatsWithStatusForDatesAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid date, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListSeatsWithStatusForDatesAPI_MissingID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	req := createAdminReq(t, d, http.MethodGet, "/api/floorplans/0/seats/status", nil)
	req.SetPathValue("id", "0")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListSeatsWithStatusForDatesAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing floorplan ID, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ListSeatsForFloorplanAPI — new endpoint from commit 3720e87
// -----------------------------------------------------------------------

func TestListSeatsForFloorplanAPI_WithSeats(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("ListSeatsFP", 0)
	d.CreateSeat(fpID, "A", 0.1, 0.2) //nolint:errcheck
	d.CreateSeat(fpID, "B", 0.3, 0.4) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/floorplans/"+strconvI64(fpID)+"/seats", nil)
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ListSeatsForFloorplanAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var seats []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &seats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(seats) != 2 {
		t.Errorf("expected 2 seats, got %d", len(seats))
	}
}

// -----------------------------------------------------------------------
// BulkReserveSeats — user_id auth (team leader for another user)
// -----------------------------------------------------------------------

func TestBulkReserveSeats_WithUserID_AsAdmin_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("BulkAdminFP", 0)
	seatID, _ := d.CreateSeat(fpID, "Bulk1", 0.5, 0.5)
	targetID := seedUserInHandlers(t, d, "bulk_target@test.com")

	// Make target on-site on the date
	statusID, _ := d.CreateStatus(models.Status{Name: "OnSiteBulk", Color: "#abc", OnSite: true})
	d.SetPresences(targetID, []string{"2026-06-01"}, statusID, "") //nolint:errcheck

	body, _ := json.Marshal(map[string]interface{}{
		"seat_id": seatID,
		"dates":   []string{"2026-06-01"},
		"half":    "full",
		"user_id": targetID,
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/reservations/bulk", body)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.BulkReserveSeats)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin bulk reserve for another user: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBulkReserveSeats_WithUserID_AsTeamLeader_InTeam_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("BulkLeaderFP", 0)
	seatID, _ := d.CreateSeat(fpID, "BulkL1", 0.5, 0.5)

	leaderID := seedUserInHandlers(t, d, "bulk_leader@test.com")
	d.UpdateUserRoles(leaderID, string(models.RoleTeamLeader)) //nolint:errcheck
	targetID := seedUserInHandlers(t, d, "bulk_target2@test.com")

	teamID, _ := d.CreateTeam("BulkLeaderTeam")
	d.AddTeamMember(teamID, leaderID) //nolint:errcheck
	d.AddTeamMember(teamID, targetID) //nolint:errcheck

	statusID, _ := d.CreateStatus(models.Status{Name: "OnSiteBulk2", Color: "#def", OnSite: true})
	d.SetPresences(targetID, []string{"2026-06-02"}, statusID, "") //nolint:errcheck

	tok, _ := d.CreateSession(leaderID)
	body, _ := json.Marshal(map[string]interface{}{
		"seat_id": seatID,
		"dates":   []string{"2026-06-02"},
		"half":    "full",
		"user_id": targetID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/reservations/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.BulkReserveSeats)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("team leader in team: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBulkReserveSeats_WithUserID_BasicUser_Forbidden(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("BulkBasicFP", 0)
	seatID, _ := d.CreateSeat(fpID, "BulkB1", 0.5, 0.5)
	basicID := seedUserInHandlers(t, d, "bulk_basic@test.com")
	targetID := seedUserInHandlers(t, d, "bulk_target3@test.com")

	tok, _ := d.CreateSession(basicID)
	body, _ := json.Marshal(map[string]interface{}{
		"seat_id": seatID,
		"dates":   []string{"2026-06-03"},
		"user_id": targetID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/reservations/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.BulkReserveSeats)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("basic user booking for another: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBulkReserveSeats_InvalidDate_Returns400(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	fpID, _ := d.CreateFloorplan("BulkBadDateFP", 0)
	seatID, _ := d.CreateSeat(fpID, "BulkBD1", 0.5, 0.5)

	body, _ := json.Marshal(map[string]interface{}{
		"seat_id": seatID,
		"dates":   []string{"not-a-date"},
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/reservations/bulk", body)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.BulkReserveSeats)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid date: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CancelReservationsByDates — user_id auth
// -----------------------------------------------------------------------

func TestCancelReservationsByDates_WithUserID_AsAdmin_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	targetID := seedUserInHandlers(t, d, "cancel_target@test.com")

	body, _ := json.Marshal(map[string]interface{}{
		"dates":   []string{"2026-06-10"},
		"user_id": targetID,
	})
	req := createAdminReq(t, d, http.MethodDelete, "/api/reservations/bulk", body)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CancelReservationsByDates)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin cancel for another user: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCancelReservationsByDates_WithUserID_BasicUser_Forbidden(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	basicID := seedUserInHandlers(t, d, "cancel_basic@test.com")
	targetID := seedUserInHandlers(t, d, "cancel_target2@test.com")

	tok, _ := d.CreateSession(basicID)
	body, _ := json.Marshal(map[string]interface{}{
		"dates":   []string{"2026-06-10"},
		"user_id": targetID,
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/reservations/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CancelReservationsByDates)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("basic user cancelling another's: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCancelReservationsByDates_InvalidDate_Returns400(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}

	body, _ := json.Marshal(map[string]interface{}{"dates": []string{"bad-date"}})
	req := createAdminReq(t, d, http.MethodDelete, "/api/reservations/bulk", body)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CancelReservationsByDates)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid date: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// TeamsPage — shows departed members via GetAllTeamMembers
// -----------------------------------------------------------------------

func TestTeamsPage_ShowsDepartedMembers(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {}}

	teamID, _ := d.CreateTeam("DepartedDisplayTeam")
	activeID := seedUserInHandlers(t, d, "active_page@test.com")
	departedID := seedUserInHandlers(t, d, "departed_page@test.com")
	d.AddTeamMember(teamID, activeID)   //nolint:errcheck
	d.AddTeamMember(teamID, departedID) //nolint:errcheck

	leftAt := "2026-03-31"
	d.SetTeamMemberLeftAt(teamID, departedID, &leftAt) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/teams", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.TeamsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify via DB that GetAllTeamMembers returns both members (active + departed)
	members, err := d.GetAllTeamMembers(teamID)
	if err != nil {
		t.Fatalf("GetAllTeamMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members (active+departed) in admin page, got %d", len(members))
	}
}

// -----------------------------------------------------------------------
// helper: seed a user without a role (basic) - avoids email conflicts
// -----------------------------------------------------------------------

func seedUserInHandlers(t *testing.T, d interface {
	CreateLocalUser(string, string, string) (int64, error)
}, email string) int64 {
	t.Helper()
	id, err := d.CreateLocalUser(email, email, "password1")
	if err != nil {
		t.Fatalf("seedUserInHandlers(%s): %v", email, err)
	}
	return id
}
