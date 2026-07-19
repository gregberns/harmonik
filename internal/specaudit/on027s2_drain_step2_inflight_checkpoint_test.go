//go:build specaudit

package specaudit_test

// hk-sx9r.34 binding test — ON-027 step 2: in-flight runs reach next checkpoint, then suspend.
//
// Spec ref: specs/operator-nfr.md §4.7 ON-027.
//
// ON-027 step 2 states: every run for which in_flight(run) holds (per §3) proceeds to
// its next durable checkpoint per [execution-model.md §4.5], then suspends. Step 2 is
// bounded by timeout.step_2 per ON-029; on timeout the escalation path fires. Step 2
// completion signal: in_flight(run) evaluates to false for every run via dispatch-status
// JSON-RPC per [process-lifecycle.md §4.1 PL-003a]. Per ON-027a, completion is durably
// marked before step 3 begins.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The drain sequencer implementation is pending;
// this sensor verifies that ON-027 step 2 is correctly declared in the spec so that:
//
//  1. ON-027 heading is present in specs/operator-nfr.md.
//  2. "in_flight(run)" predicate is named as the gating condition for step 2.
//  3. "next checkpoint, then suspends" describes the per-run action.
//  4. "execution-model.md §4.5" is cited as the checkpoint-suspension mechanism.
//  5. "timeout.step_2" bounds the step duration per ON-029.
//  6. "pausing → paused" transition requires ALL eight steps to complete.
//  7. Tags: mechanism is present in the ON-027 body window.
//
// # Failure modes
//
//   - ON-027 heading missing.
//   - in_flight(run) predicate absent.
//   - next checkpoint then suspends absent.
//   - execution-model.md §4.5 citation absent.
//   - timeout.step_2 bound absent.
//   - pausing→paused transition precondition absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on027s2Fixture prefix per
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

// on027s2FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on027s2FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on027s2FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on027s2FixtureON027Heading matches the ON-027 level-4 requirement heading line.
var on027s2FixtureON027Heading = regexp.MustCompile(`^#### ON-027 —`)

// on027s2FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on027s2FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on027s2FixtureTagsMechanism matches a "Tags: mechanism" line.
var on027s2FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on027s2FixtureBodyWindow is the maximum number of lines after the ON-027
// heading to scan for requirement-body content.
const on027s2FixtureBodyWindow = 30

// on027s2FixtureLoadLines opens specFile and returns all lines.
func on027s2FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on027s2FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on027s2FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on027s2FixtureON027BodyLines returns the lines comprising the ON-027 body.
func on027s2FixtureON027BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on027s2FixtureON027Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-027 heading not found; expected '#### ON-027 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on027s2FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on027s2FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on027s2FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on027s2FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON027Step2InFlightRunsCheckpointThenSuspend is the binding test for hk-sx9r.34.
func TestON027Step2InFlightRunsCheckpointThenSuspend(t *testing.T) {
	t.Parallel()

	specFile := on027s2FixtureOperatorNFRPath(t)
	lines := on027s2FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on027s2FixtureON027BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-027 check(1): %s", reason)
	}
	t.Logf("ON-027 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "in-flight-run-predicate-named",
			needle: "in_flight(run)",
			detail: "ON-027 body must name the 'in_flight(run)' predicate as the step-2 gating " +
				"condition (expected phrase 'in_flight(run)'); the predicate is defined in §3 " +
				"and evaluated via dispatch-status JSON-RPC — only runs satisfying the predicate " +
				"must complete their checkpoint before step 2 finishes",
		},
		{
			id:     "3",
			label:  "next-checkpoint-then-suspends",
			needle: "next checkpoint, then suspends",
			detail: "ON-027 body must describe step 2 as proceeding to the 'next checkpoint, then " +
				"suspends' (expected phrase 'next checkpoint, then suspends'); this is the per-run " +
				"action: each in-flight run is allowed to complete its current node's work and " +
				"write its durable checkpoint before it suspends, preventing partial-node loss",
		},
		{
			id:     "4",
			label:  "execution-model-section-4-5-cited",
			needle: "execution-model.md §4.5",
			detail: "ON-027 body must cite 'execution-model.md §4.5' as the checkpoint-suspension " +
				"mechanism (expected phrase 'execution-model.md §4.5'); EM §4.5 owns the durable " +
				"checkpoint contract that step 2 depends on for safe suspension",
		},
		{
			id:     "5",
			label:  "drain-timeout-bound-named",
			needle: "drain timeout",
			detail: "ON-027 body must name 'drain timeout' as the per-step bounding mechanism " +
				"(expected phrase 'drain timeout'); operator-configurable per-step timeouts are " +
				"declared in ON-029 (timeout.step_2 etc.); ON-027 references the drain timeout " +
				"in the step-3 and step-7 clauses to bound in-flight wait and escalation",
		},
		{
			id:     "6",
			label:  "pausing-to-paused-requires-all-steps",
			needle: "pausing → paused",
			detail: "ON-027 body must declare that the 'pausing → paused' transition requires " +
				"ALL steps to complete (expected phrase 'pausing → paused'); this binds the " +
				"drain sequence to the state-machine: paused is unreachable until every drain " +
				"step finishes (or times out per ON-029)",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on027s2FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-027 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-027 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in ON-027 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on027s2FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-027 check(7) FAILED: Tags: mechanism not found in ON-027 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-027 body)\n"+
					"  detail: ON-027 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.34 audit complete — ON-027 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
