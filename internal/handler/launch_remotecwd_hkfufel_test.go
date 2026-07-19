package handler_test

// launch_remotecwd_hkfufel_test.go — hk-fufel: handler.Launch's DIRECT-EXEC path
// (Substrate == nil, the path SessionIDCaptured codex/pi take) must apply the
// launch WorkDir on the REMOTE worker for a worker-tunneling runner — via
// CommandInDir, leaving the LOCAL exec.Cmd.Dir UNSET — NOT set the local ssh
// process's cmd.Dir to the REMOTE worktree path (which fork/exec-ENOENTs: the
// crit3 crash `fork/exec /usr/bin/ssh: no such file or directory`).
//
// This drives the REAL handler.Launch exec branch (no fake-registry / direct-
// SpawnWindow shortcut): a RemoteCwdRunner-capable runner + Substrate:nil +
// WorkDir=<remote path>. RED without the fix (exec path calls Command() then sets
// cmd.Dir = remote WorkDir), GREEN with it (CommandInDir taken, cmd.Dir unset).

import (
	"context"
	"os/exec"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// recordingRemoteRunner is a handler.CommandRunner that ALSO implements
// CommandInDir, so it satisfies handler.RemoteCwdRunner (structurally, exactly as
// tmux.SSHRunner does). It records which method the exec path took and the *exec.Cmd
// it returned (so the test can inspect cmd.Dir after Launch). Both methods return a
// benign local process that emits agent_ready so the watcher closes cleanly.
type recordingRemoteRunner struct {
	mu           sync.Mutex
	commandCalls int
	inDirCalls   int
	inDirArg     string
	lastCmd      *exec.Cmd
}

func (r *recordingRemoteRunner) benign(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", `printf '{"type":"agent_ready"}\n'`)
}

func (r *recordingRemoteRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commandCalls++
	c := r.benign(ctx)
	r.lastCmd = c
	return c
}

func (r *recordingRemoteRunner) CommandInDir(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inDirCalls++
	r.inDirArg = dir
	c := r.benign(ctx) // deliberately does NOT set c.Dir — the remote cwd is applied on the worker
	r.lastCmd = c
	return c
}

func TestHandler_Launch_ExecPath_RemoteCwd_hkfufel(t *testing.T) {
	t.Parallel()

	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	h := handler.NewHandler(pub, dl, handlercontract.NewAdapterRegistry())

	rr := &recordingRemoteRunner{}
	// A REMOTE worktree path that does NOT exist locally: with the bug the exec
	// path sets this as the LOCAL cmd.Dir → cmd.Start chdir-ENOENT.
	const remoteWorkDir = "/box-b/.harmonik/worktrees/run-fufel/does-not-exist-locally"
	spec := handler.LaunchSpec{
		Binary:  "codex",
		Args:    []string{"app-server"},
		Env:     []string{},
		WorkDir: remoteWorkDir,
		Role:    "implementer",
		Runner:  rr, // worker-selected codex run — SSHRunner in prod (satisfies RemoteCwdRunner)
		// Substrate intentionally nil — the SessionIDCaptured direct-exec path.
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	// With the fix, Launch succeeds (cmd.Dir unset → no chdir); drain + clean up.
	if err == nil {
		select {
		case <-watcher.Done():
		case <-t.Context().Done():
		}
		if sess != nil {
			_ = sess.Wait(t.Context()) //nolint:errcheck // cleanup
		}
	}

	rr.mu.Lock()
	defer rr.mu.Unlock()

	// The exec path MUST have taken CommandInDir (remote-cwd), NOT Command()+local Dir.
	if rr.inDirCalls != 1 {
		t.Fatalf("hk-fufel REGRESSION: handler exec path did NOT use CommandInDir "+
			"(inDirCalls=%d, commandCalls=%d) — it built cmd via Command() and set the LOCAL "+
			"cmd.Dir to the REMOTE WorkDir %q, fork/exec-ENOENTing the ssh process", rr.inDirCalls, rr.commandCalls, remoteWorkDir)
	}
	if rr.commandCalls != 0 {
		t.Errorf("Command() called %d times; want 0 for a RemoteCwdRunner + non-empty WorkDir", rr.commandCalls)
	}
	if rr.inDirArg != remoteWorkDir {
		t.Errorf("CommandInDir dir = %q; want the remote WorkDir %q", rr.inDirArg, remoteWorkDir)
	}
	// The LOCAL exec.Cmd.Dir must be UNSET — the remote path must never be a local Dir.
	if rr.lastCmd == nil {
		t.Fatal("no cmd recorded")
	}
	if rr.lastCmd.Dir != "" {
		t.Errorf("local cmd.Dir = %q; want UNSET (remote cwd applied on the worker via CommandInDir) — hk-fufel", rr.lastCmd.Dir)
	}
}
