package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// AdminSitesPage renders the sites admin page: GET /admin/sites.
func (h *FloorplanHandler) AdminSitesPage(w http.ResponseWriter, r *http.Request) {
	sites, err := h.DB.ListSites()
	if err != nil {
		sites = []*models.Site{}
	}
	fps, err := h.DB.ListFloorplans()
	if err != nil {
		fps = []models.Floorplan{}
	}

	h.Render(w, r, "admin_sites", map[string]interface{}{
		"Sites":      sites,
		"Floorplans": fps,
		"Countries":  models.AllCountries,
	})
}

// AdminListSites handles GET /api/admin/sites.
func (h *FloorplanHandler) AdminListSites(w http.ResponseWriter, r *http.Request) {
	sites, err := h.DB.ListSites()
	if err != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	if sites == nil {
		sites = []*models.Site{}
	}
	jsonOK(w, sites)
}

// AdminGetSite handles GET /api/admin/sites/{id}.
func (h *FloorplanHandler) AdminGetSite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}
	site, err := h.DB.GetSite(id)
	if err != nil {
		jsonError(w, "Site non trouvé", http.StatusNotFound)
		return
	}
	jsonOK(w, site)
}

// CreateSite handles POST /admin/sites and POST /api/admin/sites.
func (h *FloorplanHandler) CreateSite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string  `json:"name"`
		CountryCode      string  `json:"country_code"`
		NotCorporateSite bool    `json:"not_corporate_site"`
		Seats            int     `json:"seats"`
		Workstations     int     `json:"workstations"`
		FloorplanIDs     []int64 `json:"floorplan_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		jsonError(w, "sites.error_name_required", http.StatusBadRequest)
		return
	}

	// Check for duplicate site name
	if existingSites, err := h.DB.ListSites(); err == nil {
		for _, s := range existingSites {
			if strings.EqualFold(strings.TrimSpace(s.Name), req.Name) {
				jsonError(w, "sites.error_name_exists", http.StatusConflict)
				return
			}
		}
	}

	countryCode := strings.TrimSpace(req.CountryCode)
	if idx := strings.Index(countryCode, ","); idx != -1 {
		countryCode = countryCode[:idx]
	}
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))

	seats := req.Seats
	if seats <= 0 && req.Workstations > 0 {
		seats = req.Workstations
	}
	if seats < 0 {
		seats = 0
	}

	site := models.Site{
		Name:             req.Name,
		CountryCode:      countryCode,
		NotCorporateSite: req.NotCorporateSite,
		Seats:            seats,
		FloorplanIDs:     req.FloorplanIDs,
	}

	id, err := h.DB.CreateSite(site)
	if err != nil {
		jsonError(w, "Erreur lors de la création du site", http.StatusInternalServerError)
		return
	}

	actor := middleware.GetUser(r)
	if actor != nil {
		slog.Info("admin.site.create", "actor", actor.Email, "site_id", id, "name", req.Name)
	}

	jsonOK(w, map[string]interface{}{"id": id, "name": req.Name})
}

// UpdateSite handles PUT /admin/sites/{id} and PUT /api/admin/sites/{id}.
func (h *FloorplanHandler) UpdateSite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}

	var req struct {
		Name             string  `json:"name"`
		CountryCode      string  `json:"country_code"`
		NotCorporateSite bool    `json:"not_corporate_site"`
		Seats            int     `json:"seats"`
		Workstations     int     `json:"workstations"`
		FloorplanIDs     []int64 `json:"floorplan_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		jsonError(w, "sites.error_name_required", http.StatusBadRequest)
		return
	}

	// Check for duplicate site name on another site
	if existingSites, err := h.DB.ListSites(); err == nil {
		for _, s := range existingSites {
			if s.ID != id && strings.EqualFold(strings.TrimSpace(s.Name), req.Name) {
				jsonError(w, "sites.error_name_exists", http.StatusConflict)
				return
			}
		}
	}

	countryCode := strings.TrimSpace(req.CountryCode)
	if idx := strings.Index(countryCode, ","); idx != -1 {
		countryCode = countryCode[:idx]
	}
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))

	seats := req.Seats
	if seats <= 0 && req.Workstations > 0 {
		seats = req.Workstations
	}
	if seats < 0 {
		seats = 0
	}

	site := models.Site{
		ID:               id,
		Name:             req.Name,
		CountryCode:      countryCode,
		NotCorporateSite: req.NotCorporateSite,
		Seats:            seats,
		FloorplanIDs:     req.FloorplanIDs,
	}

	if err := h.DB.UpdateSite(site); err != nil {
		jsonError(w, "Erreur lors de la mise à jour du site", http.StatusInternalServerError)
		return
	}

	actor := middleware.GetUser(r)
	if actor != nil {
		slog.Info("admin.site.update", "actor", actor.Email, "site_id", id, "name", req.Name)
	}

	jsonOK(w, map[string]string{"status": "ok"})
}

// DeleteSite handles DELETE /admin/sites/{id} and DELETE /api/admin/sites/{id}.
func (h *FloorplanHandler) DeleteSite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}

	if err := h.DB.DeleteSite(id); err != nil {
		jsonError(w, "Erreur lors de la suppression du site", http.StatusInternalServerError)
		return
	}

	actor := middleware.GetUser(r)
	if actor != nil {
		slog.Info("admin.site.delete", "actor", actor.Email, "site_id", id)
	}

	jsonOK(w, map[string]string{"status": "ok"})
}
