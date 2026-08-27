package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/handlers"
)

func TestBuildAppMux(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tempDir, "logo.png"), []byte("png-logo"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "floorplan_map.png"), []byte("png-map"), 0644)

	cfg := &config.Config{
		AppName:                    "Presence Full Test",
		DBDriver:                   "sqlite",
		DataDir:                    tempDir,
		Port:                       "8080",
		SecretKey:                  "01234567890123456789012345678901",
		MetricsToken:               "secret-metrics",
		EnablePasskeys:             true,
		PasskeyRPID:                "localhost",
		PasskeyRPOrigin:            "http://localhost:8080",
		SAMLEnabled:                false,
		DisableAPI:                 false,
		DisableFloorplans:          false,
		DisableProjects:            false,
		SMTPURL:                    "smtp://localhost:25",
		TeamCalendarRefreshMinutes: 5,
	}

	database, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetBcryptCost(4)
	defer database.Close()

	// 1. Build mux with all features enabled
	handler := buildAppMux(cfg, database)
	if handler == nil {
		t.Fatalf("buildAppMux returned nil handler")
	}

	// Test public GET /health
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on /health, got %d", rec.Code)
	}

	// Test public GET /login
	req = httptest.NewRequest(http.MethodGet, "/login", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on /login, got %d", rec.Code)
	}

	// Test GET /metrics with auth
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer secret-metrics")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on /metrics, got %d", rec.Code)
	}

	// 2. Build mux with features disabled
	cfgDisabled := &config.Config{
		AppName:           "Presence Minimal Test",
		DBDriver:          "sqlite",
		DataDir:           tempDir,
		Port:              "8080",
		SecretKey:         "01234567890123456789012345678901",
		EnablePasskeys:    false,
		SAMLEnabled:       false,
		DisableAPI:        true,
		DisableFloorplans: true,
		DisableProjects:   true,
		SMTPURL:           "",
	}
	handlerDisabled := buildAppMux(cfgDisabled, database)
	if handlerDisabled == nil {
		t.Fatalf("buildAppMux minimal returned nil handler")
	}

	// Test /health on minimal
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	rec = httptest.NewRecorder()
	handlerDisabled.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on /health minimal, got %d", rec.Code)
	}
}

func TestInitOptionalHandlers(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		DBDriver: "sqlite",
		DataDir:  tempDir,
	}
	database, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	renderMock := func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {}

	// Case 1: Both enabled
	cfg.DisableAPI = false
	cfg.DisableProjects = false
	patH, projH := initOptionalHandlers(cfg, database, renderMock)
	if patH == nil || projH == nil {
		t.Errorf("expected both handlers non-nil when enabled")
	}

	// Case 2: Both disabled
	cfg.DisableAPI = true
	cfg.DisableProjects = true
	patH2, projH2 := initOptionalHandlers(cfg, database, renderMock)
	if patH2 != nil || projH2 != nil {
		t.Errorf("expected both handlers nil when disabled")
	}
}

func TestRegisterMetricsCollectors(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		DBDriver: "sqlite",
		DataDir:  tempDir,
	}
	database, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	healthHandler := &handlers.HealthHandler{DB: database, StartedAt: time.Now().Add(-10 * time.Second)}
	registerMetricsCollectors(database, healthHandler)
}

func TestRegisterOptionalPublicRoutes(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		DBDriver:          "sqlite",
		DataDir:           tempDir,
		DisableFloorplans: false,
		DisableAPI:        false,
		SMTPURL:           "smtp://localhost:25",
	}

	renderMock := func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {}
	fpHandler := &handlers.FloorplanHandler{DataDir: tempDir, Render: renderMock}
	resetPwHandler := &handlers.ResetPasswordHandler{Config: cfg, Render: renderMock}

	mux := http.NewServeMux()
	registerOptionalPublicRoutes(mux, cfg, resetPwHandler, fpHandler)

	// Verify /api/docs responds
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on /api/docs, got %d", rec.Code)
	}

	// Verify disabled branches
	cfgDisabled := &config.Config{
		DisableFloorplans: true,
		DisableAPI:        true,
		SMTPURL:           "",
	}
	muxDisabled := http.NewServeMux()
	registerOptionalPublicRoutes(muxDisabled, cfgDisabled, resetPwHandler, fpHandler)
}

func TestRegisterOptionalAuthRoutes(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		DBDriver:          "sqlite",
		DataDir:           tempDir,
		DisableAPI:        false,
		DisableFloorplans: false,
	}
	database, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	renderMock := func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {}
	patH := &handlers.PATHandler{DB: database, Render: renderMock}
	fpH := &handlers.FloorplanHandler{DB: database, DataDir: tempDir, Render: renderMock}

	authMux := http.NewServeMux()
	registerOptionalAuthRoutes(authMux, cfg, patH, fpH)

	// Disabled branch
	cfgDisabled := &config.Config{
		DisableAPI:        true,
		DisableFloorplans: true,
	}
	authMuxDisabled := http.NewServeMux()
	registerOptionalAuthRoutes(authMuxDisabled, cfgDisabled, patH, fpH)
}

func TestRegisterOptionalAdminRoutes(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		DBDriver:          "sqlite",
		DataDir:           tempDir,
		DisableFloorplans: false,
		DisableProjects:   false,
		SecretKey:         "01234567890123456789012345678901",
	}
	database, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	renderMock := func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {}
	fpH := &handlers.FloorplanHandler{DB: database, DataDir: tempDir, Render: renderMock}
	projH := &handlers.ProjectsHandler{DB: database, Config: cfg, Render: renderMock}

	mux := http.NewServeMux()
	authMux := http.NewServeMux()
	registerOptionalAdminRoutes(mux, authMux, cfg, database, fpH, projH)

	// Disabled branch
	cfgDisabled := &config.Config{
		DisableFloorplans: true,
		DisableProjects:   true,
	}
	muxDisabled := http.NewServeMux()
	authMuxDisabled := http.NewServeMux()
	registerOptionalAdminRoutes(muxDisabled, authMuxDisabled, cfgDisabled, database, fpH, projH)
}

func TestLogStartupInfo(t *testing.T) {
	cfg := &config.Config{
		SAMLEnabled:  true,
		SAMLEntityID: "http://example.com/saml",
		MetricsToken: "my-token",
	}
	logStartupInfo(cfg, ":8080")

	cfgDisabled := &config.Config{
		SAMLEnabled:  false,
		MetricsToken: "",
	}
	logStartupInfo(cfgDisabled, ":8080")
}
