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

// Month and day names are resolved at template render time via the i18n T map
// using the keys "cal.month.N" (1-12) and "cal.day.N" (0-6, Sunday=0).

// CalendarHandler handles the main calendar view.
type CalendarHandler struct {
	DB                *db.DB
	Render            func(w http.ResponseWriter, r *http.Request, page string, data interface{})
	DisableFloorplans bool
}

// teamCalendarView holds display data for one team's presence sub-table.
type teamCalendarView struct {
	Team         models.Team
	Members      []models.User
	Presences    map[int64]map[string]map[string]int64 // userID → date → half → statusID
	Reservations map[int64]map[string]bool             // userID → date → bool
	CanEdit      bool
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

	// Enrich days with holiday data
	holidayMap, _ := h.DB.GetHolidayMap(startDate, endDate)
	for i, d := range days {
		if hol, ok := holidayMap[d.Date]; ok {
			days[i].IsHoliday = true
			days[i].HolidayName = hol.Name
			days[i].HolidayAllowImputed = hol.AllowImputed
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

	// A month is complete when every declarable day has at least one status set.
	declarableDays, declaredDays, calendarComplete := computeMonthCompletion(days, userPresences)

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

	// Get statuses (only active ones for the picker)
	statuses, _ := h.DB.ListActiveStatuses()

	// Build per-team presence views for members
	canEditTeam := user.HasAnyRole(models.RoleTeamLeader, models.RoleTeamManager, models.RoleGlobal)
	myTeams, _ := h.DB.GetUserTeams(user.ID)
	var teamViews []teamCalendarView
	for _, team := range myTeams {
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
		teamViews = append(teamViews, teamCalendarView{
			Team:         team,
			Members:      members,
			Presences:    tp,
			Reservations: teamReservations,
			CanEdit:      canEditTeam,
		})
	}

	h.Render(w, r, "calendar", map[string]interface{}{
		"Year":             year,
		"Month":            month,
		"PrevYear":         prevTime.Year(),
		"PrevMonth":        int(prevTime.Month()),
		"NextYear":         nextTime.Year(),
		"NextMonth":        int(nextTime.Month()),
		"Days":             days,
		"Presences":        userPresences,
		"Statuses":         statuses,
		"CurrentUserID":    user.ID,
		"ReservationDates": reservationDates,
		"Floorplans":       floorplans,
		"CalendarComplete": calendarComplete,
		"DeclarableDays":   declarableDays,
		"DeclaredDays":     declaredDays,
		"TeamViews":        teamViews,
	})
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
	if !user.HasRole(models.RoleGlobal) && !user.HasRole(models.RoleTeamManager) && req.UserID != user.ID {
		if !user.HasRole(models.RoleTeamLeader) || !isTeamLeaderOf(h.DB, user.ID, req.UserID) {
			jsonError(w, "Non autorisé", http.StatusForbidden)
			return
		}
	}

	// Validate date format and collect date range for holiday lookup
	if len(req.Dates) == 0 {
		jsonError(w, "Aucune date fournie", http.StatusBadRequest)
		return
	}
	minDate, maxDate := req.Dates[0], req.Dates[0]
	for _, d := range req.Dates {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			jsonError(w, "Date invalide: "+d, http.StatusBadRequest)
			return
		}
		if d < minDate {
			minDate = d
		}
		if d > maxDate {
			maxDate = d
		}
	}

	// Reject dates that fall on non-imputable holidays
	holidayMap, _ := h.DB.GetHolidayMap(minDate, maxDate)
	for _, d := range req.Dates {
		if hol, ok := holidayMap[d]; ok && !hol.AllowImputed {
			jsonError(w, "Jour férié non imputable: "+hol.Name+" ("+d+")", http.StatusUnprocessableEntity)
			return
		}
	}

	if err := h.DB.SetPresences(req.UserID, req.Dates, req.StatusID, req.Half); err != nil {
		jsonError(w, "Erreur sauvegarde", http.StatusInternalServerError)
		return
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
		if !user.HasRole(models.RoleTeamLeader) || !isTeamLeaderOf(h.DB, user.ID, req.UserID) {
			jsonError(w, "Non autorisé", http.StatusForbidden)
			return
		}
	}

	if err := h.DB.ClearPresences(req.UserID, req.Dates, req.Half); err != nil {
		jsonError(w, "Erreur suppression", http.StatusInternalServerError)
		return
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

// isTeamLeaderOf returns true if leaderID and targetID share at least one common team.
// The caller must verify that leaderID has the team_leader role.
func isTeamLeaderOf(database *db.DB, leaderID, targetID int64) bool {
	leaderTeams, err := database.GetUserTeams(leaderID)
	if err != nil || len(leaderTeams) == 0 {
		return false
	}
	leaderTeamIDs := make(map[int64]bool, len(leaderTeams))
	for _, t := range leaderTeams {
		leaderTeamIDs[t.ID] = true
	}
	targetTeams, err := database.GetUserTeams(targetID)
	if err != nil {
		return false
	}
	for _, t := range targetTeams {
		if leaderTeamIDs[t.ID] {
			return true
		}
	}
	return false
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
