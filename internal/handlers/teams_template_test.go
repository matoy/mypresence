package handlers

import (
	"bytes"
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/i18n"
	"github.com/matoy/mypresence/internal/models"
)

func TestAdminTeamsTemplate_RendersCleanly(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	teamsPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../web/templates/admin_teams.html"))
	teamsBytes, err := os.ReadFile(teamsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}

	funcMap := template.FuncMap{
		"json": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"add": func(a, b int) int {
			return a + b
		},
		"flagFor": models.FlagForCountry,
	}

	tmpl, err := template.New("admin_teams.html").Funcs(funcMap).Parse(string(teamsBytes))
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	teamsList := []map[string]interface{}{
		{
			"Team": models.Team{
				ID:           1,
				Name:         "Engineering & Support",
				JiraSpaceKey: "ENG",
			},
			"Members": []models.TeamMember{
				{User: models.User{ID: 10, Name: "Alice", Email: "alice@example.com"}},
			},
			"CanEdit": true,
		},
	}

	data := models.PageData{
		T: i18n.T("fr"),
		Data: map[string]interface{}{
			"Teams":          teamsList,
			"Users":          []models.User{{ID: 10, Name: "Alice", Email: "alice@example.com"}, {ID: 20, Name: "Bob", Email: "bob@example.com"}},
			"Domains":        []models.Domain{{ID: 1, Name: "IT"}},
			"Countries":      models.AllCountries,
			"CanManageTeams": true,
			"JiraEnabled":    true,
		},
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "content", data); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	html := out.String()

	if !strings.Contains(html, "Engineering") {
		t.Error("expected team name in rendered HTML")
	}
	if !strings.Contains(html, "teamsAdmin(") {
		t.Error("expected teamsAdmin call in rendered HTML")
	}
	if !strings.Contains(html, "matchesTeam($el.dataset.teamName") {
		t.Error("expected matchesTeam with dataset variables in rendered HTML")
	}
}
