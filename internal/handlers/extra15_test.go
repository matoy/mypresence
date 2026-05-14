package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/crewjam/saml"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// UploadLogo — os.Create error (DataDir does not exist)
// -----------------------------------------------------------------------

func TestUploadLogo_CreateError(t *testing.T) {
	d := newExtraTestDB(t)
	// DataDir points to a non-existent directory → os.Create will fail
	h := &GeneralSettingsHandler{DataDir: filepath.Join(t.TempDir(), "nonexistent")}
	buf, ct := makePNGUpload(t, "logo.png", minimalPNG)
	req := createAdminReq(t, d, http.MethodPost, "/admin/settings/logo", buf.Bytes())
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UploadLogo)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/settings?error=write_error" {
		t.Errorf("expected write_error redirect, got %q", loc)
	}
}

// -----------------------------------------------------------------------
// DeleteLogo — os.Remove fails with a non-IsNotExist error
// (logo.png exists as a non-empty directory → Remove fails)
// -----------------------------------------------------------------------

func TestDeleteLogo_RemoveError(t *testing.T) {
	d := newExtraTestDB(t)
	dir := t.TempDir()
	// Create a directory named logo.png with a file inside so os.Remove fails
	logoDir := filepath.Join(dir, "logo.png")
	if err := os.MkdirAll(logoDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logoDir, "inner.txt"), []byte("x"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := &GeneralSettingsHandler{DataDir: dir}
	req := createAdminReq(t, d, http.MethodDelete, "/admin/settings/logo", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteLogo)).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when remove fails, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// syncSAMLGroupRoles — UpdateUserRoles fails (DB closed)
// -----------------------------------------------------------------------

func TestSyncSAMLGroupRoles_UpdateUserRolesError(t *testing.T) {
	d := newExtraTestDB(t)
	uid := seedUserInHandlers(t, d, "samlsync_err@test.com")
	user, _ := d.GetUserByID(uid)

	cfg := newSAMLConfig()
	cfg.SAMLGroupGlobal = "admins"
	cfg.SAMLGroupsClaim = "groups"

	h := &AuthHandler{DB: d, Config: cfg}
	a := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{{
			Attributes: []saml.Attribute{{
				Name:   "groups",
				Values: []saml.AttributeValue{{Value: "admins"}},
			}},
		}},
	}
	// Close the DB so UpdateUserRoles fails — should log warning but not panic
	d.Close()
	h.syncSAMLGroupRoles(user, a, user.Email)
	// No panic = pass
}

// -----------------------------------------------------------------------
// CalendarPage — team with members AND DisableFloorplans=false
// (covers GetUserReservationDates for each team member)
// -----------------------------------------------------------------------

func TestCalendarPage_TeamWithMembersFloorplansEnabled(t *testing.T) {
	d := newExtraTestDB(t)

	var rendered string
	h := &CalendarHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}, DisableFloorplans: false}

	leaderUID, _ := d.CreateLocalUser("calteam_leader@test.com", "Leader", "password1")
	d.UpdateUserRoles(leaderUID, models.RoleTeamLeader) //nolint:errcheck

	teamID, _ := d.CreateTeam("CalTeamFP")
	memberUID := seedUserInHandlers(t, d, "calteam_member@test.com")
	d.AddTeamMember(teamID, leaderUID) //nolint:errcheck
	d.AddTeamMember(teamID, memberUID) //nolint:errcheck

	tok, _ := d.CreateSession(leaderUID)
	req := httptest.NewRequest(http.MethodGet, "/calendar?year=2026&month=6", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)
	if rendered != "calendar" {
		t.Errorf("expected calendar page, got %q", rendered)
	}
}

// -----------------------------------------------------------------------
// CalendarPage — team exists but has NO members at the given date
// (covers the "continue" branch when len(members) == 0)
// -----------------------------------------------------------------------

func TestCalendarPage_TeamWithNoActiveMembers(t *testing.T) {
	d := newExtraTestDB(t)

	var rendered string
	h := &CalendarHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}, DisableFloorplans: true}

	uid, _ := d.CreateLocalUser("calteam_nomsb@test.com", "NoMsb", "password1")
	teamID, _ := d.CreateTeam("EmptyTeam")
	d.AddTeamMember(teamID, uid) //nolint:errcheck
	leftAt := "2025-12-31"
	d.SetTeamMemberLeftAt(teamID, uid, &leftAt) //nolint:errcheck

	tok, _ := d.CreateSession(uid)
	req := httptest.NewRequest(http.MethodGet, "/calendar?year=2026&month=1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CalendarPage)).ServeHTTP(w, req)
	if rendered != "calendar" {
		t.Errorf("expected calendar page, got %q", rendered)
	}
}

// -----------------------------------------------------------------------
// UploadFloorplanImage — no file in form
// -----------------------------------------------------------------------

func TestUploadFloorplanImage_NoFile2(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}
	fpID, _ := d.CreateFloorplan("NoFileFP2", 0)

	req := createAdminReq(t, d, http.MethodPost, "/admin/floorplans/"+strconvI64(fpID)+"/image", nil)
	req.SetPathValue("id", strconvI64(fpID))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=nothing")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UploadFloorplanImage)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// UploadFloorplanImage — unsupported extension (e.g. .bmp)
// -----------------------------------------------------------------------

func TestUploadFloorplanImage_UnsupportedExtension(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: t.TempDir()}
	fpID, _ := d.CreateFloorplan("BadExtFP", 0)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("image", "test.bmp")
	fw.Write(minimalPNG) //nolint:errcheck
	mw.Close()           //nolint:errcheck

	req := createAdminReq(t, d, http.MethodPost, "/admin/floorplans/"+strconvI64(fpID)+"/image", body.Bytes())
	req.SetPathValue("id", strconvI64(fpID))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UploadFloorplanImage)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// computeMonthCompletion — all working days declared
// -----------------------------------------------------------------------

func TestComputeMonthCompletion_AllDeclared(t *testing.T) {
	days := []models.DayInfo{
		{Date: "2026-06-01", IsWeekend: false, IsHoliday: false},
		{Date: "2026-06-06", IsWeekend: true},
	}
	pres := map[string]map[string]int64{
		"2026-06-01": {"full": 1},
	}
	declarable, declared, complete := computeMonthCompletion(days, pres)
	if declarable != 1 {
		t.Errorf("expected 1 declarable day, got %d", declarable)
	}
	if declared != 1 {
		t.Errorf("expected 1 declared day, got %d", declared)
	}
	if !complete {
		t.Error("expected month to be complete")
	}
}

// -----------------------------------------------------------------------
// isTeamLeaderOf — positive case: leader and target in same team
// -----------------------------------------------------------------------

func TestIsTeamLeaderOf_SameTeam(t *testing.T) {
	d := newExtraTestDB(t)

	leaderUID := seedUserInHandlers(t, d, "leader_st@test.com")
	d.UpdateUserRoles(leaderUID, models.RoleTeamLeader) //nolint:errcheck
	memberUID := seedUserInHandlers(t, d, "member_st@test.com")

	teamID, _ := d.CreateTeam("SameTeamST")
	d.AddTeamMember(teamID, leaderUID) //nolint:errcheck
	d.AddTeamMember(teamID, memberUID) //nolint:errcheck

	if !isTeamLeaderOf(d, leaderUID, memberUID) {
		t.Error("expected isTeamLeaderOf to return true for same team")
	}
}

// -----------------------------------------------------------------------
// ActivityPage — team member with left_at in the past
// -----------------------------------------------------------------------

func TestActivityPage_TeamWithDepartedMember(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)

	var rendered string
	h := &ActivityHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		rendered = page
	}}

	uid, _ := d.CreateLocalUser("activity_leftat2@test.com", "LeftAt2", "password1")
	d.UpdateUserRoles(uid, models.RoleGlobal) //nolint:errcheck

	teamID, _ := d.CreateTeam("LeftAtTeam2")
	memberUID := seedUserInHandlers(t, d, "member_leftat2@test.com")
	leftAt := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	d.AddTeamMember(teamID, memberUID)                //nolint:errcheck
	d.SetTeamMemberLeftAt(teamID, memberUID, &leftAt) //nolint:errcheck

	req := createAdminReq(t, d, http.MethodGet, "/admin/activity?year=2026&month=6", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if rendered != "admin_activity" {
		t.Errorf("expected admin_activity page, got %q", rendered)
	}
}
