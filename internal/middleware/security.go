package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TrustedProxyChecker determines whether reverse-proxy forwarding headers
// (X-Forwarded-Proto, X-Forwarded-For, X-Forwarded-Host) should be trusted
// based on the direct peer's remote IP.
type TrustedProxyChecker struct {
	configured bool
	allowAll   bool
	ips        []net.IP
	nets       []*net.IPNet
}

// ParseTrustedProxies builds a TrustedProxyChecker from a comma-separated list of IPs/CIDRs,
// "*" (trust all), "none" (trust none), or "" (legacy permissive default when unconfigured).
func ParseTrustedProxies(spec string) *TrustedProxyChecker {
	s := strings.TrimSpace(spec)
	if s == "" {
		return &TrustedProxyChecker{configured: false, allowAll: true}
	}
	if strings.EqualFold(s, "none") {
		return &TrustedProxyChecker{configured: true, allowAll: false}
	}
	if s == "*" {
		return &TrustedProxyChecker{configured: true, allowAll: true}
	}
	c := &TrustedProxyChecker{configured: true}
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if strings.Contains(p, "/") {
			if _, ipNet, err := net.ParseCIDR(p); err == nil {
				c.nets = append(c.nets, ipNet)
			}
		} else if ip := net.ParseIP(p); ip != nil {
			c.ips = append(c.ips, ip)
		}
	}
	return c
}

// TrustForwardedHeaders reports whether proxy headers from r.RemoteAddr may be trusted.
func (c *TrustedProxyChecker) TrustForwardedHeaders(r *http.Request) bool {
	if c == nil || !c.configured || c.allowAll {
		return true
	}
	if r == nil {
		return false
	}
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	for _, allowed := range c.ips {
		if allowed.Equal(ip) {
			return true
		}
	}
	for _, n := range c.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// IsHTTPS reports whether the incoming request arrived over HTTPS, optionally checking
// X-Forwarded-Proto only when the peer matches TrustedProxyChecker.
func IsHTTPS(r *http.Request, trusted ...*TrustedProxyChecker) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	var checker *TrustedProxyChecker
	if len(trusted) > 0 {
		checker = trusted[0]
	}
	if !checker.TrustForwardedHeaders(r) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

// LimitRequestBody caps request bodies at 10 MB to prevent resource exhaustion.
// This matches the maximum file upload size already enforced by the floorplan handler.
func LimitRequestBody(next http.Handler) http.Handler {
	const maxBytes = 10 << 20 // 10 MB
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders adds defensive HTTP response headers to every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return SecurityHeadersWithTrustedProxies("", next)
}

// SecurityHeadersWithTrustedProxies adds defensive HTTP response headers, validating
// X-Forwarded-Proto against trustedProxiesSpec when set.
func SecurityHeadersWithTrustedProxies(trustedProxiesSpec string, next http.Handler) http.Handler {
	checker := ParseTrustedProxies(trustedProxiesSpec)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if IsHTTPS(r, checker) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src 'self' https://fonts.gstatic.com; "+
				"img-src 'self' data:; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none';",
		)
		next.ServeHTTP(w, r)
	})
}

// loginAttempt tracks failed login attempts for a single IP.
type loginAttempt struct {
	count     int
	firstFail time.Time
	blockedAt time.Time
}

const (
	loginMaxFailures   = 5                // attempts before block
	loginWindow        = 15 * time.Minute // rolling window
	loginBlockDuration = 15 * time.Minute // block duration after exceeding limit
)

// LoginRateLimiter is a per-IP failed-login rate limiter.
// It blocks an IP for loginBlockDuration after loginMaxFailures failures
// within loginWindow.
type LoginRateLimiter struct {
	mu               sync.Mutex
	attempts         map[string]*loginAttempt
	stopCh           chan struct{}
	stopOnce         sync.Once
	testTickInterval time.Duration // non-zero overrides 10-min ticker (tests only)
}

// NewLoginRateLimiter creates a ready-to-use limiter and starts background cleanup.
func NewLoginRateLimiter() *LoginRateLimiter {
	l := &LoginRateLimiter{
		attempts: make(map[string]*loginAttempt),
		stopCh:   make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// Close stops the background cleanup goroutine.
func (l *LoginRateLimiter) Close() {
	if l == nil {
		return
	}
	l.stopOnce.Do(func() {
		close(l.stopCh)
	})
}

// Allow returns true if the request is allowed (not rate-limited).
// Call RecordFailure on login failure and Reset on success.
func (l *LoginRateLimiter) Allow(r *http.Request) bool {
	ip := clientIP(r)
	l.mu.Lock()
	defer l.mu.Unlock()

	a, ok := l.attempts[ip]
	if !ok {
		return true
	}

	// Still in block period?
	if !a.blockedAt.IsZero() && time.Since(a.blockedAt) < loginBlockDuration {
		return false
	}

	// Window expired — reset
	if time.Since(a.firstFail) > loginWindow {
		delete(l.attempts, ip)
		return true
	}

	return a.count < loginMaxFailures
}

// RecordFailure increments the failure counter for the request's IP.
func (l *LoginRateLimiter) RecordFailure(r *http.Request) {
	ip := clientIP(r)
	l.mu.Lock()
	defer l.mu.Unlock()

	a, ok := l.attempts[ip]
	if !ok {
		l.attempts[ip] = &loginAttempt{count: 1, firstFail: time.Now()}
		return
	}

	// Reset window if expired
	if time.Since(a.firstFail) > loginWindow {
		a.count = 1
		a.firstFail = time.Now()
		a.blockedAt = time.Time{}
		return
	}

	a.count++
	if a.count >= loginMaxFailures {
		a.blockedAt = time.Now()
	}
}

// Reset clears the failure record for the request's IP on successful login.
func (l *LoginRateLimiter) Reset(r *http.Request) {
	ip := clientIP(r)
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

// cleanupLoop removes stale entries every 10 minutes.
func (l *LoginRateLimiter) cleanupLoop() {
	interval := 10 * time.Minute
	if l.testTickInterval > 0 {
		interval = l.testTickInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			for ip, a := range l.attempts {
				expired := time.Since(a.firstFail) > loginWindow
				blockExpired := a.blockedAt.IsZero() || time.Since(a.blockedAt) > loginBlockDuration
				if expired && blockExpired {
					delete(l.attempts, ip)
				}
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

// ClientIP extracts the real client IP, honouring X-Forwarded-For only when trusted.
func ClientIP(r *http.Request, trusted ...*TrustedProxyChecker) string {
	var checker *TrustedProxyChecker
	if len(trusted) > 0 {
		checker = trusted[0]
	}
	if checker.TrustForwardedHeaders(r) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if idx := strings.IndexByte(xff, ','); idx >= 0 {
				xff = xff[:idx]
			}
			if ip := net.ParseIP(trimSpace(xff)); ip != nil {
				return ip.String()
			}
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// clientIP extracts the real client IP, checking X-Forwarded-For first.
func clientIP(r *http.Request) string {
	return ClientIP(r)
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
