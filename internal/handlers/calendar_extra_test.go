package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/myPresence/internal/config"
	"github.com/matoy/myPresence/internal/middleware"
	"github.com/matoy/myPresence/internal/models"
)

// -----------------------------------------------------------------------
// SetPresences handler
// -----------------------------------------------------------------------

func TestSetPresences_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}
	req := createAdminReq(t, d, http.MethodPost, "/api/presences", []byte("not-json"))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSetPresences_NoDates(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser("setpr@test.com", "SetPr", "password1")
	tok, _ := d.CreateSession(uid)
	body, _ := json.Marshal(map[string]interface{}{"user_id": uid, "dates": []string{}, "status_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/presences", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty dates, got %d", w.Code)
	}
}

func TestSetPresences_InvalidDate(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser("setpr2@test.com", "SetPr2", "password1")
	tok, _ := d.CreateSession(uid)
	body, _ := json.Marshal(map[string]interface{}{"user_id": uid, "dates": []string{"invalid-date"}, "status_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/presences", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid date, got %d", w.Code)
	}
}

func TestSetPresences_Forbidden(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}
	// basic user trying to set presence for another user
	uid, _ := d.CreateLocalUser("setpr3@test.com", "SetPr3", "password1")
	uid2, _ := d.CreateLocalUser("setpr4@test.com", "SetPr4", "password1")
	tok, _ := d.CreateSession(uid)
	body, _ := json.Marshal(map[string]interface{}{"user_id": uid2, "dates": []string{"2026-06-10"}, "status_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/presences", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestSetPresences_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}
	statusID, _ := d.CreateStatus(models.Status{Name: "TestStatus", Color: "#fff", Billable: false})
	adminUser, _ := d.GetUserByEmail("admin@test.com")
	if adminUser == nil {
		// Create admin user since newExtraTestDB creates a fresh DB
		_ = d
		adminUID, _ := d.CreateLocalUser("admin@test.com", "Admin", "password1")
		d.UpdateUserRoles(adminUID, models.RoleGlobal) //nolint:errcheck
		adminUser, _ = d.GetUserByEmail("admin@test.com")
	}
	body, _ := json.Marshal(map[string]interface{}{"user_id": adminUser.ID, "dates": []string{"2026-07-01"}, "status_id": statusID})
	req := createAdminReq(t, d, http.MethodPost, "/api/presences", body)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ClearPresences handler
// -----------------------------------------------------------------------

func TestClearPresences_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}
	req := createAdminReq(t, d, http.MethodDelete, "/api/presences", []byte("bad"))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ClearPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestClearPresences_Forbidden(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser("clrpr@test.com", "ClrPr", "password1")
	uid2, _ := d.CreateLocalUser("clrpr2@test.com", "ClrPr2", "password1")
	tok, _ := d.CreateSession(uid)
	body, _ := json.Marshal(map[string]interface{}{"user_id": uid2, "dates": []string{"2026-06-10"}})
	req := httptest.NewRequest(http.MethodDelete, "/api/presences", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ClearPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestClearPresences_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}
	uid, _ := d.CreateLocalUser("clearok@test.com", "ClearOK", "password1")
	d.UpdateUserRoles(uid, models.RoleGlobal) //nolint:errcheck
	tok, _ := d.CreateSession(uid)
	body, _ := json.Marshal(map[string]interface{}{
		"user_id": uid, "dates": []string{"2026-07-05"},
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/presences", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ClearPresences)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// GetPresencesAPI handler
// -----------------------------------------------------------------------

func TestGetPresencesAPI_MissingParams(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}
	req := createAdminReq(t, d, http.MethodGet, "/api/presences", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.GetPresencesAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetPresencesAPI_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &CalendarHandler{DB: d, Render: noRender}
	tid, _ := d.CreateTeam("Pres API Team")
	req := createAdminReq(t, d, http.MethodGet, "/api/presences?team_id="+strconvI64(tid)+"&year=2026&month=1", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.GetPresencesAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// ReserveSeat handler
// -----------------------------------------------------------------------

func TestReserveSeat_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}
	uid, _ := d.CreateLocalUser("res@test.com", "Res", "password1")
	tok, _ := d.CreateSession(uid)
	req := httptest.NewRequest(http.MethodPost, "/api/reservations", bytes.NewBufferString("bad"))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ReserveSeat)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestReserveSeat_MissingParams(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}
	uid, _ := d.CreateLocalUser("res2@test.com", "Res2", "password1")
	tok, _ := d.CreateSession(uid)
	body, _ := json.Marshal(map[string]interface{}{"seat_id": 0, "date": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/reservations", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ReserveSeat)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestReserveSeat_NotOnSite(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}
	uid, _ := d.CreateLocalUser("res3@test.com", "Res3", "password1")
	tok, _ := d.CreateSession(uid)
	body, _ := json.Marshal(map[string]interface{}{"seat_id": 1, "date": "2026-06-10"})
	req := httptest.NewRequest(http.MethodPost, "/api/reservations", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ReserveSeat)).ServeHTTP(w, req)
	// User is not on-site → 403
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when not on site, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// SeatsAPI handler
// -----------------------------------------------------------------------

func TestSeatsAPI_MissingParams(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}
	uid, _ := d.CreateLocalUser("seatsapi@test.com", "SeatsAPI", "password1")
	tok, _ := d.CreateSession(uid)
	req := httptest.NewRequest(http.MethodGet, "/api/seats", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SeatsAPI)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSeatsAPI_NotOnSite(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}
	fpID, _ := d.CreateFloorplan("Test FP", 0)
	uid, _ := d.CreateLocalUser("seatsapi2@test.com", "SeatsAPI2", "password1")
	tok, _ := d.CreateSession(uid)
	req := httptest.NewRequest(http.MethodGet, "/api/seats?floorplan_id="+strconvI64(fpID)+"&date=2026-06-10", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SeatsAPI)).ServeHTTP(w, req)
	// User not on site → 200 with on_site:false
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// CancelReservationsByDates handler
// -----------------------------------------------------------------------

func TestCancelReservationsByDates_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}
	uid, _ := d.CreateLocalUser("crbyd@test.com", "CRByD", "password1")
	tok, _ := d.CreateSession(uid)
	req := httptest.NewRequest(http.MethodDelete, "/api/reservations", bytes.NewBufferString("bad"))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CancelReservationsByDates)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCancelReservationsByDates_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &FloorplanHandler{DB: d, DataDir: t.TempDir(), Render: noRender}
	uid, _ := d.CreateLocalUser("crbyd2@test.com", "CRByD2", "password1")
	tok, _ := d.CreateSession(uid)
	body, _ := json.Marshal(map[string]interface{}{"dates": []string{"2026-06-10"}})
	req := httptest.NewRequest(http.MethodDelete, "/api/reservations", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CancelReservationsByDates)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// UploadFloorplanImage handler
// -----------------------------------------------------------------------

func TestUploadFloorplanImage_InvalidExt(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	dataDir := t.TempDir()
	h := &FloorplanHandler{DB: d, DataDir: dataDir, Render: noRender}
	fpID, _ := d.CreateFloorplan("Upload FP", 0)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("image", "test.bmp")
	fw.Write([]byte("fake image data")) //nolint:errcheck
	mw.Close()                          //nolint:errcheck

	req := createAdminReq(t, d, http.MethodPost, "/admin/floorplans/"+strconvI64(fpID)+"/image", buf.Bytes())
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UploadFloorplanImage)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid ext, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// filterTeamsForUser — exercised through ActivityPage with team leader
// -----------------------------------------------------------------------

func TestActivityPage_AsTeamLeader(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ActivityHandler{DB: d, Render: noRender}

	tid, _ := d.CreateTeam("Leader Team")
	uid, _ := d.CreateLocalUser("leader@test.com", "Leader", "password1")
	d.UpdateUserRoles(uid, models.RoleTeamLeader) //nolint:errcheck
	d.AddTeamMember(tid, uid)                     //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	req := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.ActivityPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// LocalLogin — disabled user path
// -----------------------------------------------------------------------

func TestLocalLogin_DisabledUser(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	cfg := &config.Config{AdminUser: "notthisuser@test.com", AdminPassword: "x"}
	h := &AuthHandler{DB: d, Render: noRender, Config: cfg}

	uid, _ := d.CreateLocalUser("disabled@test.com", "Disabled", "password1")
	d.SetUserDisabled(uid, true) //nolint:errcheck

	body := []byte("username=disabled%40test.com&password=password1")
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.LocalLogin(w, req)
	// Disabled user should be redirected to login with error
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc == "/" {
		t.Fatal("disabled user should not be redirected to home")
	}
}

// -----------------------------------------------------------------------
// PAT CreatePAT handler
// -----------------------------------------------------------------------

func TestCreatePAT_Success(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &PATHandler{DB: d, Render: noRender}

	uid, _ := d.CreateLocalUser("pat_create@test.com", "PATCreate", "password1")
	d.UpdateUserRoles(uid, models.RoleTeamManager) //nolint:errcheck
	tok, _ := d.CreateSession(uid)

	body, _ := json.Marshal(map[string]interface{}{"description": "mytoken", "expires_in": 0})
	req := httptest.NewRequest(http.MethodPost, "/api/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: tok})
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreatePAT)).ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusSeeOther {
		t.Fatalf("expected success, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// SetProjectTime handler
// -----------------------------------------------------------------------

func TestSetProjectTime_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}
	req := createAdminReq(t, d, http.MethodPost, "/api/project-time", []byte("bad"))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSetProjectTime_MissingProjectID(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	h := &ProjectsHandler{DB: d, Render: noRender}
	body, _ := json.Marshal(map[string]interface{}{"project_id": 0, "year": 2026, "month": 1, "days": 5.0})
	req := createAdminReq(t, d, http.MethodPost, "/api/project-time", body)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.SetProjectTime)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing project_id, got %d", w.Code)
	}
}
