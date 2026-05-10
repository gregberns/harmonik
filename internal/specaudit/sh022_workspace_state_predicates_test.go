package specaudit_test

// hk-i0tw.24 binding test — SH-022 workspace-state predicates inspect the captured
// snapshot in-place.
//
// Spec ref: specs/scenario-harness.md §4.7 SH-022.
//
// SH-022 states: the workspace_state assertion kind MUST be evaluated against the
// per-scenario worktree tree in-place (per SH-015a) — NOT against a copy or archive.
// The harness MUST NOT mutate the workspace during evaluation. Predicates MUST address
// files by repo-relative path; absolute-path predicates are forbidden. Symlinks under
// the worktree that resolve outside the worktree MUST be rejected as assertion-failed.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The harness implementation is pending; this sensor
// verifies that SH-022 is correctly declared in the spec so that:
//
//  1. SH-022 heading is present in specs/scenario-harness.md.
//  2. "NOT against a copy or archive" is declared.
//  3. "MUST NOT mutate the workspace during evaluation" is declared.
//  4. "repo-relative path" is declared as the required predicate addressing.
//  5. "path traversal" is named as the symlink rejection case.
//  6. Tags: mechanism is present in the SH-022 body window.
//
// # Failure modes
//
//   - SH-022 heading missing.
//   - NOT against a copy or archive absent.
//   - MUST NOT mutate absent.
//   - repo-relative path absent.
//   - path traversal absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sh022Fixture prefix per
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

// sh022FixtureScenarioHarnessPath returns the absolute path to specs/scenario-harness.md.
func sh022FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh022FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// sh022FixtureHeading matches the SH-022 level-4 requirement heading line.
var sh022FixtureHeading = regexp.MustCompile(`^#### SH-022 —`)

// sh022FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var sh022FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sh022FixtureTagsMechanism matches a "Tags: mechanism" line.
var sh022FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// sh022FixtureBodyWindow is the maximum number of lines to scan after the heading.
const sh022FixtureBodyWindow = 15

// sh022FixtureLoadLines opens specFile and returns all lines.
func sh022FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh022FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh022FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sh022FixtureBodyLines returns the lines comprising the SH-022 body.
func sh022FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh022FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-022 heading not found; expected '#### SH-022 —' in specs/scenario-harness.md"
	}

	limit := headingIdx + 1 + sh022FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if sh022FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// sh022FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func sh022FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH022WorkspaceStatePredicatesInPlace is the binding test for hk-i0tw.24.
func TestSH022WorkspaceStatePredicatesInPlace(t *testing.T) {
	t.Parallel()

	specFile := sh022FixtureScenarioHarnessPath(t)
	lines := sh022FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh022FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-022 check(1): %s", reason)
	}
	t.Logf("SH-022 heading found at specs/scenario-harness.md line %d; body window = %d lines",
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
			label:  "not-a-copy-or-archive",
			needle: "NOT against a copy or archive",
			detail: "SH-022 body must declare evaluation is 'NOT against a copy or archive' " +
				"(expected phrase 'NOT against a copy or archive'); the workspace_state assertion " +
				"kind evaluates the actual in-place worktree — not a snapshot copy — so that git " +
				"plumbing (git rev-parse, git log) works correctly against the real .git directory",
		},
		{
			id:     "3",
			label:  "must-not-mutate-workspace",
			needle: "MUST NOT mutate the workspace during evaluation",
			detail: "SH-022 body must declare harness 'MUST NOT mutate the workspace during evaluation' " +
				"(expected phrase 'MUST NOT mutate the workspace during evaluation'); evaluation is " +
				"read-only — any write to the workspace during assertion evaluation would invalidate " +
				"subsequent predicates and corrupt the scenario result",
		},
		{
			id:     "4",
			label:  "repo-relative-path-addressing",
			needle: "repo-relative path",
			detail: "SH-022 body must declare 'repo-relative path' as required predicate addressing " +
				"(expected phrase 'repo-relative path'); absolute-path predicates would make scenarios " +
				"non-portable across operator machines — repo-relative paths ensure the same scenario " +
				"works on any machine regardless of checkout location",
		},
		{
			id:     "5",
			label:  "symlink-path-traversal-rejection",
			needle: "path traversal",
			detail: "SH-022 body must name 'path traversal' as the symlink rejection case " +
				"(expected phrase 'path traversal'); a symlink under the worktree that resolves " +
				"outside the worktree boundary is a path traversal attack vector — the harness " +
				"MUST reject such predicates as assertion-failed rather than evaluating them",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !sh022FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-022 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-022 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in SH-022 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh022FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-022 check(6) FAILED: Tags: mechanism not found in SH-022 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-022 body)\n"+
					"  detail: SH-022 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-i0tw.24 audit complete — SH-022 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
