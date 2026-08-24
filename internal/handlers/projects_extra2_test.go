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
	"github.com/matoy/mypresence/internal/models"
)

func TestProjects_ExtraAPI(t *testing.T) {
	d := newCRUDTestDB(t)
	cfg := &config.Config{DisableProjects: false}
	h := &ProjectsHandler{DB: d, Config: cfg}

	adminID, _ := d.CreateLocalUser("proj_admin@example.com", "Proj Admin", "pass")
	_ = d.UpdateUserRoles(adminID, models.RoleGlobal)
	adminUser, _ := d.GetUserByID(adminID)

	pID, _ := d.CreateProject("Favorite Proj", "FAV", 0, true, "2026-01-01", "2026-12-31")

	// 1. ToggleProjectFavoriteAPI bad ID & success
	rec := httptest.NewRecorder()
	req := reqWithUser(d, adminUser, http.MethodPost, "/api/project-favorite/bad", nil)
	req.SetPathValue("id", "bad")
	h.ToggleProjectFavoriteAPI(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad ID ToggleProjectFavoriteAPI, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPost, "/api/project-favorite/"+strconv.FormatInt(pID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(pID, 10))
	h.ToggleProjectFavoriteAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on ToggleProjectFavoriteAPI, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. GetProjectMembersAPI bad ID & success
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodGet, "/api/admin/projects/bad/members", nil)
	req.SetPathValue("id", "bad")
	h.GetProjectMembersAPI(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad ID GetProjectMembersAPI, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodGet, "/api/admin/projects/"+strconv.FormatInt(pID, 10)+"/members", nil)
	req.SetPathValue("id", strconv.FormatInt(pID, 10))
	h.GetProjectMembersAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on GetProjectMembersAPI, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. SetProjectMembersAPI bad ID & bad JSON & success
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPut, "/api/admin/projects/bad/members", nil)
	req.SetPathValue("id", "bad")
	h.SetProjectMembersAPI(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad ID SetProjectMembersAPI, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPut, "/api/admin/projects/"+strconv.FormatInt(pID, 10)+"/members", strings.NewReader("bad-json"))
	req.SetPathValue("id", strconv.FormatInt(pID, 10))
	h.SetProjectMembersAPI(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad JSON SetProjectMembersAPI, got %d", rec.Code)
	}

	body, _ := json.Marshal(map[string]interface{}{"user_ids": []int64{adminID}})
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPut, "/api/admin/projects/"+strconv.FormatInt(pID, 10)+"/members", bytes.NewReader(body))
	req.SetPathValue("id", strconv.FormatInt(pID, 10))
	h.SetProjectMembersAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on SetProjectMembersAPI, got %d: %s", rec.Code, rec.Body.String())
	}
}
