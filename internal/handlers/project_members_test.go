package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// ─── GetProjectMembersAPI ─────────────────────────────────────────────────────

func TestGetProjectMembersAPI_EmptyProject(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("MembTest", "MBRT", 0, true, "2026-01-01", "2026-12-31")
	req := createAdminReq(t, d, http.MethodGet, "/api/admin/projects/"+strconvI64(pid)+"/members", nil)
	req.SetPathValue("id", strconvI64(pid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.GetProjectMembersAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string][]int64
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp["user_ids"]) != 0 {
		t.Fatalf("expected empty user_ids, got %v", resp["user_ids"])
	}
}

func TestGetProjectMembersAPI_InvalidID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/api/admin/projects/notanid/members", nil)
	req.SetPathValue("id", "notanid")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.GetProjectMembersAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ─── SetProjectMembersAPI ─────────────────────────────────────────────────────

func TestSetProjectMembersAPI_SetsMembers(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("SetMembTest", "SMBT", 0, true, "2026-01-01", "2026-12-31")
	uid, _ := d.CreateLocalUser("smbt1@test.com", "Smbt1", "password1")

	body, _ := json.Marshal(map[string]interface{}{"user_ids": []int64{uid}})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/projects/"+strconvI64(pid)+"/members", body)
	req.SetPathValue("id", strconvI64(pid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectMembersAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ids, _ := d.GetProjectMembers(pid)
	if len(ids) != 1 || ids[0] != uid {
		t.Fatalf("expected member %d, got %v", uid, ids)
	}
}

func TestSetProjectMembersAPI_InvalidID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	body, _ := json.Marshal(map[string]interface{}{"user_ids": []int64{}})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/projects/bad/members", body)
	req.SetPathValue("id", "bad")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectMembersAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSetProjectMembersAPI_InvalidBody(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("BadBodyMem", "BDBM", 0, true, "2026-01-01", "2026-12-31")
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/projects/"+strconvI64(pid)+"/members", []byte("not json"))
	req.SetPathValue("id", strconvI64(pid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectMembersAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ─── ProjectsPage / ProjectsAPI – assignment filtering ─────────────────────────

func TestProjectsPage_BasicUserOnlySeesAssignedProjects(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid1, _ := d.CreateProject("OpenP", "OPENP", 0, true, "2026-01-01", "2026-12-31")
	pid2, _ := d.CreateProject("RestP", "RSTP", 0, true, "2026-01-01", "2026-12-31")

	uid, _ := d.CreateLocalUser("basicfilt@test.com", "BasicFilt", "password1")
	tok, _ := d.CreateSession(uid)

	// Assign uid only to pid1; pid2 has members → assign another user
	other, _ := d.CreateLocalUser("other2@test.com", "Other2", "password1")
	d.SetProjectMembers(pid2, []int64{other}) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/projects?year=2026&month=5", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})

	var gotProjects []interface{}
	h.Render = func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		m := data.(map[string]interface{})
		ps, _ := m["Projects"].([]models.Project)
		for _, p := range ps {
			gotProjects = append(gotProjects, p.ID)
		}
	}

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsPage)).ServeHTTP(w, req)

	// uid should see pid1 (open project) but not pid2 (restricted to `other`)
	if len(gotProjects) != 1 {
		t.Fatalf("expected 1 project, got %d: %v", len(gotProjects), gotProjects)
	}
	if gotProjects[0] != pid1 {
		t.Fatalf("expected pid1 (%d), got %v", pid1, gotProjects[0])
	}
}

func TestProjectsAPI_AdminSeesAllProjects(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid2, _ := d.CreateProject("AdminSees", "ADMS", 0, true, "2026-01-01", "2026-12-31")

	other, _ := d.CreateLocalUser("admsother@test.com", "AdmsOther", "password1")
	d.SetProjectMembers(pid2, []int64{other}) //nolint:errcheck

	// Admin user (projects_manager role)
	req := createAdminReq(t, d, http.MethodGet, "/api/projects?year=2026&month=5", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	projects, _ := resp["projects"].([]interface{})
	if len(projects) < 1 {
		t.Fatalf("admin should see at least 1 project (restricted one), got %d", len(projects))
	}
}

// ─── SetProjectTime – assignment check ────────────────────────────────────────

func TestSetProjectTime_NotAssignedReturns403(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("ForbiddenP", "FRBDP", 0, true, "2026-01-01", "2026-12-31")
	// Assign another user, not the requester
	owner, _ := d.CreateLocalUser("frbdowner@test.com", "FrbdOwner", "password1")
	d.SetProjectMembers(pid, []int64{owner}) //nolint:errcheck

	basic, _ := d.CreateLocalUser("frbdbasic@test.com", "FrbdBasic", "password1")
	tok, _ := d.CreateSession(basic)

	body, _ := json.Marshal(map[string]interface{}{
		"project_id": pid, "year": 2026, "month": 5, "days": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/project-time", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetProjectTime_AssignedUserSucceeds(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	pid, _ := d.CreateProject("AllowedP", "ALWP", 0, true, "2026-01-01", "2026-12-31")
	uid, _ := d.CreateLocalUser("alwpuser@test.com", "AlwpUser", "password1")
	d.SetProjectMembers(pid, []int64{uid}) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	body, _ := json.Marshal(map[string]interface{}{
		"project_id": pid, "year": 2026, "month": 5, "days": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/project-time", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	// May return 422 (cap exceeded with 0 billable days) or 200 – but NOT 403.
	if w.Code == http.StatusForbidden {
		t.Fatalf("expected NOT 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetProjectTime_OpenProjectAnyUserSucceeds(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	// No members → open project
	pid, _ := d.CreateProject("OpenAllowP", "OALWP", 0, true, "2026-01-01", "2026-12-31")
	uid, _ := d.CreateLocalUser("oalwp@test.com", "Oalwp", "password1")
	tok, _ := d.CreateSession(uid)

	body, _ := json.Marshal(map[string]interface{}{
		"project_id": pid, "year": 2026, "month": 5, "days": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/project-time", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code == http.StatusForbidden {
		t.Fatalf("open project should not return 403, got %d: %s", w.Code, w.Body.String())
	}
}
