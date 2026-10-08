package o365

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/models"
)

const (
	defaultGraphBase = "https://graph.microsoft.com/v1.0"
	defaultLoginBase = "https://login.microsoftonline.com"
	graphScopes      = "openid profile offline_access Calendars.ReadWrite"
)

// Config holds Office 365 configuration for the Microsoft Graph client.
type Config struct {
	TenantID        string
	ClientID        string
	ClientSecret    string
	RedirectURL     string
	SecretKey       string
	AppName         string
	DefaultTimezone string
	AMHours         string
	PMHours         string

	// Overridable URLs for testing
	GraphBaseURL string
	LoginBaseURL string
}

// Client interacts with the Microsoft Graph REST API and manages OAuth2 tokens.
type Client struct {
	cfg        *Config
	db         *db.DB
	httpClient *http.Client
}

// NewClient returns a new Microsoft Graph client.
func NewClient(cfg *Config, database *db.DB) *Client {
	if cfg.GraphBaseURL == "" {
		cfg.GraphBaseURL = defaultGraphBase
	}
	if cfg.LoginBaseURL == "" {
		cfg.LoginBaseURL = defaultLoginBase
	}
	if cfg.TenantID == "" {
		cfg.TenantID = "common"
	}
	if cfg.DefaultTimezone == "" {
		cfg.DefaultTimezone = "Europe/Paris"
	}
	if cfg.AMHours == "" {
		cfg.AMHours = "08:30-12:30"
	}
	if cfg.PMHours == "" {
		cfg.PMHours = "13:30-17:30"
	}
	if cfg.AppName == "" {
		cfg.AppName = "Presence"
	}

	return &Client{
		cfg:        cfg,
		db:         database,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Event represents a presence calendar entry to create or update in Microsoft Graph.
type Event struct {
	Subject     string `json:"subject"`
	Date        string `json:"date"` // YYYY-MM-DD
	Half        string `json:"half"` // "full", "AM", "PM"
	ShowAs      string `json:"show_as"`
	Description string `json:"description"`
}

// GenerateState creates a cryptographically signed state parameter for OAuth2 authorization.
// Format: base64(userID:timestamp:returnTo:signature)
func (c *Client) GenerateState(userID int64, returnTo string) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	payload := fmt.Sprintf("%d:%s:%s", userID, ts, returnTo)

	mac := hmac.New(sha256.New, []byte(c.cfg.SecretKey))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	full := fmt.Sprintf("%s:%s", payload, sig)
	return base64.RawURLEncoding.EncodeToString([]byte(full))
}

// ValidateState validates the signed OAuth2 state and returns the embedded userID and returnTo.
func (c *Client) ValidateState(state string) (int64, string, error) {
	data, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return 0, "", errors.New("invalid state format")
	}

	parts := strings.Split(string(data), ":")
	if len(parts) < 4 {
		return 0, "", errors.New("malformed state")
	}

	userIDStr := parts[0]
	tsStr := parts[1]
	// returnTo may contain slashes or parameters
	returnTo := strings.Join(parts[2:len(parts)-1], ":")
	sig := parts[len(parts)-1]

	payload := fmt.Sprintf("%s:%s:%s", userIDStr, tsStr, returnTo)
	mac := hmac.New(sha256.New, []byte(c.cfg.SecretKey))
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return 0, "", errors.New("state signature mismatch")
	}

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)) > 15*time.Minute {
		return 0, "", errors.New("state expired")
	}

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return 0, "", errors.New("invalid user id in state")
	}

	return userID, returnTo, nil
}

// AuthURL returns the Microsoft authorization URL for the user to grant calendar rights.
func (c *Client) AuthURL(userID int64, returnTo string) string {
	state := c.GenerateState(userID, returnTo)
	u, _ := url.Parse(fmt.Sprintf("%s/%s/oauth2/v2.0/authorize", strings.TrimRight(c.cfg.LoginBaseURL, "/"), c.cfg.TenantID))
	q := u.Query()
	q.Set("client_id", c.cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", c.cfg.RedirectURL)
	q.Set("response_mode", "query")
	q.Set("scope", graphScopes)
	q.Set("state", state)
	q.Set("prompt", "select_account")
	u.RawQuery = q.Encode()
	return u.String()
}

type tokenResponse struct {
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// ExchangeCode exchanges an authorization code for access and refresh tokens,
// retrieves the user's Microsoft profile email, encrypts the tokens, and saves them in DB.
func (c *Client) ExchangeCode(code, state string) (int64, string, *models.UserO365Token, error) {
	userID, returnTo, err := c.ValidateState(state)
	if err != nil {
		return 0, "", nil, fmt.Errorf("state validation: %w", err)
	}

	tokenEndpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", strings.TrimRight(c.cfg.LoginBaseURL, "/"), c.cfg.TenantID)
	data := url.Values{}
	data.Set("client_id", c.cfg.ClientID)
	if c.cfg.ClientSecret != "" {
		data.Set("client_secret", c.cfg.ClientSecret)
	}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", c.cfg.RedirectURL)
	data.Set("scope", graphScopes)

	req, err := http.NewRequest(http.MethodPost, tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return 0, "", nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		c.logCall("POST", tokenEndpoint, nil, nil, nil, elapsed, err)
		return 0, "", nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)
	c.logCall("POST", tokenEndpoint, nil, resp, body, elapsed, nil)
	if resp.StatusCode != http.StatusOK {
		var errResp tokenResponse
		_ = json.Unmarshal(body, &errResp)
		return 0, "", nil, fmt.Errorf("token error (HTTP %d): %s - %s", resp.StatusCode, errResp.Error, errResp.ErrorDesc)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return 0, "", nil, fmt.Errorf("decode token: %w", err)
	}

	// Fetch user profile from Graph to get their Microsoft email
	msEmail, err := c.fetchMicrosoftEmail(tr.AccessToken)
	if err != nil {
		slog.Warn("o365: failed to fetch profile email, falling back to local user email", "user_id", userID, "err", err)
	}

	expiry := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)

	// Encrypt tokens before storing
	encAccess, err := EncryptToken(tr.AccessToken, c.cfg.SecretKey)
	if err != nil {
		return 0, "", nil, fmt.Errorf("encrypt access token: %w", err)
	}
	encRefresh, err := EncryptToken(tr.RefreshToken, c.cfg.SecretKey)
	if err != nil {
		return 0, "", nil, fmt.Errorf("encrypt refresh token: %w", err)
	}

	// Preserve existing auto_sync preference if already present
	autoSync := false
	if existing, _ := c.db.GetUserO365Token(userID); existing != nil {
		autoSync = existing.AutoSync
	}

	if err := c.db.SaveUserO365Token(userID, msEmail, encAccess, encRefresh, expiry, autoSync); err != nil {
		return 0, "", nil, fmt.Errorf("save tokens to db: %w", err)
	}

	tokenRec, err := c.db.GetUserO365Token(userID)
	if err != nil {
		return 0, "", nil, err
	}

	return userID, returnTo, tokenRec, nil
}

// fetchMicrosoftEmail calls Graph /v1.0/me to retrieve userPrincipalName or mail.
func (c *Client) fetchMicrosoftEmail(accessToken string) (string, error) {
	urlStr := fmt.Sprintf("%s/me", strings.TrimRight(c.cfg.GraphBaseURL, "/"))
	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		c.logCall("GET", urlStr, nil, nil, nil, elapsed, err)
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)
	c.logCall("GET", urlStr, nil, resp, body, elapsed, nil)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	var me struct {
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		return "", err
	}
	if me.Mail != "" {
		return me.Mail, nil
	}
	return me.UserPrincipalName, nil
}

// GetValidAccessToken returns a decrypted access token for the given user,
// refreshing it automatically via Microsoft OAuth if expired or close to expiry.
func (c *Client) GetValidAccessToken(userID int64) (string, error) {
	tokRec, err := c.db.GetUserO365Token(userID)
	if err != nil {
		return "", err
	}
	if tokRec == nil {
		return "", errors.New("o365 account not connected")
	}

	// If token expires in more than 4 minutes, use existing
	if time.Until(tokRec.TokenExpiry) > 4*time.Minute {
		return DecryptToken(tokRec.AccessToken, c.cfg.SecretKey)
	}

	// Token expired or about to expire -> refresh it
	decRefresh, err := DecryptToken(tokRec.RefreshToken, c.cfg.SecretKey)
	if err != nil || decRefresh == "" {
		return "", errors.New("refresh token missing or cannot be decrypted")
	}

	tokenEndpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", strings.TrimRight(c.cfg.LoginBaseURL, "/"), c.cfg.TenantID)
	data := url.Values{}
	data.Set("client_id", c.cfg.ClientID)
	if c.cfg.ClientSecret != "" {
		data.Set("client_secret", c.cfg.ClientSecret)
	}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", decRefresh)
	data.Set("scope", graphScopes)

	req, err := http.NewRequest(http.MethodPost, tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		c.logCall("POST", tokenEndpoint, nil, nil, nil, elapsed, err)
		return "", fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)
	c.logCall("POST", tokenEndpoint, nil, resp, body, elapsed, nil)
	if resp.StatusCode != http.StatusOK {
		var errResp tokenResponse
		_ = json.Unmarshal(body, &errResp)
		// If refresh token is revoked or invalid, disconnect
		if strings.Contains(errResp.Error, "invalid_grant") {
			_ = c.db.DeleteUserO365Token(userID)
		}
		return "", fmt.Errorf("refresh error (HTTP %d): %s - %s", resp.StatusCode, errResp.Error, errResp.ErrorDesc)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", err
	}

	newRefreshToken := tr.RefreshToken
	if newRefreshToken == "" {
		newRefreshToken = decRefresh
	}

	encAccess, err := EncryptToken(tr.AccessToken, c.cfg.SecretKey)
	if err != nil {
		return "", err
	}
	encRefresh, err := EncryptToken(newRefreshToken, c.cfg.SecretKey)
	if err != nil {
		return "", err
	}

	expiry := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	if err := c.db.SaveUserO365Token(userID, tokRec.MicrosoftEmail, encAccess, encRefresh, expiry, tokRec.AutoSync); err != nil {
		return "", err
	}

	return tr.AccessToken, nil
}

// buildGraphPayload formats an Event into Microsoft Graph Calendar event JSON payload.
func (c *Client) buildGraphPayload(evt Event) map[string]interface{} {
	payload := map[string]interface{}{
		"subject": evt.Subject,
		"body": map[string]string{
			"contentType": "text",
			"content":     evt.Description,
		},
		"showAs":     evt.ShowAs,
		"categories": []string{c.cfg.AppName},
	}

	if evt.Half == "full" || evt.Half == "" {
		// All day event: start is date at 00:00:00, end is next day at 00:00:00 UTC
		t, err := time.Parse("2006-01-02", evt.Date)
		if err != nil {
			t = time.Now()
		}
		nextDay := t.AddDate(0, 0, 1)

		payload["isAllDay"] = true
		payload["start"] = map[string]string{
			"dateTime": t.Format("2006-01-02") + "T00:00:00",
			"timeZone": "UTC",
		}
		payload["end"] = map[string]string{
			"dateTime": nextDay.Format("2006-01-02") + "T00:00:00",
			"timeZone": "UTC",
		}
	} else {
		// Half day event (AM or PM)
		payload["isAllDay"] = false
		startH, endH := "08:30:00", "12:30:00"
		switch evt.Half {
		case "AM":
			parts := strings.Split(c.cfg.AMHours, "-")
			if len(parts) == 2 {
				startH = strings.TrimSpace(parts[0]) + ":00"
				endH = strings.TrimSpace(parts[1]) + ":00"
			}
		case "PM":
			parts := strings.Split(c.cfg.PMHours, "-")
			if len(parts) == 2 {
				startH = strings.TrimSpace(parts[0]) + ":00"
				endH = strings.TrimSpace(parts[1]) + ":00"
			} else {
				startH, endH = "13:30:00", "17:30:00"
			}
		}

		payload["start"] = map[string]string{
			"dateTime": fmt.Sprintf("%sT%s", evt.Date, startH),
			"timeZone": c.cfg.DefaultTimezone,
		}
		payload["end"] = map[string]string{
			"dateTime": fmt.Sprintf("%sT%s", evt.Date, endH),
			"timeZone": c.cfg.DefaultTimezone,
		}
	}

	return payload
}

// CreateEvent creates an event in the user's primary calendar and returns the Graph event ID.
func (c *Client) CreateEvent(accessToken string, evt Event) (string, error) {
	urlStr := fmt.Sprintf("%s/me/calendar/events", strings.TrimRight(c.cfg.GraphBaseURL, "/"))
	payload := c.buildGraphPayload(evt)
	bodyBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, urlStr, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, respBody, err := c.doWithRetry(req, bodyBytes)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create event (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", err
	}
	return res.ID, nil
}

// UpdateEvent updates an existing event in the user's calendar.
func (c *Client) UpdateEvent(accessToken string, eventID string, evt Event) error {
	urlStr := fmt.Sprintf("%s/me/calendar/events/%s", strings.TrimRight(c.cfg.GraphBaseURL, "/"), url.PathEscape(eventID))
	payload := c.buildGraphPayload(evt)
	bodyBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPatch, urlStr, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, respBody, err := c.doWithRetry(req, bodyBytes)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return sql.ErrNoRows // Sentinel for caller to handle 404 (event deleted in Outlook)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update event (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteEvent removes an event from the user's primary calendar.
func (c *Client) DeleteEvent(accessToken string, eventID string) error {
	urlStr := fmt.Sprintf("%s/me/calendar/events/%s", strings.TrimRight(c.cfg.GraphBaseURL, "/"), url.PathEscape(eventID))

	req, err := http.NewRequest(http.MethodDelete, urlStr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, respBody, err := c.doWithRetry(req, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	// If already deleted in Outlook (404) or successfully deleted (204 / 200), treat as success
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}

	return fmt.Errorf("delete event (HTTP %d): %s", resp.StatusCode, string(respBody))
}

// doWithRetry executes an HTTP request, retries once on HTTP 429 Too Many Requests,
// and logs the call to stdout and slog for debugging.
func (c *Client) doWithRetry(req *http.Request, reqBody []byte) (*http.Response, []byte, error) {
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err == nil && resp.StatusCode == http.StatusTooManyRequests {
		_ = resp.Body.Close()
		retryAfter := 2
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if s, err := strconv.Atoi(ra); err == nil && s > 0 && s <= 10 {
				retryAfter = s
			}
		}
		time.Sleep(time.Duration(retryAfter) * time.Second)
		if len(reqBody) > 0 {
			req.Body = io.NopCloser(bytes.NewReader(reqBody))
		}
		resp, err = c.httpClient.Do(req)
	}

	elapsed := time.Since(start)
	var respBody []byte
	if resp != nil && resp.Body != nil {
		respBody, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
	}

	c.logCall(req.Method, req.URL.String(), reqBody, resp, respBody, elapsed, err)
	return resp, respBody, err
}

func (c *Client) logCall(method, urlStr string, reqBody []byte, resp *http.Response, respBody []byte, duration time.Duration, reqErr error) {
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}

	if reqErr != nil {
		slog.Error("o365.graph", "method", method, "url", urlStr, "error", reqErr, "duration_ms", duration.Milliseconds())
		fmt.Printf("[O365 Graph ERROR] %s %s -> %v (%dms)\n", method, urlStr, reqErr, duration.Milliseconds())
		return
	}

	if status >= 400 {
		slog.Error("o365.graph", "method", method, "url", urlStr, "status", status, "error_body", string(respBody), "duration_ms", duration.Milliseconds())
		fmt.Printf("[O365 Graph HTTP %d] %s %s (%dms)\n  Request: %s\n  Response: %s\n", status, method, urlStr, duration.Milliseconds(), string(reqBody), string(respBody))
	} else {
		slog.Info("o365.graph", "method", method, "url", urlStr, "status", status, "duration_ms", duration.Milliseconds())
		if len(reqBody) > 0 {
			fmt.Printf("[O365 Graph HTTP %d] %s %s (%dms) - Payload: %s\n", status, method, urlStr, duration.Milliseconds(), string(reqBody))
		} else {
			fmt.Printf("[O365 Graph HTTP %d] %s %s (%dms)\n", status, method, urlStr, duration.Milliseconds())
		}
	}
}
