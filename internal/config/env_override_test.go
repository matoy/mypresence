package config

import (
	"os"
	"testing"
)

func TestLoad_JiraDefaults(t *testing.T) {
	for _, k := range []string{"JIRA_BASE_URL", "JIRA_EMAIL", "JIRA_TOKEN"} {
		os.Unsetenv(k) //nolint:errcheck
	}
	c := Load()
	if c.JiraBaseURL != "" || c.JiraEmail != "" || c.JiraToken != "" {
		t.Errorf("Jira fields should default to empty, got %q %q %q", c.JiraBaseURL, c.JiraEmail, c.JiraToken)
	}
	if c.JiraEnabled {
		t.Error("JiraEnabled should be false when no Jira env vars are set")
	}
}

func TestLoad_JiraEnvOverrides(t *testing.T) {
	t.Setenv("JIRA_BASE_URL", "https://acme.atlassian.net")
	t.Setenv("JIRA_EMAIL", "bot@acme.com")
	t.Setenv("JIRA_TOKEN", "tok-abc")

	c := Load()
	if c.JiraBaseURL != "https://acme.atlassian.net" {
		t.Errorf("JiraBaseURL: got %q", c.JiraBaseURL)
	}
	if c.JiraEmail != "bot@acme.com" {
		t.Errorf("JiraEmail: got %q", c.JiraEmail)
	}
	if c.JiraToken != "tok-abc" {
		t.Errorf("JiraToken: got %q", c.JiraToken)
	}
	if !c.JiraEnabled {
		t.Error("JiraEnabled should be true when all 3 Jira env vars are set")
	}
}

func TestLoad_JiraEnabled_PartialEnvVarsDisabled(t *testing.T) {
	tests := []struct {
		name                  string
		baseURL, email, token string
	}{
		{"only base URL", "https://acme.atlassian.net", "", ""},
		{"only email", "", "bot@acme.com", ""},
		{"only token", "", "", "tok-abc"},
		{"missing token", "https://acme.atlassian.net", "bot@acme.com", ""},
		{"missing email", "https://acme.atlassian.net", "", "tok-abc"},
		{"missing base URL", "", "bot@acme.com", "tok-abc"},
		{"none set", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.baseURL == "" {
				os.Unsetenv("JIRA_BASE_URL") //nolint:errcheck
			} else {
				t.Setenv("JIRA_BASE_URL", tt.baseURL)
			}
			if tt.email == "" {
				os.Unsetenv("JIRA_EMAIL") //nolint:errcheck
			} else {
				t.Setenv("JIRA_EMAIL", tt.email)
			}
			if tt.token == "" {
				os.Unsetenv("JIRA_TOKEN") //nolint:errcheck
			} else {
				t.Setenv("JIRA_TOKEN", tt.token)
			}
			c := Load()
			if c.JiraEnabled {
				t.Error("JiraEnabled should be false unless all 3 Jira env vars are set")
			}
		})
	}
}

func TestApplyEnvOverride_JiraEnabled_Recomputed(t *testing.T) {
	os.Unsetenv("JIRA_BASE_URL") //nolint:errcheck
	os.Unsetenv("JIRA_EMAIL")    //nolint:errcheck
	os.Unsetenv("JIRA_TOKEN")    //nolint:errcheck
	c := Load()
	if c.JiraEnabled {
		t.Fatal("JiraEnabled should start false")
	}

	c.ApplyEnvOverride("JIRA_BASE_URL", "https://acme.atlassian.net")
	if c.JiraEnabled {
		t.Error("JiraEnabled should still be false with only JIRA_BASE_URL set")
	}
	c.ApplyEnvOverride("JIRA_EMAIL", "bot@acme.com")
	if c.JiraEnabled {
		t.Error("JiraEnabled should still be false without JIRA_TOKEN")
	}
	c.ApplyEnvOverride("JIRA_TOKEN", "tok-abc")
	if !c.JiraEnabled {
		t.Error("JiraEnabled should become true once all 3 Jira vars are set")
	}

	// Clearing one of the vars should disable it again.
	c.ApplyEnvOverride("JIRA_TOKEN", "")
	if c.JiraEnabled {
		t.Error("JiraEnabled should become false again once a var is cleared")
	}
}

func TestIsLiveEditable(t *testing.T) {
	editable := []string{
		"APP_NAME", "PRIMARY_COLOR", "SECONDARY_COLOR", "ACCENT_COLOR", "LOGO_PATH",
		"FONT_URL", "FONT_FAMILY", "FONT_FAMILY_MONO", "HIDE_FOOTER", "DEFAULT_LANG",
		"METRICS_TOKEN", "DISABLE_FLOORPLANS", "DISABLE_API", "DISABLE_PROJECTS",
		"ONSITE_RATIO_THRESHOLD", "SMTP_URL", "SMTP_FROM", "APP_URL",
		"ADMIN_USER", "ADMIN_PASSWORD", "PASSKEY_RP_ID", "PASSKEY_RP_ORIGIN",
		"JIRA_BASE_URL", "JIRA_EMAIL", "JIRA_TOKEN",
	}
	for _, k := range editable {
		if !IsLiveEditable(k) {
			t.Errorf("expected %s to be live-editable", k)
		}
	}

	notEditable := []string{"PORT", "DATA_DIR", "SECRET_KEY", "DB_DRIVER", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD", "DB_SSL_MODE", "PATH", "UNKNOWN_VAR"}
	for _, k := range notEditable {
		if IsLiveEditable(k) {
			t.Errorf("expected %s to NOT be live-editable", k)
		}
	}
}

func TestApplyEnvOverride_UnknownKey_ReturnsFalse(t *testing.T) {
	c := Load()
	if c.ApplyEnvOverride("SECRET_KEY", "hacked") {
		t.Error("SECRET_KEY should not be live-editable")
	}
	if c.ApplyEnvOverride("SOME_RANDOM_UNKNOWN_VAR", "x") {
		t.Error("unknown vars should not be live-editable")
	}
}

func TestApplyEnvOverride_StringFields(t *testing.T) {
	c := Load()
	tests := []struct {
		key  string
		want func(*Config) string
	}{
		{"APP_NAME", func(c *Config) string { return c.AppName }},
		{"PRIMARY_COLOR", func(c *Config) string { return c.PrimaryColor }},
		{"SECONDARY_COLOR", func(c *Config) string { return c.SecondaryColor }},
		{"ACCENT_COLOR", func(c *Config) string { return c.AccentColor }},
		{"LOGO_PATH", func(c *Config) string { return c.LogoPath }},
		{"FONT_URL", func(c *Config) string { return c.FontURL }},
		{"FONT_FAMILY", func(c *Config) string { return c.FontFamily }},
		{"FONT_FAMILY_MONO", func(c *Config) string { return c.FontFamilyMono }},
		{"DEFAULT_LANG", func(c *Config) string { return c.DefaultLang }},
		{"METRICS_TOKEN", func(c *Config) string { return c.MetricsToken }},
		{"SMTP_URL", func(c *Config) string { return c.SMTPURL }},
		{"SMTP_FROM", func(c *Config) string { return c.SMTPFrom }},
		{"APP_URL", func(c *Config) string { return c.AppURL }},
		{"ADMIN_USER", func(c *Config) string { return c.AdminUser }},
		{"ADMIN_PASSWORD", func(c *Config) string { return c.AdminPassword }},
		{"PASSKEY_RP_ID", func(c *Config) string { return c.PasskeyRPID }},
		{"PASSKEY_RP_ORIGIN", func(c *Config) string { return c.PasskeyRPOrigin }},
		{"JIRA_BASE_URL", func(c *Config) string { return c.JiraBaseURL }},
		{"JIRA_EMAIL", func(c *Config) string { return c.JiraEmail }},
		{"JIRA_TOKEN", func(c *Config) string { return c.JiraToken }},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			want := "value-for-" + tt.key
			if !c.ApplyEnvOverride(tt.key, want) {
				t.Fatalf("ApplyEnvOverride(%s) returned false", tt.key)
			}
			if got := tt.want(c); got != want {
				t.Errorf("%s: want %q, got %q", tt.key, want, got)
			}
			if got := os.Getenv(tt.key); got != want {
				t.Errorf("os.Getenv(%s): want %q, got %q", tt.key, want, got)
			}
		})
	}
}

func TestApplyEnvOverride_BoolFields(t *testing.T) {
	c := Load()
	for _, key := range []string{"HIDE_FOOTER", "DISABLE_FLOORPLANS", "DISABLE_API", "DISABLE_PROJECTS"} {
		t.Run(key, func(t *testing.T) {
			if !c.ApplyEnvOverride(key, "true") {
				t.Fatalf("ApplyEnvOverride(%s, true) returned false", key)
			}
			var got bool
			switch key {
			case "HIDE_FOOTER":
				got = c.HideFooter
			case "DISABLE_FLOORPLANS":
				got = c.DisableFloorplans
			case "DISABLE_API":
				got = c.DisableAPI
			case "DISABLE_PROJECTS":
				got = c.DisableProjects
			}
			if !got {
				t.Errorf("%s: want true after override", key)
			}

			if !c.ApplyEnvOverride(key, "false") {
				t.Fatalf("ApplyEnvOverride(%s, false) returned false", key)
			}
			switch key {
			case "HIDE_FOOTER":
				got = c.HideFooter
			case "DISABLE_FLOORPLANS":
				got = c.DisableFloorplans
			case "DISABLE_API":
				got = c.DisableAPI
			case "DISABLE_PROJECTS":
				got = c.DisableProjects
			}
			if got {
				t.Errorf("%s: want false after override", key)
			}
		})
	}
}

func TestApplyEnvOverride_BoolField_InvalidValueKeepsPrevious(t *testing.T) {
	c := Load()
	c.HideFooter = true
	if !c.ApplyEnvOverride("HIDE_FOOTER", "not-a-bool") {
		t.Fatal("ApplyEnvOverride should still return true for a recognized key")
	}
	if !c.HideFooter {
		t.Error("HideFooter should keep its previous value when given an unparseable value")
	}
}

func TestApplyEnvOverride_FloatField(t *testing.T) {
	c := Load()
	if !c.ApplyEnvOverride("ONSITE_RATIO_THRESHOLD", "80.5") {
		t.Fatal("ApplyEnvOverride(ONSITE_RATIO_THRESHOLD) returned false")
	}
	if c.OnsiteRatioThreshold != 80.5 {
		t.Errorf("OnsiteRatioThreshold: want 80.5, got %v", c.OnsiteRatioThreshold)
	}

	// Invalid float keeps the previous value.
	if !c.ApplyEnvOverride("ONSITE_RATIO_THRESHOLD", "notafloat") {
		t.Fatal("ApplyEnvOverride(ONSITE_RATIO_THRESHOLD) returned false")
	}
	if c.OnsiteRatioThreshold != 80.5 {
		t.Errorf("OnsiteRatioThreshold should keep previous value 80.5, got %v", c.OnsiteRatioThreshold)
	}
}
