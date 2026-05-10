package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matoy/myPresence/internal/middleware"
)

// Minimal 1x1 white PNG (67 bytes)
var minimalPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, // IDAT chunk
	0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
	0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc,
	0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, // IEND chunk
	0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestUploadFloorplanImage_ValidPNG(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	tmpDir := t.TempDir()
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: tmpDir}

	fpID, _ := d.CreateFloorplan("FP Upload Valid", 0)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("image", "test.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	fw.Write(minimalPNG) //nolint:errcheck
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := createAdminReq(t, d, http.MethodPost, "/admin/floorplans/"+strconvI64(fpID)+"/image", buf.Bytes())
	req.SetPathValue("id", strconvI64(fpID))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UploadFloorplanImage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUploadFloorplanImage_FakeExtension(t *testing.T) {
	d := newExtraTestDB(t)
	d.SetBcryptCost(4)
	tmpDir := t.TempDir()
	h := &FloorplanHandler{DB: d, Render: noRender, DataDir: tmpDir}

	fpID, _ := d.CreateFloorplan("FP Fake Ext", 0)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	// Use .png extension but provide text content (invalid type)
	fw, _ := mw.CreateFormFile("image", "test.png")
	fw.Write([]byte("this is not an image")) //nolint:errcheck
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := createAdminReq(t, d, http.MethodPost, "/admin/floorplans/"+strconvI64(fpID)+"/image", buf.Bytes())
	req.SetPathValue("id", strconvI64(fpID))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UploadFloorplanImage)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid content type, got %d: %s", w.Code, w.Body.String())
	}
}
