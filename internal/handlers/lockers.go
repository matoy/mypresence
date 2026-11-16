package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// AdminLockersPage renders the locker management admin page: GET /admin/lockers.
func (h *FloorplanHandler) AdminLockersPage(w http.ResponseWriter, r *http.Request) {
	sites, err := h.DB.ListSites()
	if err != nil || sites == nil {
		sites = []*models.Site{}
	}
	lockers, err := h.DB.ListLockers(0)
	if err != nil || lockers == nil {
		lockers = []models.Locker{}
	}
	users, err := h.DB.ListUsers()
	if err != nil || users == nil {
		users = []models.User{}
	}

	var selectedSiteID int64
	if s := r.URL.Query().Get("site"); s != "" {
		selectedSiteID, _ = strconv.ParseInt(s, 10, 64)
	}
	fpZones, err := h.DB.GetSiteFloorplanLockerZones()
	if err != nil || fpZones == nil {
		fpZones = map[int64][]string{}
	}

	h.Render(w, r, "admin_lockers", map[string]interface{}{
		"Sites":          sites,
		"Lockers":        lockers,
		"Users":          users,
		"SelectedSiteID": selectedSiteID,
		"FloorplanZones": fpZones,
	})
}

// AdminListLockersAPI handles GET /api/admin/lockers.
func (h *FloorplanHandler) AdminListLockersAPI(w http.ResponseWriter, r *http.Request) {
	var siteID int64
	if s := r.URL.Query().Get("site_id"); s != "" {
		siteID, _ = strconv.ParseInt(s, 10, 64)
	}
	lockers, err := h.DB.ListLockers(siteID)
	if err != nil {
		jsonError(w, "Erreur lors de la récupération des casiers", http.StatusInternalServerError)
		return
	}
	if lockers == nil {
		lockers = []models.Locker{}
	}
	jsonOK(w, lockers)
}

// CreateLocker handles POST /admin/lockers and POST /api/admin/lockers.
func (h *FloorplanHandler) CreateLocker(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SiteID int64  `json:"site_id"`
		Number string `json:"number"`
		Zone   string `json:"zone"`
		Status string `json:"status"`
		UserID int64  `json:"user_id"`
		Code   string `json:"code"`
		Notes  string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}
	req.Number = strings.TrimSpace(req.Number)
	req.Zone = strings.TrimSpace(req.Zone)
	req.Code = strings.TrimSpace(req.Code)
	req.Notes = strings.TrimSpace(req.Notes)
	if req.SiteID <= 0 || req.Number == "" {
		jsonError(w, "Site et numéro de casier requis", http.StatusBadRequest)
		return
	}
	if len(req.Code) > 32 {
		req.Code = req.Code[:32]
	}

	site, err := h.DB.GetSite(req.SiteID)
	if err != nil {
		jsonError(w, "Site introuvable", http.StatusNotFound)
		return
	}

	if req.UserID > 0 {
		targetUser, uErr := h.DB.GetUserByID(req.UserID)
		if uErr != nil || targetUser == nil || targetUser.SiteID != req.SiteID {
			jsonError(w, "lockers.error_wrong_site", http.StatusBadRequest)
			return
		}
	}

	// Check duplicate number on the same site
	if existing, err := h.DB.ListLockers(req.SiteID); err == nil {
		for _, l := range existing {
			if strings.EqualFold(strings.TrimSpace(l.Number), req.Number) {
				jsonError(w, "lockers.error_exists", http.StatusConflict)
				return
			}
		}
	}

	id, err := h.DB.CreateLocker(models.Locker{
		SiteID: req.SiteID,
		Number: req.Number,
		Zone:   req.Zone,
		Status: req.Status,
		UserID: req.UserID,
		Code:   req.Code,
		Notes:  req.Notes,
	})
	if err != nil {
		msgLower := strings.ToLower(err.Error())
		if strings.Contains(msgLower, "unique") || strings.Contains(msgLower, "duplicate") {
			jsonError(w, "lockers.error_exists", http.StatusConflict)
			return
		}
		jsonError(w, "Erreur lors de la création du casier", http.StatusInternalServerError)
		return
	}

	actor := middleware.GetUser(r)
	var actorID int64
	actorName := "Admin"
	if actor != nil {
		actorID = actor.ID
		if actor.Name != "" {
			actorName = actor.Name
		}
		h.DB.LogAdminAction(actor.ID, "locker", id, "create", fmt.Sprintf("%s #%s", site.Name, req.Number))
		slog.Info("admin.locker.create", "actor", actor.Email, "locker_id", id, "site_id", req.SiteID, "number", req.Number)
	}

	if req.UserID > 0 {
		link := fmt.Sprintf("#%s (%s)", req.Number, site.Name)
		notifTitle := "Casier attribué"
		notifMsg := fmt.Sprintf("Le casier %s vous a été attribué par %s.", link, actorName)
		h.DB.CreateNotification(req.UserID, actorID, "locker_assigned", notifTitle, notifMsg, link) //nolint:errcheck
	}

	jsonOK(w, map[string]interface{}{"id": id, "number": req.Number})
}

// BulkCreateLockers handles POST /admin/lockers/bulk and POST /api/admin/lockers/bulk.
func (h *FloorplanHandler) BulkCreateLockers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SiteID   int64  `json:"site_id"`
		Prefix   string `json:"prefix"`
		StartNum int    `json:"start_num"`
		EndNum   int    `json:"end_num"`
		PadWidth int    `json:"pad_width"`
		Zone     string `json:"zone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}
	if req.SiteID <= 0 || req.StartNum < 0 || req.EndNum < req.StartNum || (req.EndNum-req.StartNum+1) > 500 {
		jsonError(w, "Paramètres de série invalides (max 500 casiers)", http.StatusBadRequest)
		return
	}
	site, err := h.DB.GetSite(req.SiteID)
	if err != nil {
		jsonError(w, "Site introuvable", http.StatusNotFound)
		return
	}

	created, err := h.DB.BulkCreateLockers(req.SiteID, req.Prefix, req.StartNum, req.EndNum, req.PadWidth, req.Zone)
	if err != nil {
		jsonError(w, "Erreur lors de la génération en lot", http.StatusInternalServerError)
		return
	}

	actor := middleware.GetUser(r)
	if actor != nil {
		h.DB.LogAdminAction(actor.ID, "locker", req.SiteID, "bulk_create", fmt.Sprintf("%s count=%d", site.Name, created))
		slog.Info("admin.locker.bulk_create", "actor", actor.Email, "site_id", req.SiteID, "created", created)
	}

	jsonOK(w, map[string]interface{}{"created": created})
}

// UpdateLocker handles PUT /admin/lockers/{id} and PUT /api/admin/lockers/{id}.
func (h *FloorplanHandler) UpdateLocker(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}
	existing, err := h.DB.GetLocker(id)
	if err != nil {
		if errors.Is(err, db.ErrLockerNotFound) {
			jsonError(w, "Casier introuvable", http.StatusNotFound)
			return
		}
		jsonError(w, "Erreur serveur", http.StatusInternalServerError)
		return
	}

	var req struct {
		Number string `json:"number"`
		Zone   string `json:"zone"`
		Status string `json:"status"`
		UserID int64  `json:"user_id"`
		Code   string `json:"code"`
		Notes  string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}
	req.Number = strings.TrimSpace(req.Number)
	if req.Number == "" {
		jsonError(w, "Numéro de casier requis", http.StatusBadRequest)
		return
	}
	req.Zone = strings.TrimSpace(req.Zone)
	req.Code = strings.TrimSpace(req.Code)
	req.Notes = strings.TrimSpace(req.Notes)
	if len(req.Code) > 32 {
		req.Code = req.Code[:32]
	}

	if req.UserID > 0 {
		targetUser, uErr := h.DB.GetUserByID(req.UserID)
		if uErr != nil || targetUser == nil || targetUser.SiteID != existing.SiteID {
			jsonError(w, "lockers.error_wrong_site", http.StatusBadRequest)
			return
		}
	}

	assignedAt := existing.AssignedAt
	reassigned := req.UserID > 0 && existing.UserID != req.UserID
	if req.UserID == 0 {
		assignedAt = nil
	} else if reassigned {
		assignedAt = nil // DB layer will set time.Now()
	}
	if req.Code == "" && existing.Code != "" && (req.UserID == 0 || reassigned) {
		req.Code = existing.Code
	}

	err = h.DB.UpdateLocker(models.Locker{
		ID:         id,
		SiteID:     existing.SiteID,
		Number:     req.Number,
		Zone:       req.Zone,
		Status:     req.Status,
		UserID:     req.UserID,
		Code:       req.Code,
		Notes:      req.Notes,
		AssignedAt: assignedAt,
	})
	if err != nil {
		msgLower := strings.ToLower(err.Error())
		if strings.Contains(msgLower, "unique") || strings.Contains(msgLower, "duplicate") {
			jsonError(w, "lockers.error_exists", http.StatusConflict)
			return
		}
		jsonError(w, "Erreur lors de la mise à jour", http.StatusInternalServerError)
		return
	}

	actor := middleware.GetUser(r)
	var actorID int64
	actorName := "Admin"
	if actor != nil {
		actorID = actor.ID
		if actor.Name != "" {
			actorName = actor.Name
		}
		h.DB.LogAdminAction(actor.ID, "locker", id, "update", fmt.Sprintf("%s #%s", existing.SiteName, req.Number))
		slog.Info("admin.locker.update", "actor", actor.Email, "locker_id", id, "number", req.Number, "user_id", req.UserID)
	}

	if reassigned && req.UserID != actorID {
		link := fmt.Sprintf("#%s (%s)", req.Number, existing.SiteName)
		if existing.SiteName == "" {
			link = "#" + req.Number
		}
		notifTitle := "Casier attribué"
		notifMsg := fmt.Sprintf("Le casier %s vous a été attribué par %s.", link, actorName)
		h.DB.CreateNotification(req.UserID, actorID, "locker_assigned", notifTitle, notifMsg, link) //nolint:errcheck
	}

	jsonOK(w, map[string]string{"status": "ok"})
}

// DeleteLocker handles DELETE /admin/lockers/{id} and DELETE /api/admin/lockers/{id}.
func (h *FloorplanHandler) DeleteLocker(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}
	existing, _ := h.DB.GetLocker(id)
	if err := h.DB.DeleteLocker(id); err != nil {
		if errors.Is(err, db.ErrLockerNotFound) {
			jsonError(w, "Casier introuvable", http.StatusNotFound)
			return
		}
		jsonError(w, "Erreur lors de la suppression", http.StatusInternalServerError)
		return
	}
	actor := middleware.GetUser(r)
	if actor != nil {
		details := strconv.FormatInt(id, 10)
		if existing != nil {
			details = fmt.Sprintf("%s #%s", existing.SiteName, existing.Number)
		}
		h.DB.LogAdminAction(actor.ID, "locker", id, "delete", details)
		slog.Info("admin.locker.delete", "actor", actor.Email, "locker_id", id)
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// --- Floorplan locker range markers (Admin) ---

// AdminListLockerRanges handles GET /api/admin/locker-ranges?floorplan_id=.
func (h *FloorplanHandler) AdminListLockerRanges(w http.ResponseWriter, r *http.Request) {
	fpID, _ := strconv.ParseInt(r.URL.Query().Get("floorplan_id"), 10, 64)
	if fpID <= 0 {
		jsonOK(w, []models.FloorplanLockerRange{})
		return
	}
	ranges, err := h.DB.ListFloorplanLockerRanges(fpID)
	if err != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	if ranges == nil {
		ranges = []models.FloorplanLockerRange{}
	}
	jsonOK(w, ranges)
}

// CreateLockerRange handles POST /admin/floorplans/{id}/locker-ranges and POST /api/admin/floorplans/{id}/locker-ranges.
func (h *FloorplanHandler) CreateLockerRange(w http.ResponseWriter, r *http.Request) {
	fpID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || fpID <= 0 {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}
	var req struct {
		Label string  `json:"label"`
		Zone  string  `json:"zone"`
		XPct  float64 `json:"x_pct"`
		YPct  float64 `json:"y_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	req.Zone = strings.TrimSpace(req.Zone)
	if req.Label == "" {
		jsonError(w, "Libellé de plage requis", http.StatusBadRequest)
		return
	}
	id, err := h.DB.CreateFloorplanLockerRange(models.FloorplanLockerRange{
		FloorplanID: fpID,
		Label:       req.Label,
		Zone:        req.Zone,
		XPct:        req.XPct,
		YPct:        req.YPct,
	})
	if err != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{
		"id":           id,
		"floorplan_id": fpID,
		"label":        req.Label,
		"zone":         req.Zone,
		"x_pct":        req.XPct,
		"y_pct":        req.YPct,
	})
}

// UpdateLockerRange handles PUT /admin/locker-ranges/{id} and PUT /api/admin/locker-ranges/{id}.
func (h *FloorplanHandler) UpdateLockerRange(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}
	existing, err := h.DB.GetFloorplanLockerRange(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "Introuvable", http.StatusNotFound)
			return
		}
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	var req struct {
		Label *string  `json:"label"`
		Zone  *string  `json:"zone"`
		XPct  *float64 `json:"x_pct"`
		YPct  *float64 `json:"y_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}
	if req.Label != nil {
		lbl := strings.TrimSpace(*req.Label)
		if lbl == "" {
			jsonError(w, "Libellé de plage requis", http.StatusBadRequest)
			return
		}
		existing.Label = lbl
	}
	if req.Zone != nil {
		existing.Zone = strings.TrimSpace(*req.Zone)
	}
	if req.XPct != nil {
		existing.XPct = *req.XPct
	}
	if req.YPct != nil {
		existing.YPct = *req.YPct
	}

	if err := h.DB.UpdateFloorplanLockerRange(*existing); err != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// DeleteLockerRange handles DELETE /admin/locker-ranges/{id} and DELETE /api/admin/locker-ranges/{id}.
func (h *FloorplanHandler) DeleteLockerRange(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}
	if err := h.DB.DeleteFloorplanLockerRange(id); err != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// --- Collaborator Self-Service APIs ---

// ListUserLockersAPI handles GET /api/lockers.
func (h *FloorplanHandler) ListUserLockersAPI(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Non authentifié", http.StatusUnauthorized)
		return
	}
	myLockers, err := h.DB.GetUserLockers(user.ID)
	if err != nil || myLockers == nil {
		myLockers = []models.Locker{}
	}
	var sites []*models.Site
	if user.SiteID > 0 {
		if allSites, err := h.DB.ListSitesWithLockers(); err == nil {
			for _, s := range allSites {
				if s.ID == user.SiteID {
					sites = append(sites, s)
				}
			}
		}
	}
	if sites == nil {
		sites = []*models.Site{}
	}

	var available []models.Locker
	if sStr := r.URL.Query().Get("site_id"); sStr != "" {
		if siteID, err := strconv.ParseInt(sStr, 10, 64); err == nil && siteID > 0 && siteID == user.SiteID {
			available, _ = h.DB.ListAvailableLockersBySite(siteID)
		}
	} else if user.SiteID > 0 {
		available, _ = h.DB.ListAvailableLockersBySite(user.SiteID)
	}
	if available == nil {
		available = []models.Locker{}
	}

	jsonOK(w, map[string]interface{}{
		"my_lockers": myLockers,
		"available":  available,
		"sites":      sites,
	})
}

// ClaimLockerAPI handles POST /api/lockers/{id}/claim.
func (h *FloorplanHandler) ClaimLockerAPI(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Non authentifié", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	req.Code = strings.TrimSpace(req.Code)
	if len(req.Code) > 32 {
		req.Code = req.Code[:32]
	}

	err = h.DB.ClaimLocker(id, user.ID, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrLockerNotFound):
			jsonError(w, "Casier introuvable", http.StatusNotFound)
		case errors.Is(err, db.ErrLockerWrongSite):
			jsonError(w, "Vous ne pouvez choisir un casier que sur votre site d'affectation", http.StatusForbidden)
		case errors.Is(err, db.ErrUserAlreadyHasLockerOnSite):
			jsonError(w, "Vous avez déjà un casier sur ce site", http.StatusConflict)
		case errors.Is(err, db.ErrLockerAlreadyTaken):
			jsonError(w, "Ce casier n'est plus disponible", http.StatusConflict)
		default:
			jsonError(w, "Erreur lors de l'attribution", http.StatusInternalServerError)
		}
		return
	}

	slog.Info("user.locker.claim", "user", user.Email, "locker_id", id)
	jsonOK(w, map[string]string{"status": "ok"})
}

// UpdateMyLockerCodeAPI handles PUT /api/lockers/{id}/code.
func (h *FloorplanHandler) UpdateMyLockerCodeAPI(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Non authentifié", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	if len(req.Code) > 32 {
		req.Code = req.Code[:32]
	}

	if err := h.DB.UpdateUserLockerCode(id, user.ID, req.Code); err != nil {
		if errors.Is(err, db.ErrLockerNotFound) {
			jsonError(w, "Casier introuvable ou non attribué à votre compte", http.StatusNotFound)
			return
		}
		jsonError(w, "Erreur lors de la mise à jour du code", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// ReleaseMyLockerAPI handles POST /api/lockers/{id}/release and DELETE /api/lockers/{id}/claim.
func (h *FloorplanHandler) ReleaseMyLockerAPI(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Non authentifié", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}
	if err := h.DB.ReleaseUserLocker(id, user.ID); err != nil {
		if errors.Is(err, db.ErrLockerNotFound) {
			jsonError(w, "Casier introuvable ou non attribué à votre compte", http.StatusNotFound)
			return
		}
		jsonError(w, "Erreur lors de la libération", http.StatusInternalServerError)
		return
	}
	slog.Info("user.locker.release", "user", user.Email, "locker_id", id)
	jsonOK(w, map[string]string{"status": "ok"})
}
