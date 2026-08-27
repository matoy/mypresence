package handlers

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

func TestCalendarPage_WithProjectActivities_UserRow(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	uID, err := d.CreateLocalUser("dev@example.com", "Dev", "password")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 100% total on 2026-05-15 (60% + 40%)
	if _, err := d.CreateProjectActivity(uID, "2026-05-15", models.ActivityTypeJira, "PROJ-1", "Task 1", "", 60); err != nil {
		t.Fatalf("create activity 1: %v", err)
	}
	if _, err := d.CreateProjectActivity(uID, "2026-05-15", models.ActivityTypeOther, "", "", "Task 2", 40); err != nil {
		t.Fatalf("create activity 2: %v", err)
	}

	// 50% total on 2026-05-16 (< 100%)
	if _, err := d.CreateProjectActivity(uID, "2026-05-16", models.ActivityTypeOther, "", "", "Task 3", 50); err != nil {
		t.Fatalf("create activity 3: %v", err)
	}

	var capturedData map[string]interface{}
	h := &CalendarHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			if m, ok := data.(map[string]interface{}); ok {
				capturedData = m
			}
		},
	}

	tok, _ := d.CreateSession(uID)
	req := httptest.NewRequest(http.MethodGet, "/?year=2026&month=5", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	projActs, ok := capturedData["ProjectActivities"].(map[string]bool)
	if !ok {
		t.Fatalf("expected ProjectActivities map[string]bool in data, got %T", capturedData["ProjectActivities"])
	}

	if !projActs["2026-05-15"] {
		t.Errorf("expected 2026-05-15 to be true (100%% complete), got false")
	}
	if projActs["2026-05-16"] {
		t.Errorf("expected 2026-05-16 to be false (50%% complete), got true")
	}
}

func TestCalendarPage_WithProjectActivities_TeamViews(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	managerID, err := d.CreateLocalUser("manager@example.com", "Manager", "password")
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	_ = d.UpdateUserRoles(managerID, models.RoleTeamManager)

	memberID, err := d.CreateLocalUser("member@example.com", "Member", "password")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	teamID, err := d.CreateTeam("Engineering")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	_ = d.AddTeamMember(teamID, managerID)
	_ = d.AddTeamMember(teamID, memberID)

	// Member has 100% on 2026-05-15
	if _, err := d.CreateProjectActivity(memberID, "2026-05-15", models.ActivityTypeJira, "PROJ-2", "Task", "", 100); err != nil {
		t.Fatalf("create member activity: %v", err)
	}

	var capturedData map[string]interface{}
	h := &CalendarHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			if m, ok := data.(map[string]interface{}); ok {
				capturedData = m
			}
		},
	}

	tok, _ := d.CreateSession(managerID)
	req := httptest.NewRequest(http.MethodGet, "/?year=2026&month=5", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	teamViews, ok := capturedData["TeamViews"].([]teamCalendarView)
	if !ok || len(teamViews) == 0 {
		t.Fatalf("expected TeamViews in captured data")
	}

	memberActs := teamViews[0].ProjectActivities[memberID]
	if !memberActs["2026-05-15"] {
		t.Errorf("expected member 2026-05-15 to be true in team view, got %v", memberActs)
	}
}

func TestCalendarPage_WithProjectActivities_DisabledProjects(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	uID, err := d.CreateLocalUser("dev@example.com", "Dev", "password")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, _ = d.CreateProjectActivity(uID, "2026-05-15", models.ActivityTypeOther, "", "", "Task", 100)

	var capturedData map[string]interface{}
	h := &CalendarHandler{
		DB:              d,
		DisableProjects: true,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			if m, ok := data.(map[string]interface{}); ok {
				capturedData = m
			}
		},
	}

	tok, _ := d.CreateSession(uID)
	req := httptest.NewRequest(http.MethodGet, "/?year=2026&month=5", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	projActs, ok := capturedData["ProjectActivities"].(map[string]bool)
	if !ok {
		t.Fatalf("expected ProjectActivities map[string]bool")
	}
	if len(projActs) != 0 {
		t.Errorf("expected empty ProjectActivities when DisableProjects=true, got %+v", projActs)
	}
}

func TestCalendarTemplate_RendersProjectActivityBadge(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	calPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../web/templates/calendar.html"))
	calBytes, err := os.ReadFile(calPath)
	if err != nil {
		t.Fatalf("read calendar template: %v", err)
	}

	funcMap := template.FuncMap{
		"json": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"statusColor": func(statuses []models.Status, id int64) string {
			return "#3b82f6"
		},
		"statusName": func(statuses []models.Status, id int64) string {
			return "Office"
		},
		"presenceHalf": func(m map[string]map[string]int64, date, half string) int64 {
			if m != nil && m[date] != nil {
				return m[date][half]
			}
			return 0
		},
		"presenceOverride": func(m map[string]models.PresenceOverride, date string) *models.PresenceOverride {
			return nil
		},
	}

	tmpl, err := template.New("calendar.html").Funcs(funcMap).Parse(string(calBytes))
	if err != nil {
		t.Fatalf("parse calendar template: %v", err)
	}

	days := []models.DayInfo{
		{Date: "2026-05-15", Day: 15, DayIndex: 5, IsWeekend: false, IsHoliday: false},
		{Date: "2026-05-16", Day: 16, DayIndex: 6, IsWeekend: false, IsHoliday: false},
	}

	data := models.PageData{
		Config: map[string]interface{}{"AppName": "myPresence"},
		User:   &models.User{ID: 10, Name: "Alice", Roles: models.RoleBasic},
		Page:   "calendar",
		T: map[string]string{
			"cal.project_activity_complete": "Activité projet/tâche déclarée à 100%",
		},
		Data: map[string]interface{}{
			"Year":              2026,
			"Month":             5,
			"Days":              days,
			"CurrentUserID":     int64(10),
			"Presences":         map[string]map[string]int64{},
			"Overrides":         map[string]models.PresenceOverride{},
			"Statuses":          []models.Status{},
			"ReservationDates":  map[string]bool{},
			"ProjectActivities": map[string]bool{"2026-05-15": true},
			"TeamViews":         []teamCalendarView{},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "content", data); err != nil {
		t.Fatalf("render template: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, "🎯") {
		t.Errorf("expected 🎯 badge in rendered HTML, got %s", html)
	}
	if !strings.Contains(html, "Activité projet/tâche déclarée à 100%") {
		t.Errorf("expected project activity tooltip in rendered HTML, got %s", html)
	}
}
