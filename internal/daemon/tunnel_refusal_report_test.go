package daemon

// tunnel_refusal_report_test.go — the three reverse-tunnel gates in the remote
// block of beadRunOne report through the run plan's one reporter.
//
// # What this file guards
//
// The remote block used to carry its own copy of the refusal report: a stderr
// line, a worker_tunnel_failed event and a best-effort reopen, written out a
// second time inside a closure. The run plan already had all three, and its
// refusal value already carried the worker_tunnel_failed payload. The copy is
// gone and every gate now builds a refusal and hands it to refuseRunPlan.
//
// That is a refactor, so these tests are the thing that says the behaviour did
// not move. Each of the three gates must still emit its one event and still
// reopen its bead with the reverse-tunnel reason.
//
// The port-allocation gate keeps its own file — see
// tunnel_portalloc_refusal_test.go, which asserts the same event and the same
// reopen plus the two costs unique to that gate. The two gates below it had no
// end-to-end test at all, so they get one here.
//
// # Why the socket-path gate needs a test of its own
//
// The run plan refuses the same condition earlier and more cheaply, but only
// for a run whose worker the outer dispatch loop already reserved. A run that
// got its worker from the fallback selection was not yet known to be remote
// when the plan ran, so the copy inside the remote block is that path's only
// guard. runplan_hooksocket_test.go drives the pre-selected path; this file
// drives the fallback one, and the two never both refuse.
//
// # Why the readiness-gate test cancels rather than waits
//
// The gate's bound is a ten-second constant. Waiting it out would put ten
// seconds into check-fast to observe one refusal. The test instead watches the
// ssh shim's log for the `nc -z` probe — which is the gate and nothing else —
// and cancels the run only once it has seen one. That is faster AND says more:
// the probe reaching the shim is positive evidence that the machinery ran,
// which a plain "the run failed" assertion does not give.
//
// The remote fixture lives in remoterunfixture_test.go. Helper prefix:
// tunrefuse.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workers"
)

// tunrefuseWorker is the worker every refusal below names.
var tunrefuseWorker = workers.Worker{
	Name:     "tunrefuse-worker",
	Host:     "tunrefuse.invalid",
	Enabled:  true,
	MaxSlots: 1,
	RepoPath: "/tmp/tunrefuse-worker-repo",
}

// tunrefuseTunnelPayload decodes the one worker_tunnel_failed event the bus saw.
func tunrefuseTunnelPayload(t *testing.T, bus *runplanBus) workers.WorkerTunnelFailedPayload {
	t.Helper()
	raw := bus.firstPayload(core.EventTypeWorkerTunnelFailed)
	if raw == nil {
		t.Fatal("no worker_tunnel_failed event was emitted; the gate refused without telling an operator which worker")
	}
	var p workers.WorkerTunnelFailedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("worker_tunnel_failed payload does not decode: %v", err)
	}
	return p
}

// tunrefuseProbeMark is how the readiness probe appears in the ssh shim's log.
// SSHRunner shell-quotes the remote command token by token, so the log carries
// `'nc' '-z' …` rather than a bare nc. No other step of a remote run runs nc.
const tunrefuseProbeMark = "'nc' '-z'"

// tunrefuseWaitForProbe blocks until the ssh shim's log carries the readiness
// probe, and then cancels the run.
//
// It returns after the cancel so the caller can assert the probe was seen. A
// deadline of its own keeps a broken run from hanging the test binary: on
// timeout it cancels anyway and reports false.
func tunrefuseWaitForProbe(logPath string, cancel context.CancelFunc, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		raw, _ := os.ReadFile(logPath) //nolint:errcheck,gosec // G304: a log path the fixture just created under its own temp dir; the shim may not have written yet, and an unreadable log is simply "no probe seen"
		if strings.Contains(string(raw), tunrefuseProbeMark) {
			cancel()
			return true
		}
		if time.Now().After(deadline) {
			cancel()
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestTunnelRefusal_OneLinePerGate pins the report each gate builds. The stage
// name is the only part that varies, and it is the part an operator reads to
// know which step failed, so a gate that loses its own name sends the reader to
// the wrong repair.
func TestTunnelRefusal_OneLinePerGate(t *testing.T) {
	t.Parallel()

	runID := core.RunID(uuid.New())
	beadID := core.BeadID("hk-tunrefuse-probe")
	cause := errors.New("probe said no")

	for _, stage := range []string{"port alloc", "socket-path", "readiness gate"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			refusal := tunnelRefusal(runID, beadID, tunrefuseWorker, stage, "/sock", cause)

			wantLog := "daemon: workloop: reverse-tunnel " + stage + " bead hk-tunrefuse-probe run " +
				runID.String() + ": probe said no (reopening, not launching)\n"
			if refusal.LogLine != wantLog {
				t.Errorf("LogLine = %q\nwant      %q", refusal.LogLine, wantLog)
			}
			if want := "reverse-tunnel not ready: probe said no"; refusal.ReopenReason != want {
				t.Errorf("ReopenReason = %q; want %q", refusal.ReopenReason, want)
			}
			if !errors.Is(refusal.Err, cause) {
				t.Errorf("Err = %v; want the cause the gate was handed", refusal.Err)
			}
			tf := refusal.TunnelFailure
			if tf == nil {
				t.Fatal("TunnelFailure = nil; every tunnel refusal owes a worker_tunnel_failed report")
			}
			if tf.RunID != runID.String() || tf.BeadID != "hk-tunrefuse-probe" {
				t.Errorf("TunnelFailure run/bead = %q/%q; want %q/hk-tunrefuse-probe", tf.RunID, tf.BeadID, runID.String())
			}
			if tf.WorkerName != tunrefuseWorker.Name || tf.WorkerHost != tunrefuseWorker.Host {
				t.Errorf("TunnelFailure worker = %q/%q; want %q/%q",
					tf.WorkerName, tf.WorkerHost, tunrefuseWorker.Name, tunrefuseWorker.Host)
			}
			if tf.SocketPath != "/sock" || tf.Detail != "probe said no" {
				t.Errorf("TunnelFailure sock/detail = %q/%q; want /sock/probe said no", tf.SocketPath, tf.Detail)
			}
		})
	}
}

// TestTunnelRefusal_SocketPathOnTheFallbackWorkerPath drives a run that had no
// pre-selected worker, so the run plan let it through, and whose hook socket
// path is too long for the platform. The remote block's own copy of the check
// is the only thing standing between that run and a tunnel that would swallow
// every hook in silence.
func TestTunnelRefusal_SocketPathOnTheFallbackWorkerPath(t *testing.T) {
	// Not parallel: swaps a package-level seam in internal/transport/tunnel and
	// the process PATH.
	projectDir := hooksockDeepRepo(t)
	// Exit 1: the only ssh this run makes is the worker mkdir, which is
	// non-fatal. Failing it keeps the fixture off any real machine.
	sshLog := remotefixSSHShim(t, 1)

	// The gate sits ABOVE the tunnel start, so a tunnel built for this run means
	// the gate moved below an acquisition.
	var tunnelBuilds atomic.Int32
	remotefixTunnelSeam(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		tunnelBuilds.Add(1)
		t.Errorf("a reverse tunnel was constructed for a run whose hook socket path is too long: %s %v", name, args)
		return exec.CommandContext(ctx, "true")
	})

	// No slot is reserved here. beadRunOne's fallback selection takes it, which
	// is the path under test.
	reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{tunrefuseWorker}})

	var worktreeCreated bool
	worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
		worktreeCreated = true
		t.Error("worktree factory ran on a refused bead")
		return "", func() {}, nil
	}

	ledger := &runplanacqLedger{}
	bus := &runplanBus{}

	params := remotefixParams(t, projectDir)
	params.BrAdapter = ledger
	params.Bus = bus
	params.AdapterRegistry2 = runplanacqSealedRegistry(t)
	params.WorkerRegistry = reg
	params.WorktreeFactory = worktreeFactory
	deps := ExportedWorkLoopDeps(params)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bead := remotefixBead("hk-tunrefuse-sock", "fallback-path socket-path refuse probe")
	env := remotefixRunEnv(deps, bead)
	if succeeded := runBeadOneTest(ctx, deps, env, "", nil, false); succeeded {
		t.Error("beadRunOne reported success for a refused bead")
	}

	// The run really did go remote by the fallback path. Without this the
	// assertions below are all satisfied by a local run that never reached the
	// gate at all.
	if len(remotefixSSHCalls(t, sshLog)) == 0 {
		t.Fatal("the run made no ssh call, so it never took the remote path and this test measured nothing")
	}
	payload := tunrefuseTunnelPayload(t, bus)
	if payload.WorkerName != tunrefuseWorker.Name {
		t.Errorf("worker_tunnel_failed worker_name = %q; want %q", payload.WorkerName, tunrefuseWorker.Name)
	}
	if payload.SocketPath != hooksockPath(projectDir) {
		t.Errorf("worker_tunnel_failed socket_path = %q; want %q", payload.SocketPath, hooksockPath(projectDir))
	}

	// The one event this refusal owes, and no other.
	runplanWantEvents(t, bus.seen(), []core.EventType{core.EventTypeWorkerTunnelFailed})

	calls := ledger.calls()
	if len(calls) != 1 {
		t.Fatalf("ReopenBead call count = %d; want exactly 1\ncalls=%+v", len(calls), calls)
	}
	if calls[0].beadID != bead.BeadID {
		t.Errorf("ReopenBead beadID = %q; want %q", calls[0].beadID, bead.BeadID)
	}
	if !strings.Contains(calls[0].reason, "reverse-tunnel not ready") {
		t.Errorf("ReopenBead reason = %q; want it to name the reverse tunnel", calls[0].reason)
	}

	if worktreeCreated {
		t.Error("a worktree was created for a refused bead")
	}
	if got := tunnelBuilds.Load(); got != 0 {
		t.Errorf("reverse tunnels constructed = %d; want 0", got)
	}
	if got := reg.InFlight(); got != 0 {
		t.Fatalf("InFlight after the refusal = %d; want 0", got)
	}
}

// TestTunnelRefusal_ReadinessGateReportsAndReopens drives a remote run whose
// worker-side listener never becomes connectable. The gate is the authority on
// that case: it must refuse rather than launch the agent into a dead forward.
func TestTunnelRefusal_ReadinessGateReportsAndReopens(t *testing.T) {
	// Not parallel: swaps a package-level seam in internal/transport/tunnel and
	// the process PATH.
	projectDir := remotefixRepo(t)
	// Exit 1: every `nc -z` probe fails, so the listener never comes up.
	sshLog := remotefixSSHShim(t, 1)
	remotefixTunnelSeam(t, remotefixIdleTunnel)

	reg, preSelected := remotefixReserveWorker(t)

	var worktreeCreated bool
	worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
		worktreeCreated = true
		t.Error("worktree factory ran on a run the readiness gate refused")
		return "", func() {}, nil
	}

	ledger := &runplanacqLedger{}
	bus := &runplanBus{}

	params := remotefixParams(t, projectDir)
	params.BrAdapter = ledger
	params.Bus = bus
	params.AdapterRegistry2 = runplanacqSealedRegistry(t)
	params.WorkerRegistry = reg
	params.WorktreeFactory = worktreeFactory
	deps := ExportedWorkLoopDeps(params)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	probed := make(chan bool, 1)
	go func() { probed <- tunrefuseWaitForProbe(sshLog, cancel, 30*time.Second) }()

	bead := remotefixBead("hk-tunrefuse-ready", "readiness-gate refuse probe")
	env := remotefixRunEnv(deps, bead)
	if succeeded := runBeadOneTest(ctx, deps, env, "", preSelected, false); succeeded {
		t.Error("beadRunOne reported success for a refused bead")
	}

	// Positive evidence that the gate ran. Every assertion below is satisfied by
	// a run that died before reaching it.
	if !<-probed {
		t.Fatal("the readiness gate never probed the worker, so this test measured something else")
	}

	payload := tunrefuseTunnelPayload(t, bus)
	if payload.WorkerName != remotefixWorker.Name {
		t.Errorf("worker_tunnel_failed worker_name = %q; want %q", payload.WorkerName, remotefixWorker.Name)
	}
	if !strings.HasPrefix(payload.SocketPath, "tcp://") {
		t.Errorf("worker_tunnel_failed socket_path = %q; want the worker-side TCP endpoint the gate polled", payload.SocketPath)
	}
	runplanWantEvents(t, bus.seen(), []core.EventType{core.EventTypeWorkerTunnelFailed})

	calls := ledger.calls()
	if len(calls) != 1 {
		t.Fatalf("ReopenBead call count = %d; want exactly 1\ncalls=%+v", len(calls), calls)
	}
	if calls[0].beadID != bead.BeadID {
		t.Errorf("ReopenBead beadID = %q; want %q", calls[0].beadID, bead.BeadID)
	}
	if !strings.Contains(calls[0].reason, "reverse-tunnel not ready") {
		t.Errorf("ReopenBead reason = %q; want it to name the reverse tunnel", calls[0].reason)
	}

	if worktreeCreated {
		t.Error("a worktree was created for a run the readiness gate refused")
	}
	if got := reg.InFlight(); got != 0 {
		t.Fatalf("InFlight after the refusal = %d; want 0", got)
	}
}
