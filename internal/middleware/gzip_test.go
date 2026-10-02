package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGzipMiddleware_Compressed(t *testing.T) {
	samplePayload := "Hello world! This is a test string for gzip compression in myPresence."
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(samplePayload)) //nolint:errcheck
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close() //nolint:errcheck

	if res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", res.Header.Get("Content-Encoding"))
	}
	if !bytes.Contains([]byte(res.Header.Get("Vary")), []byte("Accept-Encoding")) {
		t.Errorf("expected Vary header to contain Accept-Encoding, got %q", res.Header.Get("Vary"))
	}

	gzReader, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gzReader.Close() //nolint:errcheck

	decompressed, err := io.ReadAll(gzReader)
	if err != nil {
		t.Fatalf("failed to read decompressed body: %v", err)
	}

	if string(decompressed) != samplePayload {
		t.Fatalf("expected %q, got %q", samplePayload, string(decompressed))
	}
}

func TestGzipMiddleware_NoAcceptEncoding(t *testing.T) {
	samplePayload := "Plain text response without compression"
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(samplePayload)) //nolint:errcheck
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close() //nolint:errcheck

	if res.Header.Get("Content-Encoding") != "" {
		t.Errorf("expected no Content-Encoding, got %q", res.Header.Get("Content-Encoding"))
	}

	body, _ := io.ReadAll(res.Body)
	if string(body) != samplePayload {
		t.Errorf("expected %q, got %q", samplePayload, string(body))
	}
}

func TestGzipMiddleware_UpgradeSkipped(t *testing.T) {
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("websocket")) //nolint:errcheck
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close() //nolint:errcheck

	if res.Header.Get("Content-Encoding") != "" {
		t.Errorf("expected no Content-Encoding for Upgrade request, got %q", res.Header.Get("Content-Encoding"))
	}
}

func TestGzipMiddleware_StatusNotModified(t *testing.T) {
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))

	req := httptest.NewRequest(http.MethodGet, "/static/js/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusNotModified {
		t.Fatalf("expected status 304, got %d", res.StatusCode)
	}
	if res.Header.Get("Content-Encoding") != "" {
		t.Errorf("expected no Content-Encoding for 304, got %q", res.Header.Get("Content-Encoding"))
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) != 0 {
		t.Errorf("expected empty body for 304, got %d bytes: %v", len(body), body)
	}
}

