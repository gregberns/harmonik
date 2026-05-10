package brcli_test

// terminaltransiteroute_bi012_test.go — BI-012 terminal-transition write routing lint.
//
// Per BI-012: every terminal-transition write (br claim, br close, br reopen)
// MUST route through the br-CLI adapter layer. A direct br subprocess invocation
// that bypasses the adapter for a status-change operation is a structural
// violation.
//
// This test scans the harmonik source corpus for patterns indicating a direct
// br claim/close/reopen subprocess invocation outside the adapter package
// (internal/brcli). It inspects Go source files for string literals or comments
// containing the normative status-change argv: "br claim", "br close",
// "br reopen" (with or without surrounding whitespace variants).
//
// The test complements TestBreakageAdapterIsSoleExecImporter (BI-025/BI-026),
// which enforces the os/exec import boundary. BI-012 specifically targets the
// semantic constraint: even if a package does not import os/exec, it must not
// call into a subprocess with br status-change argv via any other mechanism.
//
// Spec ref: specs/beads-integration.md §4.4 BI-012.
// Bead: hk-872.12.

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// bi012FixtureGoListPackage is the subset of "go list -json" output needed for
// the BI-012 lint test: package import path and source file list.
type bi012FixtureGoListPackage struct {
	ImportPath string   `json:"ImportPath"`
	GoFiles    []string `json:"GoFiles"`
	Dir        string   `json:"Dir"`
}

// bi012FixtureListHarmonikPackages runs "go list -json ./..." and returns the
// parsed package list. The test helper fails the test on any exec or parse error.
func bi012FixtureListHarmonikPackages(t *testing.T) []bi012FixtureGoListPackage {
	t.Helper()
	//nolint:gosec // G204: "go" is resolved from PATH; args are static strings, not user input.
	cmd := exec.CommandContext(t.Context(), "go", "list", "-json", "./...")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bi012FixtureListHarmonikPackages: go list: %v", err)
	}

	var pkgs []bi012FixtureGoListPackage
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var pkg bi012FixtureGoListPackage
		if decErr := dec.Decode(&pkg); decErr != nil {
			t.Fatalf("bi012FixtureListHarmonikPackages: json decode: %v", decErr)
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}

// bi012TerminalStatusChangePatterns is the set of br argv patterns that
// constitute a terminal-transition write per BI-010 / BI-012. Any of these
// substrings appearing in a Go source file outside the adapter indicates a
// potential bypass.
//
// The patterns are matched as substrings against source lines so that both
// exact and flag-augmented forms (e.g. "br claim --run-id foo") are caught.
//
// Spec ref: specs/beads-integration.md §4.4 BI-010 — the three terminal
// transitions are: claim, close, reopen.
var bi012TerminalStatusChangePatterns = []string{
	`"br", "claim"`,
	`"br", "close"`,
	`"br", "close"`,
	`"br", "reopen"`,
	// Shell-style invocation strings (e.g. passed to sh -c):
	`"br claim"`,
	`"br close"`,
	`"br reopen"`,
}

// bi012FileContainsPattern scans a single Go source file line-by-line and
// returns the first line (and its 1-based line number) that contains any of the
// given patterns. Returns "", 0 if no pattern is found.
func bi012FileContainsPattern(path string, patterns []string) (matchedLine string, lineNo int, err error) {
	f, err := os.Open(path) //nolint:gosec // G304: path comes from go list output, not user input.
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // cleanup on read path

	scanner := bufio.NewScanner(f)
	lineno := 0
	for scanner.Scan() {
		lineno++
		line := scanner.Text()
		for _, pat := range patterns {
			if strings.Contains(line, pat) {
				return line, lineno, nil
			}
		}
	}
	return "", 0, scanner.Err()
}

// TestBI012_TerminalTransitionWrites_RouteOnlyThroughAdapter verifies that
// br status-change argv patterns (claim, close, reopen) do not appear in any
// harmonik Go source file outside internal/brcli.
//
// A match in a non-adapter harmonik package indicates a direct br subprocess
// invocation that bypasses the adapter — a structural BI-012 violation.
//
// Spec ref: specs/beads-integration.md §4.4 BI-012 — "A direct br subprocess
// invocation that bypasses the adapter for a status-change operation is a
// structural violation."
func TestBI012_TerminalTransitionWrites_RouteOnlyThroughAdapter(t *testing.T) {
	t.Parallel()

	const (
		adapterPkg = "github.com/gregberns/harmonik/internal/brcli"
		selfPrefix = "github.com/gregberns/harmonik"
	)

	pkgs := bi012FixtureListHarmonikPackages(t)

	type violation struct {
		pkgPath  string
		filePath string
		line     string
		lineNo   int
	}
	var violations []violation

	for _, pkg := range pkgs {
		// Only inspect harmonik packages.
		if !strings.HasPrefix(pkg.ImportPath, selfPrefix) {
			continue
		}

		// The adapter package itself is expected to invoke br with status-change argv.
		if pkg.ImportPath == adapterPkg {
			continue
		}

		for _, goFile := range pkg.GoFiles {
			filePath := filepath.Join(pkg.Dir, goFile)
			matchedLine, lineNo, err := bi012FileContainsPattern(filePath, bi012TerminalStatusChangePatterns)
			if err != nil {
				t.Errorf("BI-012: reading %s: %v", filePath, err)
				continue
			}
			if matchedLine != "" {
				violations = append(violations, violation{
					pkgPath:  pkg.ImportPath,
					filePath: filePath,
					line:     strings.TrimSpace(matchedLine),
					lineNo:   lineNo,
				})
			}
		}
	}

	if len(violations) > 0 {
		var sb strings.Builder
		sb.WriteString("BI-012 violation: br terminal-transition argv found outside internal/brcli adapter —\n")
		sb.WriteString("all claim/close/reopen writes MUST route through the adapter (specs/beads-integration.md §4.4 BI-012):\n")
		for _, v := range violations {
			sb.WriteString("  ")
			sb.WriteString(v.filePath)
			sb.WriteString(":")
			sb.WriteString(strings.Repeat("0", max(0, 4-len(itoa(v.lineNo)))))
			sb.WriteString(itoa(v.lineNo))
			sb.WriteString(": ")
			sb.WriteString(v.line)
			sb.WriteString("\n")
		}
		t.Error(sb.String())
	}
}

// itoa is a minimal int-to-string helper to avoid importing strconv in the
// test helper pattern.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	if n < 0 {
		buf = append(buf, '-')
		n = -n
	}
	start := len(buf)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	// Reverse digits.
	for i, j := start, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

// max returns the larger of two ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
