//go:build specaudit

package specaudit_test

// hk-i0tw.35 binding test — SH-033: signal handling and graceful shutdown.
//
// Spec ref: specs/scenario-harness.md §4.12 SH-033.
//
// SH-033 states: the harness MUST handle SIGINT and SIGTERM by attempting graceful
// shutdown — cancel the currently-running scenario per SH-026 (treating it as a
// timeout-equivalent: `verdict=error`, `failure_class=harness-internal-error`,
// `error_detail` indicating operator interrupt), execute SH-015 teardown, write a
// partial `SuiteResult` to stdout containing the results of completed scenarios plus
// the interrupted scenario's error verdict, and exit with code 130 (SIGINT) or 143
// (SIGTERM). If a second SIGINT arrives during graceful shutdown the harness MUST exit
// immediately (exit code 130) without further teardown — operator-driven escalation
// overrides the cleanup invariant.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The harness implementation is pending; this
// sensor verifies that SH-033 is correctly declared in the spec so that when the
// harness is implemented it has a binding spec to conform to. All closed sibling beads
// for the same spec section (hk-i0tw.15/SH-015, hk-i0tw.28/SH-026, hk-i0tw.33/SH-031)
// are corpus sensors; the correct form here is the same.
//
// Sub-checks verified:
//
//  1. SH-033 heading present — "#### SH-033 —".
//  2. SIGINT cited — harness must handle SIGINT explicitly.
//  3. SIGTERM cited — harness must handle SIGTERM explicitly.
//  4. Cancel scenario per SH-026 on signal (SH-026 cited in body).
//  5. verdict=error declared for interrupted scenario.
//  6. failure_class=harness-internal-error declared for interrupted scenario.
//  7. SH-015 teardown cited as a step in the graceful-shutdown sequence.
//  8. Partial SuiteResult written to stdout (token "SuiteResult" + "stdout").
//  9. Exit code 130 declared for SIGINT.
//  10. Exit code 143 declared for SIGTERM.
//  11. Second SIGINT → immediate exit without teardown.
//  12. Tags: mechanism present within 30 lines of heading.
//
// # Decision rationale (corpus-search sensor vs. executable harness fixture)
//
// The harness implementation is pending — there is no Go runtime surface to invoke
// signal handling against. An executable fixture would require spawning a real harness
// process, injecting OS signals, and inspecting exit codes; premature at this phase.
// The corpus sensor asserts the spec body declares all normative requirements (signal
// names, verdict/failure-class classification, teardown cite, partial-result write,
// exit codes, double-SIGINT escalation) so that when the harness is implemented it has
// a complete spec to conform to. The same pattern was applied for SH-026 (hk-i0tw.28)
// and SH-031 (hk-i0tw.33).
//
// # Failure modes
//
//   - SH-033 heading missing.
//   - SIGINT not cited.
//   - SIGTERM not cited.
//   - SH-026 (cancel chain) not cited.
//   - verdict=error for interrupted scenario not declared.
//   - failure_class=harness-internal-error not declared.
//   - SH-015 teardown not cited.
//   - Partial SuiteResult / stdout write not declared.
//   - Exit code 130 not declared.
//   - Exit code 143 not declared.
//   - Second SIGINT immediate-exit escalation not declared.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sh033Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

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

// sh033FixtureScenarioHarnessPath returns the absolute path to specs/scenario-harness.md.
func sh033FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh033FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// sh033FixtureLoadLines opens specFile and returns all lines.
func sh033FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh033FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh033FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sh033FixtureSH033Heading matches the SH-033 level-4 requirement heading.
var sh033FixtureSH033Heading = regexp.MustCompile(`^#### SH-033 —`)

// sh033FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var sh033FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sh033FixtureTagsMechanism matches a "Tags: mechanism" line.
var sh033FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// sh033FixtureBodyWindow is the maximum number of lines to scan after the heading.
// SH-033's body is a single dense paragraph followed by a Tags line; 30 lines is sufficient.
const sh033FixtureBodyWindow = 30

// sh033FixtureBodyLines returns the lines comprising the SH-033 body, up to the next
// heading or the body-window limit (whichever comes first).
func sh033FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh033FixtureSH033Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-033 heading not found; expected '#### SH-033 —' in specs/scenario-harness.md"
	}

	limit := headingIdx + 1 + sh033FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if sh033FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// sh033FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func sh033FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH033SignalHandlingAndGracefulShutdown is the binding test for hk-i0tw.35.
func TestSH033SignalHandlingAndGracefulShutdown(t *testing.T) {
	t.Parallel()

	specFile := sh033FixtureScenarioHarnessPath(t)
	lines := sh033FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh033FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-033 check(1): %s", reason)
	}
	t.Logf("SH-033 heading found at specs/scenario-harness.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		// Sub-check (2): SIGINT cited.
		{
			id:     "2",
			label:  "sigint-cited",
			needle: "SIGINT",
			detail: "SH-033 body must cite SIGINT (expected token 'SIGINT'); the harness MUST handle " +
				"the SIGINT signal for operator-interrupt — without this cite the graceful-shutdown " +
				"trigger for SIGINT is unspecified and implementors may omit it",
		},
		// Sub-check (3): SIGTERM cited.
		{
			id:     "3",
			label:  "sigterm-cited",
			needle: "SIGTERM",
			detail: "SH-033 body must cite SIGTERM (expected token 'SIGTERM'); the harness MUST handle " +
				"the SIGTERM signal for graceful-stop — without this cite the SIGTERM path is " +
				"unspecified and implementors may conflate it with SIGKILL or ignore it entirely",
		},
		// Sub-check (4): SH-026 cancel chain cited.
		{
			id:     "4",
			label:  "cancel-via-sh026",
			needle: "SH-026",
			detail: "SH-033 body must cite SH-026 for the cancel-current-scenario step " +
				"(expected token 'SH-026'); SH-026 defines the cancel chain (daemon stop RPC + " +
				"bounded teardown) — SH-033 MUST invoke that same chain rather than an ad-hoc " +
				"signal-kill path, so that the cancellation bounds (N×HC-018 + ON-029) apply on " +
				"operator interrupt just as on timeout",
		},
		// Sub-check (5): verdict=error declared.
		{
			id:     "5",
			label:  "verdict-error-declared",
			needle: "verdict=error",
			detail: "SH-033 body must declare 'verdict=error' for the interrupted scenario " +
				"(expected token 'verdict=error'); operator interrupt is treated as a " +
				"timeout-equivalent harness-internal condition, not a test assertion failure " +
				"(verdict=fail) or a normal timeout (verdict=timeout) — the 'error' verdict " +
				"class is the correct classification per §8.7",
		},
		// Sub-check (6): failure_class=harness-internal-error declared.
		{
			id:     "6",
			label:  "failure-class-harness-internal-error",
			needle: "harness-internal-error",
			detail: "SH-033 body must declare 'failure_class=harness-internal-error' for the " +
				"interrupted scenario (expected token 'harness-internal-error'); §8.7 defines " +
				"this class for operator-interrupt (SH-033 item v in the closed detection list); " +
				"without this cite the interrupted scenario's failure class is unspecified and " +
				"parsers may misclassify the result",
		},
		// Sub-check (7): SH-015 teardown cited.
		{
			id:     "7",
			label:  "sh015-teardown-cited",
			needle: "SH-015",
			detail: "SH-033 body must cite SH-015 for fixture teardown in the graceful-shutdown " +
				"sequence (expected token 'SH-015'); the teardown sub-steps (handler cleanup, " +
				"lease release, event-log close, daemon stop) MUST execute on signal-driven " +
				"shutdown just as on normal termination — SH-015 is normative for this",
		},
		// Sub-check (8a): SuiteResult written on shutdown.
		{
			id:     "8a",
			label:  "partial-suite-result-written",
			needle: "SuiteResult",
			detail: "SH-033 body must declare a partial 'SuiteResult' is written on signal " +
				"(expected token 'SuiteResult'); the partial result must include completed " +
				"scenarios plus the interrupted scenario's error verdict so that callers of " +
				"the harness binary can parse the output regardless of whether the run was " +
				"interrupted or completed normally",
		},
		// Sub-check (8b): written to stdout.
		{
			id:     "8b",
			label:  "result-written-to-stdout",
			needle: "stdout",
			detail: "SH-033 body must declare the partial SuiteResult is written to stdout " +
				"(expected token 'stdout'); the harness writes results to stdout by convention " +
				"(per SH-032 CLI surface); the shutdown path MUST use the same channel so that " +
				"a parent process reading stdout receives the partial result even on interrupt",
		},
		// Sub-check (9): exit code 130 for SIGINT.
		{
			id:     "9",
			label:  "exit-code-130-sigint",
			needle: "130",
			detail: "SH-033 body must declare exit code 130 for SIGINT (expected token '130'); " +
				"exit code 130 is the POSIX convention for SIGINT-terminated processes (128+2) " +
				"and is declared in the §4.12 exit-code table; the harness MUST exit with 130 " +
				"on SIGINT so that orchestrating scripts can distinguish operator interrupt from " +
				"test failure (exit 1) or timeout (exit 124 or similar)",
		},
		// Sub-check (10): exit code 143 for SIGTERM.
		{
			id:     "10",
			label:  "exit-code-143-sigterm",
			needle: "143",
			detail: "SH-033 body must declare exit code 143 for SIGTERM (expected token '143'); " +
				"exit code 143 is the POSIX convention for SIGTERM-terminated processes (128+15); " +
				"the harness MUST exit with 143 on SIGTERM so that orchestrating scripts can " +
				"distinguish graceful-stop from operator interrupt (130) or abnormal exit",
		},
		// Sub-check (11): second SIGINT → immediate exit.
		{
			id:     "11",
			label:  "second-sigint-immediate-exit",
			needle: "second SIGINT",
			detail: "SH-033 body must declare that a second SIGINT during graceful shutdown causes " +
				"immediate exit (expected phrase 'second SIGINT'); operator-driven escalation " +
				"overrides the cleanup invariant — without this clause the harness would block " +
				"indefinitely if teardown stalls and the operator sends a second SIGINT expecting " +
				"the process to die immediately",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, "-", "_")), func(t *testing.T) {
			t.Parallel()
			if !sh033FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-033 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-033 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (12): Tags: mechanism in SH-033 body.
	t.Run("check-12-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh033FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-033 check(12) FAILED: Tags: mechanism not found in SH-033 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-033 body)\n"+
					"  detail: SH-033 carries tag 'mechanism' per the scenario-harness component "+
					"matrix (signal_handling); its absence indicates the requirement body has been "+
					"truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-i0tw.35 audit complete — SH-033 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
