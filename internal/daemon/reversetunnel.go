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
// The tunnel forwards a per-run unix socket on the WORKER
// (<worker.RepoPath>/.harmonik/run-<runID>.sock) back to box A's daemon hook
// socket (<projectDir>/.harmonik/daemon.sock), so the worker-side agent's hook
// relay can reach the dispatching daemon. The worker-side socket path is exposed
// on remoteBeadCtx (workerHookSock) so the env-override bead (2) and the
// readiness-gate bead (3) can reference it.
//
// NFR7: this path is reached ONLY for remote runs (rbc != nil). Local runs never
// construct a tunnel and are byte-identical to the pre-tunnel code.
//
// Bead: rs-tunnel-spawn (gap #7 Option A, bead 1).

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
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
// the worker-side reverse-tunnel socket. The socket should appear within ~1s of
// the forward establishing, so a sub-second cadence keeps the gate snappy
// without hammering the ssh transport.
const workerSocketPollInterval = 300 * time.Millisecond

// workerSocketReadyTimeout is the default bound for waitWorkerSocketLive: how
// long beadRunOne waits for the per-run reverse-tunnel socket to become live on
// the worker before failing the readiness gate. ~10s comfortably covers a
// healthy `ssh -N -R` establishing its forward; a longer hang means the tunnel
// will not come up and launching the agent would race ahead of a dead forward.
const workerSocketReadyTimeout = 10 * time.Second

// reverseTunnelRunner is the seam for constructing the long-lived `ssh -N -R`
// reverse-tunnel process. Production uses exec.CommandContext; tests inject a
// recorder (mirroring tmux.CommandRunner / tmux.RecordingRunner) to assert the
// argv without spawning a real ssh. Declared as a package-level var so a test in
// the daemon package can swap it for the duration of a single test.
var reverseTunnelRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// workerRunSocketPath returns the per-run worker-side reverse-tunnel socket path:
//
//	<workerRepoPath>/.harmonik/run-<runID>.sock
//
// This is the socket the worker-side agent's hook relay connects to; ssh forwards
// it back to box A's daemon hook socket.
func workerRunSocketPath(workerRepoPath, runID string) string {
	return filepath.Join(workerRepoPath, ".harmonik", "run-"+runID+".sock")
}

// buildReverseTunnelArgs constructs the argv for the long-lived reverse tunnel:
//
//	ssh -N -R <workerSock>:<daemonSock> -o StreamLocalBindUnlink=yes [opts...] <host>
//
// -N         : do not execute a remote command (tunnel only).
// -R         : reverse-forward the worker-side unix socket to box A's daemon sock.
// StreamLocalBindUnlink=yes : unlink a stale worker-side socket before binding,
//
//	so a re-dispatched run does not fail on a leftover.
//
// opts mirror the SSHRunner.Opts argv pattern (extra flags BEFORE the host, e.g.
// ["-p", "2222"]); host is the SSH destination (user@host or bare host). The
// returned slice does NOT include the leading "ssh" token — callers pass it as
// the command name to the runner (matching exec.CommandContext / SSHRunner).
func buildReverseTunnelArgs(workerSock, daemonSock, host string, opts []string) []string {
	args := make([]string, 0, 5+len(opts)+1)
	args = append(args, "-N", "-R", workerSock+":"+daemonSock, "-o", "StreamLocalBindUnlink=yes")
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
//     reverse-tunnel socket (<worker.RepoPath>/.harmonik/run-<runID>.sock), which
//     `ssh -N -R` forwards back to box A's daemon.sock.
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

// waitWorkerSocketLive blocks until the worker-side per-run reverse-tunnel
// socket (sockPath) exists AND is a socket, or until timeout / ctx cancellation
// (gap #7 Option A, bead 3 — the tunnel readiness gate).
//
// Why this gate exists: the worker-side implementer agent can fire its first
// agent_ready hook BEFORE the per-run `ssh -N -R` forward is actually live. The
// hook relay retries only on daemon_not_ready, NOT on a dial failure, so an
// agent that races ahead of the forward yields a silent bridge_dial_failed and
// an agent_ready_timeout. beadRunOne therefore MUST NOT launch the agent until
// this returns nil.
//
// The probe runs `test -S <sockPath>` through r (an SSHRunner in production, so
// the test executes ON THE WORKER). `test -S` exits 0 iff the path exists and
// is a socket; any non-zero exit (not-yet-bound, or a regular file) is treated
// as "not ready yet" and the poll continues. The path-selection contract is
// unit-tested with a fake CommandRunner, so no real ssh is needed.
//
// Returns:
//   - nil once the socket is confirmed live within timeout (Launch may proceed).
//   - ctx.Err() promptly if ctx is cancelled while waiting.
//   - a timeout error if the socket never becomes live within timeout (caller
//     emits worker_tunnel_failed + reopens the bead, and does NOT Launch).
func waitWorkerSocketLive(ctx context.Context, r tmuxpkg.CommandRunner, sockPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(workerSocketPollInterval)
	defer ticker.Stop()

	for {
		// `test -S <sock>` exits 0 iff sockPath exists and is a socket.
		if err := r.Command(ctx, "test", "-S", sockPath).Run(); err == nil {
			return nil
		}
		// Stop as soon as the deadline has passed (also covers timeout <= 0:
		// we still probe once above before bailing).
		if time.Now().After(deadline) {
			return fmt.Errorf("waitWorkerSocketLive: socket %s not live within %s", sockPath, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// next probe
		}
	}
}
