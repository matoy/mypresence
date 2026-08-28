package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/middleware"
)

// EnvEntry holds a single environment variable key-value pair.
type EnvEntry struct {
	Key      string
	Value    string
	Editable bool
}

// GeneralSettingsHandler handles the general admin settings page.
type GeneralSettingsHandler struct {
	DataDir string
	Config  *config.Config
	Render  func(w http.ResponseWriter, r *http.Request, page string, data interface{})
}

// isSensitiveEnvKey reports whether the environment variable name indicates a sensitive secret.
func isSensitiveEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	return strings.Contains(upper, "PASSWORD") ||
		strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "TOKEN") ||
		strings.Contains(upper, "KEY") ||
		strings.Contains(upper, "CREDENTIAL") ||
		strings.Contains(upper, "AUTH") ||
		upper == "SMTP_URL"
}

// maskEnvValue masks the value with bullet points if key is sensitive and value is non-empty,
// but reveals the last 3 characters.
func maskEnvValue(key, value string) string {
	if value == "" {
		return ""
	}
	if isSensitiveEnvKey(key) {
		runes := []rune(value)
		if len(runes) <= 3 {
			return "••••••••" + string(runes)
		}
		return "••••••••" + string(runes[len(runes)-3:])
	}
	return value
}

// GeneralSettingsPage renders the general settings admin page.
func (h *GeneralSettingsHandler) GeneralSettingsPage(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(filepath.Join(h.DataDir, "logo.png"))
	logoExists := err == nil

	rawEnv := os.Environ()
	sort.Strings(rawEnv)
	envVars := make([]EnvEntry, 0, len(rawEnv))
	for _, e := range rawEnv {
		k, v, _ := strings.Cut(e, "=")
		envVars = append(envVars, EnvEntry{
			Key:      k,
			Value:    maskEnvValue(k, v),
			Editable: config.IsLiveEditable(k),
		})
	}

	h.Render(w, r, "admin_general_settings", map[string]interface{}{
		"LogoExists": logoExists,
		"Error":      r.URL.Query().Get("error"),
		"Success":    r.URL.Query().Get("success"),
		"EnvVars":    envVars,
	})
}

// UpdateEnvVar handles POST /admin/settings/env — updates an environment variable's
// value in memory only (process env + the running Config). Lost on the next restart.
func (h *GeneralSettingsHandler) UpdateEnvVar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
		jsonError(w, "key required", http.StatusBadRequest)
		return
	}
	if h.Config == nil || !h.Config.ApplyEnvOverride(req.Key, req.Value) {
		jsonError(w, "this variable cannot be edited live", http.StatusBadRequest)
		return
	}
	if actor := middleware.GetUser(r); actor != nil {
		slog.Info("admin.settings.env_update", "actor", actor.Email, "key", req.Key)
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// UploadLogo handles POST /admin/settings/logo — saves the uploaded PNG as logo.png.
func (h *GeneralSettingsHandler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	const maxUploadBytes = 5 << 20 // 5 MB

	r.ParseMultipartForm(maxUploadBytes) //nolint:errcheck
	file, header, err := r.FormFile("logo")
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=missing_file", http.StatusSeeOther)
		return
	}
	defer file.Close() //nolint:errcheck

	// Only accept .png extension
	if strings.ToLower(filepath.Ext(header.Filename)) != ".png" {
		http.Redirect(w, r, "/admin/settings?error=invalid_format", http.StatusSeeOther)
		return
	}

	// Sniff first 512 bytes to prevent extension spoofing
	sniff := make([]byte, 512)
	n, _ := file.Read(sniff)
	if ct := strings.SplitN(http.DetectContentType(sniff[:n]), ";", 2)[0]; ct != "image/png" {
		http.Redirect(w, r, "/admin/settings?error=invalid_content", http.StatusSeeOther)
		return
	}

	logoPath := filepath.Join(h.DataDir, "logo.png")
	dst, err := os.Create(logoPath)
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=write_error", http.StatusSeeOther)
		return
	}
	defer dst.Close() //nolint:errcheck

	fullReader := io.LimitReader(io.MultiReader(bytes.NewReader(sniff[:n]), file), maxUploadBytes)
	if _, err := io.Copy(dst, fullReader); err != nil {
		os.Remove(logoPath) //nolint:errcheck
		http.Redirect(w, r, "/admin/settings?error=write_error", http.StatusSeeOther)
		return
	}

	if actor := middleware.GetUser(r); actor != nil {
		slog.Info("admin.settings.logo_upload", "actor", actor.Email)
	}
	http.Redirect(w, r, "/admin/settings?success=logo_uploaded", http.StatusSeeOther)
}

// DeleteLogo handles DELETE /admin/settings/logo — removes logo.png.
func (h *GeneralSettingsHandler) DeleteLogo(w http.ResponseWriter, r *http.Request) {
	logoPath := filepath.Join(h.DataDir, "logo.png")
	if err := os.Remove(logoPath); err != nil && !os.IsNotExist(err) {
		jsonError(w, "Erreur lors de la suppression du logo", http.StatusInternalServerError)
		return
	}
	if actor := middleware.GetUser(r); actor != nil {
		slog.Info("admin.settings.logo_delete", "actor", actor.Email)
	}
	jsonOK(w, map[string]string{"status": "ok"})
}
