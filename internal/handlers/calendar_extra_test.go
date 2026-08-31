package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/models"
)

func TestCalendar_DecertifyMonth_AccessControl(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &CalendarHandler{DB: d}

	uID, _ := d.CreateLocalUser("member@example.com", "Member", "pass")
	memberUser, _ := d.GetUserByID(uID)

	tlID, _ := d.CreateLocalUser("leader@example.com", "Leader", "pass")
	tlUser, _ := d.GetUserByID(tlID)

	viewerID, _ := d.CreateLocalUser("viewer@example.com", "Viewer", "pass")
	_ = d.UpdateUserRoles(viewerID, models.RoleActivityViewer)
	viewerUser, _ := d.GetUserByID(viewerID)

	otherID, _ := d.CreateLocalUser("other@example.com", "Other", "pass")
	otherTeamID, _ := d.CreateTeamWithDetails("Other Team", "", false, false)
	_ = d.SetTeamLeaders(otherTeamID, []int64{otherID})
	otherUser, _ := d.GetUserByID(otherID)

	teamID, _ := d.CreateTeamWithDetails("Dev Team", "", false, false)
	_ = d.AddTeamMember(teamID, uID)
	_ = d.AddTeamMember(teamID, tlID)
	_ = d.SetTeamLeaders(teamID, []int64{tlID})

	_ = d.CertifyMonth(uID, 2026, 8, uID)

	// 1. Regular user trying to decertify -> 403
	body, _ := json.Marshal(map[string]interface{}{"user_id": uID, "year": 2026, "month": 8})
	rec := httptest.NewRecorder()
	req := reqWithUser(d, memberUser, http.MethodPost, "/api/decertify", bytes.NewReader(body))
	h.DecertifyMonth(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 on regular member decertifying, got %d", rec.Code)
	}

	// 2. Bad payload (invalid year/month/user) -> 400
	rec = httptest.NewRecorder()
	req = reqWithUser(d, tlUser, http.MethodPost, "/api/decertify", strings.NewReader("bad-json"))
	h.DecertifyMonth(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad json in decertify, got %d", rec.Code)
	}

	// 3. Team leader of DIFFERENT team trying to decertify -> 403
	rec = httptest.NewRecorder()
	req = reqWithUser(d, otherUser, http.MethodPost, "/api/decertify", bytes.NewReader(body))
	h.DecertifyMonth(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 on leader of different team, got %d", rec.Code)
	}

	// 4. Team leader of same team -> 200 OK
	rec = httptest.NewRecorder()
	req = reqWithUser(d, tlUser, http.MethodPost, "/api/decertify", bytes.NewReader(body))
	h.DecertifyMonth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on team leader decertify, got %d: %s", rec.Code, rec.Body.String())
	}

	// 5. Activity viewer -> 200 OK
	_ = d.CertifyMonth(uID, 2026, 8, uID)
	rec = httptest.NewRecorder()
	req = reqWithUser(d, viewerUser, http.MethodPost, "/api/decertify", bytes.NewReader(body))
	h.DecertifyMonth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on activity viewer decertify, got %d: %s", rec.Code, rec.Body.String())
	}
}
