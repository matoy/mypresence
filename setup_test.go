package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/models"
	"github.com/matoy/mypresence/internal/testhelper"
)

func TestSafeNewsContent(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: "Hello world",
			want:  "Hello world",
		},
		{
			input: "Check [Google](https://google.com) now",
			want:  `Check <a href="https://google.com" target="_blank" rel="noopener noreferrer" class="underline">Google</a> now`,
		},
		{
			input: "Visit [HTTP](http://example.com/test?a=1&b=2)",
			want:  `Visit <a href="http://example.com/test?a=1&amp;amp;b=2" target="_blank" rel="noopener noreferrer" class="underline">HTTP</a>`,
		},
		{
			input: "Unsafe [click](javascript:alert(1)) link",
			want:  "Unsafe [click](javascript:alert(1)) link",
		},
		{
			input: "<script>alert('xss')</script>",
			want:  "&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;",
		},
	}

	for _, c := range cases {
		got := string(safeNewsContent(c.input))
		if got != c.want {
			t.Errorf("safeNewsContent(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestFloorplanImgHandler(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tempDir, "floorplan_map.png"), []byte("png-data"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "floorplan_bad.exe"), []byte("bad-data"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "other.png"), []byte("other-data"), 0644)

	handler := floorplanImgHandler(tempDir)

	// Valid floorplan image
	req := httptest.NewRequest(http.MethodGet, "/floorplan-img/floorplan_map.png", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for valid floorplan image, got %d", rec.Code)
	}

	// Not starting with floorplan_
	req = httptest.NewRequest(http.MethodGet, "/floorplan-img/other.png", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-floorplan image, got %d", rec.Code)
	}

	// Bad extension
	req = httptest.NewRequest(http.MethodGet, "/floorplan-img/floorplan_bad.exe", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for disallowed extension, got %d", rec.Code)
	}

	// Missing file
	req = httptest.NewRequest(http.MethodGet, "/floorplan-img/floorplan_missing.png", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing floorplan image, got %d", rec.Code)
	}
}

func TestDataFileHandler(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tempDir, "logo.png"), []byte("png-logo"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "logo.svg"), []byte("<svg></svg>"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "logo.jpg"), []byte("jpg-logo"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "secret.txt"), []byte("secret"), 0644)

	handler := dataFileHandler(tempDir)

	for _, allowed := range []string{"logo.png", "logo.svg", "logo.jpg"} {
		req := httptest.NewRequest(http.MethodGet, "/data/"+allowed, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for allowed data file %s, got %d", allowed, rec.Code)
		}
	}

	// Disallowed file
	req := httptest.NewRequest(http.MethodGet, "/data/secret.txt", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for disallowed data file, got %d", rec.Code)
	}
}

func TestMetricsHandler(t *testing.T) {
	// Disabled (empty token)
	hDisabled := metricsHandler("")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	hDisabled.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when metrics disabled, got %d", rec.Code)
	}

	// Enabled with token
	hEnabled := metricsHandler("secret-metrics-tok")

	// Unauthorized
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	hEnabled.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on missing token, got %d", rec.Code)
	}

	// Wrong token
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	hEnabled.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on wrong token, got %d", rec.Code)
	}

	// Correct token
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer secret-metrics-tok")
	hEnabled.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on valid metrics token, got %d", rec.Code)
	}
}

func TestLangSwitcherHandler(t *testing.T) {
	handler := langSwitcherHandler("en")

	// 1. Valid language with relative referer
	form := url.Values{"lang": {"fr"}}
	req := httptest.NewRequest(http.MethodPost, "/set-lang", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "/calendar?month=2026-08")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/calendar?month=2026-08" {
		t.Errorf("expected redirect to /calendar?month=2026-08, got %q", loc)
	}
	var setCookie string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "lang" {
			setCookie = c.Value
		}
	}
	if setCookie != "fr" {
		t.Errorf("expected lang cookie 'fr', got %q", setCookie)
	}

	// 2. Invalid language and no referer
	form = url.Values{"lang": {"invalid-lang"}}
	req = httptest.NewRequest(http.MethodPost, "/set-lang", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("expected redirect to '/', got %q", loc)
	}
	setCookie = ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == "lang" {
			setCookie = c.Value
		}
	}
	if setCookie != "en" {
		t.Errorf("expected default lang 'en', got %q", setCookie)
	}
}

func TestBuildTemplateFuncMap_And_LoadTemplates(t *testing.T) {
	cfg := &config.Config{
		AppName:              "TestApp",
		OnsiteRatioThreshold: 50.0,
	}
	funcMap := buildTemplateFuncMap(cfg)

	// Test add / sub
	if addFn := funcMap["add"].(func(int, int) int); addFn(3, 4) != 7 {
		t.Errorf("add(3, 4) = %d, want 7", addFn(3, 4))
	}
	if subFn := funcMap["sub"].(func(int, int) int); subFn(10, 4) != 6 {
		t.Errorf("sub(10, 4) = %d, want 6", subFn(10, 4))
	}

	// Test safehtml
	safeHtmlFn := funcMap["safehtml"].(func(string) template.HTML)
	if safeHtmlFn("<b>bold</b>") != template.HTML("<b>bold</b>") {
		t.Errorf("safehtml failed")
	}

	// Test seq
	seqFn := funcMap["seq"].(func(int) []int)
	seq := seqFn(3)
	if len(seq) != 3 || seq[0] != 0 || seq[1] != 1 || seq[2] != 2 {
		t.Errorf("seq(3) failed: %v", seq)
	}

	// Test json
	jsonFn := funcMap["json"].(func(interface{}) template.JS)
	if string(jsonFn(map[string]string{"k": "v"})) != `{"k":"v"}` {
		t.Errorf("json func failed")
	}

	// Test status helpers
	statuses := []models.Status{
		{ID: 1, Name: "Office", Color: "#10b981", Billable: true},
		{ID: 2, Name: "Remote", Color: "#3b82f6", Billable: false},
	}
	statusColorFn := funcMap["statusColor"].(func([]models.Status, int64) string)
	if statusColorFn(statuses, 1) != "#10b981" || statusColorFn(statuses, 99) != "#e5e7eb" {
		t.Errorf("statusColor failed")
	}
	statusNameFn := funcMap["statusName"].(func([]models.Status, int64) string)
	if statusNameFn(statuses, 1) != "Office" || statusNameFn(statuses, 99) != "" {
		t.Errorf("statusName failed")
	}
	statusBillableFn := funcMap["statusBillable"].(func([]models.Status, int64) bool)
	if !statusBillableFn(statuses, 1) || statusBillableFn(statuses, 2) || statusBillableFn(statuses, 99) {
		t.Errorf("statusBillable failed")
	}

	// Test map helpers
	mStrInt64 := map[string]int64{"a": 10, "b": 20}
	hasKeyFn := funcMap["hasKey"].(func(map[string]int64, string) bool)
	if !hasKeyFn(mStrInt64, "a") || hasKeyFn(mStrInt64, "c") {
		t.Errorf("hasKey failed")
	}
	getKeyFn := funcMap["getKey"].(func(map[string]int64, string) int64)
	if getKeyFn(mStrInt64, "b") != 20 || getKeyFn(nil, "b") != 0 {
		t.Errorf("getKey failed")
	}

	mInt64Int := map[int64]int{1: 5, 2: 7}
	getCountFn := funcMap["getCount"].(func(map[int64]int, int64) int)
	if getCountFn(mInt64Int, 1) != 5 {
		t.Errorf("getCount failed")
	}
	getStrCountFn := funcMap["getStrCount"].(func(map[string]int, string) int)
	if getStrCountFn(map[string]int{"x": 8}, "x") != 8 {
		t.Errorf("getStrCount failed")
	}
	sumMapFn := funcMap["sumMap"].(func(map[int64]int) int)
	if sumMapFn(mInt64Int) != 12 {
		t.Errorf("sumMap failed")
	}

	// Test activityRocket
	activityRocketFn := funcMap["activityRocket"].(func(float64, float64, float64, float64) bool)
	if !activityRocketFn(0, 10, 10, 100.0) {
		t.Errorf("activityRocket should be true when perfect")
	}

	// Test dict, intToInt64, upper, hasRole
	dictFn := funcMap["dict"].(func(...interface{}) map[string]interface{})
	d := dictFn("key1", "val1", "key2", 42)
	if d["key1"] != "val1" || d["key2"] != 42 {
		t.Errorf("dict failed: %+v", d)
	}

	intToInt64Fn := funcMap["intToInt64"].(func(int) int64)
	if intToInt64Fn(123) != 123 {
		t.Errorf("intToInt64 failed")
	}

	upperFn := funcMap["upper"].(func(string) string)
	if upperFn("hello") != "HELLO" {
		t.Errorf("upper failed")
	}

	hasRoleFn := funcMap["hasRole"].(func(*models.User, string) bool)
	if hasRoleFn(nil, models.RoleGlobal) {
		t.Errorf("hasRole(nil) should be false")
	}
	u := &models.User{Roles: models.RoleTeamLeader}
	if !hasRoleFn(u, models.RoleTeamLeader) || hasRoleFn(u, models.RoleStatusManager) {
		t.Errorf("hasRole failed for user with role")
	}

	// Test loadTemplates
	templates := loadTemplates(funcMap)
	if len(templates) == 0 {
		t.Fatalf("expected loaded templates, got 0")
	}
	if _, ok := templates["login"]; !ok {
		t.Errorf("expected 'login' template")
	}
	if _, ok := templates["calendar"]; !ok {
		t.Errorf("expected 'calendar' template")
	}
}

func TestNewRenderPage(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tempDir, "logo.png"), []byte("logo-bytes"), 0644)

	cfg := &config.Config{
		AppName:                    "PresenceTest",
		DataDir:                    tempDir,
		DBDriver:                   "sqlite",
		SecretKey:                  "01234567890123456789012345678901",
		LogoPath:                   "logo.png",
		TeamCalendarRefreshMinutes: 5,
	}

	database, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	uID, _ := database.CreateLocalUser("admin@example.com", "Admin User", "pass")
	_ = database.UpdateUserRoles(uID, models.RoleGlobal)
	user, _ := database.GetUserByID(uID)

	teamID, _ := database.CreateTeamWithDetails("Manual Team", "", true)
	_ = database.AddTeamMember(teamID, uID)

	domID, _ := database.CreateDomain("Tech")
	_ = database.SetDomainManagers(domID, []int64{uID})

	_, _ = database.CreateNewsMessage("System alert", "Maintenance tonight", "2026-08-01", "2026-08-30", "#dc2626", false)

	funcMap := buildTemplateFuncMap(cfg)
	templates := loadTemplates(funcMap)
	renderPage := newRenderPage(cfg, database, templates)

	// 1. Render without user (public page e.g. login)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	renderPage(rec, req, "login", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 rendering login page, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PresenceTest") {
		t.Errorf("expected rendered page to contain app name")
	}

	// 2. Render with user, session cookie, and impersonation real_session cookie
	sessionTok, _ := database.CreateSession(uID)
	realSessionTok, _ := database.CreateSession(uID)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/calendar", nil)
	req = testhelper.WithUserInContext(req, user)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionTok})
	req.AddCookie(&http.Cookie{Name: "real_session", Value: realSessionTok})

	renderPage(rec, req, "calendar", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 rendering calendar with user, got %d", rec.Code)
	}

	// 3. Render unknown template -> 500
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/unknown", nil)
	renderPage(rec, req, "non_existent_page", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for missing template, got %d", rec.Code)
	}
}
