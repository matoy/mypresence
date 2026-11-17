package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewjam/saml"
	_ "github.com/lib/pq"
	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
)

type recettePersona struct {
	Email       string
	DisplayName string
	Groups      []string
}

type recetteMockState struct {
	mu           sync.Mutex
	seq          int64
	acsURL       string
	persona      recettePersona
	o365Email    string
	o365Events   map[string]map[string]interface{}
	jiraRequests []string
	emails       []string
}

type recetteEnv struct {
	t      *testing.T
	cfg    *config.Config
	db     *db.DB
	appSrv *httptest.Server
	mocks  *httptest.Server
	state  *recetteMockState
	smtpLn net.Listener
}

func newRecetteEnv(t *testing.T) *recetteEnv {
	t.Helper()

	key, cert, err := generateRecetteCert()
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}
	state := &recetteMockState{
		persona: recettePersona{
			Email:       "saml.admin@example.com",
			DisplayName: "SAML Admin",
			Groups:      []string{"group-global-id"},
		},
		o365Email:  "alice.o365@office.example.com",
		o365Events: make(map[string]map[string]interface{}),
	}

	adapter := &recetteSAMLAdapter{state: state}
	idp := &saml.IdentityProvider{
		Key:                     key,
		Certificate:             cert,
		ServiceProviderProvider: adapter,
		SessionProvider:         adapter,
		NameIDFormats:           []saml.NameIDFormat{saml.EmailAddressNameIDFormat},
	}

	mockSrv := httptest.NewServer(buildRecetteMockMux(state, idp))
	t.Cleanup(mockSrv.Close)

	pubURL, _ := url.Parse(mockSrv.URL)
	metaURL := *pubURL
	metaURL.Path = "/saml/idp/metadata"
	ssoURL := *pubURL
	ssoURL.Path = "/saml/idp/sso"
	idp.MetadataURL = metaURL
	idp.SSOURL = ssoURL

	smtpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen smtp: %v", err)
	}
	t.Cleanup(func() { _ = smtpLn.Close() })
	go serveRecetteSMTP(smtpLn, state)

	dataDir := t.TempDir()
	driver := strings.ToLower(strings.TrimSpace(os.Getenv("TEST_DB_DRIVER")))
	if driver == "" {
		driver = "sqlite"
	}

	cfg := &config.Config{
		Port:                     "8080",
		DataDir:                  dataDir,
		SecretKey:                "recette-super-secret-key-at-least-32-chars",
		DBDriver:                 driver,
		DBHost:                   envDefault("DB_HOST", "localhost"),
		DBPort:                   envDefault("DB_PORT", "5432"),
		DBName:                   envDefault("DB_NAME", "mypresence"),
		DBUser:                   envDefault("DB_USER", "postgres"),
		DBPassword:               envDefault("DB_PASSWORD", "postgres"),
		DBSSLMode:                envDefault("DB_SSL_MODE", "disable"),
		AppName:                  "Presence Recette",
		AdminUser:                "admin",
		AdminPassword:            "AdminRecette#2026!",
		DefaultLang:              "fr",
		MetricsToken:             "recette-metrics-token",
		SAMLEnabled:              true,
		SAMLAutoLogin:            false,
		SAMLIDPMetadataURL:       mockSrv.URL + "/saml/idp/metadata",
		SAMLGroupsClaim:          "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups",
		SAMLGroupGlobal:          "group-global-id",
		SAMLGroupFloorplanManager: "group-fp-id",
		O365SyncEnabled:          true,
		O365TenantID:             "common",
		O365ClientID:             "recette-o365-client",
		O365ClientSecret:         "recette-o365-secret",
		O365DefaultTimezone:      "Europe/Paris",
		O365AMHours:              "08:30-12:30",
		O365PMHours:              "13:30-17:30",
		O365LoginBaseURL:         mockSrv.URL,
		O365GraphBaseURL:         mockSrv.URL + "/v1.0",
		JiraEnabled:              true,
		JiraCloudID:              "cloud-recette",
		JiraBaseURL:              mockSrv.URL,
		JiraToken:                "recette-jira-token",
		SMTPURL:                  "smtp://" + smtpLn.Addr().String(),
		SMTPFrom:                 "noreply@presence.local",
		OnsiteRatioThreshold:     60.0,
	}

	if driver == "postgres" {
		resetPostgresSchema(t, cfg)
	}

	database, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open (%s): %v", driver, err)
	}
	database.SetBcryptCost(4)
	t.Cleanup(database.Close)

	if err := database.SeedDefaults(cfg.AdminUser, cfg.AdminPassword); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	var handler http.Handler
	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(appSrv.Close)

	cfg.AppURL = appSrv.URL
	cfg.SAMLRootURL = appSrv.URL
	cfg.SAMLEntityID = appSrv.URL + "/saml/metadata"
	cfg.O365RedirectURL = appSrv.URL + "/auth/o365/callback"
	state.acsURL = appSrv.URL + "/saml/acs"

	handler = buildAppMux(cfg, database)

	return &recetteEnv{
		t:      t,
		cfg:    cfg,
		db:     database,
		appSrv: appSrv,
		mocks:  mockSrv,
		state:  state,
		smtpLn: smtpLn,
	}
}

func resetPostgresSchema(t *testing.T, cfg *config.Config) {
	t.Helper()
	dsn := fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.DBUser, cfg.DBPassword, cfg.DBSSLMode)
	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("postgres connect for reset: %v", err)
	}
	defer sqlDB.Close() //nolint:errcheck
	if _, err := sqlDB.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatalf("postgres reset schema: %v", err)
	}
}

func envDefault(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

type recetteSAMLAdapter struct {
	state *recetteMockState
}

func (a *recetteSAMLAdapter) GetServiceProvider(_ *http.Request, spID string) (*saml.EntityDescriptor, error) {
	return &saml.EntityDescriptor{
		EntityID: spID,
		SPSSODescriptors: []saml.SPSSODescriptor{{
			SSODescriptor: saml.SSODescriptor{
				RoleDescriptor: saml.RoleDescriptor{ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol"},
			},
			AssertionConsumerServices: []saml.IndexedEndpoint{{
				Binding:  saml.HTTPPostBinding,
				Location: a.state.acsURL,
				Index:    1,
			}},
		}},
	}, nil
}

func (a *recetteSAMLAdapter) GetSession(_ http.ResponseWriter, _ *http.Request, _ *saml.IdpAuthnRequest) *saml.Session {
	a.state.mu.Lock()
	p := a.state.persona
	a.state.seq++
	seq := a.state.seq
	a.state.mu.Unlock()

	groups := make([]saml.AttributeValue, 0, len(p.Groups))
	for _, g := range p.Groups {
		groups = append(groups, saml.AttributeValue{Type: "xs:string", Value: g})
	}
	now := time.Now()
	return &saml.Session{
		ID:             fmt.Sprintf("rs-%d", seq),
		CreateTime:     now,
		ExpireTime:     now.Add(time.Hour),
		Index:          fmt.Sprintf("ri-%d", seq),
		NameID:         p.Email,
		NameIDFormat:   string(saml.EmailAddressNameIDFormat),
		UserEmail:      p.Email,
		UserCommonName: p.DisplayName,
		CustomAttributes: []saml.Attribute{
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
			{
				Name:       "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups",
				NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:uri",
				Values:     groups,
			},
		},
	}
}

func generateRecetteCert() (*rsa.PrivateKey, *x509.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "recette-idp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		DNSNames:     []string{"127.0.0.1", "localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	return key, cert, err
}

func buildRecetteMockMux(state *recetteMockState, idp *saml.IdentityProvider) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /saml/idp/metadata", idp.ServeMetadata)
	mux.HandleFunc("GET /saml/idp/sso", idp.ServeSSO)
	mux.HandleFunc("GET /{tenant}/oauth2/v2.0/authorize", func(w http.ResponseWriter, r *http.Request) {
		target, _ := url.Parse(r.URL.Query().Get("redirect_uri"))
		q := target.Query()
		state.mu.Lock()
		state.seq++
		q.Set("code", fmt.Sprintf("recette-code-%d", state.seq))
		state.mu.Unlock()
		q.Set("state", r.URL.Query().Get("state"))
		target.RawQuery = q.Encode()
		http.Redirect(w, r, target.String(), http.StatusFound)
	})
	mux.HandleFunc("POST /{tenant}/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		state.seq++
		seq := state.seq
		state.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token_type":    "Bearer",
			"expires_in":    3600,
			"access_token":  fmt.Sprintf("access-%d", seq),
			"refresh_token": fmt.Sprintf("refresh-%d", seq),
		})
	})
	mux.HandleFunc("GET /v1.0/me", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		em := state.o365Email
		state.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"mail": em, "userPrincipalName": em})
	})
	mux.HandleFunc("POST /v1.0/me/calendar/events", func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		state.mu.Lock()
		state.seq++
		id := fmt.Sprintf("evt-%d", state.seq)
		state.o365Events[id] = payload
		state.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
	})
	mux.HandleFunc("GET /ex/jira/{cloudId}/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		state.jiraRequests = append(state.jiraRequests, r.URL.RawQuery)
		state.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issues": []map[string]interface{}{
				{"key": "PRJ-101", "fields": map[string]string{"summary": "Migration PostgreSQL 16"}},
				{"key": "PRJ-102", "fields": map[string]string{"summary": "Sécurisation SSO Entra ID"}},
			},
			"isLast": true,
		})
	})
	return mux
}

func serveRecetteSMTP(ln net.Listener, state *recetteMockState) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go func(c net.Conn) {
			defer c.Close() //nolint:errcheck
			tp := textproto.NewConn(c)
			_ = tp.PrintfLine("220 recette.smtp ready")
			_ = bufio.NewReader(c)
			for {
				line, err := tp.ReadLine()
				if err != nil {
					return
				}
				u := strings.ToUpper(strings.TrimSpace(line))
				switch {
				case strings.HasPrefix(u, "EHLO "):
					_ = tp.PrintfLine("250-recette.smtp")
					_ = tp.PrintfLine("250 OK")
				case strings.HasPrefix(u, "HELO "), strings.HasPrefix(u, "MAIL "), strings.HasPrefix(u, "RCPT "):
					_ = tp.PrintfLine("250 OK")
				case u == "DATA":
					_ = tp.PrintfLine("354 End data with <CR><LF>.<CR><LF>")
					lines, err := tp.ReadDotLines()
					if err != nil {
						return
					}
					msg, err := mail.ReadMessage(strings.NewReader(strings.Join(lines, "\r\n")))
					bodyStr := strings.Join(lines, "\r\n")
					if err == nil {
						if b, rerr := io.ReadAll(msg.Body); rerr == nil {
							bodyStr = string(b)
						}
					}
					state.mu.Lock()
					state.emails = append(state.emails, bodyStr)
					state.mu.Unlock()
					_ = tp.PrintfLine("250 OK")
				case u == "QUIT":
					_ = tp.PrintfLine("221 Bye")
					return
				default:
					_ = tp.PrintfLine("250 OK")
				}
			}
		}(conn)
	}
}

type browserClient struct {
	t       *testing.T
	baseURL string
	http    *http.Client
}

func newBrowserClient(t *testing.T, baseURL string) *browserClient {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &browserClient{
		t:       t,
		baseURL: baseURL,
		http: &http.Client{
			Jar: jar,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *browserClient) doJSON(method, path string, body interface{}) (int, []byte) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal %s: %v", path, err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, r)
	if err != nil {
		c.t.Fatalf("new request %s %s: %v", method, path, err)
	}
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (c *browserClient) doForm(path string, vals url.Values) *http.Response {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, strings.NewReader(vals.Encode()))
	if err != nil {
		c.t.Fatalf("new form %s: %v", path, err)
	}
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func (c *browserClient) loginLocal(username, password string) {
	c.t.Helper()
	resp := c.doForm("/login", url.Values{"username": {username}, "password": {password}})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		c.t.Fatalf("local login failed for %s: status=%d loc=%q", username, resp.StatusCode, resp.Header.Get("Location"))
	}
}

func (c *browserClient) mustGet(path string) (int, string, http.Header) {
	c.t.Helper()
	resp, err := c.http.Get(c.baseURL + path)
	if err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw), resp.Header
}

func TestRecette_FullReleaseAcceptance(t *testing.T) {
	env := newRecetteEnv(t)

	var (
		aliceID   int64
		parisID   int64
		lyonID    int64
		fpID      int64
		teamID    int64
		onsiteID  int64
		lockerIDs []int64
		lyonLkID  int64
	)

	admin := newBrowserClient(t, env.appSrv.URL)

	t.Run("01_Security_Health_PasswordResetSMTP", func(t *testing.T) {
		code, body, hdr := admin.mustGet("/health")
		if code != http.StatusOK || !strings.Contains(body, "ok") {
			t.Fatalf("unexpected /health: code=%d body=%s", code, body)
		}
		if hdr.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("missing security header X-Content-Type-Options: %+v", hdr)
		}

		// Metrics auth gate
		mCode, _, _ := admin.mustGet("/metrics")
		if mCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 on unauthenticated /metrics, got %d", mCode)
		}

		admin.loginLocal(env.cfg.AdminUser, env.cfg.AdminPassword)

		// Cross-site Origin rejection check
		evilReq, _ := http.NewRequest(http.MethodPost, env.appSrv.URL+"/api/presences", strings.NewReader(`{}`))
		evilReq.Header.Set("Origin", "https://attacker.example")
		evilResp, err := admin.http.Do(evilReq)
		if err != nil {
			t.Fatalf("evil origin req: %v", err)
		}
		_ = evilResp.Body.Close()
		if evilResp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 for cross-origin POST, got %d", evilResp.StatusCode)
		}

		// Create Alice via API, test SMTP Forgot/Reset Password flow
		st, raw := admin.doJSON(http.MethodPost, "/admin/users", map[string]string{
			"email": "alice@example.com", "name": "Alice Recette", "password": "TempPass#123",
		})
		if st != http.StatusOK {
			t.Fatalf("create alice: status=%d body=%s", st, string(raw))
		}
		var uRes struct{ ID int64 }
		_ = json.Unmarshal(raw, &uRes)
		aliceID = uRes.ID

		anon := newBrowserClient(t, env.appSrv.URL)
		fpResp := anon.doForm("/forgot-password", url.Values{"email": {"alice@example.com"}})
		_ = fpResp.Body.Close()
		if fpResp.StatusCode != http.StatusOK {
			t.Fatalf("forgot password POST: %d", fpResp.StatusCode)
		}

		var resetLink string
		deadline := time.Now().Add(5 * time.Second)
		re := regexp.MustCompile(`https?://[^\s"]+/reset-password\?token=[A-Za-z0-9_-]+`)
		for time.Now().Before(deadline) {
			env.state.mu.Lock()
			if len(env.state.emails) > 0 {
				resetLink = re.FindString(env.state.emails[len(env.state.emails)-1])
			}
			env.state.mu.Unlock()
			if resetLink != "" {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if resetLink == "" {
			t.Fatal("did not capture password reset email via SMTP")
		}

		parsedReset, _ := url.Parse(resetLink)
		tok := parsedReset.Query().Get("token")
		rstResp := anon.doForm("/reset-password", url.Values{
			"token":    {tok},
			"password": {"AliceNewPass#2026!"},
			"confirm":  {"AliceNewPass#2026!"},
		})
		rstBody, _ := io.ReadAll(rstResp.Body)
		_ = rstResp.Body.Close()
		if rstResp.StatusCode != http.StatusOK || !strings.Contains(string(rstBody), "bg-green-100") || strings.Contains(string(rstBody), `name="password"`) {
			t.Fatalf("reset password failed: code=%d body=%s", rstResp.StatusCode, string(rstBody))
		}

		anon.loginLocal("alice@example.com", "AliceNewPass#2026!")
	})

	t.Run("02_SAML_SSO_And_EntraGroupRoles", func(t *testing.T) {
		// 1. SAML Global Admin login via HTTP-Redirect -> IdP -> POST /saml/acs
		ssoBrowser := newBrowserClient(t, env.appSrv.URL)
		loginCode, _, hdr := ssoBrowser.mustGet("/saml/login")
		if loginCode != http.StatusFound {
			t.Fatalf("expected 302 from /saml/login, got %d", loginCode)
		}
		idpLoc := hdr.Get("Location")
		idpResp, err := http.Get(idpLoc) //nolint:gosec
		if err != nil {
			t.Fatalf("GET IdP SSO: %v", err)
		}
		htmlBytes, _ := io.ReadAll(idpResp.Body)
		_ = idpResp.Body.Close()

		samlVal := extractHiddenInput(t, string(htmlBytes), "SAMLResponse")
		relayVal := extractHiddenInput(t, string(htmlBytes), "RelayState")
		acsForm := url.Values{"SAMLResponse": {samlVal}}
		if relayVal != "" {
			acsForm.Set("RelayState", relayVal)
		}
		acsReq, _ := http.NewRequest(http.MethodPost, env.appSrv.URL+"/saml/acs", strings.NewReader(acsForm.Encode()))
		acsReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// Simulate real browser cross-site POST from IdP origin
		acsReq.Header.Set("Origin", env.mocks.URL)
		acsReq.Header.Set("Sec-Fetch-Site", "cross-site")
		acsResp, err := ssoBrowser.http.Do(acsReq)
		if err != nil {
			t.Fatalf("POST /saml/acs: %v", err)
		}
		_ = acsResp.Body.Close()
		if acsResp.StatusCode != http.StatusSeeOther || acsResp.Header.Get("Location") != "/" {
			t.Fatalf("SAML ACS failed: status=%d loc=%q", acsResp.StatusCode, acsResp.Header.Get("Location"))
		}
		if sc, _, _ := ssoBrowser.mustGet("/admin/users"); sc != http.StatusOK {
			t.Fatalf("expected SAML global user to open /admin/users, got %d", sc)
		}

		// 2. SAML Basic Collab user -> provisioned without admin role
		env.state.mu.Lock()
		env.state.persona = recettePersona{
			Email: "saml.collab@example.com", DisplayName: "SAML Collab", Groups: []string{"unrelated-group"},
		}
		env.state.mu.Unlock()

		collab := newBrowserClient(t, env.appSrv.URL)
		_, _, h2 := collab.mustGet("/saml/login")
		idpResp2, err := http.Get(h2.Get("Location")) //nolint:gosec
		if err != nil {
			t.Fatalf("GET IdP collab: %v", err)
		}
		b2, _ := io.ReadAll(idpResp2.Body)
		_ = idpResp2.Body.Close()
		f2 := url.Values{"SAMLResponse": {extractHiddenInput(t, string(b2), "SAMLResponse")}}
		r2, _ := http.NewRequest(http.MethodPost, env.appSrv.URL+"/saml/acs", strings.NewReader(f2.Encode()))
		r2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r2.Header.Set("Origin", env.mocks.URL)
		acs2, err := collab.http.Do(r2)
		if err != nil {
			t.Fatalf("collab ACS: %v", err)
		}
		_ = acs2.Body.Close()
		if sc, _, _ := collab.mustGet("/admin/users"); sc != http.StatusForbidden {
			t.Fatalf("expected 403 on /admin/users for non-admin SAML user, got %d", sc)
		}
	})

	t.Run("03_Sites_Floorplans_PNGUpload_Presences_O365Sync", func(t *testing.T) {
		st, raw := admin.doJSON(http.MethodPost, "/api/admin/sites", map[string]interface{}{
			"name": "Paris HQ", "country_code": "FR", "seats": 100, "workstations": 80,
		})
		if st != http.StatusOK {
			t.Fatalf("create Paris HQ: %d %s", st, string(raw))
		}
		var sRes struct{ ID int64 }
		_ = json.Unmarshal(raw, &sRes)
		parisID = sRes.ID

		st, raw = admin.doJSON(http.MethodPost, "/api/admin/sites", map[string]interface{}{
			"name": "Lyon Hub", "country_code": "FR", "seats": 40, "workstations": 35,
		})
		if st != http.StatusOK {
			t.Fatalf("create Lyon Hub: %d %s", st, string(raw))
		}
		_ = json.Unmarshal(raw, &sRes)
		lyonID = sRes.ID

		// Assign Alice to Paris HQ
		if st, r := admin.doJSON(http.MethodPut, fmt.Sprintf("/api/admin/users/%d/site", aliceID), map[string]int64{"site_id": parisID}); st != http.StatusOK {
			t.Fatalf("assign site: %d %s", st, string(r))
		}

		// Create Floorplan + Upload minimal PNG with valid CSRF token + Add Seat
		st, raw = admin.doJSON(http.MethodPost, "/api/admin/floorplans", map[string]interface{}{
			"name": "Paris F1", "site_id": parisID,
		})
		if st != http.StatusOK {
			t.Fatalf("create floorplan: %d %s", st, string(raw))
		}
		var fpRes struct{ ID int64 }
		_ = json.Unmarshal(raw, &fpRes)
		fpID = fpRes.ID

		_, adminFPHTML, _ := admin.mustGet(fmt.Sprintf("/admin/floorplans?fp=%d", fpID))
		csrfTok := extractCSRFFromHTML(adminFPHTML)
		if csrfTok == "" {
			t.Fatal("failed to locate csrf_token in /admin/floorplans")
		}

		var mbuf bytes.Buffer
		mw := multipart.NewWriter(&mbuf)
		_ = mw.WriteField("csrf_token", csrfTok)
		fw, _ := mw.CreateFormFile("image", "plan.png")
		// Valid 1x1 PNG
		_, _ = fw.Write([]byte{
			0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
			0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
			0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
			0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
			0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
			0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
		})
		_ = mw.Close()

		upReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/admin/floorplans/%d/image", env.appSrv.URL, fpID), &mbuf)
		upReq.Header.Set("Content-Type", mw.FormDataContentType())
		upReq.Header.Set("Origin", env.appSrv.URL)
		upReq.Header.Set("Sec-Fetch-Site", "same-origin")
		upResp, err := admin.http.Do(upReq)
		if err != nil || upResp.StatusCode != http.StatusOK {
			t.Fatalf("upload floorplan PNG failed: err=%v resp=%+v", err, upResp)
		}
		var upJSON struct {
			ImagePath string `json:"image_path"`
		}
		_ = json.NewDecoder(upResp.Body).Decode(&upJSON)
		_ = upResp.Body.Close()

		if code, _, imgHdr := admin.mustGet("/floorplan-img/" + upJSON.ImagePath); code != http.StatusOK || !strings.HasPrefix(imgHdr.Get("Content-Type"), "image/png") {
			t.Fatalf("uploaded floorplan image not served properly: code=%d ct=%q", code, imgHdr.Get("Content-Type"))
		}

		// Create seat + locker range
		st, raw = admin.doJSON(http.MethodPost, fmt.Sprintf("/api/admin/floorplans/%d/seats", fpID), map[string]interface{}{
			"label": "A-01", "x_pct": 25.5, "y_pct": 40.0,
		})
		if st != http.StatusOK {
			t.Fatalf("create seat: %d %s", st, string(raw))
		}
		var seatRes struct{ ID int64 }
		_ = json.Unmarshal(raw, &seatRes)

		st, _ = admin.doJSON(http.MethodPost, fmt.Sprintf("/api/admin/floorplans/%d/locker-ranges", fpID), map[string]interface{}{
			"label": "Zone A (01-55)", "zone": "Zone A", "x_pct": 60.0, "y_pct": 30.0,
		})
		if st != http.StatusOK {
			t.Fatalf("create locker range: %d", st)
		}

		// Alice sets on-site presence, reserves seat, connects O365, and syncs month
		statuses, _ := env.db.ListActiveStatuses()
		var o365SyncStatusID int64
		for _, s := range statuses {
			if s.OnSite && onsiteID == 0 {
				onsiteID = s.ID
			}
			if s.O365ShowAs() != "" && o365SyncStatusID == 0 {
				o365SyncStatusID = s.ID
			}
		}
		if onsiteID == 0 {
			t.Fatal("no seeded on-site status found")
		}
		if o365SyncStatusID == 0 {
			o365SyncStatusID = onsiteID
		}

		alice := newBrowserClient(t, env.appSrv.URL)
		alice.loginLocal("alice@example.com", "AliceNewPass#2026!")

		workDate := firstWeekdayOf(2026, 11)
		if st, r := alice.doJSON(http.MethodPost, "/api/presences", map[string]interface{}{
			"user_id": aliceID, "dates": []string{workDate}, "status_id": onsiteID, "half": "full",
		}); st != http.StatusOK {
			t.Fatalf("alice set presence: %d %s", st, string(r))
		}
		if o365SyncStatusID != onsiteID {
			if st, r := alice.doJSON(http.MethodPost, "/api/presences", map[string]interface{}{
				"user_id": aliceID, "dates": []string{secondWeekdayOf(2026, 11)}, "status_id": o365SyncStatusID, "half": "full",
			}); st != http.StatusOK {
				t.Fatalf("alice set syncable presence: %d %s", st, string(r))
			}
		}

		if st, r := alice.doJSON(http.MethodPost, "/api/reservations", map[string]interface{}{
			"seat_id": seatRes.ID, "date": workDate, "half": "full",
		}); st != http.StatusOK {
			t.Fatalf("alice reserve seat: %d %s", st, string(r))
		}

		// O365 OAuth connect loop
		cCode, _, cHdr := alice.mustGet("/auth/o365/connect?return_to=/")
		if cCode != http.StatusTemporaryRedirect {
			t.Fatalf("expected 307 from /auth/o365/connect, got %d", cCode)
		}
		msResp, err := newNoRedirectHTTPClient().Get(cHdr.Get("Location"))
		if err != nil {
			t.Fatalf("mock authorize: %v", err)
		}
		_ = msResp.Body.Close()
		cbURL := msResp.Header.Get("Location")
		cbResp, err := alice.http.Get(cbURL)
		if err != nil {
			t.Fatalf("o365 callback: %v", err)
		}
		_ = cbResp.Body.Close()
		if cbResp.StatusCode != http.StatusSeeOther || !strings.Contains(cbResp.Header.Get("Location"), "o365_connected=1") {
			t.Fatalf("unexpected O365 callback redirect: %d %q", cbResp.StatusCode, cbResp.Header.Get("Location"))
		}

		if st, r := alice.doJSON(http.MethodPost, "/api/o365/sync", map[string]int{"year": 2026, "month": 11}); st != http.StatusOK {
			t.Fatalf("o365 sync failed: %d %s", st, string(r))
		}
		env.state.mu.Lock()
		evtCount := len(env.state.o365Events)
		env.state.mu.Unlock()
		if evtCount == 0 {
			t.Fatal("expected mock Graph /v1.0/me/calendar/events to receive synced event")
		}
	})

	t.Run("04_Lockers_Pagination_SiteChangeAutoRelease_Notifications", func(t *testing.T) {
		// Bulk create 55 lockers on Paris HQ + 1 on Lyon Hub
		if st, r := admin.doJSON(http.MethodPost, "/api/admin/lockers/bulk", map[string]interface{}{
			"site_id": parisID, "prefix": "L-", "start_num": 1, "end_num": 55, "pad_width": 2, "zone": "Zone A",
		}); st != http.StatusOK {
			t.Fatalf("bulk lockers: %d %s", st, string(r))
		}
		if st, r := admin.doJSON(http.MethodPost, "/api/admin/lockers", map[string]interface{}{
			"site_id": lyonID, "number": "LY-01", "zone": "Zone Lyon", "status": "active",
		}); st != http.StatusOK {
			t.Fatalf("create lyon locker: %d %s", st, string(r))
		} else {
			var res struct{ ID int64 }
			_ = json.Unmarshal(r, &res)
			lyonLkID = res.ID
		}

		// Verify page=2 pagination envelope (55 total -> 5 items on page 2)
		code, p2Raw, _ := admin.mustGet(fmt.Sprintf("/api/admin/lockers?site_id=%d&page=2&limit=50", parisID))
		if code != http.StatusOK {
			t.Fatalf("page=2 failed: %d %s", code, p2Raw)
		}
		var pageEnv struct {
			Items []struct{ ID int64 } `json:"items"`
			Total int                  `json:"total"`
		}
		_ = json.Unmarshal([]byte(p2Raw), &pageEnv)
		if pageEnv.Total != 55 || len(pageEnv.Items) != 5 {
			t.Fatalf("unexpected pagination envelope: total=%d len=%d", pageEnv.Total, len(pageEnv.Items))
		}
		// Collect first 2 IDs from page 1
		_, p1Raw, _ := admin.mustGet(fmt.Sprintf("/api/admin/lockers?site_id=%d&page=1&limit=50", parisID))
		var p1Env struct {
			Items []struct{ ID int64 } `json:"items"`
		}
		_ = json.Unmarshal([]byte(p1Raw), &p1Env)
		lockerIDs = append(lockerIDs, p1Env.Items[0].ID, p1Env.Items[1].ID)

		alice := newBrowserClient(t, env.appSrv.URL)
		alice.loginLocal("alice@example.com", "AliceNewPass#2026!")

		// Wrong site claim rejected (403), claim L-01 OK, duplicate on Paris rejected (409), PIN preserved across auto-release
		if st, _ := alice.doJSON(http.MethodPost, fmt.Sprintf("/api/lockers/%d/claim", lyonLkID), map[string]string{"code": "1111"}); st != http.StatusForbidden {
			t.Fatalf("expected 403 claiming other site locker, got %d", st)
		}
		if st, r := alice.doJSON(http.MethodPost, fmt.Sprintf("/api/lockers/%d/claim", lockerIDs[0]), map[string]string{"code": "8421"}); st != http.StatusOK {
			t.Fatalf("claim L-01: %d %s", st, string(r))
		}
		if st, _ := alice.doJSON(http.MethodPost, fmt.Sprintf("/api/lockers/%d/claim", lockerIDs[1]), map[string]string{"code": "2222"}); st != http.StatusConflict {
			t.Fatalf("expected 409 second locker on same site, got %d", st)
		}

		// Move Alice from Paris HQ to Lyon Hub -> auto-release + PIN preserved + admin notification!
		if st, r := admin.doJSON(http.MethodPut, fmt.Sprintf("/api/admin/users/%d/site", aliceID), map[string]int64{"site_id": lyonID}); st != http.StatusOK {
			t.Fatalf("move user site: %d %s", st, string(r))
		}
		lk, err := env.db.GetLocker(lockerIDs[0])
		if err != nil || lk.UserID != 0 || lk.Code != "8421" {
			t.Fatalf("expected auto-released locker keeping code 8421, got %+v (err=%v)", lk, err)
		}
		_, nRaw, _ := admin.mustGet("/api/notifications/unread")
		if !strings.Contains(nRaw, "locker_auto_released") {
			t.Fatalf("expected locker_auto_released admin notification, got %s", nRaw)
		}
	})

	t.Run("05_Teams_ManualJiraActivities_Certify_PAT_Metrics", func(t *testing.T) {
		st, raw := admin.doJSON(http.MethodPost, "/admin/teams", map[string]interface{}{
			"name":                        "Engineering Recette",
			"jira_space_key":              "PRJ",
			"timesheets_managed_manually": true,
			"require_activity_comment":    false,
		})
		if st != http.StatusOK {
			t.Fatalf("create team: %d %s", st, string(raw))
		}
		var tRes struct{ ID int64 }
		_ = json.Unmarshal(raw, &tRes)
		teamID = tRes.ID

		if st, r := admin.doJSON(http.MethodPost, fmt.Sprintf("/admin/teams/%d/members", teamID), map[string]int64{"user_id": aliceID}); st != http.StatusOK {
			t.Fatalf("add alice to team: %d %s", st, string(r))
		}

		alice := newBrowserClient(t, env.appSrv.URL)
		alice.loginLocal("alice@example.com", "AliceNewPass#2026!")

		// Jira tickets endpoint calls mock Scoped Cloud gateway
		jCode, jBody, _ := alice.mustGet("/api/project-activities/jira-tickets")
		if jCode != http.StatusOK || !strings.Contains(jBody, "PRJ-101") {
			t.Fatalf("expected Jira tickets PRJ-101, got code=%d body=%s", jCode, jBody)
		}

		workDate := firstWeekdayOf(2026, 11)
		if st, r := alice.doJSON(http.MethodPost, "/api/project-activities", map[string]interface{}{
			"date":          workDate,
			"activity_type": "jira",
			"jira_key":      "PRJ-101",
			"jira_title":    "Migration PostgreSQL 16",
			"comment":       "Schema & indexes",
			"percentage":    100,
		}); st != http.StatusOK {
			t.Fatalf("create project activity: %d %s", st, string(r))
		}

		// Incomplete month certification rejected -> fill remaining weekdays -> certify succeeds -> locked!
		if st, _ := alice.doJSON(http.MethodPost, "/api/certify", map[string]int{"year": 2026, "month": 11}); st != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 certifying incomplete month, got %d", st)
		}
		allWeekdays := allWeekdaysOf(2026, 11)
		if st, r := alice.doJSON(http.MethodPost, "/api/presences", map[string]interface{}{
			"user_id": aliceID, "dates": allWeekdays, "status_id": onsiteID, "half": "full",
		}); st != http.StatusOK {
			t.Fatalf("fill presences: %d %s", st, string(r))
		}
		if st, r := alice.doJSON(http.MethodPost, "/api/certify", map[string]int{"year": 2026, "month": 11}); st != http.StatusOK {
			t.Fatalf("certify complete month: %d %s", st, string(r))
		}
		if st, _ := alice.doJSON(http.MethodPost, "/api/presences", map[string]interface{}{
			"user_id": aliceID, "dates": []string{workDate}, "status_id": onsiteID, "half": "full",
		}); st != http.StatusLocked {
			t.Fatalf("expected 423 Locked editing certified month, got %d", st)
		}

		// Admin views & logs pages render 200
		for _, page := range []string{
			fmt.Sprintf("/admin/teams/%d/logs", teamID),
			fmt.Sprintf("/admin/teams/members/%d/logs", aliceID),
			fmt.Sprintf("/admin/users/%d/logs", aliceID),
			"/admin/sites-report",
			"/admin/projects-report",
		} {
			if sc, _, _ := admin.mustGet(page); sc != http.StatusOK {
				t.Fatalf("GET %s returned %d, expected 200", page, sc)
			}
		}

		// PAT API check
		st, patRaw := admin.doJSON(http.MethodPost, "/api/tokens", map[string]interface{}{
			"description": "Recette Smoke Test", "expires_in": 7,
		})
		if st != http.StatusOK {
			t.Fatalf("create PAT: %d %s", st, string(patRaw))
		}
		var patRes struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(patRaw, &patRes)

		patReq, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/presences?team_id=%d&year=2026&month=11", env.appSrv.URL, teamID), nil)
		patReq.Header.Set("Authorization", "Bearer "+patRes.Token)
		patResp, err := http.DefaultClient.Do(patReq)
		if err != nil || patResp.StatusCode != http.StatusOK {
			t.Fatalf("PAT /api/presences failed: err=%v resp=%+v", err, patResp)
		}
		_ = patResp.Body.Close()

		// Authenticated /metrics check
		mReq, _ := http.NewRequest(http.MethodGet, env.appSrv.URL+"/metrics", nil)
		mReq.Header.Set("Authorization", "Bearer "+env.cfg.MetricsToken)
		mReq.Header.Set("Accept-Encoding", "identity")
		mResp, err := http.DefaultClient.Do(mReq)
		if err != nil || mResp.StatusCode != http.StatusOK {
			t.Fatalf("/metrics with token failed: err=%v resp=%+v", err, mResp)
		}
		mBody, _ := io.ReadAll(mResp.Body)
		_ = mResp.Body.Close()
		if !strings.Contains(string(mBody), "mypresence_") {
			t.Fatalf("expected mypresence_ metrics in output (got %d bytes, snippet=%q)", len(mBody), string(mBody[:min(len(mBody), 200)]))
		}
	})
}

func newNoRedirectHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func extractHiddenInput(t *testing.T, htmlStr, fieldName string) string {
	t.Helper()
	re := regexp.MustCompile(`name=["']` + regexp.QuoteMeta(fieldName) + `["'][^>]*value=["']([^"']*)["']`)
	if m := re.FindStringSubmatch(htmlStr); len(m) == 2 {
		return strings.ReplaceAll(m[1], "&#43;", "+")
	}
	reAlt := regexp.MustCompile(`value=["']([^"']*)["'][^>]*name=["']` + regexp.QuoteMeta(fieldName) + `["']`)
	if m := reAlt.FindStringSubmatch(htmlStr); len(m) == 2 {
		return strings.ReplaceAll(m[1], "&#43;", "+")
	}
	return ""
}

func extractCSRFFromHTML(htmlStr string) string {
	for _, pat := range []string{
		`name=["']csrf_token["'][^>]*value=["']([0-9a-f]+)["']`,
		`name=["']csrf-token["'][^>]*content=["']([0-9a-f]+)["']`,
		`csrfToken\s*[:=]\s*["']([0-9a-f]+)["']`,
	} {
		if m := regexp.MustCompile(pat).FindStringSubmatch(htmlStr); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

func firstWeekdayOf(year, month int) string {
	for d := 1; d <= 7; d++ {
		dt := time.Date(year, time.Month(month), d, 0, 0, 0, 0, time.UTC)
		if dt.Weekday() != time.Saturday && dt.Weekday() != time.Sunday {
			return dt.Format("2006-01-02")
		}
	}
	return fmt.Sprintf("%04d-%02d-01", year, month)
}

func secondWeekdayOf(year, month int) string {
	seen := 0
	for d := 1; d <= 10; d++ {
		dt := time.Date(year, time.Month(month), d, 0, 0, 0, 0, time.UTC)
		if dt.Weekday() != time.Saturday && dt.Weekday() != time.Sunday {
			seen++
			if seen == 2 {
				return dt.Format("2006-01-02")
			}
		}
	}
	return fmt.Sprintf("%04d-%02d-02", year, month)
}

func allWeekdaysOf(year, month int) []string {
	var out []string
	for d := 1; ; d++ {
		dt := time.Date(year, time.Month(month), d, 0, 0, 0, 0, time.UTC)
		if int(dt.Month()) != month {
			break
		}
		if dt.Weekday() != time.Saturday && dt.Weekday() != time.Sunday {
			out = append(out, dt.Format("2006-01-02"))
		}
	}
	_ = strconv.Itoa(year)
	return out
}
