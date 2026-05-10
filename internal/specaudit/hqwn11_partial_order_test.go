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
// millisecond precision. Tooling requiring stricter cross-process ordering
// MUST insert explicit causal references via `trace_context.parent_event_id`
// (§6.1) or a payload-specific field such as `triggering_event_id` on
// `hook_fired`.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The partial-order runtime enforcement is
// pending; this sensor verifies that the EV-008 requirement is correctly
// declared in the spec so that:
//
//  1. EV-008 heading is present in specs/event-model.md — the requirement
//     exists in the normative spec.
//
//  2. UUIDv7 ms-resolution total ordering across processes is declared — the
//     coarse ordering guarantee is normatively stated.
//
//  3. EV-002a intra-process sub-millisecond monotonicity is cited — the
//     reference to EV-002a as the strict intra-process strengthening is stated.
//
//  4. timestamp_mono_nsec refines within a process is declared — the
//     intra-process refinement field is normatively named.
//
//  5. No total-ordering guarantee finer than UUIDv7 ms across distinct
//     processes is declared — the explicit limitation on cross-process ordering
//     precision is stated.
//
//  6. Causal reference via trace_context.parent_event_id is declared — the
//     §6.1 causal-link mechanism for tooling that needs stricter ordering is
//     normatively cited.
//
//  7. triggering_event_id on hook_fired is declared — the payload-specific
//     causal-link alternative is normatively named.
//
//  8. Tags: mechanism is present in the EV-008 body window.
//
// # Failure modes
//
//   - EV-008 heading missing: EV-008 heading not found in specs/event-model.md.
//   - UUIDv7 ms-resolution absent: cross-process ms-resolution ordering not stated.
//   - EV-002a citation absent: intra-process sub-millisecond strengthening not cited.
//   - timestamp_mono_nsec absent: intra-process refinement field not named.
//   - No total-ordering guarantee absent: cross-process ordering limitation not stated.
//   - parent_event_id absent: trace_context causal reference not declared.
//   - triggering_event_id absent: hook_fired causal-link field not named.
//   - Tags: mechanism missing from EV-008 body window.
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

// hqwn11FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn11_partial_order_test.go
//
// so the repo root is two directories up.
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
// Used to detect the end of the EV-008 requirement body window.
var hqwn11FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn11FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn11FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn11FixtureBodyWindow is the maximum number of lines after the EV-008
// heading to scan for requirement-body content.
const hqwn11FixtureBodyWindow = 30

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

// hqwn11FixtureEV008BodyLines returns the lines comprising the EV-008
// requirement body: all lines after the EV-008 heading line up to (but not
// including) the next Markdown heading or hqwn11FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the EV-008 heading is not found.
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
		// Stop at the next Markdown heading (the next requirement).
		if hqwn11FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn11FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
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
//
// It opens specs/event-model.md, locates the EV-008 heading, and validates:
//
//	(a) The EV-008 heading is present (the requirement exists in the spec).
//	(b) UUIDv7 ms-resolution total ordering across processes is declared.
//	(c) EV-002a intra-process sub-millisecond monotonicity is cited.
//	(d) timestamp_mono_nsec refines within a process is declared.
//	(e) No total-ordering guarantee finer than UUIDv7 ms across distinct processes.
//	(f) Causal reference via trace_context.parent_event_id declared.
//	(g) triggering_event_id on hook_fired declared.
//	(h) Tags: mechanism is present in the body window.
func TestHQWN11PartialOrderContract(t *testing.T) {
	t.Parallel()

	specFile := hqwn11FixtureEventModelPath(t)
	lines := hqwn11FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn11FixtureEV008BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-008 check(a): %s", reason)
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
			id:    "b",
			label: "UUIDv7 ms-resolution total ordering across processes declared",
			// The spec uses "ms-resolution total ordering" as the precise phrase.
			needle: "ms-resolution total ordering",
			detail: "EV-008 body must declare that UUIDv7 supplies ms-resolution total ordering " +
				"across processes (expected phrase 'ms-resolution total ordering'); this is the " +
				"baseline coarse cross-process ordering guarantee that all consumers rely on",
		},
		{
			id:     "c",
			label:  "EV-002a intra-process sub-millisecond monotonicity cited",
			needle: "EV-002a",
			detail: "EV-008 body must cite EV-002a as the requirement that extends UUIDv7 ordering " +
				"to strict intra-process sub-millisecond monotonicity (expected phrase 'EV-002a'); " +
				"the cross-reference is load-bearing: EV-002a is the intra-process strengthening contract",
		},
		{
			id:     "d",
			label:  "timestamp_mono_nsec refines within a process declared",
			needle: "timestamp_mono_nsec",
			detail: "EV-008 body must name timestamp_mono_nsec as the intra-process refinement " +
				"field (expected phrase 'timestamp_mono_nsec'); this is the sub-nanosecond " +
				"within-process ordering instrument and its scope must be explicitly stated",
		},
		{
			id:     "e",
			label:  "no total-ordering guarantee finer than UUIDv7 ms across distinct processes",
			needle: "distinct",
			detail: "EV-008 body must explicitly state that there is no total-ordering guarantee " +
				"across distinct processes finer than UUIDv7's millisecond precision (expected " +
				"phrase 'distinct'); this limitation is the load-bearing boundary condition " +
				"that prevents consumers from assuming cross-process sub-ms ordering",
		},
		{
			id:     "f",
			label:  "causal reference via trace_context.parent_event_id declared",
			needle: "parent_event_id",
			detail: "EV-008 body must declare that tooling needing stricter cross-process ordering " +
				"MUST use trace_context.parent_event_id (expected phrase 'parent_event_id'); " +
				"this is the normative causal-link mechanism specified in §6.1",
		},
		{
			id:     "g",
			label:  "triggering_event_id on hook_fired declared",
			needle: "triggering_event_id",
			detail: "EV-008 body must name triggering_event_id on hook_fired as a payload-specific " +
				"causal-link alternative (expected phrase 'triggering_event_id'); this documents " +
				"the type-specific causal field that coexists deliberately with parent_event_id",
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

	// Check (h): Tags: mechanism in EV-008 body.
	t.Run("check-h-tags-mechanism", func(t *testing.T) {
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
				"EV-008 check(h) FAILED: Tags: mechanism not found in EV-008 body window\n"+
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
