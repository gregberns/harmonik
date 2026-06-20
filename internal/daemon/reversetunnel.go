package daemon

// reversetunnel.go — per-run SSH reverse tunnel for remote-worker runs.
//
// A remote-worker run spawns its implementer agent on the worker host via a
// DETACHED ssh (`ssh <host> -- tmux new-window -d …`, see
// internal/lifecycle/tmux/runner.go SSHRunner.Command + osadapter NewWindowIn's
// `-d` flag). That ssh returns immediately, so a `-R` reverse-tunnel flag riding
// it would be torn down before the agent emits its first hook (agent_ready /
// progress). The tunnel therefore MUST be a SEPARATE long-lived `ssh -N -R`
// process, keyed to the run and held open for the run's lifetime.
//
// The tunnel forwards a per-run TCP loopback listener on the WORKER
// (127.0.0.1:<port>) back to box A's daemon hook socket
// (<projectDir>/.harmonik/daemon.sock), so the worker-side agent's hook relay
// can reach the dispatching daemon. The worker-side TCP endpoint
// (tcp://127.0.0.1:<port>) is exposed on remoteBeadCtx (workerHookSock) so the
// env-override bead (2) and the readiness-gate bead (3) can reference it.
//
// WHY TCP LOOPBACK, NOT A UNIX SOCKET (hk-ege6): the remote path previously bound
// a unix-domain socket on the worker via `-R <workerUnixSock>:<daemonSock>`. On
// macOS, sshd runs as root, so a privileged `-R` StreamLocal bind creates that
// socket OWNED BY ROOT, MODE 0600. Claude's hook subprocess runs as the
// unprivileged worker user and gets `connect: permission denied` → the agent_ready
// relay silently dies → agent_ready_timeout @90s → run_failed. The client-side
// `-o StreamLocalBindMask=…` is IGNORED for `-R` binds (verified on the real
// worker), so the only reliable fix is to switch the remote forward to a TCP
// loopback listener: sshd binds a 127.0.0.1:<port> listener with no filesystem
// permission bits, connectable by the unprivileged worker user. LOCAL runs are
// untouched and still use box A's daemon unix socket directly.
//
// NFR7: this path is reached ONLY for remote runs (rbc != nil). Local runs never
// construct a tunnel and are byte-identical to the pre-tunnel code.
//
// Bead: rs-tunnel-spawn (gap #7 Option A, bead 1).

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workers"
)

// workerHarmonikPath resolves the absolute harmonik binary path ON THE WORKER,
// used as the hook "command" in the worker's per-run .claude/settings.json
// (hk-z8ek). Operators set it per-worker via workers.yaml (harmonik_path);
// when unset it falls back to the documented Go-install convention
// (workers.DefaultHarmonikPath). harmonik MUST be installed at this path on the
// worker for the hook relay to fire — a worker-setup requirement the daemon
// cannot fabricate.
func workerHarmonikPath(w workers.Worker) string {
	if w.HarmonikPath != "" {
		return w.HarmonikPath
	}
	return workers.DefaultHarmonikPath
}

// workerSocketPollInterval is the cadence at which waitWorkerSocketLive probes
// the worker-side reverse-tunnel TCP listener. The listener should appear within
// ~1s of the forward establishing, so a sub-second cadence keeps the gate snappy
// without hammering the ssh transport.
const workerSocketPollInterval = 300 * time.Millisecond

// workerSocketReadyTimeout is the default bound for waitWorkerSocketLive: how
// long beadRunOne waits for the per-run reverse-tunnel TCP listener to become
// connectable on the worker before failing the readiness gate. ~10s comfortably
// covers a healthy `ssh -N -R` establishing its forward; a longer hang means the
// tunnel will not come up and launching the agent would race ahead of a dead
// forward.
const workerSocketReadyTimeout = 10 * time.Second

// reverseTunnelRunner is the seam for constructing the long-lived `ssh -N -R`
// reverse-tunnel process. Production uses exec.CommandContext; tests inject a
// recorder (mirroring tmux.CommandRunner / tmux.RecordingRunner) to assert the
// argv without spawning a real ssh. Declared as a package-level var so a test in
// the daemon package can swap it for the duration of a single test.
var reverseTunnelRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// tcpEndpointPrefix marks a HARMONIK_DAEMON_SOCKET value as a TCP loopback
// endpoint (the REMOTE-run reverse-tunnel transport). A unix-socket path never
// starts with this prefix, so the hookrelay dialer can distinguish the two purely
// from the env value (see internal/hookrelay/hookrelay.go). Keep this string in
// sync with the hookrelay dialer.
const tcpEndpointPrefix = "tcp://"

// workerTCPEndpoint returns the per-run worker-side reverse-tunnel TCP endpoint
// the worker-side agent's hook relay dials:
//
//	tcp://127.0.0.1:<port>
//
// sshd binds this loopback listener on the worker via `-R 127.0.0.1:<port>:<sock>`
// and forwards it back to box A's daemon hook socket. The "tcp://" prefix is what
// the hookrelay dialer keys off to dial net.Dial("tcp", …) rather than "unix".
func workerTCPEndpoint(port int) string {
	return tcpEndpointPrefix + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// tcpEndpointAddr strips the "tcp://" prefix from a worker TCP endpoint, yielding
// the bare host:port for net.JoinHostPort-style use. Returns ("", false) for a
// value that is not a TCP endpoint (e.g. a unix-socket path).
func tcpEndpointAddr(endpoint string) (addr string, ok bool) {
	if strings.HasPrefix(endpoint, tcpEndpointPrefix) {
		return strings.TrimPrefix(endpoint, tcpEndpointPrefix), true
	}
	return "", false
}

// allocateReverseTunnelPort picks a free TCP port to hand sshd for the worker-side
// `-R 127.0.0.1:<port>:…` loopback bind.
//
// CONCURRENCY SAFETY (we run waves of 4+ simultaneous remote runs): each run binds
// a TCP listener on box A's 127.0.0.1:0, lets the OS assign a currently-free
// ephemeral port, reads it, and immediately closes the listener — so two
// concurrent runs get DISTINCT ports from the kernel's allocator. The port is a
// HINT for sshd's worker-side bind (the worker's free-port space is independent of
// box A's), so we pair it with `-o ExitOnForwardFailure=yes` in
// buildReverseTunnelArgs: if the hinted port is already taken on the worker, the
// tunnel FAILS FAST instead of silently binding nothing, and the connect-probe
// readiness gate (waitWorkerSocketLive) then reopens the bead for re-dispatch
// rather than launching claude into a dead tunnel. This avoids any shared mutable
// port counter and the TOCTOU false-green of "test -S exists".
func allocateReverseTunnelPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocateReverseTunnelPort: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// buildReverseTunnelArgs constructs the argv for the long-lived reverse tunnel:
//
//	ssh -N -R 127.0.0.1:<port>:<daemonSock> \
//	    -o ExitOnForwardFailure=yes [opts...] <host>
//
// -N         : do not execute a remote command (tunnel only).
// -R 127.0.0.1:<port>:<daemonSock> : reverse-forward a TCP loopback listener on
//
//	the WORKER (bound by sshd) back to box A's daemon UNIX socket. TCP loopback
//	(not a unix socket) is mandatory on macOS: sshd is root, so a `-R` StreamLocal
//	bind would create a root-owned 0600 socket the unprivileged worker user cannot
//	connect to (hk-ege6). A TCP listener has no filesystem permission bits.
//
// ExitOnForwardFailure=yes : if the worker-side bind fails (e.g. the hinted port
//
//	is already in use on the worker), the ssh exits NON-ZERO instead of staying up
//	with no forward — so the readiness gate observes a non-connectable endpoint and
//	fails the run rather than launching the agent into a dead tunnel.
//
// opts mirror the SSHRunner.Opts argv pattern (extra flags BEFORE the host, e.g.
// ["-p", "2222"]); host is the SSH destination (user@host or bare host). The
// returned slice does NOT include the leading "ssh" token — callers pass it as
// the command name to the runner (matching exec.CommandContext / SSHRunner).
func buildReverseTunnelArgs(port int, daemonSock, host string, opts []string) []string {
	forward := net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + ":" + daemonSock
	args := make([]string, 0, 5+len(opts)+1)
	args = append(args, "-N", "-R", forward, "-o", "ExitOnForwardFailure=yes")
	args = append(args, opts...)
	args = append(args, host)
	return args
}

// sshHostOpts extracts the host and extra opts from a CommandRunner when it is an
// SSHRunner (the production remote-run runner, built as
// tmuxpkg.SSHRunner{Host: w.Host}). Returns ("", nil, false) for any other runner
// type, so callers can fall back to the worker record's Host.
func sshHostOpts(r tmuxpkg.CommandRunner) (host string, opts []string, ok bool) {
	if sr, isSSH := r.(tmuxpkg.SSHRunner); isSSH {
		return sr.Host, sr.Opts, true
	}
	return "", nil, false
}

// resolveAgentDaemonSocket selects the HARMONIK_DAEMON_SOCKET path injected into
// the implementer agent's spawn env (gap #7 Option A, bead 2).
//
//   - REMOTE run (workerHookSock != ""): the agent runs on a worker host that
//     cannot reach box A's local daemon.sock, so it must dial the worker-side
//     reverse-tunnel TCP endpoint (tcp://127.0.0.1:<port>), which `ssh -N -R`
//     forwards back to box A's daemon.sock. The hookrelay dialer keys off the
//     "tcp://" prefix to dial TCP rather than a unix path.
//   - LOCAL run (workerHookSock == ""): the agent runs on box A and dials box A's
//     daemon.sock directly — returned UNCHANGED (NFR7: byte-identical to before).
//
// The function is pure (no I/O) so the path-selection contract is unit-testable
// without spawning a daemon or any ssh.
func resolveAgentDaemonSocket(workerHookSock, daemonSock string) string {
	if workerHookSock != "" {
		return workerHookSock
	}
	return daemonSock
}

// ensureWorkerHarmonikDir runs `mkdir -p <workerRepoPath>/.harmonik` on the worker
// through r (an SSHRunner in production) so the reverse tunnel can bind its per-run
// socket (run-<runID>.sock) under that directory. `ssh -N -R` fails to create the
// bind socket if the parent directory is missing, so this MUST run before the
// tunnel's bind attempt.
//
// gap #7 Option A, bead 2. Caller treats a non-nil error as non-fatal (logs and
// continues — the readiness gate in bead 3 is the authority): a transient mkdir
// failure should not abort the dispatch on its own.
func ensureWorkerHarmonikDir(ctx context.Context, r tmuxpkg.CommandRunner, workerRepoPath string) error {
	dir := filepath.Join(workerRepoPath, ".harmonik")
	cmd := r.Command(ctx, "mkdir", "-p", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ensureWorkerHarmonikDir (dir=%s): %w\nmkdir: %s", dir, err, out)
	}
	return nil
}

// waitWorkerSocketLive blocks until the worker-side per-run reverse-tunnel TCP
// listener (endpoint, a "tcp://127.0.0.1:<port>" value) is actually CONNECTABLE
// as the unprivileged worker user, or until timeout / ctx cancellation (gap #7
// Option A, bead 3 — the tunnel readiness gate).
//
// Why this gate exists, and why it is a CONNECT probe (hk-ege6): the worker-side
// implementer agent can fire its first agent_ready hook BEFORE the per-run
// `ssh -N -R` forward is actually live. The hook relay retries only on
// daemon_not_ready, NOT on a dial failure, so an agent that races ahead of the
// forward yields a silent bridge_dial_failed and an agent_ready_timeout. The old
// gate used `test -S` (existence only), which false-greened a non-connectable
// endpoint — e.g. the root-owned 0600 unix socket the unprivileged hook user
// could not actually connect to. The gate now performs an ACTUAL connect probe
// (`nc -z 127.0.0.1 <port>`) AS THE WORKER USER over r, so a non-connectable
// endpoint FAILS the gate instead of launching claude into a dead tunnel.
// beadRunOne therefore MUST NOT launch the agent until this returns nil.
//
// The probe runs `nc -z 127.0.0.1 <port>` through r (an SSHRunner in production,
// so the probe executes ON THE WORKER as the worker user). `nc -z` exits 0 iff a
// TCP connection to the port succeeds; any non-zero exit (not-yet-bound, or a
// listener present but unreachable) is treated as "not ready yet" and the poll
// continues. The probe argv is unit-tested with a fake CommandRunner, so no real
// ssh is needed.
//
// Returns:
//   - nil once the listener is confirmed connectable within timeout (Launch may
//     proceed).
//   - ctx.Err() promptly if ctx is cancelled while waiting.
//   - an error if endpoint is not a TCP endpoint, or the listener never becomes
//     connectable within timeout (caller emits worker_tunnel_failed + reopens the
//     bead, and does NOT Launch).
func waitWorkerSocketLive(ctx context.Context, r tmuxpkg.CommandRunner, endpoint string, timeout time.Duration) error {
	addr, ok := tcpEndpointAddr(endpoint)
	if !ok {
		return fmt.Errorf("waitWorkerSocketLive: endpoint %q is not a TCP endpoint", endpoint)
	}
	_, portStr, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return fmt.Errorf("waitWorkerSocketLive: malformed endpoint %q: %w", endpoint, splitErr)
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(workerSocketPollInterval)
	defer ticker.Stop()

	for {
		// `nc -z 127.0.0.1 <port>` exits 0 iff a TCP connection succeeds — an
		// ACTUAL connectability check as the worker user, not a mere existence test.
		if err := r.Command(ctx, "nc", "-z", "127.0.0.1", portStr).Run(); err == nil {
			return nil
		}
		// Stop as soon as the deadline has passed (also covers timeout <= 0:
		// we still probe once above before bailing).
		if time.Now().After(deadline) {
			return fmt.Errorf("waitWorkerSocketLive: endpoint %s not live within %s", endpoint, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// next probe
		}
	}
}
