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
//
// ── gap #7 bead 3 (tunnel readiness gate, hk-rs-tunnel-readiness-cc1w) ──
//   TestWaitWorkerSocketLive_SocketAppears:
//     the fake runner returns non-zero for the first N polls then exit 0;
//     waitWorkerSocketLive returns nil (Launch would proceed) and the probe
//     argv is `test -S <sock>`.
//   TestWaitWorkerSocketLive_Timeout:
//     the fake runner always returns non-zero; waitWorkerSocketLive returns a
//     timeout error within ~the (short) bound.
//   TestWaitWorkerSocketLive_CtxCancel:
//     a cancelled ctx makes waitWorkerSocketLive return ctx.Err() promptly.

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
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

// TestTunnelEnv_ResolveAgentDaemonSocket asserts the HARMONIK_DAEMON_SOCKET path
// selection (gap #7 bead 2): a REMOTE run resolves to the worker-side run socket
// (<worker.RepoPath>/.harmonik/run-<runID>.sock), NOT box A's daemon.sock; a LOCAL
// run resolves to box A's daemon.sock UNCHANGED (NFR7 byte-identical).
func TestTunnelEnv_ResolveAgentDaemonSocket(t *testing.T) {
	t.Parallel()

	const (
		runID    = "run-deadbeef"
		repo     = "/home/worker/repo"
		boxASock = "/Users/gb/github/harmonik/.harmonik/daemon.sock"
	)
	workerSock := workerRunSocketPath(repo, runID) // /home/worker/repo/.harmonik/run-run-deadbeef.sock

	// REMOTE: workerHookSock is set (rbc != nil) → resolved socket is the
	// worker-side run socket, and explicitly NOT box A's daemon.sock.
	if got := resolveAgentDaemonSocket(workerSock, boxASock); got != workerSock {
		t.Errorf("remote run: resolveAgentDaemonSocket = %q, want worker-side %q", got, workerSock)
	}
	if got := resolveAgentDaemonSocket(workerSock, boxASock); got == boxASock {
		t.Errorf("remote run: resolved socket must NOT be box A's daemon.sock (%q)", boxASock)
	}

	// LOCAL: workerHookSock == "" (rbc == nil) → resolved socket is box A's
	// daemon.sock, unchanged.
	if got := resolveAgentDaemonSocket("", boxASock); got != boxASock {
		t.Errorf("local run: resolveAgentDaemonSocket = %q, want box-A %q (unchanged)", got, boxASock)
	}
}

// TestTunnelEnv_EnsureWorkerHarmonikDir asserts ensureWorkerHarmonikDir runs
// `mkdir -p <repo>/.harmonik` through the injected runner (so the reverse tunnel
// can bind its socket there) and surfaces a runner error.
func TestTunnelEnv_EnsureWorkerHarmonikDir(t *testing.T) {
	t.Parallel()

	// Success path: a RecordingRunner whose CmdFunc returns a no-op `true` records
	// the exact mkdir argv.
	rr := &tmuxpkg.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "true")
		},
	}
	if err := ensureWorkerHarmonikDir(context.Background(), rr, "/home/worker/repo"); err != nil {
		t.Fatalf("ensureWorkerHarmonikDir: unexpected error: %v", err)
	}
	if len(rr.Calls) != 1 {
		t.Fatalf("expected exactly 1 runner call, got %d: %+v", len(rr.Calls), rr.Calls)
	}
	gotCall := rr.Calls[0]
	if gotCall.Name != "mkdir" {
		t.Errorf("command name = %q, want mkdir", gotCall.Name)
	}
	wantArgs := []string{"-p", "/home/worker/repo/.harmonik"}
	if !reflect.DeepEqual(gotCall.Args, wantArgs) {
		t.Errorf("mkdir argv = %v, want %v", gotCall.Args, wantArgs)
	}

	// Failure path: a runner whose command exits non-zero must surface an error
	// (the caller treats it as non-fatal, but the helper must report it).
	rrFail := &tmuxpkg.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false")
		},
	}
	if err := ensureWorkerHarmonikDir(context.Background(), rrFail, "/home/worker/repo"); err == nil {
		t.Error("ensureWorkerHarmonikDir: expected error on non-zero mkdir exit, got nil")
	}
}

// TestWaitWorkerSocketLive_SocketAppears asserts the readiness gate (gap #7
// bead 3) returns nil once the worker-side socket becomes live: the fake runner
// returns non-zero (`false`) for the first 2 polls — simulating the forward not
// yet bound — then exit 0 (`true`). The gate must then return nil (Launch would
// proceed) and the probe argv must be `test -S <sock>`.
func TestWaitWorkerSocketLive_SocketAppears(t *testing.T) {
	t.Parallel()

	const sock = "/home/worker/repo/.harmonik/run-RUNID.sock"
	var calls int32
	rr := &tmuxpkg.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Non-zero exit for the first 2 probes, then exit 0.
			if atomic.AddInt32(&calls, 1) <= 2 {
				return exec.CommandContext(ctx, "false")
			}
			return exec.CommandContext(ctx, "true")
		},
	}

	// Timeout comfortably exceeds 3 × the poll interval so the third probe lands.
	if err := waitWorkerSocketLive(context.Background(), rr, sock, 5*time.Second); err != nil {
		t.Fatalf("waitWorkerSocketLive: unexpected error: %v", err)
	}

	// waitWorkerSocketLive has returned, so the runner is no longer invoked
	// concurrently — rr.Calls is safe to read directly.
	if len(rr.Calls) < 3 {
		t.Fatalf("expected at least 3 probes (2 not-ready + 1 ready), got %d: %+v", len(rr.Calls), rr.Calls)
	}
	// Probe argv must be exactly `test -S <sock>`.
	first := rr.Calls[0]
	if first.Name != "test" {
		t.Errorf("probe command name = %q, want test", first.Name)
	}
	if want := []string{"-S", sock}; !reflect.DeepEqual(first.Args, want) {
		t.Errorf("probe argv = %v, want %v", first.Args, want)
	}
}

// TestWaitWorkerSocketLive_Timeout asserts the gate returns a timeout error
// (NOT nil) within ~the bound when the socket never becomes live: the fake
// runner always exits non-zero. A SHORT timeout (200ms) keeps the test fast.
func TestWaitWorkerSocketLive_Timeout(t *testing.T) {
	t.Parallel()

	const sock = "/home/worker/repo/.harmonik/run-RUNID.sock"
	rr := &tmuxpkg.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false") // never ready
		},
	}

	const bound = 200 * time.Millisecond
	start := time.Now()
	err := waitWorkerSocketLive(context.Background(), rr, sock, bound)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("waitWorkerSocketLive: expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "not live") {
		t.Errorf("error = %q, want it to mention the socket not being live", err.Error())
	}
	// Must return at/after the bound but not hang far beyond it (one extra poll
	// interval of slack).
	if elapsed < bound {
		t.Errorf("returned in %s, want >= bound %s", elapsed, bound)
	}
	if elapsed > bound+2*time.Second {
		t.Errorf("returned in %s, want within ~the bound %s (no hang)", elapsed, bound)
	}
}

// TestWaitWorkerSocketLive_CtxCancel asserts the gate honours ctx cancellation:
// a context cancelled while the socket is still not live makes the gate return
// ctx.Err() promptly (well before the 30s timeout would fire).
func TestWaitWorkerSocketLive_CtxCancel(t *testing.T) {
	t.Parallel()

	const sock = "/home/worker/repo/.harmonik/run-RUNID.sock"
	rr := &tmuxpkg.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false") // never ready
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after the first probe so the gate is parked on the ticker
	// select when the cancellation lands.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := waitWorkerSocketLive(ctx, rr, sock, 30*time.Second)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("waitWorkerSocketLive: expected ctx error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("returned in %s, want prompt return on ctx cancel (well under the 30s timeout)", elapsed)
	}
}
