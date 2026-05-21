package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/handlers"
	"github.com/matoy/mypresence/internal/metrics"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

//go:embed web/templates/*.html
var templateFS embed.FS

//go:embed web/static
var staticFS embed.FS

//go:embed API.md
var apiDocContent []byte

func main() {
	// Structured JSON logging to stdout
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := config.Load()

	if cfg.SecretKey == "change-me-in-production-use-random-32-chars" {
		slog.Error("SECRET_KEY is set to its default value — set a strong random secret via the SECRET_KEY environment variable")
		os.Exit(1)
	}

	// Ensure data directory exists
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		slog.Error("failed to create data directory", "error", err)
		os.Exit(1)
	}

	// Open database
	database, err := db.Open(cfg)
	if err != nil {
		slog.Error("database error", "error", err)
		os.Exit(1)
	}
	defer database.Close() //nolint:errcheck

	// Seed defaults
	if err := database.SeedDefaults(cfg.AdminUser, cfg.AdminPassword); err != nil {
		slog.Error("seed error", "error", err)
		os.Exit(1)
	}

	// Clean expired sessions and reset tokens periodically
	database.CleanExpiredSessions()
	database.CleanExpiredResetTokens()

	// Parse templates and build the render helper.
	funcMap := buildTemplateFuncMap(cfg)
	templates := loadTemplates(funcMap)
	renderPage := newRenderPage(cfg, database, templates)

	// Initialize handlers
	healthHandler := &handlers.HealthHandler{DB: database, StartedAt: time.Now()}
	authHandler := &handlers.AuthHandler{
		DB:          database,
		Config:      cfg,
		Render:      renderPage,
		RateLimiter: middleware.NewLoginRateLimiter(),
	}
	calHandler := &handlers.CalendarHandler{DB: database, Render: renderPage, DisableFloorplans: cfg.DisableFloorplans}
	adminHandler := &handlers.AdminHandler{DB: database, Render: renderPage}
	activityHandler := &handlers.ActivityHandler{DB: database, Render: renderPage, DisableProjects: cfg.DisableProjects}
	holidaysHandler := &handlers.HolidaysHandler{DB: database, Render: renderPage}
	usersAdminHandler := &handlers.UsersAdminHandler{DB: database, Render: renderPage}
	floorplanHandler := &handlers.FloorplanHandler{DB: database, DataDir: cfg.DataDir, Render: renderPage}
	settingsHandler := &handlers.SettingsHandler{DB: database, Render: renderPage}
	generalSettingsHandler := &handlers.GeneralSettingsHandler{DataDir: cfg.DataDir, Render: renderPage}
	resetPasswordHandler := &handlers.ResetPasswordHandler{DB: database, Config: cfg, Render: renderPage, RateLimiter: middleware.NewLoginRateLimiter()}
	patHandler, projectsHandler := initOptionalHandlers(cfg, database, renderPage)
	newsHandler := &handlers.NewsHandler{DB: database, Render: renderPage}

	// Initialize SAML if configured
	if cfg.SAMLEnabled {
		if err := authHandler.InitSAML(); err != nil {
			slog.Warn("SAML initialization failed — SSO disabled", "error", err)
			cfg.SAMLEnabled = false
		}
	}

	registerMetricsCollectors(database, healthHandler)

	// Router
	mux := http.NewServeMux()

	// Static files (embedded)
	staticSub, _ := fs.Sub(staticFS, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))

	registerOptionalPublicRoutes(mux, cfg, resetPasswordHandler, floorplanHandler)

	// Serve logo and data files
	mux.Handle("GET /data/", dataFileHandler(cfg.DataDir))

	// Health check (public, no auth)
	mux.HandleFunc("GET /health", healthHandler.Health)

	// Metrics endpoint (token-protected)
	mux.Handle("GET /metrics", metricsHandler(cfg.MetricsToken))

	// Language switcher (public, sets a cookie and redirects back)
	mux.Handle("POST /set-lang", langSwitcherHandler(cfg.DefaultLang))

	// Auth routes (public)
	mux.Handle("GET /login", middleware.OptionalAuth(database, http.HandlerFunc(authHandler.LoginPage)))
	mux.HandleFunc("POST /login", authHandler.LocalLogin)
	mux.Handle("POST /logout", middleware.ValidateCSRF(cfg.SecretKey)(http.HandlerFunc(authHandler.Logout)))

	// SAML routes
	mux.HandleFunc("GET /saml/metadata", authHandler.SAMLMetadata)
	mux.HandleFunc("GET /saml/login", authHandler.SAMLLogin)
	mux.HandleFunc("POST /saml/acs", authHandler.SAMLACS)

	// Protected routes
	authMux := http.NewServeMux()

	// Calendar (main page)
	authMux.HandleFunc("GET /", calHandler.CalendarPage)
	authMux.HandleFunc("GET /{$}", calHandler.CalendarPage)

	// Presence API
	authMux.HandleFunc("POST /api/presences", calHandler.SetPresences)
	authMux.HandleFunc("POST /api/presences/clear", calHandler.ClearPresences)
	authMux.HandleFunc("GET /api/presences", calHandler.GetPresencesAPI)

	// Personal settings
	authMux.HandleFunc("GET /settings/my-logs", settingsHandler.MyLogsPage)
	authMux.HandleFunc("GET /settings/change-password", settingsHandler.ChangePasswordPage)
	authMux.Handle("POST /settings/change-password", middleware.ValidateCSRF(cfg.SecretKey)(http.HandlerFunc(settingsHandler.ChangePasswordPost)))

	// Impersonation (global admin only)
	authMux.HandleFunc("GET /impersonate", settingsHandler.ImpersonatePage)
	authMux.Handle("POST /impersonate", middleware.ValidateCSRF(cfg.SecretKey)(http.HandlerFunc(settingsHandler.ImpersonatePost)))
	authMux.Handle("POST /impersonate-exit", middleware.ValidateCSRF(cfg.SecretKey)(http.HandlerFunc(settingsHandler.ImpersonateExitPost)))

	// Active news (all authenticated users)
	authMux.HandleFunc("GET /api/news", newsHandler.GetActiveNewsAPI)

	registerOptionalAuthRoutes(authMux, cfg, patHandler, floorplanHandler)

	// Admin routes - each section guarded by its own role
	teamMux := http.NewServeMux()
	teamMux.HandleFunc("GET /admin/teams", adminHandler.TeamsPage)
	teamMux.HandleFunc("GET /api/teams", adminHandler.ListTeamsAPI)
	teamMux.HandleFunc("POST /admin/teams", adminHandler.CreateTeam)
	teamMux.HandleFunc("PUT /admin/teams/{id}", adminHandler.UpdateTeam)
	teamMux.HandleFunc("DELETE /admin/teams/{id}", adminHandler.DeleteTeam)
	teamMux.HandleFunc("POST /admin/teams/{id}/members", adminHandler.AddTeamMember)
	teamMux.HandleFunc("DELETE /admin/teams/{id}/members/{userId}", adminHandler.RemoveTeamMember)
	teamMux.HandleFunc("PATCH /admin/teams/{id}/members/{userId}/left-at", adminHandler.SetTeamMemberLeftAt)

	statusMux := http.NewServeMux()
	statusMux.HandleFunc("GET /admin/statuses", adminHandler.StatusesPage)
	statusMux.HandleFunc("POST /admin/statuses", adminHandler.CreateStatus)
	statusMux.HandleFunc("PUT /admin/statuses/{id}", adminHandler.UpdateStatus)
	statusMux.HandleFunc("PATCH /admin/statuses/{id}/disabled", adminHandler.ToggleStatusDisabled)
	statusMux.HandleFunc("DELETE /admin/statuses/{id}", adminHandler.DeleteStatus)

	activityMux := http.NewServeMux()
	activityMux.HandleFunc("GET /admin/activity", activityHandler.ActivityPage)
	activityMux.HandleFunc("GET /api/activity", activityHandler.ActivityAPI)

	holidaysMux := http.NewServeMux()
	holidaysMux.HandleFunc("GET /admin/holidays", holidaysHandler.HolidaysPage)
	holidaysMux.HandleFunc("POST /admin/holidays", holidaysHandler.CreateHoliday)
	holidaysMux.HandleFunc("PUT /admin/holidays/{id}", holidaysHandler.UpdateHoliday)
	holidaysMux.HandleFunc("DELETE /admin/holidays/{id}", holidaysHandler.DeleteHoliday)

	usersMux := http.NewServeMux()
	usersMux.HandleFunc("GET /admin/users", usersAdminHandler.UsersPage)
	usersMux.HandleFunc("POST /admin/users", usersAdminHandler.CreateUser)
	usersMux.HandleFunc("GET /admin/users/{id}/logs", usersAdminHandler.UserLogsPage)
	usersMux.HandleFunc("PUT /admin/users/{id}", usersAdminHandler.UpdateUser)
	usersMux.HandleFunc("PUT /admin/users/{id}/password", usersAdminHandler.SetPassword)
	usersMux.HandleFunc("PUT /admin/users/{id}/disabled", usersAdminHandler.SetDisabled)
	usersMux.HandleFunc("DELETE /admin/users/{id}", usersAdminHandler.DeleteUser)
	usersMux.HandleFunc("GET /api/users", adminHandler.UsersAPI)
	usersMux.HandleFunc("PUT /api/users/{id}/roles", adminHandler.UpdateUserRoles)
	usersMux.HandleFunc("GET /admin/roles", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/users", http.StatusMovedPermanently)
	})

	// Wire role-based middleware
	mux.Handle("/admin/teams", middleware.Auth(database, middleware.RequireRole(models.RoleTeamManager, models.RoleTeamLeader)(teamMux)))
	mux.Handle("/admin/teams/", middleware.Auth(database, middleware.RequireRole(models.RoleTeamManager, models.RoleTeamLeader)(teamMux)))
	mux.Handle("/api/teams", middleware.Auth(database, middleware.RequireRole(models.RoleTeamManager, models.RoleTeamLeader)(teamMux)))
	mux.Handle("/api/teams/", middleware.Auth(database, middleware.RequireRole(models.RoleTeamManager, models.RoleTeamLeader)(teamMux)))
	mux.Handle("/admin/statuses", middleware.Auth(database, middleware.RequireRole(models.RoleStatusManager)(statusMux)))
	mux.Handle("/admin/statuses/", middleware.Auth(database, middleware.RequireRole(models.RoleStatusManager)(statusMux)))
	mux.Handle("/admin/activity", middleware.Auth(database, middleware.RequireRole(models.RoleActivityViewer, models.RoleTeamLeader)(activityMux)))
	mux.Handle("/api/activity", middleware.Auth(database, middleware.RequireRole(models.RoleActivityViewer, models.RoleTeamLeader)(activityMux)))
	mux.Handle("/admin/holidays", middleware.Auth(database, middleware.RequireRole(models.RoleGlobal)(holidaysMux)))
	mux.Handle("/admin/holidays/", middleware.Auth(database, middleware.RequireRole(models.RoleGlobal)(holidaysMux)))
	mux.Handle("/admin/roles", middleware.Auth(database, middleware.RequireRole(models.RoleGlobal)(usersMux)))
	mux.Handle("/api/users", middleware.Auth(database, middleware.RequireRole(models.RoleGlobal)(usersMux)))
	mux.Handle("/api/users/", middleware.Auth(database, middleware.RequireRole(models.RoleGlobal)(usersMux)))
	mux.Handle("/admin/users", middleware.Auth(database, middleware.RequireRole(models.RoleGlobal)(usersMux)))
	mux.Handle("/admin/users/", middleware.Auth(database, middleware.RequireRole(models.RoleGlobal)(usersMux)))

	generalSettingsMux := http.NewServeMux()
	generalSettingsMux.HandleFunc("GET /admin/settings", generalSettingsHandler.GeneralSettingsPage)
	generalSettingsMux.Handle("POST /admin/settings/logo", middleware.ValidateCSRF(cfg.SecretKey)(http.HandlerFunc(generalSettingsHandler.UploadLogo)))
	generalSettingsMux.HandleFunc("DELETE /admin/settings/logo", generalSettingsHandler.DeleteLogo)
	mux.Handle("/admin/settings", middleware.Auth(database, middleware.RequireRole(models.RoleGlobal)(generalSettingsMux)))
	mux.Handle("/admin/settings/", middleware.Auth(database, middleware.RequireRole(models.RoleGlobal)(generalSettingsMux)))

	newsMux := http.NewServeMux()
	newsMux.HandleFunc("GET /admin/news", newsHandler.NewsPage)
	newsMux.HandleFunc("POST /admin/news", newsHandler.CreateNews)
	newsMux.HandleFunc("PUT /admin/news/{id}", newsHandler.UpdateNews)
	newsMux.HandleFunc("DELETE /admin/news/{id}", newsHandler.DeleteNews)
	newsMux.HandleFunc("GET /api/admin/news", newsHandler.ListNewsAPI)
	newsMux.HandleFunc("POST /api/admin/news", newsHandler.CreateNews)
	newsMux.HandleFunc("PUT /api/admin/news/{id}", newsHandler.UpdateNews)
	newsMux.HandleFunc("DELETE /api/admin/news/{id}", newsHandler.DeleteNews)
	mux.Handle("/admin/news", middleware.Auth(database, middleware.RequireRole(models.RoleActivityViewer)(newsMux)))
	mux.Handle("/admin/news/", middleware.Auth(database, middleware.RequireRole(models.RoleActivityViewer)(newsMux)))
	mux.Handle("/api/admin/news", middleware.Auth(database, middleware.RequireRole(models.RoleActivityViewer)(newsMux)))
	mux.Handle("/api/admin/news/", middleware.Auth(database, middleware.RequireRole(models.RoleActivityViewer)(newsMux)))

	registerOptionalAdminRoutes(mux, authMux, cfg, database, floorplanHandler, projectsHandler)

	mux.Handle("/", middleware.AuthWithOptions(database, !cfg.DisableAPI, authMux))

	// Start server
	addr := ":" + cfg.Port
	slog.Info("server started", "app", cfg.AppName, "addr", "http://localhost"+addr, "admin", cfg.AdminUser)
	logStartupInfo(cfg, addr)
	srv := &http.Server{
		Addr:              addr,
		Handler:           middleware.SecurityHeaders(middleware.LimitRequestBody(metrics.Instrument(middleware.AccessLog(mux)))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// initOptionalHandlers creates handlers for API tokens and Projects if those features are enabled.
func initOptionalHandlers(cfg *config.Config, database *db.DB, renderPage func(http.ResponseWriter, *http.Request, string, interface{})) (*handlers.PATHandler, *handlers.ProjectsHandler) {
	var patHandler *handlers.PATHandler
	if !cfg.DisableAPI {
		patHandler = &handlers.PATHandler{DB: database, Render: renderPage}
	}
	var projectsHandler *handlers.ProjectsHandler
	if !cfg.DisableProjects {
		projectsHandler = &handlers.ProjectsHandler{DB: database, Render: renderPage}
	}
	return patHandler, projectsHandler
}

// registerMetricsCollectors registers the Prometheus DB and health gauge collectors.
func registerMetricsCollectors(database *db.DB, healthHandler *handlers.HealthHandler) {
	metrics.RegisterDBCollector(func() metrics.DBStats {
		c := database.Counts()
		return metrics.DBStats{
			Users:          float64(c.Users),
			ActiveSessions: float64(c.ActiveSessions),
			Teams:          float64(c.Teams),
			Statuses:       float64(c.Statuses),
			Presences:      float64(c.Presences),
			Floorplans:     float64(c.Floorplans),
			Seats:          float64(c.Seats),
			Projects:       float64(c.Projects),
			ProjectEntries: float64(c.ProjectEntries),
		}
	})
	metrics.RegisterHealthCollector(func() metrics.HealthStats {
		dbUp := 1.0
		if err := database.Ping(); err != nil {
			dbUp = 0
		}
		up := dbUp // currently the only check is DB
		return metrics.HealthStats{
			Up:            up,
			UptimeSeconds: time.Since(healthHandler.StartedAt).Seconds(),
			DBUp:          dbUp,
		}
	})
}

// registerOptionalPublicRoutes registers public HTTP routes for optional features:
// floorplan images, API documentation, and password reset.
func registerOptionalPublicRoutes(mux *http.ServeMux, cfg *config.Config, resetPasswordHandler *handlers.ResetPasswordHandler, _ *handlers.FloorplanHandler) {
	if !cfg.DisableFloorplans {
		mux.Handle("GET /floorplan-img/", floorplanImgHandler(cfg.DataDir))
	}
	if !cfg.DisableAPI {
		mux.HandleFunc("GET /api/docs", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Write(apiDocContent) //nolint:errcheck
		})
	}
	if cfg.SMTPURL != "" {
		mux.HandleFunc("GET /forgot-password", resetPasswordHandler.ForgotPasswordPage)
		mux.HandleFunc("POST /forgot-password", resetPasswordHandler.ForgotPasswordPost)
		mux.HandleFunc("GET /reset-password", resetPasswordHandler.ResetPasswordPage)
		mux.HandleFunc("POST /reset-password", resetPasswordHandler.ResetPasswordPost)
	}
}

// registerOptionalAuthRoutes adds authenticated routes for optional features:
// API token management and floorplan user pages.
func registerOptionalAuthRoutes(authMux *http.ServeMux, cfg *config.Config, patHandler *handlers.PATHandler, floorplanHandler *handlers.FloorplanHandler) {
	if !cfg.DisableAPI {
		authMux.HandleFunc("GET /settings/tokens", patHandler.PATPage)
		authMux.HandleFunc("GET /api/tokens", patHandler.ListPATs)
		authMux.HandleFunc("POST /api/tokens", patHandler.CreatePAT)
		authMux.HandleFunc("DELETE /api/tokens/{id}", patHandler.RevokePAT)
		authMux.HandleFunc("DELETE /api/admin/tokens/{id}", patHandler.AdminRevokePAT)
	}
	if !cfg.DisableFloorplans {
		authMux.HandleFunc("GET /floorplan", floorplanHandler.FloorplanPage)
		authMux.HandleFunc("GET /api/seats", floorplanHandler.SeatsAPI)
		authMux.HandleFunc("GET /api/floorplans", floorplanHandler.ListFloorplansAPI)
		authMux.HandleFunc("GET /api/floorplans/{id}/seats/status", floorplanHandler.ListSeatsWithStatusForDatesAPI)
		authMux.HandleFunc("GET /api/floorplans/{id}/seats", floorplanHandler.ListSeatsForFloorplanAPI)
		authMux.HandleFunc("POST /api/reservations", floorplanHandler.ReserveSeat)
		authMux.HandleFunc("POST /api/reservations/bulk", floorplanHandler.BulkReserveSeats)
		authMux.HandleFunc("DELETE /api/reservations/bulk", floorplanHandler.CancelReservationsByDates)
		authMux.HandleFunc("DELETE /api/reservations/{id}", floorplanHandler.CancelReservation)
	}
}

// registerOptionalAdminRoutes adds role-gated admin routes for optional features:
// floorplan administration and the projects feature.
func registerOptionalAdminRoutes(mux, authMux *http.ServeMux, cfg *config.Config, database *db.DB, floorplanHandler *handlers.FloorplanHandler, projectsHandler *handlers.ProjectsHandler) {
	if !cfg.DisableFloorplans {
		fpAdminMux := http.NewServeMux()
		fpAdminMux.HandleFunc("GET /admin/floorplans", floorplanHandler.AdminFloorplansPage)
		fpAdminMux.HandleFunc("POST /admin/floorplans", floorplanHandler.CreateFloorplan)
		fpAdminMux.HandleFunc("PUT /admin/floorplans/{id}", floorplanHandler.UpdateFloorplan)
		fpAdminMux.HandleFunc("DELETE /admin/floorplans/{id}", floorplanHandler.DeleteFloorplan)
		fpAdminMux.HandleFunc("POST /admin/floorplans/{id}/image", floorplanHandler.UploadFloorplanImage)
		fpAdminMux.HandleFunc("POST /admin/floorplans/{id}/seats", floorplanHandler.CreateSeat)
		fpAdminMux.HandleFunc("PUT /admin/seats/{id}", floorplanHandler.UpdateSeat)
		fpAdminMux.HandleFunc("DELETE /admin/seats/{id}", floorplanHandler.DeleteSeat)
		fpAdminMux.HandleFunc("GET /api/admin/seats", floorplanHandler.AdminListSeats)
		mux.Handle("/admin/floorplans", middleware.Auth(database, middleware.RequireRole(models.RoleFloorplanManager)(fpAdminMux)))
		mux.Handle("/admin/floorplans/", middleware.Auth(database, middleware.RequireRole(models.RoleFloorplanManager)(fpAdminMux)))
		mux.Handle("/admin/seats/", middleware.Auth(database, middleware.RequireRole(models.RoleFloorplanManager)(fpAdminMux)))
		mux.Handle("/api/admin/", middleware.Auth(database, middleware.RequireRole(models.RoleFloorplanManager)(fpAdminMux)))
	}
	if !cfg.DisableProjects {
		authMux.HandleFunc("GET /projects", projectsHandler.ProjectsPage)
		authMux.HandleFunc("GET /api/projects", projectsHandler.ProjectsAPI)
		authMux.HandleFunc("GET /api/project-time", projectsHandler.ProjectTimeAPI)
		authMux.HandleFunc("POST /api/project-time", projectsHandler.SetProjectTime)
		projAdminMux := http.NewServeMux()
		projAdminMux.HandleFunc("GET /admin/projects", projectsHandler.AdminProjectsPage)
		projAdminMux.HandleFunc("POST /admin/projects", projectsHandler.CreateProject)
		projAdminMux.HandleFunc("PUT /admin/projects/{id}", projectsHandler.UpdateProject)
		projAdminMux.HandleFunc("GET /api/admin/projects", projectsHandler.AdminProjectsAPI)
		projAdminMux.HandleFunc("POST /api/admin/projects", projectsHandler.CreateProject)
		projAdminMux.HandleFunc("PUT /api/admin/projects/{id}", projectsHandler.UpdateProject)
		mux.Handle("/admin/projects", middleware.Auth(database, middleware.RequireRole(models.RoleProjectsAdmin)(projAdminMux)))
		mux.Handle("/admin/projects/", middleware.Auth(database, middleware.RequireRole(models.RoleProjectsAdmin)(projAdminMux)))
		mux.Handle("/api/admin/projects", middleware.Auth(database, middleware.RequireRole(models.RoleProjectsAdmin)(projAdminMux)))
		mux.Handle("/api/admin/projects/", middleware.Auth(database, middleware.RequireRole(models.RoleProjectsAdmin)(projAdminMux)))
		projReportMux := http.NewServeMux()
		projReportMux.HandleFunc("GET /admin/projects-report", projectsHandler.ProjectsReportPage)
		projReportMux.HandleFunc("GET /api/projects-report", projectsHandler.ProjectsReportAPI)
		mux.Handle("/admin/projects-report", middleware.Auth(database, middleware.RequireRole(models.RoleProjectsAdmin, models.RoleProjectsViewer, models.RoleTeamLeader)(projReportMux)))
		mux.Handle("/api/projects-report", middleware.Auth(database, middleware.RequireRole(models.RoleProjectsAdmin, models.RoleProjectsViewer, models.RoleTeamLeader)(projReportMux)))
	}
}

// logStartupInfo logs informational messages about enabled optional features.
func logStartupInfo(cfg *config.Config, addr string) {
	if cfg.SAMLEnabled {
		slog.Info("SAML SSO enabled", "entity_id", cfg.SAMLEntityID)
	}
	if cfg.MetricsToken != "" {
		slog.Info("Prometheus metrics enabled", "path", "http://localhost"+addr+"/metrics")
	}
}
