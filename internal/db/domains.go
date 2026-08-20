package db

import (
	"sort"

	"github.com/matoy/mypresence/internal/models"
)

// --- Domain management ---

// ListDomains returns all domains ordered by name.
func (d *DB) ListDomains() ([]models.Domain, error) {
	rows, err := d.core.Query("SELECT id, name, created_at FROM domains ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var domains []models.Domain
	for rows.Next() {
		var dm models.Domain
		if err := rows.Scan(&dm.ID, &dm.Name, &dm.CreatedAt); err != nil {
			return nil, err
		}
		domains = append(domains, dm)
	}
	return domains, rows.Err()
}

// GetDomain returns a single domain by ID.
func (d *DB) GetDomain(id int64) (*models.Domain, error) {
	var dm models.Domain
	err := d.core.QueryRow("SELECT id, name, created_at FROM domains WHERE id = ?", id).Scan(&dm.ID, &dm.Name, &dm.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &dm, nil
}

// CreateDomain creates a new domain.
func (d *DB) CreateDomain(name string) (int64, error) {
	return d.core.InsertGetID("INSERT INTO domains (name) VALUES (?)", name)
}

// UpdateDomain renames a domain.
func (d *DB) UpdateDomain(id int64, name string) error {
	_, err := d.core.Exec("UPDATE domains SET name = ? WHERE id = ?", name, id)
	return err
}

// DeleteDomain deletes a domain, detaching any teams still assigned to it.
func (d *DB) DeleteDomain(id int64) error {
	if _, err := d.core.Exec("UPDATE teams SET domain_id = 0 WHERE domain_id = ?", id); err != nil {
		return err
	}
	_, err := d.core.Exec("DELETE FROM domains WHERE id = ?", id)
	return err
}

// GetDomainName returns a domain's name, or "" if not found.
func (d *DB) GetDomainName(id int64) string {
	dm, err := d.GetDomain(id)
	if err != nil {
		return ""
	}
	return dm.Name
}

// ListDomainManagers returns the users assigned as managers of a domain.
func (d *DB) ListDomainManagers(domainID int64) ([]models.User, error) {
	rows, err := d.core.Query(`
SELECT u.id, u.email, u.name, u.role, COALESCE(u.password_hash,''), u.disabled, u.created_at
FROM users u
JOIN domain_managers dmg ON u.id = dmg.user_id
WHERE dmg.domain_id = ?
ORDER BY u.name
`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Roles, &u.PasswordHash, &u.Disabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.IsLocal = u.PasswordHash != ""
		users = append(users, u)
	}
	return users, rows.Err()
}

// SetDomainManagers replaces the full set of managers for a domain.
func (d *DB) SetDomainManagers(domainID int64, userIDs []int64) error {
	if _, err := d.core.Exec("DELETE FROM domain_managers WHERE domain_id = ?", domainID); err != nil {
		return err
	}
	for _, uid := range userIDs {
		if _, err := d.core.Exec(d.dialect.rebind(d.dialect.insertOrIgnore(
			"domain_managers",
			[]string{"domain_id", "user_id"},
			"?, ?",
		)), domainID, uid); err != nil {
			return err
		}
	}
	return nil
}

// ListTeamsForDomain returns the teams currently attached to a domain.
func (d *DB) ListTeamsForDomain(domainID int64) ([]models.Team, error) {
	rows, err := d.core.Query("SELECT id, name, COALESCE(jira_space_key,''), timesheets_managed_manually, domain_id, created_at FROM teams WHERE domain_id = ? ORDER BY name", domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var teams []models.Team
	for rows.Next() {
		var t models.Team
		if err := rows.Scan(&t.ID, &t.Name, &t.JiraSpaceKey, &t.TimesheetsManagedManually, &t.DomainID, &t.CreatedAt); err != nil {
			return nil, err
		}
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

// GetUserDomains returns the domains the given user manages, ordered by name.
func (d *DB) GetUserDomains(userID int64) ([]models.Domain, error) {
	rows, err := d.core.Query(`
SELECT dm.id, dm.name, dm.created_at
FROM domains dm
JOIN domain_managers dmg ON dm.id = dmg.domain_id
WHERE dmg.user_id = ?
ORDER BY dm.name
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var domains []models.Domain
	for rows.Next() {
		var dm models.Domain
		if err := rows.Scan(&dm.ID, &dm.Name, &dm.CreatedAt); err != nil {
			return nil, err
		}
		domains = append(domains, dm)
	}
	return domains, rows.Err()
}

// IsDomainManager reports whether the given user manages at least one domain.
func (d *DB) IsDomainManager(userID int64) (bool, error) {
	var count int
	err := d.core.QueryRow("SELECT COUNT(*) FROM domain_managers WHERE user_id = ?", userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DomainWithTeams pairs a domain with the teams currently attached to it.
type DomainWithTeams struct {
	Domain models.Domain
	Teams  []models.Team
}

// ListDomainsWithTeams returns all domains along with their attached teams,
// ordered by domain name then team name.
func (d *DB) ListDomainsWithTeams() ([]DomainWithTeams, error) {
	domains, err := d.ListDomains()
	if err != nil {
		return nil, err
	}
	allTeams, err := d.ListTeams()
	if err != nil {
		return nil, err
	}
	teamsByDomain := map[int64][]models.Team{}
	for _, t := range allTeams {
		if t.DomainID > 0 {
			teamsByDomain[t.DomainID] = append(teamsByDomain[t.DomainID], t)
		}
	}
	result := make([]DomainWithTeams, 0, len(domains))
	for _, dm := range domains {
		ts := teamsByDomain[dm.ID]
		sort.Slice(ts, func(i, j int) bool { return ts[i].Name < ts[j].Name })
		result = append(result, DomainWithTeams{Domain: dm, Teams: ts})
	}
	return result, nil
}
