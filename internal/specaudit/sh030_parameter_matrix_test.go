//go:build specaudit

package specaudit_test

// hk-i0tw.32 binding test — SH-030: scenarios MAY declare a parameter matrix (cap 1024 cells).
//
// Spec ref: specs/scenario-harness.md §4.10 SH-030.
//
// SH-030 states: a ScenarioFile MAY declare a `matrix` field — map of
// parameter-name to list-of-values.  Harness MUST expand the scenario into one
// execution per cell of the cartesian product, capped at 1024 cells per
// scenario; over-cap → scenario-load-failure.  Synthetic per-cell names render
// as `<scenario-name>[<k1>=<v1>,<k2>=<v2>,...]` with parameter keys in byte-lex
// order.  Parameter substitution uses Go's text/template syntax (delimiters
// `{{` and `}}`); only field-substitution supported (no conditionals, loops,
// function calls beyond identity).  Unknown parameters, unresolvable templates,
// or matrix-cell name collisions (per SH-005) MUST fail at scenario-load time.
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-030 is present and declares:
//
//  1. Heading present — "#### SH-030 —".
//  2. `matrix` field declaration.
//  3. Cartesian product expansion.
//  4. 1024-cell cap + over-cap → scenario-load-failure.
//  5. Synthetic name format with byte-lex key ordering.
//  6. text/template syntax with `{{` `}}` delimiters.
//  7. Conditionals/loops/function-calls prohibition.
//  8. Unknown-parameter/unresolvable-template → scenario-load-failure.
//  9. Tags: mechanism.
//
// # Helper prefix: sh030Fixture

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

func sh030FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh030FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh030FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh030FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh030FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh030FixtureSH030Heading      = regexp.MustCompile(`^#### SH-030 —`)
	sh030FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh030FixtureTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

const sh030FixtureBodyWindow = 30

func sh030FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh030FixtureSH030Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-030 heading '#### SH-030 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh030FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh030FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh030FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH030ParameterMatrix is the binding test for SH-030.
func TestSH030ParameterMatrix(t *testing.T) {
	t.Parallel()

	specFile := sh030FixtureScenarioHarnessPath(t)
	lines := sh030FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh030FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-030 check(a): %s", reason)
	}
	t.Logf("SH-030 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "matrix-field-declaration",
			needle: "matrix",
			detail: "SH-030 body must name the `matrix` field as the parameter-matrix declaration",
		},
		{
			id:     "c",
			label:  "cartesian-product-expansion",
			needle: "cartesian product",
			detail: "SH-030 body must state expansion is one execution per cell of the cartesian product",
		},
		{
			id:     "d",
			label:  "1024-cell-cap",
			needle: "1024",
			detail: "SH-030 body must declare the 1024-cell cap; " +
				"over-cap MUST fail as scenario-load-failure",
		},
		{
			id:     "e",
			label:  "synthetic-name-format",
			needle: "byte-lexicographic",
			detail: "SH-030 body must state parameter keys in synthetic names are ordered byte-lexicographically; " +
				"this ensures deterministic name rendering across platforms",
		},
		{
			id:     "f",
			label:  "text-template-syntax",
			needle: "text/template",
			detail: "SH-030 body must state parameter substitution uses Go's text/template syntax",
		},
		{
			id:     "g",
			label:  "template-delimiters",
			needle: "{{",
			detail: "SH-030 body must declare the template delimiters `{{` and `}}`",
		},
		{
			id:     "h",
			label:  "conditionals-loops-prohibition",
			needle: "no conditionals",
			detail: "SH-030 body must state conditionals, loops, and non-identity function calls are forbidden; " +
				"only field-substitution is supported",
		},
		{
			id:     "i",
			label:  "unknown-param-scenario-load-failure",
			needle: "Unknown parameters",
			detail: "SH-030 body must state unknown parameters → scenario-load-failure at scenario-load time",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh030FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-030 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-030 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "-", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Run("check-j-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh030FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-030 check(j) FAILED: Tags: mechanism not found in SH-030 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-030 body)\n"+
					"  detail: SH-030 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-030 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
