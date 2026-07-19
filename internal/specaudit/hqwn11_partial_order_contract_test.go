//go:build specaudit

package specaudit_test

// hk-hqwn.11 binding test — EV-008 partial-order contract.
//
// Spec ref: specs/event-model.md §4.2 EV-008.
//
// EV-008 states: the event stream MUST satisfy a partial-ordering contract:
// UUIDv7 supplies ms-resolution total ordering across processes; EV-002a
// extends that to strict intra-process monotonicity at sub-millisecond
// resolution; `timestamp_mono_nsec` refines within a process; there is no
// total-ordering guarantee across distinct processes finer than UUIDv7's
// millisecond precision. Tooling that requires stricter cross-process ordering
// MUST insert explicit causal references via `trace_context.parent_event_id`
// (§6.1) or a payload-specific field such as `triggering_event_id` on
// `hook_fired`.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The ordering invariant is a coding-discipline
// and runtime property; this sensor verifies that EV-008 is correctly declared
// in the spec so that:
//
//  1. EV-008 heading is present in specs/event-model.md.
//  2. UUIDv7 ms-resolution total ordering across processes is declared.
//  3. EV-002a intra-process strict monotonicity is declared.
//  4. timestamp_mono_nsec process-scoped refinement is declared.
//  5. No total-ordering guarantee across distinct processes beyond ms precision.
//  6. Explicit causal references via trace_context.parent_event_id is the escape.
//  7. Tags: mechanism is present in the EV-008 body window.
//
// # Failure modes
//
//   - EV-008 heading missing.
//   - UUIDv7 cross-process ordering absent.
//   - EV-002a intra-process monotonicity absent.
//   - timestamp_mono_nsec absent.
//   - No total-ordering guarantee statement absent.
//   - trace_context.parent_event_id causal reference absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn11Fixture prefix per
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

// hqwn11FixtureEventModelPath returns the absolute path to specs/event-model.md.
func hqwn11FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn11FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn11FixtureEV008Heading matches the EV-008 level-4 requirement heading line.
var hqwn11FixtureEV008Heading = regexp.MustCompile(`^#### EV-008 —`)

// hqwn11FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hqwn11FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn11FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn11FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn11FixtureBodyWindow is the maximum number of lines after the EV-008
// heading to scan for requirement-body content.
const hqwn11FixtureBodyWindow = 15

// hqwn11FixtureLoadLines opens specFile and returns all lines.
func hqwn11FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn11FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn11FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn11FixtureEV008BodyLines returns the lines comprising the EV-008 body.
func hqwn11FixtureEV008BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn11FixtureEV008Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-008 heading not found; expected '#### EV-008 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn11FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hqwn11FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn11FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hqwn11FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN11PartialOrderContract is the binding test for hk-hqwn.11.
func TestHQWN11PartialOrderContract(t *testing.T) {
	t.Parallel()

	specFile := hqwn11FixtureEventModelPath(t)
	lines := hqwn11FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn11FixtureEV008BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-008 check(1): %s", reason)
	}
	t.Logf("EV-008 heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "uuidv7-cross-process-total-ordering",
			needle: "UUIDv7",
			detail: "EV-008 body must declare UUIDv7 as the cross-process total ordering mechanism " +
				"(expected phrase 'UUIDv7'); this is the primary ordering primitive that provides " +
				"millisecond-resolution total ordering across processes",
		},
		{
			id:     "3",
			label:  "ev002a-intra-process-monotonicity",
			needle: "EV-002a",
			detail: "EV-008 body must cite EV-002a for intra-process strict monotonicity " +
				"(expected phrase 'EV-002a'); EV-002a extends UUIDv7 ordering to sub-millisecond " +
				"precision within a single process",
		},
		{
			id:     "4",
			label:  "timestamp-mono-nsec-process-refinement",
			needle: "timestamp_mono_nsec",
			detail: "EV-008 body must declare timestamp_mono_nsec as the process-scoped refinement " +
				"(expected phrase 'timestamp_mono_nsec'); this field narrows ordering within a process " +
				"beyond UUIDv7's millisecond precision",
		},
		{
			id:     "5",
			label:  "no-total-ordering-across-processes",
			needle: "no total-ordering guarantee",
			detail: "EV-008 body must declare the absence of total ordering across distinct processes " +
				"finer than millisecond precision (expected phrase 'no total-ordering guarantee'); " +
				"this sets the correct expectation for cross-process consumers",
		},
		{
			id:     "6",
			label:  "trace-context-parent-event-id-causal-escape",
			needle: "trace_context.parent_event_id",
			detail: "EV-008 body must declare trace_context.parent_event_id as the escape for " +
				"tooling needing stricter cross-process ordering (expected phrase " +
				"'trace_context.parent_event_id'); this is the explicit causal-reference mechanism " +
				"provided by §6.1",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn11FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-008 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-008 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in EV-008 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn11FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-008 check(7) FAILED: Tags: mechanism not found in EV-008 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-008 body)\n"+
					"  detail: EV-008 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.11 audit complete — EV-008 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
