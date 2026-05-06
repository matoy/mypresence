package functional

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFunctionalHTTPClientsHaveTimeouts guards against reintroducing flaky
// functional tests caused by timeoutless HTTP calls.
func TestFunctionalHTTPClientsHaveTimeouts(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_test.go") || name == "http_timeout_guard_test.go" {
			continue
		}

		path := filepath.Clean(name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		content := string(data)
		lines := strings.Split(content, "\n")

		for i, line := range lines {
			ln := i + 1
			if strings.Contains(line, "http.Get(") || strings.Contains(line, "http.Post(") {
				t.Fatalf("%s:%d uses direct http.Get/http.Post; use an http.Client with Timeout", path, ln)
			}

			if !strings.Contains(line, "&http.Client{") {
				continue
			}

			depth := strings.Count(line, "{") - strings.Count(line, "}")
			hasTimeout := strings.Contains(line, "Timeout:")
			for j := i + 1; j < len(lines) && depth > 0; j++ {
				if strings.Contains(lines[j], "Timeout:") {
					hasTimeout = true
				}
				depth += strings.Count(lines[j], "{")
				depth -= strings.Count(lines[j], "}")
			}

			if !hasTimeout {
				t.Fatalf("%s:%d defines http.Client without Timeout", path, ln)
			}
		}
	}
}
