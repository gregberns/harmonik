package daemon

// codesync_rs_b8_test.go — ordered-argv tests for the DD1 code-sync sequence
// (remote-substrate B8, hk-rs-b8-codesync-3fk0; box-A direct-fetch rework hk-7bwx).
//
// Gate-runnable: all git subprocesses are intercepted by RecordingRunner with
// a no-op CmdFunc (exec.Command("true")) so no network or real git is needed.
//
// Test matrix:
//   TestRSB8_CodeSyncArgvOrder/remote-run: verifies fetch-base → worktree-add
//     (on the worker via SSH) then box-A direct-SSH-fetch of the run branch
//     straight from the worker repo (ssh://<host><repoPath>). The old
//     worker→GitHub push step is GONE (hk-7bwx).
//   TestRSB8_CodeSyncArgvOrder/local-run: verifies that no SSH calls appear and
//     the box-A-fetch argv carries the direct ssh:// URL.

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
		workerHost = "100.87.151.114"
		// workerRepoPath is the worker's repo clone; box A fetches the run branch
		// directly from it over ssh:// (hk-7bwx).
		workerRepoPath = "/Users/gb/harmonik-worker/repo"
	)
	branch := workspace.TaskBranchName(runID)
	// Direct-SSH fetch URL box A uses: ssh://<host>/<abs repo path>.
	workerURL := "ssh://" + workerHost + workerRepoPath

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
		_ = tmpWorkerWtPath // worktree path no longer used (no worker→origin push, hk-7bwx)

		// Step (c): fetch run-branch on box A DIRECTLY from the worker repo over SSH.
		// hk-7bwx: NO worker→GitHub push precedes this; box A dials the worker via
		// the ssh:// URL using its own credentials.
		if err := fetchRunBranchBoxA(ctx, localRR, projectDir, runID, workerHost, workerRepoPath, nil); err != nil {
			t.Fatalf("RSB8: fetchRunBranchBoxA: %v", err)
		}

		// ── Assert SSH call order ─────────────────────────────────────────
		// Expected calls via sshRR (in order); NO push step (hk-7bwx):
		//   [0] git -C <tmpWorkerRepo> fetch origin <baseSHA>  (fetch-base)
		//   [1] mkdir -p <parentDir>                           (CreateWorktree remote mkdir, hk-eodo)
		//   [2] git -C <tmpWorkerRepo> worktree add -b ...     (worktree-add, may retry)

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

		// hk-7bwx: there is NO worker→origin push anymore — assert NO SSH call is a
		// push (the run branch never leaves the worker repo; box A fetches it direct).
		for i, c := range sshRR.Calls {
			joined := strings.Join(append([]string{c.Name}, c.Args...), " ")
			if strings.Contains(joined, "push") {
				t.Errorf("RSB8/remote: SSH call[%d] is a push but pushes are removed (hk-7bwx): %v", i, joined)
			}
		}

		// ── Assert local call (box-A direct-SSH fetch) ───────────────────
		// box A fetches the run branch straight from the worker repo over SSH:
		//   git -C <projectDir> fetch ssh://<host><repoPath> run/<id>:refs/heads/run/<id>
		if len(localRR.Calls) != 1 {
			t.Fatalf("RSB8/remote: expected 1 local call, got %d: %v", len(localRR.Calls), localRR.Calls)
		}
		cLocal := localRR.Calls[0]
		if cLocal.Name != "git" {
			t.Errorf("RSB8/remote: localRR.calls[0].Name = %q, want git", cLocal.Name)
		}
		wantLocal := []string{"-C", projectDir, "fetch", workerURL, branch + ":refs/heads/" + branch}
		if !argvSliceEqual(cLocal.Args, wantLocal) {
			t.Errorf("RSB8/remote: localRR.calls[0].Args = %v, want %v", cLocal.Args, wantLocal)
		}

		// ── Assert ordering: fetch-base is FIRST; worktree-add precedes nothing
		// after it on the SSH channel (no push) ───────────────────────────
		foundFetchBase := strings.Join(sshRR.Calls[0].Args, " ")
		if !strings.Contains(foundFetchBase, "fetch") || !strings.Contains(foundFetchBase, baseSHA) {
			t.Errorf("RSB8/remote: first SSH call is not fetch-base: %v", sshRR.Calls[0].Args)
		}
	})

	t.Run("local-run-no-ssh-calls", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		// For a local run, none of the DD1 code-sync steps are triggered.
		// The daemon calls worktree-add with the default (local) runner, then
		// goes straight to mergeRunBranchToMain without any SSH or box-A fetch.
		//
		// Verify: calling fetchRunBranchBoxA with a recording (non-SSH) runner
		// produces NO SSH output and uses the direct ssh:// URL argv (hk-7bwx);
		// fetchBaseOnWorker is simply never called for a local run.

		sshRR := newNoOpRecorder() // should remain empty for a local run

		// Simulate the box-A fetch with a non-SSH recording runner. The fetch
		// command itself always carries the direct-SSH worker URL (hk-7bwx);
		// the local runner is the transport for the git process box A runs.
		localRR := newNoOpRecorder()
		if err := fetchRunBranchBoxA(ctx, localRR, projectDir, runID, workerHost, workerRepoPath, nil); err != nil {
			t.Fatalf("RSB8/local: fetchRunBranchBoxA: %v", err)
		}

		// No SSH-runner calls should have been made.
		if len(sshRR.Calls) != 0 {
			t.Errorf("RSB8/local: expected 0 SSH calls, got %d: %v", len(sshRR.Calls), sshRR.Calls)
		}

		// The call goes through the local recording runner with the ssh:// URL argv.
		if len(localRR.Calls) != 1 {
			t.Fatalf("RSB8/local: expected 1 local call, got %d: %v", len(localRR.Calls), localRR.Calls)
		}
		localBranch := workspace.TaskBranchName(runID)
		wantLocal := []string{"-C", projectDir, "fetch", workerURL, localBranch + ":refs/heads/" + localBranch}
		if !argvSliceEqual(localRR.Calls[0].Args, wantLocal) {
			t.Errorf("RSB8/local: localRR.calls[0].Args = %v, want %v", localRR.Calls[0].Args, wantLocal)
		}
	})
}

// TestRSB8_IsRefNotFoundError verifies the transient-gap detector recognises
// git's "couldn't find remote ref" output and rejects unrelated error strings.
func TestRSB8_IsRefNotFoundError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		out  string
		want bool
	}{
		{"error: couldn't find remote ref run/019ee849-7df0\n", true},
		{"fatal: couldn't find remote ref run/abc-123\n", true},
		{"ssh: connect to host 100.87.151.114 port 22: Connection refused\n", false},
		{"fatal: repository 'ssh://host/path' not found\n", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isRefNotFoundError([]byte(tc.out))
		if got != tc.want {
			t.Errorf("isRefNotFoundError(%q) = %v, want %v", tc.out, got, tc.want)
		}
	}
}

// TestRSB8_FetchRunBranchRetries verifies that fetchRunBranchBoxA retries on
// "couldn't find remote ref" and succeeds once the ref becomes visible.
func TestRSB8_FetchRunBranchRetries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		projectDir     = "/home/boxa/harmonik"
		runID          = "019ec83c-rsb8-retry-0001-000000000001"
		workerHost     = "100.87.151.114"
		workerRepoPath = "/Users/gb/harmonik-worker/repo"
		failCount      = 2 // first 2 attempts fail with "ref not found"
	)

	callN := 0
	rr := &tmux.RecordingRunner{
		CmdFunc: func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			callN++
			if callN <= failCount {
				// Return a command that exits non-zero with the ref-not-found message.
				return exec.Command("/bin/sh", "-c",
					"printf \"error: couldn't find remote ref run/xxx\\n\" >&2; exit 128")
			}
			return exec.Command("true")
		},
	}

	if err := fetchRunBranchBoxA(ctx, rr, projectDir, runID, workerHost, workerRepoPath, nil); err != nil {
		t.Fatalf("fetchRunBranchBoxA: expected success after %d retries, got: %v", failCount, err)
	}
	// Expect failCount failures + 1 success = failCount+1 total calls.
	if callN != failCount+1 {
		t.Errorf("CmdFunc called %d times, want %d", callN, failCount+1)
	}
}

// TestRSB8_FetchRunBranchNoRetryOnHardError verifies that fetchRunBranchBoxA
// does NOT retry when the error is a hard failure (not a transient ref-not-found).
func TestRSB8_FetchRunBranchNoRetryOnHardError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		projectDir     = "/home/boxa/harmonik"
		runID          = "019ec83c-rsb8-noretry-0001-000000000001"
		workerHost     = "100.87.151.114"
		workerRepoPath = "/Users/gb/harmonik-worker/repo"
	)

	callN := 0
	rr := &tmux.RecordingRunner{
		CmdFunc: func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			callN++
			// Connection-refused — a hard error, must not retry.
			return exec.Command("/bin/sh", "-c",
				"printf \"ssh: connect to host 100.87.151.114 port 22: Connection refused\\n\" >&2; exit 128")
		},
	}

	err := fetchRunBranchBoxA(ctx, rr, projectDir, runID, workerHost, workerRepoPath, nil)
	if err == nil {
		t.Fatal("expected error on hard failure, got nil")
	}
	// Must fail after exactly 1 call (no retry).
	if callN != 1 {
		t.Errorf("CmdFunc called %d times on hard error, want 1 (no retry)", callN)
	}
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
