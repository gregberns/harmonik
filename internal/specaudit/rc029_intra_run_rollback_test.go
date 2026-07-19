//go:build specaudit

package specaudit_test

// hk-63oh.41 binding test — RC-029: `reset-to-checkpoint` is intra-run rollback;
// worktree and run_id MUST be preserved.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-029.
//
// RC-029 states: A `reset-to-checkpoint` verdict is an intra-run rollback. The
// worktree and `run_id` MUST be preserved; the run reverts to the named checkpoint
// and re-runs from there per [execution-model.md §4.10 EM-044]
// (transition_kind + rollback_to_state_id representation).
//
// # Audit frame
//
// This test is a spec-corpus sensor. It verifies that RC-029 is correctly
// declared in specs/reconciliation/spec.md so that:
//
//  1. RC-029 heading is present — the requirement exists in the normative spec.
//
//  2. "reset-to-checkpoint" is declared — the verdict name is named explicitly
//     in the RC-029 body.
//
//  3. "intra-run rollback" is declared — the classification of this verdict as
//     an intra-run operation (not a re-run) is explicit.
//
//  4. "worktree" preservation is declared — the worktree MUST be preserved
//     (not destroyed and recreated as with reopen-bead).
//
//  5. "run_id" preservation is declared — the run_id MUST be preserved;
//     continuation on the same run_id is the defining distinction from RC-028.
//
//  6. "MUST be preserved" is declared — the normative obligation (RFC 2119 MUST)
//     for worktree+run_id preservation is stated.
//
//  7. "EM-044" is cited — the execution-model requirement that governs the
//     transition_kind + rollback_to_state_id representation is referenced.
//
//  8. "rollback_to_state_id" is declared — the transition-record field that
//     names the target checkpoint is cited in the RC-029 body.
//
//  9. Tags: mechanism is present in the RC-029 body window.
//
// # Failure modes
//
//   - RC-029 heading missing: RC-029 heading not found in specs/reconciliation/spec.md.
//   - "reset-to-checkpoint" absent: verdict name not declared in RC-029 body.
//   - "intra-run rollback" absent: intra-run classification not stated.
//   - "worktree" absent: worktree-preservation obligation not declared.
//   - "run_id" absent: run_id-preservation obligation not declared.
//   - "MUST be preserved" absent: normative preservation obligation not stated.
//   - "EM-044" absent: execution-model cross-reference missing.
//   - "rollback_to_state_id" absent: transition-record field not named.
//   - Tags: mechanism missing from RC-029 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the rc029Fixture prefix per
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

// rc029FixtureRepoRoot resolves the repository root from this test file's
// source path. The test file lives at:
//
//	internal/specaudit/rc029_intra_run_rollback_test.go
//
// so the repo root is two directories up.
func rc029FixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("rc029FixtureRepoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// rc029FixtureSpecPath returns the absolute path to specs/reconciliation/spec.md.
func rc029FixtureSpecPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(rc029FixtureRepoRoot(t), "specs", "reconciliation", "spec.md")
}

// rc029FixtureLoadLines opens specFile and returns all lines.
func rc029FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("rc029FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("rc029FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// rc029FixtureRC029Heading matches the RC-029 level-4 requirement heading line.
var rc029FixtureRC029Heading = regexp.MustCompile(`^#### RC-029 —`)

// rc029FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of a requirement body window.
var rc029FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// rc029FixtureTagsMechanism matches a "Tags: mechanism" line.
var rc029FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// rc029FixtureBodyWindow is the maximum number of lines after a heading to
// scan for requirement-body content.
const rc029FixtureBodyWindow = 30

// rc029FixtureBodyLines returns the body lines of the RC-029 section: all lines
// after the matched heading up to (but not including) the next Markdown heading
// or rc029FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the heading is not found.
func rc029FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if rc029FixtureRC029Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "RC-029 heading not found; expected '#### RC-029 —' pattern in specs/reconciliation/spec.md"
	}

	limit := headingIdx + 1 + rc029FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if rc029FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

// rc029FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func rc029FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestRC029IntraRunRollback is the binding test for hk-63oh.41.
//
// It opens specs/reconciliation/spec.md, locates the RC-029 heading, and
// validates the nine audit checks listed in the file-level comment.
func TestRC029IntraRunRollback(t *testing.T) {
	t.Parallel()

	specFile := rc029FixtureSpecPath(t)
	lines := rc029FixtureLoadLines(t, specFile)

	// Check (1): RC-029 heading present.
	rc029Body, rc029LineNo, rc029Reason := rc029FixtureBodyLines(lines)
	if rc029Reason != "" {
		t.Fatalf("RC-029 check(1): %s", rc029Reason)
	}
	t.Logf("RC-029 heading found at specs/reconciliation/spec.md line %d; body window = %d lines",
		rc029LineNo, len(rc029Body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	rc029Checks := []check{
		{
			id:     "2",
			label:  "reset-to-checkpoint verdict name",
			needle: "reset-to-checkpoint",
			detail: "RC-029 body must name 'reset-to-checkpoint' as the verdict this requirement " +
				"governs; the verdict name binds the requirement to the seven-value enum of RC-020",
		},
		{
			id:     "3",
			label:  "intra-run rollback classification",
			needle: "intra-run rollback",
			detail: "RC-029 body must classify reset-to-checkpoint as an 'intra-run rollback'; " +
				"this distinguishes it from reopen-bead (RC-028) which is an inter-run re-claim " +
				"that produces a fresh run_id and fresh worktree",
		},
		{
			id:     "4",
			label:  "worktree preservation obligation",
			needle: "worktree",
			detail: "RC-029 body must declare that the 'worktree' is preserved; the worktree " +
				"is the unit of isolation for an agent run and its preservation is what makes " +
				"reset-to-checkpoint an intra-run operation rather than a fresh dispatch",
		},
		{
			id:     "5",
			label:  "run_id preservation obligation",
			needle: "run_id",
			detail: "RC-029 body must declare that the 'run_id' is preserved; run_id continuity " +
				"is the defining property of an intra-run rollback — the daemon continues " +
				"tracking state under the same run identity after reverting to the checkpoint",
		},
		{
			id:     "6",
			label:  "MUST be preserved normative obligation",
			needle: "MUST be preserved",
			detail: "RC-029 body must contain 'MUST be preserved' to state the normative " +
				"obligation (RFC 2119 MUST) for worktree and run_id; a MUST is required " +
				"so implementers cannot interpret preservation as optional",
		},
		{
			id:     "7",
			label:  "EM-044 execution-model cross-reference",
			needle: "EM-044",
			detail: "RC-029 body must cite 'EM-044' from execution-model.md §4.10; this is " +
				"the requirement that defines the transition_kind + rollback_to_state_id " +
				"representation used to record the intra-run rollback in the transition log",
		},
		{
			id:     "8",
			label:  "rollback_to_state_id transition-record field",
			needle: "rollback_to_state_id",
			detail: "RC-029 body must name 'rollback_to_state_id' as the transition-record " +
				"field that identifies the target checkpoint; this field is the concrete " +
				"representation of the rollback target in the execution-model transition record",
		},
	}

	for _, c := range rc029Checks {
		c := c
		t.Run(fmt.Sprintf("RC029-check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !rc029FixtureBodyContains(rc029Body, c.needle) {
				t.Errorf(
					"RC-029 check(%s) FAILED: %s\n"+
						"  spec:    specs/reconciliation/spec.md line ~%d (RC-029 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, rc029LineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (9): Tags: mechanism in RC-029 body.
	t.Run("RC029-check-9-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range rc029Body {
			if rc029FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"RC-029 check(9) FAILED: Tags: mechanism not found in RC-029 body window\n"+
					"  spec:   specs/reconciliation/spec.md line ~%d (RC-029 body)\n"+
					"  detail: RC-029 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				rc029LineNo,
			)
		}
	})

	t.Logf("hk-63oh.41 audit complete — RC-029 heading at line %d (body %d lines)",
		rc029LineNo, len(rc029Body))
}
