package handlers

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"presence-app/internal/middleware"
)

// createPNGBytes creates a minimal valid PNG image as bytes.
func createPNGBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, img) //nolint:errcheck
	return buf.Bytes()
}

// TestUploadFloorplanImage_DeleteOld covers floorplan.go L.353-355 (delete old image on re-upload)
func TestUploadFloorplanImage_DeleteOld(t *testing.T) {
	d := newExtraTestDB(t)
	dataDir := t.TempDir()
	h := &FloorplanHandler{DB: d, DataDir: dataDir}

	fpID, _ := d.CreateFloorplan("OldImgFP", 0)
	// Set an existing image path (file does not need to exist — os.Remove ignores errors)
	d.SetFloorplanImage(fpID, "old_image.png") //nolint:errcheck

	// Build multipart form with a valid PNG
	pngBytes := createPNGBytes()
	var body bytes.Buffer
	mp := multipart.NewWriter(&body)
	part, err := mp.CreateFormFile("image", "test.png")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(pngBytes) //nolint:errcheck
	if err := mp.Close(); err != nil {
		t.Fatal(err)
	}

	req := createAdminReq(t, d, http.MethodPost, "/admin/floorplans/"+strconvI64(fpID)+"/image", body.Bytes())
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UploadFloorplanImage)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUploadFloorplanImage_OsCreateError covers floorplan.go L.359-363 (os.Create fails)
func TestUploadFloorplanImage_OsCreateError(t *testing.T) {
	d := newExtraTestDB(t)
	h := &FloorplanHandler{DB: d, DataDir: "/nonexistent_dir_that_cannot_be_created"}

	fpID, _ := d.CreateFloorplan("OsCreateErrFP", 0)

	pngBytes := createPNGBytes()
	var body bytes.Buffer
	mp := multipart.NewWriter(&body)
	part, err := mp.CreateFormFile("image", "test.png")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(pngBytes) //nolint:errcheck
	if err := mp.Close(); err != nil {
		t.Fatal(err)
	}

	req := createAdminReq(t, d, http.MethodPost, "/admin/floorplans/"+strconvI64(fpID)+"/image", body.Bytes())
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.SetPathValue("id", strconvI64(fpID))
	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	middleware.Auth(d, http.HandlerFunc(h.UploadFloorplanImage)).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}
