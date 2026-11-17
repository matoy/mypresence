package handlers

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/matoy/mypresence/internal/i18n"
)

func TestTemplates_I18nKeysValid(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	tplDir := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../web/templates"))
	entries, err := os.ReadDir(tplDir)
	if err != nil {
		t.Fatalf("read templates dir: %v", err)
	}

	re := regexp.MustCompile(`index\s+\$?\.T\s+"([^"]*)"`)
	langs := []string{"en", "fr", "de", "es", "it"}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(tplDir, e.Name()))
		if err != nil {
			t.Fatalf("read template %s: %v", e.Name(), err)
		}
		matches := re.FindAllStringSubmatch(string(raw), -1)
		for _, m := range matches {
			key := m[1]
			if key == "" || strings.TrimSpace(key) != key {
				t.Errorf("%s contains i18n key with invalid whitespace: %q", e.Name(), key)
			}
			for _, lang := range langs {
				if _, exists := i18n.T(lang)[key]; !exists {
					t.Errorf("%s references key %q missing from i18n locale %q", e.Name(), key, lang)
				}
			}
		}
	}
}
