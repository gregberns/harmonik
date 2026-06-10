package specaudit_test

// hk-i0tw.3 binding test — SH-003: scenario file is declarative-loadable.
//
// Spec ref: specs/scenario-harness.md §4.1 SH-003.
//
// SH-003 states: a scenario file MUST be parseable by a generic YAML loader
// plus the §6.1 schema validator without executing any scenario-defined code.
// !eval and !!python/object tags are forbidden.  Encoding MUST be UTF-8 without
// BOM.  Per-file size ceiling 1 MiB; parsed-node count ceiling 100 000 nodes;
// either overrun → scenario-load-failure.
//
// # Audit frame
//
// Spec-corpus binding test verifying that SH-003 is present and declares:
//
//  1. Heading present — "#### SH-003 —".
//  2. !eval prohibition.
//  3. !!python/object prohibition.
//  4. UTF-8 without BOM requirement.
//  5. Per-file size ceiling (1 MiB).
//  6. Parsed-node count ceiling (100 000).
//  7. Both ceiling violations → scenario-load-failure.
//  8. Tags: mechanism.
//
// # Helper prefix: sh003Fixture

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

func sh003FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh003FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh003FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh003FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh003FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh003FixtureSH003Heading      = regexp.MustCompile(`^#### SH-003 —`)
	sh003FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh003FixtureTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

const sh003FixtureBodyWindow = 30

func sh003FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh003FixtureSH003Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-003 heading '#### SH-003 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh003FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh003FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh003FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH003DeclarativeLoadable is the binding test for SH-003.
func TestSH003DeclarativeLoadable(t *testing.T) {
	t.Parallel()

	specFile := sh003FixtureScenarioHarnessPath(t)
	lines := sh003FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh003FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-003 check(a): %s", reason)
	}
	t.Logf("SH-003 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "eval-tag-prohibition",
			needle: "!eval",
			detail: "SH-003 body must forbid `!eval`-style YAML extensions; " +
				"these would execute embedded code during parse, violating the declarative-loadable invariant",
		},
		{
			id:     "c",
			label:  "python-object-prohibition",
			needle: "!!python/object",
			detail: "SH-003 body must forbid `!!python/object` constructors; " +
				"these are the canonical code-execution YAML tags",
		},
		{
			id:     "d",
			label:  "utf8-without-bom",
			needle: "UTF-8",
			detail: "SH-003 body must state the encoding MUST be UTF-8 without BOM; " +
				"files with a BOM or non-UTF-8 encoding MUST be rejected as scenario-load-failure",
		},
		{
			id:     "e",
			label:  "size-ceiling-1mib",
			needle: "1 MiB",
			detail: "SH-003 body must declare the per-file size ceiling of 1 MiB; " +
				"overrun → scenario-load-failure",
		},
		{
			id:     "f",
			label:  "node-count-ceiling",
			needle: "100 000",
			detail: "SH-003 body must declare the parsed-node count ceiling of 100 000; " +
				"overrun → scenario-load-failure",
		},
		{
			id:     "g",
			label:  "ceiling-overrun-scenario-load-failure",
			needle: "scenario-load-failure",
			detail: "SH-003 body must state ceiling overrun is classified as scenario-load-failure",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh003FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-003 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-003 body)\n"+
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
			if sh003FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-003 check(h) FAILED: Tags: mechanism not found in SH-003 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-003 body)\n"+
					"  detail: SH-003 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-003 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
