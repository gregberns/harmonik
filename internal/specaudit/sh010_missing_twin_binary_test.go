package specaudit_test

// hk-i0tw.10 binding test — SH-010: missing twin binary fails with `twin-binary-not-found`.
//
// Spec ref: specs/scenario-harness.md §4.3 SH-010.
//
// SH-010 states: if a scenario's `agent_overrides` references a twin binary
// that does not exist (absolute path missing on disk, or name not resolvable
// against the search-path prefix), the harness MUST emit a ScenarioResult
// with verdict=error and failure_class=twin-binary-not-found BEFORE attempting
// to launch the orchestration drive.  The error MUST carry the unresolved name
// and the search paths consulted.
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-010 is present and declares:
//
//  1. Heading present — "#### SH-010 —".
//  2. Absolute-path-missing detection trigger.
//  3. Name-unresolvable detection trigger.
//  4. verdict=error emission BEFORE orchestration launch.
//  5. failure_class=twin-binary-not-found.
//  6. Error carries unresolved name and search paths consulted.
//  7. Tags: mechanism.
//
// # Helper prefix: sh010Fixture

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

func sh010FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh010FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh010FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh010FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh010FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh010FixtureSH010Heading      = regexp.MustCompile(`^#### SH-010 —`)
	sh010FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh010FixtureTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

const sh010FixtureBodyWindow = 30

func sh010FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh010FixtureSH010Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-010 heading '#### SH-010 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh010FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh010FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh010FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH010MissingTwinBinaryNotFound is the binding test for SH-010.
func TestSH010MissingTwinBinaryNotFound(t *testing.T) {
	t.Parallel()

	specFile := sh010FixtureScenarioHarnessPath(t)
	lines := sh010FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh010FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-010 check(a): %s", reason)
	}
	t.Logf("SH-010 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "absolute-path-missing-trigger",
			needle: "absolute path missing",
			detail: "SH-010 body must state that absolute-path-missing is a detection trigger for twin-binary-not-found",
		},
		{
			id:     "c",
			label:  "name-unresolvable-trigger",
			needle: "not resolvable",
			detail: "SH-010 body must state that an unresolvable name (against the search-path prefix) " +
				"is a detection trigger for twin-binary-not-found",
		},
		{
			id:     "d",
			label:  "before-orchestration-drive",
			needle: "BEFORE",
			detail: "SH-010 body must state the error is emitted BEFORE attempting to launch the orchestration drive; " +
				"this ordering invariant prevents a partially-launched scenario on a missing binary",
		},
		{
			id:     "e",
			label:  "verdict-error-emission",
			needle: "verdict=error",
			detail: "SH-010 body must state the harness emits ScenarioResult with verdict=error",
		},
		{
			id:     "f",
			label:  "failure-class-twin-binary-not-found",
			needle: "twin-binary-not-found",
			detail: "SH-010 body must state failure_class=twin-binary-not-found",
		},
		{
			id:     "g",
			label:  "error-carries-unresolved-name",
			needle: "unresolved name",
			detail: "SH-010 body must state the error carries the unresolved name; " +
				"this is the operator-readable context required for debugging",
		},
		{
			id:     "h",
			label:  "error-carries-search-paths",
			needle: "search paths",
			detail: "SH-010 body must state the error carries the search paths consulted; " +
				"this is needed alongside the unresolved name for operator-readable diagnostics",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh010FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-010 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-010 body)\n"+
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
			if sh010FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-010 check(i) FAILED: Tags: mechanism not found in SH-010 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-010 body)\n"+
					"  detail: SH-010 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-010 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
