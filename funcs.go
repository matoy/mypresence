package main

import "strconv"

// tmplFmtF formats a float64 for display: whole numbers without decimals, others to 1 decimal place.
func tmplFmtF(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}

// tmplPercentF returns floor(a/b * 100), or 0 if b is zero.
func tmplPercentF(a, b float64) int {
	if b == 0 {
		return 0
	}
	return int(a * 100 / b)
}

// tmplPercent returns a*100/b (integer division), or 0 if b is zero.
func tmplPercent(a, b int) int {
	if b == 0 {
		return 0
	}
	return a * 100 / b
}

// tmplI2F converts an int to float64.
func tmplI2F(i int) float64 { return float64(i) }

// tmplSubF subtracts b from a.
func tmplSubF(a, b float64) float64 { return a - b }

// tmplSumMapF returns the sum of all values in a map[int64]float64.
func tmplSumMapF(m map[int64]float64) float64 {
	total := 0.0
	for _, v := range m {
		total += v
	}
	return total
}

// tmplGetCountF returns the value for key in m, or 0 if absent.
func tmplGetCountF(m map[int64]float64, key int64) float64 { return m[key] }

// tmplGetStrCountF returns the value for key in a map[string]float64, or 0 if absent.
func tmplGetStrCountF(m map[string]float64, key string) float64 { return m[key] }

// tmplPresenceHalf returns the status ID for (date, half) in a nested presence map.
func tmplPresenceHalf(m map[string]map[string]int64, date, half string) int64 {
	if halves, ok := m[date]; ok {
		return halves[half]
	}
	return 0
}

// tmplHasDatePresence reports whether any half-day entry exists for the given date.
func tmplHasDatePresence(m map[string]map[string]int64, date string) bool {
	if halves, ok := m[date]; ok {
		return len(halves) > 0
	}
	return false
}

// tmplActivitySummaryRocket returns true when the achievement criteria are met:
// - not set equals 0 (with tolerance)
// - on-site ratio >= onsiteThreshold (configurable, default 60%)
// - project activity equals 100% (with tolerance)
func tmplActivitySummaryRocket(notSet, onSiteDays, billableDays, projectActivity, onsiteThreshold float64) bool {
	if notSet > 0.001 {
		return false
	}
	if billableDays <= 0 {
		return false
	}
	onSiteRatio := (onSiteDays / billableDays) * 100.0
	if onSiteRatio < onsiteThreshold {
		return false
	}
	return projectActivity >= 99.999 && projectActivity <= 100.001
}
