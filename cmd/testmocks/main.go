// Package main implements cmd/testmocks, a lightweight in-process mock suite for
// release acceptance testing (SAML 2.0 IdP, Office 365 OAuth2 + Graph Calendar,
// Jira Cloud REST API v3, and capture SMTP server) plus /__mock/* inspection APIs.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/textproto"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/crewjam/saml"
)

// Persona describes the user identity & Entra group claims emitted in SAML assertions.
type Persona struct {
	Name        string   `json:"name"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Groups      []string `json:"groups"`
}

// CapturedEmail records an SMTP message delivered to the mock SMTP listener.
type CapturedEmail struct {
	From       string    `json:"from"`
	To         []string  `json:"to"`
	Subject    string    `json:"subject"`
	Body       string    `json:"body"`
	ResetLinks []string  `json:"reset_links"`
	ReceivedAt time.Time `json:"received_at"`
}

// MockO365Event records a calendar event created/updated/deleted via mock Graph.
type MockO365Event struct {
	ID         string                 `json:"id"`
	Subject    string                 `json:"subject"`
	Body       map[string]string      `json:"body,omitempty"`
	ShowAs     string                 `json:"showAs,omitempty"`
	Categories []string               `json:"categories,omitempty"`
	IsAllDay   bool                   `json:"isAllDay"`
	Start      map[string]string      `json:"start,omitempty"`
	End        map[string]string      `json:"end,omitempty"`
	Raw        map[string]interface{} `json:"raw,omitempty"`
	Deleted    bool                   `json:"deleted"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

// MockJiraIssue defines a mock Jira ticket returned by search/jql.
type MockJiraIssue struct {
	Project string `json:"project"`
	Key     string `json:"key"`
	Summary string `json:"summary"`
}

// MockJiraRequest records a call made to mock Jira search/jql.
type MockJiraRequest struct {
	Path      string    `json:"path"`
	JQL       string    `json:"jql"`
	AuthMode  string    `json:"auth_mode"`
	Timestamp time.Time `json:"timestamp"`
}

// MockSAMLLogin records a SAML assertion issued by the mock IdP.
type MockSAMLLogin struct {
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Groups      []string  `json:"groups"`
	Audience    string    `json:"audience"`
	Timestamp   time.Time `json:"timestamp"`
}

// State holds all inspectable mock state.
type State struct {
	mu            sync.RWMutex
	seq           atomic.Int64
	activePersona string
	customPersona *Persona
	personas      map[string]Persona
	samlLogins    []MockSAMLLogin
	o365UserEmail string
	o365Tokens    int
	o365Events    map[string]*MockO365Event
	jiraIssues    []MockJiraIssue
	jiraRequests  []MockJiraRequest
	emails        []CapturedEmail
	spACSOverride string
}

func newDefaultState(spACS string) *State {
	s := &State{
		spACSOverride: spACS,
		o365Events:    make(map[string]*MockO365Event),
	}
	s.resetLocked()
	return s
}

func (s *State) resetLocked() {
	s.activePersona = "admin"
	s.customPersona = nil
	s.personas = map[string]Persona{
		"admin": {
			Name:        "admin",
			Email:       "saml.admin@example.com",
			DisplayName: "SAML Admin",
			Groups:      []string{"group-global-id"},
		},
		"floorplan": {
			Name:        "floorplan",
			Email:       "saml.fp@example.com",
			DisplayName: "SAML Floorplan Mgr",
			Groups:      []string{"group-fp-id"},
		},
		"collab": {
			Name:        "collab",
			Email:       "saml.collab@example.com",
			DisplayName: "SAML Collab",
			Groups:      []string{"unrelated-group"},
		},
	}
	s.samlLogins = nil
	s.o365UserEmail = "o365.user@office.example.com"
	s.o365Tokens = 0
	s.o365Events = make(map[string]*MockO365Event)
	s.jiraIssues = []MockJiraIssue{
		{Project: "PRJ", Key: "PRJ-101", Summary: "Migration PostgreSQL 16"},
		{Project: "PRJ", Key: "PRJ-102", Summary: "Sécurisation SSO Entra ID"},
		{Project: "PRJ", Key: "PRJ-201", Summary: "Automatisation recette E2E"},
		{Project: "OPS", Key: "OPS-11", Summary: "Observabilité & Alertes"},
	}
	s.jiraRequests = nil
	s.emails = nil
}

func (s *State) currentPersona(override string) Persona {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if override != "" {
		if p, ok := s.personas[override]; ok {
			return p
		}
	}
	if s.customPersona != nil {
		return *s.customPersona
	}
	if p, ok := s.personas[s.activePersona]; ok {
		return p
	}
	return s.personas["collab"]
}

type mockSAMLAdapter struct {
	state *State
}

func (a *mockSAMLAdapter) GetServiceProvider(r *http.Request, serviceProviderID string) (*saml.EntityDescriptor, error) {
	a.state.mu.RLock()
	acsURL := a.state.spACSOverride
	a.state.mu.RUnlock()

	if acsURL == "" {
		if u, err := url.Parse(serviceProviderID); err == nil && u.Scheme != "" && u.Host != "" {
			u.Path = "/saml/acs"
			u.RawQuery = ""
			u.Fragment = ""
			acsURL = u.String()
		}
	}
	if acsURL == "" {
		return nil, os.ErrNotExist
	}

	return &saml.EntityDescriptor{
		EntityID: serviceProviderID,
		SPSSODescriptors: []saml.SPSSODescriptor{
			{
				SSODescriptor: saml.SSODescriptor{
					RoleDescriptor: saml.RoleDescriptor{
						ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol",
					},
					NameIDFormats: []saml.NameIDFormat{saml.EmailAddressNameIDFormat, saml.TransientNameIDFormat},
				},
				AssertionConsumerServices: []saml.IndexedEndpoint{
					{
						Binding:  saml.HTTPPostBinding,
						Location: acsURL,
						Index:    1,
					},
				},
			},
		},
	}, nil
}

func (a *mockSAMLAdapter) GetSession(w http.ResponseWriter, r *http.Request, req *saml.IdpAuthnRequest) *saml.Session {
	override := r.URL.Query().Get("persona")
	p := a.state.currentPersona(override)

	now := time.Now()
	groups := make([]saml.AttributeValue, 0, len(p.Groups))
	for _, g := range p.Groups {
		groups = append(groups, saml.AttributeValue{Type: "xs:string", Value: g})
	}

	attrs := []saml.Attribute{
		{
			Name:       "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
			NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:uri",
			Values:     []saml.AttributeValue{{Type: "xs:string", Value: p.Email}},
		},
		{
			Name:       "http://schemas.microsoft.com/identity/claims/displayname",
			NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:uri",
			Values:     []saml.AttributeValue{{Type: "xs:string", Value: p.DisplayName}},
		},
	}
	if len(groups) > 0 {
		attrs = append(attrs, saml.Attribute{
			Name:       "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups",
			NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:uri",
			Values:     groups,
		})
	}

	audience := ""
	if req != nil && req.ServiceProviderMetadata != nil {
		audience = req.ServiceProviderMetadata.EntityID
	}

	a.state.mu.Lock()
	a.state.samlLogins = append(a.state.samlLogins, MockSAMLLogin{
		Email:       p.Email,
		DisplayName: p.DisplayName,
		Groups:      append([]string(nil), p.Groups...),
		Audience:    audience,
		Timestamp:   now,
	})
	a.state.mu.Unlock()

	return &saml.Session{
		ID:               fmt.Sprintf("sess-%d", a.state.seq.Add(1)),
		CreateTime:       now,
		ExpireTime:       now.Add(8 * time.Hour),
		Index:            fmt.Sprintf("idx-%d", a.state.seq.Add(1)),
		NameID:           p.Email,
		NameIDFormat:     string(saml.EmailAddressNameIDFormat),
		UserEmail:        p.Email,
		UserCommonName:   p.DisplayName,
		CustomAttributes: attrs,
	}
}

func generateIDPKeyAndCert(publicHost string) (*rsa.PrivateKey, *x509.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "mypresence-mock-idp",
			Organization: []string{"mypresence-testmocks"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
		DNSNames:              []string{publicHost, "localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return key, cert, nil
}

func buildHTTPMux(state *State, idp *saml.IdentityProvider) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// SAML IdP endpoints
	mux.HandleFunc("GET /saml/idp/metadata", idp.ServeMetadata)
	mux.HandleFunc("GET /saml/idp/sso", idp.ServeSSO)
	mux.HandleFunc("POST /saml/idp/sso", idp.ServeSSO)

	// Inspection & control endpoints
	mux.HandleFunc("GET /__mock/state", func(w http.ResponseWriter, _ *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		events := make([]*MockO365Event, 0, len(state.o365Events))
		for _, ev := range state.o365Events {
			cp := *ev
			events = append(events, &cp)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"active_persona": state.activePersona,
			"custom_persona": state.customPersona,
			"saml_logins":    append([]MockSAMLLogin(nil), state.samlLogins...),
			"o365_user":      state.o365UserEmail,
			"o365_tokens":    state.o365Tokens,
			"o365_events":    events,
			"jira_issues":    append([]MockJiraIssue(nil), state.jiraIssues...),
			"jira_requests":  append([]MockJiraRequest(nil), state.jiraRequests...),
			"emails":         append([]CapturedEmail(nil), state.emails...),
		})
	})

	mux.HandleFunc("POST /__mock/reset", func(w http.ResponseWriter, _ *http.Request) {
		state.mu.Lock()
		state.resetLocked()
		state.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
	})

	mux.HandleFunc("POST /__mock/persona", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("persona"); q != "" {
			state.mu.Lock()
			if _, ok := state.personas[q]; !ok {
				state.mu.Unlock()
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown persona"})
				return
			}
			state.activePersona = q
			state.customPersona = nil
			state.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]string{"persona": q})
			return
		}
		var req Persona
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		if req.Email == "" && req.Name != "" {
			if _, ok := state.personas[req.Name]; !ok {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown persona"})
				return
			}
			state.activePersona = req.Name
			state.customPersona = nil
		} else if req.Email != "" {
			if req.DisplayName == "" {
				req.DisplayName = req.Email
			}
			state.customPersona = &req
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "persona name or email required"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /__mock/emails", func(w http.ResponseWriter, _ *http.Request) {
		state.mu.RLock()
		emails := append([]CapturedEmail(nil), state.emails...)
		state.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{"emails": emails})
	})

	mux.HandleFunc("POST /__mock/o365/user", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email required"})
			return
		}
		state.mu.Lock()
		state.o365UserEmail = body.Email
		state.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"email": body.Email})
	})

	// Office 365 OAuth2 + Microsoft Graph endpoints
	mux.HandleFunc("GET /{tenant}/oauth2/v2.0/authorize", func(w http.ResponseWriter, r *http.Request) {
		redirectURI := r.URL.Query().Get("redirect_uri")
		st := r.URL.Query().Get("state")
		if redirectURI == "" {
			http.Error(w, "missing redirect_uri", http.StatusBadRequest)
			return
		}
		target, err := url.Parse(redirectURI)
		if err != nil {
			http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
			return
		}
		q := target.Query()
		q.Set("code", fmt.Sprintf("mock-code-%d", state.seq.Add(1)))
		if st != "" {
			q.Set("state", st)
		}
		target.RawQuery = q.Encode()
		http.Redirect(w, r, target.String(), http.StatusFound)
	})

	mux.HandleFunc("POST /{tenant}/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		n := state.seq.Add(1)
		state.mu.Lock()
		state.o365Tokens++
		state.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"token_type":    "Bearer",
			"scope":         "Calendars.ReadWrite User.Read offline_access",
			"expires_in":    3600,
			"access_token":  fmt.Sprintf("mock-access-%d", n),
			"refresh_token": fmt.Sprintf("mock-refresh-%d", n),
		})
	})

	mux.HandleFunc("GET /v1.0/me", func(w http.ResponseWriter, r *http.Request) {
		if !hasBearerAuth(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		state.mu.RLock()
		em := state.o365UserEmail
		state.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]string{
			"mail":              em,
			"userPrincipalName": em,
		})
	})

	mux.HandleFunc("POST /v1.0/me/calendar/events", func(w http.ResponseWriter, r *http.Request) {
		if !hasBearerAuth(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		var raw map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
			return
		}
		id := fmt.Sprintf("evt-%d", state.seq.Add(1))
		ev := parseO365Event(id, raw)
		state.mu.Lock()
		state.o365Events[id] = ev
		state.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
	})

	mux.HandleFunc("PATCH /v1.0/me/calendar/events/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !hasBearerAuth(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		id := r.PathValue("id")
		var raw map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
			return
		}
		state.mu.Lock()
		existing, ok := state.o365Events[id]
		if !ok || existing.Deleted {
			state.mu.Unlock()
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": map[string]string{"code": "ErrorItemNotFound"}})
			return
		}
		state.o365Events[id] = parseO365Event(id, raw)
		state.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": id})
	})

	mux.HandleFunc("DELETE /v1.0/me/calendar/events/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !hasBearerAuth(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		id := r.PathValue("id")
		state.mu.Lock()
		if existing, ok := state.o365Events[id]; ok {
			existing.Deleted = true
			existing.UpdatedAt = time.Now()
		}
		state.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	// Jira Cloud REST API v3 mock (Scoped CloudID gateway + Legacy direct)
	handleJiraSearch := func(w http.ResponseWriter, r *http.Request) {
		authHdr := r.Header.Get("Authorization")
		authMode := "none"
		if strings.HasPrefix(authHdr, "Bearer ") {
			authMode = "bearer"
		} else if strings.HasPrefix(authHdr, "Basic ") {
			raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authHdr, "Basic "))
			if err == nil && strings.Contains(string(raw), ":") {
				authMode = "basic"
			}
		}
		if authMode == "none" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing auth"})
			return
		}

		jql := r.URL.Query().Get("jql")
		projKey := extractJiraProjectFromJQL(jql)

		state.mu.Lock()
		state.jiraRequests = append(state.jiraRequests, MockJiraRequest{
			Path:      r.URL.Path,
			JQL:       jql,
			AuthMode:  authMode,
			Timestamp: time.Now(),
		})
		var issues []map[string]interface{}
		for _, is := range state.jiraIssues {
			if projKey == "" || strings.EqualFold(is.Project, projKey) {
				issues = append(issues, map[string]interface{}{
					"key": is.Key,
					"fields": map[string]string{
						"summary": is.Summary,
					},
				})
			}
		}
		state.mu.Unlock()

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"issues":        issues,
			"isLast":        true,
			"nextPageToken": "",
		})
	}
	mux.HandleFunc("GET /ex/jira/{cloudId}/rest/api/3/search/jql", handleJiraSearch)
	mux.HandleFunc("GET /rest/api/3/search/jql", handleJiraSearch)

	return mux
}

func parseO365Event(id string, raw map[string]interface{}) *MockO365Event {
	subj, _ := raw["subject"].(string)
	showAs, _ := raw["showAs"].(string)
	isAllDay, _ := raw["isAllDay"].(bool)
	bodyMap := map[string]string{}
	if b, ok := raw["body"].(map[string]interface{}); ok {
		for k, v := range b {
			if vs, ok := v.(string); ok {
				bodyMap[k] = vs
			}
		}
	}
	startMap := toStringMap(raw["start"])
	endMap := toStringMap(raw["end"])
	var cats []string
	if cs, ok := raw["categories"].([]interface{}); ok {
		for _, c := range cs {
			if csStr, ok := c.(string); ok {
				cats = append(cats, csStr)
			}
		}
	}
	return &MockO365Event{
		ID:         id,
		Subject:    subj,
		Body:       bodyMap,
		ShowAs:     showAs,
		Categories: cats,
		IsAllDay:   isAllDay,
		Start:      startMap,
		End:        endMap,
		Raw:        raw,
		UpdatedAt:  time.Now(),
	}
}

func toStringMap(v interface{}) map[string]string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}

var jqlProjectRe = regexp.MustCompile(`(?i)project\s*=\s*"?([A-Za-z0-9_-]+)"?`)
var linkExtractRe = regexp.MustCompile(`https?://[^\s<>"]+`)

func extractJiraProjectFromJQL(jql string) string {
	m := jqlProjectRe.FindStringSubmatch(jql)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func hasBearerAuth(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func serveSMTP(ln net.Listener, state *State) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go handleSMTPConn(conn, state)
	}
}

func handleSMTPConn(conn net.Conn, state *State) {
	defer conn.Close() //nolint:errcheck
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	tp := textproto.NewConn(conn)
	_ = tp.PrintfLine("220 mock.smtp ESMTP ready")

	var from string
	var to []string
	reader := bufio.NewReader(conn)
	// Use textproto Reader/Writer over the buffered reader via conn directly cleanly:
	_ = reader
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		upper := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(upper, "EHLO "):
			_ = tp.PrintfLine("250-mock.smtp")
			_ = tp.PrintfLine("250-8BITMIME")
			_ = tp.PrintfLine("250 OK")
		case strings.HasPrefix(upper, "HELO "):
			_ = tp.PrintfLine("250 mock.smtp")
		case strings.HasPrefix(upper, "AUTH "):
			_ = tp.PrintfLine("235 2.7.0 Authentication successful")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			from = strings.Trim(strings.TrimSpace(line[len("MAIL FROM:"):]), "<>")
			_ = tp.PrintfLine("250 2.1.0 OK")
		case strings.HasPrefix(upper, "RCPT TO:"):
			addr := strings.Trim(strings.TrimSpace(line[len("RCPT TO:"):]), "<>")
			to = append(to, addr)
			_ = tp.PrintfLine("250 2.1.5 OK")
		case upper == "DATA":
			_ = tp.PrintfLine("354 Start mail input; end with <CRLF>.<CRLF>")
			lines, err := tp.ReadDotLines()
			if err != nil {
				return
			}
			raw := strings.Join(lines, "\r\n")
			msg, parseErr := mail.ReadMessage(strings.NewReader(raw))
			subj := ""
			body := raw
			if parseErr == nil {
				subj = msg.Header.Get("Subject")
				var b strings.Builder
				buf := make([]byte, 4096)
				for {
					n, rerr := msg.Body.Read(buf)
					if n > 0 {
						b.Write(buf[:n])
					}
					if rerr != nil {
						break
					}
				}
				body = b.String()
			}
			links := linkExtractRe.FindAllString(body, -1)
			state.mu.Lock()
			state.emails = append(state.emails, CapturedEmail{
				From:       from,
				To:         to,
				Subject:    subj,
				Body:       body,
				ResetLinks: links,
				ReceivedAt: time.Now(),
			})
			state.mu.Unlock()
			_ = tp.PrintfLine("250 2.0.0 OK queued")
			from = ""
			to = nil
		case upper == "RSET":
			from = ""
			to = nil
			_ = tp.PrintfLine("250 2.0.0 OK")
		case upper == "NOOP":
			_ = tp.PrintfLine("250 2.0.0 OK")
		case upper == "QUIT":
			_ = tp.PrintfLine("221 2.0.0 Bye")
			return
		default:
			_ = tp.PrintfLine("250 OK")
		}
	}
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func main() {
	httpAddr := flag.String("http-addr", envOr("MOCK_HTTP_ADDR", "127.0.0.1:19080"), "HTTP listen address")
	smtpAddr := flag.String("smtp-addr", envOr("MOCK_SMTP_ADDR", "127.0.0.1:19025"), "SMTP listen address")
	publicURL := flag.String("public-url", envOr("MOCK_PUBLIC_URL", ""), "Public base URL advertised by SAML IdP metadata")
	samlACSURL := flag.String("saml-acs-url", envOr("MOCK_SAML_ACS_URL", "http://127.0.0.1:8080/saml/acs"), "Default SP Assertion Consumer Service URL")
	flag.Parse()

	pub := strings.TrimRight(*publicURL, "/")
	if pub == "" {
		pub = "http://" + *httpAddr
	}
	pubParsed, err := url.Parse(pub)
	if err != nil {
		log.Fatalf("invalid public-url %q: %v", pub, err)
	}

	key, cert, err := generateIDPKeyAndCert(pubParsed.Hostname())
	if err != nil {
		log.Fatalf("generate IdP cert: %v", err)
	}

	state := newDefaultState(*samlACSURL)
	adapter := &mockSAMLAdapter{state: state}

	metaURL, _ := url.Parse(pub + "/saml/idp/metadata")
	ssoURL, _ := url.Parse(pub + "/saml/idp/sso")

	idp := &saml.IdentityProvider{
		Key:                     key,
		Certificate:             cert,
		MetadataURL:             *metaURL,
		SSOURL:                  *ssoURL,
		ServiceProviderProvider: adapter,
		SessionProvider:         adapter,
		NameIDFormats:           []saml.NameIDFormat{saml.EmailAddressNameIDFormat, saml.TransientNameIDFormat},
	}

	httpLn, err := net.Listen("tcp", *httpAddr)
	if err != nil {
		log.Fatalf("listen http %s: %v", *httpAddr, err)
	}
	smtpLn, err := net.Listen("tcp", *smtpAddr)
	if err != nil {
		log.Fatalf("listen smtp %s: %v", *smtpAddr, err)
	}

	srv := &http.Server{
		Handler:           buildHTTPMux(state, idp),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go serveSMTP(smtpLn, state)
	go func() {
		log.Printf("testmocks HTTP listening on %s (public=%s), SMTP on %s", *httpAddr, pub, *smtpAddr)
		if err := srv.Serve(httpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http serve: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = smtpLn.Close()
	_ = srv.Shutdown(ctx)
}
