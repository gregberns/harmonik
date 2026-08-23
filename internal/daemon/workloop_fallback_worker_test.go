package daemon

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	tunnelpkg "github.com/gregberns/harmonik/internal/transport/tunnel"
	"github.com/gregberns/harmonik/internal/workers"
)

const fallbackwrkQueue = "fallbackwrk-queue"

type fallbackwrkSeen struct {
	// reached is false when the run never got to the remote tunnel path at all,
	// which is what a run that stayed local looks like.
	reached bool
	// workerInFlight is the registry's in-flight count. 1 means the fallback
	// took the slot.
	workerInFlight int
	// localInFlight is the daemon's local in-flight count. 0 means the run
	// handed back the count the dispatch loop took on its behalf.
	localInFlight int32
	// remote is the handle's Remote flag.
	remote bool
	// queueTally and queueLocalTally are the per-queue counts the capacity gate
	// reads. The pair is the point: the run is in flight, and it does not count
	// against the queue's LOCAL ceiling.
	queueTally, queueLocalTally int
}

// TestBeadRunOne_FallbackWorkerSelectionTakesTheSlotAndGivesItBack drives
// beadRunOne with no pre-selected worker, a registry that holds a free slot, and
// the local in-flight count the dispatch loop takes before it starts a run it
// believes is local.
//
// The run must find the free slot, take it, hand the local count back at once,
// mark itself remote, and give the worker slot back on its way out.
func TestBeadRunOne_FallbackWorkerSelectionTakesTheSlotAndGivesItBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		// workerTarget is the queue item's placement intent. Empty picks any
		// worker with a free slot; a name picks that worker.
		workerTarget string
	}{
		{name: "no target, so the fallback takes any free slot", workerTarget: ""},
		{name: "a target names the worker", workerTarget: remotefixWorker.Name},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := remotefixRepo(t)

			sshLog := remotefixSSHShim(t, 1)

			reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{remotefixWorker}})
			if got := reg.InFlight(); got != 0 {
				t.Fatalf("setup: InFlight = %d; want 0 — the fallback must find the slot itself", got)
			}
			if !reg.HasFreeSlot() {
				t.Fatal("setup: HasFreeSlot = false; the fallback would have nothing to find")
			}

			remotefixTunnelSeam(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
				t.Errorf("a reverse tunnel was constructed for a run that refused at port allocation: %s %v", name, args)
				return exec.CommandContext(ctx, "true")
			})

			worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
				t.Error("worktree factory ran: the run stayed local, so the fallback never selected a worker")
				return "", func() {}, errors.New("fallbackwrk: no worktree for a run that should have gone remote")
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

			ExportedStoreLocalInFlight(deps, 1)
			if got := deps.handles.LocalInFlight.Load(); got != 1 {
				t.Fatalf("setup: localInFlight = %d; want 1", got)
			}

			bead := remotefixBead("hk-fallbackwrk-probe", "fallback worker selection probe")
			env := deps.runEnv(core.RunID(uuid.New()), bead, fallbackwrkQueue, tc.workerTarget, core.AgentType(""))

			var seen fallbackwrkSeen
			allocErr := errors.New("fallbackwrk: no free port")
			origAlloc := tunnelpkg.AllocatePort
			t.Cleanup(func() { tunnelpkg.AllocatePort = origAlloc })
			tunnelpkg.AllocatePort = func() (int, error) {
				seen.reached = true
				seen.workerInFlight = reg.InFlight()
				seen.localInFlight = deps.handles.LocalInFlight.Load()
				if h, ok := deps.runRegistry.Get(env.RunID); ok {
					seen.remote = h.Remote.Load()
				}
				seen.queueTally = deps.runRegistry.LenForQueue(fallbackwrkQueue)
				seen.queueLocalTally = deps.runRegistry.LenForQueueLocal(fallbackwrkQueue)
				return 0, allocErr
			}

			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			if succeeded := runBeadOneTest(ctx, deps, env, "", nil, true); succeeded {
				t.Error("beadRunOne reported success for a refused bead")
			}

			if !seen.reached {
				t.Fatal("the run never reached the remote tunnel path: the fallback selected no worker, " +
					"so this run stayed local and measured nothing")
			}

			calls := ledger.calls()
			if len(calls) != 1 {
				t.Fatalf("ReopenBead call count = %d; want exactly 1\ncalls=%+v", len(calls), calls)
			}
			if wantReason := "reverse-tunnel not ready: " + allocErr.Error(); calls[0].reason != wantReason {
				t.Errorf("ReopenBead reason = %q; want %q", calls[0].reason, wantReason)
			}
			runplanWantEvents(t, bus.seen(), []core.EventType{core.EventTypeWorkerTunnelFailed})

			if seen.workerInFlight != 1 {
				t.Errorf("InFlight during the run = %d; want 1. The fallback reached the remote path "+
					"without reserving the slot it selected", seen.workerInFlight)
			}
			if got := reg.InFlight(); got != 0 {
				t.Errorf("InFlight after the run = %d; want 0. The slot the fallback took was never "+
					"given back, which over-counts the registry until the remote path wedges", got)
			}
			if !reg.HasFreeSlot() {
				t.Error("HasFreeSlot after the run = false; the slot was not returned to the pool")
			}

			if seen.localInFlight != 0 {
				t.Errorf("localInFlight during the run = %d; want 0. The run knew it was remote and "+
					"still held the local count, which keeps the split gate closed for the whole run",
					seen.localInFlight)
			}
			if got := deps.handles.LocalInFlight.Load(); got != 0 {
				t.Errorf("localInFlight after the run = %d; want 0. The count was given back twice", got)
			}

			if !seen.remote {
				t.Error("the run handle was not marked remote, so the per-queue capacity gate counts " +
					"a remote run against the queue's LOCAL ceiling")
			}
			if seen.queueTally != 1 {
				t.Fatalf("LenForQueue(%q) during the run = %d; want 1. The handle was not registered "+
					"under the queue, so the local tally below proves nothing", fallbackwrkQueue, seen.queueTally)
			}
			if seen.queueLocalTally != 0 {
				t.Errorf("LenForQueueLocal(%q) during the run = %d; want 0. The run is in flight and "+
					"remote, so it must not count against the per-queue local cap",
					fallbackwrkQueue, seen.queueLocalTally)
			}

			if sshCalls := remotefixSSHCalls(t, sshLog); len(sshCalls) != 0 {
				t.Errorf("the run made %d ssh call(s) after a failed port allocation; want 0\ncalls:\n%s",
					len(sshCalls), strings.Join(sshCalls, "\n"))
			}
		})
	}
}
