package db

import (
	"testing"

	"github.com/matoy/mypresence/internal/config"
)

func newDomainsTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	database, err := Open(&config.Config{DBDriver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestDomainsCRUD(t *testing.T) {
	d := newDomainsTestDB(t)

	id, err := d.CreateDomain("Engineering")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}

	domains, err := d.ListDomains()
	if err != nil || len(domains) != 1 || domains[0].Name != "Engineering" {
		t.Fatalf("ListDomains: got %+v, err %v", domains, err)
	}

	got, err := d.GetDomain(id)
	if err != nil || got.Name != "Engineering" {
		t.Fatalf("GetDomain: got %+v, err %v", got, err)
	}

	if err := d.UpdateDomain(id, "Engineering & Product"); err != nil {
		t.Fatalf("UpdateDomain: %v", err)
	}
	if name := d.GetDomainName(id); name != "Engineering & Product" {
		t.Fatalf("GetDomainName after update: got %q", name)
	}

	if name := d.GetDomainName(999999); name != "" {
		t.Fatalf("GetDomainName for missing domain: got %q, want empty", name)
	}
}

func TestDomainManagers(t *testing.T) {
	d := newDomainsTestDB(t)

	domainID, _ := d.CreateDomain("Sales")
	u1, _ := d.CreateLocalUser("mgr1@example.com", "Manager One", "password1")
	u2, _ := d.CreateLocalUser("mgr2@example.com", "Manager Two", "password1")

	if err := d.SetDomainManagers(domainID, []int64{u1, u2}); err != nil {
		t.Fatalf("SetDomainManagers: %v", err)
	}

	managers, err := d.ListDomainManagers(domainID)
	if err != nil || len(managers) != 2 {
		t.Fatalf("ListDomainManagers: got %+v, err %v", managers, err)
	}

	isManager, err := d.IsDomainManager(u1)
	if err != nil || !isManager {
		t.Fatalf("IsDomainManager(u1): got %v, err %v", isManager, err)
	}

	notManager, err := d.IsDomainManager(u2 + 999)
	if err != nil || notManager {
		t.Fatalf("IsDomainManager for unrelated user: got %v, err %v", notManager, err)
	}

	domains, err := d.GetUserDomains(u1)
	if err != nil || len(domains) != 1 || domains[0].ID != domainID {
		t.Fatalf("GetUserDomains(u1): got %+v, err %v", domains, err)
	}

	// Replacing the manager set should drop u2.
	if err := d.SetDomainManagers(domainID, []int64{u1}); err != nil {
		t.Fatalf("SetDomainManagers (replace): %v", err)
	}
	managers, _ = d.ListDomainManagers(domainID)
	if len(managers) != 1 || managers[0].ID != u1 {
		t.Fatalf("ListDomainManagers after replace: got %+v", managers)
	}
}

func TestDomainTeamsAndDeletion(t *testing.T) {
	d := newDomainsTestDB(t)

	domainID, _ := d.CreateDomain("Ops")
	teamID, err := d.CreateTeamWithDetails("Ops Team", "", false, false)
	if err != nil {
		t.Fatalf("CreateTeamWithDetails: %v", err)
	}

	if err := d.UpdateTeamDomain(teamID, domainID); err != nil {
		t.Fatalf("UpdateTeamDomain: %v", err)
	}

	teams, err := d.ListTeamsForDomain(domainID)
	if err != nil || len(teams) != 1 || teams[0].ID != teamID {
		t.Fatalf("ListTeamsForDomain: got %+v, err %v", teams, err)
	}

	all, err := d.ListTeams()
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	var found bool
	for _, tm := range all {
		if tm.ID == teamID {
			found = true
			if tm.DomainID != domainID {
				t.Errorf("team.DomainID: got %d, want %d", tm.DomainID, domainID)
			}
		}
	}
	if !found {
		t.Fatal("team not found in ListTeams")
	}

	withTeams, err := d.ListDomainsWithTeams()
	if err != nil || len(withTeams) != 1 || len(withTeams[0].Teams) != 1 {
		t.Fatalf("ListDomainsWithTeams: got %+v, err %v", withTeams, err)
	}

	// Deleting the domain should detach the team rather than delete it.
	if err := d.DeleteDomain(domainID); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}
	all, _ = d.ListTeams()
	for _, tm := range all {
		if tm.ID == teamID && tm.DomainID != 0 {
			t.Errorf("team.DomainID after domain deletion: got %d, want 0", tm.DomainID)
		}
	}
	domains, _ := d.ListDomains()
	if len(domains) != 0 {
		t.Fatalf("expected no domains after deletion, got %+v", domains)
	}
}
