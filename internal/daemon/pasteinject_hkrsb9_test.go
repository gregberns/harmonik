package daemon

// pasteinject_hkrsb9_test.go — unit tests for rs B9: liveness and commit
// probes routed through the run's CommandRunner.
//
// Problem addressed: hasAnyDirectChild (pgrep -P), commandMatchesLiveAgent
// (ps -o comm=), resolveWorktreeHEAD and worktreeActivityFingerprint all used
// bare exec.Command / exec.CommandContext, which cannot be redirected to a
// remote host for remote-substrate workers.
//
// The fix adds runner-parameterized variants (*Via functions) and routes them
// through the CommandRunner stored on perRunSubstrate.  pasteInjectQuitOnCommit
// probes qs for commandRunnerProvider and uses the same runner for git probes.
//
// Test matrix:
//   - RSB9_HasAnyDirectChildVia_LocalRunner: canned pgrep exit-0 drives true.
//   - RSB9_HasAnyDirectChildVia_SSHArgv: SSHRunner argv is ssh host -- pgrep -P <pid>.
//   - RSB9_CommandMatchesLiveAgentVia_Matches: canned ps output "claude" → true.
//   - RSB9_CommandMatchesLiveAgentVia_NoMatch: canned ps output "bash" → false.
//   - RSB9_ResolveWorktreeHEADVia_RealGit: real local git repo → correct SHA.
//   - RSB9_ResolveWorktreeHEADVia_SSHArgv: SSHRunner argv is ssh host -- git -C <path> rev-parse HEAD.
//   - RSB9_WorktreeActivityFingerprintVia_RealGit: stable fingerprint on clean repo.
//   - RSB9_CommitDetect_ViaRunner: pasteInjectQuitOnCommit uses runner from qs.
//
// Bead: hk-rs-b9-liveness-1m9n.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// cannedExitRunner returns a RecordingRunner whose CmdFunc always produces a
// cmd that exits with the given code and writes output to stdout.  Used to
// simulate process/git probe responses without touching real OS resources.
func cannedExitRunner(exitCode int, stdout string) *tmux.RecordingRunner {
	return &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Use `sh -c` to produce the desired exit code and output portably.
			script := fmt.Sprintf("printf '%%s' %q; exit %d", stdout, exitCode)
			return exec.CommandContext(ctx, "sh", "-c", script)
		},
	}
}

// initGitRepo creates a temporary git repo with one commit and returns the
// repo path and HEAD SHA.
func initGitRepoB9(t *testing.T) (repoPath, headSHA string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	sha := strings.TrimSpace(string(out))
	return dir, sha
}

// ─────────────────────────────────────────────────────────────────────────────
// hasAnyDirectChildVia
// ─────────────────────────────────────────────────────────────────────────────

func TestRSB9_HasAnyDirectChildVia_LocalRunner(t *testing.T) {
	// pgrep exit-0 → hasAnyDirectChildVia returns true.
	rr := cannedExitRunner(0, "12345\n")
	got := hasAnyDirectChildVia(context.Background(), rr, 999)
	if !got {
		t.Error("RSB9: hasAnyDirectChildVia: want true for pgrep exit-0, got false")
	}
	if len(rr.Calls) != 1 || rr.Calls[0].Name != "pgrep" {
		t.Errorf("RSB9: expected pgrep call, got %v", rr.Calls)
	}
	args := rr.Calls[0].Args
	if len(args) < 2 || args[0] != "-P" || args[1] != "999" {
		t.Errorf("RSB9: pgrep args = %v, want [-P 999]", args)
	}
}

func TestRSB9_HasAnyDirectChildVia_ExitNonZero(t *testing.T) {
	// pgrep exit-1 → hasAnyDirectChildVia returns false.
	rr := cannedExitRunner(1, "")
	got := hasAnyDirectChildVia(context.Background(), rr, 999)
	if got {
		t.Error("RSB9: hasAnyDirectChildVia: want false for pgrep exit-1, got true")
	}
}

func TestRSB9_HasAnyDirectChildVia_SSHArgv(t *testing.T) {
	// Under SSHRunner the command recorded by RecordingRunner is still
	// (pgrep, [-P pid]) — SSHRunner wraps it at exec time, not at record time.
	sshHost := "worker@remote.internal"
	ssh := tmux.SSHRunner{Host: sshHost}
	rr := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return ssh.Command(ctx, name, args...)
		},
	}
	// We don't run the command (no real ssh); just verify the recorded argv shape.
	_ = hasAnyDirectChildVia(context.Background(), rr, 42)

	if len(rr.Calls) != 1 {
		t.Fatalf("RSB9/ssh: want 1 recorded call, got %d", len(rr.Calls))
	}
	call := rr.Calls[0]
	if call.Name != "pgrep" {
		t.Errorf("RSB9/ssh: call.Name = %q, want pgrep", call.Name)
	}
	if len(call.Args) < 2 || call.Args[0] != "-P" || call.Args[1] != "42" {
		t.Errorf("RSB9/ssh: pgrep args = %v, want [-P 42]", call.Args)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// commandMatchesLiveAgentVia
// ─────────────────────────────────────────────────────────────────────────────

func TestRSB9_CommandMatchesLiveAgentVia_Matches(t *testing.T) {
	rr := cannedExitRunner(0, "claude\n")
	got := commandMatchesLiveAgentVia(context.Background(), rr, 999, []string{"claude", "node"})
	if !got {
		t.Error("RSB9: commandMatchesLiveAgentVia: want true for 'claude' output, got false")
	}
	if len(rr.Calls) != 1 || rr.Calls[0].Name != "ps" {
		t.Errorf("RSB9: expected ps call, got %v", rr.Calls)
	}
}

func TestRSB9_CommandMatchesLiveAgentVia_NoMatch(t *testing.T) {
	rr := cannedExitRunner(0, "bash\n")
	got := commandMatchesLiveAgentVia(context.Background(), rr, 999, []string{"claude", "node"})
	if got {
		t.Error("RSB9: commandMatchesLiveAgentVia: want false for 'bash' output, got true")
	}
}

func TestRSB9_CommandMatchesLiveAgentVia_PSArgv(t *testing.T) {
	rr := cannedExitRunner(0, "claude\n")
	_ = commandMatchesLiveAgentVia(context.Background(), rr, 777, []string{"claude"})
	if len(rr.Calls) != 1 {
		t.Fatalf("RSB9: want 1 call, got %d", len(rr.Calls))
	}
	call := rr.Calls[0]
	if call.Name != "ps" {
		t.Errorf("RSB9: call.Name = %q, want ps", call.Name)
	}
	// Expect: ps -o comm= -p 777
	wantArgs := []string{"-o", "comm=", "-p", "777"}
	if strings.Join(call.Args, " ") != strings.Join(wantArgs, " ") {
		t.Errorf("RSB9: ps args = %v, want %v", call.Args, wantArgs)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// resolveWorktreeHEADVia
// ─────────────────────────────────────────────────────────────────────────────

func TestRSB9_ResolveWorktreeHEADVia_RealGit(t *testing.T) {
	t.Parallel()
	repoPath, wantSHA := initGitRepoB9(t)

	rr := &tmux.RecordingRunner{} // nil CmdFunc → real git runs
	got, err := resolveWorktreeHEADVia(context.Background(), rr, repoPath)
	if err != nil {
		t.Fatalf("RSB9: resolveWorktreeHEADVia: %v", err)
	}
	if got != wantSHA {
		t.Errorf("RSB9: HEAD = %q, want %q", got, wantSHA)
	}
	// Verify the call used git -C <path> rev-parse HEAD.
	if len(rr.Calls) < 1 || rr.Calls[0].Name != "git" {
		t.Fatalf("RSB9: expected git call, got %v", rr.Calls)
	}
	args := rr.Calls[0].Args
	if len(args) < 4 || args[0] != "-C" || args[1] != repoPath || args[2] != "rev-parse" || args[3] != "HEAD" {
		t.Errorf("RSB9: git args = %v, want [-C %s rev-parse HEAD]", args, repoPath)
	}
}

func TestRSB9_ResolveWorktreeHEADVia_SSHArgv(t *testing.T) {
	sshHost := "worker@remote.internal"
	ssh := tmux.SSHRunner{Host: sshHost}
	rr := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return ssh.Command(ctx, name, args...)
		},
	}
	// Will fail (no real ssh) but we only care about the recorded argv.
	_, _ = resolveWorktreeHEADVia(context.Background(), rr, "/remote/path/wt")

	if len(rr.Calls) < 1 {
		t.Fatal("RSB9/ssh: no calls recorded")
	}
	call := rr.Calls[0]
	if call.Name != "git" {
		t.Errorf("RSB9/ssh: name = %q, want git", call.Name)
	}
	if len(call.Args) < 4 || call.Args[0] != "-C" || call.Args[2] != "rev-parse" || call.Args[3] != "HEAD" {
		t.Errorf("RSB9/ssh: git args = %v, want [-C <path> rev-parse HEAD]", call.Args)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// worktreeActivityFingerprintVia
// ─────────────────────────────────────────────────────────────────────────────

func TestRSB9_WorktreeActivityFingerprintVia_RealGit(t *testing.T) {
	t.Parallel()
	repoPath, _ := initGitRepoB9(t)

	rr := &tmux.RecordingRunner{}
	fp1, ok1 := worktreeActivityFingerprintVia(context.Background(), rr, repoPath)
	if !ok1 || fp1 == "" {
		t.Fatalf("RSB9: worktreeActivityFingerprintVia: ok=%v fp=%q", ok1, fp1)
	}
	// A stable repo fingerprint should be deterministic.
	fp2, ok2 := worktreeActivityFingerprintVia(context.Background(), rr, repoPath)
	if !ok2 || fp1 != fp2 {
		t.Errorf("RSB9: fingerprint changed on stable repo: %q vs %q", fp1, fp2)
	}
	// Verify git commands used -C <path> form.
	var gitCalls []tmux.RecordingCall
	for _, c := range rr.Calls {
		if c.Name == "git" {
			gitCalls = append(gitCalls, c)
		}
	}
	if len(gitCalls) == 0 {
		t.Fatal("RSB9: no git calls recorded")
	}
	for _, c := range gitCalls {
		if len(c.Args) < 2 || c.Args[0] != "-C" || c.Args[1] != repoPath {
			t.Errorf("RSB9: git call without -C <path>: %v", c.Args)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// pasteInjectQuitOnCommit routes probes through commandRunnerProvider
// ─────────────────────────────────────────────────────────────────────────────

// rsb9QuitSenderWithRunner is a minimal quitSender + commandRunnerProvider stub
// for testing that pasteInjectQuitOnCommit picks up the runner from qs.
type rsb9QuitSenderWithRunner struct {
	runner   tmux.CommandRunner
	quitErr  error
	quitSent chan struct{}
}

func (q *rsb9QuitSenderWithRunner) SendQuitToLastPane(_ context.Context) error {
	if q.quitSent != nil {
		select {
		case q.quitSent <- struct{}{}:
		default:
		}
	}
	return q.quitErr
}

func (q *rsb9QuitSenderWithRunner) commandRunner() tmux.CommandRunner {
	return q.runner
}

func TestRSB9_CommitDetect_ViaRunner(t *testing.T) {
	// pasteInjectQuitOnCommit should use the runner from qs (commandRunnerProvider)
	// to resolve HEAD. We verify this by giving qs a RecordingRunner and asserting
	// that a git -C <path> rev-parse HEAD call is recorded.

	repoPath, initialSHA := initGitRepoB9(t)

	// Make a new commit so HEAD changes from initialSHA.
	f := filepath.Join(repoPath, "file2.txt")
	if err := os.WriteFile(f, []byte("change"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("add", ".")
	runGit("commit", "-m", "second")

	rr := &tmux.RecordingRunner{} // nil CmdFunc → real git
	quitSent := make(chan struct{}, 1)
	qs := &rsb9QuitSenderWithRunner{
		runner:   rr,
		quitSent: quitSent,
	}

	// Shorten timeouts so the test finishes in milliseconds.
	oldPollInterval := commitPollInterval
	oldPollTimeout := commitPollTimeout
	oldHardCeiling := commitHardCeiling
	oldBriefDelivered := briefDeliveredTimeout
	commitPollInterval = 5 * time.Millisecond
	commitPollTimeout = 5 * time.Second
	commitHardCeiling = 10 * time.Second
	briefDeliveredTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		commitPollInterval = oldPollInterval
		commitPollTimeout = oldPollTimeout
		commitHardCeiling = oldHardCeiling
		briefDeliveredTimeout = oldBriefDelivered
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go pasteInjectQuitOnCommit(ctx, qs, nil, repoPath, initialSHA, nil, nil, nil, nil, core.RunID{})

	select {
	case <-quitSent:
		// /quit was sent after commit detected — success.
	case <-ctx.Done():
		t.Fatal("RSB9/commit-detect: timed out waiting for /quit after new commit")
	}

	// Verify that the runner was used: at least one `git -C <repoPath> rev-parse HEAD` call.
	found := false
	for _, c := range rr.Calls {
		if c.Name == "git" && len(c.Args) >= 4 &&
			c.Args[0] == "-C" && c.Args[1] == repoPath &&
			c.Args[2] == "rev-parse" && c.Args[3] == "HEAD" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("RSB9/commit-detect: no `git -C %s rev-parse HEAD` call recorded; calls = %v", repoPath, rr.Calls)
	}
}
