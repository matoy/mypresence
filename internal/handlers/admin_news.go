package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// hexColorRE matches valid 3 or 6 digit hex color codes with leading #.
var hexColorRE = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// NewsHandler handles the news banners admin page.
type NewsHandler struct {
	DB     *db.DB
	Render func(w http.ResponseWriter, r *http.Request, page string, data interface{})
}

// GetActiveNewsAPI returns currently active news messages as JSON.
// Requires authentication only — no special role needed.
func (h *NewsHandler) GetActiveNewsAPI(w http.ResponseWriter, r *http.Request) {
	msgs, err := h.DB.GetActiveNewsMessages()
	if err != nil {
		jsonError(w, "Error loading news", http.StatusInternalServerError)
		return
	}
	if msgs == nil {
		msgs = []models.NewsMessage{}
	}
	jsonOK(w, msgs)
}

// ListNewsAPI returns all news messages as JSON. Requires activity_viewer role.
func (h *NewsHandler) ListNewsAPI(w http.ResponseWriter, r *http.Request) {
	msgs, err := h.DB.ListNewsMessages()
	if err != nil {
		jsonError(w, "Error loading news", http.StatusInternalServerError)
		return
	}
	if msgs == nil {
		msgs = []models.NewsMessage{}
	}
	jsonOK(w, msgs)
}

// NewsPage renders the admin page listing all news messages.
func (h *NewsHandler) NewsPage(w http.ResponseWriter, r *http.Request) {
	msgs, err := h.DB.ListNewsMessages()
	if err != nil {
		http.Error(w, "Error loading news banners", http.StatusInternalServerError)
		return
	}
	h.Render(w, r, "admin_news", map[string]interface{}{
		"Messages": msgs,
		"Error":    r.URL.Query().Get("error"),
	})
}

// CreateNews handles POST /admin/news.
func (h *NewsHandler) CreateNews(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     string `json:"title"`
		Content   string `json:"content"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
		BgColor   string `json:"bg_color"`
		Recurring bool   `json:"recurring"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	req.StartDate = strings.TrimSpace(req.StartDate)
	req.EndDate = strings.TrimSpace(req.EndDate)
	req.BgColor = strings.TrimSpace(req.BgColor)
	if req.BgColor == "" {
		req.BgColor = "#dc2626"
	}

	if req.Title == "" || req.Content == "" || req.StartDate == "" || req.EndDate == "" {
		jsonError(w, "news.error.fields_required", http.StatusBadRequest)
		return
	}
	if !isValidDate(req.StartDate) || !isValidDate(req.EndDate) {
		jsonError(w, "news.error.invalid_dates", http.StatusBadRequest)
		return
	}
	if !req.Recurring && req.StartDate > req.EndDate {
		jsonError(w, "news.error.invalid_dates", http.StatusBadRequest)
		return
	}
	if req.Recurring {
		if err := validateRecurringDays(req.StartDate, req.EndDate); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if !hexColorRE.MatchString(req.BgColor) {
		jsonError(w, "news.error.invalid_color", http.StatusBadRequest)
		return
	}

	id, err := h.DB.CreateNewsMessage(req.Title, req.Content, req.StartDate, req.EndDate, req.BgColor, req.Recurring)
	if err != nil {
		slog.Error("admin.news.create_error", "error", err)
		jsonError(w, "Server error", http.StatusInternalServerError)
		return
	}
	currentUser := middleware.GetUser(r)
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "news", id, "create", req.Title)
		slog.Info("admin.news.create", "actor", currentUser.Email, "title", req.Title, "id", id)
	}
	jsonOK(w, map[string]interface{}{"id": id, "status": "ok"})
}

// UpdateNews handles PUT /admin/news/{id}.
func (h *NewsHandler) UpdateNews(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	var req struct {
		Title     string `json:"title"`
		Content   string `json:"content"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
		BgColor   string `json:"bg_color"`
		Recurring bool   `json:"recurring"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	req.StartDate = strings.TrimSpace(req.StartDate)
	req.EndDate = strings.TrimSpace(req.EndDate)
	req.BgColor = strings.TrimSpace(req.BgColor)
	if req.BgColor == "" {
		req.BgColor = "#dc2626"
	}

	if req.Title == "" || req.Content == "" || req.StartDate == "" || req.EndDate == "" {
		jsonError(w, "news.error.fields_required", http.StatusBadRequest)
		return
	}
	if !isValidDate(req.StartDate) || !isValidDate(req.EndDate) {
		jsonError(w, "news.error.invalid_dates", http.StatusBadRequest)
		return
	}
	if !req.Recurring && req.StartDate > req.EndDate {
		jsonError(w, "news.error.invalid_dates", http.StatusBadRequest)
		return
	}
	if req.Recurring {
		if err := validateRecurringDays(req.StartDate, req.EndDate); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if !hexColorRE.MatchString(req.BgColor) {
		jsonError(w, "news.error.invalid_color", http.StatusBadRequest)
		return
	}

	if err := h.DB.UpdateNewsMessage(id, req.Title, req.Content, req.StartDate, req.EndDate, req.BgColor, req.Recurring); err != nil {
		slog.Error("admin.news.update_error", "id", id, "error", err)
		jsonError(w, "Server error", http.StatusInternalServerError)
		return
	}
	currentUser := middleware.GetUser(r)
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "news", id, "update", req.Title)
		slog.Info("admin.news.update", "actor", currentUser.Email, "title", req.Title, "id", id)
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// DeleteNews handles DELETE /admin/news/{id}.
func (h *NewsHandler) DeleteNews(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	title := h.DB.GetNewsMessageTitle(id)
	if err := h.DB.DeleteNewsMessage(id); err != nil {
		slog.Error("admin.news.delete_error", "id", id, "error", err)
		jsonError(w, "Server error", http.StatusInternalServerError)
		return
	}
	currentUser := middleware.GetUser(r)
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "news", id, "delete", title)
		slog.Info("admin.news.delete", "actor", currentUser.Email, "title", title, "id", id)
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// isValidDate checks that a string is a valid YYYY-MM-DD date.
func isValidDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// validateRecurringDays ensures that the day-of-month of startDate is <= that of endDate.
func validateRecurringDays(startDate, endDate string) error {
	start, err1 := time.Parse("2006-01-02", startDate)
	end, err2 := time.Parse("2006-01-02", endDate)
	if err1 != nil || err2 != nil {
		return fmt.Errorf("news.error.invalid_dates")
	}
	if start.Day() > end.Day() {
		return fmt.Errorf("news.error.invalid_dates")
	}
	return nil
}
