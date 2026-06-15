package daemon

// reversetunnel_test.go — unit tests for the per-run SSH reverse tunnel
// (rs-tunnel-spawn, gap #7 Option A, bead 1).
//
// Gate-runnable: NO real ssh is spawned. The tunnel-launcher's command
// construction is asserted directly (buildReverseTunnelArgs), and the
// process-lifecycle assertions (killed + awaited on ctx-cancel and via the
// teardown defer) use a controllable long-lived stand-in process injected
// through the reverseTunnelRunner seam — mirroring the tmux.RecordingRunner
// pattern used elsewhere in this package.
//
// Test matrix:
//   TestReverseTunnel_ArgvExact:
//     buildReverseTunnelArgs == ssh -N -R <wsock>:<dsock> -o
//     StreamLocalBindUnlink=yes <host>, with the run-id'd worker sock and the
//     box-A daemon sock.
//   TestReverseTunnel_ArgvWithOpts:
//     extra SSHRunner opts are spliced BEFORE the host (runner.go pattern).
//   TestReverseTunnel_WorkerRunSocketPath:
//     the worker-side socket path is <repo>/.harmonik/run-<runID>.sock.
//   TestReverseTunnel_SeamRecordsArgvAndProcessKilled:
//     the reverseTunnelRunner seam records the full ssh argv and the started
//     process is killed + awaited on run-ctx cancel and via the teardown defer.
//   TestReverseTunnel_SSHRunnerHostOptsExtraction:
//     sshHostOpts extracts Host/Opts from an SSHRunner and reports false for
//     other runner types (fall back to the worker record's Host).
//
// Bead: rs-tunnel-spawn.

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestReverseTunnel_ArgvExact asserts the exact argv (no opts) matches the
// normative form: ssh -N -R <wsock>:<dsock> -o StreamLocalBindUnlink=yes <host>.
func TestReverseTunnel_ArgvExact(t *testing.T) {
	t.Parallel()

	const (
		wsock = "/home/worker/repo/.harmonik/run-RUNID.sock"
		dsock = "/Users/gb/github/harmonik/.harmonik/daemon.sock"
		host  = "worker-mac-1"
	)
	got := buildReverseTunnelArgs(wsock, dsock, host, nil)
	want := []string{
		"-N", "-R", wsock + ":" + dsock,
		"-o", "StreamLocalBindUnlink=yes",
		host,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildReverseTunnelArgs argv mismatch:\n got: %v\nwant: %v", got, want)
	}
	// Full ssh argv (with the command name prepended, as the runner sees it).
	full := append([]string{"ssh"}, got...)
	if joined := strings.Join(full, " "); joined !=
		"ssh -N -R "+wsock+":"+dsock+" -o StreamLocalBindUnlink=yes "+host {
		t.Errorf("full ssh argv = %q", joined)
	}
}

// TestReverseTunnel_ArgvWithOpts asserts extra SSHRunner opts are spliced BEFORE
// the host, mirroring tmux.SSHRunner.Command's [opts...] <host> ordering.
func TestReverseTunnel_ArgvWithOpts(t *testing.T) {
	t.Parallel()

	const (
		wsock = "/w/.harmonik/run-X.sock"
		dsock = "/d/.harmonik/daemon.sock"
		host  = "user@host"
	)
	got := buildReverseTunnelArgs(wsock, dsock, host, []string{"-p", "2222"})
	want := []string{
		"-N", "-R", wsock + ":" + dsock,
		"-o", "StreamLocalBindUnlink=yes",
		"-p", "2222",
		host,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildReverseTunnelArgs (with opts) mismatch:\n got: %v\nwant: %v", got, want)
	}
	// Host must be the LAST token; opts must precede it.
	if got[len(got)-1] != host {
		t.Errorf("host not last token: %v", got)
	}
}

// TestReverseTunnel_WorkerRunSocketPath asserts the per-run worker-side socket
// path embeds the run id under the worker repo's .harmonik dir.
func TestReverseTunnel_WorkerRunSocketPath(t *testing.T) {
	t.Parallel()

	got := workerRunSocketPath("/home/worker/repo", "abc-123")
	want := "/home/worker/repo/.harmonik/run-abc-123.sock"
	if got != want {
		t.Fatalf("workerRunSocketPath = %q, want %q", got, want)
	}
}

// TestReverseTunnel_SeamRecordsArgvAndProcessKilled drives the reverseTunnelRunner
// seam end-to-end without real ssh: it injects a recorder that (a) captures the
// full ssh argv and (b) returns a controllable long-lived process. It then
// asserts the recorded argv is the run-id'd reverse-tunnel command and that the
// process is reliably killed + awaited both on run-ctx cancel and via the
// teardown sequence (Process.Kill + Wait), as beadRunOne's defer does.
func TestReverseTunnel_SeamRecordsArgvAndProcessKilled(t *testing.T) {
	// Not parallel: swaps the package-level reverseTunnelRunner seam.
	orig := reverseTunnelRunner
	t.Cleanup(func() { reverseTunnelRunner = orig })

	var (
		mu          sync.Mutex
		recordedCmd string
		recordedArg []string
	)
	reverseTunnelRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		mu.Lock()
		recordedCmd = name
		recordedArg = append([]string(nil), args...)
		mu.Unlock()
		// A long-lived stand-in for `ssh -N -R …`: blocks until killed or ctx
		// cancel. `sleep 600` is killed via Process.Kill; CommandContext also
		// kills it on ctx cancel.
		return exec.CommandContext(ctx, "sleep", "600")
	}

	const (
		runID = "run-deadbeef"
		repo  = "/home/worker/repo"
		proj  = "/Users/gb/github/harmonik"
		host  = "worker-mac-1"
	)
	wsock := workerRunSocketPath(repo, runID)
	dsock := proj + "/.harmonik/daemon.sock"

	ctx, cancel := context.WithCancel(context.Background())

	args := buildReverseTunnelArgs(wsock, dsock, host, nil)
	cmd := reverseTunnelRunner(ctx, "ssh", args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("tunnel Start: %v", err)
	}

	// Assert the seam recorded the exact reverse-tunnel ssh argv.
	mu.Lock()
	gotCmd, gotArg := recordedCmd, append([]string(nil), recordedArg...)
	mu.Unlock()
	if gotCmd != "ssh" {
		t.Errorf("recorded command name = %q, want ssh", gotCmd)
	}
	wantArg := []string{
		"-N", "-R", wsock + ":" + dsock,
		"-o", "StreamLocalBindUnlink=yes",
		host,
	}
	if !reflect.DeepEqual(gotArg, wantArg) {
		t.Errorf("recorded argv mismatch:\n got: %v\nwant: %v", gotArg, wantArg)
	}

	// (1) ctx cancel must terminate the process (CommandContext semantics) — the
	// daemon ties the tunnel to the run ctx, so cancelling the run kills it.
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	cancel()
	select {
	case <-waitDone:
		// terminated — good (Wait returns a non-nil "signal: killed" error).
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("tunnel process not terminated within 10s of ctx cancel")
	}

	// (2) The teardown sequence beadRunOne's defer uses (Process.Kill + Wait,
	// ignoring errors) must reliably terminate + reap an independently-started
	// tunnel process. Start a fresh one under a non-cancelled ctx so the kill is
	// the ONLY thing that stops it, then run the exact defer shape.
	cmd2 := reverseTunnelRunner(context.Background(), "ssh", args...)
	if err := cmd2.Start(); err != nil {
		t.Fatalf("tunnel(2) Start: %v", err)
	}
	teardownDone := make(chan struct{})
	go func() {
		// Mirror beadRunOne's defer exactly: Process.Kill then cmd.Wait.
		if cmd2.Process != nil {
			_ = cmd2.Process.Kill()
			_ = cmd2.Wait()
		}
		close(teardownDone)
	}()
	select {
	case <-teardownDone:
		// killed + awaited — good.
	case <-time.After(10 * time.Second):
		_ = cmd2.Process.Kill()
		t.Fatal("teardown did not kill+await the tunnel process within 10s")
	}
}

// TestReverseTunnel_SSHRunnerHostOptsExtraction asserts sshHostOpts pulls Host
// and Opts out of an SSHRunner and reports !ok for other runner types.
func TestReverseTunnel_SSHRunnerHostOptsExtraction(t *testing.T) {
	t.Parallel()

	host, opts, ok := sshHostOpts(tmuxpkg.SSHRunner{Host: "worker-mac-1", Opts: []string{"-p", "2222"}})
	if !ok {
		t.Fatal("sshHostOpts(SSHRunner): ok = false, want true")
	}
	if host != "worker-mac-1" {
		t.Errorf("host = %q, want worker-mac-1", host)
	}
	if !reflect.DeepEqual(opts, []string{"-p", "2222"}) {
		t.Errorf("opts = %v, want [-p 2222]", opts)
	}

	// A non-SSH runner (LocalRunner) must report !ok so the caller falls back to
	// the worker record's Host.
	if _, _, ok := sshHostOpts(tmuxpkg.LocalRunner{}); ok {
		t.Error("sshHostOpts(LocalRunner): ok = true, want false")
	}
}
