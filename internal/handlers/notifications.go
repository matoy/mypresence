package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// NotificationsHandler handles in-app notification endpoints.
type NotificationsHandler struct {
	DB     *db.DB
	Render func(w http.ResponseWriter, r *http.Request, page string, data interface{})
}

// AcknowledgeNotification marks a notification as acknowledged for the current user.
func (h *NotificationsHandler) AcknowledgeNotification(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "Invalid notification ID", http.StatusBadRequest)
		return
	}

	if err := h.DB.AcknowledgeNotification(id, user.ID); err != nil {
		jsonError(w, "Failed to acknowledge notification", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "ok"})
}

// GetUnreadNotificationsAPI returns unacknowledged notifications for the current user.
func (h *NotificationsHandler) GetUnreadNotificationsAPI(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	notifs, err := h.DB.GetUnreadNotifications(user.ID)
	if err != nil {
		jsonError(w, "Failed to fetch notifications", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(notifs)
}

// AdminNotificationsPage renders the admin notifications management page.
func (h *NotificationsHandler) AdminNotificationsPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !user.HasRole(models.RoleGlobal) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	users, err := h.DB.ListUsers()
	if err != nil {
		http.Error(w, "Failed to list users", http.StatusInternalServerError)
		return
	}

	teams, err := h.DB.ListTeams()
	if err != nil {
		teams = nil
	}

	recentNotifs, _ := h.DB.GetAllNotifications(200)

	h.Render(w, r, "admin_notifications", map[string]interface{}{
		"Users":         users,
		"Teams":         teams,
		"Notifications": recentNotifs,
		"Success":       r.URL.Query().Get("success"),
		"Error":         r.URL.Query().Get("error"),
	})
}

// AdminSendNotification handles sending a notification from a global admin.
// Accepts both JSON and form data.
func (h *NotificationsHandler) AdminSendNotification(w http.ResponseWriter, r *http.Request) {
	currentUser := middleware.GetUser(r)
	if currentUser == nil || !currentUser.HasRole(models.RoleGlobal) {
		if strings.Contains(r.Header.Get("Accept"), "application/json") || r.Header.Get("Content-Type") == "application/json" {
			jsonError(w, "Forbidden", http.StatusForbidden)
		} else {
			http.Error(w, "Forbidden", http.StatusForbidden)
		}
		return
	}

	var recipientStr, notifType, title, message, link string
	isJSON := strings.Contains(r.Header.Get("Content-Type"), "application/json")

	if isJSON {
		var payload struct {
			Recipient string `json:"recipient"` // "all", "team:<id>", "user:<id>", or user ID
			UserID    int64  `json:"user_id"`
			Type      string `json:"type"`
			Title     string `json:"title"`
			Message   string `json:"message"`
			Link      string `json:"link"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			jsonError(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}
		if payload.Recipient != "" {
			recipientStr = payload.Recipient
		} else if payload.UserID > 0 {
			recipientStr = strconv.FormatInt(payload.UserID, 10)
		}
		notifType = payload.Type
		title = payload.Title
		message = payload.Message
		link = payload.Link
	} else {
		_ = r.ParseForm()
		recipientStr = r.FormValue("recipient")
		if recipientStr == "" {
			recipientStr = r.FormValue("user_id")
		}
		notifType = r.FormValue("type")
		title = r.FormValue("title")
		message = r.FormValue("message")
		link = r.FormValue("link")
	}

	recipientStr = strings.TrimSpace(recipientStr)
	title = strings.TrimSpace(title)
	message = strings.TrimSpace(message)
	link = strings.TrimSpace(link)
	notifType = strings.TrimSpace(notifType)
	if notifType == "" {
		notifType = "info"
	}

	if recipientStr == "" {
		if isJSON {
			jsonError(w, "Recipient is required", http.StatusBadRequest)
		} else {
			http.Redirect(w, r, "/admin/notifications?error=recipient_required", http.StatusSeeOther)
		}
		return
	}
	if title == "" {
		if isJSON {
			jsonError(w, "Title is required", http.StatusBadRequest)
		} else {
			http.Redirect(w, r, "/admin/notifications?error=title_required", http.StatusSeeOther)
		}
		return
	}
	if message == "" {
		if isJSON {
			jsonError(w, "Message is required", http.StatusBadRequest)
		} else {
			http.Redirect(w, r, "/admin/notifications?error=message_required", http.StatusSeeOther)
		}
		return
	}

	count := 0
	if recipientStr == "all" {
		users, err := h.DB.ListUsers()
		if err != nil {
			if isJSON {
				jsonError(w, "Failed to load users", http.StatusInternalServerError)
			} else {
				http.Redirect(w, r, "/admin/notifications?error=server_error", http.StatusSeeOther)
			}
			return
		}
		for _, u := range users {
			if u.Disabled {
				continue
			}
			_, _ = h.DB.CreateNotification(u.ID, currentUser.ID, notifType, title, message, link)
			count++
		}
	} else if strings.HasPrefix(recipientStr, "team:") {
		teamIDStr := strings.TrimPrefix(recipientStr, "team:")
		teamID, err := strconv.ParseInt(teamIDStr, 10, 64)
		if err != nil || teamID <= 0 {
			if isJSON {
				jsonError(w, "Invalid recipient ID", http.StatusBadRequest)
			} else {
				http.Redirect(w, r, "/admin/notifications?error=invalid_recipient", http.StatusSeeOther)
			}
			return
		}
		members, err := h.DB.GetTeamMembers(teamID)
		if err != nil {
			if isJSON {
				jsonError(w, "Failed to load team members", http.StatusInternalServerError)
			} else {
				http.Redirect(w, r, "/admin/notifications?error=server_error", http.StatusSeeOther)
			}
			return
		}
		for _, m := range members {
			if m.Disabled {
				continue
			}
			_, _ = h.DB.CreateNotification(m.ID, currentUser.ID, notifType, title, message, link)
			count++
		}
	} else {
		cleanIDStr := strings.TrimPrefix(recipientStr, "user:")
		targetUserID, err := strconv.ParseInt(cleanIDStr, 10, 64)
		if err != nil || targetUserID <= 0 {
			if isJSON {
				jsonError(w, "Invalid recipient ID", http.StatusBadRequest)
			} else {
				http.Redirect(w, r, "/admin/notifications?error=invalid_recipient", http.StatusSeeOther)
			}
			return
		}
		_, err = h.DB.CreateNotification(targetUserID, currentUser.ID, notifType, title, message, link)
		if err != nil {
			if isJSON {
				jsonError(w, "Failed to create notification", http.StatusInternalServerError)
			} else {
				http.Redirect(w, r, "/admin/notifications?error=send_failed", http.StatusSeeOther)
			}
			return
		}
		count = 1
	}

	if isJSON {
		jsonOK(w, map[string]interface{}{
			"status": "ok",
			"count":  count,
		})
	} else {
		http.Redirect(w, r, "/admin/notifications?success=1", http.StatusSeeOther)
	}
}

// AdminDeleteNotification handles deletion of a notification from the admin panel.
func (h *NotificationsHandler) AdminDeleteNotification(w http.ResponseWriter, r *http.Request) {
	currentUser := middleware.GetUser(r)
	if currentUser == nil || !currentUser.HasRole(models.RoleGlobal) {
		if strings.Contains(r.Header.Get("Accept"), "application/json") || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			jsonError(w, "Forbidden", http.StatusForbidden)
		} else {
			http.Error(w, "Forbidden", http.StatusForbidden)
		}
		return
	}

	idStr := r.PathValue("id")
	if idStr == "" {
		idStr = r.FormValue("id")
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		if strings.Contains(r.Header.Get("Accept"), "application/json") || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			jsonError(w, "Invalid notification ID", http.StatusBadRequest)
		} else {
			http.Redirect(w, r, "/admin/notifications?error=invalid_id", http.StatusSeeOther)
		}
		return
	}

	if err := h.DB.DeleteNotification(id); err != nil {
		if strings.Contains(r.Header.Get("Accept"), "application/json") || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			jsonError(w, "Failed to delete notification", http.StatusInternalServerError)
		} else {
			http.Redirect(w, r, "/admin/notifications?error=delete_failed", http.StatusSeeOther)
		}
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "application/json") || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		jsonOK(w, map[string]string{"status": "ok"})
	} else {
		http.Redirect(w, r, "/admin/notifications?deleted=1", http.StatusSeeOther)
	}
}
