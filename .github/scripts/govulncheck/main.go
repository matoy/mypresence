// Command govulncheck runs `govulncheck -format json ./...` and fails (exit 3) on
// any reachable (symbol-level) vulnerability, while temporarily ignoring
// GO-2026-6909 when github.com/crewjam/saml >= v0.5.2 is in use.
//
// Background: github.com/crewjam/saml v0.5.2+ fixes the assertion namespace and
// nil-check flaws tracked by GO-2026-6909, but vuln.go.dev/ID/GO-2026-6909.json
// has not yet published `fixed: 0.5.2` (tracked in golang/vulndb#6939).
// Remove this wrapper and restore direct `govulncheck ./...` once resolved.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	ignoredOSV        = "GO-2026-6909"
	ignoredModule     = "github.com/crewjam/saml"
	minFixedSAMLVer   = "v0.5.2"
	vulndbTrackingRef = "https://github.com/golang/vulndb/issues/6939"
)

type streamMessage struct {
	Progress *progressMsg `json:"progress"`
	SBOM     *sbomMsg     `json:"SBOM"`
	OSV      *osvEntry    `json:"osv"`
	Finding  *finding     `json:"finding"`
}

type progressMsg struct {
	Message string `json:"message"`
}

type sbomMsg struct {
	GoVersion string       `json:"go_version"`
	Modules   []sbomModule `json:"modules"`
}

type sbomModule struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

type osvEntry struct {
	ID               string `json:"id"`
	Summary          string `json:"summary"`
	DatabaseSpecific struct {
		URL string `json:"url"`
	} `json:"database_specific"`
}

type finding struct {
	OSV          string  `json:"osv"`
	FixedVersion string  `json:"fixed_version"`
	Trace        []frame `json:"trace"`
}

type frame struct {
	Module   string    `json:"module"`
	Version  string    `json:"version"`
	Package  string    `json:"package"`
	Function string    `json:"function"`
	Receiver string    `json:"receiver"`
	Position *position `json:"position"`
}

type position struct {
	Filename string `json:"filename"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

func main() {
	cmd := exec.Command("go", "run", "golang.org/x/vuln/cmd/govulncheck@latest", "-format", "json", "./...")
	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "govulncheck execution failed: %v\n", err)
		os.Exit(1)
	}

	code := evaluateStream(&stdoutBuf, os.Stdout, os.Stderr)
	if code != 0 {
		os.Exit(code)
	}
}

func evaluateStream(r io.Reader, out, errOut io.Writer) int {
	dec := json.NewDecoder(r)
	osvs := make(map[string]osvEntry)
	modVersions := make(map[string]string)
	var findings []finding

	for {
		var msg streamMessage
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			_, _ = fmt.Fprintf(errOut, "failed to decode govulncheck JSON output: %v\n", err)
			return 1
		}
		if msg.Progress != nil && msg.Progress.Message != "" {
			_, _ = fmt.Fprintln(out, msg.Progress.Message)
		}
		if msg.SBOM != nil {
			for _, m := range msg.SBOM.Modules {
				modVersions[m.Path] = m.Version
			}
		}
		if msg.OSV != nil {
			osvs[msg.OSV.ID] = *msg.OSV
		}
		if msg.Finding != nil {
			findings = append(findings, *msg.Finding)
		}
	}

	samlVersion := modVersions[ignoredModule]
	var blocking []finding
	var suppressed []finding
	pkgOnlyIDs := make(map[string]struct{})
	modOnlyIDs := make(map[string]struct{})

	for _, f := range findings {
		if hasSymbolCall(f.Trace) {
			if isSuppressedFinding(f, samlVersion) {
				suppressed = append(suppressed, f)
			} else {
				blocking = append(blocking, f)
			}
			continue
		}
		if hasPackageFrame(f.Trace) {
			pkgOnlyIDs[f.OSV] = struct{}{}
		} else {
			modOnlyIDs[f.OSV] = struct{}{}
		}
	}

	if len(suppressed) > 0 {
		_, _ = fmt.Fprintf(out,
			"NOTE: ignoring %d symbol trace(s) for %s on patched %s@%s (>= %s) pending vulndb update (%s)\n",
			len(suppressed), ignoredOSV, ignoredModule, samlVersion, minFixedSAMLVer, vulndbTrackingRef,
		)
	} else {
		_, _ = fmt.Fprintf(out,
			"INFO: %s was not reported on %s@%s; the temporary filter in .github/scripts/govulncheck can be removed.\n",
			ignoredOSV, ignoredModule, samlVersion,
		)
	}

	if len(pkgOnlyIDs) > 0 || len(modOnlyIDs) > 0 {
		_, _ = fmt.Fprintf(out,
			"Informational non-called findings: %d package(s), %d module(s)\n",
			len(pkgOnlyIDs), len(modOnlyIDs),
		)
	}

	if len(blocking) > 0 {
		_, _ = fmt.Fprintf(errOut, "\nBlocking reachable vulnerabilities detected (%d trace(s)):\n", len(blocking))
		for i, f := range blocking {
			entry := osvs[f.OSV]
			_, _ = fmt.Fprintf(errOut, "\n#%d: %s — %s\n", i+1, f.OSV, entry.Summary)
			if entry.DatabaseSpecific.URL != "" {
				_, _ = fmt.Fprintf(errOut, "  More info: %s\n", entry.DatabaseSpecific.URL)
			}
			if f.FixedVersion != "" {
				_, _ = fmt.Fprintf(errOut, "  Fixed in:  %s\n", f.FixedVersion)
			}
			for _, fr := range f.Trace {
				_, _ = fmt.Fprintf(errOut, "    %s\n", formatFrame(fr))
			}
		}
		return 3
	}

	_, _ = fmt.Fprintln(out, "No blocking reachable vulnerabilities found.")
	return 0
}

func hasSymbolCall(trace []frame) bool {
	for _, fr := range trace {
		if fr.Function != "" {
			return true
		}
	}
	return false
}

func hasPackageFrame(trace []frame) bool {
	for _, fr := range trace {
		if fr.Package != "" {
			return true
		}
	}
	return false
}

func isSuppressedFinding(f finding, sbomVersion string) bool {
	if f.OSV != ignoredOSV || len(f.Trace) == 0 {
		return false
	}
	if f.Trace[0].Module != ignoredModule {
		return false
	}
	ver := f.Trace[0].Version
	if ver == "" {
		ver = sbomVersion
	}
	return isSemverAtLeast(ver, minFixedSAMLVer)
}

// isSemverAtLeast checks whether semver v is >= minVer (e.g. "v0.6.3" >= "v0.5.2").
func isSemverAtLeast(v, minVer string) bool {
	majA, minA, patA, preA, okA := parseSemver(v)
	majB, minB, patB, preB, okB := parseSemver(minVer)
	if !okA || !okB {
		return false
	}
	if majA != majB {
		return majA > majB
	}
	if minA != minB {
		return minA > minB
	}
	if patA != patB {
		return patA > patB
	}
	if preA != "" && preB == "" {
		return false
	}
	return true
}

func parseSemver(s string) (maj, min, pat int, pre string, ok bool) {
	if !strings.HasPrefix(s, "v") {
		return 0, 0, 0, "", false
	}
	s = strings.TrimPrefix(s, "v")
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		pre = s[idx+1:]
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return 0, 0, 0, "", false
	}
	var err error
	if maj, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, 0, "", false
	}
	if min, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, 0, "", false
	}
	if pat, err = strconv.Atoi(parts[2]); err != nil {
		return 0, 0, 0, "", false
	}
	return maj, min, pat, pre, true
}

func formatFrame(fr frame) string {
	sym := fr.Function
	if fr.Receiver != "" {
		sym = fr.Receiver + "." + fr.Function
	}
	if fr.Position != nil && fr.Position.Filename != "" {
		return fmt.Sprintf("%s:%d:%d: %s.%s", fr.Position.Filename, fr.Position.Line, fr.Position.Column, fr.Package, sym)
	}
	if fr.Package != "" && sym != "" {
		return fmt.Sprintf("%s.%s", fr.Package, sym)
	}
	return fmt.Sprintf("%s@%s", fr.Module, fr.Version)
}
