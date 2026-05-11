package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// helpers shared across this file
// -----------------------------------------------------------------------

func newExtraTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	d.SetBcryptCost(4)
	t.Cleanup(func() { d.Close() })
	return d
}

// createAdminReq creates an authenticated request with a global admin role.
func createAdminReq(t *testing.T, d *db.DB, method, path string, body []byte) *http.Request {
	t.Helper()
	return createAuthedReq(t, d, method, path, fmt.Sprintf("admin%d@test.com", nextID()), "Admin", "password1", models.RoleGlobal, body)
}

var _seq int

func nextID() int {
	_seq++
	return _seq
}

func noRender(w http.ResponseWriter, r *http.Request, page string, data interface{}) {}

// -----------------------------------------------------------------------
// AdminHandler — TeamsPage
// -----------------------------------------------------------------------

func TestAdminTeamsPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		if page != "admin_teams" {
			t.Errorf("expected admin_teams, got %q", page)
		}
	}}

	req := createAdminReq(t, d, http.MethodGet, "/admin/teams", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.TeamsPage)).ServeHTTP(w, req)
	// page render is injected — just verify no panic
}

// -----------------------------------------------------------------------
// AdminHandler — UpdateTeam
// -----------------------------------------------------------------------

func TestAdminUpdateTeam_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d}

	teamID, _ := d.CreateTeam("Original Team")

	body := []byte(`{"name":"Updated Team"}`)
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/teams/"+strconvI64(teamID), body)
	req.SetPathValue("id", strconvI64(teamID))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateTeam)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// AdminHandler — DeleteTeam
// -----------------------------------------------------------------------

func TestAdminDeleteTeam_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d}

	teamID, _ := d.CreateTeam("To Delete Team")
	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/teams/"+strconvI64(teamID), nil)
	req.SetPathValue("id", strconvI64(teamID))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DeleteTeam)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// AdminHandler — AddTeamMember / RemoveTeamMember
// -----------------------------------------------------------------------

func TestAdminAddAndRemoveTeamMember(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d}

	teamID, _ := d.CreateTeam("Member Team")
	uid, _ := d.CreateLocalUser("member@test.com", "Member", "password1")

	// Add member
	body, _ := json.Marshal(map[string]interface{}{"user_id": uid})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/teams/"+strconvI64(teamID)+"/members", body)
	req.SetPathValue("id", strconvI64(teamID))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.AddTeamMember)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AddTeamMember expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Remove member
	body2, _ := json.Marshal(map[string]interface{}{"user_id": uid})
	req2 := createAdminReq(t, d, http.MethodDelete, "/api/admin/teams/"+strconvI64(teamID)+"/members", body2)
	req2.SetPathValue("id", strconvI64(teamID))
	w2 := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.RemoveTeamMember)).ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("RemoveTeamMember expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

// -----------------------------------------------------------------------
// AdminHandler — UpdateStatus
// -----------------------------------------------------------------------

func TestAdminUpdateStatus_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &AdminHandler{DB: d}

	sid, _ := d.CreateStatus(models.Status{Name: "On site", Color: "#22c55e", Billable: true, OnSite: true, SortOrder: 1})

	body, _ := json.Marshal(map[string]interface{}{
		"name": "On site updated", "color": "#11bb44", "billable": true, "on_site": true, "sort_order": 1,
	})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/statuses/"+strconvI64(sid), body)
	req.SetPathValue("id", strconvI64(sid))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateStatus)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateStatus expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// HolidaysHandler
// -----------------------------------------------------------------------

func TestHolidaysPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	var rendered string
	h := &HolidaysHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}
	req := httptest.NewRequest(http.MethodGet, "/admin/holidays", nil)
	h.HolidaysPage(httptest.NewRecorder(), req)
	if rendered != "admin_holidays" {
		t.Errorf("expected admin_holidays, got %q", rendered)
	}
}

func TestHolidaysCreateUpdateDelete(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}

	// Create
	body, _ := json.Marshal(map[string]interface{}{"date": "2026-07-14", "name": "Bastille Day", "allow_imputed": false})
	wc := httptest.NewRecorder()
	wc.Body = new(bytes.Buffer)
	h.CreateHoliday(wc, httptest.NewRequest(http.MethodPost, "/api/admin/holidays", bytes.NewReader(body)))
	if wc.Code != http.StatusOK {
		t.Fatalf("CreateHoliday expected 200, got %d: %s", wc.Code, wc.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(wc.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	hid := int64(resp["id"].(float64))

	// Update
	body2, _ := json.Marshal(map[string]interface{}{"date": "2026-07-14", "name": "Fête nationale", "allow_imputed": true})
	wu := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPut, "/api/admin/holidays/"+strconvI64(hid), bytes.NewReader(body2))
	req2.SetPathValue("id", strconvI64(hid))
	h.UpdateHoliday(wu, req2)
	if wu.Code != http.StatusOK {
		t.Fatalf("UpdateHoliday expected 200, got %d", wu.Code)
	}

	// Delete
	wd := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodDelete, "/api/admin/holidays/"+strconvI64(hid), nil)
	req3.SetPathValue("id", strconvI64(hid))
	h.DeleteHoliday(wd, req3)
	if wd.Code != http.StatusOK {
		t.Fatalf("DeleteHoliday expected 200, got %d", wd.Code)
	}
}

func TestHolidaysCreate_ValidationErrors(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HolidaysHandler{DB: d, Render: noRender}

	// Bad JSON
	w := httptest.NewRecorder()
	h.CreateHoliday(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{")))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad json, got %d", w.Code)
	}

	// Missing name
	body, _ := json.Marshal(map[string]string{"date": "2026-01-01", "name": ""})
	w2 := httptest.NewRecorder()
	h.CreateHoliday(w2, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)))
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing name, got %d", w2.Code)
	}
}

// -----------------------------------------------------------------------
// UsersAdminHandler — UpdateUser / DeleteUser
// -----------------------------------------------------------------------

func TestAdminUpdateUser_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &UsersAdminHandler{DB: d}

	uid, _ := d.CreateLocalUser("update@test.com", "Update Me", "password1")
	body, _ := json.Marshal(map[string]string{"email": "updated@test.com", "name": "Updated Name"})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/users/"+strconvI64(uid), body)
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateUser)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateUser_ValidationError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &UsersAdminHandler{DB: d}

	uid, _ := d.CreateLocalUser("upv@test.com", "UpV", "password1")
	body, _ := json.Marshal(map[string]string{"email": "", "name": ""})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/users/"+strconvI64(uid), body)
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateUser)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAdminDeleteUser_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &UsersAdminHandler{DB: d}

	uid, _ := d.CreateLocalUser("delete@test.com", "Delete Me", "password1")
	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/users/"+strconvI64(uid), nil)
	req.SetPathValue("id", strconvI64(uid))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DeleteUser)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// AuthHandler — LocalLogin
// -----------------------------------------------------------------------

func TestLocalLogin_BadCredentials_RedirectsWithError(t *testing.T) {
	d := newExtraTestDB(t)
	_, _ = d.CreateLocalUser("logintest@test.com", "Login", "password1")

	h := &AuthHandler{
		DB:     d,
		Config: &config.Config{AdminUser: "admin@test.com", AdminPassword: "adminpass"},
		Render: noRender,
	}

	// Build a form POST request
	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader("username=logintest%40test.com&password=wrongpass"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.LocalLogin(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on bad credentials, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("expected error in redirect location, got %q", w.Header().Get("Location"))
	}
}

func TestLocalLogin_ValidCredentials_Redirects(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	_, _ = d.CreateLocalUser("validlogin@test.com", "Valid", "password1")

	h := &AuthHandler{
		DB:     d,
		Config: &config.Config{AdminUser: "admin@test.com", AdminPassword: "adminpass"},
		Render: noRender,
	}

	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader("username=validlogin%40test.com&password=password1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.LocalLogin(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on success, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/" {
		t.Errorf("expected redirect to /, got %q", loc)
	}
}

// -----------------------------------------------------------------------
// SettingsHandler — MyLogsPage
// -----------------------------------------------------------------------

func TestMyLogsPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	var rendered string
	h := &SettingsHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}

	uid, _ := d.CreateLocalUser("logs@test.com", "Logs User", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/settings/my-logs", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.MyLogsPage)).ServeHTTP(w, req)
	if rendered != "admin_user_logs" {
		t.Errorf("expected admin_user_logs, got %q", rendered)
	}
}

// -----------------------------------------------------------------------
// SettingsHandler — ChangePasswordPost
// -----------------------------------------------------------------------

func TestChangePasswordPost_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	var rendered string
	h := &SettingsHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}

	uid, _ := d.CreateLocalUser("chpwd@test.com", "ChPwd", "oldpassword")
	tok, _ := d.CreateSession(uid)

	body := strings.NewReader("current_password=oldpassword&new_password=newpassword1&confirm_password=newpassword1")
	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ChangePasswordPost)).ServeHTTP(w, req)

	// Success path: render with Done=true, or redirect
	_ = rendered // avoid unused var warning
	if w.Code != http.StatusOK && w.Code != http.StatusSeeOther {
		t.Fatalf("expected 200 or 303, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CalendarHandler — getDaysInMonth
// -----------------------------------------------------------------------

func TestGetDaysInMonth(t *testing.T) {
	days := getDaysInMonth(2026, 5)
	if len(days) != 31 {
		t.Errorf("expected 31 days in May 2026, got %d", len(days))
	}
	if days[0].Day != 1 {
		t.Errorf("first day should be 1, got %d", days[0].Day)
	}
	if days[30].Day != 31 {
		t.Errorf("last day should be 31, got %d", days[30].Day)
	}

	// Feb 2026 (non-leap)
	feb := getDaysInMonth(2026, 2)
	if len(feb) != 28 {
		t.Errorf("expected 28 days in Feb 2026, got %d", len(feb))
	}
}

// -----------------------------------------------------------------------
// PAT handlers
// -----------------------------------------------------------------------

func TestPATPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	var rendered string
	h := &PATHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}

	uid, _ := d.CreateLocalUser("patpage@test.com", "PATPage", "password1")
	tok, _ := d.CreateSession(uid)
	req := httptest.NewRequest(http.MethodGet, "/settings/tokens", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.PATPage)).ServeHTTP(w, req)
	if rendered != "pat" {
		t.Errorf("expected pat page, got %q", rendered)
	}
}

func TestRevokePAT_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("revpat@test.com", "RevPAT", "password1")
	_, pat, _ := d.CreatePAT(uid, "test", nil)
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodDelete, "/api/tokens/"+strconvI64(pat.ID), nil)
	req.SetPathValue("id", strconvI64(pat.ID))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.RevokePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevokePAT_BadID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("revpat2@test.com", "RevPAT2", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodDelete, "/api/tokens/abc", nil)
	req.SetPathValue("id", "abc")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.RevokePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAdminRevokePAT_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("arevpat@test.com", "ARevPAT", "password1")
	_, pat, _ := d.CreatePAT(uid, "test admin", nil)

	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/tokens/"+strconvI64(pat.ID), nil)
	req.SetPathValue("id", strconvI64(pat.ID))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.AdminRevokePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListPATs_AsAdmin(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("listpat@test.com", "ListPAT", "password1")
	_, _, _ = d.CreatePAT(uid, "test list", nil)

	req := createAdminReq(t, d, http.MethodGet, "/api/admin/tokens", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ListPATs)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ProjectsHandler — AdminProjectsAPI
// -----------------------------------------------------------------------

func TestAdminProjectsAPI_Pagination(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}

	req := createAdminReq(t, d, http.MethodGet, "/api/admin/projects?page=1", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProject_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}

	body, _ := json.Marshal(map[string]string{
		"name": "Test Project", "code": "TP01",
		"start_date": "2026-01-01", "end_date": "2026-12-31",
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/projects", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateProject)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateProject_ExtraValidationAndSuccess(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d}

	// Create a project first
	createBody, _ := json.Marshal(map[string]string{
		"name": "Proj Update", "code": "PU01",
		"start_date": "2026-01-01", "end_date": "2026-12-31",
	})
	reqCreate := createAdminReq(t, d, http.MethodPost, "/api/admin/projects", createBody)
	wCreate := httptest.NewRecorder()
	wCreate.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateProject)).ServeHTTP(wCreate, reqCreate)
	var cr map[string]interface{}
	if err := json.Unmarshal(wCreate.Body.Bytes(), &cr); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	pid := int64(cr["id"].(float64))

	// Update with missing name → 400
	badBody, _ := json.Marshal(map[string]string{"name": "", "code": "X"})
	reqBad := createAdminReq(t, d, http.MethodPut, "/api/admin/projects/"+strconvI64(pid), badBody)
	reqBad.SetPathValue("id", strconvI64(pid))
	wBad := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateProject)).ServeHTTP(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", wBad.Code)
	}

	// Valid update
	goodBody, _ := json.Marshal(map[string]string{
		"name": "Updated Project", "code": "PU02",
		"start_date": "2026-01-01", "end_date": "2026-12-31",
	})
	reqGood := createAdminReq(t, d, http.MethodPut, "/api/admin/projects/"+strconvI64(pid), goodBody)
	reqGood.SetPathValue("id", strconvI64(pid))
	wGood := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateProject)).ServeHTTP(wGood, reqGood)
	if wGood.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", wGood.Code, wGood.Body.String())
	}
}

// -----------------------------------------------------------------------
// HealthHandler (already at 100% but included for completeness)
// -----------------------------------------------------------------------

func TestHealth_OK(t *testing.T) {
	d := newExtraTestDB(t)
	h := &HealthHandler{DB: d}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.Health(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
