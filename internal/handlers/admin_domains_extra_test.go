package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/models"
)

func TestAdminDomains_DomainsPage_Render(t *testing.T) {
	d := newCRUDTestDB(t)

	adminID, _ := d.CreateLocalUser("admin@example.com", "Admin", "password")
	_ = d.UpdateUserRoles(adminID, models.RoleGlobal)
	adminUser, _ := d.GetUserByID(adminID)

	domID, _ := d.CreateDomain("Digital")
	_ = d.SetDomainManagers(domID, []int64{adminID})
	tID, _ := d.CreateTeamWithDetails("Digital Team", "", false)
	_ = d.UpdateTeamDomain(tID, domID)

	renderedPage := ""
	var renderedData interface{}
	renderMock := func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		renderedPage = page
		renderedData = data
	}

	h := &AdminHandler{
		DB:     d,
		Render: renderMock,
	}

	rec := httptest.NewRecorder()
	req := reqWithUser(d, adminUser, http.MethodGet, "/admin/domains", nil)
	h.DomainsPage(rec, req)

	if renderedPage != "admin_domains" {
		t.Fatalf("expected renderedPage 'admin_domains', got %q", renderedPage)
	}

	m, ok := renderedData.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{} data, got %+v", renderedData)
	}
	domainsList, ok := m["Domains"].([]DomainWithDetails)
	if !ok || len(domainsList) != 1 {
		t.Fatalf("expected 1 DomainWithDetails, got %+v", m["Domains"])
	}
	if domainsList[0].Domain.Name != "Digital" || len(domainsList[0].Managers) != 1 || len(domainsList[0].Teams) != 1 {
		t.Errorf("unexpected DomainWithDetails: %+v", domainsList[0])
	}
}

func TestAdminDomains_AuditLogs_And_Errors(t *testing.T) {
	d := newCRUDTestDB(t)

	adminID, _ := d.CreateLocalUser("admin_audit@example.com", "Admin Audit", "password")
	_ = d.UpdateUserRoles(adminID, models.RoleGlobal)
	adminUser, _ := d.GetUserByID(adminID)

	h := &AdminHandler{DB: d}

	// 1. CreateDomain with currentUser
	body, _ := json.Marshal(map[string]interface{}{
		"name":        "Infrastructure",
		"manager_ids": []int64{adminID},
		"team_ids":    []int64{},
	})
	rec := httptest.NewRecorder()
	req := reqWithUser(d, adminUser, http.MethodPost, "/admin/domains", bytes.NewReader(body))
	h.CreateDomain(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on CreateDomain, got %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &created) //nolint:errcheck
	domainID := int64(created["id"].(float64))

	// Verify admin action logged
	logs, err := d.GetAdminLogsByActor(adminID, time.Time{})
	if err != nil || len(logs) == 0 {
		t.Fatalf("expected admin action log, err: %v, count: %d", err, len(logs))
	}

	// 2. UpdateDomain bad JSON
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPut, "/admin/domains/"+strconv.FormatInt(domainID, 10), strings.NewReader("bad-json"))
	req.SetPathValue("id", strconv.FormatInt(domainID, 10))
	h.UpdateDomain(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad json in UpdateDomain, got %d", rec.Code)
	}

	// 3. UpdateDomain success with currentUser
	updateBody, _ := json.Marshal(map[string]interface{}{
		"name":        "Core Infra",
		"manager_ids": []int64{adminID},
		"team_ids":    []int64{},
	})
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPut, "/admin/domains/"+strconv.FormatInt(domainID, 10), bytes.NewReader(updateBody))
	req.SetPathValue("id", strconv.FormatInt(domainID, 10))
	h.UpdateDomain(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on UpdateDomain, got %d: %s", rec.Code, rec.Body.String())
	}

	// 4. DeleteDomain with currentUser
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodDelete, "/admin/domains/"+strconv.FormatInt(domainID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(domainID, 10))
	h.DeleteDomain(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on DeleteDomain, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminDomains_ListDomainsAPI(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	uID, _ := d.CreateLocalUser("dom_api_user@example.com", "Dom API User", "pass")
	_ = d.UpdateUserRoles(uID, models.RoleGlobal)
	user, _ := d.GetUserByID(uID)

	rec := httptest.NewRecorder()
	req := reqWithUser(d, user, http.MethodGet, "/api/admin/domains", nil)
	h.ListDomainsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on ListDomainsAPI, got %d", rec.Code)
	}

	d.Close()
	rec = httptest.NewRecorder()
	h.ListDomainsAPI(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on closed DB ListDomainsAPI, got %d", rec.Code)
	}
}
