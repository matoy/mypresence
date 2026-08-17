// Package jira provides a minimal Jira Cloud REST API client used to look up
// tickets for the "Timesheets managed manually" activity declaration feature.
package jira

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/matoy/mypresence/internal/models"
)

// Client is a minimal Jira Cloud REST API v3 client using Basic auth
// (email + API token).
type Client struct {
	BaseURL    string
	Email      string
	Token      string
	HTTPClient *http.Client
}

// NewClient returns a Jira client. baseURL should not have a trailing slash
// (e.g. "https://your-domain.atlassian.net").
func NewClient(baseURL, email, token string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Email:      email,
		Token:      token,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type searchResponse struct {
	Issues []struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
		} `json:"fields"`
	} `json:"issues"`
	NextPageToken string `json:"nextPageToken"`
	IsLast        bool   `json:"isLast"`
}

// maxSearchPages caps the number of pages fetched as a safety net against an
// unexpectedly large or misbehaving result set.
const maxSearchPages = 100

// SearchRecentTickets returns all tickets from the given Jira project key that
// were updated within the last 30 days, ordered by most recently updated
// first. Results are paginated by the Jira API, so this fetches every page
// until the API reports there are no more results.
func (c *Client) SearchRecentTickets(projectKey string) ([]models.JiraTicket, error) {
	jql := fmt.Sprintf("project = %q AND updated >= -30d ORDER BY updated DESC", projectKey)

	tickets := make([]models.JiraTicket, 0)
	pageToken := ""
	for page := 0; page < maxSearchPages; page++ {
		parsed, err := c.searchPage(jql, pageToken)
		if err != nil {
			return nil, err
		}
		for _, issue := range parsed.Issues {
			tickets = append(tickets, models.JiraTicket{Key: issue.Key, Title: issue.Fields.Summary})
		}
		if parsed.IsLast || parsed.NextPageToken == "" {
			break
		}
		pageToken = parsed.NextPageToken
	}
	return tickets, nil
}

// searchPage performs a single page request against the Jira search API.
func (c *Client) searchPage(jql, pageToken string) (*searchResponse, error) {
	params := url.Values{
		"jql":        {jql},
		"fields":     {"summary"},
		"maxResults": {"100"},
	}
	if pageToken != "" {
		params.Set("nextPageToken", pageToken)
	}
	// The legacy GET /rest/api/3/search endpoint was removed by Atlassian
	// (see https://developer.atlassian.com/changelog/#CHANGE-2046); use its
	// replacement, /rest/api/3/search/jql, instead.
	reqURL := c.BaseURL + "/rest/api/3/search/jql?" + params.Encode()

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(c.Email + ":" + c.Token))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Accept", "application/json")

	slog.Info("jira.request", "method", req.Method, "url", reqURL)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("jira API returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("jira response decode failed: %w", err)
	}
	return &parsed, nil
}
