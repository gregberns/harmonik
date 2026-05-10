package specaudit_test

// hk-i0tw.36 binding test — SH-034: ScenarioResult durability and emission.
//
// Spec ref: specs/scenario-harness.md §4.13 SH-034.
//
// SH-034 states: every ScenarioResult MUST be written to disk at
// `<fixture-root>/<scenario-name>/result.json` immediately after the scenario
// completes (before the next scenario begins).  The aggregate SuiteResult MUST
// be written to `<fixture-root>/suite-result.json` AND emitted to stdout per
// SH-032 at suite completion.  suite_verdict=pass iff every result.verdict==pass;
// any non-pass → suite_verdict=fail.  ScenarioResult.error_detail MUST be a
// non-empty operator-readable string when present.
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-034 is present and declares:
//
//  1. Heading present — "#### SH-034 —".
//  2. Per-scenario result.json durability path.
//  3. Written immediately after scenario completes (before next).
//  4. SuiteResult written to suite-result.json.
//  5. SuiteResult emitted to stdout (per SH-032).
//  6. suite_verdict=pass iff all result.verdict==pass.
//  7. Any non-pass implies suite_verdict=fail.
//  8. error_detail non-empty operator-readable string when present.
//  9. Tags: mechanism.
//
// # Helper prefix: sh034Fixture

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

func sh034FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh034FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh034FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh034FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh034FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var sh034FixtureSH034Heading = regexp.MustCompile(`^#### SH-034 —`)
var sh034FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
var sh034FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

const sh034FixtureBodyWindow = 30

func sh034FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh034FixtureSH034Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-034 heading '#### SH-034 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh034FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh034FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh034FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH034ScenarioResultDurability is the binding test for SH-034.
func TestSH034ScenarioResultDurability(t *testing.T) {
	t.Parallel()

	specFile := sh034FixtureScenarioHarnessPath(t)
	lines := sh034FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh034FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-034 check(a): %s", reason)
	}
	t.Logf("SH-034 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "per-scenario-result-json-path",
			needle: "result.json",
			detail: "SH-034 body must declare the per-scenario durability path `<fixture-root>/<scenario-name>/result.json`",
		},
		{
			id:     "c",
			label:  "written-before-next-scenario",
			needle: "before the next scenario",
			detail: "SH-034 body must state the per-scenario result.json is written BEFORE the next scenario begins; " +
				"this ordering invariant enables operator reconstruction on crash",
		},
		{
			id:     "d",
			label:  "suite-result-json-path",
			needle: "suite-result.json",
			detail: "SH-034 body must declare the aggregate SuiteResult durability path `<fixture-root>/suite-result.json`",
		},
		{
			id:     "e",
			label:  "suiteresult-emitted-to-stdout",
			needle: "stdout",
			detail: "SH-034 body must state SuiteResult is also emitted to stdout per SH-032",
		},
		{
			id:     "f",
			label:  "suite-verdict-pass-iff-all-pass",
			needle: "suite_verdict",
			detail: "SH-034 body must state suite_verdict=pass iff every result.verdict==pass; " +
				"any non-pass → suite_verdict=fail",
		},
		{
			id:     "g",
			label:  "error-detail-non-empty",
			needle: "error_detail",
			detail: "SH-034 body must state ScenarioResult.error_detail MUST be a non-empty operator-readable string " +
				"when present (carrying at minimum err.Error())",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh034FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-034 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-034 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "-", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Run("check-h-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh034FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-034 check(h) FAILED: Tags: mechanism not found in SH-034 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-034 body)\n"+
					"  detail: SH-034 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-034 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
