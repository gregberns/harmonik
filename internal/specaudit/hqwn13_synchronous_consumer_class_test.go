//go:build specaudit

package specaudit_test

// hk-hqwn.13 binding test — EV-010 synchronous consumer class.
//
// Spec ref: specs/event-model.md §4.2 EV-010.
//
// EV-010 states: a `synchronous` consumer runs on the producer's critical
// path. A synchronous-consumer failure MUST halt the producer's progress on
// the specific run: the producer receives a typed error, does NOT retry
// synchronously, emits a `consumer_failed` event (§8.8.2), and the run enters
// a quarantine state that requires operator escalation. At most ONE synchronous
// consumer per event type is permitted; enforced at subscription-registration
// time. A synchronous consumer MUST NOT emit events that would re-dispatch to
// itself (directly or transitively); the registration path MUST verify
// acyclicity across declared emission surfaces at startup and fail-closed on
// cycles.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The synchronous consumer class
// implementation is pending; this sensor verifies that the EV-010 requirement
// is correctly declared in the spec so that:
//
//  1. EV-010 heading is present in specs/event-model.md — the requirement
//     exists in the normative spec.
//
//  2. Critical-path placement is declared — synchronous consumer runs on the
//     producer's critical path.
//
//  3. Halt-on-failure is declared — failure MUST halt the producer's progress.
//
//  4. Typed-error delivery is declared — producer receives a typed error.
//
//  5. No-synchronous-retry is declared — producer does NOT retry synchronously.
//
//  6. consumer_failed emission is declared — producer emits `consumer_failed`.
//
//  7. Quarantine-on-failure is declared — run enters a quarantine state.
//
//  8. At-most-one constraint is declared — at most ONE synchronous consumer per
//     event type is permitted.
//
//  9. Acyclicity requirement is declared — synchronous consumer MUST NOT emit
//     events that re-dispatch to itself.
//
//  10. Fail-closed-on-cycles is declared — the registration path MUST
//      fail-closed on cycles.
//
//  11. Tags: mechanism is present in the EV-010 body window.
//
// # Failure modes
//
//   - EV-010 heading missing: EV-010 heading not found in specs/event-model.md.
//   - Critical-path placement absent: runs on producer's critical path not declared.
//   - Halt-on-failure absent: MUST halt the producer's progress not declared.
//   - Typed-error delivery absent: typed error not declared.
//   - No-synchronous-retry absent: does NOT retry synchronously not declared.
//   - consumer_failed emission absent: consumer_failed event not declared.
//   - Quarantine-on-failure absent: quarantine state not declared.
//   - At-most-one constraint absent: at most ONE not declared.
//   - Acyclicity requirement absent: MUST NOT emit events that re-dispatch not declared.
//   - Fail-closed-on-cycles absent: fail-closed on cycles not declared.
//   - Tags: mechanism missing from EV-010 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn13Fixture prefix per
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

// hqwn13FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn13_synchronous_consumer_class_test.go
//
// so the repo root is two directories up.
func hqwn13FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn13FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn13FixtureEV010Heading matches the EV-010 level-4 requirement heading line.
var hqwn13FixtureEV010Heading = regexp.MustCompile(`^#### EV-010 —`)

// hqwn13FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the EV-010 requirement body window.
var hqwn13FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn13FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn13FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn13FixtureBodyWindow is the maximum number of lines after the EV-010
// heading to scan for requirement-body content.
const hqwn13FixtureBodyWindow = 30

// hqwn13FixtureLoadLines opens specFile and returns all lines.
func hqwn13FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn13FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn13FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn13FixtureEV010BodyLines returns the lines comprising the EV-010
// requirement body: all lines after the EV-010 heading up to (but not
// including) the next Markdown heading or hqwn13FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the EV-010 heading is not found.
func hqwn13FixtureEV010BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn13FixtureEV010Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-010 heading not found; expected '#### EV-010 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn13FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hqwn13FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn13FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn13FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN13SynchronousConsumerClass is the binding test for hk-hqwn.13.
//
// It opens specs/event-model.md, locates the EV-010 heading, and validates:
//
//	(1)  The EV-010 heading is present (the requirement exists in the spec).
//	(2)  Critical-path placement is declared.
//	(3)  Halt-on-failure is declared.
//	(4)  Typed-error delivery is declared.
//	(5)  No-synchronous-retry is declared.
//	(6)  consumer_failed emission is declared.
//	(7)  Quarantine-on-failure is declared.
//	(8)  At-most-one constraint is declared.
//	(9)  Acyclicity requirement is declared.
//	(10) Fail-closed-on-cycles is declared.
//	(11) Tags: mechanism is present in the body window.
func TestHQWN13SynchronousConsumerClass(t *testing.T) {
	t.Parallel()

	specFile := hqwn13FixtureEventModelPath(t)
	lines := hqwn13FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn13FixtureEV010BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-010 check(1): %s", reason)
	}
	t.Logf("EV-010 heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "critical-path placement",
			needle: "critical path",
			detail: "EV-010 body must declare that a synchronous consumer runs on the producer's " +
				"critical path (expected phrase 'critical path'); this is the defining property " +
				"that separates synchronous from asynchronous and observer classes",
		},
		{
			id:     "3",
			label:  "halt-on-failure",
			needle: "MUST halt the producer's progress",
			detail: "EV-010 body must declare that synchronous-consumer failure MUST halt the " +
				"producer's progress (expected phrase 'MUST halt the producer's progress'); " +
				"this is the core critical-path contract",
		},
		{
			id:     "4",
			label:  "typed-error delivery",
			needle: "typed error",
			detail: "EV-010 body must declare that the producer receives a typed error on " +
				"synchronous-consumer failure (expected phrase 'typed error'); this is the " +
				"mechanism by which the producer learns that the consumer failed",
		},
		{
			id:     "5",
			label:  "no-synchronous-retry",
			needle: "does NOT retry synchronously",
			detail: "EV-010 body must declare that the producer does NOT retry synchronously " +
				"(expected phrase 'does NOT retry synchronously'); this prevents blocking the " +
				"critical path on a retry loop",
		},
		{
			id:     "6",
			label:  "consumer_failed emission",
			needle: "consumer_failed",
			detail: "EV-010 body must declare that the producer emits a `consumer_failed` event " +
				"(expected phrase 'consumer_failed'); this is the bus signal that records the " +
				"failure for operator observability",
		},
		{
			id:     "7",
			label:  "quarantine-on-failure",
			needle: "quarantine",
			detail: "EV-010 body must declare that the run enters a quarantine state on " +
				"synchronous-consumer failure (expected phrase 'quarantine'); this is the " +
				"operator-escalation gate that prevents further progress on a failed run",
		},
		{
			id:     "8",
			label:  "at-most-one constraint",
			needle: "at most ONE",
			detail: "EV-010 body must declare the at-most-one synchronous consumer per event " +
				"type constraint (expected phrase 'at most ONE'); this is enforced at " +
				"subscription-registration time to prevent ambiguous critical-path ownership",
		},
		{
			id:     "9",
			label:  "acyclicity requirement",
			needle: "MUST NOT emit events that",
			detail: "EV-010 body must declare that a synchronous consumer MUST NOT emit events " +
				"that re-dispatch to itself (expected phrase 'MUST NOT emit events that'); " +
				"this is the acyclicity clause preventing reentrant synchronous dispatch",
		},
		{
			id:     "10",
			label:  "fail-closed-on-cycles",
			needle: "fail-closed on cycles",
			detail: "EV-010 body must declare that the registration path MUST fail-closed on " +
				"cycles (expected phrase 'fail-closed on cycles'); this ensures that a " +
				"misconfigured cyclic emission graph is caught at startup, not at runtime",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn13FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-010 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-010 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (11): Tags: mechanism in EV-010 body.
	t.Run("check-11-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn13FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-010 check(11) FAILED: Tags: mechanism not found in EV-010 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-010 body)\n"+
					"  detail: EV-010 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.13 audit complete — EV-010 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
