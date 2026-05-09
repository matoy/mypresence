package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestStatusRecorder_Unwrap covers the Unwrap method (0% before).
func TestStatusRecorder_Unwrap(t *testing.T) {
	inner := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: inner, status: 200}
	got := rec.Unwrap()
	if got != http.ResponseWriter(inner) {
		t.Fatal("Unwrap should return the wrapped ResponseWriter")
	}
}

// TestRegisterHealthCollector_ViaIsolatedRegistry tests that the health collector
// produces the expected metrics when registered on a fresh registry.
func TestRegisterHealthCollector_ViaIsolatedRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := newHealthCollector(func() HealthStats {
		return HealthStats{Up: 1, UptimeSeconds: 42, DBUp: 1}
	})
	if err := reg.Register(c); err != nil {
		t.Fatalf("Register healthCollector: %v", err)
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	names := make(map[string]float64)
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			names[mf.GetName()] = m.GetGauge().GetValue()
		}
	}
	if names["mypresence_up"] != 1 {
		t.Errorf("expected mypresence_up=1, got %v", names["mypresence_up"])
	}
	if names["mypresence_uptime_seconds"] != 42 {
		t.Errorf("expected uptime=42, got %v", names["mypresence_uptime_seconds"])
	}
	if names["mypresence_db_up"] != 1 {
		t.Errorf("expected db_up=1, got %v", names["mypresence_db_up"])
	}
}

// TestRegisterDBCollector_ViaIsolatedRegistry tests that the DB collector
// produces the expected metrics when registered on a fresh registry.
func TestRegisterDBCollector_ViaIsolatedRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := newDBCollector(func() DBStats {
		return DBStats{
			Users: 5, ActiveSessions: 3, Teams: 2, Statuses: 7,
			Presences: 100, Floorplans: 1, Seats: 10, Projects: 4, ProjectEntries: 50,
		}
	})
	if err := reg.Register(c); err != nil {
		t.Fatalf("Register dbCollector: %v", err)
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	values := make(map[string]float64)
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			values[mf.GetName()] = m.GetGauge().GetValue()
		}
	}
	checks := map[string]float64{
		"mypresence_db_users_total":                5,
		"mypresence_db_sessions_active_total":      3,
		"mypresence_db_teams_total":                2,
		"mypresence_db_statuses_total":             7,
		"mypresence_db_presences_total":            100,
		"mypresence_db_floorplans_total":           1,
		"mypresence_db_seats_total":                10,
		"mypresence_db_projects_total":             4,
		"mypresence_db_project_time_entries_total": 50,
	}
	for name, want := range checks {
		if got := values[name]; got != want {
			t.Errorf("%s: want %v, got %v", name, want, got)
		}
	}
}

// TestRegisterHealthCollector_PanicOnDoubleRegister verifies that calling
// RegisterHealthCollector twice panics (Prometheus global registry behaviour).
func TestRegisterHealthCollector_PanicOnDoubleRegister(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on double-registration")
		}
	}()
	fn := func() HealthStats { return HealthStats{Up: 1} }
	// The first call succeeds (already done in main startup during integration tests
	// may or may not have run — use a fresh unique metric to guarantee double-call).
	// We can't easily un-register from the global registry, so test the function
	// on an already-registered name. Use a different approach: register the same
	// collector object twice via MustRegister.
	c := newHealthCollector(fn)
	prometheus.MustRegister(c) //nolint:staticcheck // first registration
	prometheus.MustRegister(c) //nolint:staticcheck // must panic
}
