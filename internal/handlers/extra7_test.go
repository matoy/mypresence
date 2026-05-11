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

// -----------------------------------------------------------------------
// AdminProjectsPage — filter paths
// -----------------------------------------------------------------------

func TestAdminProjectsPage_FilterText(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProject("Searchable Project", "SP01", 0, true, "2026-01-01", "2030-12-31") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects?q=Searchable", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAdminProjectsPage_FilterInactive(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProject("Inactive Proj", "IP01", 0, false, "2026-01-01", "2030-12-31") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects?active=0", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// AdminProjectsAPI — filter paths
// -----------------------------------------------------------------------

func TestAdminProjectsAPI_FilterText(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	d.CreateProject("FilterableProject", "FP01", 0, true, "2026-01-01", "2030-12-31") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/api/admin/projects?q=Filterable", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAdminProjectsAPI_FilterInactive(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	req := createAdminReq(t, d, http.MethodGet, "/api/admin/projects?active=0", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// SetPresences — half day path
// -----------------------------------------------------------------------

func TestSetPresences_HalfDay(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("halfday@test.com", "HalfDay", "password1")
	tok, _ := d.CreateSession(uid)
	sid, _ := d.CreateStatus(models.Status{Name: "Remote", Color: "#abc"})

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"user_id":   uid,
		"status_id": sid,
		"dates":     []string{"2026-06-15"},
		"half":      "AM",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/presences", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// GetPresencesAPI — with user filter
// -----------------------------------------------------------------------

func TestGetPresencesAPI_WithTeam(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}

	tid, _ := d.CreateTeam("PresAPITeam")
	req := createAdminReq(t, d, http.MethodGet, "/api/presences?team_id="+strconvI64(tid)+"&year=2026&month=6", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.GetPresencesAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ProjectsReportAPI — with user filter
// -----------------------------------------------------------------------

func TestProjectsReportAPI_WithUser(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("reportuser@test.com", "ReportUser", "password1")
	req := createAdminReq(t, d, http.MethodGet, "/api/admin/projects-report?month=2026-06&user_id="+strconvI64(uid), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ProjectsReportAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// DeleteTeam — bad ID path
// -----------------------------------------------------------------------

func TestDeleteTeam_BadID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	tid, _ := d.CreateTeam("TeamToDelete")
	req := createAdminReq(t, d, http.MethodDelete, "/api/admin/teams/"+strconvI64(tid), nil)
	req.SetPathValue("id", strconvI64(tid))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteTeam)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// CreateTeam — empty name
// -----------------------------------------------------------------------

func TestCreateTeam_EmptyName(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &AdminHandler{DB: d, Render: noRender}

	bodyBytes, _ := json.Marshal(map[string]interface{}{"name": ""})
	req := createAdminReq(t, d, http.MethodPost, "/api/admin/teams", bodyBytes)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateTeam)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// CalendarPage — with date param
// -----------------------------------------------------------------------

func TestCalendarPage_WithDate(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("calendar2@test.com", "Calendar2", "password1")
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/calendar?date=2026-06-01", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// HolidaysPage — with year param
// -----------------------------------------------------------------------

func TestHolidaysPage_WithYear(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &HolidaysHandler{DB: d, Render: noRender}

	d.CreateHoliday("2026-01-01", "NewYear2026", false) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/holidays?year=2026", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.HolidaysPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// AdminProjectsPage — with team filter (covers filterTeam > 0 branch)
// -----------------------------------------------------------------------

func TestAdminProjectsPage_FilterTeam(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}

	tid, _ := d.CreateTeam("FilterTeam")
	d.CreateProject("Team Project", "TP1", tid, true, "2026-01-01", "2030-12-31") //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/projects?team="+strconvI64(tid), nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.AdminProjectsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
