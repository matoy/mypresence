package handlers

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/i18n"
	"github.com/matoy/mypresence/internal/models"
)

func TestAdminUserLogsTemplate_RendersAllSections(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	logsPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../web/templates/admin_user_logs.html"))
	logsBytes, err := os.ReadFile(logsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}

	tmpl, err := template.New("admin_user_logs.html").Parse(string(logsBytes))
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	now := time.Now()
	targetUser := &models.User{
		ID:    42,
		Name:  "Alice Wonderland",
		Email: "alice@example.com",
	}

	notifLogs := []models.Notification{
		{
			ID:             1,
			UserID:         42,
			ActorID:        99,
			ActorName:      "Bob Boss",
			Type:           "team_added",
			Title:          "Ajout à une équipe",
			Message:        "Vous avez été ajouté à l'équipe « Engineering » par Bob Boss.",
			Link:           "Engineering",
			Acknowledged:   true,
			AcknowledgedAt: &now,
			CreatedAt:      now,
		},
		{
			ID:           2,
			UserID:       42,
			ActorID:      0,
			Type:         "info",
			Title:        "System Info",
			Message:      "Welcome to myPresence!",
			Acknowledged: false,
			CreatedAt:    now,
		},
	}

	adminLogs := []models.AdminLog{
		{
			ID:         1,
			ActorID:    42,
			EntityType: "team",
			EntityName: "Engineering",
			Action:     "create",
			CreatedAt:  now,
		},
	}

	presenceLogs := []models.PresenceLog{
		{
			ID:          1,
			UserID:      42,
			ActorID:     42,
			ActorName:   "Alice Wonderland",
			Action:      "set",
			Date:        "2026-08-26",
			StatusName:  "On-site",
			StatusColor: "#3b82f6",
			CreatedAt:   now,
		},
	}

	data := models.PageData{
		User: targetUser,
		T:    i18n.T("fr"),
		Data: map[string]interface{}{
			"TargetUser":       targetUser,
			"Logs":             presenceLogs,
			"AdminLogs":        adminLogs,
			"NotificationLogs": notifLogs,
			"Statuses":         []models.Status{{ID: 1, Name: "On-site", Color: "#3b82f6"}},
			"Days":             7,
			"FilterBaseURL":    "/admin/users/42/logs",
			"HideAdminSection": false,
		},
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "content", data); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	html := out.String()

	// Check sections
	if !strings.Contains(html, "Alice Wonderland") {
		t.Error("expected target user name in HTML")
	}
	if !strings.Contains(html, "Engineering") {
		t.Error("expected Engineering team in HTML")
	}
	if !strings.Contains(html, "Bob Boss") {
		t.Error("expected Bob Boss actor name in HTML")
	}
	if !strings.Contains(html, "showNotifs") {
		t.Error("expected showNotifs state in HTML")
	}
	if !strings.Contains(html, "showAdmin") {
		t.Error("expected showAdmin state in HTML")
	}
	if !strings.Contains(html, "showPresence") {
		t.Error("expected showPresence state in HTML")
	}
	if !strings.Contains(html, "expandAll") {
		t.Error("expected expandAll function in HTML")
	}
}
