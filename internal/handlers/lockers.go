package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/i18n"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

const adminLockersPageSize = 50

// requestTranslations resolves the i18n map matching the caller's language cookie, user preference, or config default.
func requestTranslations(r *http.Request, cfg *config.Config, user *models.User) map[string]string {
	defaultLang := "en"
	if cfg != nil && cfg.DefaultLang != "" {
		defaultLang = cfg.DefaultLang
	}
	if user != nil && user.Language != "" {
		defaultLang = user.Language
	}
	return i18n.T(i18n.LangFromRequest(r, defaultLang))
}

func (h *FloorplanHandler) tr(r *http.Request) map[string]string {
	return requestTranslations(r, h.Config, middleware.GetUser(r))
}

func trOr(tr map[string]string, key, fallback string) string {
	if v := tr[key]; v != "" {
		return v
	}
	return fallback
}

// autoReleaseAndNotifyUserLockers releases lockers no longer eligible for targetUserID
// (keeping lockers on keepSiteID when > 0), logs audit entries, and notifies locker admins.
func autoReleaseAndNotifyUserLockers(
	database *db.DB,
	actor, targetUser *models.User,
	targetUserID, keepSiteID int64,
	reasonKey string,
	tr map[string]string,
) {
	if database == nil || targetUserID <= 0 {
		return
	}
	released, err := database.ReleaseLockersForUser(targetUserID, keepSiteID)
	if err != nil {
		slog.Error("locker.auto_release.failed", "user_id", targetUserID, "error", err)
		return
	}
	if len(released) == 0 {
		return
	}

	targetDisplay := "#" + strconv.FormatInt(targetUserID, 10)
	if targetUser != nil {
		if targetUser.Name != "" {
			targetDisplay = targetUser.Name
		} else if targetUser.Email != "" {
			targetDisplay = targetUser.Email
		}
	}
	reasonLabel := trOr(tr, reasonKey, strings.TrimPrefix(reasonKey, "lockers.reason_"))

	var actorID int64
	if actor != nil {
		actorID = actor.ID
	}

	adminIDs, _ := database.GetLockerAdminUserIDs()
	seen := make(map[int64]bool, len(adminIDs)+1)
	recipients := make([]int64, 0, len(adminIDs)+1)
	for _, id := range adminIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			recipients = append(recipients, id)
		}
	}
	if len(recipients) == 0 && actorID > 0 {
		recipients = append(recipients, actorID)
	}

	title := trOr(tr, "notifications.locker_auto_released_title", "Automatic locker release")
	msgTpl := trOr(tr, "notifications.locker_auto_released_msg", "Locker %s held by %s was automatically released (%s). Physical code preserved.")

	for _, lk := range released {
		siteName := lk.SiteName
		if siteName == "" {
			siteName = fmt.Sprintf("Site #%d", lk.SiteID)
		}
		link := fmt.Sprintf("#%s (%s)", lk.Number, siteName)
		database.LogAdminAction(actorID, "locker", lk.ID, "auto_release", fmt.Sprintf("%s — %s (%s)", link, targetDisplay, reasonLabel))
		slog.Info("admin.locker.auto_release", "locker_id", lk.ID, "locker", lk.Number, "site_id", lk.SiteID, "previous_user_id", targetUserID, "reason", reasonKey, "actor_id", actorID)
		if len(recipients) > 0 {
			msg := fmt.Sprintf(msgTpl, link, targetDisplay, reasonLabel)
			database.CreateNotificationsBatch(recipients, actorID, "locker_auto_released", title, msg, link) //nolint:errcheck
		}
	}
}

func validateLockerFields(number, zone, code string, tr map[string]string) string {
	if number == "" {
		return trOr(tr, "lockers.error_invalid_request", "Invalid request.")
	}
	if len(number) > 64 || len(zone) > 128 {
		return trOr(tr, "lockers.error_invalid_request", "Invalid request.")
	}
	if len(code) > 32 {
		return trOr(tr, "lockers.error_code_too_long", "Code must be 32 characters or fewer.")
	}
	return ""
}

func validateRangePct(x, y float64) bool {
	if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
		return false
	}
	return x >= 0 && x <= 100 && y >= 0 && y <= 100
}

func (h *FloorplanHandler) validateAdminLockerAssignment(siteID, userID, excludeLockerID int64, tr map[string]string) (int, string) {
	if userID <= 0 {
		return 0, ""
	}
	targetUser, err := h.DB.GetUserByID(userID)
	if err != nil || targetUser == nil || targetUser.SiteID != siteID {
		return http.StatusBadRequest, trOr(tr, "lockers.error_wrong_site", "The user must be assigned to the locker's site.")
	}
	if targetUser.Disabled {
		return http.StatusBadRequest, trOr(tr, "lockers.error_user_disabled", "Cannot assign a locker to a disabled account.")
	}
	hasOther, err := h.DB.UserHasLockerOnSite(siteID, userID, excludeLockerID)
	if err == nil && hasOther {
		return http.StatusConflict, trOr(tr, "lockers.error_user_already_has_locker", "This user already has a locker assigned on this site.")
	}
	return 0, ""
}

// AdminLockersPage renders the locker management admin page: GET /admin/lockers.
func (h *FloorplanHandler) AdminLockersPage(w http.ResponseWriter, r *http.Request) {
	sites, err := h.DB.ListSites()
	if err != nil || sites == nil {
		sites = []*models.Site{}
	}

	var selectedSiteID int64
	if s := r.URL.Query().Get("site"); s != "" {
		selectedSiteID, _ = strconv.ParseInt(s, 10, 64)
	}

	lockers, totalLockers, stats, zones, err := h.DB.ListLockersFiltered(selectedSiteID, "", "", "", 1, adminLockersPageSize)
	if err != nil || lockers == nil {
		lockers = []models.Locker{}
	}
	userOptions, err := h.DB.ListLockerUserOptions(selectedSiteID, "", adminLockersPageSize)
	if err != nil || userOptions == nil {
		userOptions = []models.LockerUserOption{}
	}
	fpZones, err := h.DB.GetSiteFloorplanLockerZones()
	if err != nil || fpZones == nil {
		fpZones = map[int64][]string{}
	}

	h.Render(w, r, "admin_lockers", map[string]interface{}{
		"Sites":          sites,
		"Lockers":        lockers,
		"TotalLockers":   totalLockers,
		"Stats":          stats,
		"Zones":          zones,
		"PageSize":       adminLockersPageSize,
		"Users":          userOptions,
		"SelectedSiteID": selectedSiteID,
		"FloorplanZones": fpZones,
	})
}

// AdminListLockersAPI handles GET /api/admin/lockers.
// When ?page=N (> 0) is specified, returns a paginated envelope {items, total, page, limit, total_pages, stats, zones};
// otherwise returns the raw locker slice for backwards compatibility.
func (h *FloorplanHandler) AdminListLockersAPI(w http.ResponseWriter, r *http.Request) {
	tr := h.tr(r)
	q := r.URL.Query()
	var siteID int64
	if s := q.Get("site_id"); s != "" {
		siteID, _ = strconv.ParseInt(s, 10, 64)
	}

	page, _ := strconv.Atoi(q.Get("page"))
	if page > 0 {
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit <= 0 || limit > 200 {
			limit = adminLockersPageSize
		}
		items, total, stats, zones, err := h.DB.ListLockersFiltered(
			siteID, q.Get("zone"), q.Get("status"), q.Get("q"), page, limit,
		)
		if err != nil {
			jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
			return
		}
		totalPages := 1
		if total > 0 {
			totalPages = (total + limit - 1) / limit
		}
		jsonOK(w, map[string]interface{}{
			"items":       items,
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
			"stats":       stats,
			"zones":       zones,
		})
		return
	}

	lockers, err := h.DB.ListLockers(siteID)
	if err != nil {
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		return
	}
	if lockers == nil {
		lockers = []models.Locker{}
	}
	jsonOK(w, lockers)
}

// AdminListLockerUsersAPI handles GET /api/admin/lockers/users?site_id=&q=&limit=.
func (h *FloorplanHandler) AdminListLockerUsersAPI(w http.ResponseWriter, r *http.Request) {
	tr := h.tr(r)
	q := r.URL.Query()
	siteID, _ := strconv.ParseInt(q.Get("site_id"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	users, err := h.DB.ListLockerUserOptions(siteID, q.Get("q"), limit)
	if err != nil {
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		return
	}
	jsonOK(w, users)
}

// CreateLocker handles POST /admin/lockers and POST /api/admin/lockers.
func (h *FloorplanHandler) CreateLocker(w http.ResponseWriter, r *http.Request) {
	tr := h.tr(r)
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
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid request."), http.StatusBadRequest)
		return
	}
	req.Number = strings.TrimSpace(req.Number)
	req.Zone = strings.TrimSpace(req.Zone)
	req.Code = strings.TrimSpace(req.Code)
	req.Notes = strings.TrimSpace(req.Notes)
	if req.SiteID <= 0 || req.Number == "" {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Site and locker number are required."), http.StatusBadRequest)
		return
	}
	if msg := validateLockerFields(req.Number, req.Zone, req.Code, tr); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}

	site, err := h.DB.GetSite(req.SiteID)
	if err != nil {
		jsonError(w, trOr(tr, "lockers.error_not_found", "Site not found."), http.StatusNotFound)
		return
	}

	if code, msg := h.validateAdminLockerAssignment(req.SiteID, req.UserID, 0, tr); code != 0 {
		jsonError(w, msg, code)
		return
	}

	// Check duplicate number on the same site
	if existing, err := h.DB.ListLockers(req.SiteID); err == nil {
		for _, l := range existing {
			if strings.EqualFold(strings.TrimSpace(l.Number), req.Number) {
				jsonError(w, trOr(tr, "lockers.error_exists", "A locker with this number already exists on this site."), http.StatusConflict)
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
			if strings.Contains(msgLower, "site_user") || strings.Contains(msgLower, "user_id") {
				jsonError(w, trOr(tr, "lockers.error_user_already_has_locker", "This user already has a locker assigned on this site."), http.StatusConflict)
				return
			}
			jsonError(w, trOr(tr, "lockers.error_exists", "A locker with this number already exists on this site."), http.StatusConflict)
			return
		}
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
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
		notifTitle := trOr(tr, "notifications.locker_assigned_title", "Locker assignment")
		notifMsg := fmt.Sprintf(trOr(tr, "notifications.locker_assigned_msg", "Locker %s was assigned to you by %s."), link, actorName)
		h.DB.CreateNotification(req.UserID, actorID, "locker_assigned", notifTitle, notifMsg, link) //nolint:errcheck
	}

	jsonOK(w, map[string]interface{}{"id": id, "number": req.Number})
}

// BulkCreateLockers handles POST /admin/lockers/bulk and POST /api/admin/lockers/bulk.
func (h *FloorplanHandler) BulkCreateLockers(w http.ResponseWriter, r *http.Request) {
	tr := h.tr(r)
	var req struct {
		SiteID   int64  `json:"site_id"`
		Prefix   string `json:"prefix"`
		StartNum int    `json:"start_num"`
		EndNum   int    `json:"end_num"`
		PadWidth int    `json:"pad_width"`
		Zone     string `json:"zone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid request."), http.StatusBadRequest)
		return
	}
	req.Prefix = strings.TrimSpace(req.Prefix)
	req.Zone = strings.TrimSpace(req.Zone)
	if req.SiteID <= 0 || req.StartNum < 0 || req.EndNum < req.StartNum || (req.EndNum-req.StartNum+1) > 500 || len(req.Prefix) > 16 || len(req.Zone) > 128 {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid bulk range parameters."), http.StatusBadRequest)
		return
	}
	site, err := h.DB.GetSite(req.SiteID)
	if err != nil {
		jsonError(w, trOr(tr, "lockers.error_not_found", "Site not found."), http.StatusNotFound)
		return
	}

	created, err := h.DB.BulkCreateLockers(req.SiteID, req.Prefix, req.StartNum, req.EndNum, req.PadWidth, req.Zone)
	if err != nil {
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
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
	tr := h.tr(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid ID."), http.StatusBadRequest)
		return
	}
	existing, err := h.DB.GetLocker(id)
	if err != nil {
		if errors.Is(err, db.ErrLockerNotFound) {
			jsonError(w, trOr(tr, "lockers.error_not_found", "Locker not found."), http.StatusNotFound)
			return
		}
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		return
	}

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
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid request."), http.StatusBadRequest)
		return
	}
	req.Number = strings.TrimSpace(req.Number)
	req.Zone = strings.TrimSpace(req.Zone)
	req.Code = strings.TrimSpace(req.Code)
	req.Notes = strings.TrimSpace(req.Notes)
	if msg := validateLockerFields(req.Number, req.Zone, req.Code, tr); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}

	targetSiteID := existing.SiteID
	targetSiteName := existing.SiteName
	if req.SiteID > 0 && req.SiteID != existing.SiteID {
		site, sErr := h.DB.GetSite(req.SiteID)
		if sErr != nil {
			jsonError(w, trOr(tr, "lockers.error_not_found", "Site not found."), http.StatusNotFound)
			return
		}
		targetSiteID = req.SiteID
		targetSiteName = site.Name
	}

	if code, msg := h.validateAdminLockerAssignment(targetSiteID, req.UserID, id, tr); code != 0 {
		jsonError(w, msg, code)
		return
	}

	// Check duplicate number on target site
	if siteLockers, lErr := h.DB.ListLockers(targetSiteID); lErr == nil {
		for _, l := range siteLockers {
			if l.ID != id && strings.EqualFold(strings.TrimSpace(l.Number), req.Number) {
				jsonError(w, trOr(tr, "lockers.error_exists", "A locker with this number already exists on this site."), http.StatusConflict)
				return
			}
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
		SiteID:     targetSiteID,
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
			if strings.Contains(msgLower, "site_user") || strings.Contains(msgLower, "user_id") {
				jsonError(w, trOr(tr, "lockers.error_user_already_has_locker", "This user already has a locker assigned on this site."), http.StatusConflict)
				return
			}
			jsonError(w, trOr(tr, "lockers.error_exists", "A locker with this number already exists on this site."), http.StatusConflict)
			return
		}
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
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
		h.DB.LogAdminAction(actor.ID, "locker", id, "update", fmt.Sprintf("%s #%s", targetSiteName, req.Number))
		slog.Info("admin.locker.update", "actor", actor.Email, "locker_id", id, "site_id", targetSiteID, "number", req.Number, "user_id", req.UserID)
	}

	if reassigned && req.UserID != actorID {
		link := fmt.Sprintf("#%s (%s)", req.Number, targetSiteName)
		if targetSiteName == "" {
			link = "#" + req.Number
		}
		notifTitle := trOr(tr, "notifications.locker_assigned_title", "Locker assignment")
		notifMsg := fmt.Sprintf(trOr(tr, "notifications.locker_assigned_msg", "Locker %s was assigned to you by %s."), link, actorName)
		h.DB.CreateNotification(req.UserID, actorID, "locker_assigned", notifTitle, notifMsg, link) //nolint:errcheck
	}

	jsonOK(w, map[string]string{"status": "ok"})
}

// DeleteLocker handles DELETE /admin/lockers/{id} and DELETE /api/admin/lockers/{id}.
func (h *FloorplanHandler) DeleteLocker(w http.ResponseWriter, r *http.Request) {
	tr := h.tr(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid ID."), http.StatusBadRequest)
		return
	}
	existing, _ := h.DB.GetLocker(id)
	if err := h.DB.DeleteLocker(id); err != nil {
		if errors.Is(err, db.ErrLockerNotFound) {
			jsonError(w, trOr(tr, "lockers.error_not_found", "Locker not found."), http.StatusNotFound)
			return
		}
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
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
	tr := h.tr(r)
	fpID, _ := strconv.ParseInt(r.URL.Query().Get("floorplan_id"), 10, 64)
	if fpID <= 0 {
		jsonOK(w, []models.FloorplanLockerRange{})
		return
	}
	ranges, err := h.DB.ListFloorplanLockerRanges(fpID)
	if err != nil {
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		return
	}
	if ranges == nil {
		ranges = []models.FloorplanLockerRange{}
	}
	jsonOK(w, ranges)
}

// CreateLockerRange handles POST /admin/floorplans/{id}/locker-ranges and POST /api/admin/floorplans/{id}/locker-ranges.
func (h *FloorplanHandler) CreateLockerRange(w http.ResponseWriter, r *http.Request) {
	tr := h.tr(r)
	fpID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || fpID <= 0 {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid ID."), http.StatusBadRequest)
		return
	}
	fp, err := h.DB.GetFloorplan(fpID)
	if err != nil || fp == nil {
		jsonError(w, trOr(tr, "lockers.error_not_found", "Floorplan not found."), http.StatusNotFound)
		return
	}

	var req struct {
		Label string  `json:"label"`
		Zone  string  `json:"zone"`
		XPct  float64 `json:"x_pct"`
		YPct  float64 `json:"y_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid request."), http.StatusBadRequest)
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	req.Zone = strings.TrimSpace(req.Zone)
	if req.Label == "" || len(req.Label) > 64 || len(req.Zone) > 128 {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Range label is required."), http.StatusBadRequest)
		return
	}
	if !validateRangePct(req.XPct, req.YPct) {
		jsonError(w, trOr(tr, "lockers.error_invalid_coords", "Coordinates x_pct and y_pct must be between 0 and 100."), http.StatusBadRequest)
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
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		return
	}
	if actor := middleware.GetUser(r); actor != nil {
		h.DB.LogAdminAction(actor.ID, "locker_range", id, "create", fmt.Sprintf("%s: %s", fp.Name, req.Label))
		slog.Info("admin.locker_range.create", "actor", actor.Email, "range_id", id, "floorplan_id", fpID, "label", req.Label)
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
	tr := h.tr(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid ID."), http.StatusBadRequest)
		return
	}
	existing, err := h.DB.GetFloorplanLockerRange(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, trOr(tr, "lockers.error_not_found", "Not found."), http.StatusNotFound)
			return
		}
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		return
	}
	var req struct {
		Label *string  `json:"label"`
		Zone  *string  `json:"zone"`
		XPct  *float64 `json:"x_pct"`
		YPct  *float64 `json:"y_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid request."), http.StatusBadRequest)
		return
	}
	if req.Label != nil {
		lbl := strings.TrimSpace(*req.Label)
		if lbl == "" || len(lbl) > 64 {
			jsonError(w, trOr(tr, "lockers.error_invalid_request", "Range label is required."), http.StatusBadRequest)
			return
		}
		existing.Label = lbl
	}
	if req.Zone != nil {
		z := strings.TrimSpace(*req.Zone)
		if len(z) > 128 {
			jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid request."), http.StatusBadRequest)
			return
		}
		existing.Zone = z
	}
	if req.XPct != nil {
		existing.XPct = *req.XPct
	}
	if req.YPct != nil {
		existing.YPct = *req.YPct
	}
	if !validateRangePct(existing.XPct, existing.YPct) {
		jsonError(w, trOr(tr, "lockers.error_invalid_coords", "Coordinates x_pct and y_pct must be between 0 and 100."), http.StatusBadRequest)
		return
	}

	if err := h.DB.UpdateFloorplanLockerRange(*existing); err != nil {
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		return
	}
	if actor := middleware.GetUser(r); actor != nil {
		h.DB.LogAdminAction(actor.ID, "locker_range", id, "update", existing.Label)
		slog.Info("admin.locker_range.update", "actor", actor.Email, "range_id", id, "label", existing.Label)
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// DeleteLockerRange handles DELETE /admin/locker-ranges/{id} and DELETE /api/admin/locker-ranges/{id}.
func (h *FloorplanHandler) DeleteLockerRange(w http.ResponseWriter, r *http.Request) {
	tr := h.tr(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid ID."), http.StatusBadRequest)
		return
	}
	existing, err := h.DB.GetFloorplanLockerRange(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, trOr(tr, "lockers.error_not_found", "Not found."), http.StatusNotFound)
			return
		}
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		return
	}
	if err := h.DB.DeleteFloorplanLockerRange(id); err != nil {
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		return
	}
	if actor := middleware.GetUser(r); actor != nil {
		h.DB.LogAdminAction(actor.ID, "locker_range", id, "delete", existing.Label)
		slog.Info("admin.locker_range.delete", "actor", actor.Email, "range_id", id, "label", existing.Label)
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// --- Collaborator Self-Service APIs ---

// ListUserLockersAPI handles GET /api/lockers.
func (h *FloorplanHandler) ListUserLockersAPI(w http.ResponseWriter, r *http.Request) {
	tr := h.tr(r)
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Unauthorized"), http.StatusUnauthorized)
		return
	}
	myLockers, err := h.DB.GetUserLockers(user.ID)
	if err != nil || myLockers == nil {
		myLockers = []models.Locker{}
	}
	sites := []*models.Site{}
	if user.SiteID > 0 {
		if s, err := h.DB.GetSite(user.SiteID); err == nil && s != nil && s.LockersCount > 0 {
			sites = append(sites, s)
		}
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
	tr := h.tr(r)
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid ID."), http.StatusBadRequest)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	req.Code = strings.TrimSpace(req.Code)
	if len(req.Code) > 32 {
		jsonError(w, trOr(tr, "lockers.error_code_too_long", "Code must be 32 characters or fewer."), http.StatusBadRequest)
		return
	}

	err = h.DB.ClaimLocker(id, user.ID, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrLockerNotFound):
			jsonError(w, trOr(tr, "lockers.error_not_found", "Locker not found."), http.StatusNotFound)
		case errors.Is(err, db.ErrLockerUserDisabled):
			jsonError(w, trOr(tr, "lockers.error_user_disabled", "Account disabled."), http.StatusForbidden)
		case errors.Is(err, db.ErrLockerWrongSite):
			jsonError(w, trOr(tr, "lockers.error_wrong_site", "The user must be assigned to the locker's site."), http.StatusForbidden)
		case errors.Is(err, db.ErrUserAlreadyHasLockerOnSite):
			jsonError(w, trOr(tr, "lockers.error_user_already_has_locker", "You already have a locker assigned on this site."), http.StatusConflict)
		case errors.Is(err, db.ErrLockerAlreadyTaken):
			jsonError(w, trOr(tr, "lockers.error_already_taken", "This locker is no longer available."), http.StatusConflict)
		default:
			jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		}
		return
	}

	slog.Info("user.locker.claim", "user", user.Email, "locker_id", id)
	jsonOK(w, map[string]string{"status": "ok"})
}

// UpdateMyLockerCodeAPI handles PUT /api/lockers/{id}/code.
func (h *FloorplanHandler) UpdateMyLockerCodeAPI(w http.ResponseWriter, r *http.Request) {
	tr := h.tr(r)
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid ID."), http.StatusBadRequest)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid request."), http.StatusBadRequest)
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	if len(req.Code) > 32 {
		jsonError(w, trOr(tr, "lockers.error_code_too_long", "Code must be 32 characters or fewer."), http.StatusBadRequest)
		return
	}

	if err := h.DB.UpdateUserLockerCode(id, user.ID, req.Code); err != nil {
		if errors.Is(err, db.ErrLockerNotFound) {
			jsonError(w, trOr(tr, "lockers.error_not_found", "Locker not found."), http.StatusNotFound)
			return
		}
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		return
	}
	slog.Info("user.locker.update_code", "user", user.Email, "locker_id", id)
	jsonOK(w, map[string]string{"status": "ok"})
}

// ReleaseMyLockerAPI handles POST /api/lockers/{id}/release and DELETE /api/lockers/{id}/claim.
func (h *FloorplanHandler) ReleaseMyLockerAPI(w http.ResponseWriter, r *http.Request) {
	tr := h.tr(r)
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, trOr(tr, "lockers.error_invalid_request", "Invalid ID."), http.StatusBadRequest)
		return
	}
	if err := h.DB.ReleaseUserLocker(id, user.ID); err != nil {
		if errors.Is(err, db.ErrLockerNotFound) {
			jsonError(w, trOr(tr, "lockers.error_not_found", "Locker not found."), http.StatusNotFound)
			return
		}
		jsonError(w, trOr(tr, "lockers.error_server", "Server error."), http.StatusInternalServerError)
		return
	}
	slog.Info("user.locker.release", "user", user.Email, "locker_id", id)
	jsonOK(w, map[string]string{"status": "ok"})
}
