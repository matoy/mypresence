package handlers

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/i18n"
	"github.com/matoy/mypresence/internal/models"
)

func TestAdminHolidaysTemplate_RendersDuplicateButton(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	templatePath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../web/templates/admin_holidays.html"))
	content, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("failed to read admin_holidays.html: %v", err)
	}

	tmpl, err := template.New("admin_holidays.html").Funcs(template.FuncMap{
		"json": func(v interface{}) string { return "{}" },
	}).Parse(string(content))
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	data := map[string]interface{}{
		"T": i18n.T("fr"),
		"Data": map[string]interface{}{
			"Holidays": []models.Holiday{
				{
					ID:           1,
					Date:         "2026-05-01",
					Name:         "Fête du Travail",
					AllowImputed: false,
					CountryCode:  "FR",
				},
			},
			"Countries": models.AllCountries,
			"Error":     "",
		},
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "content", data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	html := buf.String()

	// Verify "Dupliquer" button exists
	if !strings.Contains(html, "duplicateHoliday(") {
		t.Errorf("expected duplicateHoliday call in template, not found")
	}

	frDuplicate := i18n.T("fr")["admin.duplicate"]
	if !strings.Contains(html, frDuplicate) {
		t.Errorf("expected %q in rendered html", frDuplicate)
	}

	// Verify duplicate button is placed between edit and delete
	editIdx := strings.Index(html, "admin.edit")
	if editIdx == -1 {
		editIdx = strings.Index(html, i18n.T("fr")["admin.edit"])
	}
	dupIdx := strings.Index(html, frDuplicate)
	delIdx := strings.Index(html, "admin.delete")
	if delIdx == -1 {
		delIdx = strings.Index(html, i18n.T("fr")["admin.delete"])
	}

	if editIdx == -1 || dupIdx == -1 || delIdx == -1 {
		t.Fatalf("could not find all three action buttons in html (edit=%d, dup=%d, del=%d)", editIdx, dupIdx, delIdx)
	}
	if !(editIdx < dupIdx && dupIdx < delIdx) {
		t.Errorf("expected duplicate button between edit and delete, got edit=%d, dup=%d, del=%d", editIdx, dupIdx, delIdx)
	}

	// Verify JS logic exists
	if !strings.Contains(html, "duplicateHoliday(h)") {
		t.Errorf("expected duplicateHoliday(h) definition in script")
	}
}
