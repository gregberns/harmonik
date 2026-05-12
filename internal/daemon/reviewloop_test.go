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
// verdictsByIteration[i] is the verdict for iteration i+1 (1-based).
func rlFixtureHandlerScript(t *testing.T, wtPath string, verdictsByIteration []string) string {
	t.Helper()

	var caseLines strings.Builder
	for i, v := range verdictsByIteration {
		iterNum := i + 1
		vj := strings.ReplaceAll(rlFixtureVerdictJSON(v), "'", "'\\''")
		fmt.Fprintf(&caseLines,
			"    %d) printf '%%s' '%s' > \"$WTP/.harmonik/review.json\" ;;\n",
			iterNum, vj,
		)
	}

	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")

	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
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
  case "$ITER" in
%s    *) printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"fallback"}' > "$WTP/.harmonik/review.json" ;;
  esac
else
  # Implementer: create a unique file and commit to advance HEAD.
  # This ensures the diff hash changes on each iteration, avoiding false
  # no-progress detection (spec: same hash = no progress per EM-015e).
  # Redirect git output to stderr so the watcher does not see it as NDJSON
  # (malformed JSON causes the watcher to exit the goroutine early, which
  # closes the stdout pipe and triggers SIGPIPE in the subprocess).
  printf '%%d' "$CNT" > "$WTP/impl_iter_$CNT.txt"
  git -C "$WTP" add "impl_iter_$CNT.txt" >/dev/null 2>&1
  git -C "$WTP" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl iter $CNT" --no-gpg-sign >/dev/null 2>&1
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
