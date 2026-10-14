package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

func TestPreprovisionUserAPI(t *testing.T) {
	d := newCRUDTestDB(t)
	uh := &UsersAdminHandler{DB: d}

	// 1. Bad JSON
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/preprovision", bytes.NewReader([]byte("{invalid")))
	uh.PreprovisionUser(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad json, got %d", w.Code)
	}

	// 2. Missing email
	w = httptest.NewRecorder()
	payload, _ := json.Marshal(map[string]interface{}{"email": "notanemail"})
	req = httptest.NewRequest(http.MethodPost, "/api/admin/users/preprovision", bytes.NewReader(payload))
	uh.PreprovisionUser(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid email, got %d", w.Code)
	}

	// Setup site & team
	siteID, err := d.CreateSite(models.Site{Name: "Paris HQ", CountryCode: "FR", Seats: 20})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	teamID, err := d.CreateTeam("Engineering")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// 3. Successful pre-provisioning
	w = httptest.NewRecorder()
	payload, _ = json.Marshal(map[string]interface{}{
		"email":    "paul.valery@corp.com",
		"name":     "Paul Valery",
		"site_id":  siteID,
		"team_ids": []int64{teamID},
	})
	adminReq := createAuthedReq(t, d, http.MethodPost, "/api/admin/users/preprovision", "admin@corp.com", "Admin", "pass", models.RoleGlobal, payload)
	uh.PreprovisionUser(w, adminReq)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		ID     int64        `json:"id"`
		Status string       `json:"status"`
		User   *models.User `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if res.Status != "ok" || res.ID == 0 || res.User == nil {
		t.Fatalf("unexpected response: %+v", res)
	}
	if res.User.Email != "paul.valery@corp.com" {
		t.Errorf("expected email paul.valery@corp.com, got %q", res.User.Email)
	}
	if res.User.SiteID != siteID {
		t.Errorf("expected siteID %d, got %d", siteID, res.User.SiteID)
	}

	// Verify team membership
	members, err := d.GetTeamMembers(teamID)
	if err != nil {
		t.Fatalf("GetTeamMembers: %v", err)
	}
	found := false
	for _, m := range members {
		if m.ID == res.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("user was not added to team %d", teamID)
	}

	// 4. Duplicate pre-provisioning fails with 409
	w = httptest.NewRecorder()
	adminReqDup := createAuthedReq(t, d, http.MethodPost, "/api/admin/users/preprovision", "admin2@corp.com", "Admin2", "pass", models.RoleGlobal, payload)
	uh.PreprovisionUser(w, adminReqDup)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate email, got %d", w.Code)
	}
}

func TestBatchPreprovisionUsersAPI(t *testing.T) {
	d := newCRUDTestDB(t)
	uh := &UsersAdminHandler{DB: d}

	siteID, err := d.CreateSite(models.Site{Name: "Lyon Office", CountryCode: "FR", Seats: 10})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	teamID, err := d.CreateTeam("DevOps")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// 1. Bad JSON
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/batch-preprovision", bytes.NewReader([]byte("{bad")))
	uh.BatchPreprovisionUsers(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad json, got %d", w.Code)
	}

	// 2. Batch with header, CSV, semicolon, comma, and existing user
	_, _ = d.PreprovisionUser("existing@corp.com", "Existing User", 0)

	csvText := fmt.Sprintf(`Email;Name;Site;Team
invalid-line
existing@corp.com;Existing;Lyon Office;DevOps
claire.delacour@corp.com;Claire Delacour;Lyon Office;DevOps
antoine.dupont@corp.com,Antoine Dupont,%d,%d
plain.user@corp.com
`, siteID, teamID)

	payload, _ := json.Marshal(map[string]interface{}{
		"text":            csvText,
		"default_site_id": siteID,
		"default_team_id": teamID,
	})
	adminReq := createAuthedReq(t, d, http.MethodPost, "/api/admin/users/batch-preprovision", "admin@corp.com", "Admin", "pass", models.RoleGlobal, payload)
	w = httptest.NewRecorder()
	uh.BatchPreprovisionUsers(w, adminReq)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var batchRes struct {
		Status  string        `json:"status"`
		Created int           `json:"created"`
		Skipped int           `json:"skipped"`
		Errors  []string      `json:"errors"`
		Users   []models.User `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &batchRes); err != nil {
		t.Fatalf("unmarshal batch response: %v", err)
	}

	if batchRes.Created != 3 {
		t.Errorf("expected 3 created, got %d", batchRes.Created)
	}
	if batchRes.Skipped != 1 {
		t.Errorf("expected 1 skipped (existing@corp.com), got %d", batchRes.Skipped)
	}
	if len(batchRes.Errors) != 1 {
		t.Errorf("expected 1 error for invalid-line, got %d: %v", len(batchRes.Errors), batchRes.Errors)
	}

	// Verify plain.user name deduced and default site/team assigned
	uPlain, err := d.GetUserByEmail("plain.user@corp.com")
	if err != nil {
		t.Fatalf("GetUserByEmail plain.user: %v", err)
	}
	if uPlain.Name != "Plain User" {
		t.Errorf("expected name 'Plain User', got %q", uPlain.Name)
	}
	if uPlain.SiteID != siteID {
		t.Errorf("expected default site %d, got %d", siteID, uPlain.SiteID)
	}
}

func TestPreprovisionTeamMemberAPI(t *testing.T) {
	d := newCRUDTestDB(t)
	ah := &AdminHandler{DB: d}

	teamID, err := d.CreateTeam("Mobile")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// 1. Forbidden when normal basic user tries to add member
	payload, _ := json.Marshal(map[string]interface{}{"email": "dev@corp.com"})
	basicReq := createAuthedReq(t, d, http.MethodPost, fmt.Sprintf("/api/admin/teams/%d/preprovision-member", teamID), "user@corp.com", "User", "pass", models.RoleBasic, payload)
	basicReq.SetPathValue("id", fmt.Sprintf("%d", teamID))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(ah.PreprovisionTeamMember)).ServeHTTP(w, basicReq)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-leader, got %d", w.Code)
	}

	// 2. Make user leader of team
	uLeader, _ := d.GetUserByEmail("user@corp.com")
	_ = d.AddTeamMember(teamID, uLeader.ID)
	_ = d.SetTeamLeaders(teamID, []int64{uLeader.ID})

	tok, err := d.CreateSession(uLeader.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	leaderReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/teams/%d/preprovision-member", teamID), bytes.NewReader(payload))
	leaderReq.Header.Set("Content-Type", "application/json")
	leaderReq.AddCookie(&http.Cookie{Name: "session", Value: tok})
	leaderReq.SetPathValue("id", fmt.Sprintf("%d", teamID))

	// Try again as leader -> Success
	w = httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(ah.PreprovisionTeamMember)).ServeHTTP(w, leaderReq)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for leader, got %d: %s", w.Code, w.Body.String())
	}

	var memberRes struct {
		Status string       `json:"status"`
		User   *models.User `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &memberRes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if memberRes.User == nil || memberRes.User.Email != "dev@corp.com" {
		t.Errorf("expected dev@corp.com user, got %+v", memberRes.User)
	}

	// 3. Preprovision existing member to team
	w = httptest.NewRecorder()
	leaderReq2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/teams/%d/preprovision-member", teamID), bytes.NewReader(payload))
	leaderReq2.Header.Set("Content-Type", "application/json")
	leaderReq2.AddCookie(&http.Cookie{Name: "session", Value: tok})
	leaderReq2.SetPathValue("id", fmt.Sprintf("%d", teamID))
	middleware.Auth(d, http.HandlerFunc(ah.PreprovisionTeamMember)).ServeHTTP(w, leaderReq2)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for already existing user, got %d", w.Code)
	}
}

