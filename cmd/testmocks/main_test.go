package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"net/url"
	"strings"
	"testing"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
)

func TestMockServer_O365_Jira_SMTP_And_SAML(t *testing.T) {
	key, cert, err := generateIDPKeyAndCert("127.0.0.1")
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}
	state := newDefaultState("")
	adapter := &mockSAMLAdapter{state: state}

	var srv *httptest.Server
	idp := &saml.IdentityProvider{
		Key:                     key,
		Certificate:             cert,
		ServiceProviderProvider: adapter,
		SessionProvider:         adapter,
		NameIDFormats:           []saml.NameIDFormat{saml.EmailAddressNameIDFormat},
	}
	srv = httptest.NewServer(buildHTTPMux(state, idp))
	defer srv.Close()

	pubURL, _ := url.Parse(srv.URL)
	metaURL := *pubURL
	metaURL.Path = "/saml/idp/metadata"
	ssoURL := *pubURL
	ssoURL.Path = "/saml/idp/sso"
	idp.MetadataURL = metaURL
	idp.SSOURL = ssoURL

	// 1. SAML metadata fetch + IdP AuthnRequest handling
	fetchedMeta, err := samlsp.FetchMetadata(context.Background(), srv.Client(), metaURL)
	if err != nil {
		t.Fatalf("FetchMetadata: %v", err)
	}
	if fetchedMeta.EntityID != metaURL.String() {
		t.Fatalf("unexpected IdP entityID: %q", fetchedMeta.EntityID)
	}

	// 2. Persona switch & SAML AuthnRequest flow check
	reqPersona, _ := http.NewRequest(http.MethodPost, srv.URL+"/__mock/persona?persona=collab", nil)
	resp, err := srv.Client().Do(reqPersona)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("set persona failed: err=%v resp=%+v", err, resp)
	}
	_ = resp.Body.Close()

	spKey, spCert, err := generateIDPKeyAndCert("127.0.0.1")
	if err != nil {
		t.Fatalf("generate SP cert: %v", err)
	}
	spRoot, _ := url.Parse("http://127.0.0.1:8080")
	acsURL := *spRoot
	acsURL.Path = "/saml/acs"
	spMetaURL := *spRoot
	spMetaURL.Path = "/saml/metadata"
	sp := &saml.ServiceProvider{
		EntityID:    spMetaURL.String(),
		Key:         spKey,
		Certificate: spCert,
		MetadataURL: spMetaURL,
		AcsURL:      acsURL,
		IDPMetadata: fetchedMeta,
	}
	authnReq, err := sp.MakeAuthenticationRequest(ssoURL.String(), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		t.Fatalf("MakeAuthenticationRequest: %v", err)
	}
	redir, err := authnReq.Redirect("/calendar", sp)
	if err != nil {
		t.Fatalf("Redirect: %v", err)
	}
	ssoResp, err := srv.Client().Get(redir.String())
	if err != nil {
		t.Fatalf("GET SSO: %v", err)
	}
	defer ssoResp.Body.Close() //nolint:errcheck
	if ssoResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 HTML form from SSO, got %d", ssoResp.StatusCode)
	}

	// 3. O365 OAuth + Graph flow
	noRedirectClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	authResp, err := noRedirectClient.Get(srv.URL + "/common/oauth2/v2.0/authorize?redirect_uri=http://127.0.0.1:8080/auth/o365/callback&state=st1")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	_ = authResp.Body.Close()
	loc := authResp.Header.Get("Location")
	if !strings.Contains(loc, "code=mock-code-") || !strings.Contains(loc, "state=st1") {
		t.Fatalf("unexpected O365 redirect location: %q", loc)
	}

	tokResp, err := srv.Client().PostForm(srv.URL+"/common/oauth2/v2.0/token", url.Values{"grant_type": {"authorization_code"}})
	if err != nil || tokResp.StatusCode != http.StatusOK {
		t.Fatalf("token failed: err=%v resp=%+v", err, tokResp)
	}
	_ = tokResp.Body.Close()

	evtBody := []byte(`{"subject":"Teletravail","isAllDay":true}`)
	evtReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1.0/me/calendar/events", bytes.NewReader(evtBody))
	evtReq.Header.Set("Authorization", "Bearer tok-1")
	evtReq.Header.Set("Content-Type", "application/json")
	evtResp, err := srv.Client().Do(evtReq)
	if err != nil || evtResp.StatusCode != http.StatusCreated {
		t.Fatalf("create graph event failed: err=%v resp=%+v", err, evtResp)
	}
	var created struct{ ID string }
	_ = json.NewDecoder(evtResp.Body).Decode(&created)
	_ = evtResp.Body.Close()
	if created.ID == "" {
		t.Fatal("expected Graph event ID")
	}

	// 4. Jira search/jql
	jiraReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/ex/jira/cloud-1/rest/api/3/search/jql?jql="+url.QueryEscape(`project = "PRJ"`), nil)
	jiraReq.Header.Set("Authorization", "Bearer jira-tok")
	jiraResp, err := srv.Client().Do(jiraReq)
	if err != nil || jiraResp.StatusCode != http.StatusOK {
		t.Fatalf("jira search failed: err=%v resp=%+v", err, jiraResp)
	}
	var jiraBody struct {
		Issues []struct{ Key string } `json:"issues"`
	}
	_ = json.NewDecoder(jiraResp.Body).Decode(&jiraBody)
	_ = jiraResp.Body.Close()
	if len(jiraBody.Issues) == 0 || !strings.HasPrefix(jiraBody.Issues[0].Key, "PRJ-") {
		t.Fatalf("unexpected Jira issues: %+v", jiraBody.Issues)
	}

	// 5. Capture SMTP listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen smtp: %v", err)
	}
	defer ln.Close() //nolint:errcheck
	go serveSMTP(ln, state)

	msg := "From: noreply@presence.local\r\nTo: alice@example.com\r\nSubject: Password reset\r\n\r\nReset here: http://127.0.0.1:8080/reset-password?token=abc123\r\n"
	if err := smtp.SendMail(ln.Addr().String(), nil, "noreply@presence.local", []string{"alice@example.com"}, []byte(msg)); err != nil {
		t.Fatalf("smtp SendMail: %v", err)
	}

	stateResp, err := srv.Client().Get(srv.URL + "/__mock/state")
	if err != nil {
		t.Fatalf("GET /__mock/state: %v", err)
	}
	defer stateResp.Body.Close() //nolint:errcheck
	var snap map[string]interface{}
	_ = json.NewDecoder(stateResp.Body).Decode(&snap)
	emails, _ := snap["emails"].([]interface{})
	if len(emails) != 1 {
		t.Fatalf("expected 1 captured email, got %d (%+v)", len(emails), snap)
	}
}
