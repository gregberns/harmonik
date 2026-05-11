package specaudit_test

// hk-i0tw.15 binding test — SH-015: fixture teardown runs on every terminal path
// (5-step ordered, idempotent).
//
// Spec ref: specs/scenario-harness.md §4.4 SH-015.
//
// SH-015 states: the harness MUST execute fixture teardown on every terminal scenario
// path (pass, fail, timeout, error). Teardown is run-to-completion best-effort: a
// failure in any sub-step MUST NOT halt remaining sub-steps; all errors accumulated.
// Sub-steps in order:
//
//	(a) terminate still-live handler subprocesses honoring HC-018 cancellation bounds;
//	(b) release worktree leases per WM-013b;
//	(c) close event-log file (fsync then close);
//	(d) stop per-scenario daemon via `daemon stop` RPC of PL-003a (drain bounded by ON-029);
//	(e) record workspace_snapshot_path (recording obligation, NOT termination) — see SH-015a.
//
// Teardown is idempotent. On any sub-step failure → cleanup-failed per §8.8;
// verdict-downgrade follows §8.0 precedence table.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The harness implementation is pending; this
// sensor verifies that SH-015 is correctly declared in the spec so that:
//
//  1. SH-015 heading is present in specs/scenario-harness.md.
//  2. "every terminal scenario path" (or equivalent all-paths coverage) is declared.
//  3. "run-to-completion" best-effort declared — sub-step failure MUST NOT halt others.
//  4. Sub-step (a): HC-018 cancellation bounds cited.
//  5. Sub-step (b): WM-013b worktree lease release cited.
//  6. Sub-step (c): event-log fsync then close declared.
//  7. Sub-step (d): PL-003a `daemon stop` RPC cited.
//  8. Sub-step (d): ON-029 drain-timeout bound cited.
//  9. Sub-step (e): workspace_snapshot_path recording obligation declared.
//  10. "idempotent" (or "idempotent") declared.
//  11. "cleanup-failed" failure class cited.
//  12. §8.0 precedence table cited.
//  13. Tags: mechanism present in the SH-015 body window.
//
// # Decision rationale (corpus-search sensor vs. executable harness fixture)
//
// The harness implementation is pending — there is no Go runtime surface to invoke
// the actual 5-step teardown against. All closed sibling beads for the same spec
// section (hk-i0tw.16/SH-015a, hk-i0tw.24/SH-022, hk-i0tw.26/SH-024) are corpus
// sensors. The correct form here is the same: assert that the spec body declares all
// five sub-steps and their cross-references (HC-018, WM-013b, PL-003a, ON-029,
// SH-015a) so that when the harness is implemented it has a binding spec to conform
// to. An executable fixture would require spawning daemon processes not yet wired
// into the test harness; premature at this phase. When hk-i0tw.38 (workspace reset
// sensor) and hk-i0tw.33 (sequential execution) are implemented, an executable
// conformance layer can layer on top.
//
// # Failure modes
//
//   - SH-015 heading missing.
//   - All-paths coverage absent (pass/fail/timeout/error).
//   - Run-to-completion / best-effort absent.
//   - HC-018 cancellation bounds absent.
//   - WM-013b lease release absent.
//   - Event-log fsync absent.
//   - PL-003a daemon stop RPC absent.
//   - ON-029 drain-timeout absent.
//   - workspace_snapshot_path absent.
//   - Idempotent absent.
//   - cleanup-failed absent.
//   - §8.0 precedence absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sh015Fixture prefix per
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

// sh015FixtureScenarioHarnessPath returns the absolute path to specs/scenario-harness.md.
func sh015FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh015FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// sh015FixtureLoadLines opens specFile and returns all lines.
func sh015FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh015FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh015FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sh015FixtureSH015Heading matches the SH-015 level-4 requirement heading (not SH-015a).
var sh015FixtureSH015Heading = regexp.MustCompile(`^#### SH-015 —`)

// sh015FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var sh015FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sh015FixtureTagsMechanism matches a "Tags: mechanism" line.
var sh015FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// sh015FixtureBodyWindow is the maximum number of lines to scan after the heading.
// SH-015's body is a dense single paragraph; 20 lines is sufficient.
const sh015FixtureBodyWindow = 20

// sh015FixtureBodyLines returns the lines comprising the SH-015 body, up to the next heading
// or the body-window limit (whichever comes first).
func sh015FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh015FixtureSH015Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-015 heading not found; expected '#### SH-015 —' in specs/scenario-harness.md"
	}

	limit := headingIdx + 1 + sh015FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if sh015FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// sh015FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func sh015FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH015FixtureTeardownOrderedIdempotent is the binding test for hk-i0tw.15.
func TestSH015FixtureTeardownOrderedIdempotent(t *testing.T) {
	t.Parallel()

	specFile := sh015FixtureScenarioHarnessPath(t)
	lines := sh015FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh015FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-015 check(1): %s", reason)
	}
	t.Logf("SH-015 heading found at specs/scenario-harness.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "2",
			label:  "all-paths-coverage",
			needle: "every terminal scenario path",
			detail: "SH-015 body must declare teardown runs on 'every terminal scenario path' " +
				"(expected phrase 'every terminal scenario path'); this is the load-bearing " +
				"coverage invariant — pass, fail, timeout, and error paths all require teardown",
		},
		{
			id:     "3",
			label:  "run-to-completion-best-effort",
			needle: "run-to-completion",
			detail: "SH-015 body must declare 'run-to-completion' best-effort discipline " +
				"(expected phrase 'run-to-completion'); a sub-step failure MUST NOT halt the " +
				"remaining sub-steps — all errors are accumulated and all sub-steps attempted",
		},
		{
			id:     "4",
			label:  "substep-a-hc018-cancellation-bounds",
			needle: "HC-018",
			detail: "SH-015 body must cite HC-018 cancellation bounds for sub-step (a) " +
				"(expected token 'HC-018'); handler-contract.md §4.4.HC-018 defines the " +
				"per-handler T_cancel ceiling that bounds subprocess termination wall-clock",
		},
		{
			id:     "5",
			label:  "substep-b-wm013b-lease-release",
			needle: "WM-013b",
			detail: "SH-015 body must cite WM-013b for sub-step (b) worktree lease release " +
				"(expected token 'WM-013b'); workspace-model.md §4.3.WM-013b defines the " +
				"lease release on terminal transitions contract",
		},
		{
			id:     "6",
			label:  "substep-c-eventlog-fsync",
			needle: "fsync",
			detail: "SH-015 body must declare event-log fsync for sub-step (c) " +
				"(expected token 'fsync'); the event-log close sequence is fsync-then-close — " +
				"fsync ensures durability before the file descriptor is released",
		},
		{
			id:     "7",
			label:  "substep-d-pl003a-daemon-stop",
			needle: "PL-003a",
			detail: "SH-015 body must cite PL-003a for sub-step (d) daemon stop RPC " +
				"(expected token 'PL-003a'); process-lifecycle.md §4.2.PL-003a defines the " +
				"`daemon stop` RPC that initiates graceful drain of the per-scenario daemon",
		},
		{
			id:     "8",
			label:  "substep-d-operator-nfr-drain-timeout",
			needle: "operator-nfr.md",
			detail: "SH-015 body must cite operator-nfr.md for the drain-timeout bound in sub-step (d) " +
				"(expected token 'operator-nfr.md'); the spec cites '[operator-nfr.md §4.7]' (ON-029) " +
				"as the bound on how long the harness waits for the daemon's graceful drain to complete. " +
				"Note: the bead description references 'ON-029' by identifier, but the spec body uses the " +
				"doc-link form '[operator-nfr.md §4.7]'; spec content wins per implementer-protocol path-discrepancy rule",
		},
		{
			id:     "9",
			label:  "substep-e-workspace-snapshot-path",
			needle: "workspace_snapshot_path",
			detail: "SH-015 body must declare workspace_snapshot_path recording obligation for sub-step (e) " +
				"(expected token 'workspace_snapshot_path'); sub-step (e) is a recording obligation, " +
				"NOT a termination action — it points at the in-place worktree per SH-015a",
		},
		{
			id:     "10",
			label:  "idempotent",
			needle: "idempotent",
			detail: "SH-015 body must declare teardown is 'idempotent' " +
				"(expected token 'idempotent'); calling teardown twice MUST be a no-op on " +
				"already-completed sub-steps — this property is required by the §7.1 lifecycle",
		},
		{
			id:     "11",
			label:  "cleanup-failed-class",
			needle: "cleanup-failed",
			detail: "SH-015 body must cite 'cleanup-failed' as the failure class for sub-step failures " +
				"(expected token 'cleanup-failed'); §8.8 defines this class and §8.0 defines its " +
				"precedence — lowest priority, so it never overwrites a prior verdict",
		},
		{
			id:     "12",
			label:  "section-8-0-precedence-table",
			needle: "§8.0",
			detail: "SH-015 body must cite §8.0 precedence table for verdict-downgrade resolution " +
				"(expected token '§8.0'); the precedence table governs how cleanup-failed interacts " +
				"with prior fail/timeout/pass verdicts — cleanup-failed is always lowest priority",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !sh015FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-015 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-015 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (13): Tags: mechanism in SH-015 body.
	t.Run("check-13-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh015FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-015 check(13) FAILED: Tags: mechanism not found in SH-015 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-015 body)\n"+
					"  detail: SH-015 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-i0tw.15 audit complete — SH-015 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
