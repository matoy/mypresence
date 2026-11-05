package handlers

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/i18n"
	"github.com/matoy/mypresence/internal/models"
)

func TestMemberLogsPage_PermissionsAndFilters(t *testing.T) {
	d := newCRUDTestDB(t)

	leaderID, _ := d.CreateLocalUser("leader1@test.com", "Leader One", "pass")
	leaderUser, _ := d.GetUserByID(leaderID)

	memberID, _ := d.CreateLocalUser("member1@test.com", "Member One", "pass")
	memberUser, _ := d.GetUserByID(memberID)

	outsiderID, _ := d.CreateLocalUser("outsider1@test.com", "Outsider", "pass")
	outsiderUser, _ := d.GetUserByID(outsiderID)

	adminID, _ := d.CreateLocalUser("admin1@test.com", "Admin One", "pass")
	_ = d.UpdateUserRoles(adminID, models.RoleGlobal)
	adminUser, _ := d.GetUserByID(adminID)

	teamID, _ := d.CreateTeamWithDetails("Engineering", "", false, false)
	_ = d.AddTeamMember(teamID, leaderID)
	_ = d.SetTeamLeaders(teamID, []int64{leaderID})
	_ = d.AddTeamMember(teamID, memberID)

	var renderedPage string
	var renderedData map[string]interface{}
	renderMock := func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		renderedPage = page
		if m, ok := data.(map[string]interface{}); ok {
			renderedData = m
		}
	}

	h := &AdminHandler{
		DB:     d,
		Render: renderMock,
	}

	// 1. Anonymous request -> 403
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/teams/members/2/logs", nil)
		req.SetPathValue("id", "2")
		h.MemberLogsPage(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 for anonymous, got %d", rec.Code)
		}
	}

	// 2. Outsider (not leader, not admin) -> 403
	{
		rec := httptest.NewRecorder()
		req := reqWithUser(d, outsiderUser, http.MethodGet, "/admin/teams/members/2/logs", nil)
		req.SetPathValue("id", "2")
		h.MemberLogsPage(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 for outsider, got %d", rec.Code)
		}
	}

	// 3. Team leader requests non-team member -> 403
	{
		rec := httptest.NewRecorder()
		req := reqWithUser(d, leaderUser, http.MethodGet, "/admin/teams/members/3/logs", nil)
		req.SetPathValue("id", "3") // outsiderID is 3
		h.MemberLogsPage(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 when leader accesses non-member, got %d", rec.Code)
		}
	}

	// 4. Team leader requests team member -> 200, renders admin_user_logs, HideAdminSection: true
	{
		renderedPage = ""
		renderedData = nil
		rec := httptest.NewRecorder()
		req := reqWithUser(d, leaderUser, http.MethodGet, "/admin/teams/members/2/logs?from=team_1", nil)
		req.SetPathValue("id", "2") // memberID is 2
		h.MemberLogsPage(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for leader, got %d", rec.Code)
		}
		if renderedPage != "admin_user_logs" {
			t.Errorf("expected page 'admin_user_logs', got %q", renderedPage)
		}
		if hide, ok := renderedData["HideAdminSection"].(bool); !ok || !hide {
			t.Errorf("expected HideAdminSection=true for team leader")
		}
		if back, ok := renderedData["BackURL"].(string); !ok || back != "/admin/teams/1/logs" {
			t.Errorf("expected BackURL='/admin/teams/1/logs', got %q", back)
		}
		if target, ok := renderedData["TargetUser"].(*models.User); !ok || target.ID != memberUser.ID {
			t.Errorf("expected TargetUser id %d", memberUser.ID)
		}
	}

	// 5. From calendar -> BackURL = "/"
	{
		renderedData = nil
		rec := httptest.NewRecorder()
		req := reqWithUser(d, leaderUser, http.MethodGet, "/admin/teams/members/2/logs?from=calendar", nil)
		req.SetPathValue("id", "2")
		h.MemberLogsPage(rec, req)

		if back, ok := renderedData["BackURL"].(string); !ok || back != "/" {
			t.Errorf("expected BackURL='/', got %q", back)
		}
	}

	// 6. Global Admin -> 200, HideAdminSection: false
	{
		renderedData = nil
		rec := httptest.NewRecorder()
		req := reqWithUser(d, adminUser, http.MethodGet, "/admin/teams/members/2/logs", nil)
		req.SetPathValue("id", "2")
		h.MemberLogsPage(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin, got %d", rec.Code)
		}
		if hide, ok := renderedData["HideAdminSection"].(bool); !ok || hide {
			t.Errorf("expected HideAdminSection=false for global admin")
		}
	}
}

func TestTeamLogsPage_PermissionsAndFilters(t *testing.T) {
	d := newCRUDTestDB(t)

	leaderID, _ := d.CreateLocalUser("tleader@test.com", "Team Leader", "pass")
	leaderUser, _ := d.GetUserByID(leaderID)

	memberID, _ := d.CreateLocalUser("tmember@test.com", "Team Member", "pass")

	outsiderID, _ := d.CreateLocalUser("toutsider@test.com", "Outsider", "pass")
	outsiderUser, _ := d.GetUserByID(outsiderID)

	adminID, _ := d.CreateLocalUser("tadmin@test.com", "Team Admin", "pass")
	_ = d.UpdateUserRoles(adminID, models.RoleGlobal)
	adminUser, _ := d.GetUserByID(adminID)

	teamID, _ := d.CreateTeamWithDetails("Alpha Team", "", false, false)
	_ = d.AddTeamMember(teamID, leaderID)
	_ = d.SetTeamLeaders(teamID, []int64{leaderID})
	_ = d.AddTeamMember(teamID, memberID)

	var renderedPage string
	var renderedData map[string]interface{}
	renderMock := func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		renderedPage = page
		if m, ok := data.(map[string]interface{}); ok {
			renderedData = m
		}
	}

	h := &AdminHandler{
		DB:     d,
		Render: renderMock,
	}

	// 1. Anonymous request -> 403
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/teams/1/logs", nil)
		req.SetPathValue("id", "1")
		h.TeamLogsPage(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 for anonymous, got %d", rec.Code)
		}
	}

	// 2. Outsider (not leader of team, not admin) -> 403
	{
		rec := httptest.NewRecorder()
		req := reqWithUser(d, outsiderUser, http.MethodGet, "/admin/teams/1/logs", nil)
		req.SetPathValue("id", "1")
		h.TeamLogsPage(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 for non-leader outsider, got %d", rec.Code)
		}
	}

	// 3. Team leader -> 200, renders admin_team_logs
	{
		renderedPage = ""
		renderedData = nil
		rec := httptest.NewRecorder()
		req := reqWithUser(d, leaderUser, http.MethodGet, "/admin/teams/1/logs?days=30&member_id=2&from=calendar", nil)
		req.SetPathValue("id", "1")
		h.TeamLogsPage(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for team leader, got %d", rec.Code)
		}
		if renderedPage != "admin_team_logs" {
			t.Errorf("expected page 'admin_team_logs', got %q", renderedPage)
		}
		if days, ok := renderedData["Days"].(int); !ok || days != 30 {
			t.Errorf("expected Days=30, got %v", renderedData["Days"])
		}
		if selMember, ok := renderedData["SelectedMemberID"].(int64); !ok || selMember != 2 {
			t.Errorf("expected SelectedMemberID=2, got %v", renderedData["SelectedMemberID"])
		}
		if back, ok := renderedData["BackURL"].(string); !ok || back != "/" {
			t.Errorf("expected BackURL='/', got %q", back)
		}
		if team, ok := renderedData["Team"].(*models.Team); !ok || team.ID != teamID {
			t.Errorf("expected Team ID %d", teamID)
		}
	}

	// 4. Global admin -> 200
	{
		renderedPage = ""
		rec := httptest.NewRecorder()
		req := reqWithUser(d, adminUser, http.MethodGet, "/admin/teams/1/logs", nil)
		req.SetPathValue("id", "1")
		h.TeamLogsPage(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for global admin, got %d", rec.Code)
		}
		if renderedPage != "admin_team_logs" {
			t.Errorf("expected page 'admin_team_logs', got %q", renderedPage)
		}
	}
}

func TestAdminTeamLogsTemplate_Renders(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	logsPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../web/templates/admin_team_logs.html"))
	logsBytes, err := os.ReadFile(logsPath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}

	tmpl, err := template.New("admin_team_logs.html").Parse(string(logsBytes))
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	now := time.Now()
	team := &models.Team{
		ID:   10,
		Name: "Core Team",
	}

	members := []models.TeamMember{
		{User: models.User{ID: 1, Name: "Alice", Email: "alice@test.com"}},
		{User: models.User{ID: 2, Name: "Bob", Email: "bob@test.com"}},
	}

	statuses := []models.Status{
		{ID: 1, Name: "Présent", Color: "#22c55e"},
		{ID: 2, Name: "Télétravail", Color: "#3b82f6"},
	}

	logs := []models.PresenceLog{
		{
			ID:          1,
			UserID:      1,
			UserName:    "Alice",
			ActorID:     1,
			ActorName:   "Alice",
			Date:        "2026-09-01",
			Action:      "set",
			StatusName:  "Présent",
			StatusColor: "#22c55e",
			CreatedAt:   now,
		},
		{
			ID:          2,
			UserID:      2,
			UserName:    "Bob",
			ActorID:     99,
			ActorName:   "Team Leader",
			Date:        "2026-09-02",
			Half:        "AM",
			Action:      "set",
			StatusName:  "Télétravail",
			StatusColor: "#3b82f6",
			CreatedAt:   now,
		},
	}

	data := models.PageData{
		User: &models.User{ID: 99, Name: "Team Leader"},
		T:    i18n.T("fr"),
		Data: map[string]interface{}{
			"Team":             team,
			"Members":          members,
			"Statuses":         statuses,
			"Logs":             logs,
			"Days":             7,
			"FilterBaseURL":    "/admin/teams/10/logs",
			"SelectedMemberID": int64(0),
			"BackURL":          "/admin/teams",
			"FromParam":        "",
		},
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "content", data); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	html := out.String()

	// Verify essential elements
	if !strings.Contains(html, "Core Team") {
		t.Error("expected team name in HTML")
	}
	if !strings.Contains(html, "Alice") {
		t.Error("expected Alice in HTML")
	}
	if !strings.Contains(html, "Bob") {
		t.Error("expected Bob in HTML")
	}
	if !strings.Contains(html, "Team Leader") {
		t.Error("expected Team Leader actor name in HTML")
	}
	if !strings.Contains(html, "filterMember") {
		t.Error("expected filterMember in Alpine data")
	}
	if !strings.Contains(html, "/admin/teams/members/1/logs?from=team_10") {
		t.Error("expected member log link with team from parameter")
	}
}
