//go:build specaudit

package specaudit_test

// hk-8mwo.47 binding test — WM-035 intra-run rollback verdicts keep the same worktree
// and task branch.
//
// Spec ref: specs/workspace-model.md §4.9 WM-035.
//
// WM-035 states: intra-run rollback verdicts — resume-here, resume-with-context,
// reset-to-checkpoint per reconciliation/spec.md §4.5 RC-020 — MUST keep the same
// worktree and the same task branch. The run's run_id is unchanged; the state reverts
// to the named checkpoint via git operations inside the existing worktree per
// execution-model.md §4.10 EM-044. The rollback_to_state_id field on the transition
// record (EM-044) is the mechanical driver of the rollback.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The workspace manager implementation is pending; this
// sensor verifies that WM-035 is correctly declared in the spec so that:
//
//  1. WM-035 heading is present in specs/workspace-model.md.
//  2. "resume-here" is named as an intra-run rollback verdict.
//  3. "MUST keep the same worktree" is declared.
//  4. "run_id is unchanged" is declared for intra-run rollbacks.
//  5. "rollback_to_state_id" is named as the mechanical driver.
//  6. Tags: mechanism is present in the WM-035 body window.
//
// # Failure modes
//
//   - WM-035 heading missing.
//   - resume-here absent.
//   - MUST keep the same worktree absent.
//   - run_id is unchanged absent.
//   - rollback_to_state_id absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wm035Fixture prefix per
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

// wm035FixtureWorkspaceModelPath returns the absolute path to specs/workspace-model.md.
func wm035FixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wm035FixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wm035FixtureHeading matches the WM-035 level-4 requirement heading line.
var wm035FixtureHeading = regexp.MustCompile(`^#### WM-035 —`)

// wm035FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wm035FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wm035FixtureTagsMechanism matches a "Tags: mechanism" line.
var wm035FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wm035FixtureBodyWindow is the maximum number of lines to scan after the heading.
const wm035FixtureBodyWindow = 30

// wm035FixtureLoadLines opens specFile and returns all lines.
func wm035FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wm035FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wm035FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wm035FixtureBodyLines returns the lines comprising the WM-035 body.
func wm035FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wm035FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-035 heading not found; expected '#### WM-035 —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wm035FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wm035FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wm035FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func wm035FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestWM035IntraRunRollbackKeepWorktree is the binding test for hk-8mwo.47.
func TestWM035IntraRunRollbackKeepWorktree(t *testing.T) {
	t.Parallel()

	specFile := wm035FixtureWorkspaceModelPath(t)
	lines := wm035FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wm035FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-035 check(1): %s", reason)
	}
	t.Logf("WM-035 heading found at specs/workspace-model.md line %d; body window = %d lines",
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
			label:  "resume-here-named",
			needle: "resume-here",
			detail: "WM-035 body must name 'resume-here' as an intra-run rollback verdict " +
				"(expected phrase 'resume-here'); resume-here is one of three intra-run rollback " +
				"verdict values (alongside resume-with-context and reset-to-checkpoint) that keep " +
				"the same worktree and task branch per RC-020",
		},
		{
			id:     "3",
			label:  "must-keep-same-worktree",
			needle: "MUST keep the same worktree",
			detail: "WM-035 body must declare 'MUST keep the same worktree' for intra-run rollbacks " +
				"(expected phrase 'MUST keep the same worktree'); keeping the same worktree is the " +
				"defining characteristic of intra-run rollback — distinct from reopen-bead which " +
				"creates a fresh worktree per WM-034",
		},
		{
			id:     "4",
			label:  "run-id-unchanged",
			needle: "run_id",
			detail: "WM-035 body must declare 'run_id' is unchanged for intra-run rollbacks " +
				"(expected phrase 'run_id'); the run_id identity continuity is what makes this " +
				"an intra-run (not a re-run) rollback — the same run continues with state reverted " +
				"to the named checkpoint",
		},
		{
			id:     "5",
			label:  "rollback-to-state-id-mechanical-driver",
			needle: "rollback_to_state_id",
			detail: "WM-035 body must name 'rollback_to_state_id' as the mechanical driver " +
				"(expected phrase 'rollback_to_state_id'); this field on the EM-044 transition " +
				"record identifies the checkpoint state to roll back to — the workspace manager " +
				"reads this field to determine the target git commit for the rollback operation",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !wm035FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-035 check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-035 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in WM-035 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wm035FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-035 check(6) FAILED: Tags: mechanism not found in WM-035 body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-035 body)\n"+
					"  detail: WM-035 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mwo.47 audit complete — WM-035 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
