package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/matoy/mypresence/internal/metrics"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// webAuthnUser implements the webauthn.User interface for our User model.
type webAuthnUser struct {
	user        *models.User
	credentials []webauthn.Credential
}

func newWebAuthnUser(u *models.User, creds []webauthn.Credential) *webAuthnUser {
	return &webAuthnUser{user: u, credentials: creds}
}

// WebAuthnID returns an opaque, stable user handle derived from the database ID.
func (u *webAuthnUser) WebAuthnID() []byte {
	id := make([]byte, 8)
	binary.BigEndian.PutUint64(id, uint64(u.user.ID))
	return id
}
func (u *webAuthnUser) WebAuthnName() string                       { return u.user.Email }
func (u *webAuthnUser) WebAuthnDisplayName() string                { return u.user.Name }
func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// passkeyChallengeEntry holds a pending WebAuthn ceremony.
type passkeyChallengeEntry struct {
	SessionData webauthn.SessionData
	UserID      int64 // >0 for registration; 0 for discoverable login
	ExpiresAt   time.Time
}

// generatePasskeyToken returns a cryptographically random 32-byte hex string.
func generatePasskeyToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// InitWebAuthn initialises the WebAuthn instance from config.
// It is a no-op when passkeys are disabled or configuration is incomplete.
func (h *AuthHandler) InitWebAuthn() error {
	cfg := h.Config
	if !cfg.EnablePasskeys {
		return nil
	}
	if cfg.PasskeyRPID == "" || cfg.PasskeyRPOrigin == "" {
		slog.Warn("passkeys.disabled", "reason", "PASSKEY_RP_ID and PASSKEY_RP_ORIGIN must be set when ENABLE_PASSKEYS=true")
		cfg.EnablePasskeys = false
		return nil
	}

	wauthn, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.PasskeyRPID,
		RPDisplayName: cfg.AppName,
		RPOrigins:     []string{cfg.PasskeyRPOrigin},
	})
	if err != nil {
		return fmt.Errorf("webauthn init: %w", err)
	}
	h.WebAuthn = wauthn
	slog.Info("passkeys.enabled", "rp_id", cfg.PasskeyRPID, "rp_origin", cfg.PasskeyRPOrigin)
	return nil
}

// ─── Registration ceremony ────────────────────────────────────────────────────

// PasskeyRegisterBegin starts the passkey registration ceremony.
// POST /webauthn/register/begin  (authenticated)
func (h *AuthHandler) PasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if h.WebAuthn == nil {
		http.Error(w, "Passkeys not enabled", http.StatusNotFound)
		return
	}
	user := middleware.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !user.IsLocal {
		http.Error(w, "Passkeys are only available for local accounts", http.StatusForbidden)
		return
	}

	creds, err := h.DB.GetWebAuthnCredentials(user.ID)
	if err != nil {
		slog.Error("passkey.register.begin", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	wUser := newWebAuthnUser(user, creds)
	options, sessionData, err := h.WebAuthn.BeginRegistration(wUser)
	if err != nil {
		slog.Error("passkey.register.begin", "error", err)
		http.Error(w, "Registration failed", http.StatusInternalServerError)
		return
	}

	token, err := generatePasskeyToken()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	h.passkeySessions.Store(token, &passkeyChallengeEntry{
		SessionData: *sessionData,
		UserID:      user.ID,
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionToken": token,
		"publicKey":    options.Response,
	}); err != nil {
		slog.Error("passkey.register.begin.encode", "error", err)
	}
}

// PasskeyRegisterFinish completes the passkey registration ceremony.
// POST /webauthn/register/finish?token=<tok>&name=<name>  (authenticated)
// Body: standard PublicKeyCredential JSON from the browser.
func (h *AuthHandler) PasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if h.WebAuthn == nil {
		http.Error(w, "Passkeys not enabled", http.StatusNotFound)
		return
	}
	user := middleware.GetUser(r)
	if user == nil || !user.IsLocal {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	token := r.URL.Query().Get("token")
	name := r.URL.Query().Get("name")
	if token == "" {
		http.Error(w, "Missing session token", http.StatusBadRequest)
		return
	}

	val, ok := h.passkeySessions.LoadAndDelete(token)
	if !ok {
		http.Error(w, "Invalid or expired session", http.StatusBadRequest)
		return
	}
	entry := val.(*passkeyChallengeEntry)
	if entry.UserID != user.ID || time.Now().After(entry.ExpiresAt) {
		http.Error(w, "Invalid or expired session", http.StatusBadRequest)
		return
	}

	creds, _ := h.DB.GetWebAuthnCredentials(user.ID)
	wUser := newWebAuthnUser(user, creds)

	credential, err := h.WebAuthn.FinishRegistration(wUser, entry.SessionData, r)
	if err != nil {
		slog.Warn("passkey.register.finish", "error", err, "user", user.Email)
		http.Error(w, "Registration verification failed", http.StatusBadRequest)
		return
	}

	if name == "" {
		name = "Passkey"
	}
	if err := h.DB.CreateWebAuthnCredential(user.ID, name, credential); err != nil {
		slog.Error("passkey.register.save", "error", err, "user", user.Email)
		http.Error(w, "Failed to save credential", http.StatusInternalServerError)
		return
	}

	slog.Info("passkey.registered", "user", user.Email, "name", name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true}) //nolint:errcheck
}

// ─── Authentication ceremony ──────────────────────────────────────────────────

// PasskeyLoginBegin starts the discoverable passkey login ceremony.
// POST /webauthn/login/begin  (public)
func (h *AuthHandler) PasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if h.WebAuthn == nil {
		http.Error(w, "Passkeys not enabled", http.StatusNotFound)
		return
	}

	options, sessionData, err := h.WebAuthn.BeginDiscoverableLogin()
	if err != nil {
		slog.Error("passkey.login.begin", "error", err)
		http.Error(w, "Login failed", http.StatusInternalServerError)
		return
	}

	token, err := generatePasskeyToken()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	h.passkeySessions.Store(token, &passkeyChallengeEntry{
		SessionData: *sessionData,
		UserID:      0,
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionToken": token,
		"publicKey":    options.Response,
	}); err != nil {
		slog.Error("passkey.login.begin.encode", "error", err)
	}
}

// PasskeyLoginFinish verifies the authenticator assertion and creates a session.
// POST /webauthn/login/finish?token=<tok>  (public)
// Body: standard AuthenticatorAssertionResponse JSON from the browser.
func (h *AuthHandler) PasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if h.WebAuthn == nil {
		http.Error(w, "Passkeys not enabled", http.StatusNotFound)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing session token", http.StatusBadRequest)
		return
	}

	val, ok := h.passkeySessions.LoadAndDelete(token)
	if !ok {
		http.Error(w, "Invalid or expired session", http.StatusBadRequest)
		return
	}
	entry := val.(*passkeyChallengeEntry)
	if time.Now().After(entry.ExpiresAt) {
		http.Error(w, "Session expired", http.StatusBadRequest)
		return
	}

	// discoverableUserHandler looks up the user by the user handle returned by the authenticator.
	discoverableUserHandler := func(rawID, userHandle []byte) (webauthn.User, error) {
		if len(userHandle) < 8 {
			return nil, errors.New("invalid user handle")
		}
		userID := int64(binary.BigEndian.Uint64(userHandle[:8]))
		u, err := h.DB.GetUserByID(userID)
		if err != nil {
			return nil, err
		}
		if u.Disabled {
			return nil, errors.New("account disabled")
		}
		dbCreds, err := h.DB.GetWebAuthnCredentials(u.ID)
		if err != nil {
			return nil, err
		}
		return newWebAuthnUser(u, dbCreds), nil
	}

	wUser, credential, err := h.WebAuthn.FinishPasskeyLogin(discoverableUserHandler, entry.SessionData, r)
	if err != nil {
		slog.Warn("passkey.login.finish", "error", err, "ip", clientIP(r))
		metrics.AuthLoginsTotal.WithLabelValues("passkey", "failure").Inc()
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	}

	// Update sign count to detect cloned authenticators.
	if err := h.DB.UpdateWebAuthnCredentialUsed(credential.ID, credential.Authenticator.SignCount); err != nil {
		slog.Warn("passkey.login.update_signcount", "error", err)
	}

	appUser := wUser.(*webAuthnUser).user
	sessionToken, err := h.DB.CreateSession(appUser.ID)
	if err != nil {
		metrics.AuthLoginsTotal.WithLabelValues("passkey", "failure").Inc()
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	metrics.AuthLoginsTotal.WithLabelValues("passkey", "success").Inc()
	slog.Info("auth.login", "result", "success", "user", appUser.Email, "method", "passkey", "ip", clientIP(r))

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 30,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true}) //nolint:errcheck
}

// ─── Settings page ────────────────────────────────────────────────────────────

// PasskeysPage renders the passkeys management settings page.
// GET /settings/passkeys  (authenticated)
func (h *AuthHandler) PasskeysPage(w http.ResponseWriter, r *http.Request) {
	if h.WebAuthn == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	user := middleware.GetUser(r)
	if user == nil || !user.IsLocal {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	passkeys, err := h.DB.ListWebAuthnCredentials(user.ID)
	if err != nil {
		slog.Error("passkey.list", "error", err, "user", user.Email)
		passkeys = nil
	}

	h.Render(w, r, "settings_passkeys", map[string]interface{}{
		"Passkeys": passkeys,
		"Error":    r.URL.Query().Get("error"),
		"Success":  r.URL.Query().Get("success"),
	})
}

// PasskeyDeletePost removes a passkey credential for the authenticated user.
// POST /settings/passkeys/delete  (authenticated, CSRF-protected)
func (h *AuthHandler) PasskeyDeletePost(w http.ResponseWriter, r *http.Request) {
	if h.WebAuthn == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	user := middleware.GetUser(r)
	if user == nil || !user.IsLocal {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	credID := r.FormValue("id")
	if credID == "" {
		http.Redirect(w, r, "/settings/passkeys?error=Invalid+request", http.StatusSeeOther)
		return
	}
	if err := h.DB.DeleteWebAuthnCredential(credID, user.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Error("passkey.delete", "error", err, "user", user.Email)
		http.Redirect(w, r, "/settings/passkeys?error=Internal+error", http.StatusSeeOther)
		return
	}

	slog.Info("passkey.deleted", "user", user.Email)
	http.Redirect(w, r, "/settings/passkeys?success=Passkey+deleted", http.StatusSeeOther)
}
