package handlers

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/matoy/myPresence/internal/models"
)

func renderAdminActivityContent(t *testing.T, pageData map[string]interface{}) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	tplPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../web/templates/admin_activity.html"))
	tplBytes, err := os.ReadFile(tplPath)
	if err != nil {
		t.Fatalf("read admin_activity template: %v", err)
	}

	funcMap := buildActivityTemplateFuncMap()

	tmpl, err := template.New("admin_activity.html").Funcs(funcMap).Parse(string(tplBytes))
	if err != nil {
		t.Fatalf("parse admin_activity template: %v", err)
	}

	root := map[string]interface{}{
		"Data": pageData,
		"T":    map[string]string{},
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "content", root); err != nil {
		t.Fatalf("execute admin_activity content template: %v", err)
	}
	return out.String()
}

func baseActivityPageData() map[string]interface{} {
	return map[string]interface{}{
		"Teams":    []models.Team{{ID: 1, Name: "Team A"}},
		"Statuses": []models.Status{{ID: 1, Name: "On Site", Color: "#22c55e"}},
		"Stats": []models.UserStats{{
			User:         models.User{ID: 1, Name: "Alice"},
			StatusCounts: map[int64]float64{1: 20},
			BillableDays: 20,
			OnSiteDays:   13,
		}},
		"ShowProjectActivity":   true,
		"ProjectActivityByUser": map[int64]float64{1: 100},
		"TotalProjectDeclared":  20.0,
		"SelectedTeamID":        int64(1),
		"Year":                  2026,
		"Month":                 5,
		"TotalBillable":         20.0,
		"TotalNotSet":           0.0,
		"TotalOnSite":           13.0,
		"TotalWorkingDays":      20.0,
		"WorkingDays":           22,
		"WorkingDaysExcl":       20,
		"HolidayCount":          2,
		"DayBillable":           map[string]float64{"2026-05-05": 1.0},
		"DayOnSite":             map[string]float64{"2026-05-05": 1.0},
		"StatusTotals":          map[int64]float64{1: 20},
		"PrevYear":              2026,
		"PrevMonth":             4,
		"NextYear":              2026,
		"NextMonth":             6,
		"Days": []models.DayInfo{{
			Day:       5,
			Date:      "2026-05-05",
			DayIndex:  2,
			IsWeekend: false,
			IsHoliday: false,
		}},
		"Users": []models.User{{ID: 1, Name: "Alice"}},
		"PresenceMap": map[int64]map[string]map[string]int64{
			1: {
				"2026-05-05": {"full": 1},
			},
		},
	}
}

func TestAdminActivityTemplate_RendersSummaryAndDailyTables(t *testing.T) {
	html := renderAdminActivityContent(t, baseActivityPageData())

	if !strings.Contains(html, `id="summary-table"`) {
		t.Fatal("summary table should be rendered")
	}
	if !strings.Contains(html, `id="daily-table"`) {
		t.Fatal("daily table should be rendered")
	}
}

func TestAdminActivityTemplate_RocketRule(t *testing.T) {
	data := baseActivityPageData()
	html := renderAdminActivityContent(t, data)

	if c := strings.Count(html, `title="Goal achieved"`); c != 1 {
		t.Fatalf("expected one row rocket marker, got %d", c)
	}

	stats := data["Stats"].([]models.UserStats)
	stats[0].StatusCounts[1] = 19
	data["Stats"] = stats

	html = renderAdminActivityContent(t, data)
	if c := strings.Count(html, `title="Goal achieved"`); c != 0 {
		t.Fatalf("expected no row rocket marker when rule is not met, got %d", c)
	}
}

// buildActivityTemplateFuncMap constructs the template.FuncMap used by the
// admin_activity.html template tests.
func buildActivityTemplateFuncMap() template.FuncMap {
	return template.FuncMap{
		"fmtF": func(f float64) string {
			if f == float64(int64(f)) {
				return strconv.FormatInt(int64(f), 10)
			}
			return strconv.FormatFloat(f, 'f', 1, 64)
		},
		"percentF": func(a, b float64) int {
			if b == 0 {
				return 0
			}
			return int(a * 100 / b)
		},
		"i2f":          func(i int) float64 { return float64(i) },
		"subF":         func(a, b float64) float64 { return a - b },
		"getCountF":    func(m map[int64]float64, key int64) float64 { return m[key] },
		"getStrCountF": func(m map[string]float64, key string) float64 { return m[key] },
		"sumMapF": func(m map[int64]float64) float64 {
			total := 0.0
			for _, v := range m {
				total += v
			}
			return total
		},
		"presenceHalf": func(m map[string]map[string]int64, date, half string) int64 {
			if halves, ok := m[date]; ok {
				return halves[half]
			}
			return 0
		},
		"statusColor": func(statuses []models.Status, id int64) string {
			for _, s := range statuses {
				if s.ID == id {
					return s.Color
				}
			}
			return "#e5e7eb"
		},
		"statusName": func(statuses []models.Status, id int64) string {
			for _, s := range statuses {
				if s.ID == id {
					return s.Name
				}
			}
			return ""
		},
		"activityRocket": testActivityRocket,
	}
}

// testActivityRocket mirrors the production activityRocket template function
// with a fixed 60% on-site threshold (used in template tests).
func testActivityRocket(notSet, onSiteDays, billableDays, projectActivity float64) bool {
	if notSet > 0.001 || billableDays <= 0 {
		return false
	}
	if (onSiteDays/billableDays)*100.0 <= 60.0 {
		return false
	}
	return projectActivity >= 99.999 && projectActivity <= 100.001
}
