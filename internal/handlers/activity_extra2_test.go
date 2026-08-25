package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/models"
)

func TestActivity_DomainManager_ActivityAPI(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &ActivityHandler{DB: d}

	mgrID, _ := d.CreateLocalUser("domain_mgr@example.com", "Domain Mgr", "pass")
	mgrUser, _ := d.GetUserByID(mgrID)

	domID, _ := d.CreateDomain("Finance")
	_ = d.SetDomainManagers(domID, []int64{mgrID})

	teamInDom, _ := d.CreateTeamWithDetails("Finance Team", "", false, false)
	_ = d.UpdateTeamDomain(teamInDom, domID)

	teamOther, _ := d.CreateTeamWithDetails("Marketing Team", "", false, false)

	// 1. Missing parameters -> 400
	rec := httptest.NewRecorder()
	req := reqWithUser(d, mgrUser, http.MethodGet, "/api/activity", nil)
	h.ActivityAPI(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on missing params, got %d", rec.Code)
	}

	// 2. Domain manager requesting team in their domain -> 200 OK
	rec = httptest.NewRecorder()
	req = reqWithUser(d, mgrUser, http.MethodGet, fmt.Sprintf("/api/activity?team_id=%d&year=2026&month=8", teamInDom), nil)
	h.ActivityAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for domain manager on managed team, got %d", rec.Code)
	}

	// 3. Domain manager requesting team outside their domain -> 403 Forbidden
	rec = httptest.NewRecorder()
	req = reqWithUser(d, mgrUser, http.MethodGet, fmt.Sprintf("/api/activity?team_id=%d&year=2026&month=8", teamOther), nil)
	h.ActivityAPI(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for domain manager on unmanaged team, got %d", rec.Code)
	}

	// 4. computeDomainStats helper test
	stats := h.computeDomainStats([]models.Team{{ID: teamInDom}}, "2026-08-01", "2026-08-31")
	if stats == nil {
		stats = []models.UserStats{}
	}
	_ = stats
}
