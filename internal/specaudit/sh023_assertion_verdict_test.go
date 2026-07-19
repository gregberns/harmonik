//go:build specaudit

package specaudit_test

// hk-i0tw.25 binding test — SH-023: assertion failure produces verdict `fail`
// with `failure_class=assertion-failed`.
//
// Spec ref: specs/scenario-harness.md §4.6 SH-023.
//
// SH-023 states: when any declared assertion fails to hold, the harness MUST set
// `ScenarioResult.verdict=fail` and `failure_class=assertion-failed`. The result
// MUST carry one `AssertionResult` record per declared expectation (§6.1),
// distinguishing which assertions passed and which failed. The harness MUST NOT
// short-circuit on first assertion failure — every declared assertion MUST be
// evaluated so the operator sees the full failure picture. Even on `verdict=timeout`
// (per SH-026) the harness MUST evaluate all declared assertions best-effort against
// the partial event log (assertions whose evaluation is impossible due to missing data
// are recorded with `passed=false` and an explanatory `error_detail`-style note); the
// verdict remains `timeout`. Full evaluation is a deliberate cost-vs-debuggability
// tradeoff per spec §4.6 RATIONALE.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The harness implementation is pending; this
// sensor verifies that SH-023 is correctly declared in the spec so that when the
// harness is implemented it has a binding spec to conform to. All closed sibling beads
// for the same spec section (hk-i0tw.28/SH-026, hk-i0tw.15/SH-015) are corpus sensors;
// the correct form here is the same.
//
// Sub-checks verified:
//
//  1. SH-023 heading present — "#### SH-023 —".
//  2. (a) verdict=`fail` declared on assertion failure.
//  3. (a) failure_class=`assertion-failed` declared on assertion failure.
//  4. (b) One AssertionResult per declared expectation — §6.1 cited.
//  5. (b) No short-circuit on first failure — "MUST NOT short-circuit" present.
//  6. (c) Full failure picture — "full failure picture" or "full failure" present.
//  7. (c) Best-effort evaluation on timeout — SH-026 cross-reference present.
//  8. (c) Impossible-to-evaluate assertions recorded with passed=false and note.
//  9. Verdict remains `timeout` when timeout co-occurs with assertion failure.
//  10. Tags: mechanism present in the SH-023 body window.
//
// # Decision rationale (corpus-search sensor vs. executable harness fixture)
//
// The harness implementation is pending — there is no Go runtime surface to invoke
// the actual assertion-evaluation path against. An executable fixture would require
// a live orchestration drive with real event-log capture and assertion resolution;
// premature at this phase. The corpus sensor asserts the spec body declares all
// normative requirements (verdict classification, no-short-circuit contract, per-assertion
// record, timeout-coexistence rule) so that when the harness is implemented it has a
// complete spec to conform to.
//
// # Failure modes
//
//   - SH-023 heading missing.
//   - verdict=`fail` not declared.
//   - failure_class=`assertion-failed` not declared.
//   - §6.1 AssertionResult-per-expectation citation absent.
//   - No-short-circuit mandate missing ("MUST NOT short-circuit" absent).
//   - Full failure picture language absent ("full failure").
//   - SH-026 cross-reference for timeout best-effort evaluation absent.
//   - passed=false + explanatory note for impossible evaluations absent.
//   - verdict-remains-timeout on co-occurrence not declared.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sh023Fixture prefix per
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

// sh023FixtureScenarioHarnessPath returns the absolute path to specs/scenario-harness.md.
func sh023FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh023FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// sh023FixtureLoadLines opens specFile and returns all lines.
func sh023FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh023FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh023FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sh023FixtureSH023Heading matches the SH-023 level-4 requirement heading.
var sh023FixtureSH023Heading = regexp.MustCompile(`^#### SH-023 —`)

// sh023FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var sh023FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sh023FixtureTagsMechanism matches a "Tags: mechanism" line.
var sh023FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// sh023FixtureBodyWindow is the maximum number of lines to scan after the heading.
// SH-023's body is a dense paragraph with RATIONALE block; 30 lines is sufficient.
const sh023FixtureBodyWindow = 30

// sh023FixtureBodyLines returns the lines comprising the SH-023 body, up to the next
// heading or the body-window limit (whichever comes first).
func sh023FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh023FixtureSH023Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-023 heading not found; expected '#### SH-023 —' in specs/scenario-harness.md"
	}

	limit := headingIdx + 1 + sh023FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if sh023FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// sh023FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func sh023FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH023AssertionVerdictAndNoShortCircuit is the binding test for hk-i0tw.25.
func TestSH023AssertionVerdictAndNoShortCircuit(t *testing.T) {
	t.Parallel()

	specFile := sh023FixtureScenarioHarnessPath(t)
	lines := sh023FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh023FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-023 check(1): %s", reason)
	}
	t.Logf("SH-023 heading found at specs/scenario-harness.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		// Sub-check (a): verdict=fail + failure_class=assertion-failed on assertion failure.
		{
			id:     "a1",
			label:  "verdict-fail-declared",
			needle: "verdict=fail",
			detail: "SH-023 body must declare 'verdict=fail' as the ScenarioResult verdict when any " +
				"declared assertion fails to hold; this is the terminal classification that " +
				"distinguishes assertion failure from timeout (verdict=timeout) or harness " +
				"error (verdict=error)",
		},
		{
			id:     "a2",
			label:  "failure-class-assertion-failed-declared",
			needle: "assertion-failed",
			detail: "SH-023 body must declare 'failure_class=assertion-failed' as the failure class on " +
				"assertion failure (expected token 'assertion-failed'); §8.5 defines this class — " +
				"it is the observational class where the run completed cleanly but ≥1 declared " +
				"assertion evaluated to passed=false",
		},
		// Sub-check (b): one AssertionResult per declared expectation + no short-circuit.
		{
			id:     "b1",
			label:  "assertion-result-per-expectation-section6-1",
			needle: "§6.1",
			detail: "SH-023 body must cite §6.1 for the one-AssertionResult-per-declared-expectation " +
				"requirement (expected token '§6.1'); §6.1 declares the AssertionResult record shape " +
				"and the assertion_results list contract; the citation is normative because it pins " +
				"the record schema the harness must populate",
		},
		{
			id:     "b2",
			label:  "no-short-circuit-must-not",
			needle: "MUST NOT short-circuit",
			detail: "SH-023 body must state 'MUST NOT short-circuit' as the no-short-circuit mandate " +
				"(expected token 'MUST NOT short-circuit'); the harness must evaluate every declared " +
				"assertion even after the first failure so the operator sees the full failure picture — " +
				"short-circuiting would hide subsequent failures and require re-running to discover them",
		},
		// Sub-check (c): full failure picture + timeout best-effort evaluation.
		{
			id:     "c1",
			label:  "full-failure-picture",
			needle: "full failure picture",
			detail: "SH-023 body must include 'full failure picture' describing the debuggability goal " +
				"of the no-short-circuit rule (expected token 'full failure picture'); this phrase " +
				"is the operator-facing rationale: the operator sees all assertion outcomes in one run, " +
				"not one-at-a-time across multiple re-runs",
		},
		{
			id:     "c2",
			label:  "timeout-best-effort-sh026-crossref",
			needle: "SH-026",
			detail: "SH-023 body must cross-reference SH-026 for the timeout-coexistence rule " +
				"(expected token 'SH-026'); even on verdict=timeout the harness must evaluate all " +
				"declared assertions best-effort against the partial event log — SH-026 is the spec " +
				"requirement that defines the timeout path the cross-reference pins",
		},
		{
			id:     "c3",
			label:  "impossible-eval-passed-false-with-note",
			needle: "passed=false",
			detail: "SH-023 body must declare that assertions impossible to evaluate due to missing data " +
				"are recorded with 'passed=false' and an explanatory note (expected token 'passed=false'); " +
				"on timeout the partial event log may not contain enough data to evaluate every assertion — " +
				"recording passed=false with a note preserves the full assertion_results list contract " +
				"while signaling to the operator which assertions could not be resolved",
		},
		// Sub-check (d): verdict-remains-timeout on co-occurrence.
		{
			id:     "d1",
			label:  "verdict-remains-timeout-on-cooccurrence",
			needle: "verdict remains",
			detail: "SH-023 body must state that the verdict remains 'timeout' when timeout co-occurs " +
				"with assertion failure (expected token 'verdict remains'); per §8.0 precedence table " +
				"scenario-timeout supersedes assertion-failed — without this declaration an implementer " +
				"might incorrectly downgrade a timeout to fail because assertions were also evaluated",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, "-", "_")), func(t *testing.T) {
			t.Parallel()
			if !sh023FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-023 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-023 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (e): Tags: mechanism in SH-023 body.
	t.Run("check-e-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh023FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-023 check(e) FAILED: Tags: mechanism not found in SH-023 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-023 body)\n"+
					"  detail: SH-023 carries tag 'mechanism' per the component-matrix "+
					"(evaluate_assertions); its absence indicates the requirement body "+
					"has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-i0tw.25 audit complete — SH-023 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
