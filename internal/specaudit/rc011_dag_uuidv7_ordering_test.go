package specaudit_test

// hk-63oh.15 binding test — RC-011: detectors MUST order checkpoints by git DAG
// parentage and events by UUIDv7 event_id; wall-clock MUST NOT drive classification.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-011.
//
// RC-011 states: Detectors MUST order checkpoints within a run's task branch by
// git DAG parentage (parent-pointer chain) and events by UUID v7 event_id per
// [event-model.md §4.1]. Wall-clock timestamps (timestamp_wall, git commit
// author_date / committer_date) MUST NOT drive classification decisions. The
// most-recent checkpoint for a run MUST be identified as the tip of the run's
// task branch (the commit with no child in the run's branch-subgraph), NOT the
// commit with the latest wall-clock timestamp.
//
// # Audit frame
//
// This test is a spec-corpus sensor. It verifies that RC-011 is correctly
// declared in specs/reconciliation/spec.md so that:
//
//  1. RC-011 heading is present — the requirement exists in the normative spec.
//
//  2. "git DAG parentage" is declared — the ordering mechanism for checkpoints
//     is named explicitly (parent-pointer chain, not wall clock).
//
//  3. "UUID v7" is declared — the ordering mechanism for events is named.
//
//  4. "event_id" is declared — the UUIDv7 field name is called out in RC-011 body.
//
//  5. "MUST NOT" is declared in context of wall-clock — the prohibition on
//     wall-clock-driven classification is explicit.
//
//  6. "timestamp_wall" is declared — the specific prohibited timestamp field is named.
//
//  7. "author_date" or "committer_date" is declared — the git commit timestamp
//     fields that MUST NOT drive classification are named.
//
//  8. "tip of the run's task branch" is declared — the definition of
//     most-recent checkpoint uses branch tip, not wall-clock ordering.
//
//  9. Tags: mechanism is present in the RC-011 body window.
//
// # Failure modes
//
//   - RC-011 heading missing: RC-011 heading not found in specs/reconciliation/spec.md.
//   - "git DAG parentage" absent: checkpoint ordering mechanism not declared.
//   - "UUID v7" absent: event ordering mechanism not declared.
//   - "event_id" absent: UUIDv7 field name not declared.
//   - "MUST NOT" absent near wall-clock: wall-clock prohibition not stated.
//   - "timestamp_wall" absent: prohibited field not named.
//   - "author_date" absent: git timestamp fields not named.
//   - "tip of the run's task branch" absent: branch-tip checkpoint definition missing.
//   - Tags: mechanism missing from RC-011 body window.
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

// rc011FixtureRepoRoot resolves the repository root from this test file's
// source path. The test file lives at:
//
//	internal/specaudit/rc011_dag_uuidv7_ordering_test.go
//
// so the repo root is two directories up.
func rc011FixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("rc011FixtureRepoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// rc011FixtureSpecPath returns the absolute path to specs/reconciliation/spec.md.
func rc011FixtureSpecPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(rc011FixtureRepoRoot(t), "specs", "reconciliation", "spec.md")
}

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

// rc011FixtureRC011Heading matches the RC-011 level-4 requirement heading line.
var rc011FixtureRC011Heading = regexp.MustCompile(`^#### RC-011 —`)

// rc011FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of a requirement body window.
var rc011FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// rc011FixtureTagsMechanism matches a "Tags: mechanism" line.
var rc011FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// rc011FixtureBodyWindow is the maximum number of lines after a heading to
// scan for requirement-body content.
const rc011FixtureBodyWindow = 30

// rc011FixtureBodyLines returns the body lines of the RC-011 section: all lines
// after the matched heading up to (but not including) the next Markdown heading
// or rc011FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the heading is not found.
func rc011FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if rc011FixtureRC011Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "RC-011 heading not found; expected '#### RC-011 —' pattern in specs/reconciliation/spec.md"
	}

	limit := headingIdx + 1 + rc011FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if rc011FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

// rc011FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func rc011FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestRC011DagUUIDv7Ordering is the binding test for hk-63oh.15.
//
// It opens specs/reconciliation/spec.md, locates the RC-011 heading, and
// validates the nine audit checks listed in the file-level comment.
func TestRC011DagUUIDv7Ordering(t *testing.T) {
	t.Parallel()

	specFile := rc011FixtureSpecPath(t)
	lines := rc011FixtureLoadLines(t, specFile)

	// Check (1): RC-011 heading present.
	rc011Body, rc011LineNo, rc011Reason := rc011FixtureBodyLines(lines)
	if rc011Reason != "" {
		t.Fatalf("RC-011 check(1): %s", rc011Reason)
	}
	t.Logf("RC-011 heading found at specs/reconciliation/spec.md line %d; body window = %d lines",
		rc011LineNo, len(rc011Body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	rc011Checks := []check{
		{
			id:     "2",
			label:  "git DAG parentage checkpoint ordering",
			needle: "git DAG parentage",
			detail: "RC-011 body must declare 'git DAG parentage' as the ordering mechanism " +
				"for checkpoints within a run's task branch; the parent-pointer chain is the " +
				"authoritative ordering, not wall-clock time",
		},
		{
			id:     "3",
			label:  "UUID v7 event ordering",
			needle: "UUID v7",
			detail: "RC-011 body must declare 'UUID v7' as the ordering mechanism for events; " +
				"the UUIDv7 event_id embeds a monotonically increasing timestamp component " +
				"that provides partial ordering without relying on wall-clock reads",
		},
		{
			id:     "4",
			label:  "event_id field name",
			needle: "event_id",
			detail: "RC-011 body must name 'event_id' as the UUIDv7 field per event-model.md §4.1; " +
				"this anchors the ordering rule to the concrete field that carries the UUIDv7 value",
		},
		{
			id:     "5",
			label:  "MUST NOT wall-clock prohibition",
			needle: "MUST NOT",
			detail: "RC-011 body must contain 'MUST NOT' to state the prohibition on " +
				"wall-clock-driven classification decisions; a normative prohibition " +
				"(RFC 2119 MUST NOT) is required so implementers cannot treat wall-clock " +
				"ordering as an acceptable fallback",
		},
		{
			id:     "6",
			label:  "timestamp_wall prohibited field",
			needle: "timestamp_wall",
			detail: "RC-011 body must name 'timestamp_wall' as one of the prohibited fields; " +
				"naming the concrete field prevents ambiguity about which timestamps are excluded",
		},
		{
			id:     "7",
			label:  "author_date git commit timestamp fields",
			needle: "author_date",
			detail: "RC-011 body must name 'author_date' (or 'committer_date') as the git " +
				"commit timestamp fields that MUST NOT drive classification; these are the " +
				"wall-clock timestamps embedded in git commits that detectors MUST ignore",
		},
		{
			id:     "8",
			label:  "tip of the run's task branch checkpoint definition",
			needle: "tip of the run",
			detail: "RC-011 body must define the most-recent checkpoint as 'the tip of the " +
				"run's task branch' (the commit with no child in the branch-subgraph); " +
				"this guards against implementations that pick the highest wall-clock commit",
		},
	}

	for _, c := range rc011Checks {
		c := c
		t.Run(fmt.Sprintf("RC011-check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !rc011FixtureBodyContains(rc011Body, c.needle) {
				t.Errorf(
					"RC-011 check(%s) FAILED: %s\n"+
						"  spec:    specs/reconciliation/spec.md line ~%d (RC-011 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, rc011LineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (9): Tags: mechanism in RC-011 body.
	t.Run("RC011-check-9-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range rc011Body {
			if rc011FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"RC-011 check(9) FAILED: Tags: mechanism not found in RC-011 body window\n"+
					"  spec:   specs/reconciliation/spec.md line ~%d (RC-011 body)\n"+
					"  detail: RC-011 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				rc011LineNo,
			)
		}
	})

	t.Logf("hk-63oh.15 audit complete — RC-011 heading at line %d (body %d lines)",
		rc011LineNo, len(rc011Body))
}
