package specaudit_test

// hk-i0tw.6 binding test — SH-006: suite-load is a discrete discovery + parse + validate phase.
//
// Spec ref: specs/scenario-harness.md §4.2 SH-006.
//
// SH-006 states: the harness MUST execute scenario discovery, file parse,
// schema validation, and uniqueness check in a discrete `suite-load` phase
// BEFORE launching any orchestration.  Suite-load failure (duplicates, parse
// errors, schema errors) MUST abort the entire suite with a single suite-level
// error and MUST NOT execute any scenarios.  Per-scenario load failures ARE
// still recorded as individual ScenarioResult entries with verdict=error.
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-006 is present and declares:
//
//  1. Heading present — "#### SH-006 —".
//  2. Discrete suite-load phase (before orchestration).
//  3. Suite-load failure aborts the entire suite.
//  4. No scenarios executed on suite-load failure.
//  5. Per-scenario load failures recorded with verdict=error.
//  6. Tags: mechanism.
//
// # Helper prefix: sh006Fixture

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

func sh006FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh006FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh006FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh006FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh006FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var sh006FixtureSH006Heading = regexp.MustCompile(`^#### SH-006 —`)
var sh006FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
var sh006FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

const sh006FixtureBodyWindow = 30

func sh006FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh006FixtureSH006Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-006 heading '#### SH-006 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh006FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh006FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh006FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH006SuiteLoadPhase is the binding test for SH-006.
func TestSH006SuiteLoadPhase(t *testing.T) {
	t.Parallel()

	specFile := sh006FixtureScenarioHarnessPath(t)
	lines := sh006FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh006FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-006 check(a): %s", reason)
	}
	t.Logf("SH-006 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "discrete-suite-load-phase",
			needle: "suite-load",
			detail: "SH-006 body must declare a discrete `suite-load` phase; " +
				"this is the load-bearing term used in SH-004/SH-005 to reference suite-load failures",
		},
		{
			id:     "c",
			label:  "before-orchestration-ordering",
			needle: "before launching",
			detail: "SH-006 body must state suite-load executes before launching any orchestration; " +
				"this ordering invariant prevents partial execution on bad input",
		},
		{
			id:     "d",
			label:  "suite-load-failure-aborts-suite",
			needle: "abort the entire suite",
			detail: "SH-006 body must state suite-load failure aborts the entire suite; " +
				"this distinguishes suite-load abort from per-scenario error recording",
		},
		{
			id:     "e",
			label:  "no-scenarios-executed-on-failure",
			needle: "MUST NOT execute any scenarios",
			detail: "SH-006 body must state no scenarios are executed when suite-load fails; " +
				"absence means the abort invariant is undocumented",
		},
		{
			id:     "f",
			label:  "per-scenario-error-recording",
			needle: "verdict=error",
			detail: "SH-006 body must state per-scenario load failures are still recorded " +
				"as ScenarioResult entries with verdict=error so the operator sees the inventory",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh006FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-006 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-006 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "-", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Run("check-g-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh006FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-006 check(g) FAILED: Tags: mechanism not found in SH-006 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-006 body)\n"+
					"  detail: SH-006 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-006 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
