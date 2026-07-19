//go:build specaudit

package specaudit_test

// hk-hqwn.15 binding test — EV-011a non-blocking producer back-pressure.
//
// Spec ref: specs/event-model.md §4.3 EV-011a.
//
// EV-011a states: the bus MUST NOT block the producer on async-consumer delivery.
// When a per-consumer queue is full, the bus MUST shed the event according to its
// durability class: lossy-tail-ok sheds first, then ordinary; fsync-boundary events
// MUST NOT be shed and MUST spill to a secondary spill file. Every shed event MUST
// cause emission of a bus_overflow event (§8.8.4). The bus MUST reserve a capacity-1
// dedicated slot in the observer queue for bus_overflow. Per-consumer spill files MUST
// be pre-created at subscription-registration time; failure to create MUST fail startup.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The event bus implementation is pending; this
// sensor verifies that EV-011a is correctly declared in the spec so that:
//
//  1. EV-011a heading is present in specs/event-model.md.
//  2. "MUST NOT block the producer" is declared.
//  3. "lossy-tail-ok" sheds first is declared.
//  4. "fsync-boundary" events MUST NOT be shed is declared.
//  5. Spill file path pattern is declared.
//  6. "bus_overflow" event emission is required per shed.
//  7. Capacity-1 reservation in observer queue is declared.
//  8. Tags: mechanism is present in the EV-011a body window.
//
// # Failure modes
//
//   - EV-011a heading missing.
//   - MUST NOT block producer absent.
//   - lossy-tail-ok shed-first absent.
//   - fsync-boundary MUST NOT be shed absent.
//   - spill file path absent.
//   - bus_overflow per-shed obligation absent.
//   - capacity-1 reservation absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the ev011aFixture prefix per
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

// ev011aFixtureEventModelPath returns the absolute path to specs/event-model.md.
func ev011aFixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ev011aFixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// ev011aFixtureEV011aHeading matches the EV-011a level-4 requirement heading line.
var ev011aFixtureEV011aHeading = regexp.MustCompile(`^#### EV-011a —`)

// ev011aFixtureAnySectionHeading matches any Markdown heading (level 1–4).
var ev011aFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// ev011aFixtureTagsMechanism matches a "Tags: mechanism" line.
var ev011aFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// ev011aFixtureBodyWindow is the maximum number of lines after the EV-011a
// heading to scan for requirement-body content.
const ev011aFixtureBodyWindow = 30

// ev011aFixtureLoadLines opens specFile and returns all lines.
func ev011aFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ev011aFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ev011aFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// ev011aFixtureEV011aBodyLines returns the lines comprising the EV-011a body.
func ev011aFixtureEV011aBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if ev011aFixtureEV011aHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-011a heading not found; expected '#### EV-011a —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + ev011aFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if ev011aFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// ev011aFixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func ev011aFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestEV011aNonBlockingProducerBackPressure is the binding test for hk-hqwn.15.
func TestEV011aNonBlockingProducerBackPressure(t *testing.T) {
	t.Parallel()

	specFile := ev011aFixtureEventModelPath(t)
	lines := ev011aFixtureLoadLines(t, specFile)

	body, headingLineNo, reason := ev011aFixtureEV011aBodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-011a check(1): %s", reason)
	}
	t.Logf("EV-011a heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "must-not-block-the-producer",
			needle: "MUST NOT block the producer",
			detail: "EV-011a body must declare 'MUST NOT block the producer' " +
				"(expected phrase 'MUST NOT block the producer'); this is the root normative " +
				"obligation that prevents a slow consumer from stalling the daemon's critical path — " +
				"the orchestrator and agent runners MUST not be blocked by event consumers",
		},
		{
			id:     "3",
			label:  "lossy-tail-ok-sheds-first",
			needle: "lossy-tail-ok",
			detail: "EV-011a body must name 'lossy-tail-ok' as the first-shed durability class " +
				"(expected phrase 'lossy-tail-ok'); the shed ordering is durability-class-dependent: " +
				"lossy-tail-ok sheds before ordinary, preserving higher-durability events as long " +
				"as possible when queues fill",
		},
		{
			id:     "4",
			label:  "fsync-boundary-must-not-be-shed",
			needle: "fsync-boundary",
			detail: "EV-011a body must declare that 'fsync-boundary' events MUST NOT be shed " +
				"(expected phrase 'fsync-boundary'); fsync-boundary events carry the strongest " +
				"durability guarantee and MUST spill to a secondary spill file rather than be " +
				"discarded — shedding them would violate the durability contract",
		},
		{
			id:     "5",
			label:  "spill-file-path-pattern",
			needle: "spill-<consumer>",
			detail: "EV-011a body must declare the spill file path pattern 'spill-<consumer>' " +
				"(expected phrase 'spill-<consumer>'); the per-consumer spill file at " +
				".harmonik/events/spill-<consumer>.jsonl is the overflow destination for " +
				"fsync-boundary events that cannot fit in the consumer queue",
		},
		{
			id:     "6",
			label:  "bus-overflow-per-shed-required",
			needle: "bus_overflow",
			detail: "EV-011a body must declare that every shed event MUST cause emission of 'bus_overflow' " +
				"(expected phrase 'bus_overflow'); the bus_overflow event (§8.8.4) is the operator " +
				"signal that the bus shed an event — without it, operators cannot detect queue-pressure " +
				"conditions from the event stream",
		},
		{
			id:     "7",
			label:  "capacity-1-reservation-in-observer-queue",
			needle: "capacity-1",
			detail: "EV-011a body must declare a 'capacity-1' dedicated slot reservation for bus_overflow " +
				"(expected phrase 'capacity-1'); reserving one slot ensures at least one bus_overflow " +
				"signal per actual shed event without requiring a recursive fill check — this breaks " +
				"the deadlock cycle where bus_overflow itself could be shed",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !ev011aFixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-011a check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-011a body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (8): Tags: mechanism in EV-011a body.
	t.Run("check-8-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if ev011aFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-011a check(8) FAILED: Tags: mechanism not found in EV-011a body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-011a body)\n"+
					"  detail: EV-011a carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.15 audit complete — EV-011a heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
