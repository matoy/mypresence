package config

import (
	"testing"
)

// TestGetEnvFloat_ValidValue covers the successful parse branch (50% → 100%).
func TestGetEnvFloat_ValidValue(t *testing.T) {
	t.Setenv("ONSITE_RATIO_THRESHOLD", "75.5")
	c := Load()
	if c.OnsiteRatioThreshold != 75.5 {
		t.Errorf("OnsiteRatioThreshold: want 75.5, got %v", c.OnsiteRatioThreshold)
	}
}

// TestGetEnvFloat_InvalidValue covers the parse-error path (returns fallback).
func TestGetEnvFloat_InvalidValue(t *testing.T) {
	t.Setenv("ONSITE_RATIO_THRESHOLD", "notanumber")
	c := Load()
	// Should fall back to the default 60.0
	if c.OnsiteRatioThreshold != 60.0 {
		t.Errorf("OnsiteRatioThreshold: want fallback 60.0, got %v", c.OnsiteRatioThreshold)
	}
}
