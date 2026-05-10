package specaudit_test

// hk-8mwo.46 binding test — WM-034 reopen-bead verdict triggers fresh worktree and
// fresh branch and mints a fresh run_id.
//
// Spec ref: specs/workspace-model.md §4.9 WM-034.
//
// WM-034 states: when the investigator agent issues a reopen-bead verdict per
// [reconciliation/spec.md §4.5 RC-020], the subsequent claim of the reopened bead MUST
// produce a new run with a FRESH run_id distinct from every prior run_id ever dispatched
// against the bead. The fresh run_id becomes the lease key for a FRESH worktree at the
// canonical path <repo>/.harmonik/worktrees/<new_run_id>/ and a FRESH task branch named
// run/<new_run_id>. The workspace manager MUST reject any attempt to reuse a prior run_id
// at workspace-create time.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The workspace manager implementation is pending;
// this sensor verifies that WM-034 is correctly declared in the spec so that:
//
//  1. WM-034 heading is present in specs/workspace-model.md.
//  2. "reopen-bead" verdict is named as the trigger.
//  3. "FRESH run_id" is declared as the required outcome.
//  4. "FRESH worktree" at canonical path is declared.
//  5. "FRESH task branch" named run/<new_run_id> is declared.
//  6. "MUST reject any attempt to reuse a prior run_id" is declared.
//  7. Tags: mechanism is present in the WM-034 body window.
//
// # Failure modes
//
//   - WM-034 heading missing.
//   - reopen-bead verdict absent.
//   - FRESH run_id absent.
//   - FRESH worktree absent.
//   - FRESH task branch absent.
//   - MUST reject reuse of prior run_id absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wm034Fixture prefix per
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

// wm034FixtureWorkspaceModelPath returns the absolute path to specs/workspace-model.md.
func wm034FixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wm034FixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wm034FixtureWM034Heading matches the WM-034 level-4 requirement heading line.
var wm034FixtureWM034Heading = regexp.MustCompile(`^#### WM-034 —`)

// wm034FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wm034FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wm034FixtureTagsMechanism matches a "Tags: mechanism" line.
var wm034FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wm034FixtureBodyWindow is the maximum number of lines after the WM-034
// heading to scan for requirement-body content.
const wm034FixtureBodyWindow = 30

// wm034FixtureLoadLines opens specFile and returns all lines.
func wm034FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wm034FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wm034FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wm034FixtureWM034BodyLines returns the lines comprising the WM-034 body.
func wm034FixtureWM034BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wm034FixtureWM034Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-034 heading not found; expected '#### WM-034 —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wm034FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wm034FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wm034FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func wm034FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestWM034ReopenBeadFreshRunID is the binding test for hk-8mwo.46.
func TestWM034ReopenBeadFreshRunID(t *testing.T) {
	t.Parallel()

	specFile := wm034FixtureWorkspaceModelPath(t)
	lines := wm034FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wm034FixtureWM034BodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-034 check(1): %s", reason)
	}
	t.Logf("WM-034 heading found at specs/workspace-model.md line %d; body window = %d lines",
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
			label:  "reopen-bead-verdict-trigger",
			needle: "reopen-bead",
			detail: "WM-034 body must name 'reopen-bead' as the triggering verdict " +
				"(expected phrase 'reopen-bead'); this is the investigator-agent verdict " +
				"that causes the workspace manager to mint a fresh run_id — other verdicts " +
				"(resume-here, reset-to-checkpoint) keep the same worktree per WM-035",
		},
		{
			id:     "3",
			label:  "fresh-run-id-required",
			needle: "FRESH run_id",
			detail: "WM-034 body must declare 'FRESH run_id' as the required outcome " +
				"(expected phrase 'FRESH run_id'); the fresh run_id must be distinct from " +
				"every prior run_id ever dispatched against the bead — reuse of a prior " +
				"run_id would create a path collision with the retained failed-run worktree",
		},
		{
			id:     "4",
			label:  "fresh-worktree-canonical-path",
			needle: "FRESH worktree",
			detail: "WM-034 body must declare 'FRESH worktree' at the canonical path " +
				"(expected phrase 'FRESH worktree'); the new run gets a new worktree at " +
				"<repo>/.harmonik/worktrees/<new_run_id>/ — the prior run's worktree " +
				"persists on disk per WM-031 (failed-run worktrees are not auto-deleted)",
		},
		{
			id:     "5",
			label:  "fresh-task-branch",
			needle: "FRESH task branch",
			detail: "WM-034 body must declare 'FRESH task branch' named run/<new_run_id> " +
				"(expected phrase 'FRESH task branch'); the fresh worktree gets a fresh branch — " +
				"distinct task branches are what allow the prior run's branch to persist " +
				"for audit without conflicting with the new run's work",
		},
		{
			id:     "6",
			label:  "must-reject-prior-run-id-reuse",
			needle: "MUST reject any attempt to reuse a prior",
			detail: "WM-034 body must declare 'MUST reject any attempt to reuse a prior' run_id " +
				"(expected phrase 'MUST reject any attempt to reuse a prior'); this is the " +
				"defensive guard at workspace-create time — if the execution model tries to " +
				"reuse a run_id the workspace manager must refuse, preventing silent data corruption",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !wm034FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-034 check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-034 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in WM-034 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wm034FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-034 check(7) FAILED: Tags: mechanism not found in WM-034 body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-034 body)\n"+
					"  detail: WM-034 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mwo.46 audit complete — WM-034 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
