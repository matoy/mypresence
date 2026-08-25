package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/models"
)

func TestProjectActivities_CRUD_And_Validation(t *testing.T) {
	d := newCRUDTestDB(t)
	cfg := &config.Config{DisableProjects: false}
	h := &ProjectsHandler{DB: d, Config: cfg}

	u1ID, _ := d.CreateLocalUser("pa_user1@example.com", "PA User 1", "pass")
	user1, _ := d.GetUserByID(u1ID)
	u2ID, _ := d.CreateLocalUser("pa_user2@example.com", "PA User 2", "pass")
	user2, _ := d.GetUserByID(u2ID)

	teamID, _ := d.CreateTeamWithDetails("Manual Dev Team", "", true, false)
	_ = d.AddTeamMember(teamID, u1ID)
	_ = d.AddTeamMember(teamID, u2ID)

	statusID, _ := d.CreateStatus(models.Status{Name: "Work", Color: "#000000", OnSite: true, Billable: true})
	date := "2026-08-15"
	_ = d.SetPresences(u1ID, []string{date}, statusID, "full")

	// 1. ListProjectActivitiesAPI - invalid month
	rec := httptest.NewRecorder()
	req := reqWithUser(d, user1, http.MethodGet, "/api/project-activities?year=2026&month=13", nil)
	h.ListProjectActivitiesAPI(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on invalid month, got %d", rec.Code)
	}

	// 2. ListProjectActivitiesAPI - valid (empty)
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodGet, "/api/project-activities?year=2026&month=8", nil)
	h.ListProjectActivitiesAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on ListProjectActivitiesAPI, got %d", rec.Code)
	}

	// 3. validateActivityRequest checks
	if err := h.validateActivityRequest(u1ID, "", models.ActivityTypeOther, "", "", 50, 0); err == nil || !strings.Contains(err.Error(), "date is required") {
		t.Errorf("expected date is required error, got %v", err)
	}
	if err := h.validateActivityRequest(u1ID, date, "invalid_type", "", "", 50, 0); err == nil || !strings.Contains(err.Error(), "invalid activity type") {
		t.Errorf("expected invalid activity type error, got %v", err)
	}
	if err := h.validateActivityRequest(u1ID, date, models.ActivityTypeJira, "", "", 50, 0); err == nil || !strings.Contains(err.Error(), "Jira ticket is required") {
		t.Errorf("expected Jira ticket required error, got %v", err)
	}
	if err := h.validateActivityRequest(u1ID, date, models.ActivityTypeOther, "", "", 0, 0); err == nil || !strings.Contains(err.Error(), "percentage") {
		t.Errorf("expected percentage error for 0, got %v", err)
	}
	if err := h.validateActivityRequest(u1ID, date, models.ActivityTypeOther, "", "", 150, 0); err == nil || !strings.Contains(err.Error(), "percentage") {
		t.Errorf("expected percentage error for 150, got %v", err)
	}
	if err := h.validateActivityRequest(u1ID, "2026-08-16", models.ActivityTypeOther, "", "", 50, 0); err == nil || !strings.Contains(err.Error(), "not a billable day") {
		t.Errorf("expected not a billable day error, got %v", err)
	}

	// 4. CreateProjectActivity - bad json
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodPost, "/api/project-activities", strings.NewReader("bad-json"))
	h.CreateProjectActivity(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad json CreateProjectActivity, got %d", rec.Code)
	}

	// 5. CreateProjectActivity - validation failure
	badBody, _ := json.Marshal(map[string]interface{}{
		"date":          date,
		"activity_type": "invalid",
		"percentage":    50,
	})
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodPost, "/api/project-activities", bytes.NewReader(badBody))
	h.CreateProjectActivity(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 on validation failure CreateProjectActivity, got %d", rec.Code)
	}

	// 6. CreateProjectActivity - success
	goodBody, _ := json.Marshal(map[string]interface{}{
		"date":          date,
		"activity_type": models.ActivityTypeOther,
		"comment":       "Initial setup",
		"percentage":    50,
	})
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodPost, "/api/project-activities", bytes.NewReader(goodBody))
	h.CreateProjectActivity(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on CreateProjectActivity, got %d: %s", rec.Code, rec.Body.String())
	}
	var createRes map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &createRes) //nolint:errcheck
	actID := int64(createRes["id"].(float64))

	// 7. UpdateProjectActivity - bad ID / not found
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodPut, "/api/project-activities/abc", strings.NewReader("{}"))
	req.SetPathValue("id", "abc")
	h.UpdateProjectActivity(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad ID UpdateProjectActivity, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodPut, "/api/project-activities/99999", strings.NewReader("{}"))
	req.SetPathValue("id", "99999")
	h.UpdateProjectActivity(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 on not found UpdateProjectActivity, got %d", rec.Code)
	}

	// 8. UpdateProjectActivity - other user forbidden
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user2, http.MethodPut, "/api/project-activities/"+strconv.FormatInt(actID, 10), bytes.NewReader(goodBody))
	req.SetPathValue("id", strconv.FormatInt(actID, 10))
	h.UpdateProjectActivity(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 when other user updates activity, got %d", rec.Code)
	}

	// 9. UpdateProjectActivity - bad json
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodPut, "/api/project-activities/"+strconv.FormatInt(actID, 10), strings.NewReader("bad-json"))
	req.SetPathValue("id", strconv.FormatInt(actID, 10))
	h.UpdateProjectActivity(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad json in UpdateProjectActivity, got %d", rec.Code)
	}

	// 10. UpdateProjectActivity - success
	updateBody, _ := json.Marshal(map[string]interface{}{
		"activity_type": models.ActivityTypeOther,
		"comment":       "Refined setup",
		"percentage":    75,
	})
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodPut, "/api/project-activities/"+strconv.FormatInt(actID, 10), bytes.NewReader(updateBody))
	req.SetPathValue("id", strconv.FormatInt(actID, 10))
	h.UpdateProjectActivity(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on UpdateProjectActivity, got %d: %s", rec.Code, rec.Body.String())
	}

	// 11. DeleteProjectActivity - bad ID / not found / forbidden
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodDelete, "/api/project-activities/bad", nil)
	req.SetPathValue("id", "bad")
	h.DeleteProjectActivity(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad ID DeleteProjectActivity, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodDelete, "/api/project-activities/99999", nil)
	req.SetPathValue("id", "99999")
	h.DeleteProjectActivity(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 on not found DeleteProjectActivity, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = reqWithUser(d, user2, http.MethodDelete, "/api/project-activities/"+strconv.FormatInt(actID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(actID, 10))
	h.DeleteProjectActivity(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 on forbidden DeleteProjectActivity, got %d", rec.Code)
	}

	// 12. DeleteProjectActivity - success
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user1, http.MethodDelete, "/api/project-activities/"+strconv.FormatInt(actID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(actID, 10))
	h.DeleteProjectActivity(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on DeleteProjectActivity, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProjectActivities_Helpers_And_Reports(t *testing.T) {
	d := newCRUDTestDB(t)

	// Mock Jira server
	jiraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issues": []map[string]interface{}{
				{"key": "DEV-101", "fields": map[string]interface{}{"summary": "Fix auth flow"}},
			},
		})
	}))
	defer jiraServer.Close()

	cfg := &config.Config{
		DisableProjects: false,
		JiraEnabled:     true,
		JiraBaseURL:     jiraServer.URL,
		JiraEmail:       "test@example.com",
		JiraToken:       "test-token",
	}

	var renderedPage string
	var renderedData interface{}
	h := &ProjectsHandler{
		DB:     d,
		Config: cfg,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			renderedPage = page
			renderedData = data
		},
	}

	uID, _ := d.CreateLocalUser("report_user@example.com", "Report User", "pass")
	_ = d.UpdateUserRoles(uID, models.RoleGlobal)
	user, _ := d.GetUserByID(uID)

	teamID, _ := d.CreateTeamWithDetails("Manual Team A", "DEV", true, false)
	_ = d.AddTeamMember(teamID, uID)

	domID, _ := d.CreateDomain("Core Dev")
	_ = d.UpdateTeamDomain(teamID, domID)
	_ = d.SetDomainManagers(domID, []int64{uID})

	statusID, _ := d.CreateStatus(models.Status{Name: "Work", Color: "#000000", OnSite: true, Billable: true})
	_ = d.SetPresences(uID, []string{"2026-08-10"}, statusID, "full")
	_, _ = d.CreateProjectActivity(uID, "2026-08-10", models.ActivityTypeOther, "", "", "Worked on tasks", 100)

	// 1. ListJiraTicketsAPI with Jira enabled
	rec := httptest.NewRecorder()
	req := reqWithUser(d, user, http.MethodGet, "/api/project-activities/jira-tickets", nil)
	h.ListJiraTicketsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on ListJiraTicketsAPI with mock server, got %d", rec.Code)
	}

	// 2. ProjectsPage in manual mode
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user, http.MethodGet, "/projects?year=2026&month=8", nil)
	h.ProjectsPage(rec, req)
	if renderedPage != "projects" {
		t.Errorf("expected renderedPage projects, got %q", renderedPage)
	}
	mData := renderedData.(map[string]interface{})
	if mData["ManualMode"] != true {
		t.Errorf("expected ManualMode true, got %v", mData["ManualMode"])
	}

	// 3. ProjectsReportPage view=activities with domain_id
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user, http.MethodGet, "/admin/projects-report?view=activities&domain="+strconv.FormatInt(domID, 10), nil)
	h.ProjectsReportPage(rec, req)
	if renderedPage != "admin_projects_report" {
		t.Errorf("expected renderedPage admin_projects_report, got %q", renderedPage)
	}

	// 4. ProjectsReportPage view=activities with team_id
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user, http.MethodGet, "/admin/projects-report?view=activities&team="+strconv.FormatInt(teamID, 10), nil)
	h.ProjectsReportPage(rec, req)
	if renderedPage != "admin_projects_report" {
		t.Errorf("expected renderedPage admin_projects_report, got %q", renderedPage)
	}

	// 5. ProjectsReportAPI view=activities
	rec = httptest.NewRecorder()
	req = reqWithUser(d, user, http.MethodGet, "/api/projects-report?view=activities&team="+strconv.FormatInt(teamID, 10), nil)
	h.ProjectsReportAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on ProjectsReportAPI view=activities, got %d", rec.Code)
	}

	// 6. domainTeamsByID helper
	dom := models.Domain{ID: 10, Name: "Tech"}
	teamList := []models.Team{{ID: 1, Name: "Team 1", DomainID: 10}}
	groups := []domainGroupView{
		{Domain: dom, Teams: teamList},
	}
	if foundTeams := domainTeamsByID(groups, 10); len(foundTeams) != 1 {
		t.Errorf("expected 1 team found for domain 10, got %d", len(foundTeams))
	}
	if missing := domainTeamsByID(groups, 99); missing != nil {
		t.Errorf("expected nil for non-matching domain, got %+v", missing)
	}
}
