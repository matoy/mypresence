package handlers

import (
	"testing"

	"github.com/matoy/myPresence/internal/models"
)

func TestPrevNextMonthAndYearHelpers(t *testing.T) {
	if got := prevMonth(1); got != 12 {
		t.Fatalf("prevMonth(1): got %d, want 12", got)
	}
	if got := prevYM(2026, 1); got != 2025 {
		t.Fatalf("prevYM(2026,1): got %d, want 2025", got)
	}
	if got := nextMonth(12); got != 1 {
		t.Fatalf("nextMonth(12): got %d, want 1", got)
	}
	if got := nextYM(2026, 12); got != 2027 {
		t.Fatalf("nextYM(2026,12): got %d, want 2027", got)
	}

	if got := prevMonth(6); got != 5 {
		t.Fatalf("prevMonth(6): got %d, want 5", got)
	}
	if got := prevYM(2026, 6); got != 2026 {
		t.Fatalf("prevYM(2026,6): got %d, want 2026", got)
	}
	if got := nextMonth(6); got != 7 {
		t.Fatalf("nextMonth(6): got %d, want 7", got)
	}
	if got := nextYM(2026, 6); got != 2026 {
		t.Fatalf("nextYM(2026,6): got %d, want 2026", got)
	}
}

func TestContainsCI(t *testing.T) {
	if !containsCI("Project Alpha", "alpha") {
		t.Fatal("expected case-insensitive match")
	}
	if !containsCI("Project Alpha", "") {
		t.Fatal("expected empty substring to match")
	}
	if containsCI("Project Alpha", "beta") {
		t.Fatal("did not expect a non-matching substring")
	}
}

func TestEnrichReportTotals_ComputesPastAndToDate(t *testing.T) {
	rows := []models.ProjectReportRow{
		{
			Project: models.Project{ID: 1, Name: "A"},
			UserRows: []models.ProjectUserMonth{
				{
					User: models.User{ID: 10, Name: "U1"},
					MonthlyDays: map[string]float64{
						"2026-03": 2,
						"2026-04": 3,
						"2026-05": 1,
					},
				},
				{
					User: models.User{ID: 11, Name: "U2"},
					MonthlyDays: map[string]float64{
						"2026-03": 0.5,
						"2026-04": 1.5,
						"2026-05": 4,
					},
				},
			},
		},
	}

	monthKeys := []string{"2026-03", "2026-04", "2026-05"}
	enrichReportTotals(rows, monthKeys)

	if got := rows[0].UserRows[0].TotalPastDays; got != 5 {
		t.Fatalf("user1 TotalPastDays: got %v, want 5", got)
	}
	if got := rows[0].UserRows[0].TotalToDateDays; got != 6 {
		t.Fatalf("user1 TotalToDateDays: got %v, want 6", got)
	}
	if got := rows[0].UserRows[1].TotalPastDays; got != 2 {
		t.Fatalf("user2 TotalPastDays: got %v, want 2", got)
	}
	if got := rows[0].UserRows[1].TotalToDateDays; got != 6 {
		t.Fatalf("user2 TotalToDateDays: got %v, want 6", got)
	}
	if got := rows[0].TotalPastDays; got != 7 {
		t.Fatalf("row TotalPastDays: got %v, want 7", got)
	}
	if got := rows[0].TotalToDateDays; got != 12 {
		t.Fatalf("row TotalToDateDays: got %v, want 12", got)
	}
}
