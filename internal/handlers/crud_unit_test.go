package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

func newCRUDTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func createAuthedReq(t *testing.T, d *db.DB, method, path, email, name, password, roles string, body []byte) *http.Request {
	t.Helper()
	uid, err := d.CreateLocalUser(email, name, password)
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	if roles != "" && roles != models.RoleBasic {
		if err := d.UpdateUserRoles(uid, roles); err != nil {
			t.Fatalf("UpdateUserRoles: %v", err)
		}
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	return req
}

func TestAdminCreateTeamAndListAPI(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	wBad := httptest.NewRecorder()
	h.CreateTeam(wBad, httptest.NewRequest(http.MethodPost, "/api/admin/teams", strings.NewReader("{")))
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad json, got %d", wBad.Code)
	}

	req := createAuthedReq(t, d, http.MethodPost, "/api/admin/teams", "tm@example.com", "TM", "password1", models.RoleTeamManager, []byte(`{"name":"Team X"}`))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateTeam)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 create team, got %d", w.Code)
	}

	wList := httptest.NewRecorder()
	h.ListTeamsAPI(wList, httptest.NewRequest(http.MethodGet, "/api/admin/teams", nil))
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 list teams, got %d", wList.Code)
	}
}

func TestAdminCreateStatusValidationAndSuccess(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	wBad := httptest.NewRecorder()
	h.CreateStatus(wBad, httptest.NewRequest(http.MethodPost, "/api/admin/statuses", strings.NewReader("{")))
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad json, got %d", wBad.Code)
	}

	req := createAuthedReq(t, d, http.MethodPost, "/api/admin/statuses", "sm@example.com", "SM", "password1", models.RoleStatusManager, []byte(`{"name":"Office","color":"#00ff00","billable":true,"on_site":true,"sort_order":1}`))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateStatus)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 create status, got %d", w.Code)
	}
}

func TestUsersAdminCreateUpdatePasswordAndDisable(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &UsersAdminHandler{DB: d}

	wBad := httptest.NewRecorder()
	h.CreateUser(wBad, httptest.NewRequest(http.MethodPost, "/api/admin/users", strings.NewReader("{")))
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 bad create user json, got %d", wBad.Code)
	}

	reqCreate := createAuthedReq(t, d, http.MethodPost, "/api/admin/users", "ga@example.com", "GA", "password1", models.RoleGlobal, []byte(`{"email":"new@example.com","name":"New","password":"password1"}`))
	wCreate := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateUser)).ServeHTTP(wCreate, reqCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("expected 200 create user, got %d", wCreate.Code)
	}

	u, err := d.GetUserByEmail("new@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}

	reqPwd := createAuthedReq(t, d, http.MethodPost, "/api/admin/users/"+strconvI64(u.ID)+"/password", "ga2@example.com", "GA2", "password1", models.RoleGlobal, []byte(`{"password":"short"}`))
	reqPwd.SetPathValue("id", strconvI64(u.ID))
	wPwd := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SetPassword)).ServeHTTP(wPwd, reqPwd)
	if wPwd.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 short password, got %d", wPwd.Code)
	}

	reqDisable := createAuthedReq(t, d, http.MethodPost, "/api/admin/users/"+strconvI64(u.ID)+"/disabled", "ga3@example.com", "GA3", "password1", models.RoleGlobal, []byte(`{"disabled":true}`))
	reqDisable.SetPathValue("id", strconvI64(u.ID))
	wDisable := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SetDisabled)).ServeHTTP(wDisable, reqDisable)
	if wDisable.Code != http.StatusOK {
		t.Fatalf("expected 200 set disabled, got %d", wDisable.Code)
	}
}

func TestCalendarAndProjectsValidationErrors(t *testing.T) {
	d := newCRUDTestDB(t)
	ch := &CalendarHandler{DB: d}
	ph := &ProjectsHandler{DB: d}

	w1 := httptest.NewRecorder()
	ch.GetPresencesAPI(w1, httptest.NewRequest(http.MethodGet, "/api/presences", nil))
	if w1.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 missing params, got %d", w1.Code)
	}

	reqP := createAuthedReq(t, d, http.MethodGet, "/api/projects?month=13", "proj@example.com", "Proj", "password1", models.RoleBasic, nil)
	w2 := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(ph.ProjectsAPI)).ServeHTTP(w2, reqP)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid month projects, got %d", w2.Code)
	}

	reqPT := createAuthedReq(t, d, http.MethodGet, "/api/project-time?month=13", "proj2@example.com", "Proj2", "password1", models.RoleBasic, nil)
	w3 := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(ph.ProjectTimeAPI)).ServeHTTP(w3, reqPT)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid month project-time, got %d", w3.Code)
	}

	reqSet := createAuthedReq(t, d, http.MethodPost, "/api/project-time", "proj3@example.com", "Proj3", "password1", models.RoleBasic, []byte(`{"project_id":0,"year":2026,"month":5,"days":1}`))
	w4 := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(ph.SetProjectTime)).ServeHTTP(w4, reqSet)
	if w4.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid set-project payload, got %d", w4.Code)
	}
}

func TestFloorplanAndProjectAdminValidation(t *testing.T) {
	d := newCRUDTestDB(t)
	fh := &FloorplanHandler{DB: d, DataDir: t.TempDir()}
	ph := &ProjectsHandler{DB: d}

	wSeats := httptest.NewRecorder()
	fh.AdminListSeats(wSeats, httptest.NewRequest(http.MethodGet, "/api/admin/seats", nil))
	if wSeats.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 missing floorplan_id, got %d", wSeats.Code)
	}

	wRes := httptest.NewRecorder()
	fh.ReserveSeat(wRes, httptest.NewRequest(http.MethodPost, "/api/reservations", strings.NewReader("{")))
	if wRes.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid reserve payload, got %d", wRes.Code)
	}

	reqCreate := createAuthedReq(t, d, http.MethodPost, "/api/admin/projects", "padm@example.com", "PAdm", "password1", models.RoleProjectsAdmin, []byte(`{"name":"","code":"X","start_date":"2026-01-01","end_date":"2026-12-31"}`))
	wCreate := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(ph.CreateProject)).ServeHTTP(wCreate, reqCreate)
	if wCreate.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 create project missing name, got %d", wCreate.Code)
	}
}

func TestUsersPageAndAdminProjectsAPI(t *testing.T) {
	d := newCRUDTestDB(t)
	uh := &UsersAdminHandler{DB: d}
	ph := &ProjectsHandler{DB: d}

	var renderedPage string
	uh.Render = func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		renderedPage = page
	}
	uid, err := d.CreateLocalUser("userspage@example.com", "Users Page", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	middleware.Auth(d, http.HandlerFunc(uh.UsersPage)).ServeHTTP(httptest.NewRecorder(), req)
	if renderedPage != "admin_users" {
		t.Fatalf("expected admin_users render, got %q", renderedPage)
	}

	if _, err := d.CreateProject("Proj API", "PAPI", 0, true, "2026-01-01", "2026-12-31"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	w := httptest.NewRecorder()
	ph.AdminProjectsAPI(w, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 admin projects api, got %d", w.Code)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
}

func TestDeleteStatus_FreeStatus_Returns200(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	// Create a status that has no linked presences.
	body := []byte(`{"name":"Unused","color":"#ff0000","sort_order":1}`)
	reqCreate := createAuthedReq(t, d, http.MethodPost, "/admin/statuses", "sm@test.com", "SM", "password1", models.RoleStatusManager, body)
	wCreate := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateStatus)).ServeHTTP(wCreate, reqCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("expected 200 create status, got %d", wCreate.Code)
	}
	var created map[string]interface{}
	json.Unmarshal(wCreate.Body.Bytes(), &created) //nolint:errcheck
	sid := int64(created["id"].(float64))

	// Delete — should succeed.
	reqDel := createAuthedReq(t, d, http.MethodDelete, "/admin/statuses/"+strconvI64(sid), "sm2@test.com", "SM2", "password1", models.RoleStatusManager, nil)
	reqDel.SetPathValue("id", strconvI64(sid))
	wDel := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DeleteStatus)).ServeHTTP(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("expected 200 delete free status, got %d: %s", wDel.Code, wDel.Body.String())
	}
}

func TestDeleteStatus_InUseReturns409WithMessage(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	// Create a user and a status, then attach a presence so the status is in use.
	uid, err := d.CreateLocalUser("del_user@test.com", "Del User", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	body := []byte(`{"name":"InUse","color":"#00ff00","sort_order":1}`)
	reqCreate := createAuthedReq(t, d, http.MethodPost, "/admin/statuses", "sm3@test.com", "SM3", "password1", models.RoleStatusManager, body)
	wCreate := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateStatus)).ServeHTTP(wCreate, reqCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status: got %d", wCreate.Code)
	}
	var created map[string]interface{}
	json.Unmarshal(wCreate.Body.Bytes(), &created) //nolint:errcheck
	sid := int64(created["id"].(float64))

	if err := d.SetPresences(uid, []string{"2026-06-02"}, sid, "full"); err != nil {
		t.Fatalf("SetPresences: %v", err)
	}

	// Delete — must return 409 with the sentinel error key.
	reqDel := createAuthedReq(t, d, http.MethodDelete, "/admin/statuses/"+strconvI64(sid), "sm4@test.com", "SM4", "password1", models.RoleStatusManager, nil)
	reqDel.SetPathValue("id", strconvI64(sid))
	wDel := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DeleteStatus)).ServeHTTP(wDel, reqDel)

	// HTTP status must be 409 Conflict.
	if wDel.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d: %s", wDel.Code, wDel.Body.String())
	}

	// Body must contain the i18n key so the front-end can resolve the message.
	var resp map[string]string
	if err := json.Unmarshal(wDel.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["error"] != "statuses.delete_in_use" {
		t.Errorf("expected error key \"statuses.delete_in_use\", got %q", resp["error"])
	}
}

func strconvI64(v int64) string {
	return strconv.FormatInt(v, 10)
}

func TestToggleStatusDisabled_DisableAndEnable(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	// Create a user and get a session token once.
	uid, err := d.CreateLocalUser("sm5@test.com", "SM5", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	if err := d.UpdateUserRoles(uid, models.RoleStatusManager); err != nil {
		t.Fatalf("UpdateUserRoles: %v", err)
	}
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Create a status via the handler.
	body := []byte(`{"name":"Togglable","color":"#aabbcc","sort_order":2}`)
	reqCreate := httptest.NewRequest(http.MethodPost, "/admin/statuses", bytes.NewReader(body))
	reqCreate.Header.Set("Content-Type", "application/json")
	reqCreate.AddCookie(&http.Cookie{Name: "session", Value: tok})
	wCreate := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateStatus)).ServeHTTP(wCreate, reqCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status: got %d", wCreate.Code)
	}
	var created map[string]interface{}
	json.Unmarshal(wCreate.Body.Bytes(), &created) //nolint:errcheck
	sid := int64(created["id"].(float64))

	doToggle := func(disabled bool) int {
		body := []byte(`{"disabled":` + func() string {
			if disabled {
				return "true"
			}
			return "false"
		}() + `}`)
		req := httptest.NewRequest(http.MethodPatch, "/admin/statuses/"+strconvI64(sid)+"/disabled", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: tok})
		req.SetPathValue("id", strconvI64(sid))
		w := httptest.NewRecorder()
		middleware.Auth(d, http.HandlerFunc(h.ToggleStatusDisabled)).ServeHTTP(w, req)
		return w.Code
	}

	// Disable → 200.
	if code := doToggle(true); code != http.StatusOK {
		t.Fatalf("expected 200 disabling status, got %d", code)
	}
	// Verify DB state.
	statuses, _ := d.ListStatuses()
	for _, s := range statuses {
		if s.ID == sid && !s.Disabled {
			t.Fatalf("status should be disabled in DB after PATCH disabled=true")
		}
	}

	// Re-enable → 200.
	if code := doToggle(false); code != http.StatusOK {
		t.Fatalf("expected 200 re-enabling status, got %d", code)
	}
	statuses, _ = d.ListStatuses()
	for _, s := range statuses {
		if s.ID == sid && s.Disabled {
			t.Fatalf("status should be active in DB after PATCH disabled=false")
		}
	}
}

func TestToggleStatusDisabled_InvalidBodyReturns400(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	req := createAuthedReq(t, d, http.MethodPatch, "/admin/statuses/1/disabled",
		"sm6@test.com", "SM6", "password1", models.RoleStatusManager, []byte("{bad json"))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ToggleStatusDisabled)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed JSON, got %d", w.Code)
	}
}

// ── StatusesPage ──────────────────────────────────────────────────────────────

func TestStatusesPage_Renders(t *testing.T) {
	d := newCRUDTestDB(t)
	var page string
	h := &AdminHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, p string, data interface{}) {
			page = p
		},
	}
	req := createAuthedReq(t, d, http.MethodGet, "/admin/statuses",
		"sm7@test.com", "SM7", "password1", models.RoleStatusManager, nil)
	middleware.Auth(d, http.HandlerFunc(h.StatusesPage)).ServeHTTP(httptest.NewRecorder(), req)
	if page != "admin_statuses" {
		t.Fatalf("expected admin_statuses page, got %q", page)
	}
}

// ── UsersAPI / UpdateUserRoles ────────────────────────────────────────────────

func TestUsersAPI_ReturnsJSON(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	// Seed a user so the list is non-empty.
	d.CreateLocalUser("list@test.com", "List", "password1") //nolint:errcheck

	w := httptest.NewRecorder()
	h.UsersAPI(w, httptest.NewRequest(http.MethodGet, "/api/admin/users", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var users []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatalf("UsersAPI: invalid JSON: %v", err)
	}
	if len(users) == 0 {
		t.Fatal("expected at least one user in response")
	}
}

func TestUpdateUserRoles_ValidAndInvalid(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	targetUID, err := d.CreateLocalUser("target@test.com", "Target", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}

	// Valid role update.
	reqOK := createAuthedReq(t, d, http.MethodPut, "/api/admin/users/"+strconvI64(targetUID)+"/roles",
		"global@test.com", "Global", "password1", models.RoleGlobal,
		[]byte(`{"roles":["team_manager"]}`))
	reqOK.SetPathValue("id", strconvI64(targetUID))
	wOK := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateUserRoles)).ServeHTTP(wOK, reqOK)
	if wOK.Code != http.StatusOK {
		t.Fatalf("expected 200 valid role update, got %d: %s", wOK.Code, wOK.Body.String())
	}

	// Invalid role name → 400.
	reqBad := createAuthedReq(t, d, http.MethodPut, "/api/admin/users/"+strconvI64(targetUID)+"/roles",
		"global2@test.com", "Global2", "password1", models.RoleGlobal,
		[]byte(`{"roles":["not_a_role"]}`))
	reqBad.SetPathValue("id", strconvI64(targetUID))
	wBad := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateUserRoles)).ServeHTTP(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid role, got %d", wBad.Code)
	}
}

// ── UserLogsPage ──────────────────────────────────────────────────────────────

func TestUserLogsPage_NotFoundForUnknownID(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &UsersAdminHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, p string, data interface{}) {}}

	req := createAuthedReq(t, d, http.MethodGet, "/admin/users/99999/logs",
		"ga@logstest.com", "GA", "password1", models.RoleGlobal, nil)
	req.SetPathValue("id", "99999")
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UserLogsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown user, got %d", w.Code)
	}
}

func TestUserLogsPage_RendersForKnownUser(t *testing.T) {
	d := newCRUDTestDB(t)
	var page string
	var pageData interface{}
	h := &UsersAdminHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, p string, data interface{}) {
			page = p
			pageData = data
		},
	}

	uid, err := d.CreateLocalUser("loguser@test.com", "Log User", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}

	req := createAuthedReq(t, d, http.MethodGet, "/admin/users/"+strconvI64(uid)+"/logs",
		"ga@logstest.com", "GA", "password1", models.RoleGlobal, nil)
	req.SetPathValue("id", strconvI64(uid))
	middleware.Auth(d, http.HandlerFunc(h.UserLogsPage)).ServeHTTP(httptest.NewRecorder(), req)
	if page != "admin_user_logs" {
		t.Fatalf("expected admin_user_logs page, got %q", page)
	}
	if pageData == nil {
		t.Fatal("expected non-nil page data")
	}
}

// ── SeatsAPI (floorplan) ──────────────────────────────────────────────────────

func TestSeatsAPI_MissingParamsReturns400(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir()}

	// No floorplan_id → 400.
	req := createAuthedReq(t, d, http.MethodGet, "/api/seats?date=2026-05-08",
		"fp@test.com", "FP", "password1", models.RoleBasic, nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SeatsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 missing floorplan_id, got %d", w.Code)
	}
}

func TestSeatsAPI_NotOnSiteReturnsEmptyList(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir()}

	// User exists but has no on-site presence → on_site=false result.
	req := createAuthedReq(t, d, http.MethodGet, "/api/seats?floorplan_id=1&date=2026-05-08",
		"fp2@test.com", "FP2", "password1", models.RoleBasic, nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SeatsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if onSite, _ := resp["on_site"].(bool); onSite {
		t.Fatal("expected on_site=false when user has no on-site presence")
	}
}

// ── ClearPresences forbidden branch ──────────────────────────────────────────

func TestClearPresences_ForbiddenForOtherUser(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &CalendarHandler{DB: d}

	// Create target user
	targetUID, err := d.CreateLocalUser("target@cal.com", "Target", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser target: %v", err)
	}

	// Request as basic user trying to clear another user's presences.
	body := []byte(`{"user_id":` + strconvI64(targetUID) + `,"dates":["2026-05-01"]}`)
	req := createAuthedReq(t, d, http.MethodPost, "/api/presences/clear",
		"basic@cal.com", "Basic", "password1", models.RoleBasic, body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.ClearPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unauthorized clear, got %d", w.Code)
	}
}

// ── SetPresences forbidden ────────────────────────────────────────────────────

func TestSetPresences_ForbiddenForOtherUser(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &CalendarHandler{DB: d}

	targetUID, err := d.CreateLocalUser("sp_target@cal.com", "SPTarget", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser target: %v", err)
	}

	body := []byte(`{"user_id":` + strconvI64(targetUID) + `,"dates":["2026-05-02"],"status_id":1,"half":"full"}`)
	req := createAuthedReq(t, d, http.MethodPost, "/api/presences",
		"sp_basic@cal.com", "SPBasic", "password1", models.RoleBasic, body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unauthorized set, got %d", w.Code)
	}
}

// ── UpdateProject ─────────────────────────────────────────────────────────────

func TestUpdateProject_ValidationAndSuccess(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &ProjectsHandler{DB: d}

	// Create a project to update.
	projID, err := d.CreateProject("Orig", "ORIG", 0, true, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Missing name → 400.
	reqBad := createAuthedReq(t, d, http.MethodPut, "/api/admin/projects/"+strconvI64(projID),
		"pa@proj.com", "PA", "password1", models.RoleProjectsAdmin,
		[]byte(`{"name":"","code":"ORIG","start_date":"2026-01-01","end_date":"2026-12-31"}`))
	reqBad.SetPathValue("id", strconvI64(projID))
	wBad := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateProject)).ServeHTTP(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 missing name, got %d", wBad.Code)
	}

	// Valid update → 200.
	reqOK := createAuthedReq(t, d, http.MethodPut, "/api/admin/projects/"+strconvI64(projID),
		"pa2@proj.com", "PA2", "password1", models.RoleProjectsAdmin,
		[]byte(`{"name":"Updated","code":"UPD","active":true,"start_date":"2026-01-01","end_date":"2026-12-31"}`))
	reqOK.SetPathValue("id", strconvI64(projID))
	wOK := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateProject)).ServeHTTP(wOK, reqOK)
	if wOK.Code != http.StatusOK {
		t.Fatalf("expected 200 valid update, got %d: %s", wOK.Code, wOK.Body.String())
	}
}

// ── SetProjectTime validation ─────────────────────────────────────────────────

func TestSetProjectTime_InvalidMonth_Returns400(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &ProjectsHandler{DB: d}

	body := []byte(`{"project_id":1,"year":2026,"month":13,"days":1}`)
	req := createAuthedReq(t, d, http.MethodPost, "/api/project-time",
		"pt@proj.com", "PT", "password1", models.RoleBasic, body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid month, got %d", w.Code)
	}
}

func TestSetProjectTime_InactiveProject_Returns400(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &ProjectsHandler{DB: d}

	projID, err := d.CreateProject("Inactive", "INACT", 0, false, "2025-01-01", "2025-12-31")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	body := []byte(`{"project_id":` + strconvI64(projID) + `,"year":2026,"month":5,"days":1}`)
	req := createAuthedReq(t, d, http.MethodPost, "/api/project-time",
		"pt2@proj.com", "PT2", "password1", models.RoleBasic, body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 inactive project, got %d: %s", w.Code, w.Body.String())
	}
}
