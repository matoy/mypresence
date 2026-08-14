package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/config"
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

func TestGeneralSettingsPage_EnvVarsEditableFlag(t *testing.T) {
	d := newExtraTestDB(t)
	t.Setenv("APP_NAME", "TestApp")
	t.Setenv("SOME_TOTALLY_UNKNOWN_TEST_VAR", "1")

	var gotEnvVars []EnvEntry
	h := &GeneralSettingsHandler{
		DataDir: t.TempDir(),
		Render: func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
			m := data.(map[string]interface{})
			gotEnvVars = m["EnvVars"].([]EnvEntry)
		},
	}
	req := createAdminReq(t, d, http.MethodGet, "/admin/settings", nil)
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.GeneralSettingsPage)).ServeHTTP(w, req)

	var foundAppName, foundUnknown bool
	for _, e := range gotEnvVars {
		if e.Key == "APP_NAME" {
			foundAppName = true
			if !e.Editable {
				t.Error("APP_NAME should be marked Editable")
			}
		}
		if e.Key == "SOME_TOTALLY_UNKNOWN_TEST_VAR" {
			foundUnknown = true
			if e.Editable {
				t.Error("SOME_TOTALLY_UNKNOWN_TEST_VAR should not be marked Editable")
			}
		}
	}
	if !foundAppName {
		t.Fatal("APP_NAME not found in EnvVars")
	}
	if !foundUnknown {
		t.Fatal("SOME_TOTALLY_UNKNOWN_TEST_VAR not found in EnvVars")
	}
}

// -----------------------------------------------------------------------
// UpdateEnvVar
// -----------------------------------------------------------------------

func TestUpdateEnvVar_Success(t *testing.T) {
	d := newExtraTestDB(t)
	cfg := &config.Config{DBDriver: "sqlite"}
	h := &GeneralSettingsHandler{DataDir: t.TempDir(), Config: cfg}

	body := []byte(`{"key":"APP_NAME","value":"New Name"}`)
	req := createAdminReq(t, d, http.MethodPost, "/admin/settings/env", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateEnvVar)).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if cfg.AppName != "New Name" {
		t.Errorf("expected cfg.AppName to be updated, got %q", cfg.AppName)
	}
}

func TestUpdateEnvVar_MissingKey_Returns400(t *testing.T) {
	d := newExtraTestDB(t)
	h := &GeneralSettingsHandler{DataDir: t.TempDir(), Config: &config.Config{}}

	body := []byte(`{"value":"x"}`)
	req := createAdminReq(t, d, http.MethodPost, "/admin/settings/env", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateEnvVar)).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateEnvVar_BadJSON_Returns400(t *testing.T) {
	d := newExtraTestDB(t)
	h := &GeneralSettingsHandler{DataDir: t.TempDir(), Config: &config.Config{}}

	req := createAdminReq(t, d, http.MethodPost, "/admin/settings/env", []byte("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateEnvVar)).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateEnvVar_NonEditableKey_Returns400(t *testing.T) {
	d := newExtraTestDB(t)
	cfg := &config.Config{SecretKey: "original-secret"}
	h := &GeneralSettingsHandler{DataDir: t.TempDir(), Config: cfg}

	body := []byte(`{"key":"SECRET_KEY","value":"hacked"}`)
	req := createAdminReq(t, d, http.MethodPost, "/admin/settings/env", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateEnvVar)).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if cfg.SecretKey != "original-secret" {
		t.Error("SECRET_KEY should not have been modified")
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp["error"], "cannot be edited") {
		t.Errorf("unexpected error message: %q", resp["error"])
	}
}

func TestUpdateEnvVar_NilConfig_Returns400(t *testing.T) {
	d := newExtraTestDB(t)
	h := &GeneralSettingsHandler{DataDir: t.TempDir()}

	body := []byte(`{"key":"APP_NAME","value":"x"}`)
	req := createAdminReq(t, d, http.MethodPost, "/admin/settings/env", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	middleware.Auth(d, http.HandlerFunc(h.UpdateEnvVar)).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when Config is nil, got %d: %s", w.Code, w.Body.String())
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
