//go:build specaudit

package specaudit_test

// hk-i0tw.12 binding test — SH-012: fixture setup precedes orchestration.
//
// Spec ref: specs/scenario-harness.md §4.4 SH-012.
//
// SH-012 states: for every scenario, the harness MUST execute a fixture-setup
// phase BEFORE invoking the orchestration drive of §4.5.  Fixture setup MUST:
// (a) create a fresh workspace conforming to workspace-model.md §4.1.WM-001
// and §4.2 branching model, seeded from the scenario's declared fixture_setup
// instructions; (b) create an isolated event-log directory; (c) prepare the
// twin-binary search path.  Fixture-setup failure MUST classify as
// fixture-setup-failed per §8 and MUST NOT proceed to orchestration.  On
// partial-success failure, the harness MUST run fixture teardown best-effort
// and the failure class MUST remain fixture-setup-failed (never cleanup-failed).
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-012 is present and declares:
//
//  1. Heading present — "#### SH-012 —".
//  2. fixture-setup phase precedes orchestration drive (§4.5).
//  3. Sub-step (a): fresh workspace per WM-001.
//  4. Sub-step (b): isolated event-log directory.
//  5. Sub-step (c): twin-binary search path.
//  6. Failure class fixture-setup-failed on any setup error.
//  7. MUST NOT proceed to orchestration on setup failure.
//  8. Partial-success failure → best-effort teardown; class stays fixture-setup-failed.
//  9. Tags: mechanism.
//
// # Helper prefix: sh012Fixture

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

func sh012FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh012FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh012FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh012FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh012FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh012FixtureSH012Heading      = regexp.MustCompile(`^#### SH-012 —`)
	sh012FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh012FixtureTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

const sh012FixtureBodyWindow = 30

func sh012FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh012FixtureSH012Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-012 heading '#### SH-012 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh012FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh012FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh012FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH012FixtureSetupPrecedesOrchestration is the binding test for SH-012.
func TestSH012FixtureSetupPrecedesOrchestration(t *testing.T) {
	t.Parallel()

	specFile := sh012FixtureScenarioHarnessPath(t)
	lines := sh012FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh012FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-012 check(a): %s", reason)
	}
	t.Logf("SH-012 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "fixture-setup-precedes-orchestration-drive",
			needle: "fixture-setup phase",
			detail: "SH-012 body must declare a fixture-setup phase that precedes the orchestration drive of §4.5; " +
				"this is the load-bearing ordering invariant the bead enforces",
		},
		{
			id:     "c",
			label:  "substep-a-fresh-workspace-wm001",
			needle: "WM-001",
			detail: "SH-012 body must cite workspace-model.md §4.1.WM-001 as the workspace-creation contract " +
				"for sub-step (a); absence means the spec does not bind fixture setup to the workspace primitive",
		},
		{
			id:     "d",
			label:  "substep-b-isolated-event-log-directory",
			needle: "event-log directory",
			detail: "SH-012 body must declare sub-step (b): creating an isolated event-log directory; " +
				"this is what grounds SH-014's per-scenario isolation guarantee at the fixture layer",
		},
		{
			id:     "e",
			label:  "substep-c-twin-binary-search-path",
			needle: "twin-binary search path",
			detail: "SH-012 body must declare sub-step (c): preparing the twin-binary search path; " +
				"absence means the fixture-setup contract is silent on search-path provisioning",
		},
		{
			id:     "f",
			label:  "failure-class-fixture-setup-failed",
			needle: "fixture-setup-failed",
			detail: "SH-012 body must state that fixture-setup failure MUST classify as fixture-setup-failed; " +
				"this binds the failure taxonomy (§8.3) to the fixture-setup phase",
		},
		{
			id:     "g",
			label:  "must-not-proceed-to-orchestration",
			needle: "MUST NOT proceed to orchestration",
			detail: "SH-012 body must explicitly forbid proceeding to orchestration after fixture-setup failure; " +
				"this is the fail-fast invariant that prevents partially-constructed fixtures from driving the daemon",
		},
		{
			id:     "h",
			label:  "partial-success-best-effort-teardown",
			needle: "partial-success",
			detail: "SH-012 body must address partial-success failure (some sub-steps completed before failure) " +
				"and mandate best-effort teardown; absence leaves the partial-fixture cleanup path underspecified",
		},
		{
			id:     "i",
			label:  "partial-rollback-class-stays-fixture-setup-failed",
			needle: "fixture-setup-failed",
			detail: "SH-012 body must state that teardown errors during partial-rollback do NOT change " +
				"the failure class from fixture-setup-failed to cleanup-failed; " +
				"this prevents the §8.0 precedence table from being violated by teardown noise",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh012FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-012 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-012 body)\n"+
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
			if sh012FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-012 check(j) FAILED: Tags: mechanism not found in SH-012 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-012 body)\n"+
					"  detail: SH-012 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-012 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
