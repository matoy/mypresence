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

	"github.com/matoy/myPresence/internal/models"
)

func renderLayoutForUser(t *testing.T, user *models.User, disableProjects bool) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	layoutPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../web/templates/layout.html"))
	layoutBytes, err := os.ReadFile(layoutPath)
	if err != nil {
		t.Fatalf("read layout template: %v", err)
	}

	funcMap := template.FuncMap{
		"json": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
	}
	base := string(layoutBytes) + `{{define "content"}}content{{end}}`
	tmpl, err := template.New("layout.html").Funcs(funcMap).Parse(base)
	if err != nil {
		t.Fatalf("parse layout template: %v", err)
	}

	data := models.PageData{
		Config: map[string]interface{}{"AppName": "myPresence"},
		User:   user,
		Page:   "calendar",
		T: map[string]string{
			"nav.admin":             "Admin",
			"nav.admin.projects":    "Projects",
			"nav.admin.impersonate": "Impersonate",
		},
		Lang:            "en",
		DisableProjects: disableProjects,
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "layout", data); err != nil {
		t.Fatalf("execute layout template: %v", err)
	}
	return out.String()
}

func TestLayoutTemplate_AdminMenu_HiddenForBasicUser(t *testing.T) {
	html := renderLayoutForUser(t, &models.User{ID: 1, Name: "Basic", Roles: models.RoleBasic}, false)

	if strings.Contains(html, "adminMenuOpen") {
		t.Fatal("admin menu should be hidden for basic user")
	}
	if strings.Contains(html, "/admin/projects") {
		t.Fatal("admin projects link should not be present for basic user")
	}
}

func TestLayoutTemplate_AdminMenu_ShowsProjectsAndImpersonateForGlobal(t *testing.T) {
	html := renderLayoutForUser(t, &models.User{ID: 1, Name: "Global", Roles: models.RoleGlobal}, false)

	if !strings.Contains(html, "adminMenuOpen") {
		t.Fatal("admin menu should be visible for global user")
	}
	if !strings.Contains(html, "/admin/projects") {
		t.Fatal("admin projects link should be present for global user")
	}
	if c := strings.Count(html, "/impersonate"); c < 2 {
		t.Fatalf("expected impersonate link in both desktop and mobile admin menus, got %d", c)
	}
}

func TestLayoutTemplate_AdminMenu_ShowsForProjectsAdminOnly(t *testing.T) {
	html := renderLayoutForUser(t, &models.User{ID: 1, Name: "Proj Admin", Roles: models.RoleProjectsAdmin}, false)

	if !strings.Contains(html, "adminMenuOpen") {
		t.Fatal("admin menu should be visible for projects_admin")
	}
	if !strings.Contains(html, "/admin/projects") {
		t.Fatal("admin projects link should be present for projects_admin")
	}
}
