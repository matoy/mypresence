package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/matoy/mypresence/internal/models"
)

// SitesReportPageData holds all data for rendering the Sites Report page.
type SitesReportPageData struct {
	Year                   int
	Month                  int
	PrevYear               int
	PrevMonth              int
	NextYear               int
	NextMonth              int
	CorpFilter             string
	Sites                  []*models.Site
	SelectedSiteID         int64
	SelectedSite           *models.Site
	Days                   []models.DayInfo
	Summaries              []models.SiteReportSummary
	DailyReports           []models.SiteDailyReport
	TotalSeats             int
	TotalAttachedPeople    int
	PeopleVsSeats          float64
	TotalCapacity          float64
	TotalOnSiteDays        float64
	AvgOccupancyRate       float64
	TotalReservations      float64
	AvgReservationRate     float64
	AvgOnSitePerWorkingDay float64
	AvgResPerWorkingDay    float64
}

// buildSitesReportData calculates all summaries, daily breakdowns and KPI aggregates.
func (h *FloorplanHandler) buildSitesReportData(r *http.Request) (*SitesReportPageData, error) {
	now := time.Now()
	year, month := now.Year(), int(now.Month())
	if yStr := r.URL.Query().Get("year"); yStr != "" {
		if y, err := strconv.Atoi(yStr); err == nil && y >= 2000 && y <= 2100 {
			year = y
		}
	}
	if mStr := r.URL.Query().Get("month"); mStr != "" {
		if m, err := strconv.Atoi(mStr); err == nil && m >= 1 && m <= 12 {
			month = m
		}
	}

	corpFilter := r.URL.Query().Get("corp")
	if corpFilter == "" {
		corpFilter = "corporate"
	}
	if corpFilter != "corporate" && corpFilter != "non_corporate" && corpFilter != "all" {
		corpFilter = "corporate"
	}

	var selectedSiteID int64
	if sStr := r.URL.Query().Get("site"); sStr != "" {
		if sID, err := strconv.ParseInt(sStr, 10, 64); err == nil && sID > 0 {
			selectedSiteID = sID
		}
	}

	prevYear, prevMonth := year, month-1
	if prevMonth < 1 {
		prevMonth = 12
		prevYear--
	}
	nextYear, nextMonth := year, month+1
	if nextMonth > 12 {
		nextMonth = 1
		nextYear++
	}

	startDate := fmt.Sprintf("%04d-%02d-01", year, month)
	lastDay := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC)
	endDate := lastDay.Format("2006-01-02")

	allSites, err := h.DB.ListSites()
	if err != nil {
		allSites = []*models.Site{}
	}
	sort.Slice(allSites, func(i, j int) bool {
		return allSites[i].Name < allSites[j].Name
	})

	var filteredSites []*models.Site
	for _, s := range allSites {
		if corpFilter == "corporate" && s.NotCorporateSite {
			continue
		}
		if corpFilter == "non_corporate" && !s.NotCorporateSite {
			continue
		}
		filteredSites = append(filteredSites, s)
	}

	var selectedSite *models.Site
	if selectedSiteID > 0 {
		for _, s := range filteredSites {
			if s.ID == selectedSiteID {
				selectedSite = s
				break
			}
		}
		if selectedSite == nil {
			selectedSiteID = 0
		}
	}

	// Active users mapped to site
	activeUsersBySite, _ := h.DB.GetActiveUsersBySite()
	allUsers, _ := h.DB.ListUsers()
	userToSite := make(map[int64]int64, len(allUsers))
	for _, u := range allUsers {
		if u.SiteID > 0 {
			userToSite[u.ID] = u.SiteID
		}
	}

	// Holidays within the month
	allHolidays, _ := h.DB.GetHolidayMap(startDate, endDate)

	// Days in the month
	days := getDaysInMonth(year, month)
	if selectedSite != nil {
		for i := range days {
			for _, hol := range allHolidays {
				if hol.Date == days[i].Date && models.UserMatchesHoliday(selectedSite.CountryCode, hol) {
					days[i].IsHoliday = true
					days[i].HolidayName = hol.Name
					days[i].HolidayAllowImputed = hol.AllowImputed
					days[i].HolidayCountryCode = hol.CountryCode
					break
				}
			}
		}
	} else {
		for i := range days {
			for _, hol := range allHolidays {
				if hol.Date == days[i].Date && hol.CountryCode == "" {
					days[i].IsHoliday = true
					days[i].HolidayName = hol.Name
					days[i].HolidayAllowImputed = hol.AllowImputed
					break
				}
			}
		}
	}

	// Seat reservations on floorplans
	reservations, _ := h.DB.GetMonthlySiteReservations(startDate, endDate)
	dailySiteReservations := make(map[int64]map[string]float64)
	for _, s := range allSites {
		dailySiteReservations[s.ID] = make(map[string]float64)
	}
	userDateResSite := make(map[string]int64, len(reservations))
	for _, r := range reservations {
		if _, ok := dailySiteReservations[r.SiteID]; !ok {
			dailySiteReservations[r.SiteID] = make(map[string]float64)
		}
		weight := 1.0
		if r.Half == "AM" || r.Half == "PM" {
			weight = 0.5
		}
		dailySiteReservations[r.SiteID][r.Date] += weight
		if r.SiteID > 0 {
			userDateResSite[fmt.Sprintf("%d_%s", r.UserID, r.Date)] = r.SiteID
		}
	}

	// Identify single corporate site fallback if applicable
	var singleCorpSiteID int64
	var corpCount int
	for _, s := range allSites {
		if !s.NotCorporateSite {
			corpCount++
			singleCorpSiteID = s.ID
		}
	}
	if corpCount != 1 {
		singleCorpSiteID = 0
	}
	if singleCorpSiteID == 0 && len(allSites) == 1 {
		singleCorpSiteID = allSites[0].ID
	}

	// If single corporate site and no users explicitly attached, attach active users
	if singleCorpSiteID > 0 && len(activeUsersBySite[singleCorpSiteID]) == 0 {
		var activeList []models.User
		for _, u := range allUsers {
			if !u.Disabled && u.SiteID == 0 {
				activeList = append(activeList, u)
			}
		}
		if len(activeList) > 0 {
			activeUsersBySite[singleCorpSiteID] = activeList
		}
	}

	// Presences on site
	presences, _ := h.DB.GetMonthlyOnSitePresences(startDate, endDate)
	dailySiteOnSite := make(map[int64]map[string]float64)
	for _, s := range allSites {
		dailySiteOnSite[s.ID] = make(map[string]float64)
	}
	for _, p := range presences {
		// Priority 1: Desk reservation on that date
		sID := userDateResSite[fmt.Sprintf("%d_%s", p.UserID, p.Date)]

		// Priority 2: User's assigned home site
		if sID == 0 {
			sID = userToSite[p.UserID]
		}

		// Priority 3: Fallback if organization has a single primary site
		if sID == 0 && singleCorpSiteID > 0 {
			sID = singleCorpSiteID
		}

		if sID > 0 {
			if _, ok := dailySiteOnSite[sID]; !ok {
				dailySiteOnSite[sID] = make(map[string]float64)
			}
			weight := 1.0
			if p.Half == "AM" || p.Half == "PM" {
				weight = 0.5
			}
			dailySiteOnSite[sID][p.Date] += weight
		}
	}

	// Compute summaries and daily reports for each site
	var summaries []models.SiteReportSummary
	var dailyReports []models.SiteDailyReport

	var totalSeats int
	var totalAttachedPeople int
	var totalCapacity float64
	var totalOnSiteDays float64
	var totalReservations float64
	var totalWorkingDaysSum float64

	sitesToProcess := filteredSites
	if selectedSiteID > 0 && selectedSite != nil {
		sitesToProcess = []*models.Site{selectedSite}
	}

	for _, s := range sitesToProcess {
		siteHols := make(map[string]models.Holiday)
		for _, hol := range allHolidays {
			if models.UserMatchesHoliday(s.CountryCode, hol) {
				siteHols[hol.Date] = hol
			}
		}

		workingDaysCount, holidayCount := computeWorkingDaysFromMap(year, month, siteHols)
		siteWorkingDays := workingDaysCount - holidayCount
		if siteWorkingDays < 0 {
			siteWorkingDays = 0
		}

		var siteOnSiteWorkingDays float64
		var siteResWorkingDays float64

		dailyOcc := make(map[string]float64)
		dailyRes := make(map[string]float64)

		for _, d := range days {
			onSiteVal := dailySiteOnSite[s.ID][d.Date]
			resVal := dailySiteReservations[s.ID][d.Date]

			if s.Seats > 0 {
				dailyOcc[d.Date] = (onSiteVal / float64(s.Seats)) * 100
				dailyRes[d.Date] = (resVal / float64(s.Seats)) * 100
			}

			// Only count working days in the monthly totals
			if !d.IsWeekend {
				if _, isHol := siteHols[d.Date]; !isHol {
					siteOnSiteWorkingDays += onSiteVal
					siteResWorkingDays += resVal
				}
			}
		}

		attachedCount := len(activeUsersBySite[s.ID])
		var peopleRatio float64
		if s.Seats > 0 {
			peopleRatio = float64(attachedCount) / float64(s.Seats)
		}

		siteCapacity := float64(s.Seats * siteWorkingDays)
		var occRate float64
		if siteCapacity > 0 {
			occRate = (siteOnSiteWorkingDays / siteCapacity) * 100
		}

		var resRate float64
		if siteCapacity > 0 {
			resRate = (siteResWorkingDays / siteCapacity) * 100
		}

		var avgOnSite float64
		var avgRes float64
		if siteWorkingDays > 0 {
			avgOnSite = siteOnSiteWorkingDays / float64(siteWorkingDays)
			avgRes = siteResWorkingDays / float64(siteWorkingDays)
		}

		sum := models.SiteReportSummary{
			Site:                  s,
			Seats:                 s.Seats,
			AttachedPeople:        attachedCount,
			PeopleVsSeats:         peopleRatio,
			WorkingDays:           siteWorkingDays,
			TotalOnSiteDays:       siteOnSiteWorkingDays,
			AvgOnSitePerDay:       avgOnSite,
			OccupancyRate:         occRate,
			TotalReservations:     siteResWorkingDays,
			AvgReservationsPerDay: avgRes,
			ReservationRate:       resRate,
		}
		summaries = append(summaries, sum)

		dailyRep := models.SiteDailyReport{
			Site:              s,
			DailyOnSite:       dailySiteOnSite[s.ID],
			DailyReservations: dailySiteReservations[s.ID],
			DailyOccupancy:    dailyOcc,
			DailyResRate:      dailyRes,
			TotalOnSite:       siteOnSiteWorkingDays,
			TotalReservations: siteResWorkingDays,
		}
		dailyReports = append(dailyReports, dailyRep)

		totalSeats += s.Seats
		totalAttachedPeople += attachedCount
		totalCapacity += siteCapacity
		totalOnSiteDays += siteOnSiteWorkingDays
		totalReservations += siteResWorkingDays
		totalWorkingDaysSum += float64(siteWorkingDays)
	}

	var globalPeopleRatio float64
	if totalSeats > 0 {
		globalPeopleRatio = float64(totalAttachedPeople) / float64(totalSeats)
	}
	var avgOccupancyRate float64
	if totalCapacity > 0 {
		avgOccupancyRate = (totalOnSiteDays / totalCapacity) * 100
	}
	var avgReservationRate float64
	if totalCapacity > 0 {
		avgReservationRate = (totalReservations / totalCapacity) * 100
	}

	var avgOnSitePerWorkingDay float64
	var avgResPerWorkingDay float64
	if len(sitesToProcess) > 0 && totalWorkingDaysSum > 0 {
		avgOnSitePerWorkingDay = totalOnSiteDays / (totalWorkingDaysSum / float64(len(sitesToProcess)))
		avgResPerWorkingDay = totalReservations / (totalWorkingDaysSum / float64(len(sitesToProcess)))
	}

	return &SitesReportPageData{
		Year:                   year,
		Month:                  month,
		PrevYear:               prevYear,
		PrevMonth:              prevMonth,
		NextYear:               nextYear,
		NextMonth:              nextMonth,
		CorpFilter:             corpFilter,
		Sites:                  filteredSites,
		SelectedSiteID:         selectedSiteID,
		SelectedSite:           selectedSite,
		Days:                   days,
		Summaries:              summaries,
		DailyReports:           dailyReports,
		TotalSeats:             totalSeats,
		TotalAttachedPeople:    totalAttachedPeople,
		PeopleVsSeats:          globalPeopleRatio,
		TotalCapacity:          totalCapacity,
		TotalOnSiteDays:        totalOnSiteDays,
		AvgOccupancyRate:       avgOccupancyRate,
		TotalReservations:      totalReservations,
		AvgReservationRate:     avgReservationRate,
		AvgOnSitePerWorkingDay: avgOnSitePerWorkingDay,
		AvgResPerWorkingDay:    avgResPerWorkingDay,
	}, nil
}

// SitesReportPage handles GET /admin/sites-report.
func (h *FloorplanHandler) SitesReportPage(w http.ResponseWriter, r *http.Request) {
	data, err := h.buildSitesReportData(r)
	if err != nil {
		http.Error(w, "Failed to compute sites report", http.StatusInternalServerError)
		return
	}
	h.Render(w, r, "admin_sites_report", data)
}

// SitesReportAPI handles GET /api/admin/sites-report.
func (h *FloorplanHandler) SitesReportAPI(w http.ResponseWriter, r *http.Request) {
	data, err := h.buildSitesReportData(r)
	if err != nil {
		jsonError(w, "Failed to compute sites report", http.StatusInternalServerError)
		return
	}
	jsonOK(w, data)
}
