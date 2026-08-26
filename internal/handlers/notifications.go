package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
)

// NotificationsHandler handles in-app notification endpoints.
type NotificationsHandler struct {
	DB *db.DB
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
