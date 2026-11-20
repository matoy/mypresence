package db

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/matoy/mypresence/internal/models"
)

var (
	ErrLockerAlreadyTaken          = errors.New("locker is already assigned or unavailable")
	ErrUserAlreadyHasLockerOnSite  = errors.New("user already has a locker assigned on this site")
	ErrLockerNotFound              = errors.New("locker not found")
	ErrLockerWrongSite             = errors.New("locker is not on user's assigned site")
	ErrLockerUserDisabled          = errors.New("user account is disabled")
)

// naturalLess compares two strings with embedded integers compared numerically.
func naturalLess(a, b string) bool {
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		ra, rb := rune(a[ia]), rune(b[ib])
		if unicode.IsDigit(ra) && unicode.IsDigit(rb) {
			ja := ia
			for ja < len(a) && unicode.IsDigit(rune(a[ja])) {
				ja++
			}
			jb := ib
			for jb < len(b) && unicode.IsDigit(rune(b[jb])) {
				jb++
			}
			na, errA := strconv.Atoi(a[ia:ja])
			nb, errB := strconv.Atoi(b[ib:jb])
			if errA == nil && errB == nil && na != nb {
				return na < nb
			}
			if (ja - ia) != (jb - ib) {
				return (ja - ia) < (jb - ib)
			}
			ia, ib = ja, jb
			continue
		}
		la, lb := strings.ToLower(string(ra)), strings.ToLower(string(rb))
		if la != lb {
			return la < lb
		}
		ia++
		ib++
	}
	return len(a) < len(b)
}

func sortLockers(lockers []models.Locker) {
	sort.Slice(lockers, func(i, j int) bool {
		if lockers[i].SiteName != lockers[j].SiteName {
			return naturalLess(lockers[i].SiteName, lockers[j].SiteName)
		}
		if lockers[i].Zone != lockers[j].Zone {
			return naturalLess(lockers[i].Zone, lockers[j].Zone)
		}
		return naturalLess(lockers[i].Number, lockers[j].Number)
	})
}

// GetSiteLockerCounts returns maps of site_id -> total lockers and site_id -> available lockers.
func (d *DB) GetSiteLockerCounts() (map[int64]int, map[int64]int, error) {
	total := make(map[int64]int)
	available := make(map[int64]int)

	rows, err := d.floorplan.Query(
		"SELECT site_id, COUNT(*), COALESCE(SUM(CASE WHEN status = 'active' AND user_id = 0 THEN 1 ELSE 0 END), 0) FROM lockers GROUP BY site_id",
	)
	if err != nil {
		return total, available, err
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var siteID int64
		var tot, avail int
		if err := rows.Scan(&siteID, &tot, &avail); err != nil {
			return total, available, err
		}
		total[siteID] = tot
		available[siteID] = avail
	}
	return total, available, rows.Err()
}

// ListSitesWithLockers returns all sites that have at least one configured locker.
func (d *DB) ListSitesWithLockers() ([]*models.Site, error) {
	sites, err := d.ListSites()
	if err != nil {
		return nil, err
	}
	res := make([]*models.Site, 0)
	for _, s := range sites {
		if s.LockersCount > 0 {
			res = append(res, s)
		}
	}
	return res, nil
}

// GetSiteWithLockers returns site metadata and locker counts for siteID if it has at least one configured locker.
func (d *DB) GetSiteWithLockers(siteID int64) (*models.Site, error) {
	if siteID <= 0 {
		return nil, nil
	}
	var s models.Site
	err := d.floorplan.QueryRow(
		"SELECT id, name, country_code, not_corporate_site, seats FROM sites WHERE id = ?",
		siteID,
	).Scan(&s.ID, &s.Name, &s.CountryCode, &s.NotCorporateSite, &s.Seats)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var total, avail int
	err = d.floorplan.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'active' AND user_id = 0 THEN 1 ELSE 0 END), 0) FROM lockers WHERE site_id = ?",
		siteID,
	).Scan(&total, &avail)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, nil
	}
	s.Floorplans = []*models.Floorplan{}
	s.FloorplanIDs = []int64{}
	s.LockersCount = total
	s.AvailableLockersCount = avail
	return &s, nil
}

// hydrateLockerUsers enriches UserName, UserEmail, UserDisabled on lockers from core.db.
func (d *DB) hydrateLockerUsers(lockers []models.Locker) {
	idSet := make(map[int64]struct{})
	for _, l := range lockers {
		if l.UserID > 0 {
			idSet[l.UserID] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return
	}

	placeholders := make([]string, 0, len(idSet))
	args := make([]interface{}, 0, len(idSet))
	for id := range idSet {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf("SELECT id, name, email, disabled FROM users WHERE id IN (%s)", strings.Join(placeholders, ","))
	rows, err := d.core.Query(query, args...)
	if err != nil {
		return
	}
	defer rows.Close() //nolint:errcheck

	type uInfo struct {
		name     string
		email    string
		disabled bool
	}
	infoMap := make(map[int64]uInfo, len(idSet))
	for rows.Next() {
		var id int64
		var info uInfo
		if err := rows.Scan(&id, &info.name, &info.email, &info.disabled); err == nil {
			infoMap[id] = info
		}
	}

	for i := range lockers {
		if lockers[i].UserID > 0 {
			if info, ok := infoMap[lockers[i].UserID]; ok {
				lockers[i].UserName = info.name
				lockers[i].UserEmail = info.email
				lockers[i].UserDisabled = info.disabled
			}
		}
	}
}

type lockerNumToken struct {
	prefix string
	num    int
}

func parseLockerNumTokens(s string) []lockerNumToken {
	var tokens []lockerNumToken
	i := 0
	for i < len(s) {
		if !unicode.IsDigit(rune(s[i])) {
			i++
			continue
		}
		j := i
		for j < len(s) && unicode.IsDigit(rune(s[j])) {
			j++
		}
		num, err := strconv.Atoi(s[i:j])
		if err == nil {
			// Scan backwards for an attached alphabetic prefix (e.g., "A01" or "A-01")
			pEnd := i
			if pEnd > 0 && (s[pEnd-1] == '-' || s[pEnd-1] == '_') {
				if pEnd >= 2 && unicode.IsLetter(rune(s[pEnd-2])) {
					pEnd--
				} else {
					pEnd = i
				}
			}
			pStart := pEnd
			for pStart > 0 && unicode.IsLetter(rune(s[pStart-1])) {
				pStart--
			}
			prefix := strings.ToLower(s[pStart:pEnd])
			tokens = append(tokens, lockerNumToken{prefix: prefix, num: num})
		}
		i = j
	}
	return tokens
}

// lockerNumberInRange parses a label like "001-050" or "A-01..A-30".
// Returns (isRange=true, matched) when label defines a numeric interval.
func lockerNumberInRange(number, label string) (bool, bool) {
	rangeTokens := parseLockerNumTokens(label)
	if len(rangeTokens) != 2 || rangeTokens[0].num > rangeTokens[1].num {
		return false, false
	}
	numTokens := parseLockerNumTokens(number)
	if len(numTokens) != 1 {
		return true, false
	}
	lt := numTokens[0]
	start, end := rangeTokens[0], rangeTokens[1]
	rPrefix := start.prefix
	if rPrefix != "" && end.prefix != "" && rPrefix != end.prefix {
		return true, false
	}
	if rPrefix == "" {
		rPrefix = end.prefix
	}
	if rPrefix != "" && rPrefix != lt.prefix {
		return true, false
	}
	return true, lt.num >= start.num && lt.num <= end.num
}

// LockerMatchesRange reports whether a locker belongs to a floorplan locker range marker
// based on zone name and/or numeric range matching.
func LockerMatchesRange(l models.Locker, r models.FloorplanLockerRange) bool {
	lZone := strings.TrimSpace(l.Zone)
	rZone := strings.TrimSpace(r.Zone)
	if lZone != "" && rZone != "" && !strings.EqualFold(lZone, rZone) {
		return false
	}

	rLabel := strings.TrimSpace(r.Label)
	lNum := strings.TrimSpace(l.Number)
	if lNum != "" && rLabel != "" && strings.EqualFold(lNum, rLabel) {
		return true
	}

	if isRange, inRange := lockerNumberInRange(lNum, rLabel); isRange {
		return inRange
	}

	if rZone != "" && lZone != "" && strings.EqualFold(lZone, rZone) {
		return true
	}
	if lZone != "" && rLabel != "" && strings.EqualFold(lZone, rLabel) {
		return true
	}
	return false
}

type siteFloorplanRange struct {
	models.FloorplanLockerRange
	FloorplanName string
}

// hydrateLockerFloorplans resolves FloorplanID, FloorplanName and MatchingRangeIDs for each locker
// by matching its zone or number against floorplan_locker_ranges on the locker's site(s).
func (d *DB) hydrateLockerFloorplans(lockers []models.Locker) {
	if len(lockers) == 0 {
		return
	}
	siteSet := make(map[int64]struct{})
	var placeholders []string
	var args []interface{}
	for _, l := range lockers {
		if l.SiteID > 0 {
			if _, ok := siteSet[l.SiteID]; !ok {
				siteSet[l.SiteID] = struct{}{}
				placeholders = append(placeholders, "?")
				args = append(args, l.SiteID)
			}
		}
	}
	if len(args) == 0 {
		return
	}

	query := fmt.Sprintf(`
SELECT r.id, r.floorplan_id, f.site_id, f.name, r.label, r.zone, r.x_pct, r.y_pct
FROM floorplan_locker_ranges r
JOIN floorplans f ON f.id = r.floorplan_id
WHERE f.site_id IN (%s)
ORDER BY f.sort_order, f.id, r.id`, strings.Join(placeholders, ","))

	rows, err := d.floorplan.Query(query, args...)
	if err != nil {
		return
	}
	defer rows.Close() //nolint:errcheck

	bySite := make(map[int64][]siteFloorplanRange, len(siteSet))
	for rows.Next() {
		var sr siteFloorplanRange
		if err := rows.Scan(&sr.ID, &sr.FloorplanID, &sr.SiteID, &sr.FloorplanName, &sr.Label, &sr.Zone, &sr.XPct, &sr.YPct); err == nil {
			bySite[sr.SiteID] = append(bySite[sr.SiteID], sr)
		}
	}
	if len(bySite) == 0 {
		return
	}

	for i := range lockers {
		ranges := bySite[lockers[i].SiteID]
		for _, sr := range ranges {
			if LockerMatchesRange(lockers[i], sr.FloorplanLockerRange) {
				if lockers[i].FloorplanID == 0 {
					lockers[i].FloorplanID = sr.FloorplanID
					lockers[i].FloorplanName = sr.FloorplanName
				}
				lockers[i].MatchingRangeIDs = append(lockers[i].MatchingRangeIDs, sr.ID)
			}
		}
	}
}

// CreateLocker inserts a single physical locker for a site.
func (d *DB) CreateLocker(l models.Locker) (int64, error) {
	status := strings.TrimSpace(l.Status)
	if status != "maintenance" {
		status = "active"
	}
	var assignedAt interface{}
	if l.UserID > 0 {
		if l.AssignedAt != nil {
			assignedAt = *l.AssignedAt
		} else {
			assignedAt = time.Now()
		}
	}
	return d.floorplan.InsertGetID(
		"INSERT INTO lockers (site_id, number, zone, status, user_id, code, notes, assigned_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		l.SiteID, strings.TrimSpace(l.Number), strings.TrimSpace(l.Zone), status, l.UserID, strings.TrimSpace(l.Code), strings.TrimSpace(l.Notes), assignedAt,
	)
}

// BulkCreateLockers generates a sequential range of lockers on a site, skipping existing numbers.
func (d *DB) BulkCreateLockers(siteID int64, prefix string, startNum, endNum, padWidth int, zone string) (int, error) {
	if siteID <= 0 || startNum < 0 || endNum < startNum || (endNum-startNum+1) > 500 {
		return 0, fmt.Errorf("invalid locker bulk range")
	}

	rows, err := d.floorplan.Query("SELECT number FROM lockers WHERE site_id = ?", siteID)
	if err != nil {
		return 0, err
	}
	existing := make(map[string]bool)
	for rows.Next() {
		var num string
		if err := rows.Scan(&num); err == nil {
			existing[strings.ToLower(strings.TrimSpace(num))] = true
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	tx, err := d.floorplan.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.Prepare(
		"INSERT INTO lockers (site_id, number, zone, status, user_id, code, notes, assigned_at) VALUES (?, ?, ?, 'active', 0, '', '', NULL)",
	)
	if err != nil {
		return 0, err
	}
	defer stmt.Close() //nolint:errcheck

	cleanZone := strings.TrimSpace(zone)
	created := 0
	for n := startNum; n <= endNum; n++ {
		var numStr string
		if padWidth > 1 {
			numStr = fmt.Sprintf("%s%0*d", prefix, padWidth, n)
		} else {
			numStr = fmt.Sprintf("%s%d", prefix, n)
		}
		numStr = strings.TrimSpace(numStr)
		if numStr == "" || existing[strings.ToLower(numStr)] {
			continue
		}
		if _, err := stmt.Exec(siteID, numStr, cleanZone); err != nil {
			return 0, err
		}
		existing[strings.ToLower(numStr)] = true
		created++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return created, nil
}

// GetLocker retrieves a locker by ID with site name and user info hydrated.
func (d *DB) GetLocker(id int64) (*models.Locker, error) {
	var l models.Locker
	var assignedAt sql.NullTime
	err := d.floorplan.QueryRow(`
SELECT l.id, l.site_id, COALESCE(s.name, ''), l.number, l.zone, l.status, l.user_id, l.code, l.notes, l.assigned_at
FROM lockers l
LEFT JOIN sites s ON s.id = l.site_id
WHERE l.id = ?`, id).Scan(
		&l.ID, &l.SiteID, &l.SiteName, &l.Number, &l.Zone, &l.Status, &l.UserID, &l.Code, &l.Notes, &assignedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLockerNotFound
		}
		return nil, err
	}
	if assignedAt.Valid {
		t := assignedAt.Time
		l.AssignedAt = &t
	}
	slice := []models.Locker{l}
	d.hydrateLockerUsers(slice)
	d.hydrateLockerFloorplans(slice)
	return &slice[0], nil
}

// ListLockers returns all lockers (optionally filtered by siteID > 0), naturally sorted.
func (d *DB) ListLockers(siteID int64) ([]models.Locker, error) {
	query := `
SELECT l.id, l.site_id, COALESCE(s.name, ''), l.number, l.zone, l.status, l.user_id, l.code, l.notes, l.assigned_at
FROM lockers l
LEFT JOIN sites s ON s.id = l.site_id`
	var args []interface{}
	if siteID > 0 {
		query += " WHERE l.site_id = ?"
		args = append(args, siteID)
	}

	rows, err := d.floorplan.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var lockers []models.Locker
	for rows.Next() {
		var l models.Locker
		var assignedAt sql.NullTime
		if err := rows.Scan(&l.ID, &l.SiteID, &l.SiteName, &l.Number, &l.Zone, &l.Status, &l.UserID, &l.Code, &l.Notes, &assignedAt); err != nil {
			return nil, err
		}
		if assignedAt.Valid {
			t := assignedAt.Time
			l.AssignedAt = &t
		}
		lockers = append(lockers, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	d.hydrateLockerUsers(lockers)
	d.hydrateLockerFloorplans(lockers)
	sortLockers(lockers)
	if lockers == nil {
		lockers = []models.Locker{}
	}
	return lockers, nil
}

// ListAvailableLockersBySite returns free, active lockers for self-service selection without exposing any code or notes.
func (d *DB) ListAvailableLockersBySite(siteID int64) ([]models.Locker, error) {
	rows, err := d.floorplan.Query(`
SELECT l.id, l.site_id, COALESCE(s.name, ''), l.number, l.zone, l.status
FROM lockers l
LEFT JOIN sites s ON s.id = l.site_id
WHERE l.site_id = ? AND l.status = 'active' AND l.user_id = 0`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var lockers []models.Locker
	for rows.Next() {
		var l models.Locker
		if err := rows.Scan(&l.ID, &l.SiteID, &l.SiteName, &l.Number, &l.Zone, &l.Status); err != nil {
			return nil, err
		}
		lockers = append(lockers, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	d.hydrateLockerFloorplans(lockers)
	sortLockers(lockers)
	if lockers == nil {
		lockers = []models.Locker{}
	}
	return lockers, nil
}

// GetUserLockers returns all lockers assigned to the given user, including their physical code memo.
func (d *DB) GetUserLockers(userID int64) ([]models.Locker, error) {
	rows, err := d.floorplan.Query(`
SELECT l.id, l.site_id, COALESCE(s.name, ''), l.number, l.zone, l.status, l.user_id, l.code, l.assigned_at
FROM lockers l
LEFT JOIN sites s ON s.id = l.site_id
WHERE l.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var lockers []models.Locker
	for rows.Next() {
		var l models.Locker
		var assignedAt sql.NullTime
		if err := rows.Scan(&l.ID, &l.SiteID, &l.SiteName, &l.Number, &l.Zone, &l.Status, &l.UserID, &l.Code, &assignedAt); err != nil {
			return nil, err
		}
		if assignedAt.Valid {
			t := assignedAt.Time
			l.AssignedAt = &t
		}
		lockers = append(lockers, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	d.hydrateLockerFloorplans(lockers)
	sortLockers(lockers)
	if lockers == nil {
		lockers = []models.Locker{}
	}
	return lockers, nil
}

// UpdateLocker updates an existing locker's attributes and assignment.
func (d *DB) UpdateLocker(l models.Locker) error {
	status := strings.TrimSpace(l.Status)
	if status != "maintenance" {
		status = "active"
	}
	var assignedAt interface{}
	if l.UserID > 0 {
		if l.AssignedAt != nil {
			assignedAt = *l.AssignedAt
		} else {
			assignedAt = time.Now()
		}
	}
	code := strings.TrimSpace(l.Code)
	query := "UPDATE lockers SET number = ?, zone = ?, status = ?, user_id = ?, code = ?, notes = ?, assigned_at = ?"
	args := []interface{}{
		strings.TrimSpace(l.Number), strings.TrimSpace(l.Zone), status, l.UserID, code, strings.TrimSpace(l.Notes), assignedAt,
	}
	if l.SiteID > 0 {
		query += ", site_id = ?"
		args = append(args, l.SiteID)
	}
	query += " WHERE id = ?"
	args = append(args, l.ID)
	res, err := d.floorplan.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrLockerNotFound
	}
	return nil
}

// DeleteLocker permanently removes a locker record.
func (d *DB) DeleteLocker(id int64) error {
	res, err := d.floorplan.Exec("DELETE FROM lockers WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrLockerNotFound
	}
	return nil
}

// ClaimLocker assigns a free, active locker to userID in self-service inside an atomic transaction,
// enforcing 1 locker per user per site, active account state, and matching site assignment.
func (d *DB) ClaimLocker(lockerID, userID int64, code string) error {
	var userSiteID int64
	var userDisabled bool
	err := d.core.QueryRow("SELECT COALESCE(site_id, 0), disabled FROM users WHERE id = ?", userID).Scan(&userSiteID, &userDisabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLockerWrongSite
		}
		return err
	}

	if d.dialect.isSQLite() {
		d.lockerMu.Lock()
		defer d.lockerMu.Unlock()
	}

	tx, err := d.floorplan.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var siteID int64
	var status string
	var currentHolder int64
	var existingCode string
	selectLockerQ := "SELECT site_id, status, user_id, code FROM lockers WHERE id = ?"
	if d.dialect.isPostgres() {
		selectLockerQ += " FOR UPDATE"
	}
	err = tx.QueryRow(selectLockerQ, lockerID).Scan(&siteID, &status, &currentHolder, &existingCode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLockerNotFound
		}
		return err
	}
	if status != "active" || currentHolder != 0 {
		return ErrLockerAlreadyTaken
	}
	if userDisabled {
		return ErrLockerUserDisabled
	}
	if userSiteID <= 0 || userSiteID != siteID {
		return ErrLockerWrongSite
	}

	var existingCount int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM lockers WHERE site_id = ? AND user_id = ?", siteID, userID,
	).Scan(&existingCount); err != nil {
		return err
	}
	if existingCount > 0 {
		return ErrUserAlreadyHasLockerOnSite
	}

	finalCode := strings.TrimSpace(code)
	if finalCode == "" {
		finalCode = existingCode
	}

	res, err := tx.Exec(
		"UPDATE lockers SET user_id = ?, code = ?, assigned_at = ? WHERE id = ? AND user_id = 0 AND status = 'active'",
		userID, finalCode, time.Now(), lockerID,
	)
	if err != nil {
		msgLower := strings.ToLower(err.Error())
		if strings.Contains(msgLower, "unique") || strings.Contains(msgLower, "duplicate") {
			return ErrUserAlreadyHasLockerOnSite
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLockerAlreadyTaken
	}
	return tx.Commit()
}

// UserHasLockerOnSite reports whether userID already holds another locker on siteID.
func (d *DB) UserHasLockerOnSite(siteID, userID, excludeLockerID int64) (bool, error) {
	if siteID <= 0 || userID <= 0 {
		return false, nil
	}
	var count int
	err := d.floorplan.QueryRow(
		"SELECT COUNT(*) FROM lockers WHERE site_id = ? AND user_id = ? AND id <> ?",
		siteID, userID, excludeLockerID,
	).Scan(&count)
	return count > 0, err
}

// ReleaseLockersForUser automatically releases lockers held by userID (excluding keepSiteID when > 0),
// preserving physical codes, and returns the released locker records for auditing and admin notifications.
func (d *DB) ReleaseLockersForUser(userID, keepSiteID int64) ([]models.Locker, error) {
	if userID <= 0 {
		return nil, nil
	}
	if d.dialect.isSQLite() {
		d.lockerMu.Lock()
		defer d.lockerMu.Unlock()
	}

	tx, err := d.floorplan.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	selectQ := `
SELECT l.id, l.site_id, COALESCE(s.name, ''), l.number, l.zone
FROM lockers l
LEFT JOIN sites s ON s.id = l.site_id
WHERE l.user_id = ?`
	updateQ := "UPDATE lockers SET user_id = 0, assigned_at = NULL WHERE user_id = ?"
	args := []interface{}{userID}
	if keepSiteID > 0 {
		selectQ += " AND l.site_id <> ?"
		updateQ += " AND site_id <> ?"
		args = append(args, keepSiteID)
	}

	rows, err := tx.Query(selectQ, args...)
	if err != nil {
		return nil, err
	}
	var released []models.Locker
	for rows.Next() {
		var l models.Locker
		if err := rows.Scan(&l.ID, &l.SiteID, &l.SiteName, &l.Number, &l.Zone); err != nil {
			_ = rows.Close()
			return nil, err
		}
		released = append(released, l)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(released) == 0 {
		return nil, nil
	}

	if _, err := tx.Exec(updateQ, args...); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	sortLockers(released)
	return released, nil
}

// ListLockerUserOptions returns a minimal user projection for locker assignment dropdowns.
func (d *DB) ListLockerUserOptions(siteID int64, search string, limit int) ([]models.LockerUserOption, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := "SELECT id, name, email, COALESCE(site_id, 0), disabled FROM users WHERE 1=1"
	var args []interface{}
	if siteID > 0 {
		query += " AND site_id = ?"
		args = append(args, siteID)
	}
	if q := strings.ToLower(strings.TrimSpace(search)); q != "" {
		like := "%" + q + "%"
		query += " AND (LOWER(name) LIKE ? OR LOWER(email) LIKE ?)"
		args = append(args, like, like)
	}
	query += " ORDER BY disabled ASC, name ASC"

	rows, err := d.core.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	users := make([]models.LockerUserOption, 0)
	for rows.Next() {
		var u models.LockerUserOption
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.SiteID, &u.Disabled); err != nil {
			return nil, err
		}
		users = append(users, u)
		if len(users) >= limit {
			break
		}
	}
	return users, rows.Err()
}

// GetLockerAdminUserIDs returns the IDs of all active users holding global or floorplan_manager roles.
func (d *DB) GetLockerAdminUserIDs() ([]int64, error) {
	rows, err := d.core.Query(
		"SELECT id, role FROM users WHERE disabled = " + d.dialect.boolDefault(false),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var ids []int64
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Roles); err != nil {
			return nil, err
		}
		if u.HasAnyRole(models.RoleGlobal, models.RoleFloorplanManager) {
			ids = append(ids, u.ID)
		}
	}
	return ids, rows.Err()
}

// ListLockersFiltered returns paginated naturally-sorted lockers along with total filtered count,
// site-scoped statistics, and available zones.
func (d *DB) ListLockersFiltered(siteID int64, zone, status, search string, page, limit int) ([]models.Locker, int, models.LockerStats, []string, error) {
	all, err := d.ListLockers(siteID)
	if err != nil {
		return nil, 0, models.LockerStats{}, nil, err
	}

	var stats models.LockerStats
	zoneSet := make(map[string]struct{})
	for _, l := range all {
		stats.Total++
		if l.Status == "maintenance" {
			stats.Maintenance++
		} else if l.UserID > 0 {
			stats.Assigned++
		} else {
			stats.Free++
		}
		if l.UserID > 0 && l.UserDisabled {
			stats.DisabledUser++
		}
		if z := strings.TrimSpace(l.Zone); z != "" {
			zoneSet[z] = struct{}{}
		}
	}
	if siteID > 0 {
		if fpZones, zErr := d.GetSiteFloorplanLockerZones(); zErr == nil {
			for _, z := range fpZones[siteID] {
				if tz := strings.TrimSpace(z); tz != "" {
					zoneSet[tz] = struct{}{}
				}
			}
		}
	}
	zones := make([]string, 0, len(zoneSet))
	for z := range zoneSet {
		zones = append(zones, z)
	}
	sort.Slice(zones, func(i, j int) bool { return naturalLess(zones[i], zones[j]) })

	cleanZone := strings.TrimSpace(zone)
	q := strings.ToLower(strings.TrimSpace(search))
	filtered := make([]models.Locker, 0, len(all))
	for _, l := range all {
		if cleanZone != "" && strings.TrimSpace(l.Zone) != cleanZone {
			continue
		}
		switch status {
		case "free":
			if l.Status != "active" || l.UserID > 0 {
				continue
			}
		case "assigned":
			if l.UserID <= 0 {
				continue
			}
		case "maintenance":
			if l.Status != "maintenance" {
				continue
			}
		case "disabled_user":
			if l.UserID <= 0 || !l.UserDisabled {
				continue
			}
		}
		if q != "" {
			hay := strings.ToLower(l.Number + " " + l.Zone + " " + l.SiteName + " " + l.UserName + " " + l.UserEmail + " " + l.Notes)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		filtered = append(filtered, l)
	}

	totalFiltered := len(filtered)
	if limit <= 0 {
		return filtered, totalFiltered, stats, zones, nil
	}
	if page < 1 {
		page = 1
	}
	start := (page - 1) * limit
	if start >= totalFiltered {
		return []models.Locker{}, totalFiltered, stats, zones, nil
	}
	end := start + limit
	if end > totalFiltered {
		end = totalFiltered
	}
	return filtered[start:end], totalFiltered, stats, zones, nil
}

// UpdateUserLockerCode updates the collaborator's personal code memo on their own locker.
func (d *DB) UpdateUserLockerCode(lockerID, userID int64, code string) error {
	res, err := d.floorplan.Exec(
		"UPDATE lockers SET code = ? WHERE id = ? AND user_id = ?",
		strings.TrimSpace(code), lockerID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrLockerNotFound
	}
	return err
}

// ReleaseUserLocker frees a locker held by userID while preserving its physical code.
func (d *DB) ReleaseUserLocker(lockerID, userID int64) error {
	res, err := d.floorplan.Exec(
		"UPDATE lockers SET user_id = 0, assigned_at = NULL WHERE id = ? AND user_id = ?",
		lockerID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrLockerNotFound
	}
	return err
}

// --- Floorplan locker range markers ---

// ListFloorplanLockerRanges returns all locker bank range markers placed on a floorplan,
// enriched with SiteID and live TotalCount / AvailableCount computed from matching site lockers.
func (d *DB) ListFloorplanLockerRanges(floorplanID int64) ([]models.FloorplanLockerRange, error) {
	rows, err := d.floorplan.Query(`
SELECT r.id, r.floorplan_id, COALESCE(f.site_id, 0), r.label, r.zone, r.x_pct, r.y_pct
FROM floorplan_locker_ranges r
LEFT JOIN floorplans f ON f.id = r.floorplan_id
WHERE r.floorplan_id = ?
ORDER BY r.id`,
		floorplanID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var siteID int64
	var ranges []models.FloorplanLockerRange
	for rows.Next() {
		var r models.FloorplanLockerRange
		if err := rows.Scan(&r.ID, &r.FloorplanID, &r.SiteID, &r.Label, &r.Zone, &r.XPct, &r.YPct); err != nil {
			return nil, err
		}
		if r.SiteID > 0 {
			siteID = r.SiteID
		}
		ranges = append(ranges, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ranges) > 0 && siteID > 0 {
		lRows, err := d.floorplan.Query(
			"SELECT id, site_id, number, zone, status, user_id FROM lockers WHERE site_id = ?", siteID,
		)
		if err == nil {
			var siteLockers []models.Locker
			for lRows.Next() {
				var l models.Locker
				if err := lRows.Scan(&l.ID, &l.SiteID, &l.Number, &l.Zone, &l.Status, &l.UserID); err == nil {
					siteLockers = append(siteLockers, l)
				}
			}
			_ = lRows.Close()
			if err := lRows.Err(); err != nil {
				return nil, err
			}
			for i := range ranges {
				for _, l := range siteLockers {
					if LockerMatchesRange(l, ranges[i]) {
						ranges[i].TotalCount++
						if l.Status == "active" && l.UserID == 0 {
							ranges[i].AvailableCount++
						}
					}
				}
			}
		}
	}
	if ranges == nil {
		ranges = []models.FloorplanLockerRange{}
	}
	return ranges, nil
}

// GetSiteFloorplanLockerZones returns distinct non-empty zone names defined on floorplan locker ranges per site.
func (d *DB) GetSiteFloorplanLockerZones() (map[int64][]string, error) {
	result := make(map[int64][]string)
	rows, err := d.floorplan.Query(`
SELECT DISTINCT f.site_id, r.zone
FROM floorplan_locker_ranges r
JOIN floorplans f ON f.id = r.floorplan_id
WHERE f.site_id > 0 AND r.zone <> ''
ORDER BY r.zone`)
	if err != nil {
		return result, err
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var siteID int64
		var zone string
		if err := rows.Scan(&siteID, &zone); err == nil {
			zone = strings.TrimSpace(zone)
			if zone != "" {
				result[siteID] = append(result[siteID], zone)
			}
		}
	}
	return result, rows.Err()
}

// GetFloorplanLockerRange fetches a single locker range marker by ID.
func (d *DB) GetFloorplanLockerRange(id int64) (*models.FloorplanLockerRange, error) {
	var r models.FloorplanLockerRange
	err := d.floorplan.QueryRow(
		"SELECT id, floorplan_id, label, zone, x_pct, y_pct FROM floorplan_locker_ranges WHERE id = ?", id,
	).Scan(&r.ID, &r.FloorplanID, &r.Label, &r.Zone, &r.XPct, &r.YPct)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateFloorplanLockerRange inserts a new locker range marker on a floorplan.
func (d *DB) CreateFloorplanLockerRange(r models.FloorplanLockerRange) (int64, error) {
	return d.floorplan.InsertGetID(
		"INSERT INTO floorplan_locker_ranges (floorplan_id, label, zone, x_pct, y_pct) VALUES (?, ?, ?, ?, ?)",
		r.FloorplanID, strings.TrimSpace(r.Label), strings.TrimSpace(r.Zone), r.XPct, r.YPct,
	)
}

// UpdateFloorplanLockerRange updates a locker range marker's label, zone, or coordinates.
func (d *DB) UpdateFloorplanLockerRange(r models.FloorplanLockerRange) error {
	res, err := d.floorplan.Exec(
		"UPDATE floorplan_locker_ranges SET label = ?, zone = ?, x_pct = ?, y_pct = ? WHERE id = ?",
		strings.TrimSpace(r.Label), strings.TrimSpace(r.Zone), r.XPct, r.YPct, r.ID,
	)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteFloorplanLockerRange removes a locker range marker.
func (d *DB) DeleteFloorplanLockerRange(id int64) error {
	_, err := d.floorplan.Exec("DELETE FROM floorplan_locker_ranges WHERE id = ?", id)
	return err
}

