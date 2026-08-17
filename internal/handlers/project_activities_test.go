package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// ─── resolveManualTeam ─────────────────────────────────────────────────────────

func TestResolveManualTeam_NoTeams(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, _ := d.CreateLocalUser("noteam@test.com", "NoTeam", "password1")

	if got := h.resolveManualTeam(uid); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestResolveManualTeam_NonManualTeam(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, _ := d.CreateLocalUser("normal@test.com", "Normal", "password1")
	tid, _ := d.CreateTeam("Regular Team")
	d.AddTeamMember(tid, uid) //nolint:errcheck

	if got := h.resolveManualTeam(uid); got != nil {
		t.Errorf("expected nil for non-manual team, got %+v", got)
	}
}

func TestResolveManualTeam_SingleManualTeam(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, _ := d.CreateLocalUser("manual@test.com", "Manual", "password1")
	tid, _ := d.CreateTeamWithDetails("Manual Team", "PROJ", true)
	d.AddTeamMember(tid, uid) //nolint:errcheck

	got := h.resolveManualTeam(uid)
	if got == nil {
		t.Fatal("expected a manual team")
	}
	if got.ID != tid || got.JiraSpaceKey != "PROJ" {
		t.Errorf("unexpected team: %+v", got)
	}
}

func TestResolveManualTeam_MultipleManualTeams_PicksFirstByName(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, _ := d.CreateLocalUser("multi@test.com", "Multi", "password1")
	tidB, _ := d.CreateTeamWithDetails("Bravo Team", "BRV", true)
	tidA, _ := d.CreateTeamWithDetails("Alpha Team", "ALP", true)
	d.AddTeamMember(tidB, uid) //nolint:errcheck
	d.AddTeamMember(tidA, uid) //nolint:errcheck

	got := h.resolveManualTeam(uid)
	if got == nil || got.ID != tidA {
		t.Errorf("expected Alpha Team (alphabetically first), got %+v", got)
	}
}

// ─── validateActivityRequest ──────────────────────────────────────────────────

func TestValidateActivityRequest_InvalidType(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, _ := d.CreateLocalUser("valid1@test.com", "Valid1", "password1")

	if err := h.validateActivityRequest(uid, "2026-05-04", "bogus", "", 50, 0); err == nil {
		t.Error("expected error for invalid activity type")
	}
}

func TestValidateActivityRequest_JiraWithoutKey(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, _ := d.CreateLocalUser("valid2@test.com", "Valid2", "password1")

	if err := h.validateActivityRequest(uid, "2026-05-04", models.ActivityTypeJira, "", 50, 0); err == nil {
		t.Error("expected error when jira type has no key")
	}
}

func TestValidateActivityRequest_PercentageOutOfRange(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, _ := d.CreateLocalUser("valid3@test.com", "Valid3", "password1")

	if err := h.validateActivityRequest(uid, "2026-05-04", models.ActivityTypeOther, "", 0, 0); err == nil {
		t.Error("expected error for percentage <= 0")
	}
	if err := h.validateActivityRequest(uid, "2026-05-04", models.ActivityTypeOther, "", 101, 0); err == nil {
		t.Error("expected error for percentage > 100")
	}
}

func TestValidateActivityRequest_NonBillableDate(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, _ := d.CreateLocalUser("valid4@test.com", "Valid4", "password1")

	if err := h.validateActivityRequest(uid, "2026-05-04", models.ActivityTypeOther, "", 50, 0); err == nil {
		t.Error("expected error for a date with no billable presence")
	}
}

func TestValidateActivityRequest_ExceedsDayAllocation(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, _ := d.CreateLocalUser("valid5@test.com", "Valid5", "password1")
	statusID, _ := d.CreateStatus(models.Status{Name: "Billable", Color: "#22c55e", Billable: true, SortOrder: 1})
	d.SetPresences(uid, []string{"2026-05-04"}, statusID, "full") //nolint:errcheck

	if err := h.validateActivityRequest(uid, "2026-05-04", models.ActivityTypeOther, "", 60, 0); err != nil {
		t.Fatalf("expected first 60%% to be valid: %v", err)
	}
	d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 60) //nolint:errcheck

	if err := h.validateActivityRequest(uid, "2026-05-04", models.ActivityTypeOther, "", 60, 0); err == nil {
		t.Error("expected error when total would exceed 100%")
	}
}

func TestValidateActivityRequest_HalfDayCapsAt50(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, _ := d.CreateLocalUser("valid6@test.com", "Valid6", "password1")
	statusID, _ := d.CreateStatus(models.Status{Name: "Billable", Color: "#22c55e", Billable: true, SortOrder: 1})
	d.SetPresences(uid, []string{"2026-05-04"}, statusID, "AM") //nolint:errcheck

	if err := h.validateActivityRequest(uid, "2026-05-04", models.ActivityTypeOther, "", 50, 0); err != nil {
		t.Fatalf("expected 50%% to be valid on a half day: %v", err)
	}
	if err := h.validateActivityRequest(uid, "2026-05-04", models.ActivityTypeOther, "", 51, 0); err == nil {
		t.Error("expected error exceeding the 50% cap on a half day")
	}
}

// ─── CreateProjectActivity / UpdateProjectActivity / DeleteProjectActivity ────

func createActivityTestUser(t *testing.T, d interface {
	CreateLocalUser(string, string, string) (int64, error)
	CreateSession(int64) (string, error)
}, email string) (int64, string) {
	t.Helper()
	uid, err := d.CreateLocalUser(email, "Test User", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return uid, tok
}

func TestCreateProjectActivity_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, tok := createActivityTestUser(t, d, "createact1@test.com")
	statusID, _ := d.CreateStatus(models.Status{Name: "Billable", Color: "#22c55e", Billable: true, SortOrder: 1})
	d.SetPresences(uid, []string{"2026-05-04"}, statusID, "full") //nolint:errcheck

	body := []byte(`{"date":"2026-05-04","activity_type":"other","comment":"support","percentage":40}`)
	req := httptest.NewRequest(http.MethodPost, "/api/project-activities", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateProjectActivity)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProjectActivity_ValidationError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	_, tok := createActivityTestUser(t, d, "createact2@test.com")

	body := []byte(`{"date":"2026-05-04","activity_type":"other","percentage":40}`)
	req := httptest.NewRequest(http.MethodPost, "/api/project-activities", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateProjectActivity)).ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 (non-billable date), got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProjectActivity_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	_, tok := createActivityTestUser(t, d, "createact3@test.com")

	req := httptest.NewRequest(http.MethodPost, "/api/project-activities", bytes.NewReader([]byte("{bad")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateProjectActivity)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateProjectActivity_OwnerCanUpdate(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, tok := createActivityTestUser(t, d, "updateact1@test.com")
	statusID, _ := d.CreateStatus(models.Status{Name: "Billable", Color: "#22c55e", Billable: true, SortOrder: 1})
	d.SetPresences(uid, []string{"2026-05-04"}, statusID, "full") //nolint:errcheck
	id, _ := d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "old", 40)

	body := []byte(`{"activity_type":"other","comment":"new","percentage":80}`)
	req := httptest.NewRequest(http.MethodPut, "/api/project-activities/"+strconvI64(id), bytes.NewReader(body))
	req.SetPathValue("id", strconvI64(id))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateProjectActivity)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	a, _ := d.GetProjectActivity(id)
	if a.Comment != "new" || a.Percentage != 80 {
		t.Errorf("unexpected activity after update: %+v", a)
	}
}

func TestUpdateProjectActivity_NotOwner_Returns403(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	owner, _ := createActivityTestUser(t, d, "owner1@test.com")
	_, otherTok := createActivityTestUser(t, d, "other1@test.com")
	statusID, _ := d.CreateStatus(models.Status{Name: "Billable", Color: "#22c55e", Billable: true, SortOrder: 1})
	d.SetPresences(owner, []string{"2026-05-04"}, statusID, "full") //nolint:errcheck
	id, _ := d.CreateProjectActivity(owner, "2026-05-04", models.ActivityTypeOther, "", "", "", 40)

	body := []byte(`{"activity_type":"other","comment":"hacked","percentage":10}`)
	req := httptest.NewRequest(http.MethodPut, "/api/project-activities/"+strconvI64(id), bytes.NewReader(body))
	req.SetPathValue("id", strconvI64(id))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: otherTok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateProjectActivity)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateProjectActivity_NotFound(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	_, tok := createActivityTestUser(t, d, "notfound1@test.com")

	body := []byte(`{"activity_type":"other","percentage":10}`)
	req := httptest.NewRequest(http.MethodPut, "/api/project-activities/99999", bytes.NewReader(body))
	req.SetPathValue("id", "99999")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateProjectActivity)).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteProjectActivity_OwnerCanDelete(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, tok := createActivityTestUser(t, d, "deleteact1@test.com")
	id, _ := d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 40)

	req := httptest.NewRequest(http.MethodDelete, "/api/project-activities/"+strconvI64(id), nil)
	req.SetPathValue("id", strconvI64(id))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DeleteProjectActivity)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := d.GetProjectActivity(id); err == nil {
		t.Error("expected activity to be deleted")
	}
}

func TestDeleteProjectActivity_NotOwner_Returns403(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	owner, _ := createActivityTestUser(t, d, "owner2@test.com")
	_, otherTok := createActivityTestUser(t, d, "other2@test.com")
	id, _ := d.CreateProjectActivity(owner, "2026-05-04", models.ActivityTypeOther, "", "", "", 40)

	req := httptest.NewRequest(http.MethodDelete, "/api/project-activities/"+strconvI64(id), nil)
	req.SetPathValue("id", strconvI64(id))
	req.AddCookie(&http.Cookie{Name: "session", Value: otherTok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DeleteProjectActivity)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

// ─── ListProjectActivitiesAPI ──────────────────────────────────────────────────

func TestListProjectActivitiesAPI_ReturnsMonthActivities(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, tok := createActivityTestUser(t, d, "listact1@test.com")
	d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 40) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/project-activities?year=2026&month=5", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ListProjectActivitiesAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &out) //nolint:errcheck
	activities := out["activities"].([]interface{})
	if len(activities) != 1 {
		t.Errorf("expected 1 activity, got %d", len(activities))
	}
}

func TestListProjectActivitiesAPI_InvalidMonth(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	_, tok := createActivityTestUser(t, d, "listact2@test.com")

	req := httptest.NewRequest(http.MethodGet, "/api/project-activities?year=2026&month=13", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ListProjectActivitiesAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ─── ListJiraTicketsAPI ─────────────────────────────────────────────────────────

func TestListJiraTicketsAPI_JiraDisabled_ReturnsEmpty(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Config: &config.Config{JiraEnabled: false}}
	_, tok := createActivityTestUser(t, d, "jira1@test.com")

	req := httptest.NewRequest(http.MethodGet, "/api/project-activities/jira-tickets", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ListJiraTicketsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &out) //nolint:errcheck
	if len(out["tickets"].([]interface{})) != 0 {
		t.Error("expected empty ticket list when Jira is disabled")
	}
}

func TestListJiraTicketsAPI_NoManualTeam_ReturnsEmpty(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Config: &config.Config{JiraEnabled: true, JiraBaseURL: "https://x.atlassian.net", JiraEmail: "a@a.com", JiraToken: "t"}}
	_, tok := createActivityTestUser(t, d, "jira2@test.com")

	req := httptest.NewRequest(http.MethodGet, "/api/project-activities/jira-tickets", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ListJiraTicketsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &out) //nolint:errcheck
	if len(out["tickets"].([]interface{})) != 0 {
		t.Error("expected empty ticket list when user has no manual team")
	}
}

// ─── ProjectsPage manual mode ───────────────────────────────────────────────────

func TestProjectsPage_ManualMode_RendersManualData(t *testing.T) {
	d := newExtraTestDB(t)
	var rendered map[string]interface{}
	h := &ProjectsHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = data.(map[string]interface{})
	}}
	uid, tok := createActivityTestUser(t, d, "manualpage1@test.com")
	tid, _ := d.CreateTeamWithDetails("Manual Team", "PROJ", true)
	d.AddTeamMember(tid, uid) //nolint:errcheck
	statusID, _ := d.CreateStatus(models.Status{Name: "Billable", Color: "#22c55e", Billable: true, SortOrder: 1})
	d.SetPresences(uid, []string{"2026-05-04"}, statusID, "full") //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/projects?year=2026&month=5", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ProjectsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if rendered["ManualMode"] != true {
		t.Fatalf("expected ManualMode=true, got %+v", rendered["ManualMode"])
	}
	dates := rendered["ManualDates"].([]string)
	if len(dates) != 1 || dates[0] != "2026-05-04" {
		t.Errorf("unexpected ManualDates: %+v", dates)
	}
}

func TestProjectsPage_NormalMode_WhenNoManualTeam(t *testing.T) {
	d := newExtraTestDB(t)
	var rendered map[string]interface{}
	h := &ProjectsHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = data.(map[string]interface{})
	}}
	_, tok := createActivityTestUser(t, d, "normalpage1@test.com")

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ProjectsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if _, ok := rendered["ManualMode"]; ok {
		t.Error("expected no ManualMode key in normal mode")
	}
	if _, ok := rendered["Projects"]; !ok {
		t.Error("expected Projects key in normal mode")
	}
}

// ─── Team activities report ─────────────────────────────────────────────────────

func TestAccessibleManualTeams_ProjectsAdmin_SeesAll(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	tid1, _ := d.CreateTeamWithDetails("Alpha", "ALP", true)
	d.CreateTeamWithDetails("Beta", "BET", false) //nolint:errcheck
	tid3, _ := d.CreateTeamWithDetails("Gamma", "GAM", true)

	admin := &models.User{Roles: models.RoleProjectsAdmin}
	teams := h.accessibleManualTeams(admin)
	if len(teams) != 2 {
		t.Fatalf("expected 2 manual teams, got %d", len(teams))
	}
	if teams[0].ID != tid1 || teams[1].ID != tid3 {
		t.Errorf("unexpected teams order: %+v", teams)
	}
}

func TestAccessibleManualTeams_TeamLeader_SeesOwnOnly(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	uid, _ := d.CreateLocalUser("tlmanual@test.com", "TL Manual", "password1")
	myTeam, _ := d.CreateTeamWithDetails("My Manual Team", "MYT", true)
	d.CreateTeamWithDetails("Other Manual Team", "OTH", true) //nolint:errcheck
	d.AddTeamMember(myTeam, uid)                              //nolint:errcheck

	leader := &models.User{ID: uid, Roles: models.RoleTeamLeader}
	teams := h.accessibleManualTeams(leader)
	if len(teams) != 1 || teams[0].ID != myTeam {
		t.Errorf("expected only own manual team, got %+v", teams)
	}
}

func TestTeamActivitiesForMonth_EnrichesUserNames(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}
	tid, _ := d.CreateTeamWithDetails("Reporting Team", "REP", true)
	uid, _ := d.CreateLocalUser("reportmember@test.com", "Report Member", "password1")
	d.AddTeamMember(tid, uid)                                                            //nolint:errcheck
	d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 40) //nolint:errcheck

	activities, err := h.teamActivitiesForMonth(tid, 2026, 5)
	if err != nil {
		t.Fatalf("teamActivitiesForMonth: %v", err)
	}
	if len(activities) != 1 || activities[0].UserName != "Report Member" {
		t.Errorf("unexpected activities: %+v", activities)
	}
}

func TestProjectsReportPage_ActivitiesView_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}
	tid, _ := d.CreateTeamWithDetails("View Team", "VWT", true)
	uid, _ := d.CreateLocalUser("viewmember@test.com", "View Member", "password1")
	d.AddTeamMember(tid, uid) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects-report?view=activities&team="+strconvI64(tid)+"&year=2026&month=5", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjectsReportPage_ActivitiesView_PassesJiraBaseURL(t *testing.T) {
	d := newExtraTestDB(t)
	var gotJiraBaseURL interface{}
	h := &ProjectsHandler{
		DB:     d,
		Config: &config.Config{JiraEnabled: true, JiraBaseURL: "https://acme.atlassian.net", JiraEmail: "a@a.com", JiraToken: "t"},
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			m := data.(map[string]interface{})
			gotJiraBaseURL = m["JiraBaseURL"]
		},
	}
	tid, _ := d.CreateTeamWithDetails("Jira URL Team", "JUT", true)
	uid, _ := d.CreateLocalUser("jiraurlmember@test.com", "Jira URL Member", "password1")
	d.AddTeamMember(tid, uid) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects-report?view=activities&team="+strconvI64(tid), nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if gotJiraBaseURL != "https://acme.atlassian.net" {
		t.Errorf("expected JiraBaseURL to be passed through, got %v", gotJiraBaseURL)
	}
}

func TestProjectsReportPage_ActivitiesView_JiraBaseURLEmptyWhenNoConfig(t *testing.T) {
	d := newExtraTestDB(t)
	var gotJiraBaseURL interface{}
	h := &ProjectsHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		m := data.(map[string]interface{})
		gotJiraBaseURL = m["JiraBaseURL"]
	}}

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects-report?view=activities", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if gotJiraBaseURL != "" {
		t.Errorf("expected empty JiraBaseURL when Config is nil, got %v", gotJiraBaseURL)
	}
}

func TestProjectsReportAPI_ActivitiesView_ReturnsJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}
	tid, _ := d.CreateTeamWithDetails("API View Team", "AVT", true)
	uid, _ := d.CreateLocalUser("apiviewmember@test.com", "API View Member", "password1")
	d.AddTeamMember(tid, uid)                                                            //nolint:errcheck
	d.CreateProjectActivity(uid, "2026-05-04", models.ActivityTypeOther, "", "", "", 40) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/projects-report?view=activities&team="+strconvI64(tid)+"&year=2026&month=5", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	activities := out["activities"].([]interface{})
	if len(activities) != 1 {
		t.Errorf("expected 1 activity, got %d", len(activities))
	}
}
