package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersHTTPAndHTTPS(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := SecurityHeaders(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS should be empty on plain HTTP, got %q", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Forwarded-Proto", "https")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if got := w2.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("expected HSTS on https")
	}
}

func TestTrimSpace(t *testing.T) {
	if got := trimSpace("\t  abc  \t"); got != "abc" {
		t.Fatalf("trimSpace = %q", got)
	}
	if got := trimSpace("   "); got != "" {
		t.Fatalf("trimSpace all spaces = %q", got)
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.1")
	if got := clientIP(req); got != "203.0.113.10" {
		t.Fatalf("clientIP XFF = %q", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "10.0.0.2:5678"
	if got := clientIP(req2); got != "10.0.0.2" {
		t.Fatalf("clientIP remote addr = %q", got)
	}
}

func TestLoginRateLimiterFlow(t *testing.T) {
	l := &LoginRateLimiter{attempts: make(map[string]*loginAttempt)}
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "10.1.1.1:1234"

	if !l.Allow(req) {
		t.Fatal("fresh IP should be allowed")
	}
	for i := 0; i < loginMaxFailures; i++ {
		l.RecordFailure(req)
	}
	if l.Allow(req) {
		t.Fatal("expected blocked IP after max failures")
	}
	l.Reset(req)
	if !l.Allow(req) {
		t.Fatal("expected allowed after reset")
	}
}

func TestLimitRequestBody_PassThrough(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := LimitRequestBody(inner)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"key":"value"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called {
		t.Fatal("inner handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestLimitRequestBody_BodyIsWrapped(t *testing.T) {
	// Verify that LimitRequestBody replaces r.Body with a limited reader.
	// We confirm the body is still readable (not nil).
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			http.Error(w, "body is nil", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	h := LimitRequestBody(inner)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestParseTrustedProxies_And_IsHTTPS(t *testing.T) {
	noneChecker := ParseTrustedProxies("none")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	if IsHTTPS(req, noneChecker) {
		t.Fatal("expected IsHTTPS=false when trusted proxies is 'none'")
	}

	cidrChecker := ParseTrustedProxies("10.0.0.0/8, 127.0.0.1")
	if !IsHTTPS(req, cidrChecker) {
		t.Fatal("expected IsHTTPS=true for peer matching trusted CIDR")
	}

	untrustedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	untrustedReq.RemoteAddr = "198.51.100.4:5555"
	untrustedReq.Header.Set("X-Forwarded-Proto", "https")
	if IsHTTPS(untrustedReq, cidrChecker) {
		t.Fatal("expected IsHTTPS=false for untrusted remote peer")
	}

	if got := ClientIP(untrustedReq, cidrChecker); got != "198.51.100.4" {
		t.Fatalf("expected peer IP 198.51.100.4 for untrusted proxy, got %q", got)
	}
}

func TestValidateSameOrigin(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	guard := ValidateSameOrigin([]string{"app.example.com"}, ParseTrustedProxies("none"), "/saml/acs")(okHandler)

	// Cross-site POST blocked by Sec-Fetch-Site
	reqCross := httptest.NewRequest(http.MethodPost, "/api/lockers/1/claim", nil)
	reqCross.Host = "app.example.com"
	reqCross.Header.Set("Sec-Fetch-Site", "cross-site")
	wCross := httptest.NewRecorder()
	guard.ServeHTTP(wCross, reqCross)
	if wCross.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for Sec-Fetch-Site: cross-site, got %d", wCross.Code)
	}

	// Mismatched Origin blocked
	reqEvilOrig := httptest.NewRequest(http.MethodPost, "/api/lockers/1/claim", nil)
	reqEvilOrig.Host = "app.example.com"
	reqEvilOrig.Header.Set("Origin", "https://evil.example.org")
	wEvil := httptest.NewRecorder()
	guard.ServeHTTP(wEvil, reqEvilOrig)
	if wEvil.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched Origin, got %d", wEvil.Code)
	}

	// Same-origin POST allowed
	reqSame := httptest.NewRequest(http.MethodPost, "/api/lockers/1/claim", nil)
	reqSame.Host = "app.example.com"
	reqSame.Header.Set("Sec-Fetch-Site", "same-origin")
	reqSame.Header.Set("Origin", "https://app.example.com")
	wSame := httptest.NewRecorder()
	guard.ServeHTTP(wSame, reqSame)
	if wSame.Code != http.StatusOK {
		t.Fatalf("expected 200 for same-origin POST, got %d", wSame.Code)
	}

	// Exempt SAML ACS POST allowed even from external IdP Origin
	reqSAML := httptest.NewRequest(http.MethodPost, "/saml/acs", nil)
	reqSAML.Host = "app.example.com"
	reqSAML.Header.Set("Sec-Fetch-Site", "cross-site")
	reqSAML.Header.Set("Origin", "https://idp.example.org")
	wSAML := httptest.NewRecorder()
	guard.ServeHTTP(wSAML, reqSAML)
	if wSAML.Code != http.StatusOK {
		t.Fatalf("expected 200 for exempt /saml/acs POST, got %d", wSAML.Code)
	}

	// Bearer PAT request exempt
	reqPAT := httptest.NewRequest(http.MethodPost, "/api/lockers/1/claim", nil)
	reqPAT.Header.Set("Authorization", "Bearer pat_123")
	reqPAT.Header.Set("Origin", "https://cli.tool")
	wPAT := httptest.NewRecorder()
	guard.ServeHTTP(wPAT, reqPAT)
	if wPAT.Code != http.StatusOK {
		t.Fatalf("expected 200 for Bearer PAT request, got %d", wPAT.Code)
	}
}
