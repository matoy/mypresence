package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/matoy/mypresence/internal/metrics"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// DomainWithDetails pairs a domain with its managers and attached teams, for
// rendering on the domains admin page.
type DomainWithDetails struct {
	Domain   models.Domain
	Managers []models.User
	Teams    []models.Team
}

// DomainsPage renders the domain management page (global admins only).
func (h *AdminHandler) DomainsPage(w http.ResponseWriter, r *http.Request) {
	domains, _ := h.DB.ListDomains()
	allTeams, _ := h.DB.ListTeams()
	users, _ := h.DB.ListUsers()

	var domainsList []DomainWithDetails
	for _, dm := range domains {
		managers, _ := h.DB.ListDomainManagers(dm.ID)
		var teams []models.Team
		for _, t := range allTeams {
			if t.DomainID == dm.ID {
				teams = append(teams, t)
			}
		}
		domainsList = append(domainsList, DomainWithDetails{Domain: dm, Managers: managers, Teams: teams})
	}

	h.Render(w, r, "admin_domains", map[string]interface{}{
		"Domains":  domainsList,
		"AllTeams": allTeams,
		"Users":    users,
	})
}

// ListDomainsAPI returns all domains (with their teams) as JSON.
func (h *AdminHandler) ListDomainsAPI(w http.ResponseWriter, r *http.Request) {
	domains, err := h.DB.ListDomainsWithTeams()
	if err != nil {
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	jsonOK(w, domains)
}

// CreateDomain creates a new domain with its managers and attached teams.
func (h *AdminHandler) CreateDomain(w http.ResponseWriter, r *http.Request) {
	currentUser := middleware.GetUser(r)
	var req struct {
		Name       string  `json:"name"`
		ManagerIDs []int64 `json:"manager_ids"`
		TeamIDs    []int64 `json:"team_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		metrics.AdminOpsTotal.WithLabelValues("domain", "create", "failure").Inc()
		jsonError(w, "name required", http.StatusBadRequest)
		return
	}
	id, err := h.DB.CreateDomain(strings.TrimSpace(req.Name))
	if err != nil {
		metrics.AdminOpsTotal.WithLabelValues("domain", "create", "failure").Inc()
		jsonError(w, "Erreur création domaine", http.StatusInternalServerError)
		return
	}
	h.DB.SetDomainManagers(id, req.ManagerIDs) //nolint:errcheck
	h.applyDomainTeams(id, req.TeamIDs)
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "domain", id, "create", req.Name)
		slog.Info("admin.domain.create", "actor", currentUser.Email, "domain", req.Name, "domain_id", id)
	}
	metrics.AdminOpsTotal.WithLabelValues("domain", "create", "success").Inc()
	jsonOK(w, map[string]interface{}{"id": id, "status": "ok"})
}

// UpdateDomain updates a domain's name, managers and attached teams.
func (h *AdminHandler) UpdateDomain(w http.ResponseWriter, r *http.Request) {
	currentUser := middleware.GetUser(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Name       string  `json:"name"`
		ManagerIDs []int64 `json:"manager_ids"`
		TeamIDs    []int64 `json:"team_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.AdminOpsTotal.WithLabelValues("domain", "update", "failure").Inc()
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if err := h.DB.UpdateDomain(id, strings.TrimSpace(req.Name)); err != nil {
		metrics.AdminOpsTotal.WithLabelValues("domain", "update", "failure").Inc()
		jsonError(w, "Erreur", http.StatusInternalServerError)
		return
	}
	h.DB.SetDomainManagers(id, req.ManagerIDs) //nolint:errcheck
	h.applyDomainTeams(id, req.TeamIDs)
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "domain", id, "update", req.Name)
		slog.Info("admin.domain.update", "actor", currentUser.Email, "domain", req.Name, "domain_id", id)
	}
	metrics.AdminOpsTotal.WithLabelValues("domain", "update", "success").Inc()
	jsonOK(w, map[string]string{"status": "ok"})
}

// DeleteDomain deletes a domain, detaching its teams.
func (h *AdminHandler) DeleteDomain(w http.ResponseWriter, r *http.Request) {
	currentUser := middleware.GetUser(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	domainName := h.DB.GetDomainName(id)
	h.DB.DeleteDomain(id) //nolint:errcheck
	if currentUser != nil {
		h.DB.LogAdminAction(currentUser.ID, "domain", id, "delete", domainName)
		slog.Info("admin.domain.delete", "actor", currentUser.Email, "domain", domainName, "domain_id", id)
	}
	metrics.AdminOpsTotal.WithLabelValues("domain", "delete", "success").Inc()
	jsonOK(w, map[string]string{"status": "ok"})
}

// applyDomainTeams attaches the given teams to a domain and detaches any team
// that was previously attached but is no longer listed.
func (h *AdminHandler) applyDomainTeams(domainID int64, teamIDs []int64) {
	wanted := map[int64]bool{}
	for _, id := range teamIDs {
		wanted[id] = true
		h.DB.UpdateTeamDomain(id, domainID) //nolint:errcheck
	}
	current, _ := h.DB.ListTeamsForDomain(domainID)
	for _, t := range current {
		if !wanted[t.ID] {
			h.DB.UpdateTeamDomain(t.ID, 0) //nolint:errcheck
		}
	}
}
