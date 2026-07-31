package daemon

// tunnel_portalloc_refusal_test.go — a remote run whose tunnel port
// allocation fails refuses at the point of failure, and takes nothing.
//
// The port is the FIRST thing the remote tunnel block acquires. When the
// allocation failed, the block used to log and carry on: it spent an ssh round
// trip to make a directory on the worker, then started a real
// `ssh -N -R 127.0.0.1:0:…` that could never carry traffic, and only the
// readiness gate ten seconds later failed the run. Every one of those was
// spent on a run that was already lost.
//
// This test is the sibling of the hook-socket refusal test in
// runplan_hooksocket_test.go and of the refusal invariant in
// runplan_before_acquisition_test.go, and it asserts the same shape they do —
// one reopen, no worktree, the worker slot back — plus the two costs that are
// unique to this refusal: no ssh round trip, and no tunnel process.
//
// Those two are "X did not happen" claims, which any dead harness satisfies
// for free. They are worth something here only because both recorders fire
// when the refusal is removed: with the refusal reverted, the ssh shim's log
// carries the mkdir round trip and the tunnel seam records an argv.
//
// The remote fixture — the repository, the ssh shim, the tunnel seam, the
// reserved worker slot, the bead and the run environment — lives in
// remoterunfixture_test.go and is shared with the cold-start token tests. Only
// what this test varies is below.

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	tunnelpkg "github.com/gregberns/harmonik/internal/transport/tunnel"
)

// TestTunnelSetup_FailedPortAllocRefusesBeforeSpendingAnything drives a remote
// run whose port allocation fails through beadRunOne and asserts the refusal is
// complete and free: the bead is reopened once with the reverse-tunnel reason,
// the one worker_tunnel_failed event is emitted, the pre-reserved worker slot
// comes back, no worktree is created, no ssh round trip is made, and no tunnel
// process is constructed.
func TestTunnelSetup_FailedPortAllocRefusesBeforeSpendingAnything(t *testing.T) {
	// Not parallel: swaps two package-level seams in internal/transport/tunnel
	// and the process PATH.
	projectDir := remotefixRepo(t)
	// Exit 1: this run must make no ssh call at all, so nothing here reads the
	// exit code. It is non-zero so that a regression which DOES call ssh gets a
	// failure rather than a fake success to carry on with.
	sshLog := remotefixSSHShim(t, 1)

	// The failure this test exists for. It is unreachable without the seam:
	// the real allocator fails only when box A can hand out no loopback port.
	allocErr := errors.New("tunport: no free port")
	origAlloc := tunnelpkg.AllocatePort
	t.Cleanup(func() { tunnelpkg.AllocatePort = origAlloc })
	tunnelpkg.AllocatePort = func() (int, error) { return 0, allocErr }

	// The tunnel seam records rather than spawns, so a tunnel started for this
	// run is a test failure and not a stray ssh process.
	var tunnelBuilds atomic.Int32
	remotefixTunnelSeam(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		tunnelBuilds.Add(1)
		t.Errorf("a reverse tunnel was constructed for a run whose port allocation failed: %s %v", name, args)
		return exec.CommandContext(ctx, "true")
	})

	reg, preSelected := remotefixReserveWorker(t)

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

	bead := remotefixBead("hk-tunport-probe", "port-alloc refuse probe")
	env := remotefixRunEnv(deps, bead)
	if succeeded := runBeadOneTest(ctx, deps, env, "", preSelected, false); succeeded {
		t.Error("beadRunOne reported success for a refused bead")
	}

	// The refusal fired once, and named the reverse tunnel and the cause.
	calls := ledger.calls()
	if len(calls) != 1 {
		t.Fatalf("ReopenBead call count = %d; want exactly 1\ncalls=%+v", len(calls), calls)
	}
	if calls[0].beadID != bead.BeadID {
		t.Errorf("ReopenBead beadID = %q; want %q", calls[0].beadID, bead.BeadID)
	}
	wantReason := "reverse-tunnel not ready: " + allocErr.Error()
	if calls[0].reason != wantReason {
		t.Errorf("ReopenBead reason = %q; want %q", calls[0].reason, wantReason)
	}

	// The one event this refusal owes, and no other.
	runplanWantEvents(t, bus.seen(), []core.EventType{core.EventTypeWorkerTunnelFailed})

	// Nothing was taken.
	if worktreeCreated {
		t.Error("a worktree was created for a refused bead")
	}
	if got := reg.InFlight(); got != 0 {
		t.Fatalf("InFlight after the refusal = %d; want 0", got)
	}
	if !reg.HasFreeSlot() {
		t.Fatal("HasFreeSlot after the refusal = false; the slot was not returned")
	}

	// Nothing was spent on the worker either: no ssh round trip, and no
	// `ssh -N -R` process for a tunnel that could never carry traffic.
	if sshCalls := remotefixSSHCalls(t, sshLog); len(sshCalls) != 0 {
		t.Errorf("the run made %d ssh call(s) after a failed port allocation; want 0\ncalls:\n%s",
			len(sshCalls), strings.Join(sshCalls, "\n"))
	}
	if got := tunnelBuilds.Load(); got != 0 {
		t.Errorf("reverse tunnels constructed = %d; want 0", got)
	}
}
