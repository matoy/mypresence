package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// ActivityHandler handles the Activity Report page.
type ActivityHandler struct {
	DB              *db.DB
	Render          func(w http.ResponseWriter, r *http.Request, page string, data interface{})
	DisableProjects bool
}

// ActivityPage renders the activity report page.
func (h *ActivityHandler) ActivityPage(w http.ResponseWriter, r *http.Request) {
	currentUser := middleware.GetUser(r)
	allTeams, _ := h.DB.ListTeams()
	statuses, _ := h.DB.ListStatuses()

	teams, myTeamIDs := filterTeamsForUser(h.DB, currentUser, allTeams)

	year, month, viewMode, teamID := normalizeActivityParams(r, time.Now(), teams, myTeamIDs)

	startDate := fmt.Sprintf("%04d-%02d-01", year, month)
	lastDay := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC)
	endDate := lastDay.Format("2006-01-02")

	var stats []models.UserStats
	if teamID > 0 {
		stats, _ = h.DB.GetTeamStats(teamID, startDate, endDate)
	}

	totalBillable, totalSetDays, statusTotals := computeStatusTotals(stats)

	// Build daily breakdown data
	allHolidays, _ := h.DB.ListHolidays()
	days := getDaysInMonth(year, month)
	markHolidaysOnDays(days, allHolidays)
	members, presenceMap := h.buildActivityMemberData(stats, teamID, startDate, endDate)

	// Count working days in the month (Mon–Fri) and holidays on those days.
	workingDays, holidayCount := computeWorkingDays(year, month, allHolidays)
	workingDaysExcluded := workingDays - holidayCount
	totalOnSite := 0.0
	for _, s := range stats {
		totalOnSite += s.OnSiteDays
	}

	projectActivityByUser := make(map[int64]float64)
	totalProjectDeclared := 0.0
	if !h.DisableProjects {
		projectActivityByUser, totalProjectDeclared = h.computeProjectActivity(stats, year, month)
	}

	totalWorkingDays := float64(workingDaysExcluded) * float64(len(stats))
	totalNotSet := totalWorkingDays - totalSetDays
	if totalNotSet < 0 {
		totalNotSet = 0
	}

	// Per-day billable / on-site counts for daily breakdown footer
	dayBillable, dayOnSite := computeDayBillableOnSite(presenceMap, statuses)

	// Executive summary — only visible to activity_viewer (and global admins)
	showExecSummary := currentUser != nil && currentUser.HasRole(models.RoleActivityViewer)
	execStatusTotals := make(map[int64]float64)
	var execTotalBillable, execTotalOnSite, execTotalNotSet, execTotalWorkingDays, execProjectActivityPct float64
	var execUserCount int
	if showExecSummary && len(allTeams) > 0 {
		execStatusTotals, execTotalBillable, execTotalOnSite, execTotalNotSet, execTotalWorkingDays, execProjectActivityPct, execUserCount =
			h.computeExecSummary(allTeams, startDate, endDate, workingDaysExcluded, year, month)
	}

	prevTime := time.Date(year, time.Month(month)-1, 1, 0, 0, 0, 0, time.UTC)
	nextTime := time.Date(year, time.Month(month)+1, 1, 0, 0, 0, 0, time.UTC)

	h.Render(w, r, "admin_activity", map[string]interface{}{
		"Teams":                  teams,
		"Statuses":               statuses,
		"Stats":                  stats,
		"ShowProjectActivity":    !h.DisableProjects,
		"ProjectActivityByUser":  projectActivityByUser,
		"TotalProjectDeclared":   totalProjectDeclared,
		"SelectedTeamID":         teamID,
		"Year":                   year,
		"Month":                  month,
		"ViewMode":               viewMode,
		"TotalBillable":          totalBillable,
		"TotalNotSet":            totalNotSet,
		"TotalOnSite":            totalOnSite,
		"TotalWorkingDays":       totalWorkingDays,
		"WorkingDays":            workingDays,
		"WorkingDaysExcl":        workingDaysExcluded,
		"HolidayCount":           holidayCount,
		"DayBillable":            dayBillable,
		"DayOnSite":              dayOnSite,
		"StatusTotals":           statusTotals,
		"PrevYear":               prevTime.Year(),
		"PrevMonth":              int(prevTime.Month()),
		"NextYear":               nextTime.Year(),
		"NextMonth":              int(nextTime.Month()),
		"Days":                   days,
		"Users":                  members,
		"PresenceMap":            presenceMap,
		"ShowExecSummary":        showExecSummary,
		"ExecStatusTotals":       execStatusTotals,
		"ExecTotalBillable":      execTotalBillable,
		"ExecTotalOnSite":        execTotalOnSite,
		"ExecTotalNotSet":        execTotalNotSet,
		"ExecTotalWorkingDays":   execTotalWorkingDays,
		"ExecProjectActivityPct": execProjectActivityPct,
		"ExecUserCount":          execUserCount,
	})
}

// ActivityAPI returns activity report data as JSON.
func (h *ActivityHandler) ActivityAPI(w http.ResponseWriter, r *http.Request) {
	currentUser := middleware.GetUser(r)
	teamID, _ := strconv.ParseInt(r.URL.Query().Get("team_id"), 10, 64)
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	month, _ := strconv.Atoi(r.URL.Query().Get("month"))

	if teamID == 0 || year == 0 || month == 0 {
		jsonError(w, "Paramètres manquants", http.StatusBadRequest)
		return
	}

	// Team leaders can only request stats for their own teams
	if currentUser != nil && currentUser.HasRole(models.RoleTeamLeader) && !currentUser.HasAnyRole(models.RoleActivityViewer, models.RoleGlobal) {
		myTeams, _ := h.DB.GetUserTeams(currentUser.ID)
		allowed := false
		for _, t := range myTeams {
			if t.ID == teamID {
				allowed = true
				break
			}
		}
		if !allowed {
			jsonError(w, "Access denied", http.StatusForbidden)
			return
		}
	}

	startDate := fmt.Sprintf("%04d-%02d-01", year, month)
	lastDay := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC)
	endDate := lastDay.Format("2006-01-02")

	stats, err := h.DB.GetTeamStats(teamID, startDate, endDate)
	if err != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}

	jsonOK(w, stats)
}

// filterTeamsForUser returns the teams visible to the given user and, if the user
// is a restricted team leader, the set of their team IDs (nil otherwise).
func filterTeamsForUser(database *db.DB, user *models.User, allTeams []models.Team) ([]models.Team, map[int64]bool) {
	if user == nil || user.HasAnyRole(models.RoleActivityViewer, models.RoleGlobal) {
		return allTeams, nil
	}
	if !user.HasRole(models.RoleTeamLeader) {
		return allTeams, nil
	}
	myTeams, _ := database.GetUserTeams(user.ID)
	myTeamIDs := map[int64]bool{}
	for _, t := range myTeams {
		myTeamIDs[t.ID] = true
	}
	var filtered []models.Team
	for _, t := range allTeams {
		if myTeamIDs[t.ID] {
			filtered = append(filtered, t)
		}
	}
	return filtered, myTeamIDs
}

// computeWorkingDays counts the working days (Mon–Fri) in the given month and
// the number of those working days that are non-imputable public holidays.
func computeWorkingDays(year, month int, holidays []models.Holiday) (workingDays, holidayCount int) {
	lastDay := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC)
	for d := 1; d <= lastDay.Day(); d++ {
		t := time.Date(year, time.Month(month), d, 0, 0, 0, 0, time.UTC)
		if t.Weekday() != time.Saturday && t.Weekday() != time.Sunday {
			workingDays++
		}
	}
	for _, hol := range holidays {
		t, err := time.Parse("2006-01-02", hol.Date)
		if err != nil {
			continue
		}
		if int(t.Month()) != month || t.Year() != year ||
			t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
			continue
		}
		if !hol.AllowImputed {
			holidayCount++
		}
	}
	return
}

// computeDayBillableOnSite aggregates per-date billable and on-site half-day
// weights from the presence map for the activity daily breakdown footer.
func computeDayBillableOnSite(presenceMap map[int64]map[string]map[string]int64, statuses []models.Status) (dayBillable, dayOnSite map[string]float64) {
	billableIDs := make(map[int64]bool)
	onSiteIDs := make(map[int64]bool)
	for _, s := range statuses {
		if s.Billable {
			billableIDs[s.ID] = true
		}
		if s.OnSite {
			onSiteIDs[s.ID] = true
		}
	}
	dayBillable = make(map[string]float64)
	dayOnSite = make(map[string]float64)
	for _, userPresences := range presenceMap {
		for date, halves := range userPresences {
			for half, statusID := range halves {
				weight := 1.0
				if half == "AM" || half == "PM" {
					weight = 0.5
				}
				if billableIDs[statusID] {
					dayBillable[date] += weight
				}
				if onSiteIDs[statusID] {
					dayOnSite[date] += weight
				}
			}
		}
	}
	return
}

// computeExecSummary aggregates stats across all teams (deduplicating users) to
// produce a single executive summary row for activity_viewer users.
func (h *ActivityHandler) computeExecSummary(
	allTeams []models.Team,
	startDate, endDate string,
	workingDaysExcl, year, month int,
) (statusTotals map[int64]float64, totalBillable, totalOnSite, totalNotSet, totalWorkingDays, projectActivityPct float64, userCount int) {
	statusTotals = make(map[int64]float64)
	seen := make(map[int64]bool)
	totalSetDays := 0.0
	totalProjectDeclared := 0.0
	for _, team := range allTeams {
		stats, err := h.DB.GetTeamStats(team.ID, startDate, endDate)
		if err != nil {
			continue
		}
		for _, s := range stats {
			if seen[s.User.ID] {
				continue
			}
			seen[s.User.ID] = true
			userCount++
			totalBillable += s.BillableDays
			totalOnSite += s.OnSiteDays
			for sid, count := range s.StatusCounts {
				statusTotals[sid] += count
				totalSetDays += count
			}
			if !h.DisableProjects {
				declared, err := h.DB.GetUserTotalDeclaredForMonth(s.User.ID, year, month)
				if err == nil {
					totalProjectDeclared += declared
				}
			}
		}
	}
	totalWorkingDays = float64(workingDaysExcl) * float64(userCount)
	totalNotSet = totalWorkingDays - totalSetDays
	if totalNotSet < 0 {
		totalNotSet = 0
	}
	if totalBillable > 0 {
		projectActivityPct = (totalProjectDeclared / totalBillable) * 100.0
	}
	return
}

// computeProjectActivity returns the per-user project activity percentage and
// total declared days for the given month across all projects.
func (h *ActivityHandler) computeProjectActivity(stats []models.UserStats, year, month int) (projectActivityByUser map[int64]float64, totalProjectDeclared float64) {
	projectActivityByUser = make(map[int64]float64)
	for _, s := range stats {
		declared, err := h.DB.GetUserTotalDeclaredForMonth(s.User.ID, year, month)
		if err != nil {
			continue
		}
		totalProjectDeclared += declared
		if s.BillableDays > 0 {
			projectActivityByUser[s.User.ID] = (declared / s.BillableDays) * 100.0
		}
	}
	return
}

// normalizeActivityParams parses and normalizes the year, month, viewMode and teamID
// query parameters, applying defaults and enforcing team-leader access restrictions.
func normalizeActivityParams(r *http.Request, now time.Time, teams []models.Team, myTeamIDs map[int64]bool) (year, month int, viewMode string, teamID int64) {
	year, _ = strconv.Atoi(r.URL.Query().Get("year"))
	month, _ = strconv.Atoi(r.URL.Query().Get("month"))
	teamID, _ = strconv.ParseInt(r.URL.Query().Get("team"), 10, 64)
	viewMode = r.URL.Query().Get("view")
	if year == 0 {
		year = now.Year()
	}
	if month == 0 {
		month = int(now.Month())
	}
	if viewMode == "" {
		viewMode = "month"
	}
	if teamID == 0 && len(teams) > 0 {
		teamID = teams[0].ID
	}
	// Team leaders cannot request stats for teams they don't belong to.
	if myTeamIDs != nil && teamID > 0 && !myTeamIDs[teamID] {
		if len(teams) > 0 {
			teamID = teams[0].ID
		} else {
			teamID = 0
		}
	}
	return
}

// computeStatusTotals aggregates billable days, total set days and per-status
// counts from a slice of UserStats.
func computeStatusTotals(stats []models.UserStats) (totalBillable, totalSetDays float64, statusTotals map[int64]float64) {
	statusTotals = make(map[int64]float64)
	for _, s := range stats {
		totalBillable += s.BillableDays
		for sid, count := range s.StatusCounts {
			statusTotals[sid] += count
			totalSetDays += count
		}
	}
	return
}

// markHolidaysOnDays sets the IsHoliday and HolidayName fields on days that
// match a holiday in the provided list.
func markHolidaysOnDays(days []models.DayInfo, holidays []models.Holiday) {
	for i, d := range days {
		for _, hol := range holidays {
			if hol.Date == d.Date {
				days[i].IsHoliday = true
				days[i].HolidayName = hol.Name
				break
			}
		}
	}
}

// buildActivityMemberData returns the ordered member list and presence map for
// the given team stats. Returns nil members and an empty map when teamID is 0.
func (h *ActivityHandler) buildActivityMemberData(stats []models.UserStats, teamID int64, startDate, endDate string) (members []models.User, presenceMap map[int64]map[string]map[string]int64) {
	presenceMap = make(map[int64]map[string]map[string]int64)
	if teamID == 0 {
		return
	}
	members = make([]models.User, len(stats))
	userIDs := make([]int64, len(stats))
	for i, s := range stats {
		members[i] = s.User
		userIDs[i] = s.User.ID
	}
	presenceMap, _ = h.DB.GetPresences(userIDs, startDate, endDate)
	return
}
