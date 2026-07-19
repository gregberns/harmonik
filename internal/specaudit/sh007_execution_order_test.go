//go:build specaudit

package specaudit_test

// hk-i0tw.7 binding test — SH-007: scenario execution order is deterministic byte-lex.
//
// Spec ref: specs/scenario-harness.md §4.2 SH-007.
//
// SH-007 states: within a single suite invocation, the harness MUST execute
// scenarios in byte-lexicographic order of their `name` field
// (locale-independent UTF-8 byte comparison; NOT Unicode-collation order).
// Matrix-expanded synthetic names (per SH-030) participate in the same
// ordering.  Determinism is required for flaky-test bisection, log-diff
// comparison, and crash-report localization.
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-007 is present and declares:
//
//  1. Heading present — "#### SH-007 —".
//  2. Byte-lexicographic ordering by `name`.
//  3. Locale-independent comparison (NOT Unicode-collation).
//  4. Matrix-expanded names participate in the same ordering.
//  5. Determinism rationale: bisection / log-diff / localization.
//  6. Tags: mechanism.
//
// # Helper prefix: sh007Fixture

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

func sh007FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh007FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh007FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh007FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh007FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh007FixtureSH007Heading      = regexp.MustCompile(`^#### SH-007 —`)
	sh007FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh007FixtureTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

const sh007FixtureBodyWindow = 30

func sh007FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh007FixtureSH007Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-007 heading '#### SH-007 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh007FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh007FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh007FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH007ExecutionOrderDeterministic is the binding test for SH-007.
func TestSH007ExecutionOrderDeterministic(t *testing.T) {
	t.Parallel()

	specFile := sh007FixtureScenarioHarnessPath(t)
	lines := sh007FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh007FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-007 check(a): %s", reason)
	}
	t.Logf("SH-007 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "byte-lexicographic-ordering",
			needle: "byte-lexicographic",
			detail: "SH-007 body must declare byte-lexicographic ordering by `name`; " +
				"this grounds the ordering predicate to a concrete byte comparison",
		},
		{
			id:     "c",
			label:  "locale-independent-comparison",
			needle: "locale-independent",
			detail: "SH-007 body must state the comparison is locale-independent (NOT Unicode-collation order); " +
				"this prevents locale-specific ordering surprises across operator machines",
		},
		{
			id:     "d",
			label:  "matrix-expanded-names-participate",
			needle: "Matrix-expanded",
			detail: "SH-007 body must state matrix-expanded synthetic names participate in the same byte-lex ordering; " +
				"this ensures matrix cells are interleaved deterministically with non-matrix scenarios",
		},
		{
			id:     "e",
			label:  "bisection-rationale",
			needle: "bisection",
			detail: "SH-007 body must name flaky-test bisection as a rationale; " +
				"this grounds the determinism requirement to a concrete operator need",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh007FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-007 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-007 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "-", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Run("check-f-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh007FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-007 check(f) FAILED: Tags: mechanism not found in SH-007 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-007 body)\n"+
					"  detail: SH-007 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-007 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
