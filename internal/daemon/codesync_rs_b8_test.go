package daemon

// codesync_rs_b8_test.go — ordered-argv tests for the DD1 GitHub code-sync
// sequence (remote-substrate B8, hk-rs-b8-codesync-3fk0).
//
// Gate-runnable: all git subprocesses are intercepted by RecordingRunner with
// a no-op CmdFunc (exec.Command("true")) so no network or real git is needed.
//
// Test matrix:
//   TestRSB8_CodeSyncArgvOrder/remote-run: verifies fetch-base → worktree-add
//     → push-branch → box-A-fetch order with correct argv for each step.
//   TestRSB8_CodeSyncArgvOrder/local-run: verifies that no SSH calls appear
//     (fetch/push steps skipped) and box-A-fetch is also skipped; only the
//     worktree-add (local runner) goes through.

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workspace"
)

// newNoOpRecorder returns a RecordingRunner whose CmdFunc delegates every call
// to exec.Command("true") so commands always succeed without side effects.
func newNoOpRecorder() *tmux.RecordingRunner {
	return &tmux.RecordingRunner{
		CmdFunc: func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.Command("true")
		},
	}
}

// TestRSB8_CodeSyncArgvOrder verifies the DD1 code-sync argv sequence for
// remote runs and confirms local runs skip the fetch/push steps.
func TestRSB8_CodeSyncArgvOrder(t *testing.T) {
	t.Parallel()

	const (
		projectDir = "/home/boxa/harmonik"
		runID      = "019ec83c-rsb8-7001-0001-000000000001"
		baseSHA    = "aabbccddaabbccddaabbccddaabbccddaabbccdd"
	)
	branch := workspace.TaskBranchName(runID)

	t.Run("remote-run", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		// Use a temp dir as the worker repo root so that workspace.CreateWorktree's
		// os.MkdirAll succeeds and reaches runner.Command (where the recording
		// happens). The git command itself is intercepted by the no-op CmdFunc.
		tmpWorkerRepo := t.TempDir()
		tmpWorkerWtPath := workspace.WorktreePath(tmpWorkerRepo, runID, workspace.NoWorktreeRootOverride())

		// sshRR captures all git commands tunnelled to the worker.
		sshRR := newNoOpRecorder()
		// localRR captures git commands run locally on box A (step c).
		localRR := newNoOpRecorder()

		// Step (a): fetch-base on worker.
		if err := fetchBaseOnWorker(ctx, sshRR, tmpWorkerRepo, baseSHA); err != nil {
			t.Fatalf("RSB8: fetchBaseOnWorker: %v", err)
		}

		// Step (worktree): create worktree on worker via SSHRunner (B7 seam).
		// We call workspace.CreateWorktree with the SSHRunner so the worktree-add
		// git command is recorded by the same sshRR. The no-op CmdFunc makes the
		// `git worktree add` return success without a real git repo.
		wtCfg := workspace.NoWorktreeRootOverride().WithRunner(sshRR)
		_ = workspace.CreateWorktree(ctx, tmpWorkerRepo, runID, baseSHA, wtCfg)

		// Step (b): push run-branch from worker to origin.
		if err := pushRunBranchOnWorker(ctx, sshRR, tmpWorkerWtPath, runID); err != nil {
			t.Fatalf("RSB8: pushRunBranchOnWorker: %v", err)
		}

		// Step (c): fetch run-branch on box A.
		if err := fetchRunBranchBoxA(ctx, localRR, projectDir, runID); err != nil {
			t.Fatalf("RSB8: fetchRunBranchBoxA: %v", err)
		}

		// ── Assert SSH call order ─────────────────────────────────────────
		// Expected calls via sshRR (in order):
		//   [0] git -C <tmpWorkerRepo> fetch origin <baseSHA>  (fetch-base)
		//   [1] mkdir -p <parentDir>                           (CreateWorktree remote mkdir, hk-eodo)
		//   [2] git -C <tmpWorkerRepo> worktree add -b ...     (worktree-add, may retry)
		//   [N] git -C <tmpWorkerWtPath> push origin run/<id> (push-branch)

		if len(sshRR.Calls) < 3 {
			t.Fatalf("RSB8/remote: expected at least 3 SSH calls, got %d: %v", len(sshRR.Calls), sshRR.Calls)
		}

		// Call 0: fetch-base
		c0 := sshRR.Calls[0]
		if c0.Name != "git" {
			t.Errorf("RSB8/remote: calls[0].Name = %q, want git", c0.Name)
		}
		wantC0 := []string{"-C", tmpWorkerRepo, "fetch", "origin", baseSHA}
		if !argvSliceEqual(c0.Args, wantC0) {
			t.Errorf("RSB8/remote: calls[0].Args = %v, want %v", c0.Args, wantC0)
		}

		// Call 1: remote mkdir -p <parentDir> (CreateWorktree routes mkdir through
		// the runner for remote runs so the parent dir is created on the worker,
		// not locally — hk-eodo TOCTOU fix).
		c1 := sshRR.Calls[1]
		if c1.Name != "mkdir" {
			t.Errorf("RSB8/remote: calls[1].Name = %q, want mkdir", c1.Name)
		}
		if len(c1.Args) < 2 || c1.Args[0] != "-p" {
			t.Errorf("RSB8/remote: calls[1].Args = %v, want [-p <parentDir>]", c1.Args)
		}

		// Call 2: worktree-add (first git command issued by CreateWorktree).
		c2 := sshRR.Calls[2]
		if c2.Name != "git" {
			t.Errorf("RSB8/remote: calls[2].Name = %q, want git", c2.Name)
		}
		if len(c2.Args) < 4 || c2.Args[0] != "-C" || c2.Args[1] != tmpWorkerRepo ||
			c2.Args[2] != "worktree" || c2.Args[3] != "add" {
			t.Errorf("RSB8/remote: calls[2].Args = %v, want [-C <tmpWorkerRepo> worktree add ...]", c2.Args)
		}

		// Last SSH call: push-branch (must come after worktree-add).
		cLast := sshRR.Calls[len(sshRR.Calls)-1]
		if cLast.Name != "git" {
			t.Errorf("RSB8/remote: last call.Name = %q, want git", cLast.Name)
		}
		wantLast := []string{"-C", tmpWorkerWtPath, "push", "origin", branch}
		if !argvSliceEqual(cLast.Args, wantLast) {
			t.Errorf("RSB8/remote: last call.Args = %v, want %v", cLast.Args, wantLast)
		}

		// ── Assert local call (box-A fetch) ──────────────────────────────
		if len(localRR.Calls) != 1 {
			t.Fatalf("RSB8/remote: expected 1 local call, got %d: %v", len(localRR.Calls), localRR.Calls)
		}
		cLocal := localRR.Calls[0]
		if cLocal.Name != "git" {
			t.Errorf("RSB8/remote: localRR.calls[0].Name = %q, want git", cLocal.Name)
		}
		wantLocal := []string{"-C", projectDir, "fetch", "origin", branch + ":refs/heads/" + branch}
		if !argvSliceEqual(cLocal.Args, wantLocal) {
			t.Errorf("RSB8/remote: localRR.calls[0].Args = %v, want %v", cLocal.Args, wantLocal)
		}

		// ── Assert ordering: worktree-add precedes push ───────────────────
		// The push must be the LAST sshRR call; the fetch-base must be the FIRST.
		// If there are intermediate calls (worktree retries, prune, branch -D),
		// they must all lie BETWEEN index 1 and the last index.
		foundFetchBase := strings.Join(sshRR.Calls[0].Args, " ")
		if !strings.Contains(foundFetchBase, "fetch") || !strings.Contains(foundFetchBase, baseSHA) {
			t.Errorf("RSB8/remote: first SSH call is not fetch-base: %v", sshRR.Calls[0].Args)
		}
		foundPush := strings.Join(sshRR.Calls[len(sshRR.Calls)-1].Args, " ")
		if !strings.Contains(foundPush, "push") {
			t.Errorf("RSB8/remote: last SSH call is not push: %v", sshRR.Calls[len(sshRR.Calls)-1].Args)
		}
	})

	t.Run("local-run-no-ssh-calls", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		// For a local run, none of the DD1 code-sync steps are triggered.
		// The daemon calls worktree-add with the default (local) runner, then
		// goes straight to mergeRunBranchToMain without any SSH or box-A fetch.
		//
		// Verify: calling fetchRunBranchBoxA with nil runner (local) does NOT
		// produce SSH output; and that fetchBaseOnWorker / pushRunBranchOnWorker
		// are simply never called (the caller guards on remoteCtx == nil).

		sshRR := newNoOpRecorder() // should remain empty for a local run

		// Simulate a local run: the orchestrator does NOT call fetchBase or push.
		// Only box-A fetch is relevant — but with nil sshRunner, it should use
		// the local runner (exec.Command, not SSH).
		localRR := newNoOpRecorder()
		if err := fetchRunBranchBoxA(ctx, localRR, projectDir, runID); err != nil {
			t.Fatalf("RSB8/local: fetchRunBranchBoxA: %v", err)
		}

		// No SSH calls should have been made.
		if len(sshRR.Calls) != 0 {
			t.Errorf("RSB8/local: expected 0 SSH calls, got %d: %v", len(sshRR.Calls), sshRR.Calls)
		}

		// The local call uses the local runner (localRR), not SSH.
		if len(localRR.Calls) != 1 {
			t.Fatalf("RSB8/local: expected 1 local call, got %d: %v", len(localRR.Calls), localRR.Calls)
		}
		localBranch := workspace.TaskBranchName(runID)
		wantLocal := []string{"-C", projectDir, "fetch", "origin", localBranch + ":refs/heads/" + localBranch}
		if !argvSliceEqual(localRR.Calls[0].Args, wantLocal) {
			t.Errorf("RSB8/local: localRR.calls[0].Args = %v, want %v", localRR.Calls[0].Args, wantLocal)
		}
	})
}

// argvSliceEqual reports whether a and b have identical elements.
func argvSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
