package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer      *gzip.Writer
	wroteHeader bool
	status      int
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status

	if status == http.StatusNotModified || status == http.StatusNoContent {
		w.ResponseWriter.WriteHeader(status)
		return
	}

	// Delete Content-Length because compressed stream size differs from original
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Add("Vary", "Accept-Encoding")
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.status == http.StatusNotModified || w.status == http.StatusNoContent {
		return 0, nil
	}
	return w.writer.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if w.status == http.StatusNotModified || w.status == http.StatusNoContent {
		return
	}
	_ = w.writer.Flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Gzip returns a middleware that compresses HTTP responses using gzip
// for clients that indicate support via Accept-Encoding.
func Gzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only compress if client accepts gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		// Skip compression for SSE (Server-Sent Events) or WebSocket upgrades
		if r.Header.Get("Upgrade") != "" {
			next.ServeHTTP(w, r)
			return
		}

		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w)

		grw := &gzipResponseWriter{
			ResponseWriter: w,
			writer:         gz,
		}

		defer func() {
			if grw.status == http.StatusNotModified || grw.status == http.StatusNoContent {
				gzipWriterPool.Put(gz)
				return
			}
			_ = gz.Close()
			gzipWriterPool.Put(gz)
		}()

		next.ServeHTTP(grw, r)
	})
}
