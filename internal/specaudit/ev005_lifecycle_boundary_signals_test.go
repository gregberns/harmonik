package specaudit_test

// hk-hqwn.8 binding test — EV-005 events are lifecycle-boundary signals, not agent internals.
//
// Spec ref: specs/event-model.md §4.1 EV-005.
//
// EV-005 states: events emitted to the bus and JSONL MUST be lifecycle-boundary signals.
// Agent-internal detail (tool calls, thinking traces, per-token output) MUST NOT be emitted
// as events — it lives in the agent's session log per workspace-model.md §4.7. The per-chunk
// types agent_output_chunk and budget_accrual ARE lifecycle-boundary signals routed to the
// bus per §8.9; they are NOT the mechanism by which the orchestrator reconstructs
// agent-internal state.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The event bus implementation is pending; this sensor
// verifies that EV-005 is correctly declared in the spec so that:
//
//  1. EV-005 heading is present in specs/event-model.md.
//  2. "MUST be lifecycle-boundary signals" is declared.
//  3. "MUST NOT be emitted as events" for agent-internal detail is declared.
//  4. "agent session log" is named as the destination for agent-internal detail.
//  5. "agent_output_chunk" is named as a lifecycle-boundary signal exception.
//  6. "budget_accrual" is named as a lifecycle-boundary signal exception.
//  7. Tags: mechanism is present in the EV-005 body window.
//
// # Failure modes
//
//   - EV-005 heading missing.
//   - MUST be lifecycle-boundary signals absent.
//   - MUST NOT be emitted as events absent.
//   - agent session log absent.
//   - agent_output_chunk absent.
//   - budget_accrual absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the ev005Fixture prefix per
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

// ev005FixtureEventModelPath returns the absolute path to specs/event-model.md.
func ev005FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ev005FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// ev005FixtureEV005Heading matches the EV-005 level-4 requirement heading line.
var ev005FixtureEV005Heading = regexp.MustCompile(`^#### EV-005 —`)

// ev005FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var ev005FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// ev005FixtureTagsMechanism matches a "Tags: mechanism" line.
var ev005FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// ev005FixtureBodyWindow is the maximum number of lines after the EV-005
// heading to scan for requirement-body content.
const ev005FixtureBodyWindow = 30

// ev005FixtureLoadLines opens specFile and returns all lines.
func ev005FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ev005FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ev005FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// ev005FixtureEV005BodyLines returns the lines comprising the EV-005 body.
func ev005FixtureEV005BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if ev005FixtureEV005Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-005 heading not found; expected '#### EV-005 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + ev005FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if ev005FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// ev005FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func ev005FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestEV005LifecycleBoundarySignalsNotAgentInternals is the binding test for hk-hqwn.8.
func TestEV005LifecycleBoundarySignalsNotAgentInternals(t *testing.T) {
	t.Parallel()

	specFile := ev005FixtureEventModelPath(t)
	lines := ev005FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := ev005FixtureEV005BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-005 check(1): %s", reason)
	}
	t.Logf("EV-005 heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "must-be-lifecycle-boundary-signals",
			needle: "MUST be lifecycle-boundary signals",
			detail: "EV-005 body must declare 'MUST be lifecycle-boundary signals' " +
				"(expected phrase 'MUST be lifecycle-boundary signals'); this is the root normative " +
				"obligation that restricts the event surface to transitions, not internals — " +
				"the bus is not a general-purpose logging channel",
		},
		{
			id:     "3",
			label:  "must-not-be-emitted-as-events",
			needle: "MUST NOT be emitted as events",
			detail: "EV-005 body must declare 'MUST NOT be emitted as events' for agent-internal " +
				"detail (expected phrase 'MUST NOT be emitted as events'); tool calls, thinking " +
				"traces, and per-token output are agent-internal and MUST NOT appear on the bus or JSONL",
		},
		{
			id:     "4",
			label:  "session-log-destination-for-internals",
			needle: "session log",
			detail: "EV-005 body must name 'session log' as the destination for agent-internal detail " +
				"(expected phrase 'session log'); workspace-model.md §4.7 owns the agent session log " +
				"where tool calls and thinking traces live — this is the normative redirect",
		},
		{
			id:     "5",
			label:  "agent-output-chunk-named-exception",
			needle: "agent_output_chunk",
			detail: "EV-005 body must name 'agent_output_chunk' as a lifecycle-boundary signal exception " +
				"(expected phrase 'agent_output_chunk'); this per-chunk type IS a lifecycle-boundary " +
				"signal routed to the bus per §8.9 — it is explicitly carved out from the " +
				"MUST-NOT-emit rule for agent-internal detail",
		},
		{
			id:     "6",
			label:  "budget-accrual-named-exception",
			needle: "budget_accrual",
			detail: "EV-005 body must name 'budget_accrual' as a lifecycle-boundary signal exception " +
				"(expected phrase 'budget_accrual'); budget_accrual tracks resource consumption at " +
				"lifecycle-boundary granularity and IS routed to the bus per §8.9, not treated as " +
				"agent-internal detail",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !ev005FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-005 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-005 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in EV-005 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if ev005FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-005 check(7) FAILED: Tags: mechanism not found in EV-005 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-005 body)\n"+
					"  detail: EV-005 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.8 audit complete — EV-005 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
