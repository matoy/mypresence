package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/i18n"
	"github.com/matoy/mypresence/internal/metrics"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// Month and day names are resolved at template render time via the i18n T map
// using the keys "cal.month.N" (1-12) and "cal.day.N" (0-6, Sunday=0).

// CalendarHandler handles the main calendar view.
type CalendarHandler struct {
	DB                *db.DB
	Render            func(w http.ResponseWriter, r *http.Request, page string, data interface{})
	DisableFloorplans bool
	DisableProjects   bool
}

// teamCalendarView holds display data for one team's presence sub-table.
type teamCalendarView struct {
	Team              models.Team
	Members           []models.User
	Presences         map[int64]map[string]map[string]int64         // userID → date → half → statusID
	Overrides         map[int64]map[string]models.PresenceOverride // userID → date → PresenceOverride
	Reservations      map[int64]map[string]bool                     // userID → date → bool
	CanEdit           bool
	Certifications    map[int64]bool             // userID → declaration certified for the displayed month
	ProjectActivities map[int64]map[string]bool // userID → date → 100% complete
}

// CalendarPage renders the monthly calendar view for the logged-in user.
func (h *CalendarHandler) CalendarPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	// Parse year/month from query
	now := time.Now()
	year, month := parseYearMonth(r, now)

	// Calculate prev/next month
	prevTime := time.Date(year, time.Month(month)-1, 1, 0, 0, 0, 0, time.UTC)
	nextTime := time.Date(year, time.Month(month)+1, 1, 0, 0, 0, 0, time.UTC)

	// Get days of month
	days := getDaysInMonth(year, month)
	startDate := fmt.Sprintf("%04d-%02d-01", year, month)
	lastDay := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC)
	endDate := lastDay.Format("2006-01-02")

	// Enrich days with holiday data for current user
	holidayMap, _ := h.DB.GetUserHolidayMap(user.ID, startDate, endDate)
	for i, d := range days {
		if hol, ok := holidayMap[d.Date]; ok {
			days[i].IsHoliday = true
			days[i].HolidayName = hol.Name
			days[i].HolidayAllowImputed = hol.AllowImputed
			days[i].HolidayCountryCode = hol.CountryCode
		}
	}

	// Get current user's presences only
	presenceMap, err := h.DB.GetPresences([]int64{user.ID}, startDate, endDate)
	if err != nil {
		http.Error(w, "Error loading presences", http.StatusInternalServerError)
		return
	}
	userPresences := presenceMap[user.ID]
	if userPresences == nil {
		userPresences = make(map[string]map[string]int64)
	}

	// Get current user's third-party overrides
	userOverridesMap, _ := h.DB.GetPresenceOverrides([]int64{user.ID}, startDate, endDate)
	userOverrides := userOverridesMap[user.ID]
	if userOverrides == nil {
		userOverrides = make(map[string]models.PresenceOverride)
	}

	// A month is complete when every declarable day has at least one status set.
	declarableDays, declaredDays, calendarComplete := computeMonthCompletion(days, userPresences)

	// Whether the current user has already certified this month's declaration.
	certified, _ := h.DB.IsMonthCertified(user.ID, year, month)

	// Get seat reservations and floorplans (skipped when floor plans are disabled)
	var reservationDates map[string]bool
	var floorplans []models.Floorplan
	if !h.DisableFloorplans {
		reservationDates, _ = h.DB.GetUserReservationDates(user.ID, startDate, endDate)
		floorplans, _ = h.DB.ListFloorplans()
	}
	if reservationDates == nil {
		reservationDates = make(map[string]bool)
	}

	// Get project activities for current user (dates with 100% activity declared)
	userProjectActivities := make(map[string]bool)
	if !h.DisableProjects {
		if activities, err := h.DB.ListUserActivitiesForMonth(user.ID, year, month); err == nil {
			activitySums := make(map[string]float64)
			for _, a := range activities {
				activitySums[a.Date] += a.Percentage
			}
			for date, sum := range activitySums {
				if sum >= 100.0-0.001 {
					userProjectActivities[date] = true
				}
			}
		}
	}

	// Get statuses (only active ones for the picker)
	statuses, _ := h.DB.ListActiveStatuses()

	// Build per-team presence views for members
	myTeams, _ := h.DB.GetUserTeams(user.ID)
	var teamViews []teamCalendarView
	for _, team := range myTeams {
		isLeader, _ := h.DB.IsLeaderOfTeam(user.ID, team.ID)
		canEditTeam := user.HasAnyRole(models.RoleTeamManager, models.RoleGlobal) || isLeader
		members, _ := h.DB.GetTeamMembersAt(team.ID, startDate)
		if len(members) == 0 {
			continue
		}
		userIDs := make([]int64, len(members))
		for i, m := range members {
			userIDs[i] = m.ID
		}
		tp, _ := h.DB.GetPresences(userIDs, startDate, endDate)
		if tp == nil {
			tp = make(map[int64]map[string]map[string]int64)
		}
		to, _ := h.DB.GetPresenceOverrides(userIDs, startDate, endDate)
		if to == nil {
			to = make(map[int64]map[string]models.PresenceOverride)
		}
		teamReservations := make(map[int64]map[string]bool, len(members))
		if !h.DisableFloorplans {
			for _, m := range members {
				r, _ := h.DB.GetUserReservationDates(m.ID, startDate, endDate)
				if r == nil {
					r = make(map[string]bool)
				}
				teamReservations[m.ID] = r
			}
		}
		teamCertifications, _ := h.DB.GetCertifiedUserIDs(userIDs, year, month)

		teamProjectActivities := make(map[int64]map[string]bool)
		if !h.DisableProjects && len(userIDs) > 0 {
			if teamActs, err := h.DB.GetActivitiesForUsersMonth(userIDs, year, month); err == nil {
				memberSums := make(map[int64]map[string]float64)
				for _, a := range teamActs {
					if memberSums[a.UserID] == nil {
						memberSums[a.UserID] = make(map[string]float64)
					}
					memberSums[a.UserID][a.Date] += a.Percentage
				}
				for uid, dates := range memberSums {
					teamProjectActivities[uid] = make(map[string]bool)
					for date, sum := range dates {
						if sum >= 100.0-0.001 {
							teamProjectActivities[uid][date] = true
						}
					}
				}
			}
		}

		teamViews = append(teamViews, teamCalendarView{
			Team:              team,
			Members:           members,
			Presences:         tp,
			Overrides:         to,
			Reservations:      teamReservations,
			CanEdit:           canEditTeam,
			Certifications:    teamCertifications,
			ProjectActivities: teamProjectActivities,
		})
	}

	h.Render(w, r, "calendar", map[string]interface{}{
		"Year":              year,
		"Month":             month,
		"PrevYear":          prevTime.Year(),
		"PrevMonth":         int(prevTime.Month()),
		"NextYear":          nextTime.Year(),
		"NextMonth":         int(nextTime.Month()),
		"Days":              days,
		"Presences":         userPresences,
		"Overrides":         userOverrides,
		"Statuses":          statuses,
		"CurrentUserID":     user.ID,
		"ReservationDates":  reservationDates,
		"Floorplans":        floorplans,
		"CalendarComplete":  calendarComplete,
		"DeclarableDays":    declarableDays,
		"DeclaredDays":      declaredDays,
		"TeamViews":         teamViews,
		"Certified":         certified,
		"ProjectActivities": userProjectActivities,
	})
}

// dateRange validates a slice of YYYY-MM-DD strings and returns the min/max.
// If any date is invalid, it is returned as the third value (others are zero).
func dateRange(dates []string) (minDate, maxDate, invalid string) {
	minDate, maxDate = dates[0], dates[0]
	for _, d := range dates {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return "", "", d
		}
		if d < minDate {
			minDate = d
		}
		if d > maxDate {
			maxDate = d
		}
	}
	return minDate, maxDate, ""
}

// SetPresences handles bulk presence setting via API.
func (h *CalendarHandler) SetPresences(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	var req struct {
		UserID   int64    `json:"user_id"`
		Dates    []string `json:"dates"`
		StatusID int64    `json:"status_id"`
		Half     string   `json:"half"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	// Validate: allow own edits, managers/global, and team leaders editing their team members.
	if !canEditPresenceFor(h.DB, user, req.UserID) {
		jsonError(w, "Non autorisé", http.StatusForbidden)
		return
	}

	// Validate date format and collect date range for holiday lookup
	if len(req.Dates) == 0 {
		jsonError(w, "Aucune date fournie", http.StatusBadRequest)
		return
	}
	minDate, maxDate, badDate := dateRange(req.Dates)
	if badDate != "" {
		jsonError(w, "Date invalide: "+badDate, http.StatusBadRequest)
		return
	}

	// Reject dates that fall on non-imputable holidays for this user
	holidayMap, _ := h.DB.GetUserHolidayMap(req.UserID, minDate, maxDate)
	for _, d := range req.Dates {
		if hol, ok := holidayMap[d]; ok && !hol.AllowImputed {
			jsonError(w, "Jour férié non imputable: "+hol.Name+" ("+d+")", http.StatusUnprocessableEntity)
			return
		}
	}

	// Reject edits for months already certified — only global admins, activity
	// viewers, or team leaders (own team) can decertify.
	locked, lerr := certifiedMonthFromDates(h.DB, req.UserID, req.Dates)
	if lerr != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	if locked {
		lang := i18n.LangFromRequest(r, "fr")
		msg := i18n.T(lang)["cert.locked_warning"]
		if msg == "" {
			msg = "Déclaration certifiée : modification impossible pour ce mois."
		}
		jsonError(w, msg, http.StatusLocked)
		return
	}

	if err := h.DB.SetPresences(req.UserID, req.Dates, req.StatusID, req.Half); err != nil {
		jsonError(w, "Erreur sauvegarde", http.StatusInternalServerError)
		return
	}

	// Cancel seat reservations on all affected dates: any status change invalidates an existing reservation.
	if !h.DisableFloorplans {
		if err := h.DB.CancelUserReservationsForDates(req.UserID, req.Dates); err != nil {
			slog.Warn("presence.set: failed to cancel reservations", "target_id", req.UserID, "err", err)
		} else {
			slog.Info("presence.set: reservations cancelled", "target_id", req.UserID, "dates", len(req.Dates))
		}
	}

	h.DB.LogPresenceAction(user.ID, req.UserID, "set", req.Dates, req.StatusID, req.Half) //nolint:errcheck
	slog.Info("presence.set", "actor", user.Email, "target_id", req.UserID, "dates", len(req.Dates), "status_id", req.StatusID, "half", req.Half)

	half := req.Half
	if half == "" {
		half = "full"
	}
	metrics.PresenceOpsTotal.WithLabelValues("set", half).Inc()
	metrics.PresenceDaysTotal.WithLabelValues("set").Add(float64(len(req.Dates)))

	jsonOK(w, map[string]string{"status": "ok"})
}

// ClearPresences handles presence deletion via API.
func (h *CalendarHandler) ClearPresences(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	var req struct {
		UserID int64    `json:"user_id"`
		Dates  []string `json:"dates"`
		Half   string   `json:"half"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	if !user.HasRole(models.RoleGlobal) && !user.HasRole(models.RoleTeamManager) && req.UserID != user.ID {
		if !h.DB.IsTeamLeaderOf(user.ID, req.UserID) {
			jsonError(w, "Non autorisé", http.StatusForbidden)
			return
		}
	}

	// Reject edits for months already certified — only global admins, activity
	// viewers, or team leaders (own team) can decertify.
	locked, lerr := certifiedMonthFromDates(h.DB, req.UserID, req.Dates)
	if lerr != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	if locked {
		lang := i18n.LangFromRequest(r, "fr")
		msg := i18n.T(lang)["cert.locked_warning"]
		if msg == "" {
			msg = "Déclaration certifiée : modification impossible pour ce mois."
		}
		jsonError(w, msg, http.StatusLocked)
		return
	}

	if err := h.DB.ClearPresences(req.UserID, req.Dates, req.Half); err != nil {
		jsonError(w, "Erreur suppression", http.StatusInternalServerError)
		return
	}

	// Cancel seat reservations on dates where the user is no longer on-site.
	if !h.DisableFloorplans {
		var datesToCancel []string
		for _, date := range req.Dates {
			if onSite, err := h.DB.GetUserOnSiteStatus(req.UserID, date); err == nil && !onSite {
				datesToCancel = append(datesToCancel, date)
			}
		}
		if len(datesToCancel) > 0 {
			if err := h.DB.CancelUserReservationsForDates(req.UserID, datesToCancel); err != nil {
				slog.Warn("presence.clear: failed to cancel reservations", "target_id", req.UserID, "err", err)
			} else {
				slog.Info("presence.clear: reservations cancelled", "target_id", req.UserID, "dates", len(datesToCancel))
			}
		}
	}

	h.DB.LogPresenceAction(user.ID, req.UserID, "clear", req.Dates, 0, req.Half) //nolint:errcheck
	slog.Info("presence.clear", "actor", user.Email, "target_id", req.UserID, "dates", len(req.Dates), "half", req.Half)

	clearHalf := req.Half
	if clearHalf == "" {
		clearHalf = "all"
	}
	metrics.PresenceOpsTotal.WithLabelValues("clear", clearHalf).Inc()
	metrics.PresenceDaysTotal.WithLabelValues("clear").Add(float64(len(req.Dates)))

	jsonOK(w, map[string]string{"status": "ok"})
}

// GetPresencesAPI returns presences as JSON.
func (h *CalendarHandler) GetPresencesAPI(w http.ResponseWriter, r *http.Request) {
	teamStr := r.URL.Query().Get("team_id")
	yearStr := r.URL.Query().Get("year")
	monthStr := r.URL.Query().Get("month")

	teamID, _ := strconv.ParseInt(teamStr, 10, 64)
	year, _ := strconv.Atoi(yearStr)
	month, _ := strconv.Atoi(monthStr)

	if teamID == 0 || year == 0 || month == 0 {
		jsonError(w, "Paramètres manquants", http.StatusBadRequest)
		return
	}

	startDate := fmt.Sprintf("%04d-%02d-01", year, month)
	lastDay := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC)
	endDate := lastDay.Format("2006-01-02")

	members, err := h.DB.GetTeamMembersAt(teamID, startDate)
	if err != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}

	userIDs := make([]int64, len(members))
	for i, m := range members {
		userIDs[i] = m.ID
	}

	presences, err := h.DB.GetPresences(userIDs, startDate, endDate)
	if err != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}

	jsonOK(w, presences)
}

// certifiedMonthFromDates reports whether any of the given dates falls within
// a month that is already certified for targetID, in which case presence
// edits must be rejected until a global admin decertifies that month.
func certifiedMonthFromDates(database *db.DB, targetID int64, dates []string) (bool, error) {
	checked := make(map[string]bool)
	for _, dstr := range dates {
		t, err := time.Parse("2006-01-02", dstr)
		if err != nil {
			continue
		}
		key := fmt.Sprintf("%04d-%02d", t.Year(), int(t.Month()))
		if checked[key] {
			continue
		}
		checked[key] = true
		certified, err := database.IsMonthCertified(targetID, t.Year(), int(t.Month()))
		if err != nil {
			return false, err
		}
		if certified {
			return true, nil
		}
	}
	return false, nil
}

// CertifyMonth allows a user to certify their own presence declaration for a
// given month, once every declarable day has a status set. Certification is
// self-service and irreversible from the user's side: once certified, the
// month's declarations can no longer be edited (see certifiedMonthFromDates)
// until a global admin decertifies it via DecertifyMonth.
func (h *CalendarHandler) CertifyMonth(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	var req struct {
		Year  int `json:"year"`
		Month int `json:"month"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Year < 2020 || req.Year > 2100 || req.Month < 1 || req.Month > 12 {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	startDate := fmt.Sprintf("%04d-%02d-01", req.Year, req.Month)
	lastDay := time.Date(req.Year, time.Month(req.Month)+1, 0, 0, 0, 0, 0, time.UTC)
	endDate := lastDay.Format("2006-01-02")

	days := getDaysInMonth(req.Year, req.Month)
	holidayMap, _ := h.DB.GetUserHolidayMap(user.ID, startDate, endDate)
	for i, d := range days {
		if hol, ok := holidayMap[d.Date]; ok {
			days[i].IsHoliday = true
			days[i].HolidayAllowImputed = hol.AllowImputed
			days[i].HolidayCountryCode = hol.CountryCode
		}
	}

	presenceMap, err := h.DB.GetPresences([]int64{user.ID}, startDate, endDate)
	if err != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	_, _, complete := computeMonthCompletion(days, presenceMap[user.ID])
	if !complete {
		jsonError(w, "Toutes les journées doivent être déclarées avant de certifier", http.StatusUnprocessableEntity)
		return
	}

	if err := h.DB.CertifyMonth(user.ID, req.Year, req.Month, user.ID); err != nil {
		jsonError(w, "Erreur lors de la certification", http.StatusInternalServerError)
		return
	}

	h.DB.LogAdminAction(user.ID, "user", user.ID, "certify_declaration", fmt.Sprintf("%04d-%02d", req.Year, req.Month))
	slog.Info("presence.certify", "actor", user.Email, "year", req.Year, "month", req.Month)
	metrics.AdminOpsTotal.WithLabelValues("certification", "certify", "success").Inc()
	metrics.CertificationsOpsTotal.WithLabelValues("presence", "certify").Inc()

	jsonOK(w, map[string]string{"status": "ok"})
}

// DecertifyMonth cancels a user's certification for a given month, allowing
// declarations to be edited again. Allowed for global admins, activity viewers,
// and team leaders (scoped to their own team's members); the route-level
// middleware (see main.go) admits these roles, and the scope is re-checked here.
func (h *CalendarHandler) DecertifyMonth(w http.ResponseWriter, r *http.Request) {
	currentUser := middleware.GetUser(r)
	if currentUser == nil {
		metrics.AdminOpsTotal.WithLabelValues("certification", "decertify", "failure").Inc()
		jsonError(w, "Non autorisé", http.StatusForbidden)
		return
	}

	var req struct {
		UserID int64 `json:"user_id"`
		Year   int   `json:"year"`
		Month  int   `json:"month"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID <= 0 || req.Year < 2020 || req.Year > 2100 || req.Month < 1 || req.Month > 12 {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	// A plain team leader (without activity_viewer/global) can only decertify members of their own team(s).
	if !currentUser.HasAnyRole(models.RoleGlobal, models.RoleActivityViewer) && !h.DB.IsTeamLeaderOf(currentUser.ID, req.UserID) {
		metrics.AdminOpsTotal.WithLabelValues("certification", "decertify", "failure").Inc()
		jsonError(w, "Non autorisé", http.StatusForbidden)
		return
	}

	if err := h.DB.DecertifyMonth(req.UserID, req.Year, req.Month); err != nil {
		jsonError(w, "Erreur lors de la décertification", http.StatusInternalServerError)
		return
	}

	h.DB.LogAdminAction(currentUser.ID, "user", req.UserID, "decertify_declaration", fmt.Sprintf("%04d-%02d", req.Year, req.Month))
	slog.Info("presence.decertify", "actor", currentUser.Email, "target_id", req.UserID, "year", req.Year, "month", req.Month)
	metrics.AdminOpsTotal.WithLabelValues("certification", "decertify", "success").Inc()
	metrics.CertificationsOpsTotal.WithLabelValues("presence", "decertify").Inc()

	jsonOK(w, map[string]string{"status": "ok"})
}

// isTeamLeaderOf returns true if leaderID is a designated leader of any team that targetID belongs to.
func isTeamLeaderOf(database *db.DB, leaderID, targetID int64) bool {
	return database.IsTeamLeaderOf(leaderID, targetID)
}

// canEditPresenceFor reports whether user is allowed to set or clear presences for targetID.
func canEditPresenceFor(database *db.DB, user *models.User, targetID int64) bool {
	if user.HasRole(models.RoleGlobal) || user.HasRole(models.RoleTeamManager) || targetID == user.ID {
		return true
	}
	return database.IsTeamLeaderOf(user.ID, targetID)
}

// parseYearMonth reads year and month from the request query string, falling
// back to the current date for missing or out-of-range values.
func parseYearMonth(r *http.Request, now time.Time) (year, month int) {
	year, month = now.Year(), int(now.Month())
	if y, err := strconv.Atoi(r.URL.Query().Get("year")); err == nil && y >= 2020 && y <= 2100 {
		year = y
	}
	if m, err := strconv.Atoi(r.URL.Query().Get("month")); err == nil && m >= 1 && m <= 12 {
		month = m
	}
	return
}

// computeMonthCompletion counts declarable days (working, non-holiday) and
// declared days (at least one presence half), and reports whether the month
// is fully declared.
func computeMonthCompletion(days []models.DayInfo, presences map[string]map[string]int64) (declarable, declared int, complete bool) {
	for _, d := range days {
		if d.IsWeekend || (d.IsHoliday && !d.HolidayAllowImputed) {
			continue
		}
		declarable++
		halves := presences[d.Date]
		if halves != nil && (halves["full"] > 0 || halves["AM"] > 0 || halves["PM"] > 0) {
			declared++
		}
	}
	complete = declarable > 0 && declared == declarable
	return
}

func getDaysInMonth(year, month int) []models.DayInfo {
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	lastDay := firstDay.AddDate(0, 1, -1)

	var days []models.DayInfo
	for d := 1; d <= lastDay.Day(); d++ {
		t := time.Date(year, time.Month(month), d, 0, 0, 0, 0, time.UTC)
		days = append(days, models.DayInfo{
			Day:       d,
			Date:      t.Format("2006-01-02"),
			DayIndex:  int(t.Weekday()),
			IsWeekend: t.Weekday() == time.Saturday || t.Weekday() == time.Sunday,
		})
	}
	return days
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data) //nolint:errcheck
}
