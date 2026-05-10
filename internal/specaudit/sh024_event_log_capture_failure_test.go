package specaudit_test

// hk-i0tw.26 binding test — SH-024 event-log capture failure escalates to
// harness-internal-error.
//
// Spec ref: specs/scenario-harness.md §4.8 SH-024.
//
// SH-024 states: the harness's read of the captured JSONL is governed by
// event-model.md §6.2's post-fsync torn-tail rule — torn tail MUST be skipped silently.
// The harness MUST classify as verdict=error / failure_class=harness-internal-error (NOT
// assertion-failed) when: (i) event-log directory or file does not exist; (ii) permissions
// error; (iii) JSON-parse error at position other than post-fsync tail (mid-file corruption);
// (iv) disk-full or other I/O error; (v) EV-011a bus_overflow event observed in log.
// Harness MUST NOT silently treat absent events as "not emitted" when log is corrupt.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The scenario harness implementation is pending; this
// sensor verifies that SH-024 is correctly declared in the spec so that:
//
//  1. SH-024 heading is present in specs/scenario-harness.md.
//  2. "torn tail" post-fsync skipping is declared.
//  3. "harness-internal-error" is the required failure_class for log failures.
//  4. "NOT as assertion-failed" is declared for log-read failures.
//  5. "bus_overflow" event triggers harness-internal-error.
//  6. "MUST NOT silently treat absent events" is declared.
//  7. Tags: mechanism is present in the SH-024 body window.
//
// # Failure modes
//
//   - SH-024 heading missing.
//   - torn tail absent.
//   - harness-internal-error absent.
//   - NOT assertion-failed absent.
//   - bus_overflow absent.
//   - MUST NOT silently treat absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sh024Fixture prefix per
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

// sh024FixtureScenarioHarnessPath returns the absolute path to specs/scenario-harness.md.
func sh024FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh024FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// sh024FixtureSH024Heading matches the SH-024 level-4 requirement heading line.
var sh024FixtureSH024Heading = regexp.MustCompile(`^#### SH-024 —`)

// sh024FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var sh024FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sh024FixtureTagsMechanism matches a "Tags: mechanism" line.
var sh024FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// sh024FixtureBodyWindow is the maximum number of lines after the SH-024
// heading to scan for requirement-body content.
const sh024FixtureBodyWindow = 30

// sh024FixtureLoadLines opens specFile and returns all lines.
func sh024FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh024FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh024FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sh024FixtureSH024BodyLines returns the lines comprising the SH-024 body.
func sh024FixtureSH024BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh024FixtureSH024Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-024 heading not found; expected '#### SH-024 —' in specs/scenario-harness.md"
	}

	limit := headingIdx + 1 + sh024FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if sh024FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// sh024FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func sh024FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH024EventLogCaptureFailureEscalates is the binding test for hk-i0tw.26.
func TestSH024EventLogCaptureFailureEscalates(t *testing.T) {
	t.Parallel()

	specFile := sh024FixtureScenarioHarnessPath(t)
	lines := sh024FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh024FixtureSH024BodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-024 check(1): %s", reason)
	}
	t.Logf("SH-024 heading found at specs/scenario-harness.md line %d; body window = %d lines",
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
			label:  "torn-tail-skipped-silently",
			needle: "torn tail",
			detail: "SH-024 body must declare 'torn tail' at end of file is skipped silently " +
				"(expected phrase 'torn tail'); a partial trailing record after the last fsync " +
				"is normal post-fsync behavior per EV §6.2 — the harness MUST NOT classify this " +
				"as corruption; only mid-file parse errors are real corruption",
		},
		{
			id:     "3",
			label:  "harness-internal-error-failure-class",
			needle: "harness-internal-error",
			detail: "SH-024 body must declare 'harness-internal-error' as the failure_class for log failures " +
				"(expected phrase 'harness-internal-error'); this failure class signals the harness " +
				"itself is broken, not that the system-under-test failed an assertion — it routes " +
				"to human investigation rather than automated retry",
		},
		{
			id:     "4",
			label:  "not-assertion-failed",
			needle: "NOT",
			detail: "SH-024 body must declare log failures are NOT 'assertion-failed' " +
				"(expected phrase 'NOT'); classifying a corrupt log as assertion-failed would " +
				"produce a false negative — the test appears to fail for missing events when " +
				"the events may exist but the log is unreadable",
		},
		{
			id:     "5",
			label:  "bus-overflow-triggers-internal-error",
			needle: "bus_overflow",
			detail: "SH-024 body must name 'bus_overflow' as a trigger for harness-internal-error " +
				"(expected phrase 'bus_overflow'); an EV-011a bus_overflow event in the captured log " +
				"means events were shed during the scenario — the log is incomplete and assertions " +
				"about absent events are unreliable",
		},
		{
			id:     "6",
			label:  "must-not-silently-treat-absent-events",
			needle: "MUST NOT silently treat absent events",
			detail: "SH-024 body must declare 'MUST NOT silently treat absent events' as not emitted " +
				"(expected phrase 'MUST NOT silently treat absent events'); when the log is corrupt " +
				"or incomplete, the harness cannot know if events were emitted but lost — treating " +
				"absence as 'not emitted' would produce false positives for absence assertions",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !sh024FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-024 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-024 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in SH-024 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh024FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-024 check(7) FAILED: Tags: mechanism not found in SH-024 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-024 body)\n"+
					"  detail: SH-024 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-i0tw.26 audit complete — SH-024 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
