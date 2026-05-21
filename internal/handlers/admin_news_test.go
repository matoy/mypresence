package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/middleware"
	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// NewsPage
// -----------------------------------------------------------------------

func TestNewsPage_Renders(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		if page != "admin_news" {
			t.Errorf("expected admin_news, got %q", page)
		}
	}}
	req := createAdminReq(t, d, http.MethodGet, "/admin/news", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.NewsPage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// CreateNews — happy path
// -----------------------------------------------------------------------

func TestCreateNews_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "Hello",
		"content":    "World",
		"start_date": today,
		"end_date":   today,
		"bg_color":   "#dc2626",
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/news", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resp["id"]; !ok {
		t.Error("expected id in response")
	}
}

// -----------------------------------------------------------------------
// CreateNews — default color applied when bg_color is empty
// -----------------------------------------------------------------------

func TestCreateNews_DefaultColor(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "No color",
		"content":    "content",
		"start_date": today,
		"end_date":   today,
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/news", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	msgs, err := d.ListNewsMessages()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].BgColor != "#dc2626" {
		t.Errorf("bg_color: got %q, want #dc2626", msgs[0].BgColor)
	}
}

// -----------------------------------------------------------------------
// CreateNews — validation errors
// -----------------------------------------------------------------------

func TestCreateNews_MissingFields(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	body, _ := json.Marshal(map[string]interface{}{
		"title": "Only title",
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/news", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateNews_InvalidDates_EndBeforeStart(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "Bad dates",
		"content":    "content",
		"start_date": "2026-06-01",
		"end_date":   "2026-05-01",
		"bg_color":   "#dc2626",
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/news", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateNews_InvalidDateFormat(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "Bad format",
		"content":    "content",
		"start_date": "not-a-date",
		"end_date":   "2026-12-31",
		"bg_color":   "#dc2626",
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/news", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateNews_InvalidColor(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "Bad color",
		"content":    "content",
		"start_date": today,
		"end_date":   today,
		"bg_color":   "notacolor",
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/news", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateNews_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	req := createAdminReq(t, d, http.MethodPost, "/admin/news", []byte("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateNews_ThreeCharHexColor(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "Short hex",
		"content":    "content",
		"start_date": today,
		"end_date":   today,
		"bg_color":   "#f00",
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/news", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for 3-char hex, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateNews_WithActivityViewerRole(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "Audit test",
		"content":    "Hello",
		"start_date": today,
		"end_date":   today,
		"bg_color":   "#123456",
	})
	req := createAuthedReq(t, d, http.MethodPost, "/admin/news",
		"actmgr@test.com", "Act Mgr", "password1", models.RoleActivityViewer, body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// UpdateNews — happy path
// -----------------------------------------------------------------------

func TestUpdateNews_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("Original", "content", today, today, "#dc2626", false)

	body, _ := json.Marshal(map[string]interface{}{
		"title":      "Updated",
		"content":    "New content",
		"start_date": today,
		"end_date":   today,
		"bg_color":   "#aabbcc",
	})
	req := createAdminReq(t, d, http.MethodPut, "/admin/news/"+strconvI64(id), body)
	req.SetPathValue("id", strconvI64(id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// UpdateNews — validation errors
// -----------------------------------------------------------------------

func TestUpdateNews_InvalidID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "x",
		"content":    "y",
		"start_date": "2026-01-01",
		"end_date":   "2026-01-31",
		"bg_color":   "#dc2626",
	})
	req := createAdminReq(t, d, http.MethodPut, "/admin/news/abc", body)
	req.SetPathValue("id", "abc")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateNews_MissingFields(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("Orig", "content", today, today, "#dc2626", false)
	body, _ := json.Marshal(map[string]interface{}{"title": ""})
	req := createAdminReq(t, d, http.MethodPut, "/admin/news/"+strconvI64(id), body)
	req.SetPathValue("id", strconvI64(id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateNews_InvalidDates(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("Orig", "content", today, today, "#dc2626", false)
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "x",
		"content":    "y",
		"start_date": "2026-06-01",
		"end_date":   "2026-05-01",
		"bg_color":   "#dc2626",
	})
	req := createAdminReq(t, d, http.MethodPut, "/admin/news/"+strconvI64(id), body)
	req.SetPathValue("id", strconvI64(id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateNews_InvalidColor(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("Orig", "content", today, today, "#dc2626", false)
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "x",
		"content":    "y",
		"start_date": today,
		"end_date":   today,
		"bg_color":   "badcolor",
	})
	req := createAdminReq(t, d, http.MethodPut, "/admin/news/"+strconvI64(id), body)
	req.SetPathValue("id", strconvI64(id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateNews_BadJSON(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("Orig", "content", today, today, "#dc2626", false)
	req := createAdminReq(t, d, http.MethodPut, "/admin/news/"+strconvI64(id), []byte("{bad"))
	req.SetPathValue("id", strconvI64(id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateNews_WithActivityViewerRole(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("Orig", "content", today, today, "#dc2626", false)
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "Updated",
		"content":    "New",
		"start_date": today,
		"end_date":   today,
		"bg_color":   "#dc2626",
	})
	req := createAuthedReq(t, d, http.MethodPut, "/admin/news/"+strconvI64(id),
		"actmgr2@test.com", "Act Mgr2", "password1", models.RoleActivityViewer, body)
	req.SetPathValue("id", strconvI64(id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// DeleteNews
// -----------------------------------------------------------------------

func TestDeleteNews_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("To delete", "content", today, today, "#dc2626", false)

	req := createAdminReq(t, d, http.MethodDelete, "/admin/news/"+strconvI64(id), nil)
	req.SetPathValue("id", strconvI64(id))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteNews)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	msgs, _ := d.ListNewsMessages()
	if len(msgs) != 0 {
		t.Error("expected no messages after delete")
	}
}

func TestDeleteNews_InvalidID(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	req := createAdminReq(t, d, http.MethodDelete, "/admin/news/xyz", nil)
	req.SetPathValue("id", "xyz")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteNews_WithActivityViewerRole(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("Audit delete", "content", today, today, "#dc2626", false)
	req := createAuthedReq(t, d, http.MethodDelete, "/admin/news/"+strconvI64(id),
		"actmgr3@test.com", "Act Mgr3", "password1", models.RoleActivityViewer, nil)
	req.SetPathValue("id", strconvI64(id))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteNews)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// -----------------------------------------------------------------------
// Role access control
// -----------------------------------------------------------------------

func TestNewsPage_RequiresRole(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	req := createAuthedReq(t, d, http.MethodGet, "/admin/news",
		"basic@test.com", "Basic User", "password1", models.RoleBasic, nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	protected := middleware.Auth(d, middleware.RequireRole(models.RoleActivityViewer)(http.HandlerFunc(h.NewsPage)))
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for basic user, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// isValidDate helper
// -----------------------------------------------------------------------

func TestIsValidDate(t *testing.T) {
	if !isValidDate("2026-01-15") {
		t.Error("expected 2026-01-15 to be valid")
	}
	if isValidDate("not-a-date") {
		t.Error("expected not-a-date to be invalid")
	}
	if isValidDate("2026-13-01") {
		t.Error("expected month 13 to be invalid")
	}
	if isValidDate("") {
		t.Error("expected empty string to be invalid")
	}
	if !isValidDate("2026-02-28") {
		t.Error("expected 2026-02-28 to be valid")
	}
}

// -----------------------------------------------------------------------
// Recurring news — create, update, active check
// -----------------------------------------------------------------------

func TestCreateNews_Recurring(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "Monthly reminder",
		"content":    "Remember to submit your hours",
		"start_date": "2026-01-20",
		"end_date":   "2026-01-25",
		"bg_color":   "#7c3aed",
		"recurring":  true,
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/news", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	msgs, _ := d.ListNewsMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !msgs[0].Recurring {
		t.Error("expected Recurring=true on created message")
	}
}

func TestCreateNews_Recurring_InvalidDayRange(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	// start day (28) > end day (5) — invalid for recurring
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "Bad range",
		"content":    "x",
		"start_date": "2026-01-28",
		"end_date":   "2026-01-05",
		"bg_color":   "#7c3aed",
		"recurring":  true,
	})
	req := createAdminReq(t, d, http.MethodPost, "/admin/news", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.CreateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateNews_ToggleRecurring(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("Monthly", "content", today, today, "#7c3aed", true)

	body, _ := json.Marshal(map[string]interface{}{
		"title":      "Monthly",
		"content":    "content",
		"start_date": today,
		"end_date":   today,
		"bg_color":   "#7c3aed",
		"recurring":  false, // toggled off
	})
	req := createAdminReq(t, d, http.MethodPut, "/admin/news/"+strconvI64(id), body)
	req.SetPathValue("id", strconvI64(id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UpdateNews)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	msgs, _ := d.ListNewsMessages()
	if msgs[0].Recurring {
		t.Error("expected Recurring=false after toggle, got true")
	}
}

func TestCreateNewsAPI_Recurring(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "API Monthly",
		"content":    "Monthly reminder via API",
		"start_date": "2026-01-15",
		"end_date":   "2026-01-20",
		"bg_color":   "#7c3aed",
		"recurring":  true,
	})
	req := createAuthedReq(t, d, http.MethodPost, "/api/admin/news",
		"creator2@test.com", "Creator2", "pass", models.RoleActivityViewer, body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	protected := middleware.Auth(d, middleware.RequireRole(models.RoleActivityViewer)(http.HandlerFunc(h.CreateNews)))
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	msgs, _ := d.ListNewsMessages()
	if len(msgs) != 1 || !msgs[0].Recurring {
		t.Fatalf("expected 1 recurring message, got %v", msgs)
	}
}

// -----------------------------------------------------------------------
// GET /api/news  (active news — all authenticated users)
// -----------------------------------------------------------------------

func TestGetActiveNewsAPI_Empty(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	req := createAdminReq(t, d, http.MethodGet, "/api/news", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.GetActiveNewsAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("expected JSON array, got: %s", w.Body.String())
	}
	if len(got) != 0 {
		t.Fatalf("expected empty array, got %d items", len(got))
	}
}

func TestGetActiveNewsAPI_ReturnsOnlyActive(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

	// Active message (today is within range)
	if _, err := d.CreateNewsMessage("Active", "Active content", today, tomorrow, "#dc2626", false); err != nil {
		t.Fatal(err)
	}
	// Expired message
	if _, err := d.CreateNewsMessage("Expired", "Old content", yesterday, yesterday, "#dc2626", false); err != nil {
		t.Fatal(err)
	}

	req := createAdminReq(t, d, http.MethodGet, "/api/news", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.GetActiveNewsAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("JSON parse error: %v — body: %s", err, w.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 active message, got %d", len(got))
	}
	if got[0]["title"] != "Active" {
		t.Errorf("expected title 'Active', got %v", got[0]["title"])
	}
}

func TestGetActiveNewsAPI_RequiresAuth(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	req, _ := http.NewRequest(http.MethodGet, "/api/news", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	// Wrap with Auth middleware — unauthenticated request should be rejected
	middleware.Auth(d, http.HandlerFunc(h.GetActiveNewsAPI)).ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatal("expected non-200 for unauthenticated request")
	}
}

// -----------------------------------------------------------------------
// GET /api/admin/news  (all news — activity_viewer required)
// -----------------------------------------------------------------------

func TestListNewsAPI_Empty(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	req := createAdminReq(t, d, http.MethodGet, "/api/admin/news", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.ListNewsAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("expected JSON array, got: %s", w.Body.String())
	}
	if len(got) != 0 {
		t.Fatalf("expected empty array, got %d items", len(got))
	}
}

func TestListNewsAPI_ReturnsAll(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	if _, err := d.CreateNewsMessage("Msg1", "Content1", today, today, "#dc2626", false); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateNewsMessage("Msg2", "Content2", yesterday, yesterday, "#1d4ed8", false); err != nil {
		t.Fatal(err)
	}

	req := createAdminReq(t, d, http.MethodGet, "/api/admin/news", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	h.ListNewsAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("JSON parse error: %v — body: %s", err, w.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
}

func TestListNewsAPI_RequiresActivityViewer(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	req := createAuthedReq(t, d, http.MethodGet, "/api/admin/news",
		"basicapi@test.com", "Basic API", "pass", models.RoleBasic, nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	protected := middleware.Auth(d, middleware.RequireRole(models.RoleActivityViewer)(http.HandlerFunc(h.ListNewsAPI)))
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestListNewsAPI_ActivityViewerCanAccess(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	req := createAuthedReq(t, d, http.MethodGet, "/api/admin/news",
		"viewer@test.com", "Viewer", "pass", models.RoleActivityViewer, nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	protected := middleware.Auth(d, middleware.RequireRole(models.RoleActivityViewer)(http.HandlerFunc(h.ListNewsAPI)))
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// POST /api/admin/news  (create via API — same handler, activity_viewer)
// -----------------------------------------------------------------------

func TestCreateNewsAPI_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	body, _ := json.Marshal(map[string]interface{}{
		"title":      "API Banner",
		"content":    "API content",
		"start_date": today,
		"end_date":   today,
		"bg_color":   "#16a34a",
	})
	req := createAuthedReq(t, d, http.MethodPost, "/api/admin/news",
		"creator@test.com", "Creator", "pass", models.RoleActivityViewer, body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	protected := middleware.Auth(d, middleware.RequireRole(models.RoleActivityViewer)(http.HandlerFunc(h.CreateNews)))
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	msgs, _ := d.ListNewsMessages()
	if len(msgs) != 1 || msgs[0].Title != "API Banner" {
		t.Fatalf("expected 1 message with title 'API Banner', got %v", msgs)
	}
}

func TestCreateNewsAPI_RequiresActivityViewer(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	body, _ := json.Marshal(map[string]interface{}{
		"title": "Should fail", "content": "x", "start_date": today, "end_date": today,
	})
	req := createAuthedReq(t, d, http.MethodPost, "/api/admin/news",
		"nobodyx@test.com", "Nobody", "pass", models.RoleBasic, body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	protected := middleware.Auth(d, middleware.RequireRole(models.RoleActivityViewer)(http.HandlerFunc(h.CreateNews)))
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// PUT /api/admin/news/{id}  (update via API)
// -----------------------------------------------------------------------

func TestUpdateNewsAPI_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("Original", "Content", today, today, "#dc2626", false)

	body, _ := json.Marshal(map[string]interface{}{
		"title": "Updated API", "content": "New content", "start_date": today, "end_date": today, "bg_color": "#dc2626",
	})
	req := createAuthedReq(t, d, http.MethodPut, "/api/admin/news/"+strconvI64(id),
		"upd@test.com", "Upd", "pass", models.RoleActivityViewer, body)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(req.Context())
	// Simulate path value
	req.SetPathValue("id", strconvI64(id))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	protected := middleware.Auth(d, middleware.RequireRole(models.RoleActivityViewer)(http.HandlerFunc(h.UpdateNews)))
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	title := d.GetNewsMessageTitle(id)
	if title != "Updated API" {
		t.Errorf("expected title 'Updated API', got %q", title)
	}
}

func TestUpdateNewsAPI_RequiresActivityViewer(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("X", "Y", today, today, "#dc2626", false)
	body, _ := json.Marshal(map[string]interface{}{
		"title": "X", "content": "Y", "start_date": today, "end_date": today, "bg_color": "#dc2626",
	})
	req := createAuthedReq(t, d, http.MethodPut, "/api/admin/news/"+strconvI64(id),
		"nobodyy@test.com", "Nobody", "pass", models.RoleBasic, body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", strconvI64(id))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	protected := middleware.Auth(d, middleware.RequireRole(models.RoleActivityViewer)(http.HandlerFunc(h.UpdateNews)))
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// DELETE /api/admin/news/{id}  (delete via API)
// -----------------------------------------------------------------------

func TestDeleteNewsAPI_Success(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("ToDelete", "Content", today, today, "#dc2626", false)

	req := createAuthedReq(t, d, http.MethodDelete, "/api/admin/news/"+strconvI64(id),
		"deler@test.com", "Deler", "pass", models.RoleActivityViewer, nil)
	req.SetPathValue("id", strconvI64(id))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	protected := middleware.Auth(d, middleware.RequireRole(models.RoleActivityViewer)(http.HandlerFunc(h.DeleteNews)))
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	msgs, _ := d.ListNewsMessages()
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after delete, got %d", len(msgs))
	}
}

func TestDeleteNewsAPI_RequiresActivityViewer(t *testing.T) {
	d := newExtraTestDB(t)
	h := &NewsHandler{DB: d, Render: noRender}
	today := time.Now().Format("2006-01-02")
	id, _ := d.CreateNewsMessage("ToDelete", "Content", today, today, "#dc2626", false)

	req := createAuthedReq(t, d, http.MethodDelete, "/api/admin/news/"+strconvI64(id),
		"nobody2@test.com", "Nobody", "pass", models.RoleBasic, nil)
	req.SetPathValue("id", strconvI64(id))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	protected := middleware.Auth(d, middleware.RequireRole(models.RoleActivityViewer)(http.HandlerFunc(h.DeleteNews)))
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}
