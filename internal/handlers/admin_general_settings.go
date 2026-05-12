package handlers

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/matoy/mypresence/internal/middleware"
)

// GeneralSettingsHandler handles the general admin settings page.
type GeneralSettingsHandler struct {
	DataDir string
	Render  func(w http.ResponseWriter, r *http.Request, page string, data interface{})
}

// GeneralSettingsPage renders the general settings admin page.
func (h *GeneralSettingsHandler) GeneralSettingsPage(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(filepath.Join(h.DataDir, "logo.png"))
	logoExists := err == nil
	h.Render(w, r, "admin_general_settings", map[string]interface{}{
		"LogoExists": logoExists,
		"Error":      r.URL.Query().Get("error"),
		"Success":    r.URL.Query().Get("success"),
	})
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
