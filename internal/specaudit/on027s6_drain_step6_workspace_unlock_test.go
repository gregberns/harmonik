package specaudit_test

// hk-sx9r.39 binding test — ON-027 step 6: workspace manager unlocks leased workspaces.
//
// Spec ref: specs/operator-nfr.md §4.7 ON-027.
//
// ON-027 step 6 states: workspace manager unlocks leased workspaces and cleans up
// incomplete adze setups per [workspace-model.md §4.3]. Bounded by timeout.step_6
// per ON-029. Per ON-027a, durably marked.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The drain step-6 workspace-unlock implementation
// is pending; this sensor verifies that ON-027 step 6 is correctly declared in the spec
// so that:
//
//  1. ON-027 heading is present in specs/operator-nfr.md.
//  2. "workspace manager unlocks leased workspaces" declares step 6.
//  3. "adze setups" cleanup is named as part of step 6.
//  4. "workspace-model.md §4.3" is cited for the unlock/cleanup contract.
//  5. Step ordering is declared — each step completes before the next.
//  6. "pausing → paused" precondition requires ALL steps.
//  7. Tags: mechanism is present in the ON-027 body window.
//
// # Failure modes
//
//   - ON-027 heading missing.
//   - workspace manager unlocks leased workspaces absent.
//   - adze setups cleanup absent.
//   - workspace-model.md §4.3 citation absent.
//   - step ordering absent.
//   - pausing→paused precondition absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on027s6Fixture prefix per
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

// on027s6FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on027s6FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on027s6FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on027s6FixtureON027Heading matches the ON-027 level-4 requirement heading line.
var on027s6FixtureON027Heading = regexp.MustCompile(`^#### ON-027 —`)

// on027s6FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on027s6FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on027s6FixtureTagsMechanism matches a "Tags: mechanism" line.
var on027s6FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on027s6FixtureBodyWindow is the maximum number of lines after the ON-027
// heading to scan for requirement-body content.
const on027s6FixtureBodyWindow = 30

// on027s6FixtureLoadLines opens specFile and returns all lines.
func on027s6FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on027s6FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on027s6FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on027s6FixtureON027BodyLines returns the lines comprising the ON-027 body.
func on027s6FixtureON027BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on027s6FixtureON027Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-027 heading not found; expected '#### ON-027 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on027s6FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on027s6FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on027s6FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on027s6FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON027Step6WorkspaceManagerUnlocksWorkspaces is the binding test for hk-sx9r.39.
func TestON027Step6WorkspaceManagerUnlocksWorkspaces(t *testing.T) {
	t.Parallel()

	specFile := on027s6FixtureOperatorNFRPath(t)
	lines := on027s6FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on027s6FixtureON027BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-027 check(1): %s", reason)
	}
	t.Logf("ON-027 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "workspace-manager-unlocks-leased-workspaces",
			needle: "workspace manager unlocks leased workspaces",
			detail: "ON-027 body must declare 'workspace manager unlocks leased workspaces' as step 6 " +
				"(expected phrase 'workspace manager unlocks leased workspaces'); step 6 is the " +
				"workspace subsystem's drain obligation: all leased worktree locks acquired during " +
				"run execution MUST be released before the daemon can safely enter paused or exit",
		},
		{
			id:     "3",
			label:  "adze-setups-cleanup-named",
			needle: "adze setups",
			detail: "ON-027 body must name 'adze setups' as part of step 6 cleanup " +
				"(expected phrase 'adze setups'); adze is the workspace setup mechanism; " +
				"incomplete adze setups (partially-provisioned workspaces) MUST be cleaned " +
				"up during drain to avoid leaving orphaned workspace state",
		},
		{
			id:     "4",
			label:  "workspace-model-section-4-3-cited",
			needle: "workspace-model.md §4.3",
			detail: "ON-027 body must cite 'workspace-model.md §4.3' for the unlock/cleanup contract " +
				"(expected phrase 'workspace-model.md §4.3'); WM §4.3 owns the workspace lease and " +
				"unlock contract that step 6 invokes — the citation anchors the drain obligation to " +
				"the normative workspace-model spec",
		},
		{
			id:     "5",
			label:  "step-ordering-each-before-next",
			needle: "each step completing before the next begins",
			detail: "ON-027 body must declare step ordering: 'each step completing before the " +
				"next begins' (expected phrase 'each step completing before the next begins'); " +
				"this is the sequential invariant that makes step 6 follow step 5 — workspace " +
				"unlock follows memory flush in the drain sequence",
		},
		{
			id:     "6",
			label:  "pausing-to-paused-all-steps-precondition",
			needle: "pausing → paused",
			detail: "ON-027 body must declare the 'pausing → paused' transition requires ALL " +
				"steps (expected phrase 'pausing → paused'); this makes step 6 a mandatory " +
				"gate: the daemon cannot enter paused until workspace unlocks in step 6 complete",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on027s6FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-027 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-027 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in ON-027 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on027s6FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-027 check(7) FAILED: Tags: mechanism not found in ON-027 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-027 body)\n"+
					"  detail: ON-027 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.39 audit complete — ON-027 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
