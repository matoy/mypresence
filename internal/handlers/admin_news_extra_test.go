package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/matoy/mypresence/internal/models"
)

func TestAdminNews_Handlers(t *testing.T) {
	d := newCRUDTestDB(t)

	adminID, _ := d.CreateLocalUser("admin_news@example.com", "Admin News", "pass")
	_ = d.UpdateUserRoles(adminID, models.RoleActivityViewer)
	adminUser, _ := d.GetUserByID(adminID)

	var renderedPage string
	var renderedData interface{}
	h := &NewsHandler{
		DB: d,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			renderedPage = page
			renderedData = data
		},
	}

	// 1. GetActiveNewsAPI & ListNewsAPI when empty
	rec := httptest.NewRecorder()
	req := reqWithUser(d, adminUser, http.MethodGet, "/api/news", nil)
	h.GetActiveNewsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on GetActiveNewsAPI, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodGet, "/api/admin/news", nil)
	h.ListNewsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on ListNewsAPI, got %d", rec.Code)
	}

	// 2. NewsPage render
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodGet, "/admin/news?error=test_err", nil)
	h.NewsPage(rec, req)
	if renderedPage != "admin_news" {
		t.Errorf("expected renderedPage admin_news, got %q", renderedPage)
	}
	m := renderedData.(map[string]interface{})
	if m["Error"] != "test_err" {
		t.Errorf("expected Error to be test_err, got %v", m["Error"])
	}

	// 3. CreateNews - bad JSON
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPost, "/admin/news", strings.NewReader("bad-json"))
	h.CreateNews(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad JSON CreateNews, got %d", rec.Code)
	}

	// 4. CreateNews - missing fields
	badPayloads := []map[string]interface{}{
		{"title": "", "content": "text", "start_date": "2026-08-01", "end_date": "2026-08-10"},
		{"title": "Title", "content": "", "start_date": "2026-08-01", "end_date": "2026-08-10"},
		{"title": "Title", "content": "text", "start_date": "invalid-date", "end_date": "2026-08-10"},
		{"title": "Title", "content": "text", "start_date": "2026-08-20", "end_date": "2026-08-10", "recurring": false},
		{"title": "Title", "content": "text", "start_date": "2026-08-20", "end_date": "2026-08-10", "recurring": true}, // start day > end day
		{"title": "Title", "content": "text", "start_date": "2026-08-01", "end_date": "2026-08-10", "bg_color": "not-a-color"},
	}
	for _, p := range badPayloads {
		body, _ := json.Marshal(p)
		rec = httptest.NewRecorder()
		req = reqWithUser(d, adminUser, http.MethodPost, "/admin/news", bytes.NewReader(body))
		h.CreateNews(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for payload %+v, got %d", p, rec.Code)
		}
	}

	// 5. CreateNews - valid
	validPayload := map[string]interface{}{
		"title":      "Maintenance",
		"content":    "Server update tonight",
		"start_date": "2026-08-01",
		"end_date":   "2026-08-31",
		"bg_color":   "#3b82f6",
		"recurring":  false,
	}
	body, _ := json.Marshal(validPayload)
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPost, "/admin/news", bytes.NewReader(body))
	h.CreateNews(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid CreateNews, got %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &created) //nolint:errcheck
	newsID := int64(created["id"].(float64))

	// Verify action logged
	logs, err := d.GetAdminLogsByActor(adminID, time.Time{})
	if err != nil || len(logs) == 0 {
		t.Errorf("expected admin action log, err: %v, count: %d", err, len(logs))
	}

	// 6. UpdateNews - bad ID / bad JSON
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPut, "/admin/news/bad-id", strings.NewReader("{}"))
	req.SetPathValue("id", "bad-id")
	h.UpdateNews(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad ID UpdateNews, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPut, "/admin/news/"+strconv.FormatInt(newsID, 10), strings.NewReader("bad-json"))
	req.SetPathValue("id", strconv.FormatInt(newsID, 10))
	h.UpdateNews(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad JSON UpdateNews, got %d", rec.Code)
	}

	// 7. UpdateNews - validation error
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPut, "/admin/news/"+strconv.FormatInt(newsID, 10), strings.NewReader(`{"title":""}`))
	req.SetPathValue("id", strconv.FormatInt(newsID, 10))
	h.UpdateNews(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on empty fields in UpdateNews, got %d", rec.Code)
	}

	// 8. UpdateNews - valid
	updatePayload := map[string]interface{}{
		"title":      "Maintenance Done",
		"content":    "All systems normal",
		"start_date": "2026-08-01",
		"end_date":   "2026-08-31",
		"bg_color":   "#10b981",
		"recurring":  false,
	}
	body, _ = json.Marshal(updatePayload)
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodPut, "/admin/news/"+strconv.FormatInt(newsID, 10), bytes.NewReader(body))
	req.SetPathValue("id", strconv.FormatInt(newsID, 10))
	h.UpdateNews(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid UpdateNews, got %d: %s", rec.Code, rec.Body.String())
	}

	// 9. DeleteNews - bad ID & success
	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodDelete, "/admin/news/bad-id", nil)
	req.SetPathValue("id", "bad-id")
	h.DeleteNews(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad ID DeleteNews, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = reqWithUser(d, adminUser, http.MethodDelete, "/admin/news/"+strconv.FormatInt(newsID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(newsID, 10))
	h.DeleteNews(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on DeleteNews, got %d: %s", rec.Code, rec.Body.String())
	}

	// 10. DB Errors when closed
	d.Close()
	rec = httptest.NewRecorder()
	h.GetActiveNewsAPI(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on closed DB GetActiveNewsAPI, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ListNewsAPI(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on closed DB ListNewsAPI, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.NewsPage(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on closed DB NewsPage, got %d", rec.Code)
	}
}
