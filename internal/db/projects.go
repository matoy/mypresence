package db

import (
	"fmt"
	"time"

	"github.com/matoy/mypresence/internal/models"
)

// migrateProjects creates the projects schema if it doesn't exist.
func (d *DB) migrateProjects() error {
	dl := d.dialect
	ai := dl.autoincrement()
	real_ := dl.realType()
	dt := dl.datetimeType()

	stmts := []string{
		dl.createTableIfNotExists("projects", fmt.Sprintf(`
  id               %s,
  name             %s NOT NULL,
  code             %s NOT NULL,
  team_id          BIGINT NOT NULL DEFAULT 0,
  active           %s NOT NULL DEFAULT %s,
  mini_project     %s NOT NULL DEFAULT %s,
  start_date       %s NOT NULL,
  end_date         %s NOT NULL,
  initial_end_date %s NOT NULL DEFAULT '',
  created_at       %s DEFAULT CURRENT_TIMESTAMP
`, ai, dl.varcharType(128), dl.varcharType(32), dl.boolType(), dl.boolDefault(true), dl.boolType(), dl.boolDefault(false), dl.varcharType(10), dl.varcharType(10), dl.varcharType(10), dt)),

		dl.createTableIfNotExists("project_time_entries", fmt.Sprintf(`
  id         %s,
  project_id BIGINT NOT NULL,
  user_id    BIGINT NOT NULL,
  year       INTEGER NOT NULL,
  month      INTEGER NOT NULL,
  days       %s    NOT NULL DEFAULT 0,
  UNIQUE(project_id, user_id, year, month),
  FOREIGN KEY (project_id) REFERENCES projects(id)
`, ai, real_)),

		// project_members stores the explicit assignment of users to projects.
		// A project with no rows in this table is "open" (accessible to everyone).
		dl.createTableIfNotExists("project_members", `
  project_id BIGINT NOT NULL,
  user_id    BIGINT NOT NULL,
  UNIQUE(project_id, user_id),
  FOREIGN KEY (project_id) REFERENCES projects(id)
`),

		dl.createTableIfNotExists("project_favorites", `
  project_id BIGINT NOT NULL,
  user_id    BIGINT NOT NULL,
  UNIQUE(project_id, user_id),
  FOREIGN KEY (project_id) REFERENCES projects(id)
`),

		// project_activities stores daily activity declarations for teams with
		// "Timesheets managed manually" enabled. Several rows may exist for the
		// same user/date; their percentages should sum to the day's billable weight.
		dl.createTableIfNotExists("project_activities", fmt.Sprintf(`
  id            %s,
  user_id       BIGINT NOT NULL,
  date          %s NOT NULL,
  activity_type %s NOT NULL,
  jira_key      %s NOT NULL DEFAULT '',
  jira_title    %s NOT NULL DEFAULT '',
  comment       %s NOT NULL DEFAULT '',
  percentage    %s NOT NULL DEFAULT 0,
  created_at    %s DEFAULT CURRENT_TIMESTAMP,
  updated_at    %s DEFAULT CURRENT_TIMESTAMP
`, ai, dl.varcharType(10), dl.varcharType(20), dl.varcharType(32), dl.varcharType(255), dl.textType(), real_, dt, dt)),
	}

	for _, stmt := range stmts {
		if _, err := d.projects.Exec(dl.rebind(stmt)); err != nil {
			return err
		}
	}

	// Additive migrations (safe to run multiple times — errors ignored)
	d.projects.Exec(dl.rebind(dl.addColumnIfNotExists("projects", "mini_project", fmt.Sprintf("%s NOT NULL DEFAULT %s", dl.boolType(), dl.boolDefault(false))))) //nolint:errcheck
	d.projects.Exec(dl.rebind(dl.addColumnIfNotExists("projects", "initial_end_date", dl.varcharType(10)+" NOT NULL DEFAULT ''")))                               //nolint:errcheck
	d.projects.Exec(`UPDATE projects SET initial_end_date = end_date WHERE initial_end_date = ''`)                                                               //nolint:errcheck
	return nil
}

// ─── Projects CRUD ────────────────────────────────────────────────────────────

// ListProjects returns all projects, enriched with team names from core.db.
func (d *DB) ListProjects() ([]models.Project, error) {
	rows, err := d.projects.Query(`
SELECT id, name, code, team_id, active, mini_project, start_date, end_date, initial_end_date, created_at
FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var projects []models.Project
	for rows.Next() {
		var p models.Project
		var createdAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.TeamID, &p.Active, &p.MiniProject, &p.StartDate, &p.EndDate, &p.InitialEndDate, &createdAt); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse("2006-01-02T15:04:05Z", createdAt)
		projects = append(projects, p)
	}
	// Enrich with team names
	teamMap, _ := d.teamNameMap()
	for i := range projects {
		if n, ok := teamMap[projects[i].TeamID]; ok {
			projects[i].TeamName = n
		}
	}
	return projects, rows.Err()
}

// ListActiveProjectsForMonth returns projects that are active and whose end_date
// is not before the first day of the given year/month.
func (d *DB) ListActiveProjectsForMonth(year, month int) ([]models.Project, error) {
	firstDay := fmt.Sprintf("%04d-%02d-01", year, month)
	rows, err := d.projects.Query(`
SELECT id, name, code, team_id, active, mini_project, start_date, end_date, initial_end_date, created_at
FROM projects
WHERE active = ? AND end_date >= ?
ORDER BY name`, true, firstDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var projects []models.Project
	for rows.Next() {
		var p models.Project
		var createdAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.TeamID, &p.Active, &p.MiniProject, &p.StartDate, &p.EndDate, &p.InitialEndDate, &createdAt); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse("2006-01-02T15:04:05Z", createdAt)
		projects = append(projects, p)
	}
	teamMap, _ := d.teamNameMap()
	for i := range projects {
		if n, ok := teamMap[projects[i].TeamID]; ok {
			projects[i].TeamName = n
		}
	}
	return projects, rows.Err()
}

// GetProject returns a single project by ID.
func (d *DB) GetProject(id int64) (models.Project, error) {
	var p models.Project
	var createdAt string
	err := d.projects.QueryRow(`
SELECT id, name, code, team_id, active, mini_project, start_date, end_date, initial_end_date, created_at
FROM projects WHERE id = ?`, id).Scan(
		&p.ID, &p.Name, &p.Code, &p.TeamID, &p.Active, &p.MiniProject, &p.StartDate, &p.EndDate, &p.InitialEndDate, &createdAt)
	if err != nil {
		return p, err
	}
	p.CreatedAt, _ = time.Parse("2006-01-02T15:04:05Z", createdAt)
	teamMap, _ := d.teamNameMap()
	if n, ok := teamMap[p.TeamID]; ok {
		p.TeamName = n
	}
	return p, nil
}

// CreateProject inserts a new project and returns its ID. The initial end date
// defaults to endDate and mini_project defaults to false.
func (d *DB) CreateProject(name, code string, teamID int64, active bool, startDate, endDate string) (int64, error) {
	return d.projects.InsertGetID(`
INSERT INTO projects (name, code, team_id, active, start_date, end_date, initial_end_date)
VALUES (?, ?, ?, ?, ?, ?, ?)`, name, code, teamID, active, startDate, endDate, endDate)
}

// CreateProjectWithDetails inserts a new project including the mini-project flag
// and initial end date. If initialEndDate is empty it defaults to endDate.
func (d *DB) CreateProjectWithDetails(name, code string, teamID int64, active bool, startDate, endDate string, miniProject bool, initialEndDate string) (int64, error) {
	if initialEndDate == "" {
		initialEndDate = endDate
	}
	return d.projects.InsertGetID(`
INSERT INTO projects (name, code, team_id, active, mini_project, start_date, end_date, initial_end_date)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, name, code, teamID, active, miniProject, startDate, endDate, initialEndDate)
}

// UpdateProject updates an existing project's core fields, leaving mini_project
// and initial_end_date unchanged.
func (d *DB) UpdateProject(id int64, name, code string, teamID int64, active bool, startDate, endDate string) error {
	_, err := d.projects.Exec(`
UPDATE projects SET name=?, code=?, team_id=?, active=?, start_date=?, end_date=?
WHERE id=?`, name, code, teamID, active, startDate, endDate, id)
	return err
}

// UpdateProjectWithDetails updates an existing project including the mini-project
// flag and initial end date.
func (d *DB) UpdateProjectWithDetails(id int64, name, code string, teamID int64, active bool, startDate, endDate string, miniProject bool, initialEndDate string) error {
	if initialEndDate == "" {
		initialEndDate = endDate
	}
	_, err := d.projects.Exec(`
UPDATE projects SET name=?, code=?, team_id=?, active=?, mini_project=?, start_date=?, end_date=?, initial_end_date=?
WHERE id=?`, name, code, teamID, active, miniProject, startDate, endDate, initialEndDate, id)
	return err
}

// ─── Project members ──────────────────────────────────────────────────────────

// GetProjectMembers returns the user IDs explicitly assigned to a project.
// An empty slice means the project is "open" (no restriction).
func (d *DB) GetProjectMembers(projectID int64) ([]int64, error) {
	rows, err := d.projects.Query(`
SELECT user_id FROM project_members WHERE project_id = ? ORDER BY user_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetProjectMembers atomically replaces the full member list of a project.
// Passing an empty slice removes all restrictions (open project).
func (d *DB) SetProjectMembers(projectID int64, userIDs []int64) error {
	tx, err := d.projects.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM project_members WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	for _, uid := range userIDs {
		if _, err := tx.Exec(
			`INSERT INTO project_members (project_id, user_id) VALUES (?, ?)`,
			projectID, uid,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// IsUserAssignedToProject returns true when the user may declare time on the
// project: either the project has no members (open access) or the user is
// explicitly listed.
func (d *DB) IsUserAssignedToProject(userID, projectID int64) (bool, error) {
	var total int
	if err := d.projects.QueryRow(
		`SELECT COUNT(*) FROM project_members WHERE project_id = ?`, projectID,
	).Scan(&total); err != nil {
		return false, err
	}
	if total == 0 {
		return true, nil // open project
	}
	var count int
	err := d.projects.QueryRow(
		`SELECT COUNT(*) FROM project_members WHERE project_id = ? AND user_id = ?`,
		projectID, userID,
	).Scan(&count)
	return count > 0, err
}

// GetUserFavoriteProjectIDs returns the IDs of projects the user has starred.
func (d *DB) GetUserFavoriteProjectIDs(userID int64) ([]int64, error) {
	rows, err := d.projects.Query(
		`SELECT project_id FROM project_favorites WHERE user_id = ? ORDER BY project_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ToggleProjectFavorite flips the favorite state for (userID, projectID).
// Returns true when the project is now a favorite, false when it was removed.
func (d *DB) ToggleProjectFavorite(userID, projectID int64) (bool, error) {
	res, err := d.projects.Exec(
		`DELETE FROM project_favorites WHERE user_id = ? AND project_id = ?`, userID, projectID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return false, nil
	}
	_, err = d.projects.Exec(
		`INSERT INTO project_favorites (user_id, project_id) VALUES (?, ?)`, userID, projectID)
	return err == nil, err
}

// ListActiveProjectsForMonthAndUser returns active projects accessible to
// the given user: either the project has no members (open) or the user is
// explicitly assigned.
func (d *DB) ListActiveProjectsForMonthAndUser(year, month int, userID int64) ([]models.Project, error) {
	firstDay := fmt.Sprintf("%04d-%02d-01", year, month)
	rows, err := d.projects.Query(`
SELECT p.id, p.name, p.code, p.team_id, p.active, p.mini_project, p.start_date, p.end_date, p.initial_end_date, p.created_at
FROM projects p
WHERE p.active = ? AND p.end_date >= ?
  AND (
    NOT EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = p.id)
    OR  EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = p.id AND pm.user_id = ?)
  )
ORDER BY p.name`, true, firstDay, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var projects []models.Project
	for rows.Next() {
		var p models.Project
		var createdAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.TeamID, &p.Active, &p.MiniProject, &p.StartDate, &p.EndDate, &p.InitialEndDate, &createdAt); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse("2006-01-02T15:04:05Z", createdAt)
		projects = append(projects, p)
	}
	teamMap, _ := d.teamNameMap()
	for i := range projects {
		if n, ok := teamMap[projects[i].TeamID]; ok {
			projects[i].TeamName = n
		}
	}
	return projects, rows.Err()
}

// ─── Time entries ─────────────────────────────────────────────────────────────

// GetUserProjectEntriesForMonth returns all time entries for a user in a given month.
func (d *DB) GetUserProjectEntriesForMonth(userID int64, year, month int) ([]models.ProjectTimeEntry, error) {
	rows, err := d.projects.Query(`
SELECT id, project_id, user_id, year, month, days
FROM project_time_entries
WHERE user_id = ? AND year = ? AND month = ?`, userID, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var entries []models.ProjectTimeEntry
	for rows.Next() {
		var e models.ProjectTimeEntry
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.UserID, &e.Year, &e.Month, &e.Days); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// GetUserTotalDeclaredForMonth returns the total days declared by a user across all projects in a month.
func (d *DB) GetUserTotalDeclaredForMonth(userID int64, year, month int) (float64, error) {
	var total float64
	err := d.projects.QueryRow(`
SELECT COALESCE(SUM(days), 0)
FROM project_time_entries
WHERE user_id = ? AND year = ? AND month = ?`, userID, year, month).Scan(&total)
	return total, err
}

// SetProjectTimeEntry upserts a user's declared days for a project/month.
// days=0 removes the entry.
func (d *DB) SetProjectTimeEntry(userID, projectID int64, year, month int, days float64) error {
	if days <= 0 {
		_, err := d.projects.Exec(d.dialect.rebind(`
DELETE FROM project_time_entries
WHERE user_id = ? AND project_id = ? AND year = ? AND month = ?`),
			userID, projectID, year, month)
		return err
	}
	stmt := d.dialect.upsertOnConflict(
		"project_time_entries",
		[]string{"project_id", "user_id", "year", "month", "days"},
		"?, ?, ?, ?, ?",
		"project_id, user_id, year, month",
		"days = excluded.days",
	)
	_, err := d.projects.Exec(d.dialect.rebind(stmt), projectID, userID, year, month, days)
	return err
}

// GetUserBillableDaysForMonth counts billable presence days for a user in a given month.
// Full days count as 1.0, half-days as 0.5.
func (d *DB) GetUserBillableDaysForMonth(userID int64, year, month int) (float64, error) {
	datePrefix := fmt.Sprintf("%04d-%02d-%%", year, month)
	var total float64
	err := d.presence.QueryRow(`
SELECT COALESCE(SUM(CASE WHEN p.half = 'full' THEN 1.0 ELSE 0.5 END), 0)
FROM presences p
JOIN statuses s ON p.status_id = s.id
WHERE p.user_id = ? AND p.date LIKE ? AND s.billable = ?`, userID, datePrefix, true).Scan(&total)
	return total, err
}

// ─── Report data ──────────────────────────────────────────────────────────────

// GetProjectsReport returns report rows for a list of projects,
// showing per-user breakdowns for the given month keys ("YYYY-MM").
// userMap maps userID -> User (loaded from core.db by the caller).
func (d *DB) GetProjectsReport(projectIDs []int64, monthKeys []string, userMap map[int64]models.User) ([]models.ProjectReportRow, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}

	// Load all relevant time entries in one query.
	placeholders := make([]string, len(projectIDs))
	args := make([]interface{}, len(projectIDs))
	for i, id := range projectIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := d.projects.Query(
		`SELECT project_id, user_id, year, month, days
         FROM project_time_entries
         WHERE project_id IN (`+joinStrings(placeholders, ",")+`)`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	// Accumulate: projectID -> userID -> monthKey -> days
	entryMap := make(map[int64]map[int64]map[string]float64) // project -> user -> month -> days
	allTime := make(map[int64]map[int64]float64)             // project -> user -> total
	for rows.Next() {
		var e models.ProjectTimeEntry
		if err := rows.Scan(&e.ProjectID, &e.UserID, &e.Year, &e.Month, &e.Days); err != nil {
			return nil, err
		}
		if entryMap[e.ProjectID] == nil {
			entryMap[e.ProjectID] = make(map[int64]map[string]float64)
		}
		if entryMap[e.ProjectID][e.UserID] == nil {
			entryMap[e.ProjectID][e.UserID] = make(map[string]float64)
		}
		mk := fmt.Sprintf("%04d-%02d", e.Year, e.Month)
		entryMap[e.ProjectID][e.UserID][mk] += e.Days

		if allTime[e.ProjectID] == nil {
			allTime[e.ProjectID] = make(map[int64]float64)
		}
		allTime[e.ProjectID][e.UserID] += e.Days
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load projects
	teamMap, _ := d.teamNameMap()
	var result []models.ProjectReportRow
	for _, pid := range projectIDs {
		proj, err := d.GetProject(pid)
		if err != nil {
			continue
		}
		if n, ok := teamMap[proj.TeamID]; ok {
			proj.TeamName = n
		}
		row := buildProjectReportRow(proj, entryMap[pid], allTime[pid], monthKeys, userMap)
		result = append(result, row)
	}
	return result, nil
}

// buildProjectReportRow constructs a ProjectReportRow for one project from the
// pre-accumulated entry and time maps.
func buildProjectReportRow(
	proj models.Project,
	userEntries map[int64]map[string]float64,
	userTotals map[int64]float64,
	monthKeys []string,
	userMap map[int64]models.User,
) models.ProjectReportRow {
	var userRows []models.ProjectUserMonth
	monthTotals := make(map[string]float64)
	projectTotal := 0.0

	for uid, monthMap := range userEntries {
		u, ok := userMap[uid]
		if !ok {
			continue
		}
		row := models.ProjectUserMonth{
			User:        u,
			MonthlyDays: make(map[string]float64),
		}
		for _, mk := range monthKeys {
			row.MonthlyDays[mk] = monthMap[mk]
			monthTotals[mk] += monthMap[mk]
		}
		row.TotalDays = userTotals[uid]
		projectTotal += row.TotalDays
		userRows = append(userRows, row)
	}
	return models.ProjectReportRow{
		Project:     proj,
		UserRows:    userRows,
		MonthTotals: monthTotals,
		TotalDays:   projectTotal,
	}
}

// ListProjectsByTeams returns all projects whose team_id is in the given set.
// Pass nil to return all projects.
func (d *DB) ListProjectsByTeams(teamIDs []int64) ([]models.Project, error) {
	if teamIDs == nil {
		return d.ListProjects()
	}
	if len(teamIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(teamIDs))
	args := make([]interface{}, len(teamIDs))
	for i, id := range teamIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := d.projects.Query(
		`SELECT id, name, code, team_id, active, mini_project, start_date, end_date, initial_end_date, created_at
         FROM projects WHERE team_id IN (`+joinStrings(placeholders, ",")+`) ORDER BY name`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var projects []models.Project
	teamMap, _ := d.teamNameMap()
	for rows.Next() {
		var p models.Project
		var createdAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.TeamID, &p.Active, &p.MiniProject, &p.StartDate, &p.EndDate, &p.InitialEndDate, &createdAt); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse("2006-01-02T15:04:05Z", createdAt)
		if n, ok := teamMap[p.TeamID]; ok {
			p.TeamName = n
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// teamNameMap returns a map of team_id -> team_name from core.db.
func (d *DB) teamNameMap() (map[int64]string, error) {
	rows, err := d.core.Query(`SELECT id, name FROM teams`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	m := make(map[int64]string)
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		m[id] = name
	}
	return m, rows.Err()
}

// joinStrings joins a slice with a separator (avoids import of strings in this file).
func joinStrings(s []string, sep string) string {
	result := ""
	for i, v := range s {
		if i > 0 {
			result += sep
		}
		result += v
	}
	return result
}

// GetTeamIDsForUser returns the IDs of teams the user is a leader of (or all teams if global/projects_manager).
// Used for scoping the report to team-leader users.
func (d *DB) GetTeamIDsForUser(userID int64) ([]int64, error) {
	rows, err := d.core.Query(`SELECT team_id FROM user_teams WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
