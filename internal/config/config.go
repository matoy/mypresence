package config

import (
	"os"
	"strconv"
)

// Version is the application version, updated manually for each release.
const Version = "0.6.3"

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// Server
	Port      string
	DataDir   string
	SecretKey string

	// Database backend
	DBDriver   string // sqlite (default), postgres, mysql, sqlserver
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	DBSSLMode  string // postgres: disable|require|verify-full; mysql: true|false|skip-verify

	// Branding
	AppName        string
	PrimaryColor   string
	SecondaryColor string
	AccentColor    string
	LogoPath       string

	// Fonts
	FontURL        string
	FontFamily     string
	FontFamilyMono string

	// Footer
	HideFooter bool

	// Local admin auth
	AdminUser     string
	AdminPassword string

	// SAML
	SAMLEnabled        bool
	SAMLIDPMetadataURL string
	SAMLEntityID       string
	SAMLRootURL        string
	SAMLCertFile       string
	SAMLKeyFile        string
	// SAML group → role mapping (Entra ID group Object IDs)
	SAMLGroupsClaim           string // claim URI that carries group values (default: Entra standard)
	SAMLGroupGlobal           string // group ID → global (admin) role
	SAMLGroupTeamManager      string // group ID → team_manager role
	SAMLGroupTeamLeader       string // group ID → team_leader role
	SAMLGroupStatusManager    string // group ID → status_manager role
	SAMLGroupActivityViewer   string // group ID → activity_viewer role
	SAMLGroupFloorplanManager string // group ID → floorplan_manager role
	SAMLGroupProjectsManager  string // group ID → projects_admin role
	SAMLGroupProjectsViewer   string // group ID → projects_viewer role

	// Internationalisation
	DefaultLang string

	// Observability
	MetricsToken string

	// Features
	DisableFloorplans    bool
	DisableAPI           bool
	DisableProjects      bool
	OnsiteRatioThreshold float64 // minimum on-site % for the activity rocket (default 60)

	// SMTP (password reset)
	SMTPURL  string
	SMTPFrom string
	AppURL   string

	// Passkeys (WebAuthn)
	EnablePasskeys  bool
	PasskeyRPID     string // Relying Party ID (domain, e.g. "presence.example.com")
	PasskeyRPOrigin string // Full origin (e.g. "https://presence.example.com")

	// Jira integration (used for team Jira space linkage)
	JiraEnabled bool   // true when JiraBaseURL, JiraEmail and JiraToken are all set
	JiraBaseURL string // e.g. "https://your-domain.atlassian.net"
	JiraEmail   string // Atlassian account email used for API auth
	JiraToken   string // Atlassian API token
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	c := &Config{
		Port:      getEnv("PORT", "8080"),
		DataDir:   getEnv("DATA_DIR", "/data"),
		SecretKey: getEnv("SECRET_KEY", "change-me-in-production-use-random-32-chars"),

		DBDriver:   getEnv("DB_DRIVER", "sqlite"),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", ""),
		DBName:     getEnv("DB_NAME", "mypresence"),
		DBUser:     getEnv("DB_USER", ""),
		DBPassword: getEnv("DB_PASSWORD", ""),
		DBSSLMode:  getEnv("DB_SSL_MODE", "disable"),

		AppName:        getEnv("APP_NAME", "Presence"),
		PrimaryColor:   getEnv("PRIMARY_COLOR", "#3b82f6"),
		SecondaryColor: getEnv("SECONDARY_COLOR", "#1e40af"),
		AccentColor:    getEnv("ACCENT_COLOR", "#f59e0b"),
		LogoPath:       getEnv("LOGO_PATH", ""),

		FontURL:        getEnv("FONT_URL", "https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap"),
		FontFamily:     getEnv("FONT_FAMILY", "'Inter', ui-sans-serif, system-ui, sans-serif"),
		FontFamilyMono: getEnv("FONT_FAMILY_MONO", "'JetBrains Mono', ui-monospace, monospace"),

		HideFooter: getEnvBool("HIDE_FOOTER", false),

		AdminUser:     getEnv("ADMIN_USER", "admin"),
		AdminPassword: getEnv("ADMIN_PASSWORD", "admin"),

		SAMLIDPMetadataURL: getEnv("SAML_IDP_METADATA_URL", ""),
		SAMLEntityID:       getEnv("SAML_ENTITY_ID", ""),
		SAMLRootURL:        getEnv("SAML_ROOT_URL", ""),
		SAMLCertFile:       getEnv("SAML_SP_CERT_FILE", ""),
		SAMLKeyFile:        getEnv("SAML_SP_KEY_FILE", ""),

		SAMLGroupsClaim:           getEnv("SAML_GROUPS_CLAIM", "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups"),
		SAMLGroupGlobal:           getEnv("SAML_GROUP_GLOBAL", ""),
		SAMLGroupTeamManager:      getEnv("SAML_GROUP_TEAM_MANAGER", ""),
		SAMLGroupTeamLeader:       getEnv("SAML_GROUP_TEAM_LEADER", ""),
		SAMLGroupStatusManager:    getEnv("SAML_GROUP_STATUS_MANAGER", ""),
		SAMLGroupActivityViewer:   getEnv("SAML_GROUP_ACTIVITY_VIEWER", ""),
		SAMLGroupFloorplanManager: getEnv("SAML_GROUP_FLOORPLAN_MANAGER", ""),
		SAMLGroupProjectsManager:  getEnv("SAML_GROUP_PROJECTS_MANAGER", ""),
		SAMLGroupProjectsViewer:   getEnv("SAML_GROUP_PROJECTS_VIEWER", ""),

		DefaultLang: getEnv("DEFAULT_LANG", "en"),

		MetricsToken: getEnv("METRICS_TOKEN", ""),

		DisableFloorplans:    getEnvBool("DISABLE_FLOORPLANS", false),
		DisableAPI:           getEnvBool("DISABLE_API", false),
		DisableProjects:      getEnvBool("DISABLE_PROJECTS", false),
		OnsiteRatioThreshold: getEnvFloat("ONSITE_RATIO_THRESHOLD", 60.0),

		SMTPURL:  getEnv("SMTP_URL", ""),
		SMTPFrom: getEnv("SMTP_FROM", "noreply@presence.local"),
		AppURL:   getEnv("APP_URL", ""),

		EnablePasskeys:  getEnvBool("ENABLE_PASSKEYS", false),
		PasskeyRPID:     getEnv("PASSKEY_RP_ID", ""),
		PasskeyRPOrigin: getEnv("PASSKEY_RP_ORIGIN", ""),

		JiraBaseURL: getEnv("JIRA_BASE_URL", ""),
		JiraEmail:   getEnv("JIRA_EMAIL", ""),
		JiraToken:   getEnv("JIRA_TOKEN", ""),
	}
	c.SAMLEnabled = c.SAMLIDPMetadataURL != "" && c.SAMLEntityID != ""
	c.JiraEnabled = c.JiraBaseURL != "" && c.JiraEmail != "" && c.JiraToken != ""
	return c
}

// liveEditableEnvVars lists the environment variables that can be changed at
// runtime via the general settings page. Changes are kept in memory only
// (process env + the running Config) and are lost on the next restart.
// Structural variables (server port, data directory, secret key, database
// connection) are intentionally excluded since altering them without
// restarting the process would have no effect or would break active sessions.
var liveEditableEnvVars = map[string]bool{
	"APP_NAME": true, "PRIMARY_COLOR": true, "SECONDARY_COLOR": true, "ACCENT_COLOR": true, "LOGO_PATH": true,
	"FONT_URL": true, "FONT_FAMILY": true, "FONT_FAMILY_MONO": true,
	"HIDE_FOOTER": true, "DEFAULT_LANG": true, "METRICS_TOKEN": true,
	"DISABLE_FLOORPLANS": true, "DISABLE_API": true, "DISABLE_PROJECTS": true, "ONSITE_RATIO_THRESHOLD": true,
	"SMTP_URL": true, "SMTP_FROM": true, "APP_URL": true,
	"ADMIN_USER": true, "ADMIN_PASSWORD": true,
	"PASSKEY_RP_ID": true, "PASSKEY_RP_ORIGIN": true,
	"JIRA_BASE_URL": true, "JIRA_EMAIL": true, "JIRA_TOKEN": true,
}

// IsLiveEditable returns true if key is allowed to be changed at runtime via ApplyEnvOverride.
func IsLiveEditable(key string) bool {
	return liveEditableEnvVars[key]
}

// ApplyEnvOverride updates the in-memory field matching the given environment
// variable name (and calls os.Setenv so any code reading it later stays
// consistent). It only accepts keys listed in liveEditableEnvVars. Returns
// true if the key was recognized and applied.
func (c *Config) ApplyEnvOverride(key, value string) bool {
	if !IsLiveEditable(key) {
		return false
	}
	os.Setenv(key, value) //nolint:errcheck
	switch key {
	case "APP_NAME":
		c.AppName = value
	case "PRIMARY_COLOR":
		c.PrimaryColor = value
	case "SECONDARY_COLOR":
		c.SecondaryColor = value
	case "ACCENT_COLOR":
		c.AccentColor = value
	case "LOGO_PATH":
		c.LogoPath = value
	case "FONT_URL":
		c.FontURL = value
	case "FONT_FAMILY":
		c.FontFamily = value
	case "FONT_FAMILY_MONO":
		c.FontFamilyMono = value
	case "HIDE_FOOTER":
		c.HideFooter = parseBool(value, c.HideFooter)
	case "DEFAULT_LANG":
		c.DefaultLang = value
	case "METRICS_TOKEN":
		c.MetricsToken = value
	case "DISABLE_FLOORPLANS":
		c.DisableFloorplans = parseBool(value, c.DisableFloorplans)
	case "DISABLE_API":
		c.DisableAPI = parseBool(value, c.DisableAPI)
	case "DISABLE_PROJECTS":
		c.DisableProjects = parseBool(value, c.DisableProjects)
	case "ONSITE_RATIO_THRESHOLD":
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			c.OnsiteRatioThreshold = f
		}
	case "SMTP_URL":
		c.SMTPURL = value
	case "SMTP_FROM":
		c.SMTPFrom = value
	case "APP_URL":
		c.AppURL = value
	case "ADMIN_USER":
		c.AdminUser = value
	case "ADMIN_PASSWORD":
		c.AdminPassword = value
	case "PASSKEY_RP_ID":
		c.PasskeyRPID = value
	case "PASSKEY_RP_ORIGIN":
		c.PasskeyRPOrigin = value
	case "JIRA_BASE_URL":
		c.JiraBaseURL = value
	case "JIRA_EMAIL":
		c.JiraEmail = value
	case "JIRA_TOKEN":
		c.JiraToken = value
	}
	if key == "JIRA_BASE_URL" || key == "JIRA_EMAIL" || key == "JIRA_TOKEN" {
		c.JiraEnabled = c.JiraBaseURL != "" && c.JiraEmail != "" && c.JiraToken != ""
	}
	return true
}

// parseBool parses "true"/"1"/"yes" and "false"/"0"/"no" (case-sensitive, matching
// getEnvBool), falling back to the current value for anything else.
func parseBool(v string, fallback bool) bool {
	if v == "true" || v == "1" || v == "yes" {
		return true
	}
	if v == "false" || v == "0" || v == "no" {
		return false
	}
	return fallback
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "true" || v == "1" || v == "yes" {
		return true
	}
	if v == "false" || v == "0" || v == "no" {
		return false
	}
	return fallback
}
