package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
)

// GenerateCSRFToken derives a CSRF token from the raw session token and the application
// secret key. The token is bound to the session, so it expires naturally with the session.
func GenerateCSRFToken(secretKey, rawSessionToken string) string {
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(rawSessionToken))
	return hex.EncodeToString(mac.Sum(nil))
}

func isStateChangingMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

// isSameOriginRequest validates Sec-Fetch-Site and Origin headers on cookie-based browser mutations.
// Programmatic clients without Origin/Sec-Fetch-Site headers (e.g. CLI tools or unit tests) or Bearer PAT requests are allowed.
func isSameOriginRequest(r *http.Request, allowedHosts []string, trusted *TrustedProxyChecker) bool {
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return true
	}
	if sfs := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); sfs != "" {
		if sfs != "same-origin" && sfs != "none" {
			return false
		}
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		if strings.EqualFold(origin, "null") {
			return false
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			return false
		}
		if r.Host != "" && strings.EqualFold(u.Host, r.Host) {
			return true
		}
		if trusted.TrustForwardedHeaders(r) {
			if xfh := strings.TrimSpace(strings.SplitN(r.Header.Get("X-Forwarded-Host"), ",", 2)[0]); xfh != "" && strings.EqualFold(u.Host, xfh) {
				return true
			}
		}
		for _, h := range allowedHosts {
			if h != "" && strings.EqualFold(u.Host, h) {
				return true
			}
		}
		return false
	}
	return true
}

// ValidateSameOrigin blocks cross-site state-changing browser requests (including JSON and text/plain forms)
// using Sec-Fetch-Site and Origin header checks, exempting listed paths such as cross-site IdP POST callbacks.
func ValidateSameOrigin(allowedHosts []string, trusted *TrustedProxyChecker, exemptPaths ...string) func(http.Handler) http.Handler {
	exempt := make(map[string]bool, len(exemptPaths))
	for _, p := range exemptPaths {
		exempt[p] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isStateChangingMethod(r.Method) && !exempt[r.URL.Path] {
				if !isSameOriginRequest(r, allowedHosts, trusted) {
					http.Error(w, "Cross-site request forbidden", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ValidateCSRF returns a middleware that checks the csrf_token form field on all
// state-changing requests with a form-encoded body (application/x-www-form-urlencoded
// or multipart/form-data), and enforces same-origin browser headers on other mutations.
func ValidateCSRF(secretKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isStateChangingMethod(r.Method) {
				ct := r.Header.Get("Content-Type")
				if strings.HasPrefix(ct, "application/x-www-form-urlencoded") ||
					strings.HasPrefix(ct, "multipart/form-data") {
					cookie, err := r.Cookie("session")
					if err != nil {
						http.Error(w, "Forbidden", http.StatusForbidden)
						return
					}
					expected := GenerateCSRFToken(secretKey, cookie.Value)
					given := r.FormValue("csrf_token")
					if !hmac.Equal([]byte(expected), []byte(given)) {
						http.Error(w, "Invalid CSRF token", http.StatusForbidden)
						return
					}
				} else if !isSameOriginRequest(r, nil, nil) {
					http.Error(w, "Cross-site request forbidden", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
