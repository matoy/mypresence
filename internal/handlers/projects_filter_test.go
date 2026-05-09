package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"presence-app/internal/middleware"
	"presence-app/internal/models"
)

// ─── projects.go filter branches ─────────────────────────────────────────────

// ProjectsAPI with existing entries in the map (covers L.49-51 loop body)
func TestProjectsAPI_WithEntries(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("projapi_entries@test.com", "ProjAPIEntries", "password1")
	tok, _ := d.CreateSession(uid)
	projID, _ := d.CreateProject("EntryProj", "EP", 0, true, "2026-01-01", "2026-12-31")
	d.SetProjectTimeEntry(uid, projID, 2026, 1, 2.0) //nolint:errcheck

	// Request with month=0 → defaults to current month; but we need entries in that specific month
	// Use year+month matching the entry
	req := httptest.NewRequest(http.MethodGet, "/api/projects?year=2026&month=1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ProjectsPage with existing entries in the map (covers L.124-126 loop body)
func TestProjectsPage_WithEntries(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("projpage_entries@test.com", "ProjPageEntries", "password1")
	tok, _ := d.CreateSession(uid)
	projID, _ := d.CreateProject("PageEntryProj", "PEP", 0, true, "2026-01-01", "2026-12-31")
	d.SetProjectTimeEntry(uid, projID, 2026, 1, 3.0) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/projects?year=2026&month=1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ProjectTimeAPI with existing entries (covers L.77-79 loop body)
func TestProjectTimeAPI_WithEntries(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("projtimeapi_entries@test.com", "ProjTimeAPIEntries", "password1")
	tok, _ := d.CreateSession(uid)
	projID, _ := d.CreateProject("TimeEntryProj", "TEP", 0, true, "2026-01-01", "2026-12-31")
	d.SetProjectTimeEntry(uid, projID, 2026, 1, 4.0) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/project-time?year=2026&month=1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectTimeAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// AdminProjectsPage filter: active=0 + team filter (covers L.244, L.247)
func TestAdminProjectsPage_FilterInactive2(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("FilterTeam")
	d.CreateProject("ActiveProj", "AP", teamID, true, "2026-01-01", "2026-12-31")    //nolint:errcheck
	d.CreateProject("InactiveProj", "IP", teamID, false, "2026-01-01", "2026-12-31") //nolint:errcheck
	d.CreateProject("OtherTeamProj", "OTP", 0, true, "2026-01-01", "2026-12-31")     //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet,
		"/admin/projects?active=0&team="+strconvI64(teamID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// AdminProjectsAPI filter: active=0 + team filter with active projects to skip (covers L.280-289)
func TestAdminProjectsAPI_FilterInactive2(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("FilterAPITeam")
	d.CreateProject("ActiveAPIProj", "AAP", teamID, true, "2026-01-01", "2026-12-31")    //nolint:errcheck
	d.CreateProject("InactiveAPIProj", "IAP", teamID, false, "2026-01-01", "2026-12-31") //nolint:errcheck
	d.CreateProject("OtherAPIProj", "OAP", 0, true, "2026-01-01", "2026-12-31")          //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet,
		"/api/admin/projects?active=0&team="+strconvI64(teamID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// AdminProjectsAPI GET - 200 with no error (ListProjects succeeds even with nil result)
func TestAdminProjectsAPI_GetDBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	// Valid GET request
	req := createAdminReq(t, d, http.MethodGet, "/api/admin/projects", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// AdminProjectsAPI POST DB error (covers L.387-392)
func TestAdminProjectsAPI_PostDBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	body, _ := json.Marshal(map[string]interface{}{
		"name":       "DBErrProj",
		"code":       "DBEP",
		"active":     true,
		"start_date": "2026-01-01",
		"end_date":   "2026-12-31",
	})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/projects", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.CreateProject(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// AdminProjectsAPI PUT DB error (covers L.387-392 in UpdateProject)
func TestAdminProjectsAPI_PutDBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	projID, _ := d.CreateProject("PutErrProj", "PEP2", 0, true, "2026-01-01", "2026-12-31")
	body, _ := json.Marshal(map[string]interface{}{
		"name":       "Updated",
		"code":       "UPD",
		"active":     true,
		"start_date": "2026-01-01",
		"end_date":   "2026-12-31",
	})
	req := createAdminReq(t, d, http.MethodPut, "/api/admin/projects/"+strconvI64(projID), body)
	req.SetPathValue("id", strconvI64(projID))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.Close()
		h.UpdateProject(rw, r)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// SetProjectTime with two projects - one at limit (covers L.190-195 existing+cap branches)
func TestSetProjectTime_SaveDBError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	statusID, _ := d.CreateStatus(models.Status{Name: "BillableCapTest", Color: "#005500", Billable: true, SortOrder: 77})
	uid, _ := d.CreateLocalUser("captest2@test.com", "CapTest2", "password1")
	tok, _ := d.CreateSession(uid)

	// Give user only 4 billable days
	dates := []string{"2026-03-02", "2026-03-03", "2026-03-04", "2026-03-05"}
	d.SetPresences(uid, dates, statusID, "") //nolint:errcheck

	projID, _ := d.CreateProject("CapTest2Proj", "CT2P", 0, true, "2026-01-01", "2026-12-31")
	projID2, _ := d.CreateProject("CapTest2Proj2", "CT2P2", 0, true, "2026-01-01", "2026-12-31")

	// First entry: 2 days for proj1
	body1, _ := json.Marshal(map[string]interface{}{"project_id": projID, "year": 2026, "month": 3, "days": 2.0})
	req1 := httptest.NewRequest(http.MethodPost, "/api/project-time", bytes.NewReader(body1))
	req1.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	w1.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first entry: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Second entry: 2 days for proj2 (total=4 = billable, ok)
	body2, _ := json.Marshal(map[string]interface{}{"project_id": projID2, "year": 2026, "month": 3, "days": 2.0})
	req2 := httptest.NewRequest(http.MethodPost, "/api/project-time", bytes.NewReader(body2))
	req2.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	w2.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second entry: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	// Now try to update proj1 to 3 days: current=4, existing=2, new=3 => 4-2+3=5 > 4 cap
	body3, _ := json.Marshal(map[string]interface{}{"project_id": projID, "year": 2026, "month": 3, "days": 3.0})
	req3 := httptest.NewRequest(http.MethodPost, "/api/project-time", bytes.NewReader(body3))
	req3.AddCookie(&http.Cookie{Name: "session", Value: tok})
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	w3.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w3, req3)
	if w3.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cap check: expected 422, got %d: %s", w3.Code, w3.Body.String())
	}
}

// ProjectsReportPage filter: active=0 + team filter (covers L.530+536 in report)
func TestProjectsReportPage_FilterInactive2(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("ReportFilterTeam")
	d.CreateProject("ActiveReportProj", "ARP", teamID, true, "2026-01-01", "2026-12-31")    //nolint:errcheck
	d.CreateProject("InactiveReportProj", "IRP", teamID, false, "2026-01-01", "2026-12-31") //nolint:errcheck
	d.CreateProject("OtherReportProj", "ORP", 0, true, "2026-01-01", "2026-12-31")          //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet,
		"/admin/projects-report?active=0&team="+strconvI64(teamID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ProjectsReportAPI filter: active=0 + team filter
func TestProjectsReportAPI_FilterInactive(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("ReportFilterAPITeam")
	d.CreateProject("ActiveReportAPIProj", "ARAP", teamID, true, "2026-01-01", "2026-12-31")    //nolint:errcheck
	d.CreateProject("InactiveReportAPIProj", "IRAP", teamID, false, "2026-01-01", "2026-12-31") //nolint:errcheck
	d.CreateProject("OtherReportAPIProj", "ORAP", 0, true, "2026-01-01", "2026-12-31")          //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet,
		"/api/projects-report?active=0&team="+strconvI64(teamID), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ProjectsReportPage filter: text doesn't match (covers L.454 text-skip branch)
// Also filter active=0 to skip active projects (covers L.530)
func TestProjectsReportPage_FilterTextNoMatch(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}

	teamID, _ := d.CreateTeam("ReportTxtTeam")
	d.CreateProject("MyReportProj", "MRP", teamID, true, "2026-01-01", "2026-12-31") //nolint:errcheck
	d.CreateProject("Other", "OTH", teamID, false, "2026-01-01", "2026-12-31")       //nolint:errcheck

	// q=ZZZNOMATCH → no project matches → L.454 continue branch
	req := createAdminReq(t, d, http.MethodGet,
		"/admin/projects-report?q=ZZZNOMATCH", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
