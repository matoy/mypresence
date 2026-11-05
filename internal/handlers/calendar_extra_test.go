package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/models"
)

func TestCalendar_DecertifyMonth_AccessControl(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &CalendarHandler{DB: d}

	uID, _ := d.CreateLocalUser("member@example.com", "Member", "pass")
	memberUser, _ := d.GetUserByID(uID)

	tlID, _ := d.CreateLocalUser("leader@example.com", "Leader", "pass")
	tlUser, _ := d.GetUserByID(tlID)

	viewerID, _ := d.CreateLocalUser("viewer@example.com", "Viewer", "pass")
	_ = d.UpdateUserRoles(viewerID, models.RoleActivityViewer)
	viewerUser, _ := d.GetUserByID(viewerID)

	otherID, _ := d.CreateLocalUser("other@example.com", "Other", "pass")
	otherTeamID, _ := d.CreateTeamWithDetails("Other Team", "", false, false)
	_ = d.SetTeamLeaders(otherTeamID, []int64{otherID})
	otherUser, _ := d.GetUserByID(otherID)

	teamID, _ := d.CreateTeamWithDetails("Dev Team", "", false, false)
	_ = d.AddTeamMember(teamID, uID)
	_ = d.AddTeamMember(teamID, tlID)
	_ = d.SetTeamLeaders(teamID, []int64{tlID})

	_ = d.CertifyMonth(uID, 2026, 8, uID)

	// 1. Regular user trying to decertify -> 403
	body, _ := json.Marshal(map[string]interface{}{"user_id": uID, "year": 2026, "month": 8})
	rec := httptest.NewRecorder()
	req := reqWithUser(d, memberUser, http.MethodPost, "/api/decertify", bytes.NewReader(body))
	h.DecertifyMonth(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 on regular member decertifying, got %d", rec.Code)
	}

	// 2. Bad payload (invalid year/month/user) -> 400
	rec = httptest.NewRecorder()
	req = reqWithUser(d, tlUser, http.MethodPost, "/api/decertify", strings.NewReader("bad-json"))
	h.DecertifyMonth(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad json in decertify, got %d", rec.Code)
	}

	// 3. Team leader of DIFFERENT team trying to decertify -> 403
	rec = httptest.NewRecorder()
	req = reqWithUser(d, otherUser, http.MethodPost, "/api/decertify", bytes.NewReader(body))
	h.DecertifyMonth(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 on leader of different team, got %d", rec.Code)
	}

	// 4. Team leader of same team -> 200 OK
	rec = httptest.NewRecorder()
	req = reqWithUser(d, tlUser, http.MethodPost, "/api/decertify", bytes.NewReader(body))
	h.DecertifyMonth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on team leader decertify, got %d: %s", rec.Code, rec.Body.String())
	}

	// 5. Activity viewer -> 200 OK
	_ = d.CertifyMonth(uID, 2026, 8, uID)
	rec = httptest.NewRecorder()
	req = reqWithUser(d, viewerUser, http.MethodPost, "/api/decertify", bytes.NewReader(body))
	h.DecertifyMonth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on activity viewer decertify, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCalendarPage_YTDBillable(t *testing.T) {
	d := newCRUDTestDB(t)

	uID, _ := d.CreateLocalUser("ytd_user@example.com", "YTD User", "pass")
	u, _ := d.GetUserByID(uID)

	billableStatusID, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#3b82f6", Billable: true, OnSite: true, SortOrder: 1})
	nonBillableStatusID, _ := d.CreateStatus(models.Status{Name: "Holiday", Color: "#eab308", Billable: false, OnSite: false, SortOrder: 2})

	// Jan 2026: 2 full billable days = 2.0
	_ = d.SetPresences(uID, []string{"2026-01-15", "2026-01-16"}, billableStatusID, "full")

	// May 2026: 1 full billable (1.0), 1 AM billable (0.5), 1 PM non-billable (0.0) = 1.5
	_ = d.SetPresences(uID, []string{"2026-05-04"}, billableStatusID, "full")
	_ = d.SetPresences(uID, []string{"2026-05-05"}, billableStatusID, "AM")
	_ = d.SetPresences(uID, []string{"2026-05-05"}, nonBillableStatusID, "PM")

	// June 2026: 1 full billable (1.0)
	_ = d.SetPresences(uID, []string{"2026-06-10"}, billableStatusID, "full")

	var capturedData map[string]interface{}
	h := &CalendarHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			if m, ok := data.(map[string]interface{}); ok {
				capturedData = m
			}
		},
	}

	// 1. May 2026: should include Jan + May = 3.5 (June excluded)
	rec := httptest.NewRecorder()
	req := reqWithUser(d, u, http.MethodGet, "/?year=2026&month=5", nil)
	h.CalendarPage(rec, req)
	if capturedData == nil {
		t.Fatal("expected Render to be called")
	}
	ytdMay, ok := capturedData["YTDBillable"].(float64)
	if !ok {
		t.Fatalf("expected YTDBillable to be float64, got %T", capturedData["YTDBillable"])
	}
	if ytdMay != 3.5 {
		t.Errorf("expected 3.5 YTDBillable for May 2026, got %v", ytdMay)
	}

	// 2. Jan 2026: should only include Jan = 2.0
	rec = httptest.NewRecorder()
	req = reqWithUser(d, u, http.MethodGet, "/?year=2026&month=1", nil)
	h.CalendarPage(rec, req)
	ytdJan := capturedData["YTDBillable"].(float64)
	if ytdJan != 2.0 {
		t.Errorf("expected 2.0 YTDBillable for Jan 2026, got %v", ytdJan)
	}

	// 3. June 2026: should include Jan + May + June = 4.5
	rec = httptest.NewRecorder()
	req = reqWithUser(d, u, http.MethodGet, "/?year=2026&month=6", nil)
	h.CalendarPage(rec, req)
	ytdJune := capturedData["YTDBillable"].(float64)
	if ytdJune != 4.5 {
		t.Errorf("expected 4.5 YTDBillable for June 2026, got %v", ytdJune)
	}
}

func TestCalendarTemplate_YTDBillableTile(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	calPath := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../web/templates/calendar.html"))
	calBytes, err := os.ReadFile(calPath)
	if err != nil {
		t.Fatalf("read calendar template: %v", err)
	}

	funcMap := template.FuncMap{
		"json": func(v interface{}) template.JS { return "" },
		"statusColor": func(statuses []models.Status, id int64) string { return "#3b82f6" },
		"statusName": func(statuses []models.Status, id int64) string { return "Office" },
		"presenceHalf": func(m map[string]map[string]int64, date, half string) int64 { return 0 },
		"presenceOverride": func(m map[string]models.PresenceOverride, date string) *models.PresenceOverride { return nil },
		"fmtF": func(f float64) string {
			if f == float64(int64(f)) {
				return fmt.Sprintf("%d", int64(f))
			}
			return fmt.Sprintf("%.1f", f)
		},
	}

	tmpl, err := template.New("calendar.html").Funcs(funcMap).Parse(string(calBytes))
	if err != nil {
		t.Fatalf("parse calendar template: %v", err)
	}

	data := map[string]interface{}{
		"Data": map[string]interface{}{
			"Year":           2026,
			"Month":          5,
			"DeclarableDays": 21,
			"DeclaredDays":   18,
			"YTDBillable":    142.5,
			"CurrentUserID":  int64(1),
			"TeamViews":      []teamCalendarView{},
		},
		"User": &models.User{ID: 1, Name: "Alice"},
		"T": map[string]string{
			"cal.working_days": "Jours ouvrés",
			"cal.declared":     "Déclaré",
			"cal.ytd_billable": "Travaillés YTD",
			"cal.progress":     "Progression",
		},
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "content", data); err != nil {
		t.Fatalf("execute calendar template: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, "Travaillés YTD") {
		t.Errorf("expected HTML to contain 'Travaillés YTD', got: %s", html)
	}
	if !strings.Contains(html, "142.5") {
		t.Errorf("expected HTML to contain '142.5', got: %s", html)
	}
	if !strings.Contains(html, "text-orange-600") {
		t.Errorf("expected HTML to contain 'text-orange-600', got: %s", html)
	}
}
