package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"presence-app/internal/db"
	"presence-app/internal/metrics"
	"presence-app/internal/middleware"
	"presence-app/internal/models"
)

// PATHandler handles Personal Access Token management.
type PATHandler struct {
	DB     *db.DB
	Render func(w http.ResponseWriter, r *http.Request, page string, data interface{})
}

// PATPage renders the token management page.
// Route: GET /settings/tokens
func (h *PATHandler) PATPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	pats, _ := h.DB.ListUserPATs(user.ID)
	if pats == nil {
		pats = []models.PersonalAccessToken{}
	}

	pageData := map[string]interface{}{
		"Tokens":    pats,
		"CanCreate": user.CanUseTokens(),
		"IsAdmin":   user.HasRole("global"),
	}

	if user.HasRole("global") {
		allPATs, _ := h.DB.ListAllPATs()
		if allPATs == nil {
			allPATs = []db.AdminPAT{}
		}
		pageData["AllTokens"] = allPATs
	}

	h.Render(w, r, "pat", pageData)
}

// CreatePAT creates a new Personal Access Token for the authenticated user.
// Route: POST /api/tokens
func (h *PATHandler) CreatePAT(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !user.CanUseTokens() {
		metrics.PATOpsTotal.WithLabelValues("create", "failure").Inc()
		jsonError(w, "Création de token non autorisée pour ce compte", http.StatusForbidden)
		return
	}

	var req struct {
		Description string `json:"description"`
		ExpiresIn   int    `json:"expires_in"` // days; 0 = no expiry
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.PATOpsTotal.WithLabelValues("create", "failure").Inc()
		jsonError(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	req.Description = strings.TrimSpace(req.Description)
	if req.Description == "" {
		metrics.PATOpsTotal.WithLabelValues("create", "failure").Inc()
		jsonError(w, "La description est requise", http.StatusBadRequest)
		return
	}
	if len(req.Description) > 200 {
		metrics.PATOpsTotal.WithLabelValues("create", "failure").Inc()
		jsonError(w, "Description trop longue (max 200 caractères)", http.StatusBadRequest)
		return
	}
	if req.ExpiresIn < 0 || req.ExpiresIn > 3650 {
		metrics.PATOpsTotal.WithLabelValues("create", "failure").Inc()
		jsonError(w, "Durée invalide (0–3650 jours)", http.StatusBadRequest)
		return
	}

	var expiresAt *time.Time
	if req.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(req.ExpiresIn) * 24 * time.Hour)
		expiresAt = &t
	}

	raw, pat, err := h.DB.CreatePAT(user.ID, req.Description, expiresAt)
	if err != nil {
		metrics.PATOpsTotal.WithLabelValues("create", "failure").Inc()
		jsonError(w, "Erreur lors de la création du token", http.StatusInternalServerError)
		return
	}
	metrics.PATOpsTotal.WithLabelValues("create", "success").Inc()
	slog.Info("pat.create", "user", user.Email, "pat_id", pat.ID, "expires", pat.ExpiresAt != nil)

	jsonOK(w, map[string]interface{}{
		"id":           pat.ID,
		"token":        raw, // raw token displayed exactly once
		"description":  pat.Description,
		"token_prefix": pat.TokenPrefix,
		"expires_at":   pat.ExpiresAt,
		"created_at":   pat.CreatedAt,
	})
}

// RevokePAT revokes a token owned by the authenticated user.
// Route: DELETE /api/tokens/{id}
func (h *PATHandler) RevokePAT(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		metrics.PATOpsTotal.WithLabelValues("revoke", "failure").Inc()
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}

	if err := h.DB.RevokePAT(id, user.ID); err != nil {
		metrics.PATOpsTotal.WithLabelValues("revoke", "failure").Inc()
		jsonError(w, "Token introuvable", http.StatusNotFound)
		return
	}
	metrics.PATOpsTotal.WithLabelValues("revoke", "success").Inc()
	slog.Info("pat.revoke", "user", user.Email, "pat_id", id)

	jsonOK(w, map[string]string{"status": "ok"})
}

// AdminRevokePAT revokes any token regardless of owner (global admin only).
// Route: DELETE /api/admin/tokens/{id}
func (h *PATHandler) AdminRevokePAT(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !user.HasRole("global") {
		metrics.PATOpsTotal.WithLabelValues("admin_revoke", "failure").Inc()
		jsonError(w, "Accès non autorisé", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		metrics.PATOpsTotal.WithLabelValues("admin_revoke", "failure").Inc()
		jsonError(w, "ID invalide", http.StatusBadRequest)
		return
	}
	if err := h.DB.AdminRevokePAT(id); err != nil {
		metrics.PATOpsTotal.WithLabelValues("admin_revoke", "failure").Inc()
		jsonError(w, "Token introuvable", http.StatusNotFound)
		return
	}
	metrics.PATOpsTotal.WithLabelValues("admin_revoke", "success").Inc()
	slog.Info("pat.admin_revoke", "admin", user.Email, "pat_id", id)
	jsonOK(w, map[string]string{"status": "ok"})
}

// ListPATs returns all PATs for the authenticated user as JSON.
// Route: GET /api/tokens
func (h *PATHandler) ListPATs(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	pats, err := h.DB.ListUserPATs(user.ID)
	if err != nil {
		metrics.PATOpsTotal.WithLabelValues("list", "failure").Inc()
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	if pats == nil {
		pats = []models.PersonalAccessToken{}
	}
	metrics.PATOpsTotal.WithLabelValues("list", "success").Inc()
	slog.Info("pat.list", "user", user.Email, "count", len(pats))
	jsonOK(w, pats)
}
