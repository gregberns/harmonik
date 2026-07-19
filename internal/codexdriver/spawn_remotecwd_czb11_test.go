package codexdriver_test

// spawn_remotecwd_czb11_test.go — hk-czb11: codexdriver.spawn must be
// remote-cwd-aware. When the wired Runner advertises RemoteCwdRunner (an ssh
// transport), spawn applies SubstrateSpawn.Cwd REMOTELY (via CommandInDir) and
// leaves the LOCAL exec.Cmd.Dir unset — a remote worktree path is never assigned
// to the local ssh process's cwd (which would fork/exec-ENOENT). A LOCAL runner
// (no CommandInDir) keeps the direct exec.Cmd.Dir = Cwd path.

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexdriver"
	"github.com/gregberns/harmonik/internal/handler"
)

// twinCmd builds the in-process twin subprocess the driver session drives, so the
// spawned session has a clean, protocol-speaking lifecycle (TestMain runs the
// twin when CODEXDRIVER_TWIN=1). The driver assigns cmd.Env from in.Env after the
// runner returns, so the twin env below reaches the process on both paths.
func twinCmd(ctx context.Context) *exec.Cmd {
	//nolint:gosec // G204: test twin subprocess — os.Args[0] is this test binary, not user input
	return exec.CommandContext(ctx, os.Args[0], "-test.run=NONE")
}

type czb11InDirCall struct {
	dir  string
	name string
	args []string
}

// czb11RemoteRunner implements BOTH Command and CommandInDir, so it satisfies the
// codexdriver RemoteCwdRunner capability. It records which path spawn took.
type czb11RemoteRunner struct {
	mu           sync.Mutex
	inDirCalls   []czb11InDirCall
	commandCalls int
	lastCmd      *exec.Cmd
}

func (r *czb11RemoteRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commandCalls++
	c := twinCmd(ctx)
	r.lastCmd = c
	return c
}

func (r *czb11RemoteRunner) CommandInDir(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inDirCalls = append(r.inDirCalls, czb11InDirCall{dir: dir, name: name, args: append([]string(nil), args...)})
	c := twinCmd(ctx)
	r.lastCmd = c
	return c
}

// czb11LocalRunner implements ONLY Command (no CommandInDir), so it is NOT a
// RemoteCwdRunner — spawn must take the local exec.Cmd.Dir path.
type czb11LocalRunner struct {
	mu           sync.Mutex
	commandCalls int
	lastCmd      *exec.Cmd
}

func (r *czb11LocalRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commandCalls++
	c := twinCmd(ctx)
	r.lastCmd = c
	return c
}

func czb11Cleanup(t *testing.T, sess handler.SubstrateSession) {
	t.Helper()
	t.Cleanup(func() {
		_ = sess.Kill(context.Background()) //nolint:errcheck // test cleanup, unactionable
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = sess.Wait(ctx) //nolint:errcheck // test cleanup, unactionable
	})
}

func TestSpawn_RemoteRunner_UsesRemoteCwd_LocalDirUnset_czb11(t *testing.T) {
	runner := &czb11RemoteRunner{}
	sub := codexdriver.NewCodexSubstrate(codexdriver.Options{Runner: runner})

	// A REMOTE worktree path that does NOT exist locally: with the bug (local
	// cmd.Dir = this path) cmd.Start would fork/exec-ENOENT. The fix leaves the
	// local Dir unset, so the twin still spawns cleanly.
	const remoteCwd = "/box-b/.harmonik/worktrees/run-czb11/does-not-exist-locally"
	sess, err := sub.SpawnWindow(context.Background(), handler.SubstrateSpawn{
		WindowName: "twin",
		Argv:       []string{os.Args[0], "-test.run=NONE"},
		Cwd:        remoteCwd,
		Env:        append(os.Environ(), "CODEXDRIVER_TWIN=1", "CODEXDRIVER_TWIN_MODE=happy"),
	})
	if err != nil {
		t.Fatalf("SpawnWindow (remote runner, non-existent remote cwd): %v", err)
	}
	czb11Cleanup(t, sess)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.inDirCalls) != 1 {
		t.Fatalf("CommandInDir called %d times; want 1 (a RemoteCwdRunner spawn must route cwd remotely)", len(runner.inDirCalls))
	}
	if got := runner.inDirCalls[0].dir; got != remoteCwd {
		t.Errorf("CommandInDir dir = %q; want the remote cwd %q", got, remoteCwd)
	}
	if runner.commandCalls != 0 {
		t.Errorf("Command (local exec.Cmd.Dir path) called %d times; want 0 for a RemoteCwdRunner", runner.commandCalls)
	}
	if runner.lastCmd.Dir != "" {
		t.Errorf("local cmd.Dir = %q; want UNSET on the remote path — the remote worktree path must never be a local Dir (hk-czb11)", runner.lastCmd.Dir)
	}
}

// TestSpawn_RemoteRunner_ForwardsEnvViaArgv_hkokqyx asserts the codex remote path
// delivers in.Env to the remote child by rewriting the launch into an
// `env KEY=VAL … <binary> <args>` argv (handler.RemoteExecArgv) — ssh does NOT
// forward the local process env, so the pre-fix `cmd.Env = in.Env` never reached
// the remote codex (hk-okqyx, the codex twin of hk-qxvc2). A forwarded var must
// appear before the binary; an empty deny-list override must be dropped (it would
// clobber the worker's ambient credential).
func TestSpawn_RemoteRunner_ForwardsEnvViaArgv_hkokqyx(t *testing.T) {
	runner := &czb11RemoteRunner{}
	sub := codexdriver.NewCodexSubstrate(codexdriver.Options{Runner: runner})

	const remoteCwd = "/box-b/.harmonik/worktrees/run-okqyx/does-not-exist-locally"
	const marker = "HK_REMOTE_MARKER=present"
	const emptyCred = "HK_EMPTY_CRED="
	binary := os.Args[0]
	sess, err := sub.SpawnWindow(context.Background(), handler.SubstrateSpawn{
		WindowName: "twin",
		Argv:       []string{binary, "-test.run=NONE"},
		Cwd:        remoteCwd,
		Env:        append(os.Environ(), "CODEXDRIVER_TWIN=1", "CODEXDRIVER_TWIN_MODE=happy", marker, emptyCred),
	})
	if err != nil {
		t.Fatalf("SpawnWindow (remote runner, env-forward): %v", err)
	}
	czb11Cleanup(t, sess)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.inDirCalls) != 1 {
		t.Fatalf("CommandInDir called %d times; want 1", len(runner.inDirCalls))
	}
	call := runner.inDirCalls[0]
	if call.name != "env" {
		t.Fatalf("CommandInDir name = %q; want \"env\" (in.Env must be forwarded via an env-prefix argv) — hk-okqyx", call.name)
	}
	markerIdx, binaryIdx := -1, -1
	for i, a := range call.args {
		switch a {
		case marker:
			markerIdx = i
		case binary:
			binaryIdx = i
		case emptyCred:
			t.Errorf("empty deny-list override %q was forwarded to the remote codex; it must be dropped (would clobber ambient credential) — hk-okqyx", emptyCred)
		}
	}
	if markerIdx < 0 {
		t.Errorf("forwarded var %q absent from remote argv %v — hk-okqyx", marker, call.args)
	}
	if binaryIdx < 0 {
		t.Fatalf("binary %q absent from remote argv %v", binary, call.args)
	}
	if markerIdx >= 0 && markerIdx > binaryIdx {
		t.Errorf("forwarded var %q at idx %d must precede the binary at idx %d (env KEY=VAL … <binary>) — hk-okqyx", marker, markerIdx, binaryIdx)
	}
}

func TestSpawn_LocalRunner_SetsLocalCmdDir_czb11(t *testing.T) {
	runner := &czb11LocalRunner{}
	sub := codexdriver.NewCodexSubstrate(codexdriver.Options{Runner: runner})

	// A LOCAL runner: Cwd is a real local dir, applied as exec.Cmd.Dir (byte-
	// identical to the pre-fix path). Must exist so cmd.Start's chdir succeeds.
	localCwd := t.TempDir()
	sess, err := sub.SpawnWindow(context.Background(), handler.SubstrateSpawn{
		WindowName: "twin",
		Argv:       []string{os.Args[0], "-test.run=NONE"},
		Cwd:        localCwd,
		Env:        append(os.Environ(), "CODEXDRIVER_TWIN=1", "CODEXDRIVER_TWIN_MODE=happy"),
	})
	if err != nil {
		t.Fatalf("SpawnWindow (local runner): %v", err)
	}
	czb11Cleanup(t, sess)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.commandCalls != 1 {
		t.Fatalf("Command called %d times; want 1 (a non-RemoteCwdRunner uses the local exec.Cmd.Dir path)", runner.commandCalls)
	}
	if runner.lastCmd.Dir != localCwd {
		t.Errorf("local cmd.Dir = %q; want the local Cwd %q (local runner path unchanged)", runner.lastCmd.Dir, localCwd)
	}
}
