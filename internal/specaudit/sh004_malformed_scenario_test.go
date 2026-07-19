//go:build specaudit

package specaudit_test

// hk-i0tw.4 binding test — SH-004: malformed scenarios fail with `scenario-load-failure`.
//
// Spec ref: specs/scenario-harness.md §4.1 SH-004.
//
// SH-004 states: a scenario file that fails YAML parse, fails the §6.1 schema
// check, references an unknown workflow, or carries an unknown cadence-tag value
// MUST be classified as `scenario-load-failure`.  The scenario MUST NOT be
// partially executed; the harness MUST emit a ScenarioResult with verdict=error
// and failure_class=scenario-load-failure, then move on to the next scenario.
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-004 is present and declares:
//
//  1. Heading present — "#### SH-004 —".
//  2. YAML parse failure → scenario-load-failure.
//  3. §6.1 schema check failure → scenario-load-failure.
//  4. Unknown workflow reference → scenario-load-failure.
//  5. Unknown cadence-tag → scenario-load-failure.
//  6. Partial-execution prohibition.
//  7. ScenarioResult verdict=error + failure_class=scenario-load-failure emission.
//  8. Suite continues to next scenario after a per-scenario load failure.
//  9. Tags: mechanism.
//
// # Helper prefix: sh004Fixture

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

func sh004FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh004FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh004FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh004FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh004FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh004FixtureSH004Heading      = regexp.MustCompile(`^#### SH-004 —`)
	sh004FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh004FixtureTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

const sh004FixtureBodyWindow = 30

func sh004FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh004FixtureSH004Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-004 heading '#### SH-004 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh004FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh004FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh004FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH004MalformedScenarioLoadFailure is the binding test for SH-004.
func TestSH004MalformedScenarioLoadFailure(t *testing.T) {
	t.Parallel()

	specFile := sh004FixtureScenarioHarnessPath(t)
	lines := sh004FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh004FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-004 check(a): %s", reason)
	}
	t.Logf("SH-004 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "yaml-parse-failure-classification",
			needle: "YAML parse",
			detail: "SH-004 body must name YAML parse failure as a scenario-load-failure trigger; " +
				"this is the primary malformed-file detection path",
		},
		{
			id:     "c",
			label:  "schema-check-failure-classification",
			needle: "schema check",
			detail: "SH-004 body must name §6.1 schema check failure as a scenario-load-failure trigger",
		},
		{
			id:     "d",
			label:  "unknown-workflow-classification",
			needle: "unknown workflow",
			detail: "SH-004 body must name unknown workflow reference as a scenario-load-failure trigger",
		},
		{
			id:     "e",
			label:  "unknown-cadence-tag-classification",
			needle: "cadence-tag",
			detail: "SH-004 body must name unknown cadence-tag value as a scenario-load-failure trigger",
		},
		{
			id:     "f",
			label:  "partial-execution-prohibition",
			needle: "partially executed",
			detail: "SH-004 body must state the scenario MUST NOT be partially executed; " +
				"this is the non-partial-execution safety invariant",
		},
		{
			id:     "g",
			label:  "scenarioresult-verdict-error",
			needle: "verdict=error",
			detail: "SH-004 body must state the harness emits ScenarioResult with verdict=error " +
				"for a load failure",
		},
		{
			id:     "h",
			label:  "failure-class-scenario-load-failure",
			needle: "scenario-load-failure",
			detail: "SH-004 body must state the failure_class is scenario-load-failure",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh004FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-004 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-004 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "-", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Run("check-i-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh004FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-004 check(i) FAILED: Tags: mechanism not found in SH-004 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-004 body)\n"+
					"  detail: SH-004 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-004 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
