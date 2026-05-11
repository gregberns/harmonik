package specaudit_test

// hk-i0tw.33 binding test — SH-031: scenarios run sequentially at MVH.
//
// Spec ref: specs/scenario-harness.md §4.11 SH-031.
//
// SH-031 states: the harness MUST execute scenarios sequentially at MVH — at most
// one scenario's full lifecycle (per SH-016a synthetic-project-root creation through
// SH-015 fixture teardown completion) may be active at any time. "Active" means:
// between the start of a scenario's fixture-up and the completion of that scenario's
// teardown sub-step (e), no other scenario MAY have any sub-step running. Pre-fetching
// scenario N+1's fixture state in parallel with scenario N's teardown is forbidden at
// v0.1. Concurrent multi-scenario execution is a post-MVH concern tracked at OQ-SH-002.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The harness implementation is pending; this
// sensor verifies that SH-031 is correctly declared in the spec so that:
//
//  1. SH-031 heading is present in specs/scenario-harness.md.
//  2. "sequential" declared (harness MUST execute scenarios sequentially at MVH).
//  3. "at most one scenario" active window declared.
//  4. Active-window start anchored to SH-016a (synthetic-project-root creation).
//  5. Active-window end anchored to SH-015 (fixture teardown completion / sub-step (e)).
//  6. Pre-fetching scenario N+1 in parallel with N's teardown forbidden at v0.1.
//  7. OQ-SH-002 cited as the post-MVH concurrent-execution open question.
//  8. Tags: mechanism present within 30 lines of heading.
//
// # Decision rationale (corpus-search sensor vs. executable harness fixture)
//
// The harness implementation is pending — there is no Go runtime surface to enforce
// sequential execution at this phase. All sibling beads for this spec section
// (hk-i0tw.15/SH-015, hk-i0tw.18/SH-016a) are corpus sensors. The correct form here
// is the same: assert that the spec body declares the sequential-execution mandate and
// all of its cross-references (SH-016a, SH-015, OQ-SH-002) so that when the harness is
// implemented it has a binding spec to conform to. An executable concurrency test would
// require spawning multiple full lifecycle runs; premature at this phase.
//
// # Failure modes
//
//   - SH-031 heading missing.
//   - "sequential" (or equivalent) absent — mandatory execution order not declared.
//   - "at most one scenario" absent — the active-window cardinality constraint missing.
//   - SH-016a absent — active-window start anchor missing.
//   - SH-015 absent — active-window end anchor missing.
//   - Pre-fetching forbidden at v0.1 not declared.
//   - OQ-SH-002 absent — post-MVH open question not cited.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sh031Fixture prefix per
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

// sh031FixtureScenarioHarnessPath returns the absolute path to specs/scenario-harness.md.
func sh031FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh031FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// sh031FixtureLoadLines opens specFile and returns all lines.
func sh031FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh031FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh031FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sh031FixtureSH031Heading matches the SH-031 level-4 requirement heading.
var sh031FixtureSH031Heading = regexp.MustCompile(`^#### SH-031 —`)

// sh031FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var sh031FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sh031FixtureTagsMechanism matches a "Tags: mechanism" line.
var sh031FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// sh031FixtureBodyWindow is the maximum number of lines to scan after the heading.
// SH-031's body is a single dense paragraph followed by a Tags line; 30 lines is sufficient.
const sh031FixtureBodyWindow = 30

// sh031FixtureBodyLines returns the lines comprising the SH-031 body, up to the next heading
// or the body-window limit (whichever comes first).
func sh031FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh031FixtureSH031Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-031 heading not found; expected '#### SH-031 —' in specs/scenario-harness.md"
	}

	limit := headingIdx + 1 + sh031FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if sh031FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// sh031FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func sh031FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH031SequentialScenarios is the binding test for hk-i0tw.33.
func TestSH031SequentialScenarios(t *testing.T) {
	t.Parallel()

	specFile := sh031FixtureScenarioHarnessPath(t)
	lines := sh031FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh031FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-031 check(1): %s", reason)
	}
	t.Logf("SH-031 heading found at specs/scenario-harness.md line %d; body window = %d lines",
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
			label:  "sequential-declared",
			needle: "sequential",
			detail: "SH-031 body must declare 'sequential' execution (expected token 'sequential'); " +
				"the harness MUST execute scenarios sequentially at MVH — this is the load-bearing " +
				"concurrency mandate, not a recommendation",
		},
		{
			id:     "3",
			label:  "at-most-one-scenario-active",
			needle: "at most one scenario",
			detail: "SH-031 body must declare 'at most one scenario' active at a time " +
				"(expected phrase 'at most one scenario'); this is the cardinality constraint that " +
				"defines the active-window policy for the sequential execution mandate",
		},
		{
			id:     "4",
			label:  "active-window-start-sh016a",
			needle: "SH-016a",
			detail: "SH-031 body must anchor the active-window start to SH-016a " +
				"(expected token 'SH-016a'); SH-016a defines synthetic-project-root creation, " +
				"which is the start of the active window for a scenario's lifecycle",
		},
		{
			id:     "5",
			label:  "active-window-end-sh015",
			needle: "SH-015",
			detail: "SH-031 body must anchor the active-window end to SH-015 " +
				"(expected token 'SH-015'); SH-015 defines fixture teardown — specifically, " +
				"sub-step (e) completion marks the end of the active window",
		},
		{
			id:     "6",
			label:  "prefetch-forbidden-v0-1",
			needle: "v0.1",
			detail: "SH-031 body must declare pre-fetching scenario N+1 forbidden at v0.1 " +
				"(expected token 'v0.1'); the version-scoped prohibition on pre-fetching " +
				"N+1 fixture state during N's teardown is normative at v0.1 and must be " +
				"explicitly stated so that the v0.2 revision (if any) can relax it deliberately",
		},
		{
			id:     "7",
			label:  "oq-sh-002-post-mvh",
			needle: "OQ-SH-002",
			detail: "SH-031 body must cite OQ-SH-002 as the post-MVH open question for concurrent " +
				"multi-scenario execution (expected token 'OQ-SH-002'); the open question tracks " +
				"the trigger (measured suite-wall-clock pain) and the isolation requirements that " +
				"any future parallel execution would need to satisfy",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !sh031FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-031 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-031 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (8): Tags: mechanism in SH-031 body.
	t.Run("check-8-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh031FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-031 check(8) FAILED: Tags: mechanism not found in SH-031 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-031 body)\n"+
					"  detail: SH-031 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-i0tw.33 audit complete — SH-031 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
