package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/matoy/mypresence/internal/i18n"
	"github.com/matoy/mypresence/internal/metrics"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// userProjectDeclaration returns a user's billable/declared days for a month,
// covering both the percentage-based and "Timesheets managed manually" modes.
func (h *ProjectsHandler) userProjectDeclaration(userID int64, year, month int) (billable, declared float64) {
	if manualTeam := h.resolveManualTeam(userID); manualTeam != nil {
		weights, err := h.DB.GetUserBillableDatesForMonth(userID, year, month)
		if err != nil {
			weights = map[string]float64{}
		}
		activities, _ := h.DB.ListUserActivitiesForMonth(userID, year, month)
		activitiesByDate := make(map[string][]models.ProjectActivity)
		for _, a := range activities {
			activitiesByDate[a.Date] = append(activitiesByDate[a.Date], a)
		}
		for date, weight := range weights {
			billable += weight
			sum := 0.0
			for _, a := range activitiesByDate[date] {
				sum += a.Percentage
			}
			if isDateComplete(sum, weight) {
				declared += weight
			}
		}
		return billable, declared
	}
	billable, _ = h.DB.GetUserBillableDaysForMonth(userID, year, month)
	declared, _ = h.DB.GetUserTotalDeclaredForMonth(userID, year, month)
	return billable, declared
}

// projectDeclarationComplete reports whether a user's project declaration for
// a month covers all of their billable days, allowing certification.
func projectDeclarationComplete(billable, declared float64) bool {
	return billable > 0 && declared+activityTolerance >= billable
}

// CertifyProjectMonth allows a user to certify their own project time
// declaration (percentage-based or manual timesheet) for a given month, once
// it covers all their billable days. Certification is self-service and
// irreversible from the user's side: once certified, the month's project
// declarations can no longer be edited until decertified (see
// DecertifyProjectMonth).
func (h *ProjectsHandler) CertifyProjectMonth(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	var req struct {
		Year  int `json:"year"`
		Month int `json:"month"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Year < 2020 || req.Year > 2100 || req.Month < 1 || req.Month > 12 {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	billable, declared := h.userProjectDeclaration(user.ID, req.Year, req.Month)
	if !projectDeclarationComplete(billable, declared) {
		jsonError(w, "Toute l'activité facturable doit être déclarée avant de certifier", http.StatusUnprocessableEntity)
		return
	}

	if err := h.DB.CertifyProjectMonth(user.ID, req.Year, req.Month, user.ID); err != nil {
		jsonError(w, "Erreur lors de la certification", http.StatusInternalServerError)
		return
	}

	h.DB.LogAdminAction(user.ID, "user", user.ID, "certify_project_declaration", fmt.Sprintf("%04d-%02d", req.Year, req.Month))
	slog.Info("project.certify", "actor", user.Email, "year", req.Year, "month", req.Month)
	metrics.AdminOpsTotal.WithLabelValues("project_certification", "certify", "success").Inc()

	jsonOK(w, map[string]string{"status": "ok"})
}

// DecertifyProjectMonth cancels a user's project declaration certification
// for a given month, allowing edits again. Allowed for global admins,
// activity viewers, and team leaders (scoped to their own team's members).
func (h *ProjectsHandler) DecertifyProjectMonth(w http.ResponseWriter, r *http.Request) {
	currentUser := middleware.GetUser(r)
	if currentUser == nil {
		metrics.AdminOpsTotal.WithLabelValues("project_certification", "decertify", "failure").Inc()
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
		metrics.AdminOpsTotal.WithLabelValues("project_certification", "decertify", "failure").Inc()
		jsonError(w, "Non autorisé", http.StatusForbidden)
		return
	}

	if err := h.DB.DecertifyProjectMonth(req.UserID, req.Year, req.Month); err != nil {
		jsonError(w, "Erreur lors de la décertification", http.StatusInternalServerError)
		return
	}

	h.DB.LogAdminAction(currentUser.ID, "user", req.UserID, "decertify_project_declaration", fmt.Sprintf("%04d-%02d", req.Year, req.Month))
	slog.Info("project.decertify", "actor", currentUser.Email, "target_id", req.UserID, "year", req.Year, "month", req.Month)
	metrics.AdminOpsTotal.WithLabelValues("project_certification", "decertify", "success").Inc()

	jsonOK(w, map[string]string{"status": "ok"})
}

// rejectIfProjectMonthCertified returns true (after writing an error
// response) if the given user's project declaration for the given year/month
// is already certified, blocking further edits until decertified.
func rejectIfProjectMonthCertified(w http.ResponseWriter, h *ProjectsHandler, userID int64, year, month int) bool {
	certified, err := h.DB.IsProjectMonthCertified(userID, year, month)
	if err != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return true
	}
	if certified {
		msg := i18n.T("fr")["cert.locked_warning"]
		if msg == "" {
			msg = "Déclaration certifiée : modification impossible pour ce mois."
		}
		jsonError(w, msg, http.StatusLocked)
		return true
	}
	return false
}

// yearMonthFromDate parses a "YYYY-MM-DD" date string into (year, month).
func yearMonthFromDate(date string) (year, month int, err error) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0, 0, err
	}
	return t.Year(), int(t.Month()), nil
}
