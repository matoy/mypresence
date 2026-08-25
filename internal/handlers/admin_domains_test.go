package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/models"
)

func TestAdminDomains_CreateUpdateDelete(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	u1, _ := d.CreateLocalUser("dm1@example.com", "DM One", "password1")
	u2, _ := d.CreateLocalUser("dm2@example.com", "DM Two", "password1")
	teamID, err := d.CreateTeamWithDetails("Team A", "", false, false)
	if err != nil {
		t.Fatalf("CreateTeamWithDetails: %v", err)
	}

	// bad JSON
	wBad := httptest.NewRecorder()
	h.CreateDomain(wBad, httptest.NewRequest(http.MethodPost, "/admin/domains", strings.NewReader("{")))
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad json, got %d", wBad.Code)
	}

	// create
	body, _ := json.Marshal(map[string]interface{}{
		"name":        "Engineering",
		"manager_ids": []int64{u1},
		"team_ids":    []int64{teamID},
	})
	w := httptest.NewRecorder()
	h.CreateDomain(w, httptest.NewRequest(http.MethodPost, "/admin/domains", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 create domain, got %d: %s", w.Code, w.Body.String())
	}
	var created map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &created) //nolint:errcheck
	domainID := int64(created["id"].(float64))

	domains, err := d.ListDomains()
	if err != nil || len(domains) != 1 || domains[0].Name != "Engineering" {
		t.Fatalf("ListDomains: got %+v, err %v", domains, err)
	}
	managers, _ := d.ListDomainManagers(domainID)
	if len(managers) != 1 || managers[0].ID != u1 {
		t.Fatalf("expected u1 as manager, got %+v", managers)
	}
	teams, _ := d.ListTeamsForDomain(domainID)
	if len(teams) != 1 || teams[0].ID != teamID {
		t.Fatalf("expected teamID attached, got %+v", teams)
	}

	// update: swap manager and detach team
	updateBody, _ := json.Marshal(map[string]interface{}{
		"name":        "Engineering & Product",
		"manager_ids": []int64{u2},
		"team_ids":    []int64{},
	})
	wUpdate := httptest.NewRecorder()
	reqUpdate := httptest.NewRequest(http.MethodPut, "/admin/domains/"+strconv.FormatInt(domainID, 10), bytes.NewReader(updateBody))
	reqUpdate.SetPathValue("id", strconv.FormatInt(domainID, 10))
	h.UpdateDomain(wUpdate, reqUpdate)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("expected 200 update domain, got %d: %s", wUpdate.Code, wUpdate.Body.String())
	}
	if name := d.GetDomainName(domainID); name != "Engineering & Product" {
		t.Fatalf("GetDomainName after update: got %q", name)
	}
	managers, _ = d.ListDomainManagers(domainID)
	if len(managers) != 1 || managers[0].ID != u2 {
		t.Fatalf("expected u2 as sole manager after update, got %+v", managers)
	}
	teams, _ = d.ListTeamsForDomain(domainID)
	if len(teams) != 0 {
		t.Fatalf("expected team detached after update, got %+v", teams)
	}

	// list API
	wList := httptest.NewRecorder()
	h.ListDomainsAPI(wList, httptest.NewRequest(http.MethodGet, "/api/domains", nil))
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 list domains API, got %d", wList.Code)
	}

	// delete
	wDelete := httptest.NewRecorder()
	reqDelete := httptest.NewRequest(http.MethodDelete, "/admin/domains/"+strconv.FormatInt(domainID, 10), nil)
	reqDelete.SetPathValue("id", strconv.FormatInt(domainID, 10))
	h.DeleteDomain(wDelete, reqDelete)
	if wDelete.Code != http.StatusOK {
		t.Fatalf("expected 200 delete domain, got %d", wDelete.Code)
	}
	domains, _ = d.ListDomains()
	if len(domains) != 0 {
		t.Fatalf("expected no domains after delete, got %+v", domains)
	}
}

func TestAdminDomains_TeamsPage_ShowsDomains(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	domainID, _ := d.CreateDomain("Ops")
	teamID, _ := d.CreateTeamWithDetails("Ops Team", "", false, false)
	if err := d.UpdateTeamDomain(teamID, domainID); err != nil {
		t.Fatalf("UpdateTeamDomain: %v", err)
	}

	rendered := false
	h.Render = func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = true
		m := data.(map[string]interface{})
		domains, ok := m["Domains"].([]models.Domain)
		if !ok || len(domains) != 1 {
			t.Fatalf("expected Domains list of length 1 in TeamsPage data, got %+v", m["Domains"])
		}
	}
	h.TeamsPage(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin/teams", nil))
	if !rendered {
		t.Fatal("TeamsPage did not render")
	}
}
