//go:build specaudit

package specaudit_test

// hk-8mup.19 binding test — PL-010 degraded state on Cat 0 infrastructure failure
// (pre-ready only).
//
// Spec ref: specs/process-lifecycle.md §4.3 PL-010.
//
// PL-010 states: when the Cat 0 pre-check (PL-005 step 4) fails, the daemon MUST
// transition to `degraded` status and remain there until all prerequisites clear. In
// `degraded`, the daemon MUST NOT classify in-flight runs, MUST NOT dispatch runs, and
// MUST NOT transition to `ready`. The daemon MUST emit `infrastructure_unavailable` and
// SHOULD also emit `daemon_degraded`. The daemon MUST periodically retry the pre-check
// at a configurable cadence (default 10s per OQ-PL-002).
//
// # Audit frame
//
// This test is a spec-corpus sensor. The daemon implementation is pending; this sensor
// verifies that PL-010 is correctly declared in the spec so that:
//
//  1. PL-010 heading is present in specs/process-lifecycle.md.
//  2. "MUST transition to `degraded`" is declared for Cat 0 failure.
//  3. "MUST NOT classify in-flight runs" is declared.
//  4. "infrastructure_unavailable" event is named.
//  5. "periodically retry" is declared for the pre-check.
//  6. Tags: mechanism is present in the PL-010 body window.
//
// # Failure modes
//
//   - PL-010 heading missing.
//   - MUST transition to degraded absent.
//   - MUST NOT classify in-flight runs absent.
//   - infrastructure_unavailable absent.
//   - periodically retry absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the pl010Fixture prefix per
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

// pl010FixtureProcessLifecyclePath returns the absolute path to specs/process-lifecycle.md.
func pl010FixtureProcessLifecyclePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("pl010FixtureProcessLifecyclePath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "process-lifecycle.md")
}

// pl010FixtureHeading matches the PL-010 level-4 requirement heading line.
var pl010FixtureHeading = regexp.MustCompile(`^#### PL-010 —`)

// pl010FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var pl010FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// pl010FixtureTagsMechanism matches a "Tags: mechanism" line.
var pl010FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// pl010FixtureBodyWindow is the maximum number of lines to scan after the heading.
const pl010FixtureBodyWindow = 30

// pl010FixtureLoadLines opens specFile and returns all lines.
func pl010FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("pl010FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("pl010FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// pl010FixtureBodyLines returns the lines comprising the PL-010 body.
func pl010FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if pl010FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "PL-010 heading not found; expected '#### PL-010 —' in specs/process-lifecycle.md"
	}

	limit := headingIdx + 1 + pl010FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if pl010FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// pl010FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func pl010FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestPL010DegradedStateCat0Failure is the binding test for hk-8mup.19.
func TestPL010DegradedStateCat0Failure(t *testing.T) {
	t.Parallel()

	specFile := pl010FixtureProcessLifecyclePath(t)
	lines := pl010FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := pl010FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("PL-010 check(1): %s", reason)
	}
	t.Logf("PL-010 heading found at specs/process-lifecycle.md line %d; body window = %d lines",
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
			label:  "must-transition-to-degraded",
			needle: "MUST transition to",
			detail: "PL-010 body must declare 'MUST transition to' degraded status on Cat 0 failure " +
				"(expected phrase 'MUST transition to'); this is the authoritative transition rule — " +
				"when any Cat 0 pre-check step fails, the daemon enters degraded instead of ready " +
				"and stays there until prerequisites clear",
		},
		{
			id:     "3",
			label:  "must-not-classify-in-flight-runs",
			needle: "MUST NOT classify in-flight runs",
			detail: "PL-010 body must declare 'MUST NOT classify in-flight runs' while degraded " +
				"(expected phrase 'MUST NOT classify in-flight runs'); classification triggers " +
				"reconciliation — if infrastructure is unavailable, classification would produce " +
				"unreliable verdicts; the daemon must wait until prerequisites clear",
		},
		{
			id:     "4",
			label:  "infrastructure-unavailable-event",
			needle: "infrastructure_unavailable",
			detail: "PL-010 body must name 'infrastructure_unavailable' as the event to emit " +
				"(expected phrase 'infrastructure_unavailable'); this event names the specific " +
				"prerequisite that failed — operators consuming ON §4.9 health events use it " +
				"to diagnose why the daemon is stuck in degraded state",
		},
		{
			id:     "5",
			label:  "periodically-retry-pre-check",
			needle: "periodically retry",
			detail: "PL-010 body must declare 'periodically retry' the pre-check while degraded " +
				"(expected phrase 'periodically retry'); the daemon must not wait indefinitely for " +
				"operator intervention — it retries at a configurable cadence (default 10s per " +
				"OQ-PL-002) so that it can self-recover when the infrastructure prerequisite clears",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !pl010FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"PL-010 check(%s) FAILED: %s\n"+
						"  spec:    specs/process-lifecycle.md line ~%d (PL-010 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in PL-010 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if pl010FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"PL-010 check(6) FAILED: Tags: mechanism not found in PL-010 body window\n"+
					"  spec:   specs/process-lifecycle.md line ~%d (PL-010 body)\n"+
					"  detail: PL-010 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mup.19 audit complete — PL-010 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
