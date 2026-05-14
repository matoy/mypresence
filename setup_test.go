package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/models"
)

// ---------------------------------------------------------------------------
// buildTemplateFuncMap — pure helper functions
// ---------------------------------------------------------------------------

func funcMap(t *testing.T) template.FuncMap {
	t.Helper()
	return buildTemplateFuncMap(&config.Config{OnsiteRatioThreshold: 60})
}

func TestFuncMap_Add(t *testing.T) {
	add := funcMap(t)["add"].(func(int, int) int)
	if got := add(3, 4); got != 7 {
		t.Errorf("add(3,4) = %d, want 7", got)
	}
	if got := add(-1, 1); got != 0 {
		t.Errorf("add(-1,1) = %d, want 0", got)
	}
}

func TestFuncMap_Sub(t *testing.T) {
	sub := funcMap(t)["sub"].(func(int, int) int)
	if got := sub(10, 3); got != 7 {
		t.Errorf("sub(10,3) = %d, want 7", got)
	}
}

func TestFuncMap_Seq(t *testing.T) {
	seq := funcMap(t)["seq"].(func(int) []int)
	if got := seq(0); len(got) != 0 {
		t.Errorf("seq(0) = %v, want []", got)
	}
	s := seq(3)
	if len(s) != 3 || s[0] != 0 || s[1] != 1 || s[2] != 2 {
		t.Errorf("seq(3) = %v, want [0 1 2]", s)
	}
}

func TestFuncMap_Json(t *testing.T) {
	jsonFn := funcMap(t)["json"].(func(interface{}) template.JS)
	got := jsonFn(map[string]int{"a": 1})
	var m map[string]int
	if err := json.Unmarshal([]byte(got), &m); err != nil || m["a"] != 1 {
		t.Errorf("json funcmap: got %q, parse error or wrong value", got)
	}
}

func TestFuncMap_StatusColor(t *testing.T) {
	statusColor := funcMap(t)["statusColor"].(func([]models.Status, int64) string)
	statuses := []models.Status{{ID: 1, Color: "#ff0000"}, {ID: 2, Color: "#00ff00"}}
	if got := statusColor(statuses, 1); got != "#ff0000" {
		t.Errorf("statusColor(1) = %q, want #ff0000", got)
	}
	if got := statusColor(statuses, 99); got != "#e5e7eb" {
		t.Errorf("statusColor(missing) = %q, want #e5e7eb", got)
	}
}

func TestFuncMap_StatusName(t *testing.T) {
	statusName := funcMap(t)["statusName"].(func([]models.Status, int64) string)
	statuses := []models.Status{{ID: 1, Name: "Remote"}, {ID: 2, Name: "On-site"}}
	if got := statusName(statuses, 2); got != "On-site" {
		t.Errorf("statusName(2) = %q, want On-site", got)
	}
	if got := statusName(statuses, 99); got != "" {
		t.Errorf("statusName(missing) = %q, want empty", got)
	}
}

func TestFuncMap_HasKey(t *testing.T) {
	hasKey := funcMap(t)["hasKey"].(func(map[string]int64, string) bool)
	m := map[string]int64{"2026-01-01": 1}
	if !hasKey(m, "2026-01-01") {
		t.Error("hasKey: expected true for existing key")
	}
	if hasKey(m, "2026-01-02") {
		t.Error("hasKey: expected false for missing key")
	}
}

func TestFuncMap_GetKey(t *testing.T) {
	getKey := funcMap(t)["getKey"].(func(map[string]int64, string) int64)
	m := map[string]int64{"2026-01-01": 42}
	if got := getKey(m, "2026-01-01"); got != 42 {
		t.Errorf("getKey: got %d, want 42", got)
	}
	if got := getKey(m, "missing"); got != 0 {
		t.Errorf("getKey(missing): got %d, want 0", got)
	}
	if got := getKey(nil, "x"); got != 0 {
		t.Errorf("getKey(nil): got %d, want 0", got)
	}
}

func TestFuncMap_GetCount(t *testing.T) {
	getCount := funcMap(t)["getCount"].(func(map[int64]int, int64) int)
	m := map[int64]int{1: 5, 2: 0}
	if got := getCount(m, 1); got != 5 {
		t.Errorf("getCount(1) = %d, want 5", got)
	}
	if got := getCount(m, 99); got != 0 {
		t.Errorf("getCount(missing) = %d, want 0", got)
	}
}

func TestFuncMap_GetStrCount(t *testing.T) {
	getStrCount := funcMap(t)["getStrCount"].(func(map[string]int, string) int)
	m := map[string]int{"remote": 3}
	if got := getStrCount(m, "remote"); got != 3 {
		t.Errorf("getStrCount(remote) = %d, want 3", got)
	}
	if got := getStrCount(m, "missing"); got != 0 {
		t.Errorf("getStrCount(missing) = %d, want 0", got)
	}
}

func TestFuncMap_SumMap(t *testing.T) {
	sumMap := funcMap(t)["sumMap"].(func(map[int64]int) int)
	if got := sumMap(map[int64]int{1: 2, 2: 3}); got != 5 {
		t.Errorf("sumMap = %d, want 5", got)
	}
	if got := sumMap(nil); got != 0 {
		t.Errorf("sumMap(nil) = %d, want 0", got)
	}
}

func TestFuncMap_HasRole(t *testing.T) {
	hasRole := funcMap(t)["hasRole"].(func(*models.User, string) bool)

	u := &models.User{Roles: "team_manager"}
	if !hasRole(u, "team_manager") {
		t.Error("hasRole: expected true for team_manager")
	}
	if hasRole(u, "global") {
		t.Error("hasRole: expected false for global when not assigned")
	}
	if hasRole(nil, "global") {
		t.Error("hasRole(nil): expected false")
	}

	// global role implies any role
	admin := &models.User{Roles: "global"}
	if !hasRole(admin, "team_manager") {
		t.Error("hasRole: global user should pass any role check")
	}
}

func TestFuncMap_Percent(t *testing.T) {
	percent := funcMap(t)["percent"].(func(int, int) int)
	if got := percent(1, 4); got != 25 {
		t.Errorf("percent(1,4) = %d, want 25", got)
	}
	if got := percent(0, 0); got != 0 {
		t.Errorf("percent(0,0) = %d, want 0 (div-by-zero guard)", got)
	}
}

func TestFuncMap_IntToInt64(t *testing.T) {
	fn := funcMap(t)["intToInt64"].(func(int) int64)
	if got := fn(42); got != 42 {
		t.Errorf("intToInt64(42) = %d, want 42", got)
	}
}

func TestFuncMap_Upper(t *testing.T) {
	upper := funcMap(t)["upper"].(func(string) string)
	if got := upper("hello"); got != "HELLO" {
		t.Errorf("upper(hello) = %q, want HELLO", got)
	}
}

func TestFuncMap_Dict(t *testing.T) {
	dict := funcMap(t)["dict"].(func(...interface{}) map[string]interface{})
	d := dict("key", "value", "n", 42)
	if d["key"] != "value" || d["n"] != 42 {
		t.Errorf("dict: got %v", d)
	}
	// odd number of args — last value is silently dropped
	d2 := dict("only")
	if len(d2) != 0 {
		t.Errorf("dict with odd args: got %v", d2)
	}
}

// ---------------------------------------------------------------------------
// floorplanImgHandler
// ---------------------------------------------------------------------------

func TestFloorplanImgHandler_InvalidPrefix(t *testing.T) {
	dir := t.TempDir()
	h := floorplanImgHandler(dir)
	req := httptest.NewRequest(http.MethodGet, "/data/evil.png", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rw.Code)
	}
}

func TestFloorplanImgHandler_DisallowedExtension(t *testing.T) {
	dir := t.TempDir()
	h := floorplanImgHandler(dir)
	req := httptest.NewRequest(http.MethodGet, "/data/floorplan_x.exe", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rw.Code)
	}
}

func TestFloorplanImgHandler_ValidFile(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "floorplan_office.png")
	if err := os.WriteFile(imgPath, []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := floorplanImgHandler(dir)
	req := httptest.NewRequest(http.MethodGet, "/data/floorplan_office.png", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rw.Code)
	}
}

// ---------------------------------------------------------------------------
// dataFileHandler
// ---------------------------------------------------------------------------

func TestDataFileHandler_DisallowedFile(t *testing.T) {
	dir := t.TempDir()
	h := dataFileHandler(dir)
	req := httptest.NewRequest(http.MethodGet, "/data/../../etc/passwd", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rw.Code)
	}
}

func TestDataFileHandler_AllowedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := dataFileHandler(dir)
	req := httptest.NewRequest(http.MethodGet, "/data/logo.png", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rw.Code)
	}
}

// ---------------------------------------------------------------------------
// metricsHandler
// ---------------------------------------------------------------------------

func TestMetricsHandler_Disabled(t *testing.T) {
	h := metricsHandler("")
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusNotFound {
		t.Errorf("expected 404 when token empty, got %d", rw.Code)
	}
}

func TestMetricsHandler_WrongToken(t *testing.T) {
	h := metricsHandler("secret")
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong token, got %d", rw.Code)
	}
	if rw.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header on 401")
	}
}

func TestMetricsHandler_NoToken(t *testing.T) {
	h := metricsHandler("secret")
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when no token provided, got %d", rw.Code)
	}
}

func TestMetricsHandler_CorrectToken(t *testing.T) {
	h := metricsHandler("secret")
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	// Prometheus handler returns 200 with metrics output.
	if rw.Code != http.StatusOK {
		t.Errorf("expected 200 for correct token, got %d", rw.Code)
	}
}

// ---------------------------------------------------------------------------
// langSwitcherHandler
// ---------------------------------------------------------------------------

func TestLangSwitcherHandler_ValidLang(t *testing.T) {
	h := langSwitcherHandler("en")
	req := httptest.NewRequest(http.MethodPost, "/lang?lang=fr", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rw.Code)
	}
	var langCookie string
	for _, c := range rw.Result().Cookies() {
		if c.Name == "lang" {
			langCookie = c.Value
		}
	}
	if langCookie != "fr" {
		t.Errorf("expected lang cookie = fr, got %q", langCookie)
	}
}

func TestLangSwitcherHandler_InvalidLang(t *testing.T) {
	h := langSwitcherHandler("en")
	req := httptest.NewRequest(http.MethodPost, "/lang?lang=zz", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	var langCookie string
	for _, c := range rw.Result().Cookies() {
		if c.Name == "lang" {
			langCookie = c.Value
		}
	}
	if langCookie != "en" {
		t.Errorf("expected lang cookie = en (default), got %q", langCookie)
	}
}

func TestLangSwitcherHandler_RefererSameOrigin(t *testing.T) {
	h := langSwitcherHandler("en")
	req := httptest.NewRequest(http.MethodPost, "/lang?lang=de", nil)
	req.Header.Set("Referer", "http://example.com/calendar?year=2026")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	loc := rw.Header().Get("Location")
	if !strings.HasPrefix(loc, "/calendar") {
		t.Errorf("expected redirect to /calendar..., got %q", loc)
	}
}

func TestLangSwitcherHandler_RefererExternal(t *testing.T) {
	h := langSwitcherHandler("en")
	req := httptest.NewRequest(http.MethodPost, "/lang?lang=de", nil)
	// Referer with no path component starting with "/" → must fall back to "/"
	req.Header.Set("Referer", "not-a-url")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	loc := rw.Header().Get("Location")
	if loc != "/" {
		t.Errorf("expected redirect to /, got %q", loc)
	}
}
