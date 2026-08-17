package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// CertifyProjectMonth handler
// -----------------------------------------------------------------------

func TestCertifyProjectMonth_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("projcert-badjson-%d@test.com", nextID()), "U", "password1")
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/certify-project", []byte("not-json"))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CertifyProjectMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCertifyProjectMonth_InvalidRange(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("projcert-range-%d@test.com", nextID()), "U", "password1")
	body, _ := json.Marshal(map[string]int{"year": 1999, "month": 6})
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/certify-project", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CertifyProjectMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range year, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCertifyProjectMonth_IncompleteDeclaration(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("projcert-incomplete-%d@test.com", nextID()), "U", "password1")

	body, _ := json.Marshal(map[string]int{"year": 2026, "month": 5})
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/certify-project", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CertifyProjectMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a declaration with no billable days, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCertifyProjectMonth_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("projcert-ok-%d@test.com", nextID()), "U", "password1")

	statusID, err := d.CreateStatus(models.Status{Name: "Billable", Color: "#22c55e", Billable: true, SortOrder: 1})
	if err != nil {
		t.Fatalf("CreateStatus: %v", err)
	}
	if err := d.SetPresences(uid, []string{"2026-05-04"}, statusID, "full"); err != nil {
		t.Fatalf("SetPresences: %v", err)
	}
	projID, err := d.CreateProject("Proj", "PRJ-1", 0, true, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := d.SetProjectTimeEntry(uid, projID, 2026, 5, 1); err != nil {
		t.Fatalf("SetProjectTimeEntry: %v", err)
	}

	body, _ := json.Marshal(map[string]int{"year": 2026, "month": 5})
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/certify-project", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CertifyProjectMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	certified, err := d.IsProjectMonthCertified(uid, 2026, 5)
	if err != nil {
		t.Fatalf("IsProjectMonthCertified: %v", err)
	}
	if !certified {
		t.Fatal("expected project declaration to be certified")
	}
}

// -----------------------------------------------------------------------
// DecertifyProjectMonth handler
// -----------------------------------------------------------------------

func TestDecertifyProjectMonth_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}
	req := createAdminReq(t, d, http.MethodPost, "/api/decertify-project", []byte("not-json"))
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DecertifyProjectMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDecertifyProjectMonth_Forbidden(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("projdecert-basic-%d@test.com", nextID()), "U", "password1")
	body, _ := json.Marshal(map[string]int64{"user_id": uid, "year": 2026, "month": 6})
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/decertify-project", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DecertifyProjectMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-privileged user, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDecertifyProjectMonth_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("projdecert-ok-%d@test.com", nextID()), "U", "password1")
	if err := d.CertifyProjectMonth(uid, 2026, 6, uid); err != nil {
		t.Fatalf("CertifyProjectMonth setup: %v", err)
	}

	body, _ := json.Marshal(map[string]int64{"user_id": uid, "year": 2026, "month": 6})
	req := createAdminReq(t, d, http.MethodPost, "/api/decertify-project", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.DecertifyProjectMonth)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	certified, err := d.IsProjectMonthCertified(uid, 2026, 6)
	if err != nil {
		t.Fatalf("IsProjectMonthCertified: %v", err)
	}
	if certified {
		t.Fatal("expected project declaration to be uncertified")
	}
}

// -----------------------------------------------------------------------
// Edit lock once certified
// -----------------------------------------------------------------------

func TestSetProjectTime_RejectedWhenCertified(t *testing.T) {
	d := newExtraTestDB(t)
	h := &ProjectsHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser(fmt.Sprintf("projlock-%d@test.com", nextID()), "U", "password1")
	projID, err := d.CreateProject("Proj", "PRJ-2", 0, true, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := d.CertifyProjectMonth(uid, 2026, 5, uid); err != nil {
		t.Fatalf("CertifyProjectMonth setup: %v", err)
	}

	body, _ := json.Marshal(map[string]interface{}{"project_id": projID, "year": 2026, "month": 5, "days": 1})
	req := newUserSessionReq(t, d, uid, http.MethodPost, "/api/project-time", body)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusLocked {
		t.Fatalf("expected 423 once the project declaration is certified, got %d: %s", w.Code, w.Body.String())
	}
}
