package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"presence-app/internal/config"
	"presence-app/internal/db"
	"presence-app/internal/middleware"
	"presence-app/internal/models"
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

func strconvI64(v int64) string {
	return strconv.FormatInt(v, 10)
}
