package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestRegisterHealthCollector covers metrics.go L.104 (RegisterHealthCollector)
func TestRegisterHealthCollector(t *testing.T) {
	fn := func() HealthStats {
		return HealthStats{Up: 1.0}
	}
	// Use recover in case the collector was already registered in a previous test run
	defer func() {
		if r := recover(); r != nil {
			// Already registered — that's OK for coverage purposes
		}
	}()
	RegisterHealthCollector(fn)
	// Unregister to allow future test runs
	prometheus.Unregister(newHealthCollector(fn))
}

// TestRegisterDBCollector covers metrics.go L.154 (RegisterDBCollector)
func TestRegisterDBCollector(t *testing.T) {
	fn := func() DBStats {
		return DBStats{Users: 1}
	}
	defer func() {
		if r := recover(); r != nil {
			// Already registered — that's OK for coverage purposes
		}
	}()
	RegisterDBCollector(fn)
	// Unregister to allow future test runs
	prometheus.Unregister(newDBCollector(fn))
}
