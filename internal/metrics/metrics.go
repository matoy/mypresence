// Package metrics defines all Prometheus metrics for myPresence.
// Metrics are registered on the default registry via promauto.
package metrics

import (
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ─── HTTP ────────────────────────────────────────────────────────────────────

var (
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mypresence_http_requests_total",
		Help: "Total number of HTTP requests handled.",
	}, []string{"method", "path", "status_class"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mypresence_http_request_duration_seconds",
		Help:    "HTTP request latency distribution.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"method", "path"})
)

// ─── Authentication ───────────────────────────────────────────────────────────

var (
	AuthLoginsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mypresence_auth_logins_total",
		Help: "Total login attempts by method and result.",
	}, []string{"method", "result"}) // method: local|saml  result: success|failure

	AuthLogoutsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mypresence_auth_logouts_total",
		Help: "Total number of logouts.",
	})
)

// ─── Presence operations ──────────────────────────────────────────────────────

var (
	// PresenceOpsTotal counts each call to set/clear (one per API call).
	PresenceOpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mypresence_presence_operations_total",
		Help: "Total presence set/clear operations.",
	}, []string{"action", "half"}) // action: set|clear  half: full|AM|PM|all

	// PresenceDaysTotal counts individual day*user records written/deleted.
	PresenceDaysTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mypresence_presence_days_total",
		Help: "Total presence day-records written or deleted.",
	}, []string{"action"}) // action: set|clear
)

// ─── Project operations ───────────────────────────────────────────────────────

var (
	ProjectOpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mypresence_project_operations_total",
		Help: "Total project operations by action and result.",
	}, []string{"action", "result"}) // action: set_time|create|update|list|report  result: success|failure

	ProjectDeclaredDaysTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mypresence_project_declared_days_total",
		Help: "Cumulative number of project days declared through set_time operations.",
	})
)

// ─── PAT, floorplan and admin operations ─────────────────────────────────────

var (
	PATOpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mypresence_pat_operations_total",
		Help: "Total PAT operations by action and result.",
	}, []string{"action", "result"}) // action: list|create|revoke|admin_revoke  result: success|failure

	FloorplanOpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mypresence_floorplan_operations_total",
		Help: "Total floorplan and reservation operations by action and result.",
	}, []string{"action", "result"}) // action: list_floorplans|list_seats|reserve|cancel|bulk_reserve|bulk_cancel|admin_floorplan|admin_seat|admin_image

	AdminOpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mypresence_admin_operations_total",
		Help: "Total admin operations by entity/action/result.",
	}, []string{"entity", "action", "result"}) // entity: team|status|user|role|site|domain|notification|news

	// SeatReservationDaysTotal counts individual desk-day reservations booked or cancelled.
	SeatReservationDaysTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mypresence_seat_reservation_days_total",
		Help: "Total desk-days booked or cancelled.",
	}, []string{"action"}) // action: booked|cancelled

	// CertificationsOpsTotal counts presence and project monthly certification operations.
	CertificationsOpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mypresence_certifications_operations_total",
		Help: "Total certification and decertification operations.",
	}, []string{"type", "action"}) // type: presence|project  action: certify|decertify

	// ProjectActivityDeclarationsTotal counts individual activity declarations.
	ProjectActivityDeclarationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mypresence_project_activity_declarations_total",
		Help: "Total project activity declarations by source and action.",
	}, []string{"source", "action"}) // source: jira|servicenow|other  action: create|update|delete
)

// ─── Health gauges (collected on each scrape via callback) ───────────────────

// HealthStats holds point-in-time health data derived from the /health check.
type HealthStats struct {
	Up            float64 // 1 = ok, 0 = degraded
	UptimeSeconds float64
	DBUp          float64 // 1 = ok, 0 = error
}

// RegisterHealthCollector constructs and registers a custom Prometheus collector
// that invokes fn on every scrape to obtain current health data.
func RegisterHealthCollector(fn func() HealthStats) {
	_ = prometheus.Register(newHealthCollector(fn))
}

type healthCollector struct {
	fn         func() HealthStats
	descUp     *prometheus.Desc
	descUptime *prometheus.Desc
	descDBUp   *prometheus.Desc
}

func newHealthCollector(fn func() HealthStats) *healthCollector {
	return &healthCollector{
		fn:         fn,
		descUp:     prometheus.NewDesc("mypresence_up", "1 if the application is healthy, 0 if degraded.", nil, nil),
		descUptime: prometheus.NewDesc("mypresence_uptime_seconds", "Seconds since the application started.", nil, nil),
		descDBUp:   prometheus.NewDesc("mypresence_db_up", "1 if the database check passes, 0 otherwise.", nil, nil),
	}
}

func (c *healthCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.descUp
	ch <- c.descUptime
	ch <- c.descDBUp
}

func (c *healthCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.fn()
	ch <- prometheus.MustNewConstMetric(c.descUp, prometheus.GaugeValue, s.Up)
	ch <- prometheus.MustNewConstMetric(c.descUptime, prometheus.GaugeValue, s.UptimeSeconds)
	ch <- prometheus.MustNewConstMetric(c.descDBUp, prometheus.GaugeValue, s.DBUp)
}

// ─── DB gauges (collected on each scrape via callback) ───────────────────────

// DBStats holds point-in-time counts fetched from the database.
type DBStats struct {
	Users                  float64
	ActiveSessions         float64
	Teams                  float64
	Statuses               float64
	Presences              float64
	Floorplans             float64
	Seats                  float64
	Projects               float64
	ProjectEntries         float64
	Sites                  float64
	SitesCorporate         float64
	SitesNonCorporate      float64
	SiteWorkstations       float64
	SeatReservations       float64
	ReservationsToday      float64
	ActiveProjects         float64
	ProjectMembers         float64
	ProjectActivities      float64
	ProjectActivitiesJira  float64
	ProjectActivitiesSN    float64
	ProjectActivitiesOther float64
	PresenceCertifications float64
	ProjectCertifications  float64
	Domains                float64
	ActivePATs             float64
	Passkeys               float64
	Notifications          float64
	News                   float64
}

// RegisterDBCollector constructs and registers a custom Prometheus collector
// that invokes fn on every scrape to obtain current DB counts.
func RegisterDBCollector(fn func() DBStats) {
	_ = prometheus.Register(newDBCollector(fn))
}

type dbCollector struct {
	fn func() DBStats

	descUsers                  *prometheus.Desc
	descSessions               *prometheus.Desc
	descTeams                  *prometheus.Desc
	descStatuses               *prometheus.Desc
	descPresences              *prometheus.Desc
	descFloorplans             *prometheus.Desc
	descSeats                  *prometheus.Desc
	descProjects               *prometheus.Desc
	descProjEntr               *prometheus.Desc
	descSites                  *prometheus.Desc
	descSitesCorporate         *prometheus.Desc
	descSitesNonCorporate      *prometheus.Desc
	descSiteWorkstations       *prometheus.Desc
	descSeatReservations       *prometheus.Desc
	descReservationsToday      *prometheus.Desc
	descActiveProjects         *prometheus.Desc
	descProjectMembers         *prometheus.Desc
	descProjectActivities      *prometheus.Desc
	descProjectActivitiesJira  *prometheus.Desc
	descProjectActivitiesSN    *prometheus.Desc
	descProjectActivitiesOther *prometheus.Desc
	descCertificationsPres     *prometheus.Desc
	descCertificationsProj     *prometheus.Desc
	descDomains                *prometheus.Desc
	descActivePATs             *prometheus.Desc
	descPasskeys               *prometheus.Desc
	descNotifications          *prometheus.Desc
	descNews                   *prometheus.Desc
}

func newDBCollector(fn func() DBStats) *dbCollector {
	return &dbCollector{
		fn:                         fn,
		descUsers:                  prometheus.NewDesc("mypresence_db_users_total", "Total registered users.", nil, nil),
		descSessions:               prometheus.NewDesc("mypresence_db_sessions_active_total", "Currently active sessions.", nil, nil),
		descTeams:                  prometheus.NewDesc("mypresence_db_teams_total", "Total teams.", nil, nil),
		descStatuses:               prometheus.NewDesc("mypresence_db_statuses_total", "Total presence statuses defined.", nil, nil),
		descPresences:              prometheus.NewDesc("mypresence_db_presences_total", "Total presence records stored.", nil, nil),
		descFloorplans:             prometheus.NewDesc("mypresence_db_floorplans_total", "Total floor plans.", nil, nil),
		descSeats:                  prometheus.NewDesc("mypresence_db_seats_total", "Total seats defined across all floor plans.", nil, nil),
		descProjects:               prometheus.NewDesc("mypresence_db_projects_total", "Total projects.", nil, nil),
		descProjEntr:               prometheus.NewDesc("mypresence_db_project_time_entries_total", "Total project time entries stored.", nil, nil),
		descSites:                  prometheus.NewDesc("mypresence_db_sites_total", "Total sites.", nil, nil),
		descSitesCorporate:         prometheus.NewDesc("mypresence_db_sites_corporate_total", "Total corporate sites.", nil, nil),
		descSitesNonCorporate:      prometheus.NewDesc("mypresence_db_sites_non_corporate_total", "Total non-corporate sites.", nil, nil),
		descSiteWorkstations:       prometheus.NewDesc("mypresence_db_site_workstations_total", "Total workstations capacity defined across all sites.", nil, nil),
		descSeatReservations:       prometheus.NewDesc("mypresence_db_seat_reservations_total", "Total seat reservations stored.", nil, nil),
		descReservationsToday:      prometheus.NewDesc("mypresence_db_seat_reservations_today", "Total seat reservations for today.", nil, nil),
		descActiveProjects:         prometheus.NewDesc("mypresence_db_projects_active_total", "Total active projects.", nil, nil),
		descProjectMembers:         prometheus.NewDesc("mypresence_db_project_members_total", "Total project member assignments.", nil, nil),
		descProjectActivities:      prometheus.NewDesc("mypresence_db_project_activities_total", "Total project activities stored.", nil, nil),
		descProjectActivitiesJira:  prometheus.NewDesc("mypresence_db_project_activities_jira_total", "Total Jira project activities.", nil, nil),
		descProjectActivitiesSN:    prometheus.NewDesc("mypresence_db_project_activities_servicenow_total", "Total ServiceNow project activities.", nil, nil),
		descProjectActivitiesOther: prometheus.NewDesc("mypresence_db_project_activities_other_total", "Total other project activities.", nil, nil),
		descCertificationsPres:     prometheus.NewDesc("mypresence_db_certifications_presence_total", "Total certified presence months.", nil, nil),
		descCertificationsProj:     prometheus.NewDesc("mypresence_db_certifications_project_total", "Total certified project months.", nil, nil),
		descDomains:                prometheus.NewDesc("mypresence_db_domains_total", "Total domains.", nil, nil),
		descActivePATs:             prometheus.NewDesc("mypresence_db_personal_access_tokens_active_total", "Currently active Personal Access Tokens.", nil, nil),
		descPasskeys:               prometheus.NewDesc("mypresence_db_passkeys_total", "Total registered WebAuthn / passkey credentials.", nil, nil),
		descNotifications:          prometheus.NewDesc("mypresence_db_notifications_total", "Total notifications stored.", nil, nil),
		descNews:                   prometheus.NewDesc("mypresence_db_news_total", "Total news articles published.", nil, nil),
	}
}

func (c *dbCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.descUsers
	ch <- c.descSessions
	ch <- c.descTeams
	ch <- c.descStatuses
	ch <- c.descPresences
	ch <- c.descFloorplans
	ch <- c.descSeats
	ch <- c.descProjects
	ch <- c.descProjEntr
	ch <- c.descSites
	ch <- c.descSitesCorporate
	ch <- c.descSitesNonCorporate
	ch <- c.descSiteWorkstations
	ch <- c.descSeatReservations
	ch <- c.descReservationsToday
	ch <- c.descActiveProjects
	ch <- c.descProjectMembers
	ch <- c.descProjectActivities
	ch <- c.descProjectActivitiesJira
	ch <- c.descProjectActivitiesSN
	ch <- c.descProjectActivitiesOther
	ch <- c.descCertificationsPres
	ch <- c.descCertificationsProj
	ch <- c.descDomains
	ch <- c.descActivePATs
	ch <- c.descPasskeys
	ch <- c.descNotifications
	ch <- c.descNews
}

func (c *dbCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.fn()
	ch <- prometheus.MustNewConstMetric(c.descUsers, prometheus.GaugeValue, s.Users)
	ch <- prometheus.MustNewConstMetric(c.descSessions, prometheus.GaugeValue, s.ActiveSessions)
	ch <- prometheus.MustNewConstMetric(c.descTeams, prometheus.GaugeValue, s.Teams)
	ch <- prometheus.MustNewConstMetric(c.descStatuses, prometheus.GaugeValue, s.Statuses)
	ch <- prometheus.MustNewConstMetric(c.descPresences, prometheus.GaugeValue, s.Presences)
	ch <- prometheus.MustNewConstMetric(c.descFloorplans, prometheus.GaugeValue, s.Floorplans)
	ch <- prometheus.MustNewConstMetric(c.descSeats, prometheus.GaugeValue, s.Seats)
	ch <- prometheus.MustNewConstMetric(c.descProjects, prometheus.GaugeValue, s.Projects)
	ch <- prometheus.MustNewConstMetric(c.descProjEntr, prometheus.GaugeValue, s.ProjectEntries)
	ch <- prometheus.MustNewConstMetric(c.descSites, prometheus.GaugeValue, s.Sites)
	ch <- prometheus.MustNewConstMetric(c.descSitesCorporate, prometheus.GaugeValue, s.SitesCorporate)
	ch <- prometheus.MustNewConstMetric(c.descSitesNonCorporate, prometheus.GaugeValue, s.SitesNonCorporate)
	ch <- prometheus.MustNewConstMetric(c.descSiteWorkstations, prometheus.GaugeValue, s.SiteWorkstations)
	ch <- prometheus.MustNewConstMetric(c.descSeatReservations, prometheus.GaugeValue, s.SeatReservations)
	ch <- prometheus.MustNewConstMetric(c.descReservationsToday, prometheus.GaugeValue, s.ReservationsToday)
	ch <- prometheus.MustNewConstMetric(c.descActiveProjects, prometheus.GaugeValue, s.ActiveProjects)
	ch <- prometheus.MustNewConstMetric(c.descProjectMembers, prometheus.GaugeValue, s.ProjectMembers)
	ch <- prometheus.MustNewConstMetric(c.descProjectActivities, prometheus.GaugeValue, s.ProjectActivities)
	ch <- prometheus.MustNewConstMetric(c.descProjectActivitiesJira, prometheus.GaugeValue, s.ProjectActivitiesJira)
	ch <- prometheus.MustNewConstMetric(c.descProjectActivitiesSN, prometheus.GaugeValue, s.ProjectActivitiesSN)
	ch <- prometheus.MustNewConstMetric(c.descProjectActivitiesOther, prometheus.GaugeValue, s.ProjectActivitiesOther)
	ch <- prometheus.MustNewConstMetric(c.descCertificationsPres, prometheus.GaugeValue, s.PresenceCertifications)
	ch <- prometheus.MustNewConstMetric(c.descCertificationsProj, prometheus.GaugeValue, s.ProjectCertifications)
	ch <- prometheus.MustNewConstMetric(c.descDomains, prometheus.GaugeValue, s.Domains)
	ch <- prometheus.MustNewConstMetric(c.descActivePATs, prometheus.GaugeValue, s.ActivePATs)
	ch <- prometheus.MustNewConstMetric(c.descPasskeys, prometheus.GaugeValue, s.Passkeys)
	ch <- prometheus.MustNewConstMetric(c.descNotifications, prometheus.GaugeValue, s.Notifications)
	ch <- prometheus.MustNewConstMetric(c.descNews, prometheus.GaugeValue, s.News)
}

// ─── HTTP instrumentation middleware ─────────────────────────────────────────

// numericSegment matches a URL path segment that is a bare integer.
var numericSegment = regexp.MustCompile(`/\d+`)

// normalizePath replaces pure-numeric path segments with {id} to avoid
// label cardinality explosion (e.g. /admin/users/42/logs → /admin/users/{id}/logs).
func normalizePath(path string) string {
	return numericSegment.ReplaceAllString(path, "/{id}")
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Instrument wraps a handler and records HTTP request metrics.
// Paths starting with /static/, /floorplan-img/, /data/, or /metrics itself
// are skipped to avoid noise.
func Instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/metrics" ||
			len(path) >= 8 && path[:8] == "/static/" ||
			len(path) >= 14 && path[:14] == "/floorplan-img" ||
			len(path) >= 6 && path[:6] == "/data/" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		normalized := normalizePath(path)
		statusClass := fmt.Sprintf("%dxx", rec.status/100)
		dur := time.Since(start).Seconds()

		HTTPRequestsTotal.WithLabelValues(r.Method, normalized, statusClass).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, normalized).Observe(dur)
	})
}
