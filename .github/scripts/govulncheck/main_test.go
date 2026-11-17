package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestEvaluateStream_IgnoresGO20266909OnPatchedSAML(t *testing.T) {
	stream := `
{"SBOM":{"go_version":"go1.26.11","modules":[{"path":"github.com/crewjam/saml","version":"v0.6.3"}]}}
{"osv":{"id":"GO-2026-6909","summary":"SAML vuln"}}
{"finding":{"osv":"GO-2026-6909","trace":[{"module":"github.com/crewjam/saml","version":"v0.6.3","package":"github.com/crewjam/saml","function":"ParseResponse","receiver":"*ServiceProvider"}]}}
{"finding":{"osv":"GO-2026-6354","fixed_version":"v0.56.0","trace":[{"module":"golang.org/x/crypto","version":"v0.55.0"}]}}
`
	var out, errOut bytes.Buffer
	code := evaluateStream(strings.NewReader(stream), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "ignoring 1 symbol trace(s) for GO-2026-6909") {
		t.Fatalf("expected suppression notice, got: %s", out.String())
	}
}

func TestEvaluateStream_BlocksGO20266909OnVulnerableSAML(t *testing.T) {
	stream := `
{"SBOM":{"go_version":"go1.26.11","modules":[{"path":"github.com/crewjam/saml","version":"v0.4.14"}]}}
{"osv":{"id":"GO-2026-6909","summary":"SAML vuln"}}
{"finding":{"osv":"GO-2026-6909","trace":[{"module":"github.com/crewjam/saml","version":"v0.4.14","package":"github.com/crewjam/saml","function":"ParseResponse","receiver":"*ServiceProvider"}]}}
`
	var out, errOut bytes.Buffer
	code := evaluateStream(strings.NewReader(stream), &out, &errOut)
	if code != 3 {
		t.Fatalf("expected exit 3 on unpatched v0.4.14, got %d", code)
	}
	if !strings.Contains(errOut.String(), "GO-2026-6909") {
		t.Fatalf("expected GO-2026-6909 on stderr, got: %s", errOut.String())
	}
}

func TestEvaluateStream_BlocksOtherReachableVulnerability(t *testing.T) {
	stream := `
{"SBOM":{"go_version":"go1.26.10","modules":[{"path":"github.com/crewjam/saml","version":"v0.6.3"},{"path":"stdlib","version":"v1.26.10"}]}}
{"osv":{"id":"GO-2026-6909","summary":"SAML vuln"}}
{"osv":{"id":"GO-2026-6859","summary":"HTTP/1 header race"}}
{"finding":{"osv":"GO-2026-6909","trace":[{"module":"github.com/crewjam/saml","version":"v0.6.3","package":"github.com/crewjam/saml","function":"ParseResponse"}]}}
{"finding":{"osv":"GO-2026-6859","fixed_version":"go1.26.11","trace":[{"module":"stdlib","version":"v1.26.10","package":"net/http","function":"NewRequest"}]}}
`
	var out, errOut bytes.Buffer
	code := evaluateStream(strings.NewReader(stream), &out, &errOut)
	if code != 3 {
		t.Fatalf("expected exit 3 when another reachable vulnerability exists, got %d", code)
	}
	if !strings.Contains(errOut.String(), "GO-2026-6859") {
		t.Fatalf("expected GO-2026-6859 on stderr, got: %s", errOut.String())
	}
}

func TestIsSemverAtLeast(t *testing.T) {
	cases := []struct {
		v    string
		min  string
		want bool
	}{
		{"v0.5.2", "v0.5.2", true},
		{"v0.6.3", "v0.5.2", true},
		{"v1.0.0", "v0.5.2", true},
		{"v0.5.1", "v0.5.2", false},
		{"v0.4.14", "v0.5.2", false},
		{"v0.5.2-rc1", "v0.5.2", false},
		{"v0.5.2+incompatible", "v0.5.2", true},
		{"invalid", "v0.5.2", false},
	}
	for _, tc := range cases {
		if got := isSemverAtLeast(tc.v, tc.min); got != tc.want {
			t.Errorf("isSemverAtLeast(%q, %q) = %v, want %v", tc.v, tc.min, got, tc.want)
		}
	}
}
