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

func TestCalendarPage_TeamViewsHaveHydratedSites(t *testing.T) {
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

	siteID, err := d.CreateSite(models.Site{Name: "HQ Paris", CountryCode: "FR"})
	if err != nil {
		t.Fatalf("create site: %v", err)
	}

	aliceID, _ := d.CreateLocalUser("alice@test.com", "Alice Dupont", "password1")
	bobID, _ := d.CreateLocalUser("bob@test.com", "Bob Martin", "password1")

	// Assign Alice to HQ Paris
	if err := d.UpdateUserSite(aliceID, siteID); err != nil {
		t.Fatalf("update user site: %v", err)
	}

	teamID, _ := d.CreateTeam("Frontend Team")
	_ = d.AddTeamMember(teamID, aliceID)
	_ = d.AddTeamMember(teamID, bobID)

	tok, _ := d.CreateSession(aliceID)
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

	teamViews, ok := data["TeamViews"].([]teamCalendarView)
	if !ok || len(teamViews) == 0 {
		t.Fatalf("expected TeamViews with at least 1 team, got %v", teamViews)
	}

	foundAlice := false
	for _, m := range teamViews[0].Members {
		if m.ID == aliceID {
			foundAlice = true
			if m.SiteName != "HQ Paris" {
				t.Errorf("expected Alice's SiteName to be 'HQ Paris', got %q", m.SiteName)
			}
		}
	}
	if !foundAlice {
		t.Errorf("Alice not found in team members")
	}

	// Bob has no site assigned, so HasUnassignedSite should be true
	if !teamViews[0].HasUnassignedSite {
		t.Errorf("expected HasUnassignedSite to be true for team with Bob (no site)")
	}
	if len(teamViews[0].Sites) != 1 || teamViews[0].Sites[0] != "HQ Paris" {
		t.Errorf("expected team Sites to be ['HQ Paris'], got %v", teamViews[0].Sites)
	}
}

func TestCalendarTemplate_TeamFilterAndFullNames(t *testing.T) {
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
	}

	longNameUser := "Jean-Christophe de Montesquiou-Fezensac"
	pageData := models.PageData{
		Page: "calendar",
		Lang: "fr",
		User: &models.User{ID: 1, Name: longNameUser},
		T: map[string]string{
			"cal.person":                   "Personne",
			"cal.filter_name_placeholder":  "Filtrer par nom…",
			"cal.no_members_found":         "Aucun membre de l'équipe ne correspond aux filtres.",
			"teams.filter_site_all":        "Tous les sites",
			"teams.filter_site_unassigned": "⚠️ Sans affectation de site",
		},
		Data: map[string]interface{}{
			"Year":              2026,
			"Month":             5,
			"Days":              days,
			"Statuses":          []models.Status{{ID: 1, Name: "Office", Color: "#3b82f6"}},
			"Presences":         map[string]map[string]int64{},
			"Overrides":         map[string]models.PresenceOverride{},
			"CurrentUserID":     int64(1),
			"ReservationDates":  map[string]bool{},
			"DeclarableDays":    20,
			"DeclaredDays":      1,
			"TeamViews": []teamCalendarView{
				{
					Team: models.Team{ID: 1, Name: "Dev Team With Unassigned"},
					Members: []models.User{
						{ID: 1, Name: longNameUser, SiteID: 1, SiteName: "HQ Paris"},
						{ID: 2, Name: "Bob Martin", SiteID: 2, SiteName: "Lyon"},
						{ID: 3, Name: "Charlie NoSite", SiteID: 0, SiteName: ""},
					},
					Sites:             []string{"HQ Paris", "Lyon"},
					HasUnassignedSite: true,
					Presences:         map[int64]map[string]map[string]int64{},
					Overrides:         map[int64]map[string]models.PresenceOverride{},
					Reservations:      map[int64]map[string]bool{},
					CanEdit:           false,
					Certifications:    map[int64]bool{},
					ProjectActivities: map[int64]map[string]bool{},
				},
				{
					Team: models.Team{ID: 2, Name: "Dev Team All Assigned"},
					Members: []models.User{
						{ID: 1, Name: longNameUser, SiteID: 1, SiteName: "HQ Paris"},
						{ID: 2, Name: "Bob Martin", SiteID: 2, SiteName: "Lyon"},
					},
					Sites:             []string{"HQ Paris", "Lyon"},
					HasUnassignedSite: false,
					Presences:         map[int64]map[string]map[string]int64{},
					Overrides:         map[int64]map[string]models.PresenceOverride{},
					Reservations:      map[int64]map[string]bool{},
					CanEdit:           false,
					Certifications:    map[int64]bool{},
					ProjectActivities: map[int64]map[string]bool{},
				},
			},
		},
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "content", pageData); err != nil {
		t.Fatalf("execute calendar template: %v", err)
	}

	html := out.String()

	// Verify person column is widened (w-44 sm:w-48)
	if !strings.Contains(html, "w-44 sm:w-48") {
		t.Errorf("expected person column class 'w-44 sm:w-48' in rendered HTML")
	}

	// Verify the long name is displayed completely without being constrained by truncate/max-w-[100px]
	if strings.Contains(html, "max-w-[100px]") {
		t.Errorf("found obsolete 'max-w-[100px]' in rendered HTML")
	}
	if !strings.Contains(html, longNameUser) {
		t.Errorf("expected full long name %q in rendered HTML", longNameUser)
	}

	// Verify filter inputs
	if !strings.Contains(html, "x-model=\"filterName\"") {
		t.Errorf("expected filterName x-model input in rendered HTML")
	}
	if !strings.Contains(html, "x-model=\"filterSite\"") {
		t.Errorf("expected filterSite x-model select in rendered HTML")
	}
	if !strings.Contains(html, "isMemberVisibleByID(") {
		t.Errorf("expected isMemberVisibleByID binding on member row")
	}
	if !strings.Contains(html, "hasNoVisibleMembers") {
		t.Errorf("expected hasNoVisibleMembers fallback row")
	}

	// Verify unassigned site option is present for the team WITH unassigned members
	if !strings.Contains(html, "<option value=\"__none__\">⚠️ Sans affectation de site</option>") {
		t.Errorf("expected unassigned site option in rendered HTML for team with unassigned members")
	}

	// Split by team sections to verify the team with ALL assigned members does NOT have the unassigned option
	parts := strings.Split(html, "👥 Dev Team All Assigned")
	if len(parts) < 2 {
		t.Fatalf("expected '👥 Dev Team All Assigned' header in rendered HTML")
	}
	allAssignedSection := parts[1]
	// The section ends before any other team or end of template; take next 1000 chars around the select
	selectIdx := strings.Index(allAssignedSection, "x-model=\"filterSite\"")
	if selectIdx == -1 {
		t.Fatalf("could not find select in all-assigned team section")
	}
	selectSnippet := allAssignedSection[selectIdx : selectIdx+strings.Index(allAssignedSection[selectIdx:], "</select>")]
	if strings.Contains(selectSnippet, "value=\"__none__\"") {
		t.Errorf("expected unassigned site option to be ABSENT in team where all members have a site, but found it in select snippet: %s", selectSnippet)
	}
}
