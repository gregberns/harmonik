package specaudit_test

// hk-i0tw.16 binding test — SH-015a: workspace snapshot mechanism (in-place worktree).
//
// Spec ref: specs/scenario-harness.md §4.4 SH-015a.
//
// SH-015a states: the workspace_snapshot_path recorded in ScenarioResult MUST
// point at the per-scenario worktree directory in-place (same path as SH-012
// fixture-up). The harness MUST NOT copy, archive, or otherwise relocate the
// worktree at teardown; SH-016's "fixture root is not auto-deleted" rule
// preserves the worktree for post-hoc inspection. workspace_state predicates
// (SH-022) that resolve git_ref_at or commit_trailer_present MUST inspect
// .git/ directly via git plumbing; predicates that resolve file_exists,
// file_contents_equal, or file_contents_match MUST inspect working files
// directly. Snapshot is captured AFTER (a)-(d) of SH-015.
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-015a is present and declares:
//
//  1. Heading present — "#### SH-015a —".
//  2. workspace_snapshot_path points at worktree in-place.
//  3. Harness MUST NOT copy, archive, or relocate the worktree at teardown.
//  4. SH-016 "fixture root is not auto-deleted" rule cited.
//  5. git_ref_at / commit_trailer_present predicates inspect .git/ via git plumbing.
//  6. file_exists / file_contents_equal / file_contents_match inspect working files directly.
//  7. Snapshot captured AFTER (a)-(d) of SH-015.
//  8. Tags: mechanism.
//
// # Helper prefix: sh015aFixture

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

func sh015aFixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh015aFixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh015aFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh015aFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh015aFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh015aFixtureSH015aHeading     = regexp.MustCompile(`^#### SH-015a —`)
	sh015aFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh015aFixtureTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

const sh015aFixtureBodyWindow = 30

func sh015aFixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh015aFixtureSH015aHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-015a heading '#### SH-015a —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh015aFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh015aFixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh015aFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH015aWorkspaceSnapshotInplace is the binding test for SH-015a.
func TestSH015aWorkspaceSnapshotInplace(t *testing.T) {
	t.Parallel()

	specFile := sh015aFixtureScenarioHarnessPath(t)
	lines := sh015aFixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh015aFixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-015a check(a): %s", reason)
	}
	t.Logf("SH-015a heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "snapshot-points-at-worktree-in-place",
			needle: "in-place",
			detail: "SH-015a body must state workspace_snapshot_path points at the worktree directory in-place " +
				"(not a copy); this is the load-bearing SH-015a invariant",
		},
		{
			id:     "c",
			label:  "must-not-copy-archive-relocate",
			needle: "MUST NOT copy",
			detail: "SH-015a body must explicitly forbid copying, archiving, or relocating the worktree at teardown; " +
				"this preserves the post-run workspace for operator inspection",
		},
		{
			id:     "d",
			label:  "sh016-fixture-root-not-auto-deleted-cited",
			needle: "not auto-deleted",
			detail: "SH-015a body must cite SH-016's fixture-root-not-auto-deleted rule as the mechanism " +
				"that preserves the in-place worktree; without this cite the preservation contract is underspecified",
		},
		{
			id:     "e",
			label:  "git-plumbing-for-git-ref-at",
			needle: "git_ref_at",
			detail: "SH-015a body must declare that git_ref_at predicates inspect .git/ via git plumbing; " +
				"this binds SH-022 predicate resolution to the in-place snapshot mechanism",
		},
		{
			id:     "f",
			label:  "git-plumbing-for-commit-trailer-present",
			needle: "commit_trailer_present",
			detail: "SH-015a body must declare that commit_trailer_present predicates inspect .git/ via git plumbing; " +
				"this binds SH-022 predicate resolution to the in-place snapshot mechanism",
		},
		{
			id:     "g",
			label:  "file-predicates-inspect-working-files-directly",
			needle: "working files directly",
			detail: "SH-015a body must state that file_exists/file_contents_equal/file_contents_match " +
				"inspect working files directly (not via git or a copy); " +
				"this distinguishes the two predicate families at the mechanism level",
		},
		{
			id:     "h",
			label:  "snapshot-captured-after-abcd-of-sh015",
			needle: "AFTER (a)-(d)",
			detail: "SH-015a body must state the snapshot is captured AFTER sub-steps (a)-(d) of SH-015; " +
				"this orders the recording obligation relative to teardown completion",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh015aFixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-015a check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-015a body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "-", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Run("check-i-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh015aFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-015a check(i) FAILED: Tags: mechanism not found in SH-015a body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-015a body)\n"+
					"  detail: SH-015a carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-015a audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
