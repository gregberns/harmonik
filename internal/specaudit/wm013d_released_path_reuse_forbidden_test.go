//go:build specaudit

package specaudit_test

// hk-8mwo.22 binding test — WM-013d released-workspace path re-use is forbidden.
//
// Spec ref: specs/workspace-model.md §4.3 WM-013d.
//
// WM-013d states: a released workspace's canonical path (WM-002) MUST NOT be re-leased
// by a subsequent run. New runs receive new canonical paths via new run_ids per WM-034.
// The prior run's worktree directory and branch MAY persist on disk per WM-031; re-use
// of the path for a different run_id is forbidden — the canonical-path invariant
// (WM-INV-005) would be violated.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The workspace manager implementation is pending; this
// sensor verifies that WM-013d is correctly declared in the spec so that:
//
//  1. WM-013d heading is present in specs/workspace-model.md.
//  2. "MUST NOT be re-leased" is declared for a released workspace's canonical path.
//  3. "new canonical paths via new run_ids" is declared for subsequent runs.
//  4. "re-use of the path for a different run_id is forbidden" is declared.
//  5. "WM-INV-005" is cited as the invariant that would be violated.
//  6. Tags: mechanism is present in the WM-013d body window.
//
// # Failure modes
//
//   - WM-013d heading missing.
//   - MUST NOT be re-leased absent.
//   - new canonical paths via new run_ids absent.
//   - re-use forbidden absent.
//   - WM-INV-005 citation absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wm013dFixture prefix per
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

// wm013dFixtureWorkspaceModelPath returns the absolute path to specs/workspace-model.md.
func wm013dFixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wm013dFixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wm013dFixtureHeading matches the WM-013d level-4 requirement heading line.
var wm013dFixtureHeading = regexp.MustCompile(`^#### WM-013d —`)

// wm013dFixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wm013dFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wm013dFixtureTagsMechanism matches a "Tags: mechanism" line.
var wm013dFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wm013dFixtureBodyWindow is the maximum number of lines to scan after the heading.
const wm013dFixtureBodyWindow = 30

// wm013dFixtureLoadLines opens specFile and returns all lines.
func wm013dFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wm013dFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wm013dFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wm013dFixtureBodyLines returns the lines comprising the WM-013d body.
func wm013dFixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wm013dFixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-013d heading not found; expected '#### WM-013d —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wm013dFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wm013dFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wm013dFixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func wm013dFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestWM013dReleasedWorkspacePathReuseForbidden is the binding test for hk-8mwo.22.
func TestWM013dReleasedWorkspacePathReuseForbidden(t *testing.T) {
	t.Parallel()

	specFile := wm013dFixtureWorkspaceModelPath(t)
	lines := wm013dFixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wm013dFixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-013d check(1): %s", reason)
	}
	t.Logf("WM-013d heading found at specs/workspace-model.md line %d; body window = %d lines",
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
			label:  "must-not-be-re-leased",
			needle: "MUST NOT be re-leased",
			detail: "WM-013d body must declare 'MUST NOT be re-leased' for a released workspace's canonical path " +
				"(expected phrase 'MUST NOT be re-leased'); re-leasing the same path to a new run " +
				"would create a path collision with the retained failed-run worktree, violating " +
				"the canonical-path invariant WM-INV-005",
		},
		{
			id:     "3",
			label:  "new-canonical-paths-via-new-run-ids",
			needle: "new canonical paths via new",
			detail: "WM-013d body must declare 'new canonical paths via new' run_ids for subsequent runs " +
				"(expected phrase 'new canonical paths via new'); the canonical path formula " +
				"<repo>/.harmonik/worktrees/<run_id>/ ensures that every new run_id yields a " +
				"new, unused path — the path formula is the path-uniqueness mechanism",
		},
		{
			id:     "4",
			label:  "re-use-of-path-for-different-run-id-forbidden",
			needle: "re-use of the path for a different",
			detail: "WM-013d body must declare 're-use of the path for a different' run_id is forbidden " +
				"(expected phrase 're-use of the path for a different'); this closes the rule " +
				"explicitly — even if the prior run's worktree persists, the workspace manager " +
				"MUST NOT create a new lease pointing at that same path",
		},
		{
			id:     "5",
			label:  "wm-inv-005-cited",
			needle: "WM-INV-005",
			detail: "WM-013d body must cite 'WM-INV-005' as the invariant that would be violated " +
				"(expected phrase 'WM-INV-005'); WM-INV-005 is the canonical-path invariant that " +
				"requires workspace state to be derivable from run_id alone — path re-use would " +
				"make two different run_ids map to the same path, violating the invariant",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !wm013dFixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-013d check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-013d body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in WM-013d body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wm013dFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-013d check(6) FAILED: Tags: mechanism not found in WM-013d body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-013d body)\n"+
					"  detail: WM-013d carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mwo.22 audit complete — WM-013d heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
