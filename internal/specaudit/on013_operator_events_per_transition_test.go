//go:build specaudit

package specaudit_test

// hk-sx9r.15 binding test — ON-013 operator-control events are emitted per state transition.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-013.
//
// ON-013 states: the daemon MUST emit one typed event per operator-control state
// transition. Paired-phase events (operator_pause_status) are emitted on each of
// the pausing and paused transitions. ON owns *when* events are emitted; EV owns
// *shape* (payload schemas in event-model.md §8.7).
//
// # Audit frame
//
// This test is a spec-corpus sensor. The operator-control state machine and event
// bus implementation are pending; this sensor verifies that ON-013 is correctly
// declared in the spec so that:
//
//  1. ON-013 heading is present in specs/operator-nfr.md.
//  2. "MUST emit one typed event per operator-control state transition" is declared.
//  3. "operator_pause_status" paired-phase event is named.
//  4. "operator_resuming" is named as an event.
//  5. "operator_stopped" is named as an event.
//  6. "operator_upgrading" is named as an event.
//  7. "operator_command_rejected" is named as an event.
//  8. "dispatch_deferred" is named as an event.
//  9. Tags: mechanism is present in the ON-013 body window.
//
// # Failure modes
//
//   - ON-013 heading missing.
//   - "MUST emit one typed event" absent.
//   - "operator_pause_status" absent.
//   - "operator_resuming" absent.
//   - "operator_stopped" absent.
//   - "operator_upgrading" absent.
//   - "operator_command_rejected" absent.
//   - "dispatch_deferred" absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on013Fixture prefix per
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

// on013FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on013FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on013FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on013FixtureON013Heading matches the ON-013 level-4 requirement heading line.
var on013FixtureON013Heading = regexp.MustCompile(`^#### ON-013 —`)

// on013FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on013FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on013FixtureTagsMechanism matches a "Tags: mechanism" line.
var on013FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on013FixtureBodyWindow is the maximum number of lines after the ON-013
// heading to scan for requirement-body content.
const on013FixtureBodyWindow = 30

// on013FixtureLoadLines opens specFile and returns all lines.
func on013FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on013FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on013FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on013FixtureON013BodyLines returns the lines comprising the ON-013 body.
func on013FixtureON013BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on013FixtureON013Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-013 heading not found; expected '#### ON-013 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on013FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on013FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on013FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on013FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON013OperatorEventsPerStateTransition is the binding test for hk-sx9r.15.
func TestON013OperatorEventsPerStateTransition(t *testing.T) {
	t.Parallel()

	specFile := on013FixtureOperatorNFRPath(t)
	lines := on013FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on013FixtureON013BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-013 check(1): %s", reason)
	}
	t.Logf("ON-013 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "must-emit-one-event-per-transition",
			needle: "MUST emit one typed event per operator-control state transition",
			detail: "ON-013 body must declare the obligation 'MUST emit one typed event per " +
				"operator-control state transition'; this is the core normative requirement " +
				"binding the daemon's emission timing to each state-machine edge",
		},
		{
			id:     "3",
			label:  "operator-pause-status-paired-phase",
			needle: "operator_pause_status",
			detail: "ON-013 body must name the 'operator_pause_status' paired-phase event " +
				"(expected phrase 'operator_pause_status'); pause and paused are lifecycle " +
				"phases of a single event per event-model.md §8.9(h) — this event covers " +
				"both the running→pausing and pausing→paused transitions",
		},
		{
			id:     "4",
			label:  "operator-resuming-event-named",
			needle: "operator_resuming",
			detail: "ON-013 body must name 'operator_resuming' as an emitted event " +
				"(expected phrase 'operator_resuming'); this event covers the paused→resuming " +
				"transition and is required for operator-side audit-trail completeness",
		},
		{
			id:     "5",
			label:  "operator-stopped-event-named",
			needle: "operator_stopped",
			detail: "ON-013 body must name 'operator_stopped' as an emitted event " +
				"(expected phrase 'operator_stopped'); this event covers entry into the stopped " +
				"state with mode∈{graceful,immediate} per event-model.md §8.7.8",
		},
		{
			id:     "6",
			label:  "operator-upgrading-event-named",
			needle: "operator_upgrading",
			detail: "ON-013 body must name 'operator_upgrading' as an emitted event " +
				"(expected phrase 'operator_upgrading'); this event covers the paused→upgrading " +
				"transition with the upgrade_version field per event-model.md §8.7.9",
		},
		{
			id:     "7",
			label:  "operator-command-rejected-event-named",
			needle: "operator_command_rejected",
			detail: "ON-013 body must name 'operator_command_rejected' as an emitted event " +
				"(expected phrase 'operator_command_rejected'); this event is emitted when an " +
				"operator command is invalid for the current state-machine state (§8 code 16)",
		},
		{
			id:     "8",
			label:  "dispatch-deferred-event-named",
			needle: "dispatch_deferred",
			detail: "ON-013 body must name 'dispatch_deferred' as an emitted event " +
				"(expected phrase 'dispatch_deferred'); this event is emitted when dispatch is " +
				"blocked by the §4.10.ON-041 machine-ceiling or other deferral condition (§8 code 18)",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on013FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-013 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-013 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (9): Tags: mechanism in ON-013 body.
	t.Run("check-9-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on013FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-013 check(9) FAILED: Tags: mechanism not found in ON-013 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-013 body)\n"+
					"  detail: ON-013 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.15 audit complete — ON-013 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
