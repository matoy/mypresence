package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/mypresence/internal/metrics"
	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func counterValue(cv *prometheus.CounterVec, labels ...string) float64 {
	return testutil.ToFloat64(cv.WithLabelValues(labels...))
}

func TestObservabilityCounter_PATCreate(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &PATHandler{DB: d}

	beforeFail := counterValue(metrics.PATOpsTotal, "create", "failure")
	reqFail := createAuthedReq(t, d, http.MethodPost, "/api/pat", "basic-pat@example.com", "Basic PAT", "password1", models.RoleBasic, []byte(`{"description":"Token basic","expires_in":7}`))
	wFail := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreatePAT)).ServeHTTP(wFail, reqFail)
	if wFail.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for basic user PAT create, got %d", wFail.Code)
	}
	afterFail := counterValue(metrics.PATOpsTotal, "create", "failure")
	if delta := afterFail - beforeFail; delta != 1 {
		t.Fatalf("expected create/failure delta=1, got %v", delta)
	}

	beforeOK := counterValue(metrics.PATOpsTotal, "create", "success")
	reqOK := createAuthedReq(t, d, http.MethodPost, "/api/pat", "admin-pat@example.com", "Admin PAT", "password1", models.RoleGlobal, []byte(`{"description":"Token admin","expires_in":7}`))
	wOK := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreatePAT)).ServeHTTP(wOK, reqOK)
	if wOK.Code != http.StatusOK {
		t.Fatalf("expected 200 for PAT create, got %d", wOK.Code)
	}
	afterOK := counterValue(metrics.PATOpsTotal, "create", "success")
	if delta := afterOK - beforeOK; delta != 1 {
		t.Fatalf("expected create/success delta=1, got %v", delta)
	}
}

func TestObservabilityCounter_AdminCreateTeam(t *testing.T) {
	d := newCRUDTestDB(t)
	h := &AdminHandler{DB: d}

	beforeFail := counterValue(metrics.AdminOpsTotal, "team", "create", "failure")
	reqFail := createAuthedReq(t, d, http.MethodPost, "/api/admin/teams", "basic-adminteam@example.com", "Basic AdminTeam", "password1", models.RoleBasic, []byte(`{"name":"Ops Team"}`))
	wFail := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateTeam)).ServeHTTP(wFail, reqFail)
	if wFail.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for team create without rights, got %d", wFail.Code)
	}
	afterFail := counterValue(metrics.AdminOpsTotal, "team", "create", "failure")
	if delta := afterFail - beforeFail; delta != 1 {
		t.Fatalf("expected team/create/failure delta=1, got %v", delta)
	}

	beforeOK := counterValue(metrics.AdminOpsTotal, "team", "create", "success")
	reqOK := createAuthedReq(t, d, http.MethodPost, "/api/admin/teams", "manager-adminteam@example.com", "Manager AdminTeam", "password1", models.RoleTeamManager, []byte(`{"name":"Ops Team"}`))
	wOK := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.CreateTeam)).ServeHTTP(wOK, reqOK)
	if wOK.Code != http.StatusOK {
		t.Fatalf("expected 200 for team create by manager, got %d", wOK.Code)
	}
	afterOK := counterValue(metrics.AdminOpsTotal, "team", "create", "success")
	if delta := afterOK - beforeOK; delta != 1 {
		t.Fatalf("expected team/create/success delta=1, got %v", delta)
	}
}

func TestObservabilityCounter_FloorplanAndUserOps(t *testing.T) {
	d := newCRUDTestDB(t)
	fh := &FloorplanHandler{DB: d, DataDir: t.TempDir()}
	uh := &UsersAdminHandler{DB: d}

	beforeSeatFail := counterValue(metrics.FloorplanOpsTotal, "admin_seat", "failure")
	wSeatFail := httptest.NewRecorder()
	fh.AdminListSeats(wSeatFail, httptest.NewRequest(http.MethodGet, "/api/admin/seats", nil))
	if wSeatFail.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing floorplan_id, got %d", wSeatFail.Code)
	}
	afterSeatFail := counterValue(metrics.FloorplanOpsTotal, "admin_seat", "failure")
	if delta := afterSeatFail - beforeSeatFail; delta != 1 {
		t.Fatalf("expected admin_seat/failure delta=1, got %v", delta)
	}

	beforeFloorplanOK := counterValue(metrics.FloorplanOpsTotal, "list_floorplans", "success")
	wFloorplanOK := httptest.NewRecorder()
	fh.ListFloorplansAPI(wFloorplanOK, httptest.NewRequest(http.MethodGet, "/api/floorplans", nil))
	if wFloorplanOK.Code != http.StatusOK {
		t.Fatalf("expected 200 for list floorplans, got %d", wFloorplanOK.Code)
	}
	afterFloorplanOK := counterValue(metrics.FloorplanOpsTotal, "list_floorplans", "success")
	if delta := afterFloorplanOK - beforeFloorplanOK; delta != 1 {
		t.Fatalf("expected list_floorplans/success delta=1, got %v", delta)
	}

	targetID, err := d.CreateLocalUser("target-user@example.com", "Target User", "password1")
	if err != nil {
		t.Fatalf("CreateLocalUser target: %v", err)
	}
	beforeSetPwdFail := counterValue(metrics.AdminOpsTotal, "user", "set_password", "failure")
	reqPwdFail := createAuthedReq(t, d, http.MethodPost, "/api/admin/users/"+strconvI64(targetID)+"/password", "global-userops@example.com", "Global UserOps", "password1", models.RoleGlobal, []byte(`{"password":"short"}`))
	reqPwdFail.SetPathValue("id", strconvI64(targetID))
	wPwdFail := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(uh.SetPassword)).ServeHTTP(wPwdFail, reqPwdFail)
	if wPwdFail.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short password, got %d", wPwdFail.Code)
	}
	afterSetPwdFail := counterValue(metrics.AdminOpsTotal, "user", "set_password", "failure")
	if delta := afterSetPwdFail - beforeSetPwdFail; delta != 1 {
		t.Fatalf("expected user/set_password/failure delta=1, got %v", delta)
	}

	beforeSetPwdOK := counterValue(metrics.AdminOpsTotal, "user", "set_password", "success")
	reqPwdOK := createAuthedReq(t, d, http.MethodPost, "/api/admin/users/"+strconvI64(targetID)+"/password", "global-userops2@example.com", "Global UserOps 2", "password1", models.RoleGlobal, []byte(`{"password":"longpassword"}`))
	reqPwdOK.SetPathValue("id", strconvI64(targetID))
	wPwdOK := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(uh.SetPassword)).ServeHTTP(wPwdOK, reqPwdOK)
	if wPwdOK.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid password update, got %d", wPwdOK.Code)
	}
	afterSetPwdOK := counterValue(metrics.AdminOpsTotal, "user", "set_password", "success")
	if delta := afterSetPwdOK - beforeSetPwdOK; delta != 1 {
		t.Fatalf("expected user/set_password/success delta=1, got %v", delta)
	}
}
