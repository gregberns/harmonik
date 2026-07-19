//go:build specaudit

package specaudit_test

// hk-i0tw.27 binding test — SH-025: every scenario declares a wall-clock budget.
//
// Spec ref: specs/scenario-harness.md §4.7 SH-025.
//
// SH-025 states: every ScenarioFile MUST declare a positive `timeout_secs`
// field (integer seconds, range [1, 7200]); out-of-range values MUST fail at
// scenario-load time per SH-004.  Deadline MUST be measured against a monotonic
// clock (Go's time.Now() monotonic component) so NTP corrections do not cause
// spurious timeouts.  No harness-default budget.
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-025 is present and declares:
//
//  1. Heading present — "#### SH-025 —".
//  2. `timeout_secs` field required.
//  3. Valid range [1, 7200].
//  4. Out-of-range → scenario-load-failure (per SH-004).
//  5. Monotonic clock obligation.
//  6. No harness-default budget.
//  7. Tags: mechanism.
//
// # Helper prefix: sh025Fixture

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

func sh025FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh025FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh025FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh025FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh025FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh025FixtureSH025Heading      = regexp.MustCompile(`^#### SH-025 —`)
	sh025FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh025FixtureTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

const sh025FixtureBodyWindow = 30

func sh025FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh025FixtureSH025Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-025 heading '#### SH-025 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh025FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh025FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh025FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH025TimeoutSecsRequired is the binding test for SH-025.
func TestSH025TimeoutSecsRequired(t *testing.T) {
	t.Parallel()

	specFile := sh025FixtureScenarioHarnessPath(t)
	lines := sh025FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh025FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-025 check(a): %s", reason)
	}
	t.Logf("SH-025 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "timeout-secs-field-required",
			needle: "timeout_secs",
			detail: "SH-025 body must name the `timeout_secs` field as the required wall-clock budget declaration",
		},
		{
			id:     "c",
			label:  "valid-range-upper-bound",
			needle: "7200",
			detail: "SH-025 body must declare the upper bound of the valid range (7200 seconds = 2 hours); " +
				"this bounds resource exposure from accidental long-running scenarios",
		},
		{
			id:     "d",
			label:  "out-of-range-scenario-load-failure",
			needle: "scenario-load time",
			detail: "SH-025 body must state out-of-range values MUST fail at scenario-load time per SH-004",
		},
		{
			id:     "e",
			label:  "monotonic-clock-obligation",
			needle: "monotonic",
			detail: "SH-025 body must state the deadline MUST be measured against a monotonic clock; " +
				"this prevents spurious timeouts during NTP corrections",
		},
		{
			id:     "f",
			label:  "no-harness-default-budget",
			needle: "no harness-default",
			detail: "SH-025 body must state there is no harness-default budget; " +
				"explicit declaration is the discipline to prevent accidental long-runners",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh025FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-025 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-025 body)\n"+
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
			if sh025FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-025 check(g) FAILED: Tags: mechanism not found in SH-025 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-025 body)\n"+
					"  detail: SH-025 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-025 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
