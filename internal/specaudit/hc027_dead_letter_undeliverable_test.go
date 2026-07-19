//go:build specaudit

package specaudit_test

// hk-8i31.34 binding test — HC-027 dead-letter behavior for undeliverable events.
//
// Spec ref: specs/handler-contract.md §4.6 HC-027.
//
// HC-027 states: events that cannot be delivered by the watcher to the in-process bus
// (bus full, subscriber panic) MUST be routed to the dead-letter destination declared by
// event-model.md §4.3. The watcher MUST NOT drop events silently.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The watcher implementation is pending; this sensor
// verifies that HC-027 is correctly declared in the spec so that:
//
//  1. HC-027 heading is present in specs/handler-contract.md.
//  2. "cannot be delivered" condition is declared (bus full, subscriber panic).
//  3. "MUST be routed to the dead-letter destination" is declared.
//  4. "event-model.md §4.3" is cited as the dead-letter destination authority.
//  5. "MUST NOT drop events silently" is declared.
//  6. Tags: mechanism is present in the HC-027 body window.
//
// # Failure modes
//
//   - HC-027 heading missing.
//   - cannot be delivered condition absent.
//   - MUST be routed to dead-letter absent.
//   - event-model.md §4.3 citation absent.
//   - MUST NOT drop absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc027Fixture prefix per
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

// hc027FixtureHandlerContractPath returns the absolute path to specs/handler-contract.md.
func hc027FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc027FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc027FixtureHC027Heading matches the HC-027 level-4 requirement heading line.
var hc027FixtureHC027Heading = regexp.MustCompile(`^#### HC-027 —`)

// hc027FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hc027FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc027FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc027FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc027FixtureBodyWindow is the maximum number of lines after the HC-027
// heading to scan for requirement-body content.
const hc027FixtureBodyWindow = 30

// hc027FixtureLoadLines opens specFile and returns all lines.
func hc027FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc027FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc027FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc027FixtureHC027BodyLines returns the lines comprising the HC-027 body.
func hc027FixtureHC027BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc027FixtureHC027Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-027 heading not found; expected '#### HC-027 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc027FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc027FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc027FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hc027FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC027DeadLetterUndeliverableEvents is the binding test for hk-8i31.34.
func TestHC027DeadLetterUndeliverableEvents(t *testing.T) {
	t.Parallel()

	specFile := hc027FixtureHandlerContractPath(t)
	lines := hc027FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc027FixtureHC027BodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-027 check(1): %s", reason)
	}
	t.Logf("HC-027 heading found at specs/handler-contract.md line %d; body window = %d lines",
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
			label:  "cannot-be-delivered-condition",
			needle: "cannot be delivered",
			detail: "HC-027 body must declare 'cannot be delivered' as the condition triggering dead-letter " +
				"(expected phrase 'cannot be delivered'); the two specific cases are bus full " +
				"(publish channel buffer exhausted) and subscriber panic (watcher isolates per-subscriber " +
				"per HC-011's recover barrier)",
		},
		{
			id:     "3",
			label:  "must-be-routed-to-dead-letter",
			needle: "MUST be routed to the dead-letter destination",
			detail: "HC-027 body must declare 'MUST be routed to the dead-letter destination' " +
				"(expected phrase 'MUST be routed to the dead-letter destination'); routing to dead-letter " +
				"preserves the event for operator inspection and recovery — the dead-letter queue " +
				"is the defined boundary for undeliverable events per EV §4.3",
		},
		{
			id:     "4",
			label:  "event-model-section-4-3-citation",
			needle: "event-model.md §4.3",
			detail: "HC-027 body must cite 'event-model.md §4.3' as the dead-letter destination authority " +
				"(expected phrase 'event-model.md §4.3'); EV §4.3 defines the dead-letter queue " +
				"(DLQ) and its consumer taxonomy — HC-027 delegates the DLQ definition to EV " +
				"rather than redefining it",
		},
		{
			id:     "5",
			label:  "must-not-drop-events-silently",
			needle: "MUST NOT drop events silently",
			detail: "HC-027 body must declare 'MUST NOT drop events silently' " +
				"(expected phrase 'MUST NOT drop events silently'); silent dropping would make " +
				"bus-full conditions invisible to operators — events would disappear without any " +
				"signal that the consumer is not keeping up",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc027FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-027 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-027 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in HC-027 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc027FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-027 check(6) FAILED: Tags: mechanism not found in HC-027 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-027 body)\n"+
					"  detail: HC-027 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.34 audit complete — HC-027 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
