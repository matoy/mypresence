package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/o365"
)

// O365Handler handles Microsoft Office 365 calendar integration endpoints.
type O365Handler struct {
	DB        *db.DB
	Config    *config.Config
	Client    *o365.Client
	Debouncer *o365.Debouncer
}

// NewO365Handler creates a new O365Handler.
func NewO365Handler(database *db.DB, cfg *config.Config, client *o365.Client, debouncer *o365.Debouncer) *O365Handler {
	return &O365Handler{
		DB:        database,
		Config:    cfg,
		Client:    client,
		Debouncer: debouncer,
	}
}

// Connect initiates the OAuth2 authorization code flow with Microsoft Entra ID.
func (h *O365Handler) Connect(w http.ResponseWriter, r *http.Request) {
	if !h.Config.O365SyncEnabled || h.Client == nil {
		http.Error(w, "Office 365 integration is disabled", http.StatusNotFound)
		return
	}

	user := middleware.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" {
		returnTo = "/"
	}

	authURL := h.Client.AuthURL(user.ID, returnTo)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// Callback handles the OAuth2 redirect from Microsoft.
func (h *O365Handler) Callback(w http.ResponseWriter, r *http.Request) {
	if !h.Config.O365SyncEnabled || h.Client == nil {
		http.Error(w, "Office 365 integration is disabled", http.StatusNotFound)
		return
	}

	// Check for Microsoft error
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		slog.Warn("o365.callback: microsoft error", "error", errParam, "desc", errDesc)
		http.Redirect(w, r, "/?o365_error="+url.QueryEscape(errParam), http.StatusSeeOther)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, "/?o365_error=missing_code_or_state", http.StatusSeeOther)
		return
	}

	userID, returnTo, _, err := h.Client.ExchangeCode(code, state)
	if err != nil {
		slog.Error("o365.callback: exchange failed", "err", err)
		http.Redirect(w, r, "/?o365_error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	slog.Info("o365.callback: successfully connected", "user_id", userID)

	// Clean up redirect URL and add o365_connected=1
	sep := "?"
	if strings.Contains(returnTo, "?") {
		sep = "&"
	}
	redirectTarget := returnTo + sep + "o365_connected=1"
	http.Redirect(w, r, redirectTarget, http.StatusSeeOther)
}

// Status returns the current Office 365 connection status for the logged-in user.
func (h *O365Handler) Status(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Non authentifié", http.StatusUnauthorized)
		return
	}

	if !h.Config.O365SyncEnabled || h.Client == nil {
		jsonOK(w, map[string]interface{}{
			"enabled":   false,
			"connected": false,
		})
		return
	}

	tok, err := h.DB.GetUserO365Token(user.ID)
	if err != nil {
		jsonError(w, "Erreur base de données", http.StatusInternalServerError)
		return
	}

	if tok == nil {
		jsonOK(w, map[string]interface{}{
			"enabled":   true,
			"connected": false,
		})
		return
	}

	jsonOK(w, map[string]interface{}{
		"enabled":         true,
		"connected":       true,
		"microsoft_email": tok.MicrosoftEmail,
		"auto_sync":       tok.AutoSync,
	})
}

// Disconnect removes the user's Office 365 tokens and sync records.
func (h *O365Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Non authentifié", http.StatusUnauthorized)
		return
	}

	if err := h.DB.DeleteUserO365Token(user.ID); err != nil {
		jsonError(w, "Erreur déconnexion", http.StatusInternalServerError)
		return
	}
	_ = h.DB.DeleteAllCalendarSyncEventsForUser(user.ID)

	slog.Info("o365.disconnect: disconnected", "user_id", user.ID)
	jsonOK(w, map[string]string{"status": "ok"})
}

// Sync runs calendar reconciliation for the given year and month.
func (h *O365Handler) Sync(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Non authentifié", http.StatusUnauthorized)
		return
	}

	if !h.Config.O365SyncEnabled || h.Client == nil {
		jsonError(w, "Intégration Office 365 désactivée", http.StatusBadRequest)
		return
	}

	var req struct {
		Year  int `json:"year"`
		Month int `json:"month"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Year <= 0 || req.Month < 1 || req.Month > 12 {
		now := time.Now()
		req.Year = now.Year()
		req.Month = int(now.Month())
	}

	report, err := h.Client.SyncMonth(user.ID, req.Year, req.Month)
	if err != nil {
		slog.Error("o365.sync handler error", "user_id", user.ID, "err", err)
		jsonError(w, fmt.Sprintf("Erreur synchronisation : %v", err), http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]interface{}{
		"status":    "ok",
		"year":      report.Year,
		"month":     report.Month,
		"created":   report.Created,
		"updated":   report.Updated,
		"deleted":   report.Deleted,
		"unchanged": report.Unchanged,
		"errors":    report.Errors,
	})
}

// ToggleAutoSync updates the user's auto-sync preference.
func (h *O365Handler) ToggleAutoSync(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		jsonError(w, "Non authentifié", http.StatusUnauthorized)
		return
	}

	var req struct {
		AutoSync bool `json:"auto_sync"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	if err := h.DB.SetUserO365AutoSync(user.ID, req.AutoSync); err != nil {
		jsonError(w, "Erreur mise à jour", http.StatusInternalServerError)
		return
	}

	slog.Info("o365.toggle_auto_sync", "user_id", user.ID, "auto_sync", req.AutoSync)
	jsonOK(w, map[string]interface{}{
		"status":    "ok",
		"auto_sync": req.AutoSync,
	})
}
