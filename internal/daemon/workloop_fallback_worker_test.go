package daemon

// workloop_fallback_worker_test.go — the fallback worker selection in
// beadRunOne, and the three things it does that nothing measured.
//
// # What the block is
//
// The outer dispatch loop usually picks the worker and reserves its slot before
// it starts the run. Sometimes it cannot: every slot was busy when the loop
// peeked. beadRunOne then gets a nil pre-selected worker, and a slot can still
// free up between that peek and the run. The fallback block is what takes it.
//
// # Why this test exists
//
// Every other test of the remote path hands beadRunOne a worker the caller
// already reserved. Nothing drove it with no pre-selected worker against a
// registry that held a free slot, so the whole block could be deleted and the
// suite stayed green. Three things in it were unmeasured: the worker slot it
// takes and gives back, the early give-back of the daemon's local in-flight
// count once the run turns out to be remote after all, and the Remote mirror on
// the run handle that stops the run counting against the per-queue local cap.
//
// # How it avoids passing for the wrong reason
//
// "The registry has a free slot at the end" and "the local count is back where
// it started" are both true of a run that took neither. So every give-back claim
// here is paired with positive evidence, read from INSIDE the run, that the
// machinery ran first: the slot was taken, the local count was handed back
// early, and the handle was marked remote. The reading point is the tunnel port
// allocator, which is the first thing the remote path reaches after the fallback
// block and is unreachable on a local run.
//
// The allocator then refuses, which ends the run at a point that acquires
// nothing further. That keeps the test to the block it is about.
//
// # Why there are two cases and not one
//
// The block has two ways to find a worker: by name when the queue item asks for
// one, and by free slot otherwise. They share everything after the selection, so
// the two cases run one assertion body. That makes them LOOK like a copy, and a
// later reader is right to ask whether the second one earns its keep.
//
// It does, and here is the evidence rather than the claim. Replace the body of
// the by-name branch with `w = nil` and ONLY the by-name case goes red — the
// free-slot case stays green, because it never reaches that branch. Neither case
// covers the other. Do not collapse them to one.
//
// Bead: hk-edfx1.

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

// fallbackwrkQueue is the queue the run is registered under. It is named rather
// than empty because two of the assertions below are per-queue tallies, and a
// tally over the empty name reads as an accident.
const fallbackwrkQueue = "fallbackwrk-queue"

// fallbackwrkSeen is what the run looked like from inside, at the first point on
// the remote path after the fallback block. Every field is a fact that is gone
// by the time beadRunOne returns.
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
			// Not parallel: swaps two package-level seams in
			// internal/transport/tunnel and the process PATH.
			projectDir := remotefixRepo(t)

			// Exit 1: this run refuses before its first ssh, so nothing reads the
			// exit code. Non-zero means a regression that DOES call ssh fails
			// rather than getting a fake success to carry on with.
			sshLog := remotefixSSHShim(t, 1)

			// A registry with one free slot, and NOTHING reserved. This is the
			// state the fallback exists for: the dispatch loop found no worker,
			// so it started the run as local, and a slot freed up before the run
			// reached the selection.
			reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{remotefixWorker}})
			if got := reg.InFlight(); got != 0 {
				t.Fatalf("setup: InFlight = %d; want 0 — the fallback must find the slot itself", got)
			}
			if !reg.HasFreeSlot() {
				t.Fatal("setup: HasFreeSlot = false; the fallback would have nothing to find")
			}

			// A tunnel this run must never build. The port allocation below
			// refuses first, so a constructed tunnel means the refusal moved.
			remotefixTunnelSeam(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
				t.Errorf("a reverse tunnel was constructed for a run that refused at port allocation: %s %v", name, args)
				return exec.CommandContext(ctx, "true")
			})

			// A worktree factory that fails the test and then refuses. The
			// refusal is what keeps a regression fast: a run that stays local
			// reaches this instead of the remote path, and returning an error
			// ends it here rather than letting it go on to launch an agent.
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
			deps := ExportedWorkLoopDeps(params)

			// The dispatch loop increments the local count before it starts a run
			// it believes is local, and hands localSlotHeld=true to the run that
			// owes it back. Without this the give-back site has nothing to give
			// and the test measures nothing.
			ExportedStoreLocalInFlight(deps, 1)
			if got := deps.localInFlight.Load(); got != 1 {
				t.Fatalf("setup: localInFlight = %d; want 1", got)
			}

			bead := remotefixBead("hk-fallbackwrk-probe", "fallback worker selection probe")
			env := deps.runEnv(core.RunID(uuid.New()), bead, fallbackwrkQueue, nil, nil, 0, "", "", nil, false, tc.workerTarget, core.AgentType(""))

			// The reading point. AllocatePort is the first call on the remote
			// tunnel path, so it runs after the fallback block and only when the
			// fallback selected a worker. It records what the run holds, then
			// refuses.
			var seen fallbackwrkSeen
			allocErr := errors.New("fallbackwrk: no free port")
			origAlloc := tunnelpkg.AllocatePort
			t.Cleanup(func() { tunnelpkg.AllocatePort = origAlloc })
			tunnelpkg.AllocatePort = func() (int, error) {
				seen.reached = true
				seen.workerInFlight = reg.InFlight()
				seen.localInFlight = deps.localInFlight.Load()
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

			// ── The run went remote, and it did so through the fallback ───────
			//
			// Nothing pre-selected a worker, so the only way to reach the remote
			// tunnel path is the fallback block. This is what makes every claim
			// below a claim about that block rather than about an empty run.
			if !seen.reached {
				t.Fatal("the run never reached the remote tunnel path: the fallback selected no worker, " +
					"so this run stayed local and measured nothing")
			}

			// The refusal that ends the run names the reverse tunnel, which only
			// a remote run can reach.
			calls := ledger.calls()
			if len(calls) != 1 {
				t.Fatalf("ReopenBead call count = %d; want exactly 1\ncalls=%+v", len(calls), calls)
			}
			if wantReason := "reverse-tunnel not ready: " + allocErr.Error(); calls[0].reason != wantReason {
				t.Errorf("ReopenBead reason = %q; want %q", calls[0].reason, wantReason)
			}
			runplanWantEvents(t, bus.seen(), []core.EventType{core.EventTypeWorkerTunnelFailed})

			// ── 1. The worker slot: taken by the fallback, then given back ────
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

			// ── 2. The local count: handed back as soon as the run is remote ──
			//
			// The dispatch loop took it on a guess that this run was local. The
			// guess is known to be wrong the moment the fallback finds a worker,
			// and the count goes back THERE rather than at the end of the run.
			if seen.localInFlight != 0 {
				t.Errorf("localInFlight during the run = %d; want 0. The run knew it was remote and "+
					"still held the local count, which keeps the split gate closed for the whole run",
					seen.localInFlight)
			}
			// And exactly once. A give-back at both sites drives the count
			// negative and opens the gate wider than the daemon's own ceiling.
			if got := deps.localInFlight.Load(); got != 0 {
				t.Errorf("localInFlight after the run = %d; want 0. The count was given back twice", got)
			}

			// ── 3. The Remote mirror on the handle ───────────────────────────
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

			// Nothing was spent on the worker: the refusal is at the first
			// remote call.
			if sshCalls := remotefixSSHCalls(t, sshLog); len(sshCalls) != 0 {
				t.Errorf("the run made %d ssh call(s) after a failed port allocation; want 0\ncalls:\n%s",
					len(sshCalls), strings.Join(sshCalls, "\n"))
			}
		})
	}
}
