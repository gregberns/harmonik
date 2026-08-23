package daemon

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
	projectDir := remotefixRepo(t)
	sshLog := remotefixSSHShim(t, 1)

	allocErr := errors.New("tunport: no free port")
	origAlloc := tunnelpkg.AllocatePort
	t.Cleanup(func() { tunnelpkg.AllocatePort = origAlloc })
	tunnelpkg.AllocatePort = func() (int, error) { return 0, allocErr }

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
	deps := ExportedTestRuntime(params)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bead := remotefixBead("hk-tunport-probe", "port-alloc refuse probe")
	env := remotefixRunEnv(deps, bead)
	if succeeded := runBeadOneTest(ctx, deps, env, "", preSelected, false); succeeded {
		t.Error("beadRunOne reported success for a refused bead")
	}

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

	runplanWantEvents(t, bus.seen(), []core.EventType{core.EventTypeWorkerTunnelFailed})

	if worktreeCreated {
		t.Error("a worktree was created for a refused bead")
	}
	if got := reg.InFlight(); got != 0 {
		t.Fatalf("InFlight after the refusal = %d; want 0", got)
	}
	if !reg.HasFreeSlot() {
		t.Fatal("HasFreeSlot after the refusal = false; the slot was not returned")
	}

	if sshCalls := remotefixSSHCalls(t, sshLog); len(sshCalls) != 0 {
		t.Errorf("the run made %d ssh call(s) after a failed port allocation; want 0\ncalls:\n%s",
			len(sshCalls), strings.Join(sshCalls, "\n"))
	}
	if got := tunnelBuilds.Load(); got != 0 {
		t.Errorf("reverse tunnels constructed = %d; want 0", got)
	}
}
