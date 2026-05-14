package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/matoy/mypresence/internal/middleware"
)

// -----------------------------------------------------------------------
// GeneralSettingsPage
// -----------------------------------------------------------------------

func TestGeneralSettingsPage_NoLogo(t *testing.T) {
	d := newExtraTestDB(t)
	dir := t.TempDir()
	var gotLogoExists interface{}
	h := &GeneralSettingsHandler{
		DataDir: dir,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			m := data.(map[string]interface{})
			gotLogoExists = m["LogoExists"]
		},
	}
	req := createAdminReq(t, d, http.MethodGet, "/admin/settings", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.GeneralSettingsPage)).ServeHTTP(w, req)
	if gotLogoExists != false {
		t.Errorf("expected LogoExists=false, got %v", gotLogoExists)
	}
}

func TestGeneralSettingsPage_WithLogo(t *testing.T) {
	d := newExtraTestDB(t)
	dir := t.TempDir()
	// Create a logo file so Stat succeeds
	os.WriteFile(filepath.Join(dir, "logo.png"), minimalPNG, 0600) //nolint:errcheck
	var gotLogoExists interface{}
	h := &GeneralSettingsHandler{
		DataDir: dir,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			m := data.(map[string]interface{})
			gotLogoExists = m["LogoExists"]
		},
	}
	req := createAdminReq(t, d, http.MethodGet, "/admin/settings", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.GeneralSettingsPage)).ServeHTTP(w, req)
	if gotLogoExists != true {
		t.Errorf("expected LogoExists=true, got %v", gotLogoExists)
	}
}

func TestGeneralSettingsPage_QueryParams(t *testing.T) {
	d := newExtraTestDB(t)
	dir := t.TempDir()
	var gotError, gotSuccess interface{}
	h := &GeneralSettingsHandler{
		DataDir: dir,
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			m := data.(map[string]interface{})
			gotError = m["Error"]
			gotSuccess = m["Success"]
		},
	}
	req := createAdminReq(t, d, http.MethodGet, "/admin/settings?error=oops&success=yay", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.GeneralSettingsPage)).ServeHTTP(w, req)
	if gotError != "oops" {
		t.Errorf("expected Error=oops, got %v", gotError)
	}
	if gotSuccess != "yay" {
		t.Errorf("expected Success=yay, got %v", gotSuccess)
	}
}

// -----------------------------------------------------------------------
// UploadLogo
// -----------------------------------------------------------------------

func makePNGUpload(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("logo", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	fw.Write(content) //nolint:errcheck
	mw.Close()        //nolint:errcheck
	return &buf, mw.FormDataContentType()
}

func TestUploadLogo_NoFile(t *testing.T) {
	d := newExtraTestDB(t)
	h := &GeneralSettingsHandler{DataDir: t.TempDir()}
	req := createAdminReq(t, d, http.MethodPost, "/admin/settings/logo", nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=nothing")
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UploadLogo)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/settings?error=missing_file" {
		t.Errorf("expected redirect to missing_file, got %q", loc)
	}
}

func TestUploadLogo_WrongExtension(t *testing.T) {
	d := newExtraTestDB(t)
	h := &GeneralSettingsHandler{DataDir: t.TempDir()}
	buf, ct := makePNGUpload(t, "logo.txt", minimalPNG)
	req := createAdminReq(t, d, http.MethodPost, "/admin/settings/logo", buf.Bytes())
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UploadLogo)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/settings?error=invalid_format" {
		t.Errorf("expected redirect to invalid_format, got %q", loc)
	}
}

func TestUploadLogo_WrongContentType(t *testing.T) {
	d := newExtraTestDB(t)
	h := &GeneralSettingsHandler{DataDir: t.TempDir()}
	// .png extension but plain text content
	buf, ct := makePNGUpload(t, "logo.png", []byte("this is definitely not a PNG file"))
	req := createAdminReq(t, d, http.MethodPost, "/admin/settings/logo", buf.Bytes())
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UploadLogo)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/settings?error=invalid_content" {
		t.Errorf("expected redirect to invalid_content, got %q", loc)
	}
}

func TestUploadLogo_Success(t *testing.T) {
	d := newExtraTestDB(t)
	dir := t.TempDir()
	h := &GeneralSettingsHandler{DataDir: dir}
	buf, ct := makePNGUpload(t, "logo.png", minimalPNG)
	req := createAdminReq(t, d, http.MethodPost, "/admin/settings/logo", buf.Bytes())
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UploadLogo)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/settings?success=logo_uploaded" {
		t.Errorf("expected redirect to success, got %q", loc)
	}
	if _, err := os.Stat(filepath.Join(dir, "logo.png")); err != nil {
		t.Errorf("expected logo.png to exist after upload: %v", err)
	}
}

// -----------------------------------------------------------------------
// DeleteLogo
// -----------------------------------------------------------------------

func TestDeleteLogo_FileExists(t *testing.T) {
	d := newExtraTestDB(t)
	dir := t.TempDir()
	logoPath := filepath.Join(dir, "logo.png")
	os.WriteFile(logoPath, minimalPNG, 0600) //nolint:errcheck
	h := &GeneralSettingsHandler{DataDir: dir}
	req := createAdminReq(t, d, http.MethodDelete, "/admin/settings/logo", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteLogo)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(logoPath); !os.IsNotExist(err) {
		t.Error("expected logo.png to be deleted")
	}
}

func TestDeleteLogo_FileNotExist(t *testing.T) {
	d := newExtraTestDB(t)
	h := &GeneralSettingsHandler{DataDir: t.TempDir()}
	req := createAdminReq(t, d, http.MethodDelete, "/admin/settings/logo", nil)
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.DeleteLogo)).ServeHTTP(w, req)
	// os.IsNotExist → no error returned, should still succeed
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when file doesn't exist, got %d: %s", w.Code, w.Body.String())
	}
}
