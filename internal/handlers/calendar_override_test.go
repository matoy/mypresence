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

func TestCalendarPage_WithPresenceOverrides(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	var renderedPage string
	var renderedData interface{}
	h := &CalendarHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			renderedPage = page
			renderedData = data
		},
		DisableFloorplans: true,
	}

	managerID, _ := d.CreateLocalUser("manager@test.com", "Manager Bob", "password1")
	userID, _ := d.CreateLocalUser("user@test.com", "User Alice", "password1")
	teamID, _ := d.CreateTeam("Engineering")
	_ = d.AddTeamMember(teamID, managerID)
	_ = d.AddTeamMember(teamID, userID)

	statusID, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#3b82f6", OnSite: true})

	// Manager Bob sets Alice's presence on 2026-05-15
	_ = d.SetPresences(userID, []string{"2026-05-15"}, statusID, "full")
	_ = d.LogPresenceAction(managerID, userID, "set", []string{"2026-05-15"}, statusID, "full")

	// Manager Bob clears Alice's presence on 2026-05-16
	_ = d.LogPresenceAction(managerID, userID, "clear", []string{"2026-05-16"}, 0, "full")

	tok, _ := d.CreateSession(userID)
	req := httptest.NewRequest(http.MethodGet, "/?year=2026&month=5", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()

	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)

	if renderedPage != "calendar" {
		t.Fatalf("expected page 'calendar', got '%s'", renderedPage)
	}

	data, ok := renderedData.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", renderedData)
	}

	overrides, ok := data["Overrides"].(map[string]models.PresenceOverride)
	if !ok {
		t.Fatalf("expected Overrides to be map[string]models.PresenceOverride, got %T", data["Overrides"])
	}

	if ov, ok := overrides["2026-05-15"]; !ok || ov.ActorID != managerID || ov.Action != "set" {
		t.Errorf("expected override on 2026-05-15 with actor %d and action set, got %+v", managerID, ov)
	}
	if ov, ok := overrides["2026-05-16"]; !ok || ov.ActorID != managerID || ov.Action != "clear" {
		t.Errorf("expected override on 2026-05-16 with actor %d and action clear, got %+v", managerID, ov)
	}

	teamViews, ok := data["TeamViews"].([]teamCalendarView)
	if !ok || len(teamViews) == 0 {
		t.Fatalf("expected teamViews, got %v", teamViews)
	}
	memberOverrides := teamViews[0].Overrides[userID]
	if ov, ok := memberOverrides["2026-05-15"]; !ok || ov.ActorID != managerID {
		t.Errorf("expected teamView member override on 2026-05-15, got %+v", ov)
	}
}

func TestCalendarTemplate_RendersOverrideBadge(t *testing.T) {
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
			if m == nil {
				return nil
			}
			if ov, ok := m[date]; ok {
				return &ov
			}
			return nil
		},
	}

	base := `{{define "content"}}` + string(calBytes) + `{{end}}`
	tmpl, err := template.New("calendar.html").Funcs(funcMap).Parse(string(calBytes))
	if err != nil {
		t.Fatalf("parse calendar template: %v", err)
	}
	_ = base

	days := []models.DayInfo{
		{Date: "2026-05-15", Day: 15, DayIndex: 5, IsWeekend: false, IsHoliday: false},
		{Date: "2026-05-16", Day: 16, DayIndex: 6, IsWeekend: false, IsHoliday: false},
	}

	userPresences := map[string]map[string]int64{
		"2026-05-15": {"full": 1},
	}
	userOverrides := map[string]models.PresenceOverride{
		"2026-05-15": {ActorID: 99, ActorName: "Bob Boss", Action: "set", Half: "full"},
		"2026-05-16": {ActorID: 99, ActorName: "Bob Boss", Action: "clear", Half: "full"},
	}

	pageData := models.PageData{
		Page: "calendar",
		Lang: "fr",
		User: &models.User{ID: 1, Name: "Alice"},
		T: map[string]string{
			"cal.modified_by": "Modifié par %s",
			"cal.cleared_by":  "Statut retiré par %s",
			"cal.not_set":     "Non défini",
		},
		Data: map[string]interface{}{
			"Year":             2026,
			"Month":            5,
			"Days":             days,
			"Statuses":         []models.Status{{ID: 1, Name: "Office", Color: "#3b82f6"}},
			"Presences":        userPresences,
			"Overrides":        userOverrides,
			"CurrentUserID":    int64(1),
			"ReservationDates": map[string]bool{},
			"DeclarableDays":   20,
			"DeclaredDays":     1,
			"TeamViews":        []teamCalendarView{},
		},
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "content", pageData); err != nil {
		t.Fatalf("execute calendar template: %v", err)
	}

	html := out.String()

	// Check for override badge emoji
	if !strings.Contains(html, "✍️") {
		t.Fatal("expected override badge icon ✍️ in rendered HTML")
	}

	// Check for modified tooltip
	if !strings.Contains(html, "Modifié par Bob Boss") {
		t.Fatal("expected 'Modifié par Bob Boss' tooltip in rendered HTML")
	}

	// Check for cleared tooltip
	if !strings.Contains(html, "Statut retiré par Bob Boss") {
		t.Fatal("expected 'Statut retiré par Bob Boss' tooltip in rendered HTML")
	}
}
