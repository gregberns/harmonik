//go:build specaudit

package specaudit_test

// hk-i0tw.5 binding test — SH-005: scenario name uniqueness is repository-wide.
//
// Spec ref: specs/scenario-harness.md §4.1 SH-005.
//
// SH-005 states: every scenario file MUST declare a `name` field unique across
// the entire `scenarios/` tree.  Names MUST match
// `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$` (no slashes, no whitespace, no
// path-traversal sequences); names that do not match MUST fail with
// `scenario-load-failure`.  Name collision after matrix expansion (per SH-030)
// is also a `scenario-load-failure`.  Duplicates detected at suite-load time
// cause the ENTIRE suite to fail (not merely the duplicate scenarios).
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-005 is present and declares:
//
//  1. Heading present — "#### SH-005 —".
//  2. `name` field uniqueness obligation.
//  3. Name regex `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`.
//  4. Non-matching names → scenario-load-failure.
//  5. Post-matrix-expansion collision → scenario-load-failure.
//  6. Entire suite fails on duplicates (not merely the duplicate scenarios).
//  7. Tags: mechanism.
//
// # Helper prefix: sh005Fixture

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

func sh005FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh005FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh005FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh005FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh005FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh005FixtureSH005Heading      = regexp.MustCompile(`^#### SH-005 —`)
	sh005FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh005FixtureTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

const sh005FixtureBodyWindow = 30

func sh005FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh005FixtureSH005Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-005 heading '#### SH-005 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh005FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh005FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh005FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH005ScenarioNameUniqueness is the binding test for SH-005.
func TestSH005ScenarioNameUniqueness(t *testing.T) {
	t.Parallel()

	specFile := sh005FixtureScenarioHarnessPath(t)
	lines := sh005FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh005FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-005 check(a): %s", reason)
	}
	t.Logf("SH-005 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "name-field-uniqueness",
			needle: "unique",
			detail: "SH-005 body must state `name` field uniqueness across the scenarios/ tree; " +
				"this is the primary naming invariant",
		},
		{
			id:     "c",
			label:  "name-regex",
			needle: `[A-Za-z0-9][A-Za-z0-9._-]`,
			detail: "SH-005 body must declare the name regex `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`; " +
				"this grounds the allowed-character set to a concrete pattern",
		},
		{
			id:     "d",
			label:  "non-matching-name-scenario-load-failure",
			needle: "scenario-load-failure",
			detail: "SH-005 body must state non-matching names → scenario-load-failure",
		},
		{
			id:     "e",
			label:  "matrix-expansion-collision",
			needle: "matrix",
			detail: "SH-005 body must state name collision after matrix expansion (per SH-030) " +
				"is also a scenario-load-failure",
		},
		{
			id:     "f",
			label:  "entire-suite-fails-on-duplicate",
			needle: "entire suite",
			detail: "SH-005 body must state the entire suite fails on duplicates " +
				"(not merely the duplicate scenarios); " +
				"this is the suite-abort invariant distinguishing per-scenario from suite-level failures",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh005FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-005 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-005 body)\n"+
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
			if sh005FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-005 check(g) FAILED: Tags: mechanism not found in SH-005 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-005 body)\n"+
					"  detail: SH-005 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-005 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
