package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

func TestSitesAPI_CRUDAndAccessControl(t *testing.T) {
	d := newCRUDTestDB(t)
	var renderedPage string
	var renderedData map[string]interface{}
	mockRender := func(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
		renderedPage = name
		if m, ok := data.(map[string]interface{}); ok {
			renderedData = m
		}
		w.WriteHeader(http.StatusOK)
	}

	h := &FloorplanHandler{
		DB:     d,
		Render: mockRender,
	}

	// Create floorplans to link
	fp1, err := d.CreateFloorplan("1st Floor", 0)
	if err != nil {
		t.Fatalf("CreateFloorplan: %v", err)
	}
	fp2, err := d.CreateFloorplan("2nd Floor", 1)
	if err != nil {
		t.Fatalf("CreateFloorplan: %v", err)
	}

	// 1. Access Control: Basic user forbidden on POST /api/admin/sites
	reqForbidden := createAuthedReq(t, d, http.MethodPost, "/api/admin/sites",
		"basic_site_user@test.com", "Basic User", "password123", models.RoleBasic,
		[]byte(`{"name":"Site Forbidden"}`))
	wForbidden := httptest.NewRecorder()
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.CreateSite))).ServeHTTP(wForbidden, reqForbidden)
	if wForbidden.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for basic user, got %d", wForbidden.Code)
	}

	// 2. Validation: Empty name -> 400
	reqEmptyName := createAuthedReq(t, d, http.MethodPost, "/api/admin/sites",
		"fp_admin@test.com", "FP Admin", "password123", models.RoleFloorplanManager,
		[]byte(`{"name":"   ", "country_code":"FR"}`))
	wEmptyName := httptest.NewRecorder()
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.CreateSite))).ServeHTTP(wEmptyName, reqEmptyName)
	if wEmptyName.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d", wEmptyName.Code)
	}

	// 3. CreateSite: Floorplan Manager creates site with floors
	createPayload := map[string]interface{}{
		"name":               "HQ Troyes",
		"country_code":       "FR",
		"not_corporate_site": false,
		"floorplan_ids":      []int64{fp1},
	}
	createBody, _ := json.Marshal(createPayload)
	reqCreate := createAuthedReq(t, d, http.MethodPost, "/api/admin/sites",
		"fp_admin2@test.com", "FP Admin 2", "password123", models.RoleFloorplanManager,
		createBody)
	wCreate := httptest.NewRecorder()
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.CreateSite))).ServeHTTP(wCreate, reqCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("expected 200 on create, got %d: %s", wCreate.Code, wCreate.Body.String())
	}
	var createRes map[string]interface{}
	if err := json.Unmarshal(wCreate.Body.Bytes(), &createRes); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	siteID := int64(createRes["id"].(float64))
	if siteID <= 0 {
		t.Fatalf("invalid created site ID %d", siteID)
	}

	// 3b. CreateSite: duplicate name should be rejected with 409 Conflict
	wDup := httptest.NewRecorder()
	reqDup := createAuthedReq(t, d, http.MethodPost, "/api/admin/sites",
		"fp_admin2b@test.com", "FP Admin 2b", "password123", models.RoleFloorplanManager,
		createBody)
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.CreateSite))).ServeHTTP(wDup, reqDup)
	if wDup.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate site name, got %d: %s", wDup.Code, wDup.Body.String())
	}

	// 4. AdminListSites
	reqList := createAuthedReq(t, d, http.MethodGet, "/api/admin/sites",
		"fp_admin3@test.com", "FP Admin 3", "password123", models.RoleFloorplanManager,
		nil)
	wList := httptest.NewRecorder()
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.AdminListSites))).ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 on list sites, got %d", wList.Code)
	}
	var sitesList []*models.Site
	if err := json.Unmarshal(wList.Body.Bytes(), &sitesList); err != nil {
		t.Fatalf("unmarshal sites list: %v", err)
	}
	if len(sitesList) != 1 || sitesList[0].Name != "HQ Troyes" || len(sitesList[0].FloorplanIDs) != 1 {
		t.Fatalf("unexpected sites list: %+v", sitesList)
	}

	// 5. AdminGetSite
	reqGet := createAuthedReq(t, d, http.MethodGet, "/api/admin/sites/"+strconvI64(siteID),
		"fp_admin4@test.com", "FP Admin 4", "password123", models.RoleFloorplanManager,
		nil)
	reqGet.SetPathValue("id", strconvI64(siteID))
	wGet := httptest.NewRecorder()
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.AdminGetSite))).ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 on get site, got %d", wGet.Code)
	}
	var siteGet models.Site
	if err := json.Unmarshal(wGet.Body.Bytes(), &siteGet); err != nil {
		t.Fatalf("unmarshal get site: %v", err)
	}
	if siteGet.ID != siteID || siteGet.Name != "HQ Troyes" || siteGet.CountryCode != "FR" {
		t.Fatalf("unexpected get site payload: %+v", siteGet)
	}

	// 6. UpdateSite: change name, country, not_corporate, and add second floor
	updatePayload := map[string]interface{}{
		"name":               "HQ Troyes Campus",
		"country_code":       "ES",
		"not_corporate_site": true,
		"floorplan_ids":      []int64{fp1, fp2},
	}
	updateBody, _ := json.Marshal(updatePayload)
	reqUpdate := createAuthedReq(t, d, http.MethodPut, "/api/admin/sites/"+strconvI64(siteID),
		"fp_admin5@test.com", "FP Admin 5", "password123", models.RoleFloorplanManager,
		updateBody)
	reqUpdate.SetPathValue("id", strconvI64(siteID))
	wUpdate := httptest.NewRecorder()
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.UpdateSite))).ServeHTTP(wUpdate, reqUpdate)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("expected 200 on update site, got %d", wUpdate.Code)
	}

	updatedSite, err := d.GetSite(siteID)
	if err != nil {
		t.Fatalf("GetSite after update: %v", err)
	}
	if updatedSite.Name != "HQ Troyes Campus" || updatedSite.CountryCode != "ES" || !updatedSite.NotCorporateSite || len(updatedSite.FloorplanIDs) != 2 {
		t.Fatalf("site not updated properly in DB: %+v", updatedSite)
	}

	// 7. AdminSitesPage render
	reqPage := createAuthedReq(t, d, http.MethodGet, "/admin/sites",
		"fp_admin6@test.com", "FP Admin 6", "password123", models.RoleFloorplanManager,
		nil)
	wPage := httptest.NewRecorder()
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.AdminSitesPage))).ServeHTTP(wPage, reqPage)
	if wPage.Code != http.StatusOK || renderedPage != "admin_sites" {
		t.Fatalf("expected admin_sites page rendered, got page=%q code=%d", renderedPage, wPage.Code)
	}
	if renderedData["Sites"] == nil || renderedData["Floorplans"] == nil || renderedData["Countries"] == nil {
		t.Fatalf("expected Sites, Floorplans, Countries in template data, got %+v", renderedData)
	}

	// 8. DeleteSite
	reqDelete := createAuthedReq(t, d, http.MethodDelete, "/api/admin/sites/"+strconvI64(siteID),
		"fp_admin7@test.com", "FP Admin 7", "password123", models.RoleFloorplanManager,
		nil)
	reqDelete.SetPathValue("id", strconvI64(siteID))
	wDelete := httptest.NewRecorder()
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.DeleteSite))).ServeHTTP(wDelete, reqDelete)
	if wDelete.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete site, got %d", wDelete.Code)
	}
	deletedSite, err := d.GetSite(siteID)
	if err == nil && deletedSite != nil {
		t.Fatalf("site was not deleted from DB")
	}
}

func TestFloorplanHandler_SiteIntegration(t *testing.T) {
	d := newCRUDTestDB(t)
	var renderedData map[string]interface{}
	mockRender := func(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
		if m, ok := data.(map[string]interface{}); ok {
			renderedData = m
		}
		w.WriteHeader(http.StatusOK)
	}

	h := &FloorplanHandler{
		DB:     d,
		Render: mockRender,
	}

	// Create Site
	siteID, err := d.CreateSite(models.Site{
		Name:        "Site Geneva",
		CountryCode: "CH",
	})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	// 1. CreateFloorplan with site_id
	createReq := createAuthedReq(t, d, http.MethodPost, "/admin/floorplans",
		"fp_mgr1@test.com", "FP Mgr", "password123", models.RoleFloorplanManager,
		[]byte(`{"name":"Floor Geneva 1", "site_id":`+strconvI64(siteID)+`}`))
	wCreate := httptest.NewRecorder()
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.CreateFloorplan))).ServeHTTP(wCreate, createReq)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("expected 200 on create floorplan with site, got %d", wCreate.Code)
	}
	var res map[string]interface{}
	_ = json.Unmarshal(wCreate.Body.Bytes(), &res)
	fpID := int64(res["id"].(float64))

	fp, err := d.GetFloorplan(fpID)
	if err != nil || fp.SiteID != siteID || fp.SiteName != "Site Geneva" {
		t.Fatalf("floorplan site association failed: %+v", fp)
	}

	// 2. UpdateFloorplan with site_id
	updateReq := createAuthedReq(t, d, http.MethodPut, "/admin/floorplans/"+strconvI64(fpID),
		"fp_mgr2@test.com", "FP Mgr 2", "password123", models.RoleFloorplanManager,
		[]byte(`{"name":"Floor Geneva 1 Renamed", "sort_order":2, "site_id":0}`))
	updateReq.SetPathValue("id", strconvI64(fpID))
	wUpdate := httptest.NewRecorder()
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.UpdateFloorplan))).ServeHTTP(wUpdate, updateReq)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("expected 200 on update floorplan, got %d", wUpdate.Code)
	}

	fpUpdated, err := d.GetFloorplan(fpID)
	if err != nil || fpUpdated.SiteID != 0 || fpUpdated.SiteName != "" || fpUpdated.Name != "Floor Geneva 1 Renamed" {
		t.Fatalf("floorplan update site detached failed: %+v", fpUpdated)
	}

	// 3. AdminFloorplansPage passes Sites
	reqAdminFP := createAuthedReq(t, d, http.MethodGet, "/admin/floorplans",
		"fp_mgr3@test.com", "FP Mgr 3", "password123", models.RoleFloorplanManager,
		nil)
	wAdminFP := httptest.NewRecorder()
	middleware.Auth(d, middleware.RequireRole(models.RoleFloorplanManager)(http.HandlerFunc(h.AdminFloorplansPage))).ServeHTTP(wAdminFP, reqAdminFP)
	if wAdminFP.Code != http.StatusOK {
		t.Fatalf("expected 200 on AdminFloorplansPage, got %d", wAdminFP.Code)
	}
	if renderedData["Sites"] == nil {
		t.Fatalf("expected Sites in AdminFloorplansPage data")
	}
}
