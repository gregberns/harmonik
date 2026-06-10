package daemon_test

// reviewloop_test.go — smoke tests for the review-loop dispatch driver (T-WM-020).
//
// Covers the three acceptance-criteria paths from the bead body:
//   1. TestReviewLoop_HappyPath_APPROVE — iteration 1 APPROVE → success.
//   2. TestReviewLoop_RequestChangesThenAPPROVE — REQUEST_CHANGES iter 1, APPROVE iter 2.
//   3. TestReviewLoop_CapHit — three REQUEST_CHANGES iterations → cap-hit.
//
// Each test calls daemon.ExportedRunReviewLoop directly (bypassing runWorkLoop)
// with a pre-created git worktree and a shell-script handler that writes a
// verdict file on each reviewer invocation.
//
// Handler script contract:
//   - Each launch (implementer or reviewer) increments a counter in
//     $WT/.harmonik/rl_count.
//   - Odd invocations (1, 3, 5, …) are implementer launches — exit 0 silently.
//   - Even invocations (2, 4, 6, …) are reviewer launches — write
//     $WT/.harmonik/review.json from a per-iteration verdict table.
//
// Helper prefix: rlFixture (per implementer-protocol.md §Helper-prefix discipline;
// bead hk-7om2q.20).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// rlFixtureProjectDir creates the minimal project directory tree for review-loop
// tests: .harmonik/events/, .harmonik/beads-intents/.
func rlFixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rlFixtureProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("rlFixtureProjectDir: mkdir beads-intents: %v", err)
	}
	return dir
}

// rlFixtureGitRepo initialises a bare git repository with one initial commit in dir.
func rlFixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("rlFixtureGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("harmonik review-loop test repo\n"), 0o644); err != nil {
		t.Fatalf("rlFixtureGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
}

// rlFixtureWorktree creates a detached git worktree, creates .harmonik/ inside
// it, and registers a cleanup. Returns the worktree path and the parent commit
// SHA (project HEAD at creation time).
func rlFixtureWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()

	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("rlFixtureWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = strings.TrimSpace(string(out))

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")

	//nolint:gosec // G204: git args are test-internal literals; not user input
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("rlFixtureWorktree: git worktree add: %v\n%s", err, out)
	}

	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("rlFixtureWorktree: mkdir .harmonik: %v", err)
	}

	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})

	return wtPath, parentSHA
}

// rlFixtureRunID generates a fresh test RunID using UUIDv7.
func rlFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("rlFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// rlFixtureVerdictJSON returns a minimal valid agent-reviewer JSON schema v1
// verdict payload for the given verdict string.
func rlFixtureVerdictJSON(verdict string) string {
	type vFile struct {
		SchemaVersion int      `json:"schema_version"`
		Verdict       string   `json:"verdict"`
		Flags         []string `json:"flags"`
		Notes         string   `json:"notes"`
	}
	b, _ := json.Marshal(vFile{
		SchemaVersion: 1,
		Verdict:       verdict,
		Flags:         []string{},
		Notes:         fmt.Sprintf("Test verdict: %s", verdict),
	})
	return string(b)
}

// rlFixtureHandlerScript writes a /bin/sh handler script to a temp dir.
//
// The script increments a counter in wtPath/.harmonik/rl_count on each
// invocation. Odd invocations (implementer) exit 0 silently. Even invocations
// (reviewer) write review.json using the verdict table indexed by iteration
// number (invocation/2).
//
// The counter file always lives in the implementer's wtPath (shared across all
// invocations).  The verdict file is written to $HARMONIK_WORKSPACE_PATH so
// it reaches the reviewer's isolated worktree (hk-dut6b) rather than the
// hardcoded wtPath.
//
// verdictsByIteration[i] is the verdict for iteration i+1 (1-based).
func rlFixtureHandlerScript(t *testing.T, wtPath string, verdictsByIteration []string) string {
	t.Helper()

	var caseLines strings.Builder
	for i, v := range verdictsByIteration {
		iterNum := i + 1
		vj := strings.ReplaceAll(rlFixtureVerdictJSON(v), "'", "'\\''")
		fmt.Fprintf(&caseLines,
			"    %d) printf '%%s' '%s' > \"$WS/.harmonik/review.json\" ;;\n",
			iterNum, vj,
		)
	}

	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")

	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
# WS is the per-invocation workspace path: reviewer gets an isolated worktree
# (hk-dut6b); implementer gets the shared wtPath.
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/rl_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
# Even invocations = reviewer (implementer is odd).
if [ $((CNT %% 2)) -eq 0 ]; then
  ITER=$((CNT / 2))
  mkdir -p "$WS/.harmonik"
  case "$ITER" in
%s    *) printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"fallback"}' > "$WS/.harmonik/review.json" ;;
  esac
else
  # Implementer: create a unique file and commit to advance HEAD.
  # This ensures the diff hash changes on each iteration, avoiding false
  # no-progress detection (spec: same hash = no progress per EM-015e).
  # Redirect git output to stderr so the watcher does not see it as NDJSON
  # (malformed JSON causes the watcher to exit the goroutine early, which
  # closes the stdout pipe and triggers SIGPIPE in the subprocess).
  printf '%%d' "$CNT" > "$WS/impl_iter_$CNT.txt"
  git -C "$WS" add "impl_iter_$CNT.txt" >/dev/null 2>&1
  git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl iter $CNT" --no-gpg-sign >/dev/null 2>&1
fi
exit 0
`, wtpEsc, caseLines.String())

	scriptPath := filepath.Join(t.TempDir(), "rl_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rlFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestReviewLoop_HappyPath_APPROVE verifies that an APPROVE verdict on
// iteration 1 terminates successfully:
//   - result.Success = true
//   - result.CompletionReason = "approved"
//   - result.NeedsAttention = false
//   - reviewer_launched, reviewer_verdict, review_loop_cycle_complete emitted in order.
func TestReviewLoop_HappyPath_APPROVE(t *testing.T) {
	t.Parallel()

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	scriptPath := rlFixtureHandlerScript(t, wtPath, []string{"APPROVE"})

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("rl-approve-001"),
		wtPath, parentSHA,
	)

	if !result.Success {
		t.Errorf("expected success=true; summary=%q", result.Summary)
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonApproved) {
		t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonApproved)
	}
	if result.NeedsAttention {
		t.Error("expected needs_attention=false on APPROVE path")
	}

	eventTypes := collector.eventTypes()
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewerLaunched))
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewerVerdict))
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewLoopCycleComplete))
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeReviewerLaunched),
		string(core.EventTypeReviewerVerdict),
		string(core.EventTypeReviewLoopCycleComplete),
	})
}

// TestReviewLoop_RequestChangesThenAPPROVE verifies the two-iteration path:
//   - Iteration 1: REQUEST_CHANGES → iteration 2 dispatched.
//   - Iteration 2: APPROVE → cycle terminates as completed.
//   - implementer_resumed emitted before iteration 2's reviewer.
//   - Verdict file from iteration 1 archived to .harmonik/review.iter-1.json
//     (T-WM-027 acceptance criterion: archive file exists after cycle).
func TestReviewLoop_RequestChangesThenAPPROVE(t *testing.T) {
	t.Parallel()

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	scriptPath := rlFixtureHandlerScript(t, wtPath, []string{"REQUEST_CHANGES", "APPROVE"})

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("rl-rc-approve-001"),
		wtPath, parentSHA,
	)

	if !result.Success {
		t.Errorf("expected success=true on RC→APPROVE; summary=%q", result.Summary)
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonApproved) {
		t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonApproved)
	}

	eventTypes := collector.eventTypes()
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeImplementerResumed))
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeReviewerLaunched),   // iter 1
		string(core.EventTypeReviewerVerdict),    // iter 1
		string(core.EventTypeImplementerResumed), // iter 2 dispatch
		string(core.EventTypeReviewerLaunched),   // iter 2
		string(core.EventTypeReviewerVerdict),    // iter 2
		string(core.EventTypeReviewLoopCycleComplete),
	})

	// T-WM-027 acceptance criterion: iteration 1 verdict is archived to
	// .harmonik/review.iter-1.json. The daemon calls ArchiveVerdict(wtPath, 1)
	// immediately after emitting reviewer_verdict for iteration 1, before looping
	// to iteration 2. Verify the file exists at the canonical path.
	//nolint:gosec // G304: test fixture path; wtPath is a t.TempDir()-derived value
	archivePath := filepath.Join(wtPath, ".harmonik", "review.iter-1.json")
	if _, err := os.Stat(archivePath); err != nil {
		t.Errorf("T-WM-027: verdict archive file missing after RC→APPROVE cycle: %s: %v", archivePath, err)
	}
}

// TestReviewLoop_CapHit verifies that three REQUEST_CHANGES verdicts trigger
// cap-hit termination:
//   - result.Success = false
//   - result.CompletionReason = "cap_hit"
//   - result.NeedsAttention = true
//   - iteration_cap_hit emitted before review_loop_cycle_complete.
func TestReviewLoop_CapHit(t *testing.T) {
	t.Parallel()

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	scriptPath := rlFixtureHandlerScript(t, wtPath, []string{
		"REQUEST_CHANGES", "REQUEST_CHANGES", "REQUEST_CHANGES",
	})

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("rl-cap-hit-001"),
		wtPath, parentSHA,
	)

	if result.Success {
		t.Error("expected success=false on cap-hit path")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonCapHit) {
		t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonCapHit)
	}
	if !result.NeedsAttention {
		t.Error("expected needs_attention=true on cap-hit path")
	}

	eventTypes := collector.eventTypes()
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeIterationCapHit))
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeIterationCapHit),
		string(core.EventTypeReviewLoopCycleComplete),
	})
}

// rlFixtureNoProgressHandlerScript writes a /bin/sh handler script whose
// implementer phase (odd invocations) commits a new file on the FIRST
// implementer run and does nothing on subsequent runs.  This ensures the diff
// hash is identical on iterations ≥ 2, triggering no-progress detection.
//
// The reviewer phase (even invocations) writes the verdict supplied in
// firstVerdict (used for iteration 1 only; no iteration-2 reviewer is expected
// because no-progress terminates before launching it).
//
// Helper prefix: rlFixture (bead hk-7om2q.22).
func rlFixtureNoProgressHandlerScript(t *testing.T, wtPath, firstVerdict string) string {
	t.Helper()

	vj := strings.ReplaceAll(rlFixtureVerdictJSON(firstVerdict), "'", "'\\''")
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")

	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/rl_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
# Even invocations = reviewer; odd = implementer.
if [ $((CNT %% 2)) -eq 0 ]; then
  # Reviewer: write the verdict to reviewer's isolated workspace (hk-dut6b).
  mkdir -p "$WS/.harmonik"
  printf '%%s' '%s' > "$WS/.harmonik/review.json"
else
  IMPL_NUM=$(((CNT + 1) / 2))
  if [ "$IMPL_NUM" -eq 1 ]; then
    # First implementer: commit a file so diff hash advances.
    printf '%%d' "$CNT" > "$WS/impl_iter_$CNT.txt"
    git -C "$WS" add "impl_iter_$CNT.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl iter $CNT" --no-gpg-sign >/dev/null 2>&1
  fi
  # Subsequent implementers: do nothing — diff hash stays identical.
fi
exit 0
`, wtpEsc, vj)

	scriptPath := filepath.Join(t.TempDir(), "rl_noprogress_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rlFixtureNoProgressHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// TestReviewLoop_NoProgress verifies that identical implementer output across
// iterations triggers the no-progress termination path (EM-015e / T-WM-022):
//   - The first iteration runs normally (implementer commits, reviewer issues
//     REQUEST_CHANGES).
//   - The second implementer run does NOT commit anything — diff hash is
//     unchanged relative to iteration 1.
//   - Before launching the iteration-2 reviewer, the daemon detects the stale
//     hash, emits no_progress_detected, and terminates with needs-attention.
//   - result.Success = false
//   - result.CompletionReason = "no_progress"
//   - result.NeedsAttention = true
//   - no_progress_detected is emitted BEFORE review_loop_cycle_complete.
//   - reviewer_launched is emitted exactly once (iteration 1 only).
func TestReviewLoop_NoProgress(t *testing.T) {
	t.Parallel()

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	// Script: iter-1 implementer commits → iter-1 reviewer issues REQUEST_CHANGES
	// → iter-2 implementer does nothing → no_progress fires before iter-2 reviewer.
	scriptPath := rlFixtureNoProgressHandlerScript(t, wtPath, "REQUEST_CHANGES")

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("rl-noprogress-001"),
		wtPath, parentSHA,
	)

	if result.Success {
		t.Error("expected success=false on no-progress path")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonNoProgress) {
		t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonNoProgress)
	}
	if !result.NeedsAttention {
		t.Error("expected needs_attention=true on no-progress path")
	}

	eventTypes := collector.eventTypes()

	// no_progress_detected must be emitted.
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeNoProgressDetected))

	// reviewer_launched appears exactly once (iteration 1 only — iteration 2
	// reviewer must NOT be launched when no-progress is detected).
	launchCount := 0
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerLaunched) {
			launchCount++
		}
	}
	if launchCount != 1 {
		t.Errorf("reviewer_launched emitted %d times; want 1 (no iter-2 reviewer)", launchCount)
	}

	// Ordering: no_progress_detected before review_loop_cycle_complete.
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeNoProgressDetected),
		string(core.EventTypeReviewLoopCycleComplete),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Config-path isolation (hk-1o0cc de-flake)
// ─────────────────────────────────────────────────────────────────────────────

// rlIsolateClaudeConfig redirects EnsureWorktreeTrust's ~/.claude.json target to
// a per-test temp file so the test never touches the REAL user config and never
// contends on its ~/.claude.json.lock sidecar.
//
// Why this fixes the hk-1o0cc -short flakes: the review-loop scenario tests boot
// ExportedRunReviewLoop, which calls buildClaudeLaunchSpec → EnsureWorktreeTrust.
// On a first-seen worktree path that call takes a BOUNDED LOCK_EX on
// <cfgPath>.lock. When the tests run unisolated against the real ~/.claude.json
// while a live harmonik daemon plus other test processes are also rewriting that
// same 8MB config, the bounded acquire times out (ErrTrustLockTimeout) and the
// launch fails → the test reds intermittently ("write-lock acquire timed out
// (contended ~/.claude.json)" / "SpawnWindow: Start: context deadline exceeded").
// Pointing each test at its own temp config removes the cross-process contention.
//
// MUST be called from a NON-parallel test: HARMONIK_CLAUDE_CONFIG_PATH is a
// process-global env var (t.Setenv forbids t.Parallel, and a racy os.Setenv from
// concurrent parallel tests would clobber each other). This matches the existing
// convention in the daemon scenario tests (e.g. scenariotest/concurrent_merge.go,
// scenario_happypath_n1_test.go) which all set HARMONIK_CLAUDE_CONFIG_PATH and run
// serially for exactly this reason. The cleanup RESTORES the prior value rather
// than blindly unsetting, so a TestMain-level default (if ever added) survives.
func rlIsolateClaudeConfig(t *testing.T) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), ".claude.json")
	prev, had := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", cfgPath); err != nil {
		t.Fatalf("rlIsolateClaudeConfig: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", prev)
		} else {
			_ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH")
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Assertion helpers
// ─────────────────────────────────────────────────────────────────────────────

// rlAssertEventPresent asserts that eventType appears at least once in types.
func rlAssertEventPresent(t *testing.T, types []string, eventType string) {
	t.Helper()
	for _, et := range types {
		if et == eventType {
			return
		}
	}
	t.Errorf("event %q not found in emitted events %v", eventType, types)
}

// rlAssertEventSubsequence asserts that the given subsequence appears in order
// (not necessarily contiguously) within types.
func rlAssertEventSubsequence(t *testing.T, types []string, subsequence []string) {
	t.Helper()
	if len(subsequence) == 0 {
		return
	}
	si := 0
	for _, et := range types {
		if et == subsequence[si] {
			si++
			if si == len(subsequence) {
				return
			}
		}
	}
	t.Errorf("event subsequence %v not found in order; emitted: %v", subsequence, types)
}

// TestReviewLoop_RunIDPropagation verifies the T-WM-025 acceptance criteria:
//   - A REQUEST_CHANGES → APPROVE cycle emits the expected ordered sequence.
//   - Every §8.1a event payload carries a run_id matching the one passed to runReviewLoop.
//   - Each reviewer_verdict payload is well-formed per agent-reviewer schema v1
//     (validated via ReviewerVerdictPayload.Valid()).
//
// Helper prefix: rlFixture (per implementer-protocol.md §Helper-prefix discipline;
// bead hk-7om2q.25).
func TestReviewLoop_RunIDPropagation(t *testing.T) {
	t.Parallel()

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	scriptPath := rlFixtureHandlerScript(t, wtPath, []string{"REQUEST_CHANGES", "APPROVE"})

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runID := rlFixtureRunID(t)

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		runID,
		core.BeadID("rl-runid-prop-001"),
		wtPath, parentSHA,
	)

	if !result.Success {
		t.Fatalf("expected success=true on RC→APPROVE; summary=%q", result.Summary)
	}

	// ── 1. Expected ordered event sequence ───────────────────────────────────
	eventTypes := collector.eventTypes()
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeReviewerLaunched),   // iter 1
		string(core.EventTypeReviewerVerdict),    // iter 1 REQUEST_CHANGES
		string(core.EventTypeImplementerResumed), // iter 2 dispatch
		string(core.EventTypeReviewerLaunched),   // iter 2
		string(core.EventTypeReviewerVerdict),    // iter 2 APPROVE
		string(core.EventTypeReviewLoopCycleComplete),
	})

	// ── 2. All §8.1a event payloads carry the same run_id ────────────────────
	//
	// The §8.1a payload types all embed RunID at the top level as "run_id".
	// We unmarshal each payload into a minimal struct to extract run_id and
	// compare it to the runID passed into runReviewLoop.
	type runIDEnvelope struct {
		RunID uuid.UUID `json:"run_id"`
	}

	// §8.1a event types that carry run_id per the spec.
	rl8a1EventTypes := map[string]struct{}{
		string(core.EventTypeImplementerResumed):      {},
		string(core.EventTypeReviewerLaunched):        {},
		string(core.EventTypeReviewerVerdict):         {},
		string(core.EventTypeIterationCapHit):         {},
		string(core.EventTypeNoProgressDetected):      {},
		string(core.EventTypeReviewLoopCycleComplete): {},
	}

	wantRunUUID := uuid.UUID(runID)
	allEvents := collector.allEvents()

	for i, ev := range allEvents {
		if _, ok := rl8a1EventTypes[ev.EventType]; !ok {
			continue
		}
		var env runIDEnvelope
		if err := json.Unmarshal(ev.Payload, &env); err != nil {
			t.Errorf("event[%d] %q: unmarshal run_id: %v", i, ev.EventType, err)
			continue
		}
		if env.RunID != wantRunUUID {
			t.Errorf("event[%d] %q: run_id = %v; want %v", i, ev.EventType, env.RunID, wantRunUUID)
		}
	}

	// ── 3. reviewer_verdict payloads conform to agent-reviewer schema v1 ─────
	//
	// We expect two reviewer_verdict events (iter 1 = REQUEST_CHANGES, iter 2 = APPROVE).
	// Each must unmarshal to a well-formed ReviewerVerdictPayload per .Valid().
	verdictCount := 0
	for i, ev := range allEvents {
		if ev.EventType != string(core.EventTypeReviewerVerdict) {
			continue
		}
		var pl core.ReviewerVerdictPayload
		if err := json.Unmarshal(ev.Payload, &pl); err != nil {
			t.Errorf("reviewer_verdict[%d]: unmarshal: %v", verdictCount, err)
			verdictCount++
			continue
		}
		if !pl.Valid() {
			t.Errorf("reviewer_verdict event[%d] (payload index %d): ReviewerVerdictPayload.Valid() = false; payload: %s",
				verdictCount, i, ev.Payload)
		}
		verdictCount++
	}
	if verdictCount != 2 {
		t.Errorf("expected 2 reviewer_verdict events (iter 1 RC + iter 2 APPROVE); got %d", verdictCount)
	}
}
