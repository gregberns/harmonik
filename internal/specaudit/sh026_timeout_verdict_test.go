package specaudit_test

// hk-i0tw.28 binding test — SH-026: timeout exceedance produces verdict `timeout`
// with `failure_class=scenario-timeout`.
//
// Spec ref: specs/scenario-harness.md §4.7 SH-026.
//
// SH-026 states: when a scenario's orchestration drive does not reach a terminal
// state within `timeout_secs` (measured per SH-025 from fixture-setup completion to
// the orchestrator emitting a terminal `run_completed`/`run_failed` event), the harness
// MUST cancel the orchestration by invoking the per-scenario daemon's `stop` RPC of
// [process-lifecycle.md §4.2.PL-003a] (graceful drain, escalating to SIGKILL of agent
// subprocesses on drain-timeout per [operator-nfr.md §4.7 ON-029]). Because each
// scenario runs against its own per-scenario daemon (per SH-016a), `daemon stop` halts
// the scenario's run cleanly without affecting any other scenario. The harness MUST then
// execute fixture teardown per SH-015 and emit a `ScenarioResult` with `verdict=timeout`
// and `failure_class=scenario-timeout`. Per-handler cancellation MUST honor the
// bounded-cancellation contract of [handler-contract.md §4.4.HC-018]; the harness's
// overall cancel-and-teardown wall-clock bound is `(N × HC-018-ceiling) + ON-029-drain-timeout`,
// where N is the live-handler count at timeout.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The harness implementation is pending; this
// sensor verifies that SH-026 is correctly declared in the spec so that when the
// harness is implemented it has a binding spec to conform to. All closed sibling beads
// for the same spec section (hk-i0tw.15/SH-015, hk-i0tw.27/SH-025) are corpus sensors;
// the correct form here is the same.
//
// Sub-checks verified:
//
//  1. SH-026 heading present — "#### SH-026 —".
//  2. (a) verdict=`timeout` declared on timeout exceedance.
//  3. (a) failure_class=`scenario-timeout` declared on timeout exceedance.
//  4. (b) Cancel chain cites PL-003a `stop` RPC.
//  5. (b) Cancel chain cites ON-029 drain-timeout (via operator-nfr.md §4.7 citation).
//  6. (b) Per-handler cancellation cites HC-018 bounded-cancellation contract.
//  7. (c) Cancel-and-teardown bound formula present: `(N × HC-018-ceiling) + ON-029-drain-timeout`
//         (checked as two tokens: "HC-018-ceiling" and "ON-029-drain-timeout").
//  8. Per-scenario daemon isolation (SH-016a) cited — `daemon stop` does not affect other scenarios.
//  9. Fixture teardown (SH-015) cited as the step after cancellation.
//  10. Tags: mechanism present in the SH-026 body window.
//
// # Decision rationale (corpus-search sensor vs. executable harness fixture)
//
// The harness implementation is pending — there is no Go runtime surface to invoke
// the actual timeout-cancellation path against. An executable fixture would require
// spawning daemon processes and timing out real orchestration drives; premature at this
// phase. The corpus sensor asserts the spec body declares all normative requirements
// (cancel chain references, verdict classification, bound formula) so that when the
// harness is implemented it has a complete spec to conform to.
//
// # Failure modes
//
//   - SH-026 heading missing.
//   - verdict=`timeout` not declared.
//   - failure_class=`scenario-timeout` not declared.
//   - PL-003a stop RPC not cited in cancel chain.
//   - ON-029 / operator-nfr.md drain-timeout not cited.
//   - HC-018 bounded-cancellation not cited.
//   - Cancel-and-teardown bound formula missing ("HC-018-ceiling" or "ON-029-drain-timeout" absent).
//   - SH-016a per-scenario isolation not cited.
//   - SH-015 teardown reference absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sh026Fixture prefix per
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

// sh026FixtureScenarioHarnessPath returns the absolute path to specs/scenario-harness.md.
func sh026FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh026FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// sh026FixtureLoadLines opens specFile and returns all lines.
func sh026FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh026FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh026FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sh026FixtureSH026Heading matches the SH-026 level-4 requirement heading.
var sh026FixtureSH026Heading = regexp.MustCompile(`^#### SH-026 —`)

// sh026FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var sh026FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sh026FixtureTagsMechanism matches a "Tags: mechanism" line.
var sh026FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// sh026FixtureBodyWindow is the maximum number of lines to scan after the heading.
// SH-026's body is a dense single paragraph; 30 lines is sufficient.
const sh026FixtureBodyWindow = 30

// sh026FixtureBodyLines returns the lines comprising the SH-026 body, up to the next
// heading or the body-window limit (whichever comes first).
func sh026FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh026FixtureSH026Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-026 heading not found; expected '#### SH-026 —' in specs/scenario-harness.md"
	}

	limit := headingIdx + 1 + sh026FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if sh026FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// sh026FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func sh026FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH026TimeoutVerdictAndCancelChain is the binding test for hk-i0tw.28.
func TestSH026TimeoutVerdictAndCancelChain(t *testing.T) {
	t.Parallel()

	specFile := sh026FixtureScenarioHarnessPath(t)
	lines := sh026FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh026FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-026 check(1): %s", reason)
	}
	t.Logf("SH-026 heading found at specs/scenario-harness.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		// Sub-check (a): verdict=timeout + failure_class=scenario-timeout on exceedance.
		{
			id:     "a1",
			label:  "verdict-timeout-declared",
			needle: "verdict=timeout",
			detail: "SH-026 body must declare 'verdict=timeout' as the ScenarioResult verdict when the " +
				"scenario exceeds its timeout_secs budget; this is the terminal classification that " +
				"distinguishes timeout exceedance from assertion failure (verdict=fail) or harness " +
				"error (verdict=error)",
		},
		{
			id:     "a2",
			label:  "failure-class-scenario-timeout-declared",
			needle: "scenario-timeout",
			detail: "SH-026 body must declare 'failure_class=scenario-timeout' as the failure class on " +
				"timeout exceedance (expected token 'scenario-timeout'); §8.6 defines this class — " +
				"it supersedes assertion outcomes because a timeout means assertions are evaluated " +
				"on partial data only",
		},
		// Sub-check (b): cancel chain references PL-003a + ON-029 + HC-018.
		{
			id:     "b1",
			label:  "cancel-chain-pl003a-stop-rpc",
			needle: "PL-003a",
			detail: "SH-026 body must cite PL-003a for the cancel chain's daemon stop RPC " +
				"(expected token 'PL-003a'); process-lifecycle.md §4.2.PL-003a defines the " +
				"graceful `daemon stop` RPC that initiates orchestration cancellation on timeout — " +
				"the harness MUST invoke this surface, not an ad-hoc signal",
		},
		{
			id:     "b2",
			label:  "cancel-chain-on029-drain-timeout",
			needle: "operator-nfr.md",
			detail: "SH-026 body must cite operator-nfr.md (ON-029 drain-timeout) in the cancel chain " +
				"(expected token 'operator-nfr.md'); the spec body uses the doc-link form " +
				"'[operator-nfr.md §4.7 ON-029]' — spec content wins per implementer-protocol " +
				"path-discrepancy rule; this bounds the graceful-drain wait before SIGKILL escalation",
		},
		{
			id:     "b3",
			label:  "cancel-chain-hc018-bounded-cancellation",
			needle: "HC-018",
			detail: "SH-026 body must cite HC-018 bounded-cancellation for per-handler cancellation " +
				"(expected token 'HC-018'); handler-contract.md §4.4.HC-018 defines the per-handler " +
				"T_cancel ceiling — each handler's cancellation is bounded individually before the " +
				"overall teardown bound is computed",
		},
		// Sub-check (c): cancel-and-teardown bound formula present.
		{
			id:     "c1",
			label:  "bound-formula-hc018-ceiling",
			needle: "HC-018-ceiling",
			detail: "SH-026 body must include 'HC-018-ceiling' in the cancel-and-teardown bound formula " +
				"(expected token 'HC-018-ceiling'); the formula is '(N × HC-018-ceiling) + ON-029-drain-timeout' " +
				"where N is the live-handler count at timeout — the HC-018-ceiling factor is the per-handler " +
				"bound; without it the formula is incomplete and the overall wall-clock bound is unspecified",
		},
		{
			id:     "c2",
			label:  "bound-formula-on029-drain-timeout",
			needle: "ON-029-drain-timeout",
			detail: "SH-026 body must include 'ON-029-drain-timeout' in the cancel-and-teardown bound formula " +
				"(expected token 'ON-029-drain-timeout'); the formula is '(N × HC-018-ceiling) + ON-029-drain-timeout' " +
				"— this additive term is the daemon's graceful-drain ceiling; without it the formula is " +
				"incomplete and the harness cannot bound its total cancellation wall-clock",
		},
		// Per-scenario isolation and teardown references.
		{
			id:     "d1",
			label:  "per-scenario-isolation-sh016a",
			needle: "SH-016a",
			detail: "SH-026 body must cite SH-016a for per-scenario daemon isolation " +
				"(expected token 'SH-016a'); because each scenario runs against its own per-scenario " +
				"daemon, `daemon stop` halts the scenario's run without affecting any other scenario — " +
				"this isolation property is normative and must appear in the spec body",
		},
		{
			id:     "d2",
			label:  "fixture-teardown-sh015-cited",
			needle: "SH-015",
			detail: "SH-026 body must cite SH-015 for fixture teardown post-cancellation " +
				"(expected token 'SH-015'); the cancel-and-teardown sequence is: stop RPC → " +
				"fixture teardown (SH-015) → emit ScenarioResult — the teardown reference is " +
				"normative because it defines the sub-steps (handler cleanup, lease release, " +
				"event-log close, daemon stop) that must follow the timeout cancellation",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, "-", "_")), func(t *testing.T) {
			t.Parallel()
			if !sh026FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-026 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-026 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (e): Tags: mechanism in SH-026 body.
	t.Run("check-e-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh026FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-026 check(e) FAILED: Tags: mechanism not found in SH-026 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-026 body)\n"+
					"  detail: SH-026 carries tag 'mechanism' per the component-matrix "+
					"(enforce_scenario_timeout); its absence indicates the requirement body "+
					"has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-i0tw.28 audit complete — SH-026 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
