package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestT_KnownLanguages(t *testing.T) {
	langs := []string{"en", "fr", "de", "es", "it"}
	for _, lang := range langs {
		m := T(lang)
		if m == nil {
			t.Errorf("T(%q) returned nil", lang)
		}
	}
}

func TestT_FallsBackToEnglish(t *testing.T) {
	m := T("xx")
	if m == nil {
		t.Fatal("T(unknown) should fall back to English map, got nil")
	}
	// English map must be non-empty
	if len(m) == 0 {
		t.Error("English fallback map is empty")
	}
}

func TestT_EnglishAndFrenchDiffer(t *testing.T) {
	en := T("en")
	fr := T("fr")
	if len(en) == 0 || len(fr) == 0 {
		t.Fatal("translation maps should not be empty")
	}
	// At least one key should differ between English and French
	different := false
	for k, vEN := range en {
		if vFR, ok := fr[k]; ok && vFR != vEN {
			different = true
			break
		}
	}
	if !different {
		t.Error("English and French translations appear identical")
	}
}

func TestLangFromRequest_CookieOverrides(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "lang", Value: "fr"})
	got := LangFromRequest(req, "en")
	if got != "fr" {
		t.Errorf("expected 'fr' from cookie, got %q", got)
	}
}

func TestLangFromRequest_InvalidCookieFallsToDefault(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "lang", Value: "xx"})
	got := LangFromRequest(req, "de")
	if got != "de" {
		t.Errorf("invalid cookie lang should fall back to default 'de', got %q", got)
	}
}

func TestLangFromRequest_NoCookie_ValidDefault(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	got := LangFromRequest(req, "it")
	if got != "it" {
		t.Errorf("no cookie: expected default 'it', got %q", got)
	}
}

func TestLangFromRequest_NoCookie_InvalidDefault_FallsToEnglish(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	got := LangFromRequest(req, "zz")
	if got != "en" {
		t.Errorf("invalid default lang should fall back to 'en', got %q", got)
	}
}

func TestLangFromRequest_AllSupportedLangs(t *testing.T) {
	for _, s := range Supported {
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: "lang", Value: s.Code})
		got := LangFromRequest(req, "en")
		if got != s.Code {
			t.Errorf("supported lang %q from cookie: got %q", s.Code, got)
		}
	}
}

func TestHelpKeys_AllLanguages(t *testing.T) {
	requiredKeys := []string{
		"help.button_title",
		"help.title",
		"help.topic_select",
		"help.close",
		"help.general_hint",
		"help.topic.calendar.title",
		"help.topic.calendar.desc",
		"help.topic.calendar.item1",
		"help.topic.floorplan.title",
		"help.topic.projects.title",
		"help.topic.admin_activity.title",
		"help.topic.admin_projects_report.title",
		"help.topic.admin_teams.title",
		"help.topic.admin_statuses.title",
		"help.topic.admin_floorplans.title",
		"help.topic.admin_projects.title",
		"help.topic.admin_users.title",
		"help.topic.admin_domains.title",
		"help.topic.admin_holidays.title",
		"help.topic.admin_general_settings.title",
		"help.topic.admin_news.title",
		"help.topic.settings.title",
		"help.topic.impersonate.title",
	}

	langs := []string{"en", "fr", "de", "es", "it"}
	for _, lang := range langs {
		m := T(lang)
		for _, key := range requiredKeys {
			val, ok := m[key]
			if !ok || val == "" {
				t.Errorf("lang %q is missing required help key %q", lang, key)
			}
		}
	}
}

func TestProjectsManualKeys_AllLanguages(t *testing.T) {
	requiredKeys := []string{
		"projects.manual.none",
		"projects.manual.non_billable",
		"projects.manual.complete",
		"projects.manual.type_jira",
		"projects.manual.type_servicenow",
		"projects.manual.type_other",
		"projects.manual.jira_placeholder",
		"projects.manual.no_tickets",
		"projects.manual.comments",
		"projects.manual.comments_mandatory",
		"projects.manual.comment_required",
		"projects.manual.save_day",
		"projects.manual.day_saved",
		"projects.manual.add_activity",
		"projects.manual.no_activities_day",
		"projects.manual.filter_hide_non_billable",
		"projects.manual.filter_hide_completed",
		"projects.manual.collapse_all",
		"projects.manual.expand_all",
		"projects.manual.unsaved",
		"projects.manual.unsaved_changes",
	}

	langs := []string{"en", "fr", "de", "es", "it"}
	for _, lang := range langs {
		m := T(lang)
		for _, key := range requiredKeys {
			val, ok := m[key]
			if !ok || val == "" {
				t.Errorf("lang %q is missing required manual tasks key %q", lang, key)
			}
		}
	}
}

func TestCountryKeys_AllLanguages(t *testing.T) {
	requiredKeys := []string{
		"teams.countries",
		"teams.countries_hint",
		"teams.no_countries",
		"teams.add_country",
		"holidays.country",
		"holidays.countries_hint",
		"holidays.all_countries",
		"holidays.col_country",
		"holidays.country_placeholder",
	}

	langs := []string{"en", "fr", "de", "es", "it"}
	for _, lang := range langs {
		m := T(lang)
		for _, key := range requiredKeys {
			val, ok := m[key]
			if !ok || val == "" {
				t.Errorf("lang %q is missing required country key %q", lang, key)
			}
		}
	}
}
