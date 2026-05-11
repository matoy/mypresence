package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/metrics"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// ProjectsHandler handles all project-management pages.
type ProjectsHandler struct {
	DB     *db.DB
	Render func(w http.ResponseWriter, r *http.Request, page string, data interface{})
}

// ProjectsAPI returns project time-declaration data for the current user.
// GET /api/projects?year=2026&month=5
func (h *ProjectsHandler) ProjectsAPI(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	now := time.Now()

	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	month, _ := strconv.Atoi(r.URL.Query().Get("month"))
	if year == 0 {
		year = now.Year()
	}
	if month == 0 {
		month = int(now.Month())
	}
	if month < 1 || month > 12 {
		metrics.ProjectOpsTotal.WithLabelValues("list", "failure").Inc()
		jsonError(w, "Invalid month", http.StatusBadRequest)
		return
	}

	projects, _ := h.DB.ListActiveProjectsForMonth(year, month)
	entries, _ := h.DB.GetUserProjectEntriesForMonth(user.ID, year, month)
	billableDays, _ := h.DB.GetUserBillableDaysForMonth(user.ID, year, month)
	totalDeclared, _ := h.DB.GetUserTotalDeclaredForMonth(user.ID, year, month)

	entryMap := make(map[int64]float64)
	for _, e := range entries {
		entryMap[e.ProjectID] = e.Days
	}

	jsonOK(w, map[string]interface{}{
		"year":           year,
		"month":          month,
		"projects":       projects,
		"entries":        entries,
		"entry_map":      entryMap,
		"billable_days":  billableDays,
		"total_declared": totalDeclared,
	})
	metrics.ProjectOpsTotal.WithLabelValues("list", "success").Inc()
	slog.Info("project.api.list", "user", user.Email, "year", year, "month", month, "count", len(projects))
}

// ProjectTimeAPI returns the current user's project time entries for a month.
// GET /api/project-time?year=2026&month=5
func (h *ProjectsHandler) ProjectTimeAPI(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	now := time.Now()

	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	month, _ := strconv.Atoi(r.URL.Query().Get("month"))
	if year == 0 {
		year = now.Year()
	}
	if month == 0 {
		month = int(now.Month())
	}
	if month < 1 || month > 12 {
		metrics.ProjectOpsTotal.WithLabelValues("list", "failure").Inc()
		jsonError(w, "Invalid month", http.StatusBadRequest)
		return
	}

	entries, _ := h.DB.GetUserProjectEntriesForMonth(user.ID, year, month)
	billableDays, _ := h.DB.GetUserBillableDaysForMonth(user.ID, year, month)
	totalDeclared, _ := h.DB.GetUserTotalDeclaredForMonth(user.ID, year, month)

	jsonOK(w, map[string]interface{}{
		"year":           year,
		"month":          month,
		"entries":        entries,
		"billable_days":  billableDays,
		"total_declared": totalDeclared,
	})
	metrics.ProjectOpsTotal.WithLabelValues("list", "success").Inc()
	slog.Info("project.api.time", "user", user.Email, "year", year, "month", month, "entries", len(entries))
}

// ─── User: time declaration ───────────────────────────────────────────────────

// ProjectsPage renders the user-facing time-declaration page (GET /projects).
func (h *ProjectsHandler) ProjectsPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	now := time.Now()

	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	month, _ := strconv.Atoi(r.URL.Query().Get("month"))
	if year == 0 {
		year = now.Year()
	}
	if month == 0 {
		month = int(now.Month())
	}

	projects, _ := h.DB.ListActiveProjectsForMonth(year, month)
	entries, _ := h.DB.GetUserProjectEntriesForMonth(user.ID, year, month)
	billableDays, _ := h.DB.GetUserBillableDaysForMonth(user.ID, year, month)
	totalDeclared, _ := h.DB.GetUserTotalDeclaredForMonth(user.ID, year, month)

	// Build entry map: projectID -> days
	entryMap := make(map[int64]float64)
	for _, e := range entries {
		entryMap[e.ProjectID] = e.Days
	}

	h.Render(w, r, "projects", map[string]interface{}{
		"Projects":      projects,
		"EntryMap":      entryMap,
		"BillableDays":  billableDays,
		"TotalDeclared": totalDeclared,
		"Year":          year,
		"Month":         month,
		"PrevYear":      prevYM(year, month),
		"PrevMonth":     prevMonth(month),
		"NextYear":      nextYM(year, month),
		"NextMonth":     nextMonth(month),
	})
}

// SetProjectTime handles POST /api/project-time.
// Body: {"project_id": 1, "year": 2026, "month": 5, "days": 3.5}
func (h *ProjectsHandler) SetProjectTime(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	var req struct {
		ProjectID int64   `json:"project_id"`
		Year      int     `json:"year"`
		Month     int     `json:"month"`
		Days      float64 `json:"days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.ProjectOpsTotal.WithLabelValues("set_time", "failure").Inc()
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.ProjectID == 0 || req.Year == 0 || req.Month < 1 || req.Month > 12 {
		metrics.ProjectOpsTotal.WithLabelValues("set_time", "failure").Inc()
		jsonError(w, "Invalid parameters", http.StatusBadRequest)
		return
	}
	if req.Days < 0 {
		metrics.ProjectOpsTotal.WithLabelValues("set_time", "failure").Inc()
		jsonError(w, "Days must be >= 0", http.StatusBadRequest)
		return
	}

	// Verify project exists, is active and end_date not in the past relative to this month
	proj, err := h.DB.GetProject(req.ProjectID)
	if err != nil || !proj.Active {
		metrics.ProjectOpsTotal.WithLabelValues("set_time", "failure").Inc()
		jsonError(w, "Project not found or inactive", http.StatusBadRequest)
		return
	}
	firstDay := fmt.Sprintf("%04d-%02d-01", req.Year, req.Month)
	if proj.EndDate < firstDay {
		metrics.ProjectOpsTotal.WithLabelValues("set_time", "failure").Inc()
		jsonError(w, "Project ended before this month", http.StatusBadRequest)
		return
	}

	if req.Days > 0 && h.exceedsBillableCap(user.ID, req.ProjectID, req.Year, req.Month, req.Days) {
		metrics.ProjectOpsTotal.WithLabelValues("set_time", "failure").Inc()
		jsonError(w, "Exceeds billable days cap", http.StatusUnprocessableEntity)
		return
	}

	if err := h.DB.SetProjectTimeEntry(user.ID, req.ProjectID, req.Year, req.Month, req.Days); err != nil {
		slog.Error("project.time.set", "error", err)
		metrics.ProjectOpsTotal.WithLabelValues("set_time", "failure").Inc()
		jsonError(w, "Server error", http.StatusInternalServerError)
		return
	}

	slog.Info("project.time.set", "user", user.Email, "project_id", req.ProjectID, "year", req.Year, "month", req.Month, "days", req.Days)
	h.DB.LogAdminAction(user.ID, "project", req.ProjectID, "set_time", fmt.Sprintf("year=%d month=%d days=%.1f", req.Year, req.Month, req.Days)) //nolint:errcheck
	metrics.ProjectOpsTotal.WithLabelValues("set_time", "success").Inc()
	if req.Days > 0 {
		metrics.ProjectDeclaredDaysTotal.Add(req.Days)
	}
	totalDeclared, _ := h.DB.GetUserTotalDeclaredForMonth(user.ID, req.Year, req.Month)
	billable, _ := h.DB.GetUserBillableDaysForMonth(user.ID, req.Year, req.Month)
	jsonOK(w, map[string]interface{}{"status": "ok", "total_declared": totalDeclared, "billable": billable})
}

// ─── Admin: project management ────────────────────────────────────────────────

// AdminProjectsPage renders the admin project management page (GET /admin/projects).
func (h *ProjectsHandler) AdminProjectsPage(w http.ResponseWriter, r *http.Request) {
	projects, _ := h.DB.ListProjects()
	teams, _ := h.DB.ListTeams()

	query := r.URL.Query()
	filterText := query.Get("q")
	filterActive := query.Get("active") // "1", "0", or ""
	if _, hasActive := query["active"]; !hasActive {
		filterActive = "1"
	}
	filterTeam, _ := strconv.ParseInt(query.Get("team"), 10, 64)

	filtered := make([]models.Project, 0, len(projects))
	for _, p := range projects {
		if filterText != "" && !containsCI(p.Name, filterText) && !containsCI(p.Code, filterText) {
			continue
		}
		if filterActive == "1" && !p.Active {
			continue
		}
		if filterActive == "0" && p.Active {
			continue
		}
		if filterTeam > 0 && p.TeamID != filterTeam {
			continue
		}
		filtered = append(filtered, p)
	}

	h.Render(w, r, "admin_projects", map[string]interface{}{
		"Projects":     filtered,
		"Teams":        teams,
		"FilterText":   filterText,
		"FilterActive": filterActive,
		"FilterTeam":   filterTeam,
		"ProjectCount": len(filtered),
		"ProjectTotal": len(projects),
	})
}

// AdminProjectsAPI returns projects + teams for admin management.
// GET /api/admin/projects
func (h *ProjectsHandler) AdminProjectsAPI(w http.ResponseWriter, r *http.Request) {
	projects, _ := h.DB.ListProjects()
	teams, _ := h.DB.ListTeams()

	query := r.URL.Query()
	filterText := query.Get("q")
	filterActive := query.Get("active")
	if _, hasActive := query["active"]; !hasActive {
		filterActive = "1"
	}
	filterTeam, _ := strconv.ParseInt(query.Get("team"), 10, 64)

	filtered := make([]models.Project, 0, len(projects))
	for _, p := range projects {
		if filterText != "" && !containsCI(p.Name, filterText) && !containsCI(p.Code, filterText) {
			continue
		}
		if filterActive == "1" && !p.Active {
			continue
		}
		if filterActive == "0" && p.Active {
			continue
		}
		if filterTeam > 0 && p.TeamID != filterTeam {
			continue
		}
		filtered = append(filtered, p)
	}

	jsonOK(w, map[string]interface{}{
		"projects":      filtered,
		"teams":         teams,
		"filter_text":   filterText,
		"filter_active": filterActive,
		"filter_team":   filterTeam,
	})
	metrics.ProjectOpsTotal.WithLabelValues("list", "success").Inc()
	slog.Info("admin.project.api.list", "count", len(filtered), "filter_active", filterActive, "filter_team", filterTeam)
}

// CreateProject handles POST /admin/projects.
func (h *ProjectsHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	actor := middleware.GetUser(r)

	var req struct {
		Name      string `json:"name"`
		Code      string `json:"code"`
		TeamID    int64  `json:"team_id"`
		Active    bool   `json:"active"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.ProjectOpsTotal.WithLabelValues("create", "failure").Inc()
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Code == "" {
		metrics.ProjectOpsTotal.WithLabelValues("create", "failure").Inc()
		jsonError(w, "Name and code are required", http.StatusBadRequest)
		return
	}
	if req.StartDate == "" || req.EndDate == "" {
		metrics.ProjectOpsTotal.WithLabelValues("create", "failure").Inc()
		jsonError(w, "Start and end dates are required", http.StatusBadRequest)
		return
	}
	if req.StartDate > req.EndDate {
		metrics.ProjectOpsTotal.WithLabelValues("create", "failure").Inc()
		jsonError(w, "Start date must be before end date", http.StatusBadRequest)
		return
	}

	id, err := h.DB.CreateProject(req.Name, req.Code, req.TeamID, req.Active, req.StartDate, req.EndDate)
	if err != nil {
		slog.Error("admin.project.create", "error", err)
		metrics.ProjectOpsTotal.WithLabelValues("create", "failure").Inc()
		jsonError(w, "Server error", http.StatusInternalServerError)
		return
	}
	h.DB.LogAdminAction(actor.ID, "project", id, "create", req.Code+" "+req.Name) //nolint:errcheck
	metrics.ProjectOpsTotal.WithLabelValues("create", "success").Inc()
	slog.Info("admin.project.create", "actor", actor.Email, "project_id", id, "name", req.Name)
	jsonOK(w, map[string]interface{}{"id": id, "status": "ok"})
}

// UpdateProject handles PUT /admin/projects/{id}.
func (h *ProjectsHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	actor := middleware.GetUser(r)
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		metrics.ProjectOpsTotal.WithLabelValues("update", "failure").Inc()
		jsonError(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Name      string `json:"name"`
		Code      string `json:"code"`
		TeamID    int64  `json:"team_id"`
		Active    bool   `json:"active"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.ProjectOpsTotal.WithLabelValues("update", "failure").Inc()
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Code == "" {
		metrics.ProjectOpsTotal.WithLabelValues("update", "failure").Inc()
		jsonError(w, "Name and code are required", http.StatusBadRequest)
		return
	}
	if req.StartDate > req.EndDate {
		metrics.ProjectOpsTotal.WithLabelValues("update", "failure").Inc()
		jsonError(w, "Start date must be before end date", http.StatusBadRequest)
		return
	}

	if err := h.DB.UpdateProject(id, req.Name, req.Code, req.TeamID, req.Active, req.StartDate, req.EndDate); err != nil {
		slog.Error("admin.project.update", "error", err)
		metrics.ProjectOpsTotal.WithLabelValues("update", "failure").Inc()
		jsonError(w, "Server error", http.StatusInternalServerError)
		return
	}
	h.DB.LogAdminAction(actor.ID, "project", id, "update", req.Code+" "+req.Name) //nolint:errcheck
	metrics.ProjectOpsTotal.WithLabelValues("update", "success").Inc()
	slog.Info("admin.project.update", "actor", actor.Email, "project_id", id, "name", req.Name)
	jsonOK(w, map[string]string{"status": "ok"})
}

// ─── Projects Report ──────────────────────────────────────────────────────────

// ProjectsReportPage renders the projects report page (GET /admin/projects-report).
func (h *ProjectsHandler) ProjectsReportPage(w http.ResponseWriter, r *http.Request) {
	currentUser := middleware.GetUser(r)
	now := time.Now()

	// Build 3 month keys: current + 2 previous
	monthKeys := make([]string, 3)
	for i := 0; i < 3; i++ {
		t := now.AddDate(0, -i, 0)
		monthKeys[2-i] = fmt.Sprintf("%04d-%02d", t.Year(), int(t.Month()))
	}

	allProjects, allTeams := h.buildProjectReportRows(currentUser, monthKeys)

	// Apply optional UI filters
	query := r.URL.Query()
	filterText := query.Get("q")
	filterActive := query.Get("active") // "1", "0", or ""
	if _, hasActive := query["active"]; !hasActive {
		// Default only on first load (no active parameter in URL).
		// If active is present but empty, user explicitly selected "all".
		filterActive = "1"
	}
	filterTeam, _ := strconv.ParseInt(query.Get("team"), 10, 64)

	filtered := filterReportRows(allProjects, filterText, filterActive, filterTeam)

	h.Render(w, r, "admin_projects_report", map[string]interface{}{
		"Rows":         filtered,
		"MonthKeys":    monthKeys,
		"CurrentMonth": monthKeys[len(monthKeys)-1],
		"Teams":        allTeams,
		"FilterText":   filterText,
		"FilterActive": filterActive,
		"FilterTeam":   filterTeam,
	})
	metrics.ProjectOpsTotal.WithLabelValues("report", "success").Inc()
	slog.Info("project.report.view", "user", currentUser.Email, "rows", len(filtered), "filter_active", filterActive, "filter_team", filterTeam)
}

// ProjectsReportAPI returns the projects report payload as JSON.
// GET /api/projects-report
func (h *ProjectsHandler) ProjectsReportAPI(w http.ResponseWriter, r *http.Request) {
	currentUser := middleware.GetUser(r)
	now := time.Now()

	monthKeys := make([]string, 3)
	for i := 0; i < 3; i++ {
		t := now.AddDate(0, -i, 0)
		monthKeys[2-i] = fmt.Sprintf("%04d-%02d", t.Year(), int(t.Month()))
	}

	reportRows, allTeams := h.buildProjectReportRows(currentUser, monthKeys)

	query := r.URL.Query()
	filterText := query.Get("q")
	filterActive := query.Get("active")
	if _, hasActive := query["active"]; !hasActive {
		filterActive = "1"
	}
	filterTeam, _ := strconv.ParseInt(query.Get("team"), 10, 64)

	filtered := filterReportRows(reportRows, filterText, filterActive, filterTeam)

	jsonOK(w, map[string]interface{}{
		"rows":          filtered,
		"month_keys":    monthKeys,
		"teams":         allTeams,
		"filter_text":   filterText,
		"filter_active": filterActive,
		"filter_team":   filterTeam,
		"project_scope": len(reportRows),
	})
	metrics.ProjectOpsTotal.WithLabelValues("report", "success").Inc()
	slog.Info("project.report.api", "user", currentUser.Email, "rows", len(filtered), "filter_active", filterActive, "filter_team", filterTeam)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// exceedsBillableCap returns true when adding days for projectID would exceed the
// user's billable-days cap for the given year/month.
func (h *ProjectsHandler) exceedsBillableCap(userID, projectID int64, year, month int, days float64) bool {
	billable, _ := h.DB.GetUserBillableDaysForMonth(userID, year, month)
	current, _ := h.DB.GetUserTotalDeclaredForMonth(userID, year, month)
	entries, _ := h.DB.GetUserProjectEntriesForMonth(userID, year, month)
	var existing float64
	for _, e := range entries {
		if e.ProjectID == projectID {
			existing = e.Days
			break
		}
	}
	return current-existing+days > billable+0.001 // small tolerance for float
}

// buildProjectReportRows loads and assembles the report rows for the given user
// and set of month keys, restricting to the user's teams when they lack admin/viewer role.
func (h *ProjectsHandler) buildProjectReportRows(currentUser *models.User, monthKeys []string) ([]models.ProjectReportRow, []models.Team) {
	var teamIDFilter []int64
	if !currentUser.HasAnyRole(models.RoleProjectsAdmin, models.RoleProjectsViewer) {
		ids, _ := h.DB.GetTeamIDsForUser(currentUser.ID)
		teamIDFilter = ids
	}
	allProjects, _ := h.DB.ListProjectsByTeams(teamIDFilter)
	allTeams, _ := h.DB.ListTeams()
	allUsers, _ := h.DB.ListUsers()
	userMap := make(map[int64]models.User)
	for _, u := range allUsers {
		userMap[u.ID] = u
	}
	projectIDs := make([]int64, 0, len(allProjects))
	for _, p := range allProjects {
		projectIDs = append(projectIDs, p.ID)
	}
	reportRows, _ := h.DB.GetProjectsReport(projectIDs, monthKeys, userMap)
	enrichReportTotals(reportRows, monthKeys)
	return reportRows, allTeams
}

// filterReportRows applies text/active/team filters to a slice of report rows.
func filterReportRows(rows []models.ProjectReportRow, filterText, filterActive string, filterTeam int64) []models.ProjectReportRow {
	filtered := make([]models.ProjectReportRow, 0, len(rows))
	for _, row := range rows {
		if filterText != "" && !containsCI(row.Project.Name, filterText) && !containsCI(row.Project.Code, filterText) {
			continue
		}
		if filterActive == "1" && !row.Project.Active {
			continue
		}
		if filterActive == "0" && row.Project.Active {
			continue
		}
		if filterTeam > 0 && row.Project.TeamID != filterTeam {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

// enrichReportTotals computes TotalPastDays and TotalToDateDays on each row/userrow.
// monthKeys is sorted ascending; currentMonthKey is monthKeys[len-1].
func enrichReportTotals(rows []models.ProjectReportRow, monthKeys []string) {
	currentMonthKey := monthKeys[len(monthKeys)-1]
	for ri := range rows {
		row := &rows[ri]
		var rowPast, rowToDate float64
		for ui := range row.UserRows {
			ur := &row.UserRows[ui]
			var past, toDate float64
			for _, mk := range monthKeys {
				v := ur.MonthlyDays[mk]
				toDate += v
				if mk < currentMonthKey {
					past += v
				}
			}
			ur.TotalPastDays = past
			ur.TotalToDateDays = toDate
			rowPast += past
			rowToDate += toDate
		}
		row.TotalPastDays = rowPast
		row.TotalToDateDays = rowToDate
	}
}

func prevMonth(month int) int {
	m := month - 1
	if m < 1 {
		return 12
	}
	return m
}
func prevYM(year, month int) int {
	if month == 1 {
		return year - 1
	}
	return year
}
func nextMonth(month int) int {
	m := month + 1
	if m > 12 {
		return 1
	}
	return m
}
func nextYM(year, month int) int {
	if month == 12 {
		return year + 1
	}
	return year
}
func containsCI(s, sub string) bool {
	sl := len(sub)
	if sl == 0 {
		return true
	}
	lowerS := toLower(s)
	lowerSub := toLower(sub)
	return len(lowerS) >= sl && (func() bool {
		for i := 0; i <= len(lowerS)-sl; i++ {
			if lowerS[i:i+sl] == lowerSub {
				return true
			}
		}
		return false
	})()
}
func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
