package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/metrics"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// UsersAdminHandler handles local user account management.
type UsersAdminHandler struct {
	DB     *db.DB
	Render func(w http.ResponseWriter, r *http.Request, page string, data interface{})
}

// UsersPage renders the user management page.
func (h *UsersAdminHandler) UsersPage(w http.ResponseWriter, r *http.Request) {
	users, _ := h.DB.ListUsers()
	sites, _ := h.DB.ListSites()
	teams, _ := h.DB.ListTeams()
	currentUser := middleware.GetUser(r)
	var currentUserID int64
	if currentUser != nil {
		currentUserID = currentUser.ID
	}
	h.Render(w, r, "admin_users", map[string]interface{}{
		"Users":         users,
		"Sites":         sites,
		"Teams":         teams,
		"AllRoles":      models.AllRoles,
		"CurrentUserID": currentUserID,
		"Error":         r.URL.Query().Get("error"),
	})
}

// PreprovisionUser creates a new SSO user account and optionally assigns a site and teams.
func (h *UsersAdminHandler) PreprovisionUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email   string  `json:"email"`
		Name    string  `json:"name"`
		SiteID  int64   `json:"site_id"`
		TeamIDs []int64 `json:"team_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.AdminOpsTotal.WithLabelValues("user", "preprovision", "failure").Inc()
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Name = strings.TrimSpace(req.Name)
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		metrics.AdminOpsTotal.WithLabelValues("user", "preprovision", "failure").Inc()
		jsonError(w, "Valid email is required", http.StatusBadRequest)
		return
	}

	// Check if user already exists
	if existing, _ := h.DB.GetUserByEmail(req.Email); existing != nil {
		metrics.AdminOpsTotal.WithLabelValues("user", "preprovision", "failure").Inc()
		jsonError(w, "Email already in use", http.StatusConflict)
		return
	}

	uid, err := h.DB.PreprovisionUser(req.Email, req.Name, req.SiteID)
	if err != nil {
		metrics.AdminOpsTotal.WithLabelValues("user", "preprovision", "failure").Inc()
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, tid := range req.TeamIDs {
		if tid > 0 {
			_ = h.DB.AddTeamMember(tid, uid)
		}
	}

	createdUser, _ := h.DB.GetUserByID(uid)
	if createdUser != nil {
		hydrated := h.DB.HydrateUsersTeams([]models.User{*createdUser})
		if len(hydrated) > 0 {
			createdUser = &hydrated[0]
		}
	}

	currentUser := middleware.GetUser(r)
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "user", uid, "preprovision", req.Email)
		slog.Info("admin.user.preprovision", "actor", currentUser.Email, "new_user", req.Email)
	}
	metrics.AdminOpsTotal.WithLabelValues("user", "preprovision", "success").Inc()
	jsonOK(w, map[string]interface{}{
		"id":     uid,
		"status": "ok",
		"user":   createdUser,
	})
}

// BatchPreprovisionUsers handles batch pre-provisioning from raw multiline text or CSV.
func (h *UsersAdminHandler) BatchPreprovisionUsers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text          string `json:"text"`
		DefaultSiteID int64  `json:"default_site_id"`
		DefaultTeamID int64  `json:"default_team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.AdminOpsTotal.WithLabelValues("user", "batch_preprovision", "failure").Inc()
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	lines := strings.Split(req.Text, "\n")
	var created, skipped int
	var errMsgs []string

	allSites, _ := h.DB.ListSites()
	allTeams, _ := h.DB.ListTeams()

	findSiteID := func(val string) int64 {
		val = strings.TrimSpace(val)
		if val == "" {
			return req.DefaultSiteID
		}
		if id, err := strconv.ParseInt(val, 10, 64); err == nil && id > 0 {
			return id
		}
		for _, s := range allSites {
			if strings.EqualFold(s.Name, val) {
				return s.ID
			}
		}
		return req.DefaultSiteID
	}

	findTeamID := func(val string) int64 {
		val = strings.TrimSpace(val)
		if val == "" {
			return req.DefaultTeamID
		}
		if id, err := strconv.ParseInt(val, 10, 64); err == nil && id > 0 {
			return id
		}
		for _, t := range allTeams {
			if strings.EqualFold(t.Name, val) {
				return t.ID
			}
		}
		return req.DefaultTeamID
	}

	for lineNum, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "email") || strings.HasPrefix(lower, "courriel") {
			continue
		}

		var cols []string
		if strings.Contains(line, ";") {
			cols = strings.Split(line, ";")
		} else if strings.Contains(line, "\t") {
			cols = strings.Split(line, "\t")
		} else if strings.Contains(line, ",") {
			cols = strings.Split(line, ",")
		} else {
			cols = []string{line}
		}

		email := strings.ToLower(strings.TrimSpace(cols[0]))
		if email == "" || !strings.Contains(email, "@") {
			errMsgs = append(errMsgs, fmt.Sprintf("Line %d: invalid email '%s'", lineNum+1, cols[0]))
			continue
		}

		var name string
		if len(cols) > 1 {
			name = strings.TrimSpace(cols[1])
		}
		siteID := req.DefaultSiteID
		if len(cols) > 2 && strings.TrimSpace(cols[2]) != "" {
			siteID = findSiteID(cols[2])
		}
		teamID := req.DefaultTeamID
		if len(cols) > 3 && strings.TrimSpace(cols[3]) != "" {
			teamID = findTeamID(cols[3])
		}

		existing, _ := h.DB.GetUserByEmail(email)
		if existing != nil {
			skipped++
			if teamID > 0 {
				_ = h.DB.AddTeamMember(teamID, existing.ID)
			}
			continue
		}

		uid, err := h.DB.PreprovisionUser(email, name, siteID)
		if err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("Line %d (%s): %v", lineNum+1, email, err))
			continue
		}

		if teamID > 0 {
			_ = h.DB.AddTeamMember(teamID, uid)
		}
		created++
	}

	currentUser := middleware.GetUser(r)
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "user", 0, "batch_preprovision", fmt.Sprintf("created=%d, skipped=%d", created, skipped))
		slog.Info("admin.user.batch_preprovision", "actor", currentUser.Email, "created", created, "skipped", skipped)
	}
	metrics.AdminOpsTotal.WithLabelValues("user", "batch_preprovision", "success").Inc()

	users, _ := h.DB.ListUsers()
	jsonOK(w, map[string]interface{}{
		"status":  "ok",
		"created": created,
		"skipped": skipped,
		"errors":  errMsgs,
		"users":   users,
	})
}

// CreateUser creates a new local user account.
func (h *UsersAdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.AdminOpsTotal.WithLabelValues("user", "create", "failure").Inc()
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.Name = strings.TrimSpace(req.Name)
	if req.Email == "" || req.Name == "" || req.Password == "" {
		metrics.AdminOpsTotal.WithLabelValues("user", "create", "failure").Inc()
		jsonError(w, "All fields are required", http.StatusBadRequest)
		return
	}
	uid, err := h.DB.CreateLocalUser(req.Email, req.Name, req.Password)
	if err != nil {
		metrics.AdminOpsTotal.WithLabelValues("user", "create", "failure").Inc()
		jsonError(w, "Email already in use", http.StatusConflict)
		return
	}
	currentUser := middleware.GetUser(r)
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "user", uid, "create", req.Email)
		slog.Info("admin.user.create", "actor", currentUser.Email, "new_user", req.Email)
	}
	metrics.AdminOpsTotal.WithLabelValues("user", "create", "success").Inc()
	jsonOK(w, map[string]interface{}{"id": uid, "status": "ok"})
}

// UpdateUser updates a user's email and display name.
func (h *UsersAdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	req.Email = strings.TrimSpace(req.Email)
	req.Name = strings.TrimSpace(req.Name)
	if req.Email == "" || req.Name == "" {
		metrics.AdminOpsTotal.WithLabelValues("user", "update", "failure").Inc()
		jsonError(w, "Email and name are required", http.StatusBadRequest)
		return
	}
	if err := h.DB.UpdateLocalUser(id, req.Email, req.Name); err != nil {
		metrics.AdminOpsTotal.WithLabelValues("user", "update", "failure").Inc()
		jsonError(w, "Email already in use", http.StatusConflict)
		return
	}
	currentUser := middleware.GetUser(r)
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "user", id, "update", req.Email+" "+req.Name)
		slog.Info("admin.user.update", "actor", currentUser.Email, "target_id", id, "email", req.Email)
	}
	metrics.AdminOpsTotal.WithLabelValues("user", "update", "success").Inc()
	jsonOK(w, map[string]string{"status": "ok"})
}

// SetPassword changes the password of a local account.
func (h *UsersAdminHandler) SetPassword(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if req.Password == "" {
		metrics.AdminOpsTotal.WithLabelValues("user", "set_password", "failure").Inc()
		jsonError(w, "Password is required", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		metrics.AdminOpsTotal.WithLabelValues("user", "set_password", "failure").Inc()
		jsonError(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	if err := h.DB.SetUserPassword(id, req.Password); err != nil {
		metrics.AdminOpsTotal.WithLabelValues("user", "set_password", "failure").Inc()
		jsonError(w, "Error updating password", http.StatusInternalServerError)
		return
	}
	currentUser := middleware.GetUser(r)
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "user", id, "set_password", "")
	}

	// Invalidate active sessions for the reset account.
	// If the admin is resetting their own account, preserve the current session cookie.
	var exceptToken string
	if currentUser != nil && currentUser.ID == id {
		if cookie, err := r.Cookie("session"); err == nil {
			exceptToken = cookie.Value
		}
	}
	h.DB.DeleteUserSessions(id, exceptToken)

	metrics.AdminOpsTotal.WithLabelValues("user", "set_password", "success").Inc()
	jsonOK(w, map[string]string{"status": "ok"})
}

// SetDisabled enables or disables a user account.
func (h *UsersAdminHandler) SetDisabled(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	currentUser := middleware.GetUser(r)
	if currentUser != nil && currentUser.ID == id {
		metrics.AdminOpsTotal.WithLabelValues("user", "set_disabled", "failure").Inc()
		jsonError(w, "You cannot disable your own account", http.StatusBadRequest)
		return
	}
	var req struct {
		Disabled bool `json:"disabled"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if err := h.DB.SetUserDisabled(id, req.Disabled); err != nil {
		metrics.AdminOpsTotal.WithLabelValues("user", "set_disabled", "failure").Inc()
		jsonError(w, "Error updating user", http.StatusInternalServerError)
		return
	}
	if currentUser != nil {
		action := "set_enabled"
		if req.Disabled {
			action = "set_disabled"
		}
		h.DB.LogAdminAction(currentUser.ID, "user", id, action, "")
		slog.Info("admin.user.disabled", "actor", currentUser.Email, "target_id", id, "disabled", req.Disabled)
	}
	metrics.AdminOpsTotal.WithLabelValues("user", "set_disabled", "success").Inc()
	jsonOK(w, map[string]string{"status": "ok"})
}

// DeleteUser permanently deletes a user account.
func (h *UsersAdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	currentUser := middleware.GetUser(r)
	if currentUser != nil && currentUser.ID == id {
		metrics.AdminOpsTotal.WithLabelValues("user", "delete", "failure").Inc()
		jsonError(w, "You cannot delete your own account", http.StatusBadRequest)
		return
	}
	targetUser, _ := h.DB.GetUserByID(id)
	if err := h.DB.DeleteLocalUser(id); err != nil {
		metrics.AdminOpsTotal.WithLabelValues("user", "delete", "failure").Inc()
		jsonError(w, "Error deleting user", http.StatusInternalServerError)
		return
	}
	if currentUser != nil {
		details := ""
		if targetUser != nil {
			details = targetUser.Name + " <" + targetUser.Email + ">"
		}
		h.DB.LogAdminAction(currentUser.ID, "user", id, "delete", details)
		slog.Info("admin.user.delete", "actor", currentUser.Email, "target_id", id, "target_email", func() string {
			if targetUser != nil {
				return targetUser.Email
			}
			return ""
		}())
	}
	metrics.AdminOpsTotal.WithLabelValues("user", "delete", "success").Inc()
	jsonOK(w, map[string]string{"status": "ok"})
}

// UserLogsPage renders the presence log history for a specific user.
func (h *UsersAdminHandler) UserLogsPage(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	targetUser, err := h.DB.GetUserByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Parse period filter: 7 (default), 30, 90, 0 = all
	daysParam := r.URL.Query().Get("days")
	days := 7
	if daysParam != "" {
		if v, err := strconv.Atoi(daysParam); err == nil && v >= 0 {
			days = v
		}
	}
	var since time.Time
	if days > 0 {
		since = time.Now().AddDate(0, 0, -days)
	}

	logs, _ := h.DB.GetUserLogs(id, since)
	adminLogs, _ := h.DB.GetAdminLogsByActor(id, since)
	notifLogs, _ := h.DB.GetUserNotificationLogs(id, since)
	statuses, _ := h.DB.ListStatuses()
	h.Render(w, r, "admin_user_logs", map[string]interface{}{
		"TargetUser":       targetUser,
		"Logs":             logs,
		"AdminLogs":        adminLogs,
		"NotificationLogs": notifLogs,
		"Statuses":         statuses,
		"Days":             days,
		"FilterBaseURL":    "/admin/users/" + strconv.FormatInt(id, 10) + "/logs",
		"HideAdminSection": false,
	})
}
