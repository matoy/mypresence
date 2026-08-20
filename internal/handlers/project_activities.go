package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/matoy/mypresence/internal/jira"
	"github.com/matoy/mypresence/internal/metrics"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// activityTolerance absorbs floating-point rounding when comparing declared
// percentages against a day's billable weight.
const activityTolerance = 0.001

// dayThreshold returns the percentage a day's activities must reach to be
// considered complete: 100% for a full billable day, 50% for a half day.
func dayThreshold(weight float64) float64 { return weight * 100.0 }

// isDateComplete reports whether the sum of a day's activity percentages
// reaches its billable weight threshold.
func isDateComplete(sumPct, weight float64) bool {
	return weight > 0 && sumPct >= dayThreshold(weight)-activityTolerance
}

// resolveManualTeam returns the team that governs "Timesheets managed
// manually" mode for the given user, or nil if none of their teams has it
// enabled. If several of the user's teams have it enabled, the first one by
// name is used and a warning is logged.
func (h *ProjectsHandler) resolveManualTeam(userID int64) *models.Team {
	teams, err := h.DB.GetUserTeams(userID)
	if err != nil || len(teams) == 0 {
		return nil
	}
	var manual []models.Team
	for _, t := range teams {
		if t.TimesheetsManagedManually {
			manual = append(manual, t)
		}
	}
	if len(manual) == 0 {
		return nil
	}
	sort.Slice(manual, func(i, j int) bool { return manual[i].Name < manual[j].Name })
	if len(manual) > 1 {
		slog.Warn("user belongs to several manual-timesheet teams; using the first by name",
			"user_id", userID, "chosen_team", manual[0].Name, "team_count", len(manual))
	}
	chosen := manual[0]
	return &chosen
}

// renderManualProjectsPage renders /projects in "Timesheets managed manually"
// mode: a vertical list of billable days, each requiring one or more
// activities whose percentages sum to the day's billable weight.
func (h *ProjectsHandler) renderManualProjectsPage(w http.ResponseWriter, r *http.Request, user *models.User, team *models.Team, year, month int) {
	weights, err := h.DB.GetUserBillableDatesForMonth(user.ID, year, month)
	if err != nil {
		weights = map[string]float64{}
	}
	activities, _ := h.DB.ListUserActivitiesForMonth(user.ID, year, month)

	activitiesByDate := make(map[string][]models.ProjectActivity)
	for _, a := range activities {
		activitiesByDate[a.Date] = append(activitiesByDate[a.Date], a)
	}

	dates := make([]string, 0, len(weights))
	for d := range weights {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	var billable, declared float64
	for _, date := range dates {
		weight := weights[date]
		billable += weight
		sum := 0.0
		for _, a := range activitiesByDate[date] {
			sum += a.Percentage
		}
		if isDateComplete(sum, weight) {
			declared += weight
		}
	}

	jiraEnabled := h.Config != nil && h.Config.JiraEnabled && team.JiraSpaceKey != ""

	certified, _ := h.DB.IsProjectMonthCertified(user.ID, year, month)

	h.Render(w, r, "projects", map[string]interface{}{
		"ManualMode":       true,
		"ManualDates":      dates,
		"ManualWeights":    weights,
		"ManualActivities": activitiesByDate,
		"JiraEnabled":      jiraEnabled,
		"BillableDays":     billable,
		"TotalDeclared":    declared,
		"Certified":        certified,
		"Year":             year,
		"Month":            month,
		"PrevYear":         prevYM(year, month),
		"PrevMonth":        prevMonth(month),
		"NextYear":         nextYM(year, month),
		"NextMonth":        nextMonth(month),
	})
}

// ListProjectActivitiesAPI returns the current user's activity declarations
// for a month. GET /api/project-activities?year=2026&month=5
func (h *ProjectsHandler) ListProjectActivitiesAPI(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	month, _ := strconv.Atoi(r.URL.Query().Get("month"))
	if year == 0 || month < 1 || month > 12 {
		jsonError(w, "Invalid month", http.StatusBadRequest)
		return
	}
	activities, err := h.DB.ListUserActivitiesForMonth(user.ID, year, month)
	if err != nil {
		jsonError(w, "Server error", http.StatusInternalServerError)
		return
	}
	if activities == nil {
		activities = []models.ProjectActivity{}
	}
	jsonOK(w, map[string]interface{}{"activities": activities})
}

// validateActivityRequest checks that an activity declaration is well-formed
// and does not exceed the day's billable allocation.
func (h *ProjectsHandler) validateActivityRequest(userID int64, date, activityType, jiraKey string, percentage float64, excludeID int64) error {
	if date == "" {
		return fmt.Errorf("date is required")
	}
	switch activityType {
	case models.ActivityTypeJira, models.ActivityTypeServiceNow, models.ActivityTypeOther:
	default:
		return fmt.Errorf("invalid activity type")
	}
	if activityType == models.ActivityTypeJira && jiraKey == "" {
		return fmt.Errorf("a Jira ticket is required")
	}
	if percentage <= 0 || percentage > 100 {
		return fmt.Errorf("percentage must be between 0 and 100")
	}
	weight, err := h.DB.GetUserBillableWeightForDate(userID, date)
	if err != nil {
		return fmt.Errorf("server error")
	}
	if weight <= 0 {
		return fmt.Errorf("this date is not a billable day")
	}
	existing, err := h.DB.GetUserActivitiesTotalForDate(userID, date, excludeID)
	if err != nil {
		return fmt.Errorf("server error")
	}
	if existing+percentage > dayThreshold(weight)+activityTolerance {
		return fmt.Errorf("exceeds this day's allocation (max %.0f%%)", dayThreshold(weight))
	}
	return nil
}

type activityRequestBody struct {
	Date         string  `json:"date"`
	ActivityType string  `json:"activity_type"`
	JiraKey      string  `json:"jira_key"`
	JiraTitle    string  `json:"jira_title"`
	Comment      string  `json:"comment"`
	Percentage   float64 `json:"percentage"`
}

// CreateProjectActivity handles POST /api/project-activities.
func (h *ProjectsHandler) CreateProjectActivity(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	var req activityRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.ProjectOpsTotal.WithLabelValues("activity_create", "failure").Inc()
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if err := h.validateActivityRequest(user.ID, req.Date, req.ActivityType, req.JiraKey, req.Percentage, 0); err != nil {
		metrics.ProjectOpsTotal.WithLabelValues("activity_create", "failure").Inc()
		jsonError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if year, month, err := yearMonthFromDate(req.Date); err == nil && rejectIfProjectMonthCertified(w, h, user.ID, year, month) {
		metrics.ProjectOpsTotal.WithLabelValues("activity_create", "failure").Inc()
		return
	}

	id, err := h.DB.CreateProjectActivity(user.ID, req.Date, req.ActivityType, req.JiraKey, req.JiraTitle, req.Comment, req.Percentage)
	if err != nil {
		slog.Error("project.activity.create", "error", err)
		metrics.ProjectOpsTotal.WithLabelValues("activity_create", "failure").Inc()
		jsonError(w, "Server error", http.StatusInternalServerError)
		return
	}
	metrics.ProjectOpsTotal.WithLabelValues("activity_create", "success").Inc()
	slog.Info("project.activity.create", "user", user.Email, "date", req.Date, "type", req.ActivityType, "percentage", req.Percentage)
	jsonOK(w, map[string]interface{}{"id": id, "status": "ok"})
}

// UpdateProjectActivity handles PUT /api/project-activities/{id}.
func (h *ProjectsHandler) UpdateProjectActivity(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	existing, err := h.DB.GetProjectActivity(id)
	if err != nil {
		jsonError(w, "Activity not found", http.StatusNotFound)
		return
	}
	if existing.UserID != user.ID {
		metrics.ProjectOpsTotal.WithLabelValues("activity_update", "failure").Inc()
		jsonError(w, "Access denied", http.StatusForbidden)
		return
	}

	var req activityRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.ProjectOpsTotal.WithLabelValues("activity_update", "failure").Inc()
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if err := h.validateActivityRequest(user.ID, existing.Date, req.ActivityType, req.JiraKey, req.Percentage, id); err != nil {
		metrics.ProjectOpsTotal.WithLabelValues("activity_update", "failure").Inc()
		jsonError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if year, month, err := yearMonthFromDate(existing.Date); err == nil && rejectIfProjectMonthCertified(w, h, user.ID, year, month) {
		metrics.ProjectOpsTotal.WithLabelValues("activity_update", "failure").Inc()
		return
	}

	if err := h.DB.UpdateProjectActivity(id, req.ActivityType, req.JiraKey, req.JiraTitle, req.Comment, req.Percentage); err != nil {
		slog.Error("project.activity.update", "error", err)
		metrics.ProjectOpsTotal.WithLabelValues("activity_update", "failure").Inc()
		jsonError(w, "Server error", http.StatusInternalServerError)
		return
	}
	metrics.ProjectOpsTotal.WithLabelValues("activity_update", "success").Inc()
	slog.Info("project.activity.update", "user", user.Email, "activity_id", id)
	jsonOK(w, map[string]string{"status": "ok"})
}

// DeleteProjectActivity handles DELETE /api/project-activities/{id}.
func (h *ProjectsHandler) DeleteProjectActivity(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	existing, err := h.DB.GetProjectActivity(id)
	if err != nil {
		jsonError(w, "Activity not found", http.StatusNotFound)
		return
	}
	if existing.UserID != user.ID {
		metrics.ProjectOpsTotal.WithLabelValues("activity_delete", "failure").Inc()
		jsonError(w, "Access denied", http.StatusForbidden)
		return
	}
	if year, month, err := yearMonthFromDate(existing.Date); err == nil && rejectIfProjectMonthCertified(w, h, user.ID, year, month) {
		metrics.ProjectOpsTotal.WithLabelValues("activity_delete", "failure").Inc()
		return
	}
	if err := h.DB.DeleteProjectActivity(id); err != nil {
		slog.Error("project.activity.delete", "error", err)
		metrics.ProjectOpsTotal.WithLabelValues("activity_delete", "failure").Inc()
		jsonError(w, "Server error", http.StatusInternalServerError)
		return
	}
	metrics.ProjectOpsTotal.WithLabelValues("activity_delete", "success").Inc()
	slog.Info("project.activity.delete", "user", user.Email, "activity_id", id)
	jsonOK(w, map[string]string{"status": "ok"})
}

// ListJiraTicketsAPI returns Jira tickets updated in the last 30 days for the
// current user's manual-timesheet team. GET /api/project-activities/jira-tickets
func (h *ProjectsHandler) ListJiraTicketsAPI(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	empty := map[string]interface{}{"tickets": []models.JiraTicket{}}
	if h.Config == nil || !h.Config.JiraEnabled {
		jsonOK(w, empty)
		return
	}
	team := h.resolveManualTeam(user.ID)
	if team == nil || team.JiraSpaceKey == "" {
		jsonOK(w, empty)
		return
	}

	client := jira.NewClient(h.Config.JiraBaseURL, h.Config.JiraEmail, h.Config.JiraToken)
	tickets, err := client.SearchRecentTickets(team.JiraSpaceKey)
	if err != nil {
		slog.Error("jira.search_tickets", "error", err, "team", team.Name)
		jsonError(w, "Failed to fetch Jira tickets", http.StatusBadGateway)
		return
	}
	if tickets == nil {
		tickets = []models.JiraTicket{}
	}
	jsonOK(w, map[string]interface{}{"tickets": tickets})
}

// teamActivitiesForMonth loads and enriches all project activities for the
// members of a team for a given month, sorted by user name then date.
func (h *ProjectsHandler) teamActivitiesForMonth(teamID int64, year, month int) ([]models.ProjectActivity, error) {
	members, err := h.DB.GetAllTeamMembers(teamID)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}
	userIDs := make([]int64, len(members))
	nameByID := make(map[int64]string, len(members))
	for i, m := range members {
		userIDs[i] = m.ID
		nameByID[m.ID] = m.Name
	}
	activities, err := h.DB.GetActivitiesForUsersMonth(userIDs, year, month)
	if err != nil {
		return nil, err
	}
	for i := range activities {
		activities[i].UserName = nameByID[activities[i].UserID]
	}
	sort.Slice(activities, func(i, j int) bool {
		if activities[i].UserName != activities[j].UserName {
			return activities[i].UserName < activities[j].UserName
		}
		if activities[i].Date != activities[j].Date {
			return activities[i].Date < activities[j].Date
		}
		return activities[i].ID < activities[j].ID
	})
	return activities, nil
}

// manualTeamsForUser returns the teams with manual timesheets enabled that are
// visible to the given user: all such teams for projects_admin/projects_viewer,
// only their own team(s) for a team_leader.
func manualTeamsForUser(allTeams []models.Team, myTeamIDs map[int64]bool) []models.Team {
	var result []models.Team
	for _, t := range allTeams {
		if !t.TimesheetsManagedManually {
			continue
		}
		if myTeamIDs != nil && !myTeamIDs[t.ID] {
			continue
		}
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// accessibleManualTeams returns the manual-timesheet teams visible to currentUser.
func (h *ProjectsHandler) accessibleManualTeams(currentUser *models.User) []models.Team {
	allTeams, err := h.DB.ListTeams()
	if err != nil {
		return nil
	}
	if currentUser.HasAnyRole(models.RoleProjectsAdmin, models.RoleProjectsViewer) {
		return manualTeamsForUser(allTeams, nil)
	}
	ids, _ := h.DB.GetTeamIDsForUser(currentUser.ID)
	myTeamIDs := make(map[int64]bool, len(ids))
	for _, id := range ids {
		myTeamIDs[id] = true
	}
	// Domain managers additionally see the manual teams within domains they manage.
	myDomains, _ := h.DB.GetUserDomains(currentUser.ID)
	if len(myDomains) > 0 {
		domainIDs := map[int64]bool{}
		for _, dm := range myDomains {
			domainIDs[dm.ID] = true
		}
		for _, t := range allTeams {
			if domainIDs[t.DomainID] {
				myTeamIDs[t.ID] = true
			}
		}
	}
	return manualTeamsForUser(allTeams, myTeamIDs)
}

// domainTeamsByID returns the teams of the domain group matching domainID.
func domainTeamsByID(groups []domainGroupView, domainID int64) []models.Team {
	for _, g := range groups {
		if g.Domain.ID == domainID {
			return g.Teams
		}
	}
	return nil
}

// domainActivitiesForMonth loads and merges the project activities of every
// team in the given slice for a given month, sorted by user name then date.
func (h *ProjectsHandler) domainActivitiesForMonth(teams []models.Team, year, month int) []models.ProjectActivity {
	var all []models.ProjectActivity
	for _, t := range teams {
		acts, err := h.teamActivitiesForMonth(t.ID, year, month)
		if err != nil {
			continue
		}
		all = append(all, acts...)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].UserName != all[j].UserName {
			return all[i].UserName < all[j].UserName
		}
		if all[i].Date != all[j].Date {
			return all[i].Date < all[j].Date
		}
		return all[i].ID < all[j].ID
	})
	return all
}

// activitiesReportParams bundles the resolved query parameters and access
// scope for the team-activities report view.
type activitiesReportParams struct {
	Teams        []models.Team
	TeamID       int64
	DomainID     int64
	DomainGroups []domainGroupView
	Year, Month  int
}

// resolveActivitiesReportParams parses year/month/team/domain query params for
// the team-activities report view, restricting access to what the user can see.
func (h *ProjectsHandler) resolveActivitiesReportParams(r *http.Request, currentUser *models.User) activitiesReportParams {
	now := time.Now()
	query := r.URL.Query()
	year, _ := strconv.Atoi(query.Get("year"))
	month, _ := strconv.Atoi(query.Get("month"))
	if year == 0 {
		year = now.Year()
	}
	if month == 0 {
		month = int(now.Month())
	}

	teams := h.accessibleManualTeams(currentUser)
	myDomains, teamsByDomain := domainsAccessForUser(h.DB, currentUser, teams)
	var domainGroups []domainGroupView
	for _, dm := range myDomains {
		domainGroups = append(domainGroups, domainGroupView{Domain: dm, Teams: teamsByDomain[dm.ID]})
	}

	domainID, _ := strconv.ParseInt(query.Get("domain"), 10, 64)
	validDomain := false
	for _, dm := range myDomains {
		if dm.ID == domainID {
			validDomain = true
			break
		}
	}
	if !validDomain {
		domainID = 0
	}

	var teamID int64
	if domainID == 0 {
		teamID, _ = strconv.ParseInt(query.Get("team"), 10, 64)
		allowed := teamID == 0
		for _, t := range teams {
			if t.ID == teamID {
				allowed = true
				break
			}
		}
		if !allowed {
			teamID = 0
		}
		if teamID == 0 && len(teams) > 0 {
			teamID = teams[0].ID
		}
	}

	return activitiesReportParams{
		Teams: teams, TeamID: teamID, DomainID: domainID,
		DomainGroups: domainGroups, Year: year, Month: month,
	}
}

// renderTeamActivitiesReportPage renders the team-activities view of the
// projects report page (GET /admin/projects-report?view=activities).
func (h *ProjectsHandler) renderTeamActivitiesReportPage(w http.ResponseWriter, r *http.Request, currentUser *models.User, showProjectsTab, showTasksTab bool) {
	p := h.resolveActivitiesReportParams(r, currentUser)

	var activities []models.ProjectActivity
	if p.DomainID > 0 {
		activities = h.domainActivitiesForMonth(domainTeamsByID(p.DomainGroups, p.DomainID), p.Year, p.Month)
	} else if p.TeamID > 0 {
		activities, _ = h.teamActivitiesForMonth(p.TeamID, p.Year, p.Month)
	}
	if activities == nil {
		activities = []models.ProjectActivity{}
	}

	jiraBaseURL := ""
	if h.Config != nil {
		jiraBaseURL = h.Config.JiraBaseURL
	}

	h.Render(w, r, "admin_projects_report", map[string]interface{}{
		"ViewMode":         "activities",
		"ManualTeams":      p.Teams,
		"SelectedTeamID":   p.TeamID,
		"SelectedDomainID": p.DomainID,
		"DomainGroups":     p.DomainGroups,
		"IsDomainManager":  len(p.DomainGroups) > 0,
		"Activities":       activities,
		"ActivityYear":     p.Year,
		"ActivityMonth":    p.Month,
		"PrevYear":         prevYM(p.Year, p.Month),
		"PrevMonth":        prevMonth(p.Month),
		"NextYear":         nextYM(p.Year, p.Month),
		"NextMonth":        nextMonth(p.Month),
		"JiraBaseURL":      jiraBaseURL,
		"ShowProjectsTab":  showProjectsTab,
		"ShowTasksTab":     showTasksTab,
	})
	metrics.ProjectOpsTotal.WithLabelValues("team_activities_report", "success").Inc()
	slog.Info("project.team_activities_report.view", "user", currentUser.Email, "team_id", p.TeamID, "domain_id", p.DomainID, "rows", len(activities))
}

// renderTeamActivitiesReportAPI returns the team-activities report payload as JSON
// (GET /api/projects-report?view=activities).
func (h *ProjectsHandler) renderTeamActivitiesReportAPI(w http.ResponseWriter, r *http.Request, currentUser *models.User) {
	p := h.resolveActivitiesReportParams(r, currentUser)

	var activities []models.ProjectActivity
	if p.DomainID > 0 {
		activities = h.domainActivitiesForMonth(domainTeamsByID(p.DomainGroups, p.DomainID), p.Year, p.Month)
	} else if p.TeamID > 0 {
		activities, _ = h.teamActivitiesForMonth(p.TeamID, p.Year, p.Month)
	}
	if activities == nil {
		activities = []models.ProjectActivity{}
	}

	jsonOK(w, map[string]interface{}{
		"teams":           p.Teams,
		"selected_team":   p.TeamID,
		"selected_domain": p.DomainID,
		"activities":      activities,
		"year":            p.Year,
		"month":           p.Month,
	})
	metrics.ProjectOpsTotal.WithLabelValues("team_activities_report", "success").Inc()
	slog.Info("project.team_activities_report.api", "user", currentUser.Email, "team_id", p.TeamID, "domain_id", p.DomainID, "rows", len(activities))
}
