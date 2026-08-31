package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/models"
)

func TestYearMonthFromDate(t *testing.T) {
	y, m, err := yearMonthFromDate("2026-08-24")
	if err != nil || y != 2026 || m != 8 {
		t.Fatalf("yearMonthFromDate valid failed: %d, %d, %v", y, m, err)
	}

	_, _, err = yearMonthFromDate("invalid-date")
	if err == nil {
		t.Fatalf("expected error for invalid date format")
	}
}

func TestProjectDeclarationComplete(t *testing.T) {
	if projectDeclarationComplete(0, 0) {
		t.Errorf("expected false for 0 billable days")
	}
	if projectDeclarationComplete(10, 5) {
		t.Errorf("expected false when declared < billable")
	}
	if !projectDeclarationComplete(10, 10) {
		t.Errorf("expected true when declared == billable")
	}
	if !projectDeclarationComplete(10, 9.9999) {
		t.Errorf("expected true within activityTolerance")
	}
}

func TestRejectIfProjectMonthCertified(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &ProjectsHandler{DB: d}

	uID, _ := d.CreateLocalUser("cert_check@example.com", "Cert Check", "password")

	w1 := httptest.NewRecorder()
	if rejectIfProjectMonthCertified(w1, h, uID, 2026, 8) {
		t.Errorf("expected rejectIfProjectMonthCertified to return false for uncertified month")
	}

	_ = d.CertifyProjectMonth(uID, 2026, 8, uID)

	w2 := httptest.NewRecorder()
	if !rejectIfProjectMonthCertified(w2, h, uID, 2026, 8) {
		t.Errorf("expected rejectIfProjectMonthCertified to return true for certified month")
	}
	if w2.Code != http.StatusLocked {
		t.Errorf("expected 423 Locked, got %d", w2.Code)
	}
}

func TestUserProjectDeclaration_ManualAndStandard(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &ProjectsHandler{DB: d, Config: &config.Config{}}

	uID, _ := d.CreateLocalUser("decl_user@example.com", "Decl User", "pass")

	// Standard mode initially (no manual team)
	billable, declared := h.userProjectDeclaration(uID, 2026, 8)
	if billable != 0 || declared != 0 {
		t.Errorf("expected (0, 0), got (%v, %v)", billable, declared)
	}

	// Add manual team
	tID, _ := d.CreateTeamWithDetails("Manual Team X", "", true, false)
	_ = d.AddTeamMember(tID, uID)

	// Add status and presence for a date
	statusID, _ := d.CreateStatus(models.Status{Name: "Work", Color: "#000000", OnSite: true, Billable: true})
	date := "2026-08-10"
	_ = d.SetPresences(uID, []string{date}, statusID, "full")

	// Add project activity
	_, _ = d.CreateProjectActivity(uID, date, models.ActivityTypeOther, "", "", "Work item", 100.0)

	billableM, declaredM := h.userProjectDeclaration(uID, 2026, 8)
	if billableM != 1.0 || declaredM != 1.0 {
		t.Errorf("expected (1.0, 1.0) in manual mode, got (%v, %v)", billableM, declaredM)
	}
}

func TestCertifyAndDecertifyProjectMonth(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &ProjectsHandler{DB: d, Config: &config.Config{}}

	uID, _ := d.CreateLocalUser("dev@example.com", "Dev User", "pass")
	devUser, _ := d.GetUserByID(uID)

	tlID, _ := d.CreateLocalUser("tl@example.com", "Team Leader", "pass")
	tlUser, _ := d.GetUserByID(tlID)

	adminID, _ := d.CreateLocalUser("admin_cert@example.com", "Admin Cert", "pass")
	_ = d.UpdateUserRoles(adminID, models.RoleGlobal)
	adminUser, _ := d.GetUserByID(adminID)

	teamID, _ := d.CreateTeamWithDetails("Dev Team", "", false, false)
	_ = d.AddTeamMember(teamID, uID)
	_ = d.AddTeamMember(teamID, tlID)
	_ = d.SetTeamLeaders(teamID, []int64{tlID})

	statusID, _ := d.CreateStatus(models.Status{Name: "Work", Color: "#000000", OnSite: true, Billable: true})
	_ = d.SetPresences(uID, []string{"2026-08-10"}, statusID, "full")
	pID, _ := d.CreateProject("Project Alpha", "PA", 0, true, "2026-01-01", "2026-12-31")

	// 1. CertifyProjectMonth - invalid json
	rec := httptest.NewRecorder()
	req := reqWithUser(d, devUser, http.MethodPost, "/api/certify-project", strings.NewReader("bad-json"))
	h.CertifyProjectMonth(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad json, got %d", rec.Code)
	}

	// 2. CertifyProjectMonth - incomplete declaration (422)
	certBody, _ := json.Marshal(map[string]int{"year": 2026, "month": 8})
	rec = httptest.NewRecorder()
	req = reqWithUser(d, devUser, http.MethodPost, "/api/certify-project", bytes.NewReader(certBody))
	h.CertifyProjectMonth(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for incomplete declaration, got %d: %s", rec.Code, rec.Body.String())
	}

	// Declare full time
	_ = d.SetProjectTimeEntry(uID, pID, 2026, 8, 1.0)

	// 3. CertifyProjectMonth - success
	rec = httptest.NewRecorder()
	req = reqWithUser(d, devUser, http.MethodPost, "/api/certify-project", bytes.NewReader(certBody))
	h.CertifyProjectMonth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on CertifyProjectMonth, got %d: %s", rec.Code, rec.Body.String())
	}

	// 4. DecertifyProjectMonth - unauthorized (regular dev user)
	decertBody, _ := json.Marshal(map[string]interface{}{"user_id": uID, "year": 2026, "month": 8})
	rec = httptest.NewRecorder()
	req = reqWithUser(d, devUser, http.MethodPost, "/api/decertify-project", bytes.NewReader(decertBody))
	h.DecertifyProjectMonth(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 when dev user tries to decertify, got %d", rec.Code)
	}

	// 5. DecertifyProjectMonth - team leader of user's team -> success
	rec = httptest.NewRecorder()
	req = reqWithUser(d, tlUser, http.MethodPost, "/api/decertify-project", bytes.NewReader(decertBody))
	h.DecertifyProjectMonth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when team leader decertifies member, got %d: %s", rec.Code, rec.Body.String())
	}

	// 6. DecertifyProjectMonth - global admin -> success
	_ = d.CertifyProjectMonth(uID, 2026, 8, uID)
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPost, "/api/decertify-project", bytes.NewReader(decertBody))
	h.DecertifyProjectMonth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when global admin decertifies, got %d: %s", rec.Code, rec.Body.String())
	}
}
