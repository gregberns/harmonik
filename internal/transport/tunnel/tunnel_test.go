package tunnel

import (
	"context"
	"errors"
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
// hk-ege6 TCP-loopback form: ssh -N -R 127.0.0.1:<port>:<dsock> -o
// ExitOnForwardFailure=yes <host>. The worker-side bind is a TCP loopback
// listener (NOT a unix socket), so the macOS-root sshd cannot create a root-owned
// 0600 socket the unprivileged hook user can't connect to.
func TestReverseTunnel_ArgvExact(t *testing.T) {
	t.Parallel()

	const (
		port  = 51234
		dsock = "/Users/gb/github/harmonik/.harmonik/daemon.sock"
		host  = "worker-mac-1"
	)
	got := BuildArgs(port, dsock, host, nil)
	want := []string{
		"-N", "-R", "127.0.0.1:51234:" + dsock,
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ControlMaster=no",
		"-o", "ControlPath=none",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=4",
		host,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgs argv mismatch:\n got: %v\nwant: %v", got, want)
	}
	if !strings.HasPrefix(got[2], "127.0.0.1:") {
		t.Errorf("remote forward %q is not a 127.0.0.1 TCP loopback bind", got[2])
	}
	if strings.Contains(strings.Join(got, " "), "StreamLocal") {
		t.Errorf("argv must NOT use a StreamLocal/unix-socket bind (hk-ege6): %v", got)
	}
	full := append([]string{"ssh"}, got...)
	if joined := strings.Join(full, " "); joined !=
		"ssh -N -R 127.0.0.1:51234:"+dsock+" -o ExitOnForwardFailure=yes "+
			"-o ControlMaster=no -o ControlPath=none "+
			"-o ServerAliveInterval=15 -o ServerAliveCountMax=4 "+host {
		t.Errorf("full ssh argv = %q", joined)
	}
}

// TestReverseTunnel_ArgvWithOpts asserts extra SSHRunner opts are spliced BEFORE
// the host, mirroring tmux.SSHRunner.Command's [opts...] <host> ordering.
func TestReverseTunnel_ArgvWithOpts(t *testing.T) {
	t.Parallel()

	const (
		port  = 2200
		dsock = "/d/.harmonik/daemon.sock"
		host  = "user@host"
	)
	got := BuildArgs(port, dsock, host, []string{"-p", "2222"})
	want := []string{
		"-N", "-R", "127.0.0.1:2200:" + dsock,
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ControlMaster=no",
		"-o", "ControlPath=none",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=4",
		"-p", "2222",
		host,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgs (with opts) mismatch:\n got: %v\nwant: %v", got, want)
	}
	if got[len(got)-1] != host {
		t.Errorf("host not last token: %v", got)
	}
	cmIdx, pIdx := indexOf(got, "ControlMaster=no"), indexOf(got, "2222")
	if cmIdx < 0 || pIdx < 0 || cmIdx > pIdx {
		t.Errorf("ControlMaster=no must appear before worker opts: %v", got)
	}
}

func indexOf(ss []string, target string) int {
	for i, s := range ss {
		if s == target {
			return i
		}
	}
	return -1
}

// TestReverseTunnel_WorkerTCPEndpoint asserts the per-run worker-side endpoint is
// the "tcp://127.0.0.1:<port>" form the hookrelay dialer keys off, and that
// tcpEndpointAddr round-trips it (and rejects a unix path).
func TestReverseTunnel_WorkerTCPEndpoint(t *testing.T) {
	t.Parallel()

	got := WorkerTCPEndpoint(51234)
	want := "tcp://127.0.0.1:51234"
	if got != want {
		t.Fatalf("WorkerTCPEndpoint = %q, want %q", got, want)
	}

	addr, ok := tcpEndpointAddr(got)
	if !ok {
		t.Fatal("tcpEndpointAddr(tcp endpoint): ok = false, want true")
	}
	if addr != "127.0.0.1:51234" {
		t.Errorf("tcpEndpointAddr = %q, want 127.0.0.1:51234", addr)
	}

	if _, ok := tcpEndpointAddr("/home/worker/repo/.harmonik/daemon.sock"); ok {
		t.Error("tcpEndpointAddr(unix path): ok = true, want false")
	}
}

// TestReverseTunnel_AllocatePortConcurrencySafe asserts AllocatePort
// returns a usable port and that a batch of concurrent allocations (mirroring a
// wave of 4+ remote runs) yields DISTINCT ports — the concurrency-safety property
// the daemon relies on (no shared mutable counter; the kernel hands out distinct
// free ephemeral ports).
func TestReverseTunnel_AllocatePortConcurrencySafe(t *testing.T) {
	t.Parallel()

	const n = 8
	ports := make([]int, n)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p, err := AllocatePort()
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			ports[idx] = p
		}(i)
	}
	wg.Wait()
	if firstErr != nil {
		t.Fatalf("AllocatePort: %v", firstErr)
	}
	seen := make(map[int]bool, n)
	for _, p := range ports {
		if p <= 0 || p > 65535 {
			t.Errorf("allocated port %d out of range", p)
		}
		if seen[p] {
			t.Errorf("duplicate port %d across concurrent allocations (not collision-safe)", p)
		}
		seen[p] = true
	}
}

// TestReverseTunnel_SeamRecordsArgvAndProcessKilled drives the ReverseTunnelRunner
// seam end-to-end without real ssh: it injects a recorder that (a) captures the
// full ssh argv and (b) returns a controllable long-lived process. It then
// asserts the recorded argv is the run-id'd reverse-tunnel command and that the
// process is reliably killed + awaited both on run-ctx cancel and via the
// teardown sequence (Process.Kill + Wait), as beadRunOne's defer does.
func TestReverseTunnel_SeamRecordsArgvAndProcessKilled(t *testing.T) {
	orig := ReverseTunnelRunner
	t.Cleanup(func() { ReverseTunnelRunner = orig })

	var (
		mu          sync.Mutex
		recordedCmd string
		recordedArg []string
	)
	ReverseTunnelRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		mu.Lock()
		recordedCmd = name
		recordedArg = append([]string(nil), args...)
		mu.Unlock()
		return exec.CommandContext(ctx, "sleep", "600")
	}

	const (
		port = 51234
		proj = "/Users/gb/github/harmonik"
		host = "worker-mac-1"
	)
	dsock := proj + "/.harmonik/daemon.sock"

	ctx, cancel := context.WithCancel(context.Background())

	args := BuildArgs(port, dsock, host, nil)
	cmd := ReverseTunnelRunner(ctx, "ssh", args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("tunnel Start: %v", err)
	}

	mu.Lock()
	gotCmd, gotArg := recordedCmd, append([]string(nil), recordedArg...)
	mu.Unlock()
	if gotCmd != "ssh" {
		t.Errorf("recorded command name = %q, want ssh", gotCmd)
	}
	wantArg := []string{
		"-N", "-R", "127.0.0.1:51234:" + dsock,
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ControlMaster=no",
		"-o", "ControlPath=none",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=4",
		host,
	}
	if !reflect.DeepEqual(gotArg, wantArg) {
		t.Errorf("recorded argv mismatch:\n got: %v\nwant: %v", gotArg, wantArg)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	cancel()
	select {
	case <-waitDone:
	case <-time.After(10 * time.Second):
		if killErr := cmd.Process.Kill(); killErr != nil {
			t.Errorf("kill tunnel process after context-cancel timeout: %v", killErr)
		}
		t.Fatal("tunnel process not terminated within 10s of ctx cancel")
	}

	cmd2 := ReverseTunnelRunner(context.Background(), "ssh", args...)
	if err := cmd2.Start(); err != nil {
		t.Fatalf("tunnel(2) Start: %v", err)
	}
	teardownDone := make(chan struct{})
	go func() {
		if cmd2.Process != nil {
			if killErr := cmd2.Process.Kill(); killErr != nil {
				t.Errorf("kill tunnel process during teardown: %v", killErr)
			}
			if waitErr := cmd2.Wait(); waitErr != nil {
				var exitErr *exec.ExitError
				if !errors.As(waitErr, &exitErr) {
					t.Errorf("wait for tunnel process during teardown: %v", waitErr)
				}
			}
		}
		close(teardownDone)
	}()
	select {
	case <-teardownDone:
	case <-time.After(10 * time.Second):
		if killErr := cmd2.Process.Kill(); killErr != nil {
			t.Errorf("kill tunnel process after teardown timeout: %v", killErr)
		}
		t.Fatal("teardown did not kill+await the tunnel process within 10s")
	}
}

// TestReverseTunnel_SSHRunnerHostOptsExtraction asserts SSHHostOpts pulls Host
// and Opts out of an SSHRunner and reports !ok for other runner types.
func TestReverseTunnel_SSHRunnerHostOptsExtraction(t *testing.T) {
	t.Parallel()

	host, opts, ok := SSHHostOpts(tmuxpkg.SSHRunner{Host: "worker-mac-1", Opts: []string{"-p", "2222"}})
	if !ok {
		t.Fatal("SSHHostOpts(SSHRunner): ok = false, want true")
	}
	if host != "worker-mac-1" {
		t.Errorf("host = %q, want worker-mac-1", host)
	}
	if !reflect.DeepEqual(opts, []string{"-p", "2222"}) {
		t.Errorf("opts = %v, want [-p 2222]", opts)
	}

	if _, _, ok := SSHHostOpts(tmuxpkg.LocalRunner{}); ok {
		t.Error("SSHHostOpts(LocalRunner): ok = true, want false")
	}
}

// TestTunnelEnv_ResolveAgentDaemonSocket asserts the HARMONIK_DAEMON_SOCKET
// selection (gap #7 bead 2): a REMOTE run resolves to the worker-side TCP
// reverse-tunnel endpoint (tcp://127.0.0.1:<port>), NOT box A's daemon.sock; a
// LOCAL run resolves to box A's daemon.sock UNCHANGED (NFR7 byte-identical).
func TestTunnelEnv_ResolveAgentDaemonSocket(t *testing.T) {
	t.Parallel()

	const boxASock = "/Users/gb/github/harmonik/.harmonik/daemon.sock"
	workerSock := WorkerTCPEndpoint(51234) // endpoint is tcp 127.0.0.1 port 51234

	if got := ResolveAgentDaemonSocket(workerSock, boxASock); got != workerSock {
		t.Errorf("remote run: ResolveAgentDaemonSocket = %q, want worker-side %q", got, workerSock)
	}
	if got := ResolveAgentDaemonSocket(workerSock, boxASock); got == boxASock {
		t.Errorf("remote run: resolved endpoint must NOT be box A's daemon.sock (%q)", boxASock)
	}

	if got := ResolveAgentDaemonSocket("", boxASock); got != boxASock {
		t.Errorf("local run: ResolveAgentDaemonSocket = %q, want box-A %q (unchanged)", got, boxASock)
	}
}

// TestTunnelEnv_EnsureWorkerHarmonikDir asserts EnsureWorkerHarmonikDir runs
// `mkdir -p <repo>/.harmonik` through the injected runner (so the reverse tunnel
// can bind its socket there) and surfaces a runner error.
func TestTunnelEnv_EnsureWorkerHarmonikDir(t *testing.T) {
	t.Parallel()

	rr := &tmuxpkg.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "true")
		},
	}
	if err := EnsureWorkerHarmonikDir(context.Background(), rr, "/home/worker/repo"); err != nil {
		t.Fatalf("EnsureWorkerHarmonikDir: unexpected error: %v", err)
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

	rrFail := &tmuxpkg.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false")
		},
	}
	if err := EnsureWorkerHarmonikDir(context.Background(), rrFail, "/home/worker/repo"); err == nil {
		t.Error("EnsureWorkerHarmonikDir: expected error on non-zero mkdir exit, got nil")
	}
}

// TestWaitWorkerSocketLive_SocketAppears asserts the readiness gate (gap #7
// bead 3) returns nil once the worker-side TCP listener becomes CONNECTABLE: the
// fake runner returns non-zero (`false`) for the first 2 polls — simulating the
// forward not yet bound — then exit 0 (`true`). The gate must then return nil
// (Launch would proceed) and the probe argv must be `nc -z 127.0.0.1 <port>` (an
// actual connect probe, hk-ege6 — NOT a `test -S` existence check).
func TestWaitWorkerSocketLive_SocketAppears(t *testing.T) {
	t.Parallel()

	const endpoint = "tcp://127.0.0.1:51234"
	var calls int32
	rr := &tmuxpkg.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if atomic.AddInt32(&calls, 1) <= 2 {
				return exec.CommandContext(ctx, "false")
			}
			return exec.CommandContext(ctx, "true")
		},
	}

	if err := WaitWorkerSocketLive(context.Background(), rr, endpoint, 5*time.Second); err != nil {
		t.Fatalf("WaitWorkerSocketLive: unexpected error: %v", err)
	}

	if len(rr.Calls) < 3 {
		t.Fatalf("expected at least 3 probes (2 not-ready + 1 ready), got %d: %+v", len(rr.Calls), rr.Calls)
	}
	first := rr.Calls[0]
	if first.Name != "nc" {
		t.Errorf("probe command name = %q, want nc (connect probe)", first.Name)
	}
	if want := []string{"-z", "127.0.0.1", "51234"}; !reflect.DeepEqual(first.Args, want) {
		t.Errorf("probe argv = %v, want %v", first.Args, want)
	}
}

// TestWaitWorkerSocketLive_NonConnectableFails asserts the gate FAILS (does not
// false-green) when the endpoint never becomes connectable — the exact regression
// the old `test -S` existence check allowed (a present-but-unconnectable
// root-owned 0600 socket). The fake runner always exits non-zero (connection
// refused), and a malformed/non-TCP endpoint is also rejected outright.
func TestWaitWorkerSocketLive_NonConnectableFails(t *testing.T) {
	t.Parallel()

	const endpoint = "tcp://127.0.0.1:51234"
	rr := &tmuxpkg.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false") // connection always refused
		},
	}
	if err := WaitWorkerSocketLive(context.Background(), rr, endpoint, 200*time.Millisecond); err == nil {
		t.Fatal("non-connectable endpoint: expected an error, got nil (false-green regression)")
	}

	if err := WaitWorkerSocketLive(context.Background(), rr, "", 200*time.Millisecond); err == nil {
		t.Error("empty endpoint: expected an error, got nil")
	}
	if err := WaitWorkerSocketLive(context.Background(), rr, "/some/unix.sock", 200*time.Millisecond); err == nil {
		t.Error("unix-path endpoint: expected a not-a-TCP-endpoint error, got nil")
	}
}

// TestWaitWorkerSocketLive_Timeout asserts the gate returns a timeout error
// (NOT nil) within ~the bound when the listener never becomes connectable: the
// fake runner always exits non-zero. A SHORT timeout (200ms) keeps the test fast.
func TestWaitWorkerSocketLive_Timeout(t *testing.T) {
	t.Parallel()

	const endpoint = "tcp://127.0.0.1:51234"
	rr := &tmuxpkg.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false") // never ready
		},
	}

	const bound = 200 * time.Millisecond
	start := time.Now()
	err := WaitWorkerSocketLive(context.Background(), rr, endpoint, bound)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("WaitWorkerSocketLive: expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "not live") {
		t.Errorf("error = %q, want it to mention the socket not being live", err.Error())
	}
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

	const endpoint = "tcp://127.0.0.1:51234"
	rr := &tmuxpkg.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false") // never ready
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := WaitWorkerSocketLive(ctx, rr, endpoint, 30*time.Second)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("WaitWorkerSocketLive: expected ctx error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("returned in %s, want prompt return on ctx cancel (well under the 30s timeout)", elapsed)
	}
}
