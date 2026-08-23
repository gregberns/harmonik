package codesync

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workspace"
)

func newNoOpRecorder() *tmux.RecordingRunner {
	return &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "true")
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
	workerURL := "ssh://" + workerHost + workerRepoPath

	t.Run("remote-run", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		tmpWorkerRepo := t.TempDir()
		tmpWorkerWtPath := workspace.WorktreePath(tmpWorkerRepo, runID, workspace.NoWorktreeRootOverride())

		sshRR := newNoOpRecorder()
		localRR := newNoOpRecorder()

		if err := fetchBaseOnWorker(ctx, sshRR, tmpWorkerRepo, baseSHA); err != nil {
			t.Fatalf("RSB8: fetchBaseOnWorker: %v", err)
		}

		wtCfg := workspace.NoWorktreeRootOverride().WithRunner(sshRR)
		if err := workspace.CreateWorktree(ctx, tmpWorkerRepo, runID, baseSHA, wtCfg); err == nil ||
			!strings.Contains(err.Error(), "empty HEAD") {
			t.Fatalf("RSB8: CreateWorktree error = %v, want mocked-command empty HEAD verification failure", err)
		}
		_ = tmpWorkerWtPath // worktree path no longer used (no worker→origin push, hk-7bwx)

		if err := FetchRunBranchBoxA(ctx, localRR, projectDir, runID, workerHost, workerRepoPath, nil); err != nil {
			t.Fatalf("RSB8: FetchRunBranchBoxA: %v", err)
		}

		if len(sshRR.Calls) < 4 {
			t.Fatalf("RSB8/remote: expected at least 4 SSH calls, got %d: %v", len(sshRR.Calls), sshRR.Calls)
		}

		c0 := sshRR.Calls[0]
		if c0.Name != "git" {
			t.Errorf("RSB8/remote: calls[0].Name = %q, want git", c0.Name)
		}
		wantC0 := []string{"-C", tmpWorkerRepo, "fetch", "origin", baseSHA}
		if !argvSliceEqual(c0.Args, wantC0) {
			t.Errorf("RSB8/remote: calls[0].Args = %v, want %v", c0.Args, wantC0)
		}

		c1 := sshRR.Calls[1]
		if c1.Name != "git" {
			t.Errorf("RSB8/remote: calls[1].Name = %q, want git", c1.Name)
		}
		wantC1 := []string{"-C", tmpWorkerRepo, "cat-file", "-t", baseSHA}
		if !argvSliceEqual(c1.Args, wantC1) {
			t.Errorf("RSB8/remote: calls[1].Args = %v, want %v", c1.Args, wantC1)
		}

		c2 := sshRR.Calls[2]
		if c2.Name != "mkdir" {
			t.Errorf("RSB8/remote: calls[2].Name = %q, want mkdir", c2.Name)
		}
		if len(c2.Args) < 2 || c2.Args[0] != "-p" {
			t.Errorf("RSB8/remote: calls[2].Args = %v, want [-p <parentDir>]", c2.Args)
		}

		c3 := sshRR.Calls[3]
		if c3.Name != "git" {
			t.Errorf("RSB8/remote: calls[3].Name = %q, want git", c3.Name)
		}
		if len(c3.Args) < 4 || c3.Args[0] != "-C" || c3.Args[1] != tmpWorkerRepo ||
			c3.Args[2] != "worktree" || c3.Args[3] != "add" {
			t.Errorf("RSB8/remote: calls[3].Args = %v, want [-C <tmpWorkerRepo> worktree add ...]", c3.Args)
		}

		for i, c := range sshRR.Calls {
			joined := strings.Join(append([]string{c.Name}, c.Args...), " ")
			if strings.Contains(joined, "push") {
				t.Errorf("RSB8/remote: SSH call[%d] is a push but pushes are removed (hk-7bwx): %v", i, joined)
			}
		}

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

		foundFetchBase := strings.Join(sshRR.Calls[0].Args, " ")
		if !strings.Contains(foundFetchBase, "fetch") || !strings.Contains(foundFetchBase, baseSHA) {
			t.Errorf("RSB8/remote: first SSH call is not fetch-base: %v", sshRR.Calls[0].Args)
		}
	})

	t.Run("local-run-no-ssh-calls", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		sshRR := newNoOpRecorder() // should remain empty for a local run

		localRR := newNoOpRecorder()
		if err := FetchRunBranchBoxA(ctx, localRR, projectDir, runID, workerHost, workerRepoPath, nil); err != nil {
			t.Fatalf("RSB8/local: FetchRunBranchBoxA: %v", err)
		}

		if len(sshRR.Calls) != 0 {
			t.Errorf("RSB8/local: expected 0 SSH calls, got %d: %v", len(sshRR.Calls), sshRR.Calls)
		}

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

// TestRSB8_FetchRunBranchRetries verifies that FetchRunBranchBoxA retries on
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
		CmdFunc: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			callN++
			if callN <= failCount {
				return exec.CommandContext(ctx, "/bin/sh", "-c",
					"printf \"error: couldn't find remote ref run/xxx\\n\" >&2; exit 128")
			}
			return exec.CommandContext(ctx, "true")
		},
	}

	if err := FetchRunBranchBoxA(ctx, rr, projectDir, runID, workerHost, workerRepoPath, nil); err != nil {
		t.Fatalf("FetchRunBranchBoxA: expected success after %d retries, got: %v", failCount, err)
	}
	if callN != failCount+1 {
		t.Errorf("CmdFunc called %d times, want %d", callN, failCount+1)
	}
}

// TestRSB8_FetchRunBranchNoRetryOnHardError verifies that FetchRunBranchBoxA
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
		CmdFunc: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			callN++
			return exec.CommandContext(ctx, "/bin/sh", "-c",
				"printf \"ssh: connect to host 100.87.151.114 port 22: Connection refused\\n\" >&2; exit 128")
		},
	}

	err := FetchRunBranchBoxA(ctx, rr, projectDir, runID, workerHost, workerRepoPath, nil)
	if err == nil {
		t.Fatal("expected error on hard failure, got nil")
	}
	if callN != 1 {
		t.Errorf("CmdFunc called %d times on hard error, want 1 (no retry)", callN)
	}
}

// TestEnsureBaseOnWorker_PushFallback verifies that EnsureBaseOnWorker falls
// back to pushBaseToWorker when fetch origin exits 0 but the SHA is absent on
// the worker (hk-2hfyt: unpushed base commit).
//
// Scenario:
//   - The worker-side runner (sshRR) simulates `git fetch origin <sha>` exiting
//     0 (silent no-op) followed by `git cat-file -t <sha>` exiting 128 (SHA
//     absent). fetchBaseOnWorker returns errBaseSHAAbsent.
//   - EnsureBaseOnWorker detects errBaseSHAAbsent and calls pushBaseToWorker
//     via the local runner (localRR).
//   - Verify localRR received exactly one call: `git push ssh://<host>/<repo> <sha>:refs/harmonik/base`.
func TestEnsureBaseOnWorker_PushFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		workerRepoPath = "/Users/gb/harmonik-worker/repo"
		workerHost     = "100.87.151.114"
		boxAProjectDir = "/home/boxa/harmonik"
		baseSHA        = "aabbccddaabbccddaabbccddaabbccddaabbccdd"
	)

	fetchCalled, catFileCalled := 0, 0
	sshRR := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			for _, a := range args {
				if a == "fetch" {
					fetchCalled++
					return exec.CommandContext(ctx, "true") // exit 0 — silent no-op
				}
				if a == "cat-file" {
					catFileCalled++
					return exec.CommandContext(ctx, "/bin/sh", "-c",
						"printf 'fatal: git cat-file: not in the object database\n' >&2; exit 128")
				}
			}
			return exec.CommandContext(ctx, "true")
		},
	}

	localRR := newNoOpRecorder()

	err := EnsureBaseOnWorker(ctx, sshRR, workerRepoPath, baseSHA,
		localRR, boxAProjectDir, workerHost, nil)
	if err != nil {
		t.Fatalf("EnsureBaseOnWorker: expected nil after push fallback, got: %v", err)
	}

	if fetchCalled != 1 {
		t.Errorf("fetch called %d times, want 1", fetchCalled)
	}
	if catFileCalled != 1 {
		t.Errorf("cat-file called %d times, want 1", catFileCalled)
	}

	if len(localRR.Calls) != 1 {
		t.Fatalf("localRR: expected 1 push call, got %d: %v", len(localRR.Calls), localRR.Calls)
	}
	pushCall := localRR.Calls[0]
	if pushCall.Name != "git" {
		t.Errorf("localRR push call Name = %q, want git", pushCall.Name)
	}
	wantURL := "ssh://" + workerHost + workerRepoPath
	wantRefspec := baseSHA + ":refs/harmonik/base"
	wantArgs := []string{"-C", boxAProjectDir, "push", wantURL, wantRefspec}
	if !argvSliceEqual(pushCall.Args, wantArgs) {
		t.Errorf("localRR push call Args = %v\nwant %v", pushCall.Args, wantArgs)
	}
}

// TestEnsureBaseOnWorker_NoFallbackOnConnectionError verifies that
// EnsureBaseOnWorker does NOT attempt the push fallback when fetchBaseOnWorker
// returns a hard connection error (not errBaseSHAAbsent).
func TestEnsureBaseOnWorker_NoFallbackOnConnectionError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sshRR := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "/bin/sh", "-c",
				"printf 'ssh: connect to host 100.87.151.114 port 22: Connection refused\n' >&2; exit 128")
		},
	}
	localRR := newNoOpRecorder()

	err := EnsureBaseOnWorker(ctx, sshRR, "/repo", "aabbccdd",
		localRR, "/project", "100.87.151.114", nil)
	if err == nil {
		t.Fatal("expected error on SSH connection failure, got nil")
	}
	if len(localRR.Calls) != 0 {
		t.Errorf("localRR: expected 0 calls on connection error, got %d: %v", len(localRR.Calls), localRR.Calls)
	}
}

// TestPushBaseToWorker_ArgvShape verifies pushBaseToWorker produces the expected
// git push argv for the direct box-A→worker transfer (hk-2hfyt).
func TestPushBaseToWorker_ArgvShape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		boxAProjectDir = "/home/boxa/harmonik"
		workerHost     = "100.87.151.114"
		workerRepoPath = "/Users/gb/harmonik-worker/repo"
		baseSHA        = "aabbccddaabbccddaabbccddaabbccddaabbccdd"
	)

	rr := newNoOpRecorder()
	if err := pushBaseToWorker(ctx, rr, boxAProjectDir, workerHost, workerRepoPath, baseSHA, nil); err != nil {
		t.Fatalf("pushBaseToWorker: %v", err)
	}

	if len(rr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %v", len(rr.Calls), rr.Calls)
	}
	c := rr.Calls[0]
	if c.Name != "git" {
		t.Errorf("Name = %q, want git", c.Name)
	}
	wantURL := "ssh://" + workerHost + workerRepoPath
	wantRefspec := baseSHA + ":refs/harmonik/base"
	want := []string{"-C", boxAProjectDir, "push", wantURL, wantRefspec}
	if !argvSliceEqual(c.Args, want) {
		t.Errorf("Args = %v\nwant %v", c.Args, want)
	}
}

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
