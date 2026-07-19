//go:build specaudit

package specaudit_test

// hk-i0tw.22 binding test — SH-020 harness reads the captured JSONL event log as the
// assertion surface.
//
// Spec ref: specs/scenario-harness.md §4.6 SH-020.
//
// SH-020 states: after orchestration completes, the harness MUST read the captured JSONL
// event log and evaluate each declared assertion against it. The read MUST conform to
// EV-021 observational-replay rules. The harness MUST NOT reconstruct state from JSONL.
// The harness MUST also capture each handler subprocess's stdout/stderr to per-scenario
// log files.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The harness implementation is pending; this sensor
// verifies that SH-020 is correctly declared in the spec so that:
//
//  1. SH-020 heading is present in specs/scenario-harness.md.
//  2. "observational-replay" rules are declared for the JSONL read.
//  3. "MUST NOT reconstruct state from JSONL" is declared.
//  4. "stdout and stderr" capture to per-scenario files is declared.
//  5. "ScenarioResult" is named as the reporting structure.
//  6. Tags: mechanism is present in the SH-020 body window.
//
// # Failure modes
//
//   - SH-020 heading missing.
//   - observational-replay absent.
//   - MUST NOT reconstruct state from JSONL absent.
//   - stdout and stderr absent.
//   - ScenarioResult absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sh020Fixture prefix per
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

// sh020FixtureScenarioHarnessPath returns the absolute path to specs/scenario-harness.md.
func sh020FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh020FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// sh020FixtureHeading matches the SH-020 level-4 requirement heading line.
var sh020FixtureHeading = regexp.MustCompile(`^#### SH-020 —`)

// sh020FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var sh020FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sh020FixtureTagsMechanism matches a "Tags: mechanism" line.
var sh020FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// sh020FixtureBodyWindow is the maximum number of lines to scan after the heading.
const sh020FixtureBodyWindow = 15

// sh020FixtureLoadLines opens specFile and returns all lines.
func sh020FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh020FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh020FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sh020FixtureBodyLines returns the lines comprising the SH-020 body.
func sh020FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh020FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-020 heading not found; expected '#### SH-020 —' in specs/scenario-harness.md"
	}

	limit := headingIdx + 1 + sh020FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if sh020FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// sh020FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func sh020FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH020JSONLEventLogAssertionSurface is the binding test for hk-i0tw.22.
func TestSH020JSONLEventLogAssertionSurface(t *testing.T) {
	t.Parallel()

	specFile := sh020FixtureScenarioHarnessPath(t)
	lines := sh020FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh020FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-020 check(1): %s", reason)
	}
	t.Logf("SH-020 heading found at specs/scenario-harness.md line %d; body window = %d lines",
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
			label:  "observational-replay-rules",
			needle: "observational-replay",
			detail: "SH-020 body must declare 'observational-replay' rules for the JSONL read " +
				"(expected phrase 'observational-replay'); the harness reads the event log as an " +
				"observer — output is advisory for state but authoritative for which events were " +
				"observed; this is the EV-021 observational-replay discipline",
		},
		{
			id:     "3",
			label:  "must-not-reconstruct-state-from-jsonl",
			needle: "MUST NOT reconstruct state from JSONL",
			detail: "SH-020 body must declare harness 'MUST NOT reconstruct state from JSONL' " +
				"(expected phrase 'MUST NOT reconstruct state from JSONL'); this is the negative " +
				"constraint paired with observational-replay — the harness uses git+Beads (not JSONL) " +
				"for workspace state; JSONL is the event assertion surface only",
		},
		{
			id:     "4",
			label:  "stdout-stderr-capture",
			needle: "stdout and stderr",
			detail: "SH-020 body must declare 'stdout and stderr' capture to per-scenario files " +
				"(expected phrase 'stdout and stderr'); capturing handler stdout/stderr gives " +
				"operators a post-hoc debugging record without making it part of the assertion " +
				"surface — assertions run against the JSONL event log, not the subprocess output",
		},
		{
			id:     "5",
			label:  "scenarioresult-reporting",
			needle: "ScenarioResult",
			detail: "SH-020 body must name 'ScenarioResult' as the reporting structure " +
				"(expected phrase 'ScenarioResult'); the paths to captured stdout/stderr log files " +
				"MUST be included in ScenarioResult so operators can find them for debugging — " +
				"even though they are not assertion surface",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !sh020FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-020 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-020 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in SH-020 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh020FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-020 check(6) FAILED: Tags: mechanism not found in SH-020 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-020 body)\n"+
					"  detail: SH-020 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-i0tw.22 audit complete — SH-020 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
