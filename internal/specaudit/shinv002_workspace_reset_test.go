package specaudit_test

// hk-i0tw.38 binding test — SH-INV-002: workspace state fully reset on teardown.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-002.
//
// SH-INV-002 states: "After SH-015 teardown completes, no live process spawned
// by the scenario (including handler subprocesses, `br` CLI subprocesses spawned
// by the daemon per [process-lifecycle.md §4.1.PL-006], any twin-spawned
// grandchild processes orphaned by twin exit) remains, no worktree lease is
// still held by the scenario's `run_id`, no event-log file descriptor is still
// open."
//
// # Audit frame
//
// This is a spec-corpus binding test. The harness implementation that enforces
// SH-INV-002 at runtime is pending; this test encodes the obligation at the
// spec-text layer so that any drift in the spec from the SH-INV-002 contract
// surfaces immediately. The test verifies that specs/scenario-harness.md carries:
//
//  1. Heading present — "#### SH-INV-002 —" heading is present in the spec.
//
//  2. No-live-process obligation — the requirement body states the harness MUST
//     guarantee no live process spawned by the scenario remains after teardown;
//     "no live process" is the primary obligation.
//
//  3. Descendant-tree enumeration method — the body names the getppid()-walk
//     (or equivalent) mechanism for enumerating the full descendant process tree.
//
//  4. HARMONIK_RUN_ID env-var cross-confirmation — the body cites the
//     HARMONIK_RUN_ID env-var (per PL-006a) as the cross-confirmation mechanism
//     for live-process identification.
//
//  5. Worktree-lease obligation — the body states no worktree lease is held by
//     the scenario's run_id after teardown; the lease.lock path pattern is named.
//
//  6. Event-log fd obligation — the body states no event-log file descriptor
//     remains open under the fixture root after teardown.
//
//  7. All-terminal-paths guarantee — the body states these guarantees apply on
//     every terminal path including fail, timeout, error.
//
//  8. Tags: mechanism — the body carries the "mechanism" tag per the invariant
//     requirement convention.
//
// # Failure modes
//
//   - Heading missing: "#### SH-INV-002" is absent from scenario-harness.md.
//   - No-live-process clause missing: the requirement body does not state the
//     no-live-process obligation.
//   - Descendant-tree method missing: getppid-walk mechanism not named.
//   - HARMONIK_RUN_ID citation missing: env-var cross-confirmation not cited.
//   - Lease obligation missing: lease.lock obligation not present.
//   - Event-log fd obligation missing: open fd check not present.
//   - All-terminal-paths guarantee missing: fail/timeout/error coverage not stated.
//   - Tags: mechanism missing: requirement lacks its mechanism tag.
//
// # Helper prefix
//
// All package-level identifiers in this file use the shinv002Fixture prefix per
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

// shinv002FixtureScenarioHarnessPath returns the absolute path to
// specs/scenario-harness.md by resolving from this test file's source path.
// The test file lives at:
//
//	internal/specaudit/shinv002_workspace_reset_test.go
//
// so the repo root is two directories up.
func shinv002FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("shinv002FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// shinv002FixtureSHINV002Heading matches the SH-INV-002 level-4 heading.
var shinv002FixtureSHINV002Heading = regexp.MustCompile(`^#### SH-INV-002 —`)

// shinv002FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the SH-INV-002 requirement body window.
var shinv002FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// shinv002FixtureTagsMechanism matches a "Tags: mechanism" line in the body.
var shinv002FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// shinv002FixtureBodyWindow is the maximum number of lines after the SH-INV-002
// heading to scan for requirement-body content. Matches the 30-line cap used by
// sibling specaudit tests.
const shinv002FixtureBodyWindow = 30

// shinv002FixtureLoadLines opens specFile and returns all lines.
func shinv002FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("shinv002FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("shinv002FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// shinv002FixtureBodyLines returns the lines comprising the SH-INV-002 requirement
// body: all lines after the heading up to the next Markdown heading or
// shinv002FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the SH-INV-002 heading is not found.
func shinv002FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if shinv002FixtureSHINV002Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-INV-002 heading not found; expected '#### SH-INV-002 —' in specs/scenario-harness.md"
	}

	limit := headingIdx + 1 + shinv002FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if shinv002FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// shinv002FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func shinv002FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSHINV002WorkspaceResetOnTeardown is the binding test for SH-INV-002.
//
// It opens specs/scenario-harness.md, locates the SH-INV-002 heading, and
// validates that the requirement body declares all load-bearing obligations of
// the workspace-reset invariant:
//
//	(a) Heading present.
//	(b) No-live-process obligation: "no live process" clause present.
//	(c) Descendant-tree method: getppid-walk mechanism cited.
//	(d) HARMONIK_RUN_ID env-var cross-confirmation cited.
//	(e) Worktree-lease obligation: lease.lock file pattern named.
//	(f) Event-log fd obligation: open fd check stated.
//	(g) All-terminal-paths guarantee: fail/timeout/error coverage stated.
//	(h) Tags: mechanism present.
func TestSHINV002WorkspaceResetOnTeardown(t *testing.T) {
	t.Parallel()

	specFile := shinv002FixtureScenarioHarnessPath(t)
	lines := shinv002FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := shinv002FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-INV-002 check(a): %s", reason)
	}
	t.Logf("SH-INV-002 heading found at specs/scenario-harness.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string // substring to search for (case-insensitive)
		detail string // shown on failure
	}

	checks := []check{
		{
			id:     "b",
			label:  "no-live-process_obligation",
			needle: "no live process",
			detail: "SH-INV-002 body must state that no live process spawned by the scenario " +
				"remains after teardown (expected phrase 'no live process'); " +
				"this is the primary obligation of the workspace-reset invariant",
		},
		{
			id:     "c",
			label:  "descendant-tree_enumeration",
			needle: "getppid",
			detail: "SH-INV-002 body must cite the getppid()-walk mechanism for enumerating " +
				"the full descendant process tree (expected phrase 'getppid'); " +
				"this grounds the 'no live process' predicate to a concrete OS-level mechanism",
		},
		{
			id:     "d",
			label:  "HARMONIK_RUN_ID_env-var",
			needle: "HARMONIK_RUN_ID",
			detail: "SH-INV-002 body must cite the HARMONIK_RUN_ID env-var (per PL-006a) " +
				"for cross-confirmation of live-process identification; " +
				"absence means the sensor relies solely on getppid-walk with no marker cross-check",
		},
		{
			id:     "e",
			label:  "worktree-lease_obligation",
			needle: "lease.lock",
			detail: "SH-INV-002 body must name the lease.lock file pattern " +
				"(expected phrase 'lease.lock') to ground the no-lease obligation; " +
				"this is the concrete artifact the sensor inspects in the worktree-lease registry",
		},
		{
			id:     "f",
			label:  "event-log-fd_obligation",
			needle: "event-log file",
			detail: "SH-INV-002 body must state that no event-log file descriptor remains open " +
				"under the fixture root after teardown (expected phrase 'event-log file'); " +
				"this obligation closes the fd-leak path for the captured JSONL event log",
		},
		{
			id:     "g",
			label:  "all-terminal-paths_guarantee",
			needle: "fail",
			detail: "SH-INV-002 body must state the guarantee applies on every terminal path " +
				"including fail, timeout, error (expected phrase 'fail'); " +
				"omitting this clause allows silent elision on non-pass paths",
		},
	}

	// Run checks b–g as sub-tests.
	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !shinv002FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-INV-002 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-INV-002 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "_", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (h): Tags: mechanism
	t.Run("check-h-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if shinv002FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-INV-002 check(h) FAILED: Tags: mechanism not found in SH-INV-002 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-INV-002 body)\n"+
					"  detail: SH-INV-002 carries tag 'mechanism' per the spec; its absence indicates "+
					"the requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-INV-002 audit complete (heading at line %d, body = %d lines)",
		headingLineNo, len(body))
}
