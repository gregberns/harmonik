package specaudit_test

// hk-63oh.15 binding test — RC-011 ordering uses git DAG parentage and UUID v7,
// not wall clock.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-011.
//
// RC-011 states: detectors MUST order checkpoints within a run's task branch by git DAG
// parentage (parent-pointer chain) and events by UUID v7 event_id per event-model.md §4.1.
// Wall-clock timestamps (timestamp_wall, git commit author_date/committer_date) MUST NOT
// drive classification decisions. The most-recent checkpoint for a run MUST be identified
// as the tip of the run's task branch (the commit with no child in the run's
// branch-subgraph), NOT the commit with the latest wall-clock timestamp.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The reconciliation detector implementation is pending;
// this sensor verifies that RC-011 is correctly declared in the spec so that:
//
//  1. RC-011 heading is present in specs/reconciliation/spec.md.
//  2. "git DAG parentage" is declared as the checkpoint ordering mechanism.
//  3. "UUID v7" is declared for event ordering.
//  4. "MUST NOT drive classification" for wall-clock timestamps is declared.
//  5. "tip of the run's task branch" is declared as the most-recent checkpoint criterion.
//  6. Tags: mechanism is present in the RC-011 body window.
//
// # Failure modes
//
//   - RC-011 heading missing.
//   - git DAG parentage absent.
//   - UUID v7 absent.
//   - MUST NOT drive classification absent.
//   - tip of the run's task branch absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the rc011Fixture prefix per
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

// rc011FixtureReconciliationSpecPath returns the absolute path to specs/reconciliation/spec.md.
func rc011FixtureReconciliationSpecPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("rc011FixtureReconciliationSpecPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "reconciliation", "spec.md")
}

// rc011FixtureRC011Heading matches the RC-011 level-4 requirement heading line.
var rc011FixtureRC011Heading = regexp.MustCompile(`^#### RC-011 —`)

// rc011FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var rc011FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// rc011FixtureTagsMechanism matches a "Tags: mechanism" line.
var rc011FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// rc011FixtureBodyWindow is the maximum number of lines after the RC-011
// heading to scan for requirement-body content.
const rc011FixtureBodyWindow = 30

// rc011FixtureLoadLines opens specFile and returns all lines.
func rc011FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("rc011FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("rc011FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// rc011FixtureRC011BodyLines returns the lines comprising the RC-011 body.
func rc011FixtureRC011BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if rc011FixtureRC011Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "RC-011 heading not found; expected '#### RC-011 —' in specs/reconciliation/spec.md"
	}

	limit := headingIdx + 1 + rc011FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if rc011FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// rc011FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func rc011FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestRC011OrderingGitDAGAndUUIDv7 is the binding test for hk-63oh.15.
func TestRC011OrderingGitDAGAndUUIDv7(t *testing.T) {
	t.Parallel()

	specFile := rc011FixtureReconciliationSpecPath(t)
	lines := rc011FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := rc011FixtureRC011BodyLines(lines)
	if reason != "" {
		t.Fatalf("RC-011 check(1): %s", reason)
	}
	t.Logf("RC-011 heading found at specs/reconciliation/spec.md line %d; body window = %d lines",
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
			label:  "git-dag-parentage-checkpoint-ordering",
			needle: "git DAG parentage",
			detail: "RC-011 body must declare 'git DAG parentage' as the checkpoint ordering mechanism " +
				"(expected phrase 'git DAG parentage'); checkpoints are git commits — their causal order " +
				"is encoded in the parent-pointer chain of the DAG, not in author_date/committer_date " +
				"which can be backdated, clock-skewed, or identical for fast commits",
		},
		{
			id:     "3",
			label:  "uuid-v7-event-ordering",
			needle: "UUID v7",
			detail: "RC-011 body must declare 'UUID v7' for event ordering " +
				"(expected phrase 'UUID v7'); events in the JSONL log are ordered by event_id which " +
				"MUST be a UUID v7 per event-model.md §4.1 — UUID v7 embeds a millisecond-precision " +
				"timestamp in the high bits, yielding total lexicographic ordering within a run",
		},
		{
			id:     "4",
			label:  "must-not-drive-classification-wall-clock",
			needle: "MUST NOT drive classification",
			detail: "RC-011 body must declare wall-clock timestamps 'MUST NOT drive classification' decisions " +
				"(expected phrase 'MUST NOT drive classification'); wall-clock timestamps from git " +
				"(author_date, committer_date) and JSONL (timestamp_wall) are unreliable for ordering — " +
				"they can be set to arbitrary values by the committing process or system clock drift",
		},
		{
			id:     "5",
			label:  "tip-of-task-branch-most-recent",
			needle: "tip of the run's task branch",
			detail: "RC-011 body must declare 'tip of the run's task branch' as the most-recent checkpoint criterion " +
				"(expected phrase 'tip of the run's task branch'); the tip is the commit with no child " +
				"in the run's branch-subgraph — it is unambiguous even when multiple commits share the " +
				"same wall-clock timestamp (e.g., fast automated commits within a single second)",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !rc011FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"RC-011 check(%s) FAILED: %s\n"+
						"  spec:    specs/reconciliation/spec.md line ~%d (RC-011 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in RC-011 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if rc011FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"RC-011 check(6) FAILED: Tags: mechanism not found in RC-011 body window\n"+
					"  spec:   specs/reconciliation/spec.md line ~%d (RC-011 body)\n"+
					"  detail: RC-011 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-63oh.15 audit complete — RC-011 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
