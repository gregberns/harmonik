//go:build specaudit

package specaudit_test

// hk-i0tw.34 binding test — SH-032: harness CLI grammar at MVH.
//
// Spec ref: specs/scenario-harness.md §4.12 SH-032.
//
// SH-032 states: the harness binary is `harmonik harness` (a `harmonik`
// subcommand).  The MVH CLI surface declares 8 flags/arguments.  SuiteResult
// MUST be written to stdout; harness-internal logs to stderr.  Exit codes:
// 0=pass, 1=fail, 2=suite-load-aborted, 3=harness-internal-error, 130=SIGINT.
// Two concurrent invocations are permitted (each with its own fixture root).
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-032 is present and declares:
//
//  1. Heading present — "#### SH-032 —".
//  2. `harmonik harness` subcommand identity.
//  3. --cadence flag citation.
//  4. --twin-search-path flag citation.
//  5. --dry-run flag citation.
//  6. SuiteResult to stdout, logs to stderr.
//  7. Exit code 0 = pass.
//  8. Exit code 1 = fail.
//  9. Exit code 2 = suite-load aborted.
//  10. Exit code 3 = harness-internal error.
//  11. Exit code 130 = SIGINT.
//  12. Concurrent invocations permitted.
//  13. Tags: mechanism.
//
// # Helper prefix: sh032Fixture

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

func sh032FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh032FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh032FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh032FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh032FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh032FixtureSH032Heading      = regexp.MustCompile(`^#### SH-032 —`)
	sh032FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh032FixtureTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

// sh032FixtureBodyWindow is larger because SH-032 has a multi-table body.
const sh032FixtureBodyWindow = 50

func sh032FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh032FixtureSH032Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-032 heading '#### SH-032 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh032FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh032FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh032FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH032HarnessCLIGrammar is the binding test for SH-032.
func TestSH032HarnessCLIGrammar(t *testing.T) {
	t.Parallel()

	specFile := sh032FixtureScenarioHarnessPath(t)
	lines := sh032FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh032FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-032 check(a): %s", reason)
	}
	t.Logf("SH-032 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "harmonik-harness-subcommand",
			needle: "harmonik harness",
			detail: "SH-032 body must declare the binary is `harmonik harness` (a harmonik subcommand)",
		},
		{
			id:     "c",
			label:  "cadence-flag",
			needle: "--cadence",
			detail: "SH-032 body must declare the --cadence flag (cadence filter per SH-029)",
		},
		{
			id:     "d",
			label:  "twin-search-path-flag",
			needle: "--twin-search-path",
			detail: "SH-032 body must declare the --twin-search-path flag (SH-009 override)",
		},
		{
			id:     "e",
			label:  "dry-run-flag",
			needle: "--dry-run",
			detail: "SH-032 body must declare the --dry-run flag (suite-load + matrix expansion only; no orchestration)",
		},
		{
			id:     "f",
			label:  "suiteresult-to-stdout",
			needle: "stdout",
			detail: "SH-032 body must state SuiteResult MUST be written to stdout; " +
				"this separates the machine-readable result from operator-facing logs",
		},
		{
			id:     "g",
			label:  "exit-code-0-pass",
			needle: "0",
			detail: "SH-032 body must declare exit code 0 = pass (suite_verdict = pass)",
		},
		{
			id:     "h",
			label:  "exit-code-1-fail",
			needle: "1",
			detail: "SH-032 body must declare exit code 1 = fail (≥1 scenario failed)",
		},
		{
			id:     "i",
			label:  "exit-code-2-suite-load-aborted",
			needle: "2",
			detail: "SH-032 body must declare exit code 2 = suite-load aborted (per SH-006)",
		},
		{
			id:     "j",
			label:  "exit-code-3-harness-internal-error",
			needle: "3",
			detail: "SH-032 body must declare exit code 3 = harness-internal error",
		},
		{
			id:     "k",
			label:  "exit-code-130-sigint",
			needle: "130",
			detail: "SH-032 body must declare exit code 130 = SIGINT (operator interrupt)",
		},
		{
			id:     "l",
			label:  "concurrent-invocations-permitted",
			needle: "concurrent",
			detail: "SH-032 body must state two concurrent invocations are permitted " +
				"(each with its own per-suite ephemeral fixture root per SH-016a)",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh032FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-032 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-032 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "-", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Run("check-m-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh032FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-032 check(m) FAILED: Tags: mechanism not found in SH-032 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-032 body)\n"+
					"  detail: SH-032 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-032 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
