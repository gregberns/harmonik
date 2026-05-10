package specaudit_test

// hk-63oh.40 binding test — RC-028: `reopen-bead` verdict MUST clear in-flight
// tracking; subsequent claim MUST produce a new run with fresh worktree, fresh
// branch, and fresh run_id.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-028.
//
// RC-028 states: A `reopen-bead` verdict MUST clear the in-flight tracking for
// the target bead. A subsequent claim of the bead MUST produce a new run with a
// fresh worktree and a fresh branch per [workspace-model.md §4.9 WM-034]. The
// new run MUST receive a fresh `run_id`; continuation of the prior `run_id`
// after a `reopen-bead` verdict is forbidden.
//
// # Audit frame
//
// This test is a spec-corpus sensor. It verifies that RC-028 is correctly
// declared in specs/reconciliation/spec.md so that:
//
//  1. RC-028 heading is present — the requirement exists in the normative spec.
//
//  2. "reopen-bead" is declared — the verdict name is named in the RC-028 body.
//
//  3. "in-flight tracking" clearing is declared — the obligation to clear
//     in-flight state is named explicitly.
//
//  4. "fresh worktree" is declared — the new run receives a fresh worktree
//     (distinguishing this from the intra-run rollback of RC-029).
//
//  5. "fresh branch" is declared — the new run receives a fresh task branch.
//
//  6. "fresh `run_id`" or "fresh run_id" is declared — the new run_id
//     obligation is stated explicitly.
//
//  7. "WM-034" is cited — the workspace-model requirement that governs
//     fresh-worktree creation on reopen-bead is referenced.
//
//  8. "forbidden" is declared — the prohibition on continuing the prior
//     run_id after a reopen-bead verdict uses a normative prohibition.
//
//  9. Tags: mechanism is present in the RC-028 body window.
//
// # Failure modes
//
//   - RC-028 heading missing: RC-028 heading not found in specs/reconciliation/spec.md.
//   - "reopen-bead" absent: verdict name not declared in RC-028 body.
//   - "in-flight tracking" absent: clearing obligation not declared.
//   - "fresh worktree" absent: fresh-worktree obligation not declared.
//   - "fresh branch" absent: fresh-branch obligation not declared.
//   - "fresh" and "run_id" absent: fresh run_id obligation not declared.
//   - "WM-034" absent: workspace-model cross-reference missing.
//   - "forbidden" absent: prior-run_id continuation prohibition not stated.
//   - Tags: mechanism missing from RC-028 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the rc028Fixture prefix per
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

// rc028FixtureRepoRoot resolves the repository root from this test file's
// source path. The test file lives at:
//
//	internal/specaudit/rc028_reopen_bead_new_run_test.go
//
// so the repo root is two directories up.
func rc028FixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("rc028FixtureRepoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// rc028FixtureSpecPath returns the absolute path to specs/reconciliation/spec.md.
func rc028FixtureSpecPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(rc028FixtureRepoRoot(t), "specs", "reconciliation", "spec.md")
}

// rc028FixtureLoadLines opens specFile and returns all lines.
func rc028FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("rc028FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("rc028FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// rc028FixtureRC028Heading matches the RC-028 level-4 requirement heading line.
var rc028FixtureRC028Heading = regexp.MustCompile("^#### RC-028 —")

// rc028FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of a requirement body window.
var rc028FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// rc028FixtureTagsMechanism matches a "Tags: mechanism" line.
var rc028FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// rc028FixtureBodyWindow is the maximum number of lines after a heading to
// scan for requirement-body content.
const rc028FixtureBodyWindow = 30

// rc028FixtureBodyLines returns the body lines of the RC-028 section: all lines
// after the matched heading up to (but not including) the next Markdown heading
// or rc028FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the heading is not found.
func rc028FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if rc028FixtureRC028Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "RC-028 heading not found; expected '#### RC-028 —' pattern in specs/reconciliation/spec.md"
	}

	limit := headingIdx + 1 + rc028FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if rc028FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

// rc028FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func rc028FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestRC028ReopenBeadNewRun is the binding test for hk-63oh.40.
//
// It opens specs/reconciliation/spec.md, locates the RC-028 heading, and
// validates the nine audit checks listed in the file-level comment.
func TestRC028ReopenBeadNewRun(t *testing.T) {
	t.Parallel()

	specFile := rc028FixtureSpecPath(t)
	lines := rc028FixtureLoadLines(t, specFile)

	// Check (1): RC-028 heading present.
	rc028Body, rc028LineNo, rc028Reason := rc028FixtureBodyLines(lines)
	if rc028Reason != "" {
		t.Fatalf("RC-028 check(1): %s", rc028Reason)
	}
	t.Logf("RC-028 heading found at specs/reconciliation/spec.md line %d; body window = %d lines",
		rc028LineNo, len(rc028Body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	rc028Checks := []check{
		{
			id:     "2",
			label:  "reopen-bead verdict name",
			needle: "reopen-bead",
			detail: "RC-028 body must name 'reopen-bead' as the verdict this requirement " +
				"governs; the verdict name binds the requirement to the seven-value enum of RC-020",
		},
		{
			id:     "3",
			label:  "in-flight tracking clearing obligation",
			needle: "in-flight tracking",
			detail: "RC-028 body must declare that 'in-flight tracking' is cleared for the " +
				"target bead; clearing the in-flight tracking is what allows the bead to be " +
				"re-claimed and produces a new run rather than re-entering the prior run",
		},
		{
			id:     "4",
			label:  "fresh worktree obligation",
			needle: "fresh worktree",
			detail: "RC-028 body must declare that the new run receives a 'fresh worktree'; " +
				"this is the primary distinction from the intra-run rollback of RC-029 " +
				"(reset-to-checkpoint), which preserves the worktree",
		},
		{
			id:     "5",
			label:  "fresh branch obligation",
			needle: "fresh branch",
			detail: "RC-028 body must declare that the new run receives a 'fresh branch'; " +
				"the task branch isolation per workspace-model.md §4.9 requires a new branch " +
				"for each run so the prior run's git history is not entangled with the new run",
		},
		{
			id:     "6",
			label:  "fresh run_id obligation",
			needle: "fresh `run_id`",
			detail: "RC-028 body must declare that the new run receives a 'fresh `run_id`'; " +
				"a new run_id is the normative identifier that separates the new run from the " +
				"prior run — downstream components use run_id to scope their queries",
		},
		{
			id:     "7",
			label:  "WM-034 workspace-model cross-reference",
			needle: "WM-034",
			detail: "RC-028 body must cite 'WM-034' from workspace-model.md §4.9; this is the " +
				"workspace-model requirement that defines the fresh-worktree creation " +
				"procedure triggered by a reopen-bead verdict",
		},
		{
			id:     "8",
			label:  "prior run_id continuation forbidden",
			needle: "forbidden",
			detail: "RC-028 body must declare that continuation of the prior run_id is " +
				"'forbidden'; a normative prohibition is required so implementations cannot " +
				"reuse the old run_id under any circumstances after a reopen-bead verdict",
		},
	}

	for _, c := range rc028Checks {
		c := c
		t.Run(fmt.Sprintf("RC028-check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !rc028FixtureBodyContains(rc028Body, c.needle) {
				t.Errorf(
					"RC-028 check(%s) FAILED: %s\n"+
						"  spec:    specs/reconciliation/spec.md line ~%d (RC-028 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, rc028LineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (9): Tags: mechanism in RC-028 body.
	t.Run("RC028-check-9-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range rc028Body {
			if rc028FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"RC-028 check(9) FAILED: Tags: mechanism not found in RC-028 body window\n"+
					"  spec:   specs/reconciliation/spec.md line ~%d (RC-028 body)\n"+
					"  detail: RC-028 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				rc028LineNo,
			)
		}
	})

	t.Logf("hk-63oh.40 audit complete — RC-028 heading at line %d (body %d lines)",
		rc028LineNo, len(rc028Body))
}
