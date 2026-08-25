package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// HolidaysHandler manages the public holidays admin page.
type HolidaysHandler struct {
	DB     *db.DB
	Render func(w http.ResponseWriter, r *http.Request, page string, data interface{})
}

// HolidaysPage renders the list of public holidays.
func (h *HolidaysHandler) HolidaysPage(w http.ResponseWriter, r *http.Request) {
	holidays, err := h.DB.ListHolidays()
	if err != nil {
		http.Error(w, "Error loading holidays", http.StatusInternalServerError)
		return
	}

	h.Render(w, r, "admin_holidays", map[string]interface{}{
		"Holidays":  holidays,
		"Countries": models.AllCountries,
		"Error":     r.URL.Query().Get("error"),
	})
}

// CreateHoliday handles POST /admin/holidays.
func (h *HolidaysHandler) CreateHoliday(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Date         string `json:"date"`
		Name         string `json:"name"`
		AllowImputed bool   `json:"allow_imputed"`
		CountryCode  string `json:"country_code"`
		CountryCodes string `json:"country_codes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Date == "" || req.Name == "" {
		jsonError(w, "Date and name are required", http.StatusBadRequest)
		return
	}
	cc := req.CountryCode
	if cc == "" {
		cc = req.CountryCodes
	}
	cc = strings.ToUpper(strings.TrimSpace(cc))
	id, err := h.DB.CreateHoliday(req.Date, req.Name, req.AllowImputed, cc)
	if err != nil {
		jsonError(w, "Date already exists or server error", http.StatusConflict)
		return
	}
	currentUser := middleware.GetUser(r)
	if currentUser != nil {
		details := req.Date + " " + req.Name
		if cc != "" {
			details += " [" + cc + "]"
		}
		h.DB.LogAdminAction(currentUser.ID, "holiday", id, "create", details)
		slog.Info("admin.holiday.create", "actor", currentUser.Email, "date", req.Date, "name", req.Name, "country_code", cc, "holiday_id", id)
	}
	jsonOK(w, map[string]interface{}{"id": id, "status": "ok"})
}

// UpdateHoliday handles PUT /admin/holidays/{id}.
func (h *HolidaysHandler) UpdateHoliday(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}

	var req struct {
		Date         string `json:"date"`
		Name         string `json:"name"`
		AllowImputed bool   `json:"allow_imputed"`
		CountryCode  string `json:"country_code"`
		CountryCodes string `json:"country_codes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	if req.Date == "" || req.Name == "" {
		jsonError(w, "Date et nom requis", http.StatusBadRequest)
		return
	}

	cc := req.CountryCode
	if cc == "" {
		cc = req.CountryCodes
	}
	cc = strings.ToUpper(strings.TrimSpace(cc))
	if err := h.DB.UpdateHoliday(id, req.Date, req.Name, req.AllowImputed, cc); err != nil {
		jsonError(w, "Erreur serveur", http.StatusInternalServerError)
		return
	}

	currentUser := middleware.GetUser(r)
	if currentUser != nil {
		details := req.Date + " " + req.Name
		if cc != "" {
			details += " [" + cc + "]"
		}
		h.DB.LogAdminAction(currentUser.ID, "holiday", id, "update", details)
		slog.Info("admin.holiday.update", "actor", currentUser.Email, "date", req.Date, "name", req.Name, "country_code", cc, "holiday_id", id)
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// DeleteHoliday handles DELETE /admin/holidays/{id}.
func (h *HolidaysHandler) DeleteHoliday(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}

	holidayName := h.DB.GetHolidayName(id)

	if err := h.DB.DeleteHoliday(id); err != nil {
		jsonError(w, "Erreur suppression", http.StatusInternalServerError)
		return
	}

	currentUser := middleware.GetUser(r)
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "holiday", id, "delete", holidayName)
		slog.Info("admin.holiday.delete", "actor", currentUser.Email, "name", holidayName, "holiday_id", id)
	}
	jsonOK(w, map[string]string{"status": "ok"})
}
