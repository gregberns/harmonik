package daemon

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

var tunrefuseWorker = workers.Worker{
	Name:     "tunrefuse-worker",
	Host:     "tunrefuse.invalid",
	Enabled:  true,
	MaxSlots: 1,
	RepoPath: "/tmp/tunrefuse-worker-repo",
}

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

const tunrefuseProbeMark = "'nc' '-z'"

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
	projectDir := hooksockDeepRepo(t)
	sshLog := remotefixSSHShim(t, 1)

	var tunnelBuilds atomic.Int32
	remotefixTunnelSeam(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		tunnelBuilds.Add(1)
		t.Errorf("a reverse tunnel was constructed for a run whose hook socket path is too long: %s %v", name, args)
		return exec.CommandContext(ctx, "true")
	})

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
	deps := ExportedTestRuntime(params)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bead := remotefixBead("hk-tunrefuse-sock", "fallback-path socket-path refuse probe")
	env := remotefixRunEnv(deps, bead)
	if succeeded := runBeadOneTest(ctx, deps, env, "", nil, false); succeeded {
		t.Error("beadRunOne reported success for a refused bead")
	}

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
	projectDir := remotefixRepo(t)
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
	deps := ExportedTestRuntime(params)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	probed := make(chan bool, 1)
	go func() { probed <- tunrefuseWaitForProbe(sshLog, cancel, 30*time.Second) }()

	bead := remotefixBead("hk-tunrefuse-ready", "readiness-gate refuse probe")
	env := remotefixRunEnv(deps, bead)
	if succeeded := runBeadOneTest(ctx, deps, env, "", preSelected, false); succeeded {
		t.Error("beadRunOne reported success for a refused bead")
	}

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
