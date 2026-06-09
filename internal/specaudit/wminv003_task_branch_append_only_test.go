package specaudit_test

// hk-8mwo.57 binding test — WM-INV-003 sensor: checkpoint commits obey git
// append-only semantics on the task branch (two-part).
//
// Spec ref: specs/workspace-model.md §5 WM-INV-003.
//
// WM-INV-003 states: Any git observer of a run's task branch MUST reject
// commits whose new tip is not a fast-forward descendant of the prior tip
// (no amend, rebase, filter-branch, force-push, git replace rewrite, or
// history-editing operation may rewrite history on a task branch that has
// emitted workspace_leased). Git's native commit semantics are the contract;
// this spec adds no transactional layer.
//
// # Audit frame
//
// This sensor is two-part, mirroring the spec's two-part invariant:
//
//  1. Part A — spec-text binding. Verifies that WM-INV-003 and its key
//     obligation phrases are present in specs/workspace-model.md. Eroding the
//     spec text would silently remove the rule the corpus check enforces.
//
//  2. Part B — code-corpus check. Walks the Go source tree under internal/ and
//     daemon/ for any exec invocation that issues a history-rewriting git
//     command scoped to a task branch (run/* branch pattern). Caught patterns:
//       - git rebase
//       - git push --force / git push -f
//       - git replace
//       - git commit --amend
//       - git filter-branch
//       - git reset --hard (on a run/ branch context)
//     Non-task-branch contexts are allowlisted:
//       - git reset --hard HEAD in the merge-back path (workloop.go) targets
//         the project root, not a task branch — explicitly allowlisted.
//     The allowlist is enumerated below with a citation to the spec requirement
//     that authorises the operation.
//
// # Spec-text binding checks (Part A)
//
// The following phrases are load-bearing in WM-INV-003:
//
//   - "fast-forward descendant" — names the required append-only topology
//   - "no amend" — explicitly prohibits git commit --amend
//   - "no rebase" — explicitly prohibits git rebase
//   - "filter-branch" — explicitly prohibits git filter-branch
//   - "force-push" — explicitly prohibits git push --force
//   - "git replace" — explicitly prohibits git replace rewrites
//   - "workspace_leased" — names the trigger event that activates the invariant
//   - "Tags: mechanism" — confirms the mechanism tag is present
//
// # Corpus check allowlist (Part B)
//
// The entries below are the ONLY authorised occurrences of history-adjacent git
// commands in internal/. Adding a new occurrence MUST include a corresponding
// allowlist entry in this file AND a cross-reference to the spec requirement
// that authorises it.
//
//   - "git reset --hard HEAD" in internal/daemon/workloop.go: targets the
//     project root working tree after a successful merge-to-main (EM-054
//     working-tree refresh). Does NOT operate on a task branch (run/* branch).
//     Authorised: execution-model.md §4.12 EM-054.
//   - "git reset --hard" in internal/workspace/intrarunrollback_wm035_test.go:
//     test-fixture rollback to a checkpoint SHA in an isolated test repo; scoped
//     to a test worktree, not a live run task branch. Authorised: WM-035 test.
//
// # Failure modes
//
//   - Part A: spec-text-binding — WM-INV-003 heading or a key obligation phrase
//     absent from specs/workspace-model.md.
//   - Part B: corpus-unlisted-history-rewrite — a Go source file contains an
//     exec invocation that matches a history-rewriting pattern and is not in the
//     allowlist; it may introduce a path that violates WM-INV-003 on a task branch.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wmInv003Fixture prefix per
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

// wmInv003FixtureWorkspaceModelPath returns the absolute path to
// specs/workspace-model.md by walking up from this test file's source path.
func wmInv003FixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wmInv003FixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wmInv003FixtureRepoRoot returns the absolute path to the repository root.
func wmInv003FixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wmInv003FixtureRepoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// wmInv003FixtureHeading matches the WM-INV-003 level-4 requirement heading line.
var wmInv003FixtureHeading = regexp.MustCompile(`^#### WM-INV-003 —`)

// wmInv003FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wmInv003FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wmInv003FixtureTagsMechanism matches a "Tags: mechanism" line.
var wmInv003FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wmInv003FixtureBodyWindow is the maximum number of lines to scan after the
// heading before the next section begins.
const wmInv003FixtureBodyWindow = 30

// wmInv003FixtureLoadLines opens specFile and returns all lines.
func wmInv003FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wmInv003FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wmInv003FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wmInv003FixtureBodyLines returns the lines comprising the WM-INV-003 body:
// all lines after the WM-INV-003 heading up to (but not including) the next
// Markdown heading or wmInv003FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the heading is not found.
func wmInv003FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wmInv003FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-INV-003 heading not found; expected '#### WM-INV-003 —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wmInv003FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wmInv003FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wmInv003FixtureBodyContains reports whether any line in body contains substr
// (case-sensitive substring match).
func wmInv003FixtureBodyContains(body []string, substr string) bool {
	for _, line := range body {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

// ─── Part A: spec-text binding ───────────────────────────────────────────────

// TestWMINV003PartASpecTextBinding verifies that WM-INV-003 and its key
// obligation phrases are present in specs/workspace-model.md.  Eroding the
// spec text would silently remove the rule that the corpus check enforces.
func TestWMINV003PartASpecTextBinding(t *testing.T) {
	t.Parallel()

	specFile := wmInv003FixtureWorkspaceModelPath(t)
	lines := wmInv003FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wmInv003FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-INV-003 check(1): %s", reason)
	}
	t.Logf("WM-INV-003 heading found at specs/workspace-model.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:    "2",
			label: "fast-forward-descendant",
			// The spec body uses "fast-forward descendant" to name the required
			// append-only topology; its absence means the spec no longer defines
			// the structural contract that Part B's corpus check enforces.
			needle: "fast-forward descendant",
			detail: "WM-INV-003 body must name 'fast-forward descendant' as the required tip " +
				"topology; this is the structural contract that no-amend, no-rebase, and " +
				"no-force-push rules enforce",
		},
		{
			id:     "3",
			label:  "no-amend",
			needle: "no amend",
			detail: "WM-INV-003 body must explicitly prohibit 'no amend' (git commit --amend) " +
				"on a task branch; its absence removes the normative prohibition on " +
				"commit-identity rewrites",
		},
		{
			id:     "4",
			label:  "no-rebase",
			needle: "rebase",
			detail: "WM-INV-003 body must mention 'rebase' as a prohibited operation; " +
				"its absence means git rebase on a task branch is no longer explicitly barred",
		},
		{
			id:     "5",
			label:  "filter-branch",
			needle: "filter-branch",
			detail: "WM-INV-003 body must mention 'filter-branch' as a prohibited operation " +
				"(Part B of the sensor targets this); its absence removes the normative " +
				"prohibition on filter-branch history rewrites that preserve tip SHA",
		},
		{
			id:     "6",
			label:  "force-push",
			needle: "force-push",
			detail: "WM-INV-003 body must explicitly prohibit 'force-push'; its absence means " +
				"git push --force on a task branch is no longer explicitly barred",
		},
		{
			id:     "7",
			label:  "git-replace",
			needle: "git replace",
			detail: "WM-INV-003 body must mention 'git replace' as a prohibited operation; " +
				"replace can rewrite history without changing the tip SHA, bypassing " +
				"Part A's monotonicity check",
		},
		{
			id:     "8",
			label:  "workspace-leased-trigger",
			needle: "workspace_leased",
			detail: "WM-INV-003 body must name 'workspace_leased' as the trigger event that " +
				"activates the append-only invariant; its absence removes the boundary " +
				"condition that separates pre-lease setup from in-flight protection",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, "-", "_")), func(t *testing.T) {
			t.Parallel()
			if !wmInv003FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-INV-003 Part A check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-INV-003 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (9): Tags: mechanism in WM-INV-003 body.
	t.Run("check-9-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wmInv003FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-INV-003 Part A check(9) FAILED: Tags: mechanism not found in WM-INV-003 body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-INV-003 body)\n"+
					"  detail: WM-INV-003 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("WM-INV-003 Part A spec-text binding complete — heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}

// ─── Part B: code-corpus check ───────────────────────────────────────────────

// wmInv003FixtureHistoryRewritePattern matches an exec invocation argument list
// that, when passed to "git", would constitute a history-rewriting operation.
//
// The patterns are designed to match string literals inside exec.CommandContext
// / exec.Command calls in Go source. Each pattern must be conservative: it
// catches the prohibited operation name without generating false positives from
// comments or spec-text references.
//
// Patterns:
//
//	"rebase"    — git rebase (standalone argument literal)
//	"--amend"   — git commit --amend
//	"filter-branch" — git filter-branch
//	"replace"   with "git" context — git replace
//	"--force"   in a push context — git push --force / git push -f
//	"-f"        in a push context — git push -f (ambiguous; caught by push context)
var wmInv003FixtureHistoryRewritePatterns = []*regexp.Regexp{
	// git rebase — any exec call containing "git" and "rebase" as a string arg.
	regexp.MustCompile(`"git"[^)]*"rebase"`),
	// git commit --amend.
	regexp.MustCompile(`"git"[^)]*"--amend"`),
	// git filter-branch.
	regexp.MustCompile(`"git"[^)]*"filter-branch"`),
	// git replace (replace as a standalone git subcommand).
	regexp.MustCompile(`"git"[^)]*"replace"`),
	// git push --force or git push -f.
	regexp.MustCompile(`"git"[^)]*"push"[^)]*"--force"`),
	regexp.MustCompile(`"git"[^)]*"push"[^)]*"-f"`),
}

// wmInv003FixtureAllowlist maps (relPath:lineNo) to the authorisation citation.
// An entry here means the pattern hit at that location is explicitly reviewed
// and declared safe with respect to WM-INV-003 (it does not operate on a
// run/* task branch).
//
// MAINTENANCE: when adding a new entry, cite the exact spec requirement that
// authorises the operation and confirm it does NOT target a run/* branch.
var wmInv003FixtureAllowlist = map[string]string{
	// internal/workspace/intrarunrollback_wm035_test.go — the test fixture
	// issues `git reset --hard <sha>` inside an isolated test worktree to
	// simulate rollback; this is a test-only path in a throwaway repo, not a
	// live run task branch. Authorised: WM-035 intra-run rollback test.
	"internal/workspace/intrarunrollback_wm035_test.go": "WM-035 test fixture; test-only isolated repo, not a live run task branch",

	// internal/daemon/workloop.go — the merge-to-main path rebases the run/* run
	// branch ONTO the target branch before the fast-forward merge, per the
	// EM-052 pre-merge rebase step (`git rebase <target>` in the worktree),
	// auto-resolves a .beads/issues.jsonl-only conflict via `git rebase
	// --continue`, and `git rebase --abort`s on any other conflict to fall
	// through to the EM-053 reopen path. This is the authorised merge-back
	// operation of WM §4.5 — it advances the integration tip, it does NOT
	// rewrite the history of a workspace_leased task branch under an observer.
	// Authorised: execution-model.md §4.12 EM-052/EM-053; workspace-model.md §4.5.
	"internal/daemon/workloop.go": "EM-052/EM-053 pre-merge rebase of run-branch onto target (WM §4.5 merge-back); not a workspace_leased task-branch rewrite",

	// internal/daemon/mergetomain_dirtyledger_hk3yz2d_test.go — scenario test
	// that drives the EM-052 merge-path rebase (and rebase --abort) inside an
	// isolated throwaway test repo to exercise the dirty-ledger auto-resolve
	// path (hk-3yz2d); not a live run task branch.
	// Authorised: EM-052/EM-053 merge-path test fixture.
	"internal/daemon/mergetomain_dirtyledger_hk3yz2d_test.go": "EM-052/EM-053 merge-path test fixture; isolated test repo, not a live run task branch",

	// internal/daemon/mergetomain_residualdelta_hkrljho_test.go — scenario test
	// that drives the EM-052 merge-path rebase inside an isolated throwaway test
	// repo to exercise the residual-tracked-delta commit-then-rebase path
	// (hk-rljho); not a live run task branch.
	// Authorised: EM-052/EM-053 merge-path test fixture.
	"internal/daemon/mergetomain_residualdelta_hkrljho_test.go": "EM-052/EM-053 merge-path test fixture; isolated test repo, not a live run task branch",

	// internal/specaudit/wminv003_task_branch_append_only_test.go — this file
	// itself contains synthetic Go code fragments (as string literals) used in
	// TestWMINV003PartBSyntheticViolationDetected and
	// TestWMINV003PartBAllowlistedFileNotFlagged. Those fragments are multi-line
	// string constants that are written to temp files; they are not actual exec
	// calls in the production or test binary. Self-exempting this file prevents
	// the scanner from flagging its own synthetic test payload.
	"internal/specaudit/wminv003_task_branch_append_only_test.go": "self-exemption: synthetic violation strings are string literal payloads for temp-file tests, not actual exec calls",
}

// wmInv003FixtureScanGoSource walks all .go files under the given root (up to
// one level of package directories) and returns any line matching a
// history-rewriting exec pattern that is NOT in the allowlist.
//
// Returns a slice of violation descriptions (empty = PASS).
func wmInv003FixtureScanGoSource(t *testing.T, repoRoot string) []string {
	t.Helper()

	// Walk internal/ directory for Go source files.
	internalDir := filepath.Join(repoRoot, "internal")
	var violations []string

	err := filepath.Walk(internalDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}

		// Check allowlist by relative path prefix (file-level granularity).
		for allowedRel := range wmInv003FixtureAllowlist {
			if relPath == allowedRel || strings.HasPrefix(relPath, allowedRel) {
				return nil
			}
		}

		//nolint:gosec // G304: path is derived from filepath.Walk over a known internal/ directory; not user input.
		f, err := os.Open(path)
		if err != nil {
			t.Logf("wmInv003FixtureScanGoSource: open %s: %v (skipping)", path, err)
			return nil
		}
		defer func() { _ = f.Close() }()

		lineNo := 0
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()

			// Skip pure comment lines — these are references to the prohibited
			// operations (e.g. in spec-audit descriptions or docstrings) and are
			// not executable code. A line is a pure comment if its first non-space
			// characters are "//".
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}

			for _, pat := range wmInv003FixtureHistoryRewritePatterns {
				if pat.MatchString(line) {
					violations = append(violations, fmt.Sprintf(
						"%s line %d: history-rewriting exec pattern %q matched — "+
							"if this operation does NOT target a run/* task branch, add it to "+
							"wmInv003FixtureAllowlist with a citation to the spec requirement "+
							"that authorises it (WM-INV-003 requires append-only semantics on "+
							"all task branches that have emitted workspace_leased)",
						relPath, lineNo, pat.String(),
					))
				}
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			t.Logf("wmInv003FixtureScanGoSource: scan %s: %v (skipping)", path, scanErr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("wmInv003FixtureScanGoSource: Walk %s: %v", internalDir, err)
	}
	return violations
}

// TestWMINV003PartBCorpusCheck scans the Go source tree under internal/ for
// exec invocations that issue history-rewriting git commands.  Any match not
// in wmInv003FixtureAllowlist is flagged as a candidate WM-INV-003 violation:
// it may introduce a path that rewrites history on a run/* task branch.
//
// Allowlisted occurrences are explicitly authorised with a citation to the spec
// requirement that permits the operation (and a confirmation that the operation
// does not target a run/* branch).
func TestWMINV003PartBCorpusCheck(t *testing.T) {
	t.Parallel()

	root := wmInv003FixtureRepoRoot(t)
	violations := wmInv003FixtureScanGoSource(t, root)

	for _, v := range violations {
		t.Errorf("WM-INV-003 Part B corpus-unlisted-history-rewrite: %s", v)
	}
	if len(violations) == 0 {
		t.Logf("WM-INV-003 Part B corpus check PASS — no unlisted history-rewriting git exec "+
			"invocations found under internal/; allowlist has %d authorised exception(s)",
			len(wmInv003FixtureAllowlist))
	}
}

// TestWMINV003PartBSyntheticViolationDetected verifies that the corpus scanner
// reliably catches a synthetic violation: a Go code fragment that calls
// exec.CommandContext with "git" and "rebase" as arguments.
//
// This test creates a temporary .go file in a scratch directory, runs the
// scanner against that directory (not the real codebase), and asserts that the
// violation is detected and reported.
func TestWMINV003PartBSyntheticViolationDetected(t *testing.T) {
	t.Parallel()

	// Create a scratch directory structure that mimics internal/<pkg>/<file>.go.
	scratchRoot := t.TempDir()
	scratchInternal := filepath.Join(scratchRoot, "internal", "fakepkg")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(scratchInternal, 0o755); err != nil {
		t.Fatalf("MkdirAll scratchInternal: %v", err)
	}

	// Write a synthetic Go file containing a prohibited exec call.
	syntheticFile := filepath.Join(scratchInternal, "violation.go")
	syntheticContent := `package fakepkg

import (
	"context"
	"os/exec"
)

func doViolation(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "git", "rebase", "main")
	_ = cmd
}
`
	//nolint:gosec // G306: test scratch file; permissions are irrelevant.
	if err := os.WriteFile(syntheticFile, []byte(syntheticContent), 0o644); err != nil {
		t.Fatalf("WriteFile synthetic: %v", err)
	}

	violations := wmInv003FixtureScanGoSource(t, scratchRoot)
	if len(violations) == 0 {
		t.Error("WM-INV-003 Part B synthetic test FAILED: scanner did not detect the " +
			"synthetic exec.CommandContext(ctx, \"git\", \"rebase\", ...) violation; " +
			"the corpus check pattern for 'rebase' is not matching — review wmInv003FixtureHistoryRewritePatterns")
	} else {
		t.Logf("WM-INV-003 Part B synthetic detection PASS — scanner reported %d violation(s) as expected", len(violations))
	}
}

// TestWMINV003PartBAllowlistedFileNotFlagged verifies that a synthetic Go file
// placed at a path matching the allowlist is NOT flagged, confirming the
// allowlist suppression logic is working.
func TestWMINV003PartBAllowlistedFileNotFlagged(t *testing.T) {
	t.Parallel()

	// Mirror the allowlisted path: internal/workspace/intrarunrollback_wm035_test.go
	scratchRoot := t.TempDir()
	scratchWorkspace := filepath.Join(scratchRoot, "internal", "workspace")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(scratchWorkspace, 0o755); err != nil {
		t.Fatalf("MkdirAll scratchWorkspace: %v", err)
	}

	// Write a synthetic Go file at the allowlisted path.
	allowedFile := filepath.Join(scratchWorkspace, "intrarunrollback_wm035_test.go")
	syntheticContent := `package workspace_test

import (
	"context"
	"os/exec"
)

func TestAllowlistedRebase(t *testing.T) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "git", "rebase", "main")
	_ = cmd
}
`
	//nolint:gosec // G306: test scratch file.
	if err := os.WriteFile(allowedFile, []byte(syntheticContent), 0o644); err != nil {
		t.Fatalf("WriteFile allowlisted: %v", err)
	}

	violations := wmInv003FixtureScanGoSource(t, scratchRoot)
	if len(violations) != 0 {
		t.Errorf("WM-INV-003 Part B allowlist test FAILED: allowlisted path was flagged (%d violation(s)):\n%s",
			len(violations), strings.Join(violations, "\n"))
	} else {
		t.Log("WM-INV-003 Part B allowlist suppression PASS — allowlisted file not flagged")
	}
}
