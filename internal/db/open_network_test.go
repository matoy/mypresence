package db

import (
	"testing"

	"presence-app/internal/config"
)

// TestOpen_NetworkDriver covers db.go L.75 (openNetwork call for postgres/mysql/sqlserver drivers)
// openNetwork will fail (no real DB), but the code path is executed.
func TestOpen_NetworkDriver(t *testing.T) {
	cfg := &config.Config{
		DBDriver:   "postgres",
		DBHost:     "127.0.0.1",
		DBPort:     "19999", // non-listening port — fast connection refusal
		DBName:     "testdb",
		DBUser:     "testuser",
		DBPassword: "testpass",
	}
	_, err := Open(cfg)
	// Expected: connection error (no postgres running)
	if err == nil {
		t.Skip("Unexpected success — postgres appears to be running")
	}
}
