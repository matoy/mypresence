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
	ErrLockerAlreadyTaken         = errors.New("locker is already assigned or unavailable")
	ErrUserAlreadyHasLockerOnSite = errors.New("user already has a locker assigned on this site")
	ErrLockerNotFound             = errors.New("locker not found")
	ErrLockerWrongSite            = errors.New("locker is not on user's assigned site")
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

// hydrateLockerFloorplans resolves FloorplanID and FloorplanName for each locker
// by matching its zone or number against floorplan_locker_ranges on the same site.
func (d *DB) hydrateLockerFloorplans(lockers []models.Locker) {
	if len(lockers) == 0 {
		return
	}
	rows, err := d.floorplan.Query(`
SELECT r.id, r.floorplan_id, f.site_id, f.name, r.label, r.zone, r.x_pct, r.y_pct
FROM floorplan_locker_ranges r
JOIN floorplans f ON f.id = r.floorplan_id
WHERE f.site_id > 0
ORDER BY f.sort_order, f.id, r.id`)
	if err != nil {
		return
	}
	defer rows.Close() //nolint:errcheck

	bySite := make(map[int64][]siteFloorplanRange)
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
				lockers[i].FloorplanID = sr.FloorplanID
				lockers[i].FloorplanName = sr.FloorplanName
				break
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

	stmt, err := tx.Prepare(d.dialect.rebind(
		"INSERT INTO lockers (site_id, number, zone, status, user_id, code, notes, assigned_at) VALUES (?, ?, ?, 'active', 0, '', '', NULL)",
	))
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
	res, err := d.floorplan.Exec(
		"UPDATE lockers SET number = ?, zone = ?, status = ?, user_id = ?, code = ?, notes = ?, assigned_at = ? WHERE id = ?",
		strings.TrimSpace(l.Number), strings.TrimSpace(l.Zone), status, l.UserID, code, strings.TrimSpace(l.Notes), assignedAt, l.ID,
	)
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

// ClaimLocker assigns a free, active locker to userID in self-service, enforcing 1 locker per user per site
// and requiring the user to be assigned to the locker's site.
func (d *DB) ClaimLocker(lockerID, userID int64, code string) error {
	var siteID int64
	var status string
	var currentHolder int64
	var existingCode string
	err := d.floorplan.QueryRow(
		"SELECT site_id, status, user_id, code FROM lockers WHERE id = ?", lockerID,
	).Scan(&siteID, &status, &currentHolder, &existingCode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLockerNotFound
		}
		return err
	}
	if status != "active" || currentHolder != 0 {
		return ErrLockerAlreadyTaken
	}

	var userSiteID int64
	if err := d.core.QueryRow("SELECT COALESCE(site_id, 0) FROM users WHERE id = ?", userID).Scan(&userSiteID); err != nil {
		return err
	}
	if userSiteID <= 0 || userSiteID != siteID {
		return ErrLockerWrongSite
	}

	var existingCount int
	if err := d.floorplan.QueryRow(
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

	res, err := d.floorplan.Exec(
		"UPDATE lockers SET user_id = ?, code = ?, assigned_at = ? WHERE id = ? AND user_id = 0 AND status = 'active'",
		userID, finalCode, time.Now(), lockerID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLockerAlreadyTaken
	}
	return nil
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

