package specaudit_test

// hk-63oh.41 binding test — RC-029 intra-run rollbacks keep the worktree and run_id.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-029.
//
// RC-029 states: a reset-to-checkpoint verdict is an intra-run rollback. The worktree
// and run_id MUST be preserved; the run reverts to the named checkpoint and re-runs from
// there per execution-model.md §4.10 EM-044 (transition_kind + rollback_to_state_id
// representation).
//
// # Audit frame
//
// This test is a spec-corpus sensor. The reconciliation verdict execution implementation
// is pending; this sensor verifies that RC-029 is correctly declared in the spec so that:
//
//  1. RC-029 heading is present in specs/reconciliation/spec.md.
//  2. "reset-to-checkpoint" is named as the triggering verdict.
//  3. "intra-run rollback" classification is declared.
//  4. "worktree" MUST be preserved is declared.
//  5. "run_id" MUST be preserved is declared.
//  6. "rollback_to_state_id" representation is cited.
//  7. Tags: mechanism is present in the RC-029 body window.
//
// # Failure modes
//
//   - RC-029 heading missing.
//   - reset-to-checkpoint verdict absent.
//   - intra-run rollback absent.
//   - worktree preservation absent.
//   - run_id preservation absent.
//   - rollback_to_state_id absent.
//   - Tags: mechanism missing.
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

// rc029FixtureReconciliationSpecPath returns the absolute path to specs/reconciliation/spec.md.
func rc029FixtureReconciliationSpecPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("rc029FixtureReconciliationSpecPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "reconciliation", "spec.md")
}

// rc029FixtureRC029Heading matches the RC-029 level-4 requirement heading line.
var rc029FixtureRC029Heading = regexp.MustCompile(`^#### RC-029 —`)

// rc029FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var rc029FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// rc029FixtureTagsMechanism matches a "Tags: mechanism" line.
var rc029FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// rc029FixtureBodyWindow is the maximum number of lines after the RC-029
// heading to scan for requirement-body content.
const rc029FixtureBodyWindow = 30

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

// rc029FixtureRC029BodyLines returns the lines comprising the RC-029 body.
func rc029FixtureRC029BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if rc029FixtureRC029Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "RC-029 heading not found; expected '#### RC-029 —' in specs/reconciliation/spec.md"
	}

	limit := headingIdx + 1 + rc029FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if rc029FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// rc029FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func rc029FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestRC029IntraRunRollbackKeepsWorktree is the binding test for hk-63oh.41.
func TestRC029IntraRunRollbackKeepsWorktree(t *testing.T) {
	t.Parallel()

	specFile := rc029FixtureReconciliationSpecPath(t)
	lines := rc029FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := rc029FixtureRC029BodyLines(lines)
	if reason != "" {
		t.Fatalf("RC-029 check(1): %s", reason)
	}
	t.Logf("RC-029 heading found at specs/reconciliation/spec.md line %d; body window = %d lines",
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
			label:  "reset-to-checkpoint-verdict",
			needle: "reset-to-checkpoint",
			detail: "RC-029 body must name 'reset-to-checkpoint' as the triggering verdict " +
				"(expected phrase 'reset-to-checkpoint'); this is the specific verdict value " +
				"from the seven-value enum that triggers an intra-run rollback — other verdicts " +
				"(reopen-bead) trigger a fresh worktree per WM-034",
		},
		{
			id:     "3",
			label:  "intra-run-rollback-classification",
			needle: "intra-run rollback",
			detail: "RC-029 body must declare 'intra-run rollback' as the classification " +
				"(expected phrase 'intra-run rollback'); this term distinguishes reset-to-checkpoint " +
				"from reopen-bead (which is a re-run) — the distinction determines whether " +
				"worktree and run_id are preserved or replaced",
		},
		{
			id:     "4",
			label:  "worktree-must-be-preserved",
			needle: "worktree",
			detail: "RC-029 body must declare the 'worktree' MUST be preserved on intra-run rollback " +
				"(expected phrase 'worktree'); preserving the worktree means git operations revert " +
				"the working tree in-place rather than creating a new worktree at a new path — " +
				"this allows the run to continue from the rollback checkpoint without path changes",
		},
		{
			id:     "5",
			label:  "run-id-must-be-preserved",
			needle: "run_id",
			detail: "RC-029 body must declare the 'run_id' MUST be preserved on intra-run rollback " +
				"(expected phrase 'run_id'); preserving the run_id means all events and checkpoints " +
				"in the rollback continuation share the same run identity — the run is continuous, " +
				"not a new run branched from a checkpoint",
		},
		{
			id:     "6",
			label:  "rollback-to-state-id-representation",
			needle: "rollback_to_state_id",
			detail: "RC-029 body must cite 'rollback_to_state_id' as the representation field " +
				"(expected phrase 'rollback_to_state_id'); this field on the EM-044 transition record " +
				"is the mechanical driver of the rollback — it names which checkpoint state_id to " +
				"revert to, enabling the execution model to re-run from that exact checkpoint",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !rc029FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"RC-029 check(%s) FAILED: %s\n"+
						"  spec:    specs/reconciliation/spec.md line ~%d (RC-029 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in RC-029 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if rc029FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"RC-029 check(7) FAILED: Tags: mechanism not found in RC-029 body window\n"+
					"  spec:   specs/reconciliation/spec.md line ~%d (RC-029 body)\n"+
					"  detail: RC-029 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-63oh.41 audit complete — RC-029 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
