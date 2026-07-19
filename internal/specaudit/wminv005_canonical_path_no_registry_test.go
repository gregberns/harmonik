//go:build specaudit

package specaudit_test

// hk-8mwo.58 binding test — WM-INV-005 canonical-path discovery without a registry.
//
// Spec ref: specs/workspace-model.md §5 WM-INV-005.
//
// WM-INV-005 states: any subsystem reconstructing workspace state from the filesystem
// MUST derive the workspace location from run_id alone per WM-002 without consulting a
// separate registry, index, or SQLite table. No registry-backed lookup MAY be declared
// authoritative for workspace path resolution. Sensor: WM-013c filesystem discovery by
// construction — any workspace whose path does not match <repo>/.harmonik/worktrees/<run_id>/
// for some recognized run_id is a violation.
//
// # Audit frame
//
// This test is a spec-corpus sensor. It verifies that WM-INV-005 is correctly declared
// in the spec so that:
//
//  1. WM-INV-005 heading is present in specs/workspace-model.md.
//  2. "MUST derive the workspace location from run_id alone" is declared.
//  3. "without consulting a separate registry, index, or SQLite table" is declared.
//  4. "No registry-backed lookup MAY be declared authoritative" is declared.
//  5. "WM-013c" is named as the sensor mechanism.
//  6. Tags: mechanism is present in the WM-INV-005 body window.
//
// # Failure modes
//
//   - WM-INV-005 heading missing.
//   - MUST derive workspace location from run_id alone absent.
//   - without consulting a separate registry absent.
//   - No registry-backed lookup absent.
//   - WM-013c sensor absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wmInv005Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// wmInv005FixtureWorkspaceModelPath returns the absolute path to specs/workspace-model.md.
func wmInv005FixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wmInv005FixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wmInv005FixtureHeading matches the WM-INV-005 level-4 requirement heading line.
var wmInv005FixtureHeading = regexp.MustCompile(`^#### WM-INV-005 —`)

// wmInv005FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wmInv005FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wmInv005FixtureTagsMechanism matches a "Tags: mechanism" line.
var wmInv005FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wmInv005FixtureBodyWindow is the maximum number of lines to scan after the heading.
const wmInv005FixtureBodyWindow = 30

// wmInv005FixtureLoadLines opens specFile and returns all lines.
func wmInv005FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wmInv005FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wmInv005FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wmInv005FixtureBodyLines returns the lines comprising the WM-INV-005 body.
func wmInv005FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wmInv005FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-INV-005 heading not found; expected '#### WM-INV-005 —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wmInv005FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wmInv005FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wmInv005FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func wmInv005FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestWMINV005CanonicalPathDiscoveryWithoutRegistry is the binding test for hk-8mwo.58.
func TestWMINV005CanonicalPathDiscoveryWithoutRegistry(t *testing.T) {
	t.Parallel()

	specFile := wmInv005FixtureWorkspaceModelPath(t)
	lines := wmInv005FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wmInv005FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-INV-005 check(1): %s", reason)
	}
	t.Logf("WM-INV-005 heading found at specs/workspace-model.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "2",
			label:  "must-derive-from-run-id-alone",
			needle: "MUST derive the workspace location from",
			detail: "WM-INV-005 body must declare 'MUST derive the workspace location from' run_id alone " +
				"(expected phrase 'MUST derive the workspace location from'); the canonical path " +
				"<repo>/.harmonik/worktrees/<run_id>/ is derivable from run_id alone — no lookup " +
				"table or index is needed, and requiring one would create a registry-as-SPOF",
		},
		{
			id:     "3",
			label:  "without-consulting-registry-index-sqlite",
			needle: "without consulting a separate registry",
			detail: "WM-INV-005 body must declare 'without consulting a separate registry' (or index or SQLite table) " +
				"(expected phrase 'without consulting a separate registry'); this is the anti-pattern " +
				"to prevent — if any subsystem declares a registry authoritative for path resolution, " +
				"that registry becomes a consistency boundary that can diverge from the filesystem",
		},
		{
			id:     "4",
			label:  "no-registry-lookup-authoritative",
			needle: "No registry-backed lookup",
			detail: "WM-INV-005 body must declare 'No registry-backed lookup' MAY be authoritative " +
				"(expected phrase 'No registry-backed lookup'); this closes the invariant normatively — " +
				"even if a registry is used for caching or indexing, it MUST NOT be the authority " +
				"for workspace path resolution; the filesystem path formula is the authority",
		},
		{
			id:     "5",
			label:  "wm-013c-sensor-named",
			needle: "WM-013c",
			detail: "WM-INV-005 body must name 'WM-013c' as the sensor mechanism " +
				"(expected phrase 'WM-013c'); WM-013c is the filesystem discovery requirement " +
				"that implements the sensor — it enumerates worktrees by path pattern and routes " +
				"path-mismatched entries to reconciliation Cat 3c or Cat 6a",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !wmInv005FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-INV-005 check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-INV-005 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in WM-INV-005 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wmInv005FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-INV-005 check(6) FAILED: Tags: mechanism not found in WM-INV-005 body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-INV-005 body)\n"+
					"  detail: WM-INV-005 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mwo.58 audit complete — WM-INV-005 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
