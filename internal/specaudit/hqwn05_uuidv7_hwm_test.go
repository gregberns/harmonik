package specaudit_test

// hk-hqwn.5 binding test — EV-002c UUIDv7 monotonicity across daemon restart
// via high-water-mark file.
//
// Spec ref: specs/event-model.md §4.1 EV-002c.
//
// EV-002c states: the daemon MUST persist a UUIDv7 high-water-mark (HWM) to
// `.harmonik/event_id_hwm`. The HWM MUST be updated on every `fsync-boundary`
// write. On daemon startup the generator MUST read the HWM and ensure every
// new `event_id` is strictly greater than it. If the wall clock is behind the
// HWM by more than 1 second the daemon MUST emit
// `daemon_degraded{reason=clock_regression}`. If the HWM file is missing or
// unreadable at startup (first-run / `.harmonik/` wiped), the daemon MUST log
// a structured warning and seed from the wall clock.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The HWM generator implementation is
// pending; this sensor verifies that the EV-002c requirement is correctly
// declared in the spec so that:
//
//  1. EV-002c heading is present in specs/event-model.md — the requirement
//     exists in the normative spec.
//
//  2. HWM file path `.harmonik/event_id_hwm` is declared — the persisted path
//     is normatively named so implementors cannot diverge.
//
//  3. HWM updated on fsync-boundary write is declared — the update coupling
//     to the boundary-event fsync domain (no additional fsync cost) is stated.
//
//  4. Startup reads HWM for strict-greater guarantee is declared — the
//     generator MUST read the HWM and ensure new event_ids are strictly
//     greater.
//
//  5. Clock-regression emit daemon_degraded is declared — the 1-second
//     threshold and `daemon_degraded{reason=clock_regression}` emission are
//     specified.
//
//  6. Missing HWM structured warning is declared — the missing-file case
//     produces a structured warning (not a hard failure) is stated.
//
//  7. Tags: mechanism is present in the EV-002c body window.
//
// # Failure modes
//
//   - EV-002c heading missing: EV-002c heading not found in specs/event-model.md.
//   - HWM path absent: `.harmonik/event_id_hwm` not declared.
//   - fsync-boundary coupling absent: HWM-update coupling to boundary fsync not stated.
//   - Startup strict-greater absent: startup read + strict-greater guarantee not stated.
//   - Clock-regression daemon_degraded absent: clock_regression emit not declared.
//   - Missing-HWM structured warning absent: structured-warning for missing file not stated.
//   - Tags: mechanism missing from EV-002c body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn05Fixture prefix per
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

// hqwn05FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn05_uuidv7_hwm_test.go
//
// so the repo root is two directories up.
func hqwn05FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn05FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn05FixtureEV002cHeading matches the EV-002c level-4 requirement heading line.
var hqwn05FixtureEV002cHeading = regexp.MustCompile(`^#### EV-002c —`)

// hqwn05FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the EV-002c requirement body window.
var hqwn05FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn05FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn05FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn05FixtureBodyWindow is the maximum number of lines after the EV-002c
// heading to scan for requirement-body content.
const hqwn05FixtureBodyWindow = 30

// hqwn05FixtureLoadLines opens specFile and returns all lines.
func hqwn05FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn05FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn05FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn05FixtureEV002cBodyLines returns the lines comprising the EV-002c
// requirement body: all lines after the EV-002c heading line up to (but not
// including) the next Markdown heading or hqwn05FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the EV-002c heading is not found.
func hqwn05FixtureEV002cBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn05FixtureEV002cHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-002c heading not found; expected '#### EV-002c —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn05FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hqwn05FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn05FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn05FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN05UUIDv7HighWaterMarkMonotonicity is the binding test for hk-hqwn.5.
//
// It opens specs/event-model.md, locates the EV-002c heading, and validates:
//
//	(a) The EV-002c heading is present (the requirement exists in the spec).
//	(b) HWM file path `.harmonik/event_id_hwm` is declared.
//	(c) HWM updated on fsync-boundary write is declared.
//	(d) Startup reads HWM for strict-greater guarantee.
//	(e) Clock-regression emit daemon_degraded{reason=clock_regression} declared.
//	(f) Missing HWM produces structured warning (not hard failure).
//	(g) Tags: mechanism is present in the body window.
func TestHQWN05UUIDv7HighWaterMarkMonotonicity(t *testing.T) {
	t.Parallel()

	specFile := hqwn05FixtureEventModelPath(t)
	lines := hqwn05FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn05FixtureEV002cBodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-002c check(a): %s", reason)
	}
	t.Logf("EV-002c heading found at specs/event-model.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "b",
			label:  "HWM file path .harmonik/event_id_hwm declared",
			needle: "event_id_hwm",
			detail: "EV-002c body must declare the HWM file path containing 'event_id_hwm' " +
				"(expected phrase 'event_id_hwm'); this is the normative persisted path that " +
				"implementors must use — divergence breaks cross-restart monotonicity",
		},
		{
			id:     "c",
			label:  "HWM updated on fsync-boundary write",
			needle: "fsync-boundary",
			detail: "EV-002c body must state that the HWM is updated on every fsync-boundary " +
				"write (expected phrase 'fsync-boundary'); the piggyback-on-boundary-fsync " +
				"constraint is the normative no-additional-cost coupling",
		},
		{
			id:     "d",
			label:  "startup reads HWM strict-greater guarantee",
			needle: "strictly greater",
			detail: "EV-002c body must state that on startup the generator MUST ensure every " +
				"new event_id is strictly greater than the HWM (expected phrase 'strictly greater'); " +
				"this is the load-bearing cross-restart monotonicity contract",
		},
		{
			id:     "e",
			label:  "clock-regression daemon_degraded emission",
			needle: "clock_regression",
			detail: "EV-002c body must declare the clock_regression variant of daemon_degraded " +
				"(expected phrase 'clock_regression'); this is the observable signal when the " +
				"wall clock has regressed behind the HWM by more than 1 second",
		},
		{
			id:     "f",
			label:  "missing HWM produces structured warning",
			needle: "structured warning",
			detail: "EV-002c body must state that a missing or unreadable HWM file produces a " +
				"structured warning (expected phrase 'structured warning'); this distinguishes " +
				"first-run from a hard error and seeds from wall clock",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn05FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-002c check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-002c body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (g): Tags: mechanism in EV-002c body.
	t.Run("check-g-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn05FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-002c check(g) FAILED: Tags: mechanism not found in EV-002c body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-002c body)\n"+
					"  detail: EV-002c carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.5 audit complete — EV-002c heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
