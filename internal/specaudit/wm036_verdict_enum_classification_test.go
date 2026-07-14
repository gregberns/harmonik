//go:build specaudit

package specaudit_test

// hk-8mwo.48 binding test — WM-036 re-run vs intra-run classification is deterministic
// on the seven-value verdict enum.
//
// Spec ref: specs/workspace-model.md §4.9 WM-036.
//
// WM-036 states: the decision between "fresh worktree" (WM-034) and "keep worktree"
// (WM-035) MUST be deterministic on the verdict enum value declared in
// reconciliation/schemas.md §6.1 Verdict. No cognition participates in the classification.
// The authoritative classification maps the seven verdict values to workspace dispositions.
// Any verdict value not in the table is malformed per RC-023 and MUST NOT be routed.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The workspace manager implementation is pending; this
// sensor verifies that WM-036 is correctly declared in the spec so that:
//
//  1. WM-036 heading is present in specs/workspace-model.md.
//  2. "MUST be deterministic" is declared.
//  3. "No cognition participates" is declared.
//  4. "no-op-accept" verdict row is present in the classification table.
//  5. "RC-023" is named as the malformed-verdict rule.
//  6. "MUST NOT be routed" is declared for malformed verdicts.
//  7. Tags: mechanism is present in the WM-036 body window.
//
// # Failure modes
//
//   - WM-036 heading missing.
//   - MUST be deterministic absent.
//   - No cognition participates absent.
//   - no-op-accept absent.
//   - RC-023 absent.
//   - MUST NOT be routed absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wm036Fixture prefix per
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

// wm036FixtureWorkspaceModelPath returns the absolute path to specs/workspace-model.md.
func wm036FixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wm036FixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wm036FixtureHeading matches the WM-036 level-4 requirement heading line.
var wm036FixtureHeading = regexp.MustCompile(`^#### WM-036 —`)

// wm036FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wm036FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wm036FixtureTagsMechanism matches a "Tags: mechanism" line.
var wm036FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wm036FixtureBodyWindow is the maximum number of lines to scan after the heading.
// Extended to 25 to capture the full seven-row verdict disposition table.
const wm036FixtureBodyWindow = 25

// wm036FixtureLoadLines opens specFile and returns all lines.
func wm036FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wm036FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wm036FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wm036FixtureBodyLines returns the lines comprising the WM-036 body.
func wm036FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wm036FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-036 heading not found; expected '#### WM-036 —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wm036FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wm036FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wm036FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func wm036FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestWM036VerdictEnumClassificationDeterministic is the binding test for hk-8mwo.48.
func TestWM036VerdictEnumClassificationDeterministic(t *testing.T) {
	t.Parallel()

	specFile := wm036FixtureWorkspaceModelPath(t)
	lines := wm036FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wm036FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-036 check(1): %s", reason)
	}
	t.Logf("WM-036 heading found at specs/workspace-model.md line %d; body window = %d lines",
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
			label:  "must-be-deterministic",
			needle: "MUST be deterministic",
			detail: "WM-036 body must declare 'MUST be deterministic' for the classification " +
				"(expected phrase 'MUST be deterministic'); determinism on the verdict enum value " +
				"means the workspace manager's routing decision is a pure function of the enum — " +
				"no runtime conditions, history, or operator input can alter it",
		},
		{
			id:     "3",
			label:  "no-cognition-participates",
			needle: "No cognition participates",
			detail: "WM-036 body must declare 'No cognition participates' in the classification " +
				"(expected phrase 'No cognition participates'); this is the explicit statement that " +
				"the workspace disposition routing is implemented as a switch/lookup table — " +
				"no LLM inference or heuristic is involved",
		},
		{
			id:     "4",
			label:  "no-op-accept-row-present",
			needle: "no-op-accept",
			detail: "WM-036 body must include 'no-op-accept' verdict row in the classification table " +
				"(expected phrase 'no-op-accept'); this is the seventh verdict value added in RC v0.3.0 " +
				"that resolves OQ-RC-011 — its absence would indicate the seven-row table is incomplete",
		},
		{
			id:     "5",
			label:  "rc-023-malformed-verdict",
			needle: "RC-023",
			detail: "WM-036 body must name 'RC-023' as the malformed-verdict rule " +
				"(expected phrase 'RC-023'); RC-023 is the reconciliation rule that defines what " +
				"happens when the workspace manager receives a verdict value not in the enum — " +
				"the classifier returns a classification-error and emits no workspace action",
		},
		{
			id:     "6",
			label:  "must-not-be-routed",
			needle: "MUST NOT be routed",
			detail: "WM-036 body must declare 'MUST NOT be routed' for malformed verdicts " +
				"(expected phrase 'MUST NOT be routed'); routing a malformed verdict to a workspace " +
				"disposition handler would cause unpredictable state mutations — the classifier MUST " +
				"return an error to the caller instead",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !wm036FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-036 check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-036 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in WM-036 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wm036FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-036 check(7) FAILED: Tags: mechanism not found in WM-036 body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-036 body)\n"+
					"  detail: WM-036 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mwo.48 audit complete — WM-036 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
