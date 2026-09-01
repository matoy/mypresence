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
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/i18n"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

func TestSitesAdmin_SeatsCRUD(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	// 1. Create site with seats = 42
	createBody, _ := json.Marshal(map[string]interface{}{
		"name":         "Paris HQ",
		"country_code": "FR",
		"seats":        42,
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/sites", createBody)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateSite)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create site failed: code %d, body %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	siteID := int64(resp["id"].(float64))

	// Verify via GetSite
	site, err := d.GetSite(siteID)
	if err != nil {
		t.Fatalf("GetSite failed: %v", err)
	}
	if site.Seats != 42 {
		t.Errorf("expected seats=42, got %d", site.Seats)
	}

	// 2. Update site with seats = 80
	updateBody, _ := json.Marshal(map[string]interface{}{
		"name":         "Paris HQ Renamed",
		"country_code": "FR",
		"seats":        80,
	})
	req = createAdminReq(t, d, http.MethodPut, fmt.Sprintf("/api/admin/sites/%d", siteID), updateBody)
	req.SetPathValue("id", fmt.Sprintf("%d", siteID))
	w = httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateSite)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update site failed: code %d, body %s", w.Code, w.Body.String())
	}

	// Verify via ListSites
	sites, err := d.ListSites()
	if err != nil {
		t.Fatalf("ListSites failed: %v", err)
	}
	found := false
	for _, s := range sites {
		if s.ID == siteID {
			found = true
			if s.Seats != 80 {
				t.Errorf("expected updated seats=80, got %d", s.Seats)
			}
		}
	}
	if !found {
		t.Errorf("site %d not found in ListSites", siteID)
	}
}

func TestSitesReportPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	var rendered string
	var capturedData interface{}
	h := &FloorplanHandler{
		DB:      d,
		DataDir: t.TempDir(),
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			rendered = page
			capturedData = data
		},
	}

	// Create test site with 50 desks
	siteID, err := d.CreateSite(models.Site{
		Name:        "Lyon Tech",
		CountryCode: "FR",
		Seats:       50,
	})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	// Attach an active user to Lyon Tech
	uID, err := d.CreateLocalUser("dev@lyon.com", "Dev Lyon", "pass")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	_ = d.UpdateUserSite(uID, siteID)

	req := createAdminReq(t, d, http.MethodGet, "/admin/sites-report?year=2026&month=6", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SitesReportPage)).ServeHTTP(w, req)

	if rendered != "admin_sites_report" {
		t.Errorf("expected admin_sites_report, got %q", rendered)
	}
	pageData, ok := capturedData.(*SitesReportPageData)
	if !ok {
		t.Fatalf("expected *SitesReportPageData, got %T", capturedData)
	}
	if pageData.Year != 2026 || pageData.Month != 6 {
		t.Errorf("expected 2026/6, got %d/%d", pageData.Year, pageData.Month)
	}
	if pageData.TotalSeats < 50 {
		t.Errorf("expected total seats >= 50, got %d", pageData.TotalSeats)
	}
	if pageData.TotalAttachedPeople < 1 {
		t.Errorf("expected total attached people >= 1, got %d", pageData.TotalAttachedPeople)
	}
}

func TestSitesReportAPI_Calculations(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	// Create site with 10 desks
	siteID, err := d.CreateSite(models.Site{
		Name:        "Bordeaux Hub",
		CountryCode: "FR",
		Seats:       10,
	})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	// Create floorplan, seat, reservation
	fpID, _ := d.CreateFloorplanWithSite("Floor 1", siteID, 1)
	seatID, _ := d.CreateSeat(fpID, "A1", 50, 50)

	// User attached to Bordeaux Hub
	uID, _ := d.CreateLocalUser("user@bordeaux.com", "Bordeaux User", "pass")
	_ = d.UpdateUserSite(uID, siteID)

	// Status with on_site = 1
	statusID, _ := d.CreateStatus(models.Status{Name: "Office", Color: "#00ff00", Billable: true, OnSite: true, SortOrder: 1})

	// Add presence on a weekday (2026-06-02 is Tuesday)
	_ = d.SetPresences(uID, []string{"2026-06-02"}, statusID, "full")

	// Add reservation on that seat on 2026-06-02
	_ = d.ReserveSeat(seatID, uID, "2026-06-02", "full")

	// Call SitesReportAPI
	req := createAdminReq(t, d, http.MethodGet, fmt.Sprintf("/api/admin/sites-report?year=2026&month=6&site=%d", siteID), nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SitesReportAPI)).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var data SitesReportPageData
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}

	if data.TotalSeats != 10 {
		t.Errorf("expected 10 seats, got %d", data.TotalSeats)
	}
	if data.TotalAttachedPeople != 1 {
		t.Errorf("expected 1 attached person, got %d", data.TotalAttachedPeople)
	}
	if data.PeopleVsSeats != 0.1 {
		t.Errorf("expected 0.1 ratio, got %f", data.PeopleVsSeats)
	}
	if data.TotalOnSiteDays < 1.0 {
		t.Errorf("expected at least 1 onsite day, got %f", data.TotalOnSiteDays)
	}
	if data.TotalReservations < 1.0 {
		t.Errorf("expected at least 1 reservation, got %f", data.TotalReservations)
	}
}

func TestSitesReportTemplate_RendersHTML(t *testing.T) {
	// locate project root
	wd, _ := os.Getwd()
	for !strings.HasSuffix(wd, "mypresence") {
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	tplPath := filepath.Join(wd, "web/templates/admin_sites_report.html")
	tplBytes, err := os.ReadFile(tplPath)
	if err != nil {
		t.Fatalf("read admin_sites_report template: %v", err)
	}

	funcMap := template.FuncMap{
		"index": func(m map[string]string, key string) string {
			if m == nil {
				return key
			}
			if v, ok := m[key]; ok {
				return v
			}
			return key
		},
		"printf": fmt.Sprintf,
		"int": func(f float64) int {
			return int(f)
		},
		"fmtF": func(f float64) string {
			return fmt.Sprintf("%.1f", f)
		},
		"percentF": func(a, b float64) int {
			if b == 0 {
				return 0
			}
			return int(a * 100 / b)
		},
		"getStrCountF": func(m map[string]float64, key string) float64 {
			return m[key]
		},
		"gt": func(a, b int) bool {
			return a > b
		},
		"eq": func(a, b interface{}) bool {
			return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
		},
		"upper": strings.ToUpper,
		"slice": func(s string, i, j int) string {
			if i < len(s) && j <= len(s) {
				return s[i:j]
			}
			return s
		},
		"hasRole": func(u *models.User, r string) bool {
			return true
		},
		"json": func(v interface{}) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
	}

	tmpl, err := template.New("admin_sites_report.html").Funcs(funcMap).Parse(string(tplBytes))
	if err != nil {
		t.Fatalf("parse admin_sites_report: %v", err)
	}

	sampleSite := &models.Site{ID: 1, Name: "Paris HQ", CountryCode: "FR", Seats: 100}
	sampleData := map[string]interface{}{
		"Page": "admin_sites_report",
		"User": &models.User{Email: "admin@example.com", Roles: "floorplan_manager,global"},
		"T":    i18n.T("fr"),
		"Data": &SitesReportPageData{
			Year:                   2026,
			Month:                  6,
			PrevYear:               2026,
			PrevMonth:              5,
			NextYear:               2026,
			NextMonth:              7,
			Sites:                  []*models.Site{sampleSite},
			SelectedSiteID:         0,
			TotalSeats:             100,
			TotalAttachedPeople:    120,
			PeopleVsSeats:          1.2,
			TotalCapacity:          2100,
			TotalOnSiteDays:        1400,
			AvgOccupancyRate:       66.7,
			TotalReservations:      1250,
			AvgReservationRate:     59.5,
			AvgOnSitePerWorkingDay: 66.7,
			AvgResPerWorkingDay:    59.5,
			Days: []models.DayInfo{
				{Day: 1, Date: "2026-06-01", DayIndex: 1},
				{Day: 2, Date: "2026-06-02", DayIndex: 2},
			},
			Summaries: []models.SiteReportSummary{
				{
					Site:                  sampleSite,
					Seats:                 100,
					AttachedPeople:        120,
					PeopleVsSeats:         1.2,
					WorkingDays:           21,
					TotalOnSiteDays:       1400,
					AvgOnSitePerDay:       66.7,
					OccupancyRate:         66.7,
					TotalReservations:     1250,
					AvgReservationsPerDay: 59.5,
					ReservationRate:       59.5,
				},
			},
			DailyReports: []models.SiteDailyReport{
				{
					Site: sampleSite,
					DailyOnSite: map[string]float64{
						"2026-06-01": 70,
						"2026-06-02": 85,
					},
					DailyReservations: map[string]float64{
						"2026-06-01": 60,
						"2026-06-02": 80,
					},
					DailyOccupancy: map[string]float64{
						"2026-06-01": 70.0,
						"2026-06-02": 85.0,
					},
					DailyResRate: map[string]float64{
						"2026-06-01": 60.0,
						"2026-06-02": 80.0,
					},
					TotalOnSite:       1400,
					TotalReservations: 1250,
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "content", sampleData); err != nil {
		t.Fatalf("render template: %v", err)
	}

	renderedHTML := buf.String()
	if !strings.Contains(renderedHTML, "Paris HQ") {
		t.Errorf("expected 'Paris HQ' in rendered HTML")
	}
	if !strings.Contains(renderedHTML, "sites-summary-table") {
		t.Errorf("expected 'sites-summary-table' in rendered HTML")
	}
}

func TestSitesReport_Permissions(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	handler := middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.SitesReportPage)))

	// 1. Basic user without floorplan_manager role -> 403
	reqBasic := createAuthedReq(t, d, http.MethodGet, "/admin/sites-report", "basic@test.com", "Basic", "pass", "", nil)
	wBasic := httptest.NewRecorder()
	handler.ServeHTTP(wBasic, reqBasic)
	if wBasic.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for basic user, got %d", wBasic.Code)
	}

	// 2. User with floorplan_manager role -> 200
	reqFP := createAuthedReq(t, d, http.MethodGet, "/admin/sites-report", "fp@test.com", "FP Mgr", "pass", models.RoleFloorplanManager, nil)
	wFP := httptest.NewRecorder()
	handler.ServeHTTP(wFP, reqFP)
	if wFP.Code != http.StatusOK {
		t.Errorf("expected 200 OK for floorplan_manager, got %d", wFP.Code)
	}

	// 3. User with global role -> 200
	reqGlobal := createAuthedReq(t, d, http.MethodGet, "/admin/sites-report", "global@test.com", "Global Admin", "pass", models.RoleGlobal, nil)
	wGlobal := httptest.NewRecorder()
	handler.ServeHTTP(wGlobal, reqGlobal)
	if wGlobal.Code != http.StatusOK {
		t.Errorf("expected 200 OK for global admin, got %d", wGlobal.Code)
	}
}

func TestSitesReport_CorporateFilter(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}

	// 1 corporate site
	_, err := d.CreateSite(models.Site{
		Name:             "Corp Site Paris",
		CountryCode:      "FR",
		Seats:            30,
		NotCorporateSite: false,
	})
	if err != nil {
		t.Fatalf("create corp site: %v", err)
	}

	// 1 non-corporate site
	_, err = d.CreateSite(models.Site{
		Name:             "Coworking Lyon",
		CountryCode:      "FR",
		Seats:            10,
		NotCorporateSite: true,
	})
	if err != nil {
		t.Fatalf("create non-corp site: %v", err)
	}

	// Case 1: Default (no corp param) -> should be corporate only
	reqDef := httptest.NewRequest(http.MethodGet, "/api/admin/sites-report", nil)
	dataDef, err := h.buildSitesReportData(reqDef)
	if err != nil {
		t.Fatalf("buildSitesReportData default: %v", err)
	}
	if dataDef.CorpFilter != "corporate" {
		t.Errorf("expected default CorpFilter 'corporate', got '%s'", dataDef.CorpFilter)
	}
	if len(dataDef.Sites) != 1 || dataDef.Sites[0].Name != "Corp Site Paris" {
		t.Errorf("expected only corporate site, got %d sites", len(dataDef.Sites))
	}
	if dataDef.TotalSeats != 30 {
		t.Errorf("expected TotalSeats=30, got %d", dataDef.TotalSeats)
	}

	// Case 2: corp=non_corporate -> should be non-corporate only
	reqNonCorp := httptest.NewRequest(http.MethodGet, "/api/admin/sites-report?corp=non_corporate", nil)
	dataNonCorp, err := h.buildSitesReportData(reqNonCorp)
	if err != nil {
		t.Fatalf("buildSitesReportData non_corporate: %v", err)
	}
	if dataNonCorp.CorpFilter != "non_corporate" {
		t.Errorf("expected CorpFilter 'non_corporate', got '%s'", dataNonCorp.CorpFilter)
	}
	if len(dataNonCorp.Sites) != 1 || dataNonCorp.Sites[0].Name != "Coworking Lyon" {
		t.Errorf("expected only non-corporate site, got %d sites", len(dataNonCorp.Sites))
	}
	if dataNonCorp.TotalSeats != 10 {
		t.Errorf("expected TotalSeats=10, got %d", dataNonCorp.TotalSeats)
	}

	// Case 3: corp=all -> should include both sites
	reqAll := httptest.NewRequest(http.MethodGet, "/api/admin/sites-report?corp=all", nil)
	dataAll, err := h.buildSitesReportData(reqAll)
	if err != nil {
		t.Fatalf("buildSitesReportData all: %v", err)
	}
	if dataAll.CorpFilter != "all" {
		t.Errorf("expected CorpFilter 'all', got '%s'", dataAll.CorpFilter)
	}
	if len(dataAll.Sites) != 2 {
		t.Errorf("expected 2 sites for corp=all, got %d", len(dataAll.Sites))
	}
	if dataAll.TotalSeats != 40 {
		t.Errorf("expected TotalSeats=40, got %d", dataAll.TotalSeats)
	}
}

func TestSitesReport_TemplateIconsAndScrollbarFix(t *testing.T) {
	tmplContent, err := os.ReadFile(filepath.Join("..", "..", "web", "templates", "admin_sites_report.html"))
	if err != nil {
		t.Fatalf("read template: %v", err)
	}

	funcMap := template.FuncMap{
		"index": func(m map[string]string, k string) string {
			if v, ok := m[k]; ok {
				return v
			}
			return k
		},
		"printf": fmt.Sprintf,
		"fmtF": func(v float64) string {
			return fmt.Sprintf("%.1f", v)
		},
		"int": func(v float64) int {
			return int(v)
		},
		"getStrCountF": func(m map[string]float64, k string) float64 {
			return m[k]
		},
	}

	tmpl, err := template.New("admin_sites_report.html").Funcs(funcMap).Parse(string(tmplContent))
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	site1 := &models.Site{ID: 1, Name: "Paris HQ", CountryCode: "FR", NotCorporateSite: false}
	site2 := &models.Site{ID: 2, Name: "Lyon Coworking", CountryCode: "FR", NotCorporateSite: true}

	sampleData := map[string]interface{}{
		"T": i18n.T("fr"),
		"Data": &SitesReportPageData{
			Year:       2026,
			Month:      8,
			CorpFilter: "corporate",
			Sites:      []*models.Site{site1, site2},
			Summaries: []models.SiteReportSummary{
				{Site: site1},
				{Site: site2},
			},
			Days: []models.DayInfo{
				{Date: "2026-08-01", Day: 1, DayIndex: 6, IsWeekend: true},
			},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "content", sampleData); err != nil {
		t.Fatalf("render template: %v", err)
	}

	html := buf.String()

	// Verify icons
	if !strings.Contains(html, "🏬 Lyon Coworking") {
		t.Errorf("expected non-corporate icon 🏬 for Lyon Coworking in dropdown")
	}
	if !strings.Contains(html, "🏢 Paris HQ") {
		t.Errorf("expected corporate icon 🏢 for Paris HQ in dropdown")
	}

	// Verify corporate filter dropdown presence
	if !strings.Contains(html, "sites.filter_corporate") && !strings.Contains(html, ">Type<") && !strings.Contains(html, "Type") {
		t.Errorf("expected corporate filter label in template")
	}
	if !strings.Contains(html, `value="corporate" selected`) {
		t.Errorf("expected corporate option to be selected by default")
	}
	if !strings.Contains(html, `value="non_corporate"`) {
		t.Errorf("expected non_corporate option in dropdown")
	}

	// Verify Daily Breakdown table has no min-width:36px
	if strings.Contains(html, "min-width:36px") {
		t.Errorf("found unwanted min-width:36px causing horizontal scrollbar")
	}

	// Verify xlsx library script is loaded for Excel export
	if !strings.Contains(html, `/static/js/xlsx.full.min.js`) {
		t.Errorf("expected /static/js/xlsx.full.min.js to be included for Excel export")
	}
}
