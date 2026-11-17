package handlers

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/i18n"
	"github.com/matoy/mypresence/internal/models"
)

func TestAdminLockerHandlers_CRUDAndNotifications(t *testing.T) {
	d := newCRUDTestDB(t)

	adminID, _ := d.CreateLocalUser("fpadmin@test.com", "FP Admin", "pass1234")
	_ = d.UpdateUserRoles(adminID, models.RoleFloorplanManager)
	adminUser, _ := d.GetUserByID(adminID)

	userID, _ := d.CreateLocalUser("collab@test.com", "Alice Collab", "pass1234")
	otherUserID, _ := d.CreateLocalUser("other@test.com", "Bob OtherSite", "pass1234")
	siteID, _ := d.CreateSite(models.Site{Name: "Paris", CountryCode: "FR"})
	otherSiteID, _ := d.CreateSite(models.Site{Name: "Lyon", CountryCode: "FR"})
	_ = d.UpdateUserSite(userID, siteID)
	_ = d.UpdateUserSite(otherUserID, otherSiteID)
	fpID, _ := d.CreateFloorplan("RDC", 0)

	h := &FloorplanHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		},
	}

	// 0. CreateLocker assigning a user from another site fails with 400
	{
		body, _ := json.Marshal(map[string]interface{}{
			"site_id": siteID,
			"number":  "ERR-01",
			"user_id": otherUserID,
		})
		rec := httptest.NewRecorder()
		req := reqWithUser(d, adminUser, http.MethodPost, "/api/admin/lockers", bytes.NewReader(body))
		h.CreateLocker(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 when assigning user from another site, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 1. CreateLocker with assignment sends notification
	{
		body, _ := json.Marshal(map[string]interface{}{
			"site_id": siteID,
			"number":  "A-01",
			"zone":    "Hall",
			"status":  "active",
			"user_id": userID,
			"code":    "1234",
		})
		rec := httptest.NewRecorder()
		req := reqWithUser(d, adminUser, http.MethodPost, "/api/admin/lockers", bytes.NewReader(body))
		h.CreateLocker(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("CreateLocker expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		notifs, err := d.GetUnreadNotifications(userID)
		if err != nil || len(notifs) != 1 || notifs[0].Type != "locker_assigned" {
			t.Fatalf("expected 1 locker_assigned notification, got %+v (err=%v)", notifs, err)
		}
	}

	// 2. Duplicate locker number on same site returns 409 Conflict
	{
		body, _ := json.Marshal(map[string]interface{}{
			"site_id": siteID,
			"number":  "A-01",
		})
		rec := httptest.NewRecorder()
		req := reqWithUser(d, adminUser, http.MethodPost, "/api/admin/lockers", bytes.NewReader(body))
		h.CreateLocker(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 for duplicate locker, got %d", rec.Code)
		}
	}

	// 3. BulkCreateLockers
	{
		body, _ := json.Marshal(map[string]interface{}{
			"site_id":   siteID,
			"prefix":    "A-",
			"start_num": 1,
			"end_num":   5,
			"pad_width": 2,
			"zone":      "Hall",
		})
		rec := httptest.NewRecorder()
		req := reqWithUser(d, adminUser, http.MethodPost, "/api/admin/lockers/bulk", bytes.NewReader(body))
		h.BulkCreateLockers(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("BulkCreateLockers expected 200, got %d", rec.Code)
		}
		var out struct {
			Created int `json:"created"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out.Created != 4 {
			t.Errorf("expected 4 created (A-01 existed), got %d", out.Created)
		}
	}

	// 4. UpdateLocker to unassigned preserves code (either existing when empty or newly passed)
	{
		lockers, _ := d.ListLockers(siteID)
		var targetID int64
		for _, l := range lockers {
			if l.Number == "A-01" {
				targetID = l.ID
				break
			}
		}
		// Empty code preserves "1234"
		body, _ := json.Marshal(map[string]interface{}{
			"number":  "A-01",
			"zone":    "Hall Nord",
			"status":  "active",
			"user_id": 0,
			"code":    "",
		})
		rec := httptest.NewRecorder()
		req := reqWithUser(d, adminUser, http.MethodPut, "/api/admin/lockers/"+strconv.FormatInt(targetID, 10), bytes.NewReader(body))
		req.SetPathValue("id", strconv.FormatInt(targetID, 10))
		h.UpdateLocker(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("UpdateLocker expected 200, got %d", rec.Code)
		}
		updated, _ := d.GetLocker(targetID)
		if updated.UserID != 0 || updated.Code != "1234" {
			t.Errorf("expected unassigned locker preserving code '1234', got user=%d code=%q", updated.UserID, updated.Code)
		}

		// Explicit code while unassigned updates/keeps it
		bodyKeep, _ := json.Marshal(map[string]interface{}{
			"number":  "A-01",
			"zone":    "Hall Nord",
			"status":  "active",
			"user_id": 0,
			"code":    "should-be-kept",
		})
		recKeep := httptest.NewRecorder()
		reqKeep := reqWithUser(d, adminUser, http.MethodPut, "/api/admin/lockers/"+strconv.FormatInt(targetID, 10), bytes.NewReader(bodyKeep))
		reqKeep.SetPathValue("id", strconv.FormatInt(targetID, 10))
		h.UpdateLocker(recKeep, reqKeep)
		if recKeep.Code != http.StatusOK {
			t.Fatalf("UpdateLocker explicit code expected 200, got %d", recKeep.Code)
		}
		updatedKeep, _ := d.GetLocker(targetID)
		if updatedKeep.UserID != 0 || updatedKeep.Code != "should-be-kept" {
			t.Errorf("expected unassigned locker keeping explicit code 'should-be-kept', got user=%d code=%q", updatedKeep.UserID, updatedKeep.Code)
		}

		// Changing site_id to nonexistent site fails with 404
		bodyMissingSite, _ := json.Marshal(map[string]interface{}{
			"site_id": 999999,
			"number":  "A-01",
			"status":  "active",
		})
		recMissing := httptest.NewRecorder()
		reqMissing := reqWithUser(d, adminUser, http.MethodPut, "/api/admin/lockers/"+strconv.FormatInt(targetID, 10), bytes.NewReader(bodyMissingSite))
		reqMissing.SetPathValue("id", strconv.FormatInt(targetID, 10))
		h.UpdateLocker(recMissing, reqMissing)
		if recMissing.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for nonexistent target site, got %d", recMissing.Code)
		}

		// Changing site_id while assigning a user from the old site fails with 400
		bodyWrongUser, _ := json.Marshal(map[string]interface{}{
			"site_id": otherSiteID,
			"number":  "A-01",
			"status":  "active",
			"user_id": userID,
		})
		recWrongUser := httptest.NewRecorder()
		reqWrongUser := reqWithUser(d, adminUser, http.MethodPut, "/api/admin/lockers/"+strconv.FormatInt(targetID, 10), bytes.NewReader(bodyWrongUser))
		reqWrongUser.SetPathValue("id", strconv.FormatInt(targetID, 10))
		h.UpdateLocker(recWrongUser, reqWrongUser)
		if recWrongUser.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 when assigning old-site user after moving site, got %d", recWrongUser.Code)
		}

		// Moving to otherSiteID with matching user succeeds and preserves code
		bodyMove, _ := json.Marshal(map[string]interface{}{
			"site_id": otherSiteID,
			"number":  "LY-01",
			"zone":    "Sud",
			"status":  "active",
			"user_id": otherUserID,
		})
		recMove := httptest.NewRecorder()
		reqMove := reqWithUser(d, adminUser, http.MethodPut, "/api/admin/lockers/"+strconv.FormatInt(targetID, 10), bytes.NewReader(bodyMove))
		reqMove.SetPathValue("id", strconv.FormatInt(targetID, 10))
		h.UpdateLocker(recMove, reqMove)
		if recMove.Code != http.StatusOK {
			t.Fatalf("expected 200 when moving locker to otherSiteID, got %d: %s", recMove.Code, recMove.Body.String())
		}
		moved, _ := d.GetLocker(targetID)
		if moved.SiteID != otherSiteID || moved.Number != "LY-01" || moved.UserID != otherUserID || moved.Code != "should-be-kept" {
			t.Errorf("unexpected moved locker state: %+v", moved)
		}

		// Moving another locker from siteID to otherSiteID with duplicate number LY-01 fails with 409
		var secondID int64
		for _, l := range lockers {
			if l.ID != targetID {
				secondID = l.ID
				break
			}
		}
		bodyConflict, _ := json.Marshal(map[string]interface{}{
			"site_id": otherSiteID,
			"number":  "ly-01",
			"status":  "active",
		})
		recConflict := httptest.NewRecorder()
		reqConflict := reqWithUser(d, adminUser, http.MethodPut, "/api/admin/lockers/"+strconv.FormatInt(secondID, 10), bytes.NewReader(bodyConflict))
		reqConflict.SetPathValue("id", strconv.FormatInt(secondID, 10))
		h.UpdateLocker(recConflict, reqConflict)
		if recConflict.Code != http.StatusConflict {
			t.Fatalf("expected 409 for duplicate number on target site, got %d", recConflict.Code)
		}
	}

	// 5. Floorplan locker range CRUD
	{
		body, _ := json.Marshal(map[string]interface{}{
			"label": "A-01..A-05",
			"zone":  "Hall",
			"x_pct": 12.5,
			"y_pct": 88.0,
		})
		rec := httptest.NewRecorder()
		req := reqWithUser(d, adminUser, http.MethodPost, "/api/admin/floorplans/1/locker-ranges", bytes.NewReader(body))
		req.SetPathValue("id", strconv.FormatInt(fpID, 10))
		h.CreateLockerRange(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("CreateLockerRange expected 200, got %d", rec.Code)
		}
		var created struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &created)

		// Partial update (drag only) preserves label and zone
		dragBody, _ := json.Marshal(map[string]interface{}{"x_pct": 20.0, "y_pct": 75.0})
		rec2 := httptest.NewRecorder()
		req2 := reqWithUser(d, adminUser, http.MethodPut, "/api/admin/locker-ranges/1", bytes.NewReader(dragBody))
		req2.SetPathValue("id", strconv.FormatInt(created.ID, 10))
		h.UpdateLockerRange(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("UpdateLockerRange expected 200, got %d", rec2.Code)
		}
		got, _ := d.GetFloorplanLockerRange(created.ID)
		if got.Label != "A-01..A-05" || got.XPct != 20.0 {
			t.Errorf("unexpected updated range: %+v", got)
		}

		// Delete range
		rec3 := httptest.NewRecorder()
		req3 := reqWithUser(d, adminUser, http.MethodDelete, "/api/admin/locker-ranges/1", nil)
		req3.SetPathValue("id", strconv.FormatInt(created.ID, 10))
		h.DeleteLockerRange(rec3, req3)
		if rec3.Code != http.StatusOK {
			t.Fatalf("DeleteLockerRange expected 200, got %d", rec3.Code)
		}
	}
}

func TestUserLockerSelfServiceHandlers(t *testing.T) {
	d := newCRUDTestDB(t)

	u1ID, _ := d.CreateLocalUser("u1lock@test.com", "User One", "pass1234")
	u2ID, _ := d.CreateLocalUser("u2lock@test.com", "User Two", "pass1234")
	u3ID, _ := d.CreateLocalUser("u3other@test.com", "User Three", "pass1234")

	siteID, _ := d.CreateSite(models.Site{Name: "HQ", CountryCode: "FR"})
	otherSiteID, _ := d.CreateSite(models.Site{Name: "Remote", CountryCode: "FR"})
	_ = d.UpdateUserSite(u1ID, siteID)
	_ = d.UpdateUserSite(u2ID, siteID)
	_ = d.UpdateUserSite(u3ID, otherSiteID)

	u1, _ := d.GetUserByID(u1ID)
	u2, _ := d.GetUserByID(u2ID)
	u3, _ := d.GetUserByID(u3ID)

	l1ID, _ := d.CreateLocker(models.Locker{SiteID: siteID, Number: "101", Zone: "RDC"})
	l2ID, _ := d.CreateLocker(models.Locker{SiteID: siteID, Number: "102", Zone: "RDC"})

	h := &FloorplanHandler{DB: d}

	// u3 (assigned to another site) cannot claim l1 -> 403 Forbidden
	{
		rec := httptest.NewRecorder()
		req := reqWithUser(d, u3, http.MethodPost, "/api/lockers/1/claim", bytes.NewReader([]byte(`{}`)))
		req.SetPathValue("id", strconv.FormatInt(l1ID, 10))
		h.ClaimLockerAPI(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for wrong site user, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// u1 claims l1 with physical code
	{
		body, _ := json.Marshal(map[string]string{"code": "7788"})
		rec := httptest.NewRecorder()
		req := reqWithUser(d, u1, http.MethodPost, "/api/lockers/1/claim", bytes.NewReader(body))
		req.SetPathValue("id", strconv.FormatInt(l1ID, 10))
		h.ClaimLockerAPI(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("ClaimLockerAPI expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// u1 cannot claim a second locker on same site
	{
		rec := httptest.NewRecorder()
		req := reqWithUser(d, u1, http.MethodPost, "/api/lockers/2/claim", bytes.NewReader([]byte(`{}`)))
		req.SetPathValue("id", strconv.FormatInt(l2ID, 10))
		h.ClaimLockerAPI(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 for second locker on same site, got %d", rec.Code)
		}
	}

	// u2 cannot claim l1
	{
		rec := httptest.NewRecorder()
		req := reqWithUser(d, u2, http.MethodPost, "/api/lockers/1/claim", bytes.NewReader([]byte(`{}`)))
		req.SetPathValue("id", strconv.FormatInt(l1ID, 10))
		h.ClaimLockerAPI(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 for already taken locker, got %d", rec.Code)
		}
	}

	// u2 lists available lockers: sees only l2, never u1's code
	{
		rec := httptest.NewRecorder()
		req := reqWithUser(d, u2, http.MethodGet, "/api/lockers?site_id="+strconv.FormatInt(siteID, 10), nil)
		h.ListUserLockersAPI(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("ListUserLockersAPI expected 200, got %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "7788") {
			t.Fatalf("response leaked u1's locker code!")
		}
		var out struct {
			Available []models.Locker `json:"available"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if len(out.Available) != 1 || out.Available[0].ID != l2ID {
			t.Errorf("expected 1 available locker (l2), got %+v", out.Available)
		}
	}

	// u1 updates code and then releases; code '9900' is preserved and inherited when u2 claims with {}
	{
		rec := httptest.NewRecorder()
		req := reqWithUser(d, u1, http.MethodPut, "/api/lockers/1/code", bytes.NewReader([]byte(`{"code":"9900"}`)))
		req.SetPathValue("id", strconv.FormatInt(l1ID, 10))
		h.UpdateMyLockerCodeAPI(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("UpdateMyLockerCodeAPI expected 200, got %d", rec.Code)
		}

		// u2 tries to update u1's locker code -> 404
		recForb := httptest.NewRecorder()
		reqForb := reqWithUser(d, u2, http.MethodPut, "/api/lockers/1/code", bytes.NewReader([]byte(`{"code":"0000"}`)))
		reqForb.SetPathValue("id", strconv.FormatInt(l1ID, 10))
		h.UpdateMyLockerCodeAPI(recForb, reqForb)
		if recForb.Code != http.StatusNotFound {
			t.Fatalf("expected 404 when non-holder updates code, got %d", recForb.Code)
		}

		// u1 releases -> code preserved
		recRel := httptest.NewRecorder()
		reqRel := reqWithUser(d, u1, http.MethodPost, "/api/lockers/1/release", nil)
		reqRel.SetPathValue("id", strconv.FormatInt(l1ID, 10))
		h.ReleaseMyLockerAPI(recRel, reqRel)
		if recRel.Code != http.StatusOK {
			t.Fatalf("ReleaseMyLockerAPI expected 200, got %d", recRel.Code)
		}
		got, _ := d.GetLocker(l1ID)
		if got.UserID != 0 || got.Code != "9900" {
			t.Errorf("expected unassigned locker preserving code '9900', got %+v", got)
		}

		// u2 claims l1 with empty body -> inherits preserved code '9900'
		recReclaim := httptest.NewRecorder()
		reqReclaim := reqWithUser(d, u2, http.MethodPost, "/api/lockers/1/claim", bytes.NewReader([]byte(`{}`)))
		reqReclaim.SetPathValue("id", strconv.FormatInt(l1ID, 10))
		h.ClaimLockerAPI(recReclaim, reqReclaim)
		if recReclaim.Code != http.StatusOK {
			t.Fatalf("ClaimLockerAPI reclaim expected 200, got %d: %s", recReclaim.Code, recReclaim.Body.String())
		}
		gotReclaimed, _ := d.GetLocker(l1ID)
		if gotReclaimed.UserID != u2ID || gotReclaimed.Code != "9900" {
			t.Errorf("expected u2 holding l1 with preserved code '9900', got %+v", gotReclaimed)
		}
	}
}

func TestAdminLockersAndFloorplanTemplates_Render(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	funcMap := template.FuncMap{
		"json": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"safehtml": func(s string) template.HTML {
			return template.HTML(s)
		},
	}

	// 1. admin_lockers.html
	{
		p := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../web/templates/admin_lockers.html"))
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read admin_lockers.html: %v", err)
		}
		tmpl, err := template.New("admin_lockers.html").Funcs(funcMap).Parse(string(raw))
		if err != nil {
			t.Fatalf("parse admin_lockers.html: %v", err)
		}
		var out bytes.Buffer
		err = tmpl.ExecuteTemplate(&out, "content", models.PageData{
			User: &models.User{ID: 1, Name: "Admin", Roles: models.RoleGlobal},
			T:    i18n.T("fr"),
			Data: map[string]interface{}{
				"Sites":          []*models.Site{{ID: 1, Name: "Paris"}},
				"Lockers":        []models.Locker{{ID: 1, SiteID: 1, SiteName: "Paris", Number: "A-01", Zone: "Hall", Status: "active", FloorplanID: 7, FloorplanName: "RDC"}},
				"Users":          []models.User{},
				"SelectedSiteID": int64(0),
				"FloorplanZones": map[int64][]string{1: {"Hall"}},
			},
		})
		if err != nil {
			t.Fatalf("execute admin_lockers.html: %v", err)
		}
		if !strings.Contains(out.String(), "lockersAdmin") || !strings.Contains(out.String(), "A-01") {
			t.Errorf("expected rendered locker page content")
		}
	}

	// 2. floorplan.html with LockerRanges
	{
		p := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../web/templates/floorplan.html"))
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read floorplan.html: %v", err)
		}
		tmpl, err := template.New("floorplan.html").Funcs(funcMap).Parse(string(raw))
		if err != nil {
			t.Fatalf("parse floorplan.html: %v", err)
		}
		fp := &models.Floorplan{ID: 1, Name: "2nd Floor", ImagePath: "plan.png"}
		var out bytes.Buffer
		err = tmpl.ExecuteTemplate(&out, "content", models.PageData{
			User: &models.User{ID: 1, Name: "User"},
			T:    i18n.T("fr"),
			Data: map[string]interface{}{
				"Floorplans": []models.Floorplan{*fp},
				"CurrentFP":  fp,
				"Seats":      []models.SeatWithStatus{},
				"LockerRanges": []models.FloorplanLockerRange{
					{ID: 1, FloorplanID: 1, SiteID: 1, Label: "001-050", Zone: "Nord", XPct: 15, YPct: 25, TotalCount: 50, AvailableCount: 12},
				},
				"UserLockers": []models.Locker{},
				"Date":        "2026-11-16",
				"Half":        "full",
				"IsOnSite":    true,
				"IsFavorite":  false,
			},
		})
		if err != nil {
			t.Fatalf("execute floorplan.html: %v", err)
		}
		html := out.String()
		if !strings.Contains(html, "001-050") || !strings.Contains(html, "12/50") {
			t.Errorf("expected locker range label 001-050 and count 12/50 in floorplan.html")
		}
	}
}

func TestFloorplanAndAdminLockersPages_PassLinkedData(t *testing.T) {
	d := newCRUDTestDB(t)

	adminID, _ := d.CreateLocalUser("fpadm@test.com", "Admin", "pass1234")
	_ = d.UpdateUserRoles(adminID, models.RoleFloorplanManager)
	adminUser, _ := d.GetUserByID(adminID)

	siteID, _ := d.CreateSite(models.Site{Name: "Paris", CountryCode: "FR"})
	fpID, _ := d.CreateFloorplanWithSite("RDC", siteID, 1)
	_, _ = d.CreateFloorplanLockerRange(models.FloorplanLockerRange{
		FloorplanID: fpID,
		Label:       "001-020",
		Zone:        "Vestiaire RDC",
		XPct:        50,
		YPct:        50,
	})
	_, _ = d.CreateLocker(models.Locker{SiteID: siteID, Number: "005", Zone: "Vestiaire RDC"})

	var lastData map[string]interface{}
	h := &FloorplanHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			lastData, _ = data.(map[string]interface{})
		},
	}

	// AdminLockersPage passes FloorplanZones
	recAdmin := httptest.NewRecorder()
	reqAdmin := reqWithUser(d, adminUser, http.MethodGet, "/admin/lockers", nil)
	h.AdminLockersPage(recAdmin, reqAdmin)
	fpZones, ok := lastData["FloorplanZones"].(map[int64][]string)
	if !ok || len(fpZones[siteID]) != 1 || fpZones[siteID][0] != "Vestiaire RDC" {
		t.Errorf("expected FloorplanZones[siteID]=[Vestiaire RDC], got %+v", lastData["FloorplanZones"])
	}

	// FloorplanPage passes LockerRanges with counts and UserLockers
	recFP := httptest.NewRecorder()
	reqFP := reqWithUser(d, adminUser, http.MethodGet, "/floorplan?floorplan="+strconv.FormatInt(fpID, 10), nil)
	h.FloorplanPage(recFP, reqFP)
	ranges, ok := lastData["LockerRanges"].([]models.FloorplanLockerRange)
	if !ok || len(ranges) != 1 || ranges[0].TotalCount != 1 || ranges[0].AvailableCount != 1 {
		t.Errorf("expected enriched LockerRanges with Total=1 Avail=1, got %+v", lastData["LockerRanges"])
	}
	if _, ok := lastData["UserLockers"].([]models.Locker); !ok {
		t.Errorf("expected UserLockers slice in FloorplanPage data")
	}
}

