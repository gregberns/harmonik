package daemon

// scheduler.go — the daemon's dispatch scheduler: runWorkLoop and the helpers
// only it reaches.
//
// One long-lived goroutine for the life of the daemon. It polls, picks a queue,
// applies the admission gates, claims the bead, reserves the queue item, and
// spawns one run goroutine per dispatch.
//
// Seam A (plans/2026-07-27-delete-and-rewrite/DECOMPOSITION-MAP.md §2): what
// crosses to the run path is ONE immutable dispatch decision — run id, bead
// record, queue coordinates, per-item overrides, a pre-selected worker or nil,
// the local-slot-held flag, and the extra context string. What comes back is one
// boolean plus a terminal summary. The run driver itself (beadRunOne) stays in
// workloop.go: a run is one goroutine for 10 to 90 minutes, it fails one bead
// rather than the fleet, and it talks to tmux, ssh, git and the harness CLIs,
// which the scheduler never touches.
//
// This file was cut out of workloop.go with no change to any declaration. Do not
// move a run-path symbol in here, and do not put a scheduler symbol back in
// workloop.go: scripts/workloop-scheduler-freeze-gate.sh fails the build if you
// do.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/orchestrator"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/schedule"
	"github.com/gregberns/harmonik/internal/workers"
)

// workloopPollInterval is the sleep duration used for retry backoff and the
// br-ready fallback poll path (no queue loaded). It is NOT used for
// queue-loaded idle states, which block indefinitely via workloopIdleWait
// per PL-013 (retired-with-stub): idle daemon MUST wait without a periodic
// re-query timer until a queue-submit wake signal or shutdown arrives.
const workloopPollInterval = 2 * time.Second

// shutdownDrainTimeout is the maximum time exitClean waits for in-flight bead
// goroutines to complete their graceful shutdown sequence after the daemon
// context is cancelled (SIGTERM / SIGINT).
//
// In-flight goroutines detect ctx cancellation and run cleanup code such as
// ReopenBead(context.Background(), ...). The worst-case per-goroutine time is
// brcli write-timeout (10 s) + sigtermGrace (5 s) = ~15 s per goroutine.
// Without a bound, three concurrent beads can hold the process alive for ~45 s
// (the original SIGTERM hang observed in hk-az4fd).
//
// This ceiling caps the total drain wait at a predictable window so SIGTERM
// causes the daemon to exit promptly. Goroutines that have not finished within
// the window leave their beads in the in_progress state; QM-002a on next
// startup resets them to open and the queue item recovers to pending.
//
// Bead ref: hk-vlkh4.
const shutdownDrainTimeout = 10 * time.Second

// claimSkipInProgressCooldown is the minimum interval between ShowBead calls
// for a bead that is in_progress with an active run. After the first
// bead_claim_skipped detection, subsequent selection attempts for the same
// bead are skipped without calling ShowBead or emitting another event, until
// this TTL expires. This prevents the ~2.5s spin-loop that produces hundreds
// of bead_claim_skipped events while a run is still in flight. Bead ref: hk-403fw.
const claimSkipInProgressCooldown = 5 * time.Minute

// windowCleaner is the optional interface implemented by substrates that track
// spawned tmux windows. exitClean probes deps.substrate for this interface and
// calls KillAllWindows after wg.Wait() to clean up orphan tmux windows on wave
// completion or daemon exit (hk-j6npz).
type windowCleaner interface {
	KillAllWindows(ctx context.Context) error
}

// maxItemAttempts mirrors queue.MaxItemAttempts for use in the workloop and
// br-ready path. Kept as a package-level alias for readability; the canonical
// value lives in queue.MaxItemAttempts.
//
// Bead ref: hk-6pspu.
const maxItemAttempts = queue.MaxItemAttempts

// queuePreClaimAttemptKey identifies ONE queue item for the queue-path
// pre-claim ShowBead attempt counter (hk-pina9).
//
// Keyed on queueID (NOT queueName): a queue name slot is reusable — `queue
// clear` + a fresh submit installs a NEW QueueID under the same name — so a
// name-keyed counter would carry a dead queue's failures onto a fresh item.
// itemIdx + beadID together pin the exact item: itemIdx alone is reused when a
// bead is re-appended to a group, and beadID alone would conflate two entries
// for the same bead in one stream group (the hk-wifef re-append case).
//
// Bead ref: hk-pina9.
type queuePreClaimAttemptKey struct {
	queueID    string
	groupIndex int
	itemIdx    int
	beadID     core.BeadID
}

// The cadenced maintenance — the schedule tick, the coordinator reap, the disk
// check, the dashboard forcing gate, the eager refill and the sentinel governor
// — left this loop for loopmaintenance.go (DECOMPOSITION-MAP §3 Step 2). It
// carries loopMaintenanceState, periodicCoordinatorReapInterval, and the two
// maintenance passes the loop calls below.
//
// runWorkLoop is the main dispatch goroutine. It blocks until ctx is cancelled
// (typically from SIGINT/SIGTERM received by the daemon process). On context
// cancellation it stops accepting new beads, waits for all in-flight goroutines
// to finish, then returns nil. Non-nil errors indicate a fatal setup failure
// within the loop itself (never an error from a single bead run — those are
// absorbed and result in ReopenBead).
//
// Goroutine-per-bead model (hk-e61c3.2, POST_OPERATIONAL_PARALLELISM_ROADMAP row 5):
//
// Each iteration of the outer poll loop:
//  1. Check context cancellation.
//  2. Split capacity gate (hk-hs7ex): if localInFlight >= gateMax AND no
//     remote worker has a free slot: sleep and retry (local at hard cap).
//     When a worker has a free slot, admit — SelectWorker routes remotely
//     and localInFlight is NOT incremented for that run.
//  3. Queue-pull path (when queueStore is set and has an active queue):
//     3a. If queue status is paused or completed, idle-wait.
//     3b. Get the active group's eligible items via EligibleItems().
//     3c. Pick the first eligible item, claim it, and dispatch.
//     (EM-015f group-advance evaluation fires in the goroutine on run completion.)
//  4. Fallback br-ready poll (when no queue is loaded): poll br ready; if none
//     sleep and retry. This path preserves backward compatibility for tests and
//     single-bead dispatch that do not use the queue surface.
//  5. Spawn goroutine: Register → dispatch (worktree+handler) → Unregister.
//
// Goroutine dispatch path:
//  1. resolveHEAD + CreateWorktree.
//  2. emitRunStarted (with optional queue_id + queue_group_index).
//  3. Route to mode-specific driver (dot, review-loop, or single).
//  4. CloseBead or ReopenBead based on outcome.
//  5. On queue-dispatched run: update item status + evaluate EM-015f group advance.
//  6. removeWorktree.
//  7. Unregister from runRegistry.
//
// At MaxConcurrent=1 the loop is semantically equivalent to the prior serial
// implementation: only one goroutine is ever in-flight, so the poll loop
// blocks on capacity before polling again.
//
// Shutdown: when ctx is cancelled the outer loop exits immediately. The
// embedded WaitGroup wg waits for all in-flight goroutines to drain before
// runWorkLoop returns, satisfying the per-run Drain guarantee (hk-fx6zl).
//
// Spec ref: specs/execution-model.md §7.4 (TS-1 dispatch loop pseudocode);
// §4.3.EM-015f (group-advance gate).
// Bead ref: hk-45ude.

// queueSelection is the result of selectNextQueue: the snapshot of one eligible
// (queue, active group, first eligible item) chosen by the cross-queue
// round-robin policy under the QueueStore write lock. All fields are local
// copies; the *Queue pointer is NOT retained, so the caller may release the
// lock and re-acquire it for the dispatch stamp (Phase 3) without holding a
// stale reference.
//
// Bead ref: hk-tigaf.4 (NQ-B1).
type queueSelection struct {
	queueName        string
	queueID          string
	groupIndex       int
	itemIdx          int
	itemBeadID       core.BeadID
	itemContext      string
	itemWFMode       string
	itemWFRef        string
	itemTemplateMap  map[string]string
	anyEligible      bool // true if any queue had an active group with eligible items
	anyPausedOrEmpty bool // true if at least one queue existed but contributed nothing
	// Per-queue routing fields (hk-f10xl [L5 Move 2]).
	queueLocalOnly      bool           // mirrors Queue.LocalOnly — skip SelectWorker when true
	queueWorkerTarget   string         // mirrors Queue.WorkerTarget — pin to named worker when non-empty
	queueDefaultHarness core.AgentType // mirrors Queue.DefaultHarness — tier-2 harness default
}

// effectiveQueueWorkers resolves the per-queue worker ceiling for q, defaulting
// a zero/absent Workers field to the global cap (QM-066). Mirrors
// queue.DefaultWorkers but lives here so the workloop never imports the global
// cap into the queue package's submit path twice.
//
// Bead ref: hk-tigaf.4 (NQ-B1).
func effectiveQueueWorkers(q *queue.Queue, globalCap int) int {
	return queue.DefaultWorkers(q.Workers, globalCap)
}

// selectNextQueue implements the QM-062/QM-067 two-level capacity gate plus the
// cross-queue round-robin dispatch policy. Called once per dispatch tick while
// holding the QueueStore write lock (via lq). It scans every loaded queue and
// returns the next (queue, active group, first eligible item) to dispatch, or
// (queueSelection{}, false) when no queue can contribute under its own
// per-queue Workers cap and the global ceiling.
//
// Policy (NQ-B1):
//   - A queue is a candidate iff it is QueueStatusActive, has an active group
//     with at least one eligible item, AND its in-flight tally
//     (runRegistry.LenForQueue(name)) is below its effective Workers ceiling.
//   - Candidate queue names are sorted lexicographically (name-ordered), then a
//     daemon-state cursor (*rrCursor, advanced by the CALLER every tick) selects
//     the starting offset. The cursor is NOT reset to 0 each tick: that is what
//     prevents a lexicographically-earlier queue (e.g. "investigate") from
//     perpetually starving a later one (e.g. "main"). This is plain round-robin,
//     explicitly NOT weighted fairness (deferred to v0.2 / N3).
//
// The global ceiling (runRegistry.Len() < globalCap) is enforced by the CALLER
// before invoking selectNextQueue; this function enforces only the per-queue
// cap so the two levels compose to min(group_pending, per_queue_workers -
// queue_running, global_cap - global_running) per QM-062.
//
// Bead ref: hk-tigaf.4 (NQ-B1).
//
// blockedQueues is the hk-xg6rw dashboard forcing-gate set: queue names
// present (with value true) are captain-curated queues currently gated by a
// stale dashboard.json. A gated queue contributes nothing to dispatch this
// tick but — like a paused-by-failure queue — MUST NOT block sibling queues.
// nil disables the gate (pre-hk-xg6rw behaviour).
func selectNextQueue(lq *queuewiring.LockedQueueStore, reg *RunRegistry, globalCap, rrCursor int, blockedQueues map[string]bool) (queueSelection, bool) {
	// M5 slice 3A: the pure NQ-B1 decision moved to internal/orchestrator. This
	// shell projects the live QueueStore/RunRegistry into a narrow FleetSnapshot
	// under the (already-held) write lock, calls the pure selector, and maps the
	// Selection back onto queueSelection so callers are unchanged. The Phase-3
	// claim-time re-validation downstream (see the dispatch stamp block) remains
	// the load-bearing race guardrail.
	sel, ok := orchestrator.SelectNextQueue(snapshotFleet(lq, reg, globalCap, rrCursor, blockedQueues))
	if !ok {
		return queueSelection{anyPausedOrEmpty: sel.SawNonContributing}, false
	}
	return queueSelection{
		queueName:           sel.QueueName,
		queueID:             sel.QueueID,
		groupIndex:          sel.GroupIndex,
		itemIdx:             sel.Item.ItemIdx,
		itemBeadID:          sel.Item.BeadID,
		itemContext:         sel.Item.Context,
		itemWFMode:          sel.Item.WorkflowMode,
		itemWFRef:           sel.Item.WorkflowRef,
		itemTemplateMap:     sel.Item.TemplateParams,
		anyEligible:         true,
		queueLocalOnly:      sel.LocalOnly,
		queueWorkerTarget:   sel.WorkerTarget,
		queueDefaultHarness: sel.DefaultHarness,
	}, true
}

// snapshotFleet projects the live QueueStore/RunRegistry into the narrow
// orchestrator.FleetSnapshot the pure selector reads (M5 slice 3A). It is the
// single queue.* → snapshot mapping point, built while the caller holds the
// QueueStore write lock (mirrors drainSnapshot in draindetect.go). WorkerCap is
// precomputed here via effectiveQueueWorkers so orchestrator never imports
// internal/queue; enum-typed status/kind fields are projected as booleans.
func snapshotFleet(lq *queuewiring.LockedQueueStore, reg *RunRegistry, globalCap, rrCursor int, blockedQueues map[string]bool) orchestrator.FleetSnapshot {
	names := lq.LockedAllQueueNames()
	queues := make([]orchestrator.QueueSnapshot, 0, len(names))
	for _, name := range names {
		q := lq.LockedQueueByName(name)
		if q == nil {
			continue
		}
		queues = append(queues, orchestrator.QueueSnapshot{
			Name:           name,
			QueueID:        q.QueueID,
			Active:         q.Status == queue.QueueStatusActive,
			Blocked:        blockedQueues[name],
			LocalInFlight:  reg.LenForQueueLocal(name),
			WorkerCap:      effectiveQueueWorkers(q, globalCap),
			LocalOnly:      q.LocalOnly,
			WorkerTarget:   q.WorkerTarget,
			DefaultHarness: q.DefaultHarness,
			ActiveGroup:    projectActiveGroup(q),
		})
	}
	return orchestrator.FleetSnapshot{Queues: queues, RRCursor: rrCursor}
}

// projectActiveGroup projects q's FIRST active group into a GroupSnapshot (nil
// when none), stamping each eligible item's ABSOLUTE index into Group.Items so
// the dispatch stamp lands on the right item (addendum fix #1). The absolute
// index is resolved exactly as the legacy selectNextQueue did: the first
// Items entry matching the eligible item's BeadID with ItemStatusPending.
//
//nolint:gocognit // pre-existing: Seam A moved this code out of workloop.go unchanged
func projectActiveGroup(q *queue.Queue) *orchestrator.GroupSnapshot {
	for gi := range q.Groups {
		if q.Groups[gi].Status != queue.GroupStatusActive {
			continue
		}
		g := &q.Groups[gi]
		// PendingCount is counted over ALL group items (not just the eligible
		// head) so it faithfully mirrors the daemon's original eager-fill pending
		// scan (eagerfill_em063.go). It feeds orchestrator.EagerFillTarget.
		pendingCount := 0
		for ii := range g.Items {
			if g.Items[ii].Status == queue.ItemStatusPending {
				pendingCount++
			}
		}
		eligible := queue.EligibleItems(g)
		items := make([]orchestrator.ItemSnapshot, 0, len(eligible))
		for _, ep := range eligible {
			idx := -1
			for j := range g.Items {
				if g.Items[j].BeadID == ep.BeadID && g.Items[j].Status == queue.ItemStatusPending {
					idx = j
					break
				}
			}
			if idx < 0 {
				continue
			}
			it := &g.Items[idx]
			items = append(items, orchestrator.ItemSnapshot{
				ItemIdx:        idx,
				BeadID:         it.BeadID,
				Context:        it.Context,
				WorkflowMode:   it.WorkflowMode,
				WorkflowRef:    it.WorkflowRef,
				TemplateParams: it.TemplateParams,
			})
		}
		return &orchestrator.GroupSnapshot{
			GroupIndex:   g.GroupIndex,
			Eligible:     items,
			Kind:         string(g.Kind),
			PendingCount: pendingCount,
		}
	}
	return nil
}

//nolint:gocognit,cyclop,funlen // pre-existing: Seam A moved this code out of workloop.go unchanged
func runWorkLoop(ctx context.Context, deps workLoopDeps) error {
	// wg tracks all in-flight bead goroutines. runWorkLoop waits on this before
	// returning so callers know all bead work is complete on return.
	var wg sync.WaitGroup

	// RSM-015: own the merge exclusion-domain queue. runWorkLoop CREATES and
	// starts the queue when deps carries none (production, and every StartForTesting
	// / ExportedRunWorkLoop test that does not inject one) — and it alone cancels
	// the owner, on return, which is AFTER every wg.Wait() below, so all in-flight
	// drains complete first (no leak, no lost drain). The owner runs on a
	// background-derived context (NOT the shutdown ctx) so a shutdown-drain merge —
	// submitted on a bgCtx after ctx is cancelled — still executes.
	//
	// A queue supplied by the caller (WithMergeQueue) is left UNTOUCHED: it is
	// already Started and the injector owns its lifecycle. Starting it again would
	// spawn a second owner goroutine draining the same intake channel (concurrent
	// critical sections + a double close(done) panic on shutdown) — the double-Start
	// bug this ownership split exists to prevent.
	if deps.mergeQ == nil {
		deps.mergeQ = mergeq.New(nil)
		mergeQCtx, mergeQCancel := context.WithCancel(context.Background())
		deps.mergeQ.Start(mergeQCtx)
		defer mergeQCancel()
	}

	// effectiveMax: 0-value → 1 to preserve the single-threaded default.
	effectiveMax := deps.maxConcurrent
	if effectiveMax <= 0 {
		effectiveMax = 1
	}

	// claimSem is a buffered-channel semaphore (hk-e61c3.3, POST_OPERATIONAL_PARALLELISM_ROADMAP
	// row 9) that bounds the number of simultaneous ClaimBead SQLite write calls to
	// effectiveMax. A token is acquired before ClaimBead and released immediately
	// after, keeping the SQLite write surface narrow even as effectiveMax goroutines
	// run concurrently. This prevents "BrDbLocked" storms under N>5 ready beads.
	//
	// Anti-pattern (roadmap §6): do NOT push the semaphore into brAdapter. The
	// ceiling belongs here in the work-loop scheduler.
	//
	// Spec ref: specs/execution-model.md §4.11 EM-050 (claim-write serialization token-pool of size max_concurrent).
	// Bead ref: hk-e61c3.3.
	claimSem := make(chan struct{}, effectiveMax)

	// Initialise the held-event dedup map (hk-kac8g).  Written only from this
	// goroutine (outer poll loop) — no locking needed.
	if deps.heldEventDedup == nil {
		deps.heldEventDedup = make(map[string]struct{})
	}
	// lastSeenPauseEpoch tracks the most recent pause epoch observed by the
	// dispatcher.  When the epoch advances (pause lifted or new pause window),
	// all prior-epoch dedup entries are stale and pruned (hk-o48pb).
	lastSeenPauseEpoch := 0

	// rrCursor is the cross-queue round-robin cursor (NQ-B1). It is daemon state:
	// declared here and advanced once per successful queue selection across the
	// ENTIRE life of the loop — never reset to 0 each tick. Resetting it would let
	// a lexicographically-earlier queue (e.g. "investigate") win every tick and
	// starve a later one (e.g. "main"); rotating the start offset round-robins
	// dispatch fairly among active queues. Plain round-robin, NOT weighted
	// fairness (QM-067; weighting deferred to v0.2).
	rrCursor := 0

	// maint owns the cadenced maintenance: the periodic-maintenance timing state
	// (RSM-011) plus the dashboard forcing gate and the sentinel movement
	// governor, both of which are SWITCHABLE subsystems that may be absent. It is
	// touched only from this goroutine. See loopmaintenance.go.
	maint := newLoopMaintenance(deps, os.Stderr)

	// claimSkipInProgressUntil tracks beads whose pre-claim check observed
	// in_progress with an active run. Entries suppress the item from the
	// ShowBead + bead_claim_skipped path until the TTL expires, preventing the
	// ~2.5s spin-loop that emits hundreds of bead_claim_skipped events while a
	// run is in flight. Arms in the non-stranded BI-013c path; expires naturally.
	// Bead ref: hk-403fw.
	claimSkipInProgressUntil := make(map[core.BeadID]time.Time)

	// readyPathAttempts tracks dispatch attempts for each bead on the br-ready
	// fallback path (no queue). Bounded by maxItemAttempts. Resets on daemon
	// restart (acceptable — the br-ready path is the backward-compat fallback).
	//
	// Bead ref: hk-kupeo (ShowBead bounded retry), hk-6pspu (dispatch bound).
	readyPathAttempts := make(map[core.BeadID]int)

	// queuePreClaimShowAttempts tracks consecutive pre-claim ShowBead failures
	// per QUEUE ITEM on the queue path. Bounded by maxItemAttempts; the entry is
	// deleted as soon as ShowBead succeeds (so a transient error episode never
	// accumulates) and when the item is failed at the bound.
	//
	// Deliberately NOT the item's persisted Attempts field, which the hk-6pspu
	// dispatch-stamp bound owns. Sharing that budget would mean two transient
	// ShowBead blips leave the item with only one real dispatch attempt left —
	// a bead that recovers would be failed at the stamp without ever running.
	// The two failure modes get independent budgets. In-memory (like
	// readyPathAttempts) so a daemon restart forgives a transient outage rather
	// than resuming a half-spent budget.
	//
	// Bead ref: hk-pina9.
	queuePreClaimShowAttempts := make(map[queuePreClaimAttemptKey]int)

	// dispatchCtx is the context checked by the outer poll loop to decide
	// whether to halt dispatch. It is separate from ctx (the main daemon context)
	// so that CancelOnQueueDrain/CancelOnQueueExit can stop the dispatch loop
	// without cancelling in-flight goroutines (hk-2o2i9).
	//
	// When deps.stopDispatchCtx is set (wired from Config.StopDispatchCtx by the
	// harmonik run subcommand), the outer loop halts when stopDispatchCtx is
	// cancelled. In-flight goroutines still receive ctx and are unaffected.
	//
	// When deps.stopDispatchCtx is nil, dispatchCtx falls back to ctx, preserving
	// the prior behavior for normal daemon operation and existing tests.
	dispatchCtx := ctx //nolint:contextcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
	if deps.stopDispatchCtx != nil {
		dispatchCtx = deps.stopDispatchCtx
	}

	// exitClean terminates the loop cleanly: it waits for in-flight goroutines
	// (up to shutdownDrainTimeout), kills any orphan tmux windows spawned by this
	// daemon instance (hk-j6npz), then drains any still-active queue to
	// QueueStatusCancelled so the next harmonik run can start without the QM-027
	// "already active" guard blocking it (hk-ppt32). The background context is
	// intentional: by the time exitClean runs, ctx is always cancelled;
	// queue.Persist and KillAllWindows need a live context.
	exitClean := func() error { //nolint:unparam // pre-existing: Seam A moved this code out of workloop.go unchanged
		// Wait for in-flight goroutines with a bounded timeout so SIGTERM always
		// exits promptly (hk-vlkh4). Without a bound, goroutines that run
		// ReopenBead(context.Background(), ...) can each block for up to
		// brcli write-timeout + sigtermGrace (~15 s); with N concurrent beads
		// the daemon hangs for N×15 s. The drain timeout caps the total wait.
		// Goroutines that exceed the window leave beads in_progress; QM-002a
		// at next startup resets them to open.
		drainDone := make(chan struct{})
		go func() {
			wg.Wait()
			close(drainDone)
		}()
		select {
		case <-drainDone:
			// All in-flight goroutines drained cleanly.
		case <-time.After(shutdownDrainTimeout):
			remaining := deps.runRegistry.Len()
			fmt.Fprintf(os.Stderr,
				"daemon: workloop: shutdown: drain timeout after %v with %d run(s) still in-flight; exiting (QM-002a recovers on next start)\n",
				shutdownDrainTimeout, remaining)
		}
		// Kill any tmux windows spawned during this run. deps.substrate is nil
		// when tmux hosting is not used (exec.CommandContext path); the type
		// assertion is a no-op in that case.
		if wc, ok := deps.substrate.(windowCleaner); ok {
			_ = wc.KillAllWindows(context.Background()) //nolint:errcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
		}
		drainCancelledQueue(context.Background(), deps)
		return nil
	}

	// hk-bk33: gate post-boot re-dispatch on spawn-substrate readiness.
	// When a restart-backoff was applied and the substrate exposes a readiness
	// probe, daemon.Start starts a goroutine that probes the substrate and closes
	// this channel when done. Waiting here prevents the first dispatch tick from
	// launching a run before the tmux session is ready to accept new windows,
	// avoiding spurious agent_ready_timeout on QM-002a-reverted beads.
	// Nil on normal (no-backoff) boots — the select is skipped entirely.
	if deps.spawnSubstrateReadyCh != nil {
		select {
		case <-deps.spawnSubstrateReadyCh:
		case <-ctx.Done():
			return exitClean()
		}
	}

	// hk-o85ye: adopt surviving run sessions from the prior daemon instance.
	// Dead sessions were already reset by adoptDeadRunSessions in daemon.Start
	// (before QM-002a so their queue items get reverted to pending).
	// Live sessions need a monitor goroutine: when Claude eventually exits, reset
	// the bead and revert the queue item so the dispatch loop re-dispatches it.
	if deps.projectDir != "" {
		if tmuxAdp := extractTmuxAdapterFromSubstrate(deps.substrate); tmuxAdp != nil {
			if liveRecs, listErr := runpkg.List(deps.projectDir); listErr == nil {
				for _, rec := range liveRecs {
					//nolint:copyloopvar // pre-existing: Seam A moved this code out of workloop.go unchanged
					rec := rec // capture loop variable
					// Track in wg so exitClean's bounded drain awaits an
					// in-progress bead/queue reset (hk-o85ye): without this a
					// SIGTERM could exit mid-write, leaving the bead half-reset.
					// The monitor returns promptly on ctx.Done, so a clean
					// shutdown is not delayed beyond shutdownDrainTimeout.
					wg.Add(1)
					go func() {
						defer wg.Done()
						adoptLiveRunSession(ctx, deps, rec, tmuxAdp)
					}()
				}
			}
		}
	}

	for {
		// Step 1: check for dispatch-halt before pulling new work.
		// Uses dispatchCtx (not ctx) so that CancelOnQueueDrain/CancelOnQueueExit
		// stop dispatch without cancelling in-flight goroutines (hk-2o2i9).
		select {
		case <-dispatchCtx.Done():
			return exitClean()
		default:
		}

		// Step 1b: the pre-dispatch maintenance pass — governor halt check,
		// schedule tick, coordinator reap, disk check (loopmaintenance.go). It
		// reports. This loop decides. In particular the sentinel governor asks for
		// a halt through preObs.halt rather than shutting the daemon down itself,
		// so exitClean stays owned here.
		preObs := maint.tickBeforeDispatch(ctx, &deps)
		if preObs.halt {
			return exitClean()
		}
		if preObs.diskLow {
			if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
				return exitClean()
			}
			continue
		}

		// Step 2: split capacity gate (hk-hs7ex) — local hard sub-cap separate from
		// remote-worker capacity. Read from the controller on every tick when set
		// (hk-ohiaf) so that queue-set-concurrency adjustments take effect without a
		// restart. Raising n lets the local gate admit up to n local runs; remote
		// runs are bounded only by worker.MaxSlots (enforced by SelectWorker).
		//
		// Block only when local is full AND no remote worker has a free slot. When a
		// worker has a free slot the loop proceeds: SelectWorker (hoisted to after
		// ClaimBead in the pre-selection block below) will route the run remotely.
		//
		// Spec ref: specs/execution-model.md §4.11 EM-049 (in-flight-run capacity gate).
		gateMax := effectiveMax
		if deps.concurrencyCtrl != nil {
			gateMax = deps.concurrencyCtrl.Get()
		}
		if int(deps.localInFlight.Load()) >= gateMax {
			if deps.workerRegistry == nil || !deps.workerRegistry.HasFreeSlot() {
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}
		}

		// Step 2b: the post-capacity maintenance pass — dashboard forcing gate,
		// eager refill, sentinel governor evaluation (loopmaintenance.go). It runs
		// AFTER the capacity gate above because that gate's sleep-and-continue path
		// is meant to skip all three for the tick. Its verdict — the set of
		// captain-curated queues withheld from NEW item dispatch — is consulted by
		// selectNextQueue below (Step 3).
		selObs := maint.tickBeforeSelect(ctx, deps, time.Now())

		// Step 3: dispatch source — queue-pull or br-ready fallback.
		//
		// When queueStore is set and has an active queue, pull from the head of
		// the active group per execution-model.md §7.4 (TS-1). The br-ready
		// poll path is the backward-compatible fallback for tests and single-bead
		// dispatch that do not use the queue surface (spec: daemon MUST NOT fall
		// back to br ready when a queue is loaded, per EM-015f).
		//
		// Bead ref: hk-45ude.

		var (
			beadRecord                  core.BeadRecord
			queueItemIndex              int    // item index within the group (-1 = no queue)
			capturedQueueName           string // NQ-B1: name of the dispatching queue ("" = br-ready)
			queueIDField                *string
			queueGroupIdxFd             *int
			capturedExtraContext        string            // hk-boiwe: per-item context from queue.Item.Context
			capturedItemWFMode          string            // hk-hiqrl: per-item workflow mode from queue.Item.WorkflowMode
			capturedItemWFRef           string            // hk-qo9pq: per-item workflow ref from queue.Item.WorkflowRef
			capturedItemTemplateParams  map[string]string // hk-55zv2 / WG-045: template params from queue.Item.TemplateParams
			capturedQueueLocalOnly      bool              // hk-f10xl [L5 Move 2]: per-queue local-only routing gate
			capturedQueueWorkerTarget   string            // hk-f10xl [L5 Move 2]: per-queue worker-target pin
			capturedQueueDefaultHarness core.AgentType    // per-queue tier-2 harness default
		)
		queueItemIndex = -1 // sentinel: not queue-dispatched

		if deps.queueStore != nil {
			// Phase 1 — snapshot queue state under write lock.
			//
			// The previous pattern called deps.queueStore.Queue() (which immediately
			// releases the read lock) and then read q.Status, group statuses, and item
			// statuses without holding any lock. This raced with per-run goroutines that
			// write those fields inside evaluateGroupAdvanceWithOutcome under the write
			// lock. Fix: hold the write lock for the entire initial read so the two
			// never overlap.
			//
			// After Phase 1 the lock is released; all queue-derived values are captured
			// as local copies. Phase 2 (handler-pause gate) runs without the lock.
			// Phase 3 (dispatch stamp) re-acquires the write lock for a TOCTOU check.
			var (
				//nolint:staticcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
				snapItemIdx            int = -1 // -1 → no item found (no queue can contribute)
				snapItemBeadID         core.BeadID
				snapItemContext        string
				snapItemWFMode         string
				snapItemWFRef          string
				snapItemTemplateParams map[string]string
				snapGroupIndex         int
				snapQueueID            string
				snapQueueName          string
			)
			{
				// Two-level capacity gate + cross-queue round-robin (NQ-B1).
				//
				// Prior to named queues this block read the single "main" queue via
				// lq.Queue(). It now scans EVERY loaded queue: it bootstraps each
				// queue's first pending group (hk-veoht), re-evaluates deferred items
				// per active group (hk-nbjht), then selectNextQueue picks the next
				// (queue, group, item) honouring each queue's per-queue Workers cap and
				// the name-ordered round-robin cursor. The global ceiling was already
				// checked at Step 2; selectNextQueue enforces only the per-queue cap so
				// the two compose per QM-062.
				//
				// Spec ref: specs/queue-model.md §9.3 QM-062, §9.7 QM-066, §9.8 QM-067.
				lq := deps.queueStore.LockForMutation()

				// Bootstrap any queue whose first group is still pending. A
				// freshly-submitted/loaded queue persists group 0 as pending and nothing
				// else advances it; activate it inline (under the held write lock) so
				// this same tick can dispatch its items.
				bootstrapped := false
				var bootstrapEvents []core.Event
				for _, name := range lq.LockedAllQueueNames() {
					q := lq.LockedQueueByName(name)
					if q == nil || q.Status != queue.QueueStatusActive {
						continue
					}
					hasActiveGroup := false
					for i := range q.Groups {
						if q.Groups[i].Status == queue.GroupStatusActive {
							hasActiveGroup = true
							break
						}
					}
					if hasActiveGroup {
						continue
					}
					if ok, evts := activateFirstPendingGroupLocked(ctx, deps, lq, q); ok {
						bootstrapped = true
						bootstrapEvents = append(bootstrapEvents, evts...)
					}
				}
				if bootstrapped {
					// A pending group became active — emit started events after lock
					// release, then re-evaluate from the top so the next pass sees the
					// active group and dispatches its items.
					lq.Done()
					for _, evt := range bootstrapEvents {
						raw, mErr := json.Marshal(evt.Payload)
						if mErr != nil {
							raw = evt.Payload
						}
						_ = deps.bus.Emit(ctx, core.EventType(evt.Type), raw) //nolint:errcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
					}
					continue
				}

				// §2.8 deferred-item re-evaluation across every active queue's active
				// group: transition any deferred-for-ledger-dep item whose blockers all
				// resolved back to pending (hk-nbjht). Mutates groups in place under the
				// write lock; persists the owning queue when any flip occurred.
				//
				// hk-gf59k S2-F-S2-2: also track whether any items remain deferred after
				// re-evaluation so the idle path can use a bounded poll rather than an
				// indefinite wait (see hasDeferredItems use below).
				hasDeferredItems := false
				for _, name := range lq.LockedAllQueueNames() {
					q := lq.LockedQueueByName(name)
					if q == nil || q.Status != queue.QueueStatusActive {
						continue
					}
					for gi := range q.Groups {
						if q.Groups[gi].Status != queue.GroupStatusActive {
							continue
						}
						if undeferred, reErr := queue.ReevaluateDeferred(ctx, &q.Groups[gi], deps.queueLedger); reErr != nil {
							fmt.Fprintf(os.Stderr, "daemon: workloop: ReevaluateDeferred queueID=%s groupIndex=%d: %v\n",
								q.QueueID, q.Groups[gi].GroupIndex, reErr)
						} else if len(undeferred) > 0 {
							if persistErr := queue.Persist(ctx, deps.projectDir, q); persistErr != nil {
								fmt.Fprintf(os.Stderr, "daemon: workloop: Persist after ReevaluateDeferred queueID=%s: %v\n",
									q.QueueID, persistErr)
							}
						}
						for _, item := range q.Groups[gi].Items {
							if item.Status == queue.ItemStatusDeferredForLedgerDep {
								hasDeferredItems = true
							}
						}
						break // only the first active group per queue
					}
				}

				// Round-robin selection across all queues honouring per-queue Workers
				// caps. rrCursor is daemon state (declared before the loop) advanced on
				// every successful selection so the start offset rotates — this is what
				// prevents a lexicographically-earlier queue from starving a later one.
				//
				// Asymmetry: we pass effectiveMax (static startup value) rather than gateMax
				// (bandwidth-tuner runtime value) for the per-queue Workers ceiling.  This is
				// intentional: the global gate at Step 2 already blocks dispatch when the tuner
				// reduces gateMax below deps.runRegistry.Len(), so per-queue candidates are never
				// actually dispatched while the global ceiling is throttled.  Per-queue Workers
				// reflects the queue-owner's permanent concurrency intent, not the current tuner
				// state; scaling it with the tuner would under-count eligible queues in the
				// round-robin even when the global gate is the binding constraint.
				sel, ok := selectNextQueue(lq, deps.runRegistry, effectiveMax, rrCursor, selObs.blockedQueues)
				// Capture queue count while the lock is still held so we can
				// distinguish "zero queues loaded" from "queues exist but all
				// paused/at-cap" after lq.Done() releases the lock (hk-mgoo7).
				loadedQueueCount := len(lq.LockedAllQueueNames())
				lq.Done()
				if !ok {
					if loadedQueueCount > 0 {
						// Queues are loaded but none can contribute right now (all
						// paused, drained, or at their per-queue cap). Block until a
						// queue-submit wake signal or shutdown.
						//
						// hk-0es: when a schedule with enabled jobs is loaded the daemon
						// must NOT block indefinitely — it has to re-tick to fire due jobs.
						// scheduleAwareIdleWait bounds the wait by the poll interval in that
						// case so runScheduleTick re-runs at sub-minute latency, while
						// degrading to the indefinite block when no schedule is armed.
						//
						// hk-gf59k S2-F-S2-2: when deferred items remain after re-evaluation
						// (their blockers are still open), use a bounded poll so
						// ReevaluateDeferred re-checks blocker closure on the next tick.
						// Without this, workloopIdleWait blocks indefinitely and deferred
						// chains must be re-submitted to wake the loop — the re-submit churn
						// logged in iter20 (4 full re-submits over 7.5h for a 7-bead chain).
						if hasDeferredItems {
							if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
								return exitClean()
							}
						} else {
							if sleepErr := scheduleAwareIdleWait(dispatchCtx, deps); sleepErr != nil {
								return exitClean()
							}
						}
						continue
					}
					// Zero queues loaded — skip snap assignments so snapItemIdx stays
					// at its -1 sentinel and the br-ready fallback path below handles
					// dispatch. This restores --auto-pull and smoke-test behaviour
					// broken by the NQ-B1 refactor (a027808d).
				} else {
					// Advance the round-robin cursor EVERY time we pick a queue so the
					// next tick starts at the next name (no reset-to-0 → no starvation).
					rrCursor++

					snapItemIdx = sel.itemIdx
					snapItemBeadID = sel.itemBeadID
					snapItemContext = sel.itemContext
					snapItemWFMode = sel.itemWFMode
					snapItemWFRef = sel.itemWFRef
					snapItemTemplateParams = sel.itemTemplateMap
					snapGroupIndex = sel.groupIndex
					snapQueueID = sel.queueID
					snapQueueName = sel.queueName
					// hk-f10xl [L5 Move 2]: capture per-queue routing fields.
					capturedQueueLocalOnly = sel.queueLocalOnly
					capturedQueueWorkerTarget = sel.queueWorkerTarget
					capturedQueueDefaultHarness = sel.queueDefaultHarness
				}
			}

			if snapItemIdx >= 0 {
				// hk-403fw: cooldown guard — skip items whose bead is known to be
				// in_progress with an active run, suppressing repeated ShowBead calls
				// and bead_claim_skipped emissions at poll cadence. The cooldown is
				// armed in the BI-013c non-stranded path below; it expires after
				// claimSkipInProgressCooldown (5 min) and re-evaluates naturally.
				if expiry, ok := claimSkipInProgressUntil[snapItemBeadID]; ok && time.Now().Before(expiry) {
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}

				// Phase 2 — handler-pause gate (hk-kac8g): check whether the resolved
				// agent type is paused before claiming/dispatching the item.  All
				// beads map to AgentTypeClaudeCode; multi-agent resolution is deferred.
				//
				// When paused:
				//   - The item remains ItemStatus=pending (no stamp, no claim).
				//   - Emit queue_item_held_for_handler_pause at-most-once per
				//     (bead_id, paused_epoch) per §8.11.3 dedup contract.
				//   - Idle-wait and retry on next poll tick.
				//
				// Spec ref: specs/handler-pause.md §6.
				// Bead ref: hk-kac8g.
				if deps.handlerPauseController != nil {
					epoch, isPaused := deps.handlerPauseController.PausedEpochFor(core.AgentTypeClaudeCode)
					lastSeenPauseEpoch = pruneHeldDedupOnEpochChange(&deps, epoch, lastSeenPauseEpoch)
					if isPaused {
						emitHeldEvent(ctx, deps, snapItemBeadID, core.AgentTypeClaudeCode, epoch)
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
				}

				// Decision-required dispatch-blocking gate (EV-043, queue path):
				// if the bead has an unacknowledged decision_required pending,
				// hold it without claiming and retry on the next poll tick.
				//
				// Sentinel queue-level gate (FW3 hk-4toh): also hold when the
				// sentinel governor trip is pending, which blocks ALL beads — not
				// just a specific one — until real movement clears the trip. The
				// gate is asked THROUGH the maintenance handle that owns the governor, so
				// it disappears with the subsystem rather than outliving the only
				// code that can open it.
				//
				// Spec ref: specs/event-model.md §4.12 EV-043, EV-043a.
				// Bead ref: hk-pbmsq (bead gate), hk-4toh (sentinel queue gate).
				if deps.decisionBlocker != nil && deps.decisionBlocker.IsBeadBlocked(snapItemBeadID) {
					fmt.Fprintf(os.Stderr,
						"daemon: workloop: bead %s blocked by unacknowledged decision_required (EV-043) — holding\n",
						snapItemBeadID)
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}
				if maint.sentinelBlocksDispatch(deps) {
					fmt.Fprintf(os.Stderr,
						"daemon: workloop: bead %s blocked by sentinel governor trip (EV-043, FW3) — holding until real movement\n",
						snapItemBeadID)
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}

				// Pre-claim status guard for queue-path (BI-013c): between the
				// dispatcher's selection of a queue item and the claim write to Beads,
				// re-read the bead's status via br show and confirm status = open.
				//
				// If the re-read returns a non-open status, skip the claim, emit
				// bead_claim_skipped, and return the item to its group with status
				// deferred-for-ledger-dep per queue-model.md §6 QM-022.
				//
				//   blocked (hk-n91y0): deps-blocked beads fall through to Phase 3
				//     and ClaimBead, where the dedicated guard handles them.
				//
				// hk-lr5t: preClaimRecord is declared outside the anonymous block so
				// its labels/title/description are available at beadRecord construction
				// below (line ~1658). This avoids a second ShowBead round-trip for the
				// most common case; the post-claim ShowBead at ~line 1954 refreshes if
				// anything changed between the pre-claim read and the claim write.
				var preClaimRecord core.BeadRecord
				{
					// hk-pina9: bound the pre-claim ShowBead retry on the QUEUE path.
					// Without a bound this item stays pending at the head of its group
					// and is re-selected every tick forever, so one bead whose `br show`
					// persistently errors wedges the whole queue.
					//
					// Mirrors the br-ready bound (hk-kupeo/hk-6pspu, readyPathAttempts)
					// with ONE deliberate difference: the ready path merely SKIPS the
					// bead, because a ready bead has no queue state and the next poll
					// simply looks past it. A queue item does have state, and skipping
					// it would leave it pending at the head of its group — the wedge we
					// are fixing. So the queue path drives the item to a TERMINAL status
					// (failed) via evaluateGroupAdvanceWithOutcome, which is what lets
					// the group reach allItemsTerminal and advance. `queue resume` resets
					// failed items to pending with Attempts=0, so this is recoverable.
					preClaimKey := queuePreClaimAttemptKey{
						queueID:    snapQueueID,
						groupIndex: snapGroupIndex,
						itemIdx:    snapItemIdx,
						beadID:     snapItemBeadID,
					}
					rec, preClaimErr := deps.brAdapter.ShowBead(ctx, snapItemBeadID)
					if preClaimErr != nil {
						if dispatchCtx.Err() != nil {
							return exitClean()
						}
						queuePreClaimShowAttempts[preClaimKey]++
						preClaimAttempts := queuePreClaimShowAttempts[preClaimKey]
						if preClaimAttempts >= maxItemAttempts {
							delete(queuePreClaimShowAttempts, preClaimKey)
							fmt.Fprintf(os.Stderr,
								"daemon: workloop: ShowBead pre-claim (queue-path) %s failed %d times — failing queue item so the group can advance (hk-pina9): %v\n",
								snapItemBeadID, preClaimAttempts, preClaimErr)
							markQueueItemFailureReason(ctx, deps, snapQueueName, snapGroupIndex, snapItemIdx, snapItemBeadID, "show_bead_failed")
							evaluateGroupAdvanceWithOutcome(ctx, deps, snapQueueName, snapQueueID, snapGroupIndex, snapItemIdx, false)
							continue
						}
						fmt.Fprintf(os.Stderr,
							"daemon: workloop: ShowBead pre-claim (queue-path) %s error (attempt %d/%d, will retry): %v\n",
							snapItemBeadID, preClaimAttempts, maxItemAttempts, preClaimErr)
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
					// Success clears the counter: only a CONSECUTIVE run of failures
					// consumes the budget, so a transient blip never poisons the item.
					delete(queuePreClaimShowAttempts, preClaimKey)
					preClaimRecord = rec
					if preClaimRecord.Status != core.CoarseStatusOpen && preClaimRecord.Status != core.CoarseStatusBlocked {
						// BI-013c: non-open status observed — skip claim, emit bead_claim_skipped.
						fmt.Fprintf(os.Stderr,
							"daemon: workloop: bead_claim_skipped %s observed_status=%s reason=status_changed_between_select_and_claim (BI-013c)\n",
							snapItemBeadID, preClaimRecord.Status)
						skipPayload := core.BeadClaimSkippedPayload{
							BeadID:         string(snapItemBeadID),
							ObservedStatus: string(preClaimRecord.Status),
							Reason:         "status_changed_between_select_and_claim",
							DetectedAt:     time.Now().UTC().Format(time.RFC3339),
						}
						if raw, mErr := json.Marshal(skipPayload); mErr == nil {
							_ = deps.bus.Emit(ctx, core.EventTypeBeadClaimSkipped, raw) //nolint:errcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
						}
						// BI-013c terminal path: closed/tombstone beads are done — fail the
						// queue item directly via evaluateGroupAdvanceWithOutcome so the group
						// can reach allItemsTerminal. Non-terminal statuses (in_progress, draft,
						// deferred, pinned) remain deferred-for-ledger-dep to be re-evaluated
						// on the next poll cycle (hk-3kq05).
						if preClaimRecord.Status == core.CoarseStatusClosed ||
							preClaimRecord.Status == core.CoarseStatusTombstone {
							evaluateGroupAdvanceWithOutcome(ctx, deps, snapQueueName, snapQueueID, snapGroupIndex, snapItemIdx, false)
						} else {
							// hk-l2xd1: in_progress with no active run → auto-reset to break
							// the bead_claim_skipped live-lock that starves sibling queue items.
							// The queue item is still pending (claim was skipped); setting it to
							// deferred-for-ledger-dep is ineffective because ReevaluateDeferred
							// sees no blocking siblings and immediately un-defers it, causing a
							// ~2.5s spin loop. Instead, detect the stranded state and reset the
							// bead to open so the next tick claims it normally.
							if preClaimRecord.Status == core.CoarseStatusInProgress &&
								deps.strandedInProgressResetter != nil &&
								!deps.runRegistry.HasBeadRun(snapItemBeadID) &&
								!strandedBeadHasOnDiskRun(deps.projectDir, snapItemBeadID) {
								if resetErr := deps.strandedInProgressResetter.ResetBead(
									ctx, deps.intentLogDir, deps.brTimeoutCfg,
									snapItemBeadID,
									deps.strandedResetProjectHash,
									deps.strandedResetDaemonNS,
								); resetErr != nil {
									fmt.Fprintf(os.Stderr,
										"daemon: workloop: stranded_bead_auto_reset FAILED bead=%s: %v — bead stays in_progress until next restart\n",
										snapItemBeadID, resetErr)
									// Fall through: set DeferredForLedgerDep to slow the spin
									// even though ReevaluateDeferred will un-defer it quickly.
								} else {
									fmt.Fprintf(os.Stderr,
										"daemon: workloop: stranded_bead_auto_reset bead=%s reason=in_progress_with_no_run (hk-l2xd1)\n",
										snapItemBeadID)
									// Queue item is still pending; the next dispatch tick will
									// see the bead as open and claim it normally. No state
									// change needed here — skip the deferred-for-ledger-dep path.
									if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
										return exitClean()
									}
									continue
								}
							}
							// hk-403fw: arm cooldown for in_progress beads to prevent the
							// poll-cadence spin loop. DeferredForLedgerDep (set below) is
							// immediately un-deferred by ReevaluateDeferred when the item has
							// no in-group blockers, causing repeated ShowBead + claim_skipped
							// at ~2.5s. The cooldown suppresses re-selection for 5 min.
							// Purge stale entries while arming to bound map growth.
							if preClaimRecord.Status == core.CoarseStatusInProgress {
								now := time.Now()
								for id, exp := range claimSkipInProgressUntil {
									if now.After(exp) {
										delete(claimSkipInProgressUntil, id)
									}
								}
								claimSkipInProgressUntil[snapItemBeadID] = now.Add(claimSkipInProgressCooldown)
							}
							// Set the queue item to deferred-for-ledger-dep under the write lock.
							if deps.queueStore != nil {
								lq := deps.queueStore.LockForMutation()
								liveQ := lq.LockedQueueByName(snapQueueName)
								if liveQ != nil {
									for gi := range liveQ.Groups {
										if liveQ.Groups[gi].Status != queue.GroupStatusActive {
											continue
										}
										if liveQ.Groups[gi].GroupIndex != snapGroupIndex {
											continue
										}
										if snapItemIdx < len(liveQ.Groups[gi].Items) &&
											liveQ.Groups[gi].Items[snapItemIdx].BeadID == snapItemBeadID &&
											liveQ.Groups[gi].Items[snapItemIdx].Status == queue.ItemStatusPending {
											liveQ.Groups[gi].Items[snapItemIdx].Status = queue.ItemStatusDeferredForLedgerDep
										}
									}
									lq.LockedSetQueueByName(snapQueueName, liveQ)
									if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
										fmt.Fprintf(os.Stderr, "daemon: workloop: Persist bead_claim_skipped deferred-for-ledger-dep queueID=%s: %v\n",
											liveQ.QueueID, persistErr)
									}
								}
								lq.Done()
							}
						}
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
				}

				// Greenlight gate (AC2 — hk-lacr, queue path): staged deploy+verify beads
				// carry "needs-greenlight" and MUST NOT be dispatched until a captain clears
				// the label via `harmonik greenlight <bead-id>`. This check is independent of
				// --no-auto-pull because it reads the live bead label, not a daemon mode flag.
				// The br-ready path is gated at adapter read time (brcli/ready.go).
				{
					greenlightHeld := false
					for _, lbl := range preClaimRecord.Labels {
						if lbl == labelNeedsGreenlight {
							greenlightHeld = true
							break
						}
					}
					if greenlightHeld {
						fmt.Fprintf(os.Stderr,
							"daemon: workloop: bead %s has needs-greenlight label — holding until captain runs `harmonik greenlight %s`\n",
							snapItemBeadID, snapItemBeadID)
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
				}

				// hk-l5saf: secondary local-cap guard, HOISTED to before the Phase-3
				// dispatch stamp (was previously post-stamp, at the loop-body level).
				// The Step-2 split gate (~line 1818) may have admitted in "remote bypass"
				// mode (localInFlight >= gateMax, HasFreeSlot=true), expecting this bead to
				// route remotely. If the selected item turned out local-only
				// (capturedQueueLocalOnly=true), dispatching it locally would overrun the
				// HARD session cap — so defer WITHOUT stamping, exactly like the sibling
				// hold gates above (handler-pause, decision-required, sentinel, greenlight).
				// The old post-stamp position stranded the item forever: Phase 3 had already
				// stamped ItemStatusDispatched + a placeholder RunID and PERSISTED queue.json,
				// then the guard's sleep+continue left it un-reverted; a Dispatched item with
				// no run/goroutine is never re-selected (only Pending is projected) and no
				// reverter reclaims it, wedging the group until daemon restart. Hoisting is
				// safe because localInFlight is incremented only by this single dispatch
				// goroutine and not until ~line 3072 (post-claim), so a pre-stamp read that
				// is < gateMax stays < gateMax through dispatch.
				if capturedQueueLocalOnly && int(deps.localInFlight.Load()) >= gateMax {
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}

				// Phase 3 — stamp item as dispatched under the write lock (TOCTOU).
				// NQ-B1: operate on the SELECTED queue (snapQueueName), not the "main"
				// slot, so the dispatch stamp lands on the queue the round-robin chose.
				{
					lq := deps.queueStore.LockForMutation()
					liveQ := lq.LockedQueueByName(snapQueueName)
					if liveQ == nil {
						lq.Done()
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
					// Cross-queue bead dedup guard (hk-a11re): under the write lock
					// check every OTHER active queue for an in-flight item carrying the
					// same bead_id. If found, the bead is already being executed from
					// another queue — fail this item immediately to prevent two concurrent
					// implementers. The check must happen while the lock is held so that
					// the "dispatched" stamp in the winning queue is visible here; no race
					// is possible between the two queues' Phase 3 blocks because LockForMutation
					// serializes them.
					{
						var crossQueueConflict string
						for _, otherName := range lq.LockedAllQueueNames() {
							if otherName == snapQueueName {
								continue
							}
							otherQ := lq.LockedQueueByName(otherName)
							if otherQ == nil || otherQ.Status != queue.QueueStatusActive {
								continue
							}
							for _, g := range otherQ.Groups {
								for _, item := range g.Items {
									if item.BeadID == snapItemBeadID &&
										(item.Status == queue.ItemStatusDispatched || item.Status == queue.ItemStatusCompleted) {
										crossQueueConflict = otherName
										break
									}
								}
								if crossQueueConflict != "" {
									break
								}
							}
							if crossQueueConflict != "" {
								break
							}
						}
						if crossQueueConflict != "" {
							// Fail the duplicate item so the group can advance rather than stall.
							for gi := range liveQ.Groups {
								if liveQ.Groups[gi].Status != queue.GroupStatusActive {
									continue
								}
								if liveQ.Groups[gi].GroupIndex != snapGroupIndex {
									continue
								}
								if snapItemIdx < len(liveQ.Groups[gi].Items) &&
									liveQ.Groups[gi].Items[snapItemIdx].BeadID == snapItemBeadID {
									liveQ.Groups[gi].Items[snapItemIdx].Status = queue.ItemStatusFailed
									liveQ.Groups[gi].Items[snapItemIdx].LastFailureReason = "cross_queue_duplicate"
								}
							}
							lq.LockedSetQueueByName(snapQueueName, liveQ)
							if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
								fmt.Fprintf(os.Stderr, "daemon: workloop: Persist cross-queue-duplicate queueID=%s: %v\n",
									liveQ.QueueID, persistErr)
							}
							lq.Done()
							fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s already dispatched/completed from queue %q — failing cross-queue duplicate item (hk-a11re, hk-dorz9)\n",
								snapItemBeadID, crossQueueConflict)
							evaluateGroupAdvanceWithOutcome(ctx, deps, snapQueueName, snapQueueID, snapGroupIndex, snapItemIdx, false)
							continue
						}
					}

					// Locate the same group and item in the live snapshot.
					foundItem := false
					maxAttemptsHit := false
					for gi := range liveQ.Groups {
						if liveQ.Groups[gi].Status != queue.GroupStatusActive {
							continue
						}
						if liveQ.Groups[gi].GroupIndex != snapGroupIndex {
							continue
						}
						if snapItemIdx < len(liveQ.Groups[gi].Items) &&
							liveQ.Groups[gi].Items[snapItemIdx].BeadID == snapItemBeadID &&
							liveQ.Groups[gi].Items[snapItemIdx].Status == queue.ItemStatusPending {
							// hk-6pspu: increment Attempts and enforce maxItemAttempts.
							liveQ.Groups[gi].Items[snapItemIdx].Attempts++
							if liveQ.Groups[gi].Items[snapItemIdx].Attempts >= maxItemAttempts {
								// Set the terminal status so the item leaves Pending —
								// otherwise the next select re-picks it, re-increments
								// Attempts, and live-locks dispatch (mirrors the
								// cross-queue-duplicate sibling above). hk-6pspu.
								liveQ.Groups[gi].Items[snapItemIdx].Status = queue.ItemStatusFailed
								liveQ.Groups[gi].Items[snapItemIdx].LastFailureReason = "max_attempts_exceeded"
								fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s exceeded maxItemAttempts=%d — failing queue item (hk-6pspu)\n",
									snapItemBeadID, maxItemAttempts)
								maxAttemptsHit = true
								break
							}
							runUUIDStr := "" // filled after uuid generation below
							liveQ.Groups[gi].Items[snapItemIdx].Status = queue.ItemStatusDispatched
							liveQ.Groups[gi].Items[snapItemIdx].RunID = &runUUIDStr // placeholder; updated after
							_ = runUUIDStr                                          // suppress lint
							foundItem = true
						}
					}
					if maxAttemptsHit {
						lq.LockedSetQueueByName(snapQueueName, liveQ)
						if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
							fmt.Fprintf(os.Stderr, "daemon: workloop: Persist max-attempts queueID=%s: %v\n",
								liveQ.QueueID, persistErr)
						}
						lq.Done()
						evaluateGroupAdvanceWithOutcome(ctx, deps, snapQueueName, snapQueueID, snapGroupIndex, snapItemIdx, false)
						continue
					}
					if !foundItem {
						lq.Done()
						// Already dispatched by a concurrent path — retry.
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
					lq.LockedSetQueueByName(snapQueueName, liveQ)
					// Persist the dispatched-stamp so queue.json reflects the
					// in-memory state (hk-xsutm). Non-fatal: RunID placeholder
					// will be patched shortly; the important invariant is that
					// the item is marked dispatched before any other path reads it.
					if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
						fmt.Fprintf(os.Stderr, "daemon: workloop: Persist dispatch-stamp queueID=%s: %v\n",
							liveQ.QueueID, persistErr)
					}
					lq.Done()
				}

				// hk-lr5t: initialize beadRecord with the pre-claim ShowBead result so
				// labels (harness:<agent-type>, workflow:<mode>, model:<alias>, etc.)
				// are available to resolveHarness and resolveWorkflowMode even when
				// the post-claim ShowBead below fails. The post-claim ShowBead at
				// ~line 1954 refreshes these fields after claim and remains the
				// authoritative source; this initialization closes the label-load gap
				// where a post-claim ShowBead failure left Labels=nil, causing
				// resolveHarness to fall through to the claude-code built-in fallback
				// despite a harness:codex label on the bead (root cause of hk-lr5t).
				beadRecord = core.BeadRecord{
					BeadID:      snapItemBeadID,
					Labels:      preClaimRecord.Labels,
					Title:       preClaimRecord.Title,
					Description: preClaimRecord.Description,
				}
				queueItemIndex = snapItemIdx
				capturedQueueName = snapQueueName // NQ-B1: tag the run with its queue
				qID := snapQueueID
				gIdx := snapGroupIndex
				queueIDField = &qID
				queueGroupIdxFd = &gIdx
				capturedExtraContext = snapItemContext              // hk-boiwe
				capturedItemWFMode = snapItemWFMode                 // hk-hiqrl
				capturedItemWFRef = snapItemWFRef                   // hk-qo9pq
				capturedItemTemplateParams = snapItemTemplateParams // hk-55zv2 / WG-045
			}
		}

		var beadID core.BeadID
		if queueItemIndex < 0 {
			// No-auto-pull gate (hk-exd7m): when --no-auto-pull is set, suppress the
			// br-ready fallback path entirely so the daemon only dispatches work that
			// arrives via the queue surface (harmonik queue submit / append).  This is
			// the queue-only mode required by the flywheel topology (CL-013/070/071)
			// where a Pi cognition loop curates dispatch timing.
			if deps.noAutoPull {
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			// Operator-pause gate for the br-ready path (hk-ry8q1): when the daemon
			// is operator-paused, hold dispatch without claiming any bead. The queue
			// path is already gated via QueueStatusPausedByDrain.
			//
			// Spec ref: specs/operator-nfr.md §4.3 ON-007–ON-010.
			if deps.operatorPauseCtrl != nil && deps.operatorPauseCtrl.IsPaused() {
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			// No queue active — fall back to br-ready poll.
			readyRecords, err := deps.brAdapter.Ready(ctx)
			if err != nil {
				// Treat poll errors as transient: log and backoff.
				if dispatchCtx.Err() != nil {
					return exitClean()
				}
				// Non-fatal: surface to stderr so operators can diagnose CWD/PATH
				// misconfiguration (hk-c1ln2: silent-failure fix).
				fmt.Fprintf(os.Stderr, "daemon: workloop: Ready poll error (will retry): %v\n", err)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			if len(readyRecords) == 0 {
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			// Pick first ready bead; labels carry workflow:<mode> for mode resolution.
			beadRecord = readyRecords[0]

			// hk-6pspu: br-ready path attempt bound. Skip beads that have
			// exceeded maxItemAttempts on this path. The counter is incremented
			// on ShowBead/claim failures (not on every poll). Resets on daemon
			// restart (acceptable for the backward-compat fallback path).
			if readyPathAttempts[beadRecord.BeadID] >= maxItemAttempts {
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s exceeded maxItemAttempts=%d on br-ready path — skipping (hk-6pspu)\n",
					beadRecord.BeadID, maxItemAttempts)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			// Handler-pause gate for the br-ready path (hk-kac8g): mirror the same
			// check applied in the queue path above.  The bead remains in the br
			// ready queue (not claimed) while the handler is paused.
			//
			// Bead ref: hk-kac8g.
			if deps.handlerPauseController != nil {
				epoch, isPaused := deps.handlerPauseController.PausedEpochFor(core.AgentTypeClaudeCode)
				lastSeenPauseEpoch = pruneHeldDedupOnEpochChange(&deps, epoch, lastSeenPauseEpoch)
				if isPaused {
					emitHeldEvent(ctx, deps, beadRecord.BeadID, core.AgentTypeClaudeCode, epoch)
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}
			}

			// Decision-required dispatch-blocking gate (EV-043, br-ready path):
			// mirror the check applied in the queue path above.
			//
			// Sentinel queue-level gate (FW3 hk-4toh): also hold when the sentinel
			// governor trip is pending — all beads are blocked until real movement.
			// Asked THROUGH the maintenance handle that owns the governor, so the
			// gate disappears with the subsystem.
			//
			// Spec ref: specs/event-model.md §4.12 EV-043, EV-043a.
			// Bead ref: hk-pbmsq (bead gate), hk-4toh (sentinel queue gate).
			if deps.decisionBlocker != nil && deps.decisionBlocker.IsBeadBlocked(beadRecord.BeadID) {
				fmt.Fprintf(os.Stderr,
					"daemon: workloop: bead %s blocked by unacknowledged decision_required (EV-043, br-ready path) — holding\n",
					beadRecord.BeadID)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}
			if maint.sentinelBlocksDispatch(deps) {
				fmt.Fprintf(os.Stderr,
					"daemon: workloop: bead %s blocked by sentinel governor trip (EV-043, FW3, br-ready path) — holding until real movement\n",
					beadRecord.BeadID)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}
		}
		beadID = beadRecord.BeadID

		runUUID, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			// UUID generation failure is fatal — system entropy problem.
			wg.Wait()
			return fmt.Errorf("daemon: workloop: generate RunID: %w", uuidErr)
		}
		runID := core.RunID(runUUID)

		// Patch the placeholder RunID string in the queue item now that we have it.
		// NQ-B1: target the selected queue by name (capturedQueueName), not "main".
		if queueItemIndex >= 0 && deps.queueStore != nil {
			lq := deps.queueStore.LockForMutation()
			liveQ := lq.LockedQueueByName(capturedQueueName)
			if liveQ != nil {
				for gi := range liveQ.Groups {
					if liveQ.Groups[gi].Status != queue.GroupStatusActive {
						continue
					}
					if queueGroupIdxFd != nil && liveQ.Groups[gi].GroupIndex != *queueGroupIdxFd {
						continue
					}
					if queueItemIndex < len(liveQ.Groups[gi].Items) &&
						liveQ.Groups[gi].Items[queueItemIndex].Status == queue.ItemStatusDispatched {
						runIDStr := runID.String()
						liveQ.Groups[gi].Items[queueItemIndex].RunID = &runIDStr
					}
				}
				lq.LockedSetQueueByName(capturedQueueName, liveQ)
				// Persist the RunID patch (hk-xsutm).
				if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: Persist RunID-patch queueID=%s: %v\n",
						liveQ.QueueID, persistErr)
				}
			}
			lq.Done()
		}

		claimTID, tidErr := deps.tidGen.Next()
		if tidErr != nil {
			wg.Wait()
			return fmt.Errorf("daemon: workloop: generate claim TransitionID: %w", tidErr)
		}

		if queueItemIndex < 0 {
			// br-ready path: pre-claim status guard (hk-p4xbw) + label hydration
			// (hk-a0htu).
			//
			// ShowBead serves two purposes here:
			//   1. Guard: confirm the bead is still open before claiming (TOCTOU
			//      window is acceptable per the claim-semaphore note above).
			//   2. Label hydration: `br ready --format json` (br v0.1.45) does not
			//      include the `labels` field, so BeadRecord.Labels from Ready() is
			//      always nil.  ShowBead returns the full record including labels;
			//      we overwrite beadRecord.Labels so resolveWorkflowMode (tier-1)
			//      and ResolveModelPreference can read per-bead overrides correctly.
			//
			// Queue-path items are already exclusively owned by this loop (set to
			// dispatched under write lock), so the guard is skipped there; their
			// label hydration is handled below after the claim write.
			showRecord, showErr := deps.brAdapter.ShowBead(ctx, beadID)
			if showErr != nil {
				if dispatchCtx.Err() != nil {
					return exitClean()
				}
				readyPathAttempts[beadID]++
				if readyPathAttempts[beadID] >= maxItemAttempts {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead pre-claim check %s failed %d times, skipping bead (hk-kupeo): %v\n",
						beadID, readyPathAttempts[beadID], showErr)
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}
				fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead pre-claim check %s error (attempt %d/%d, will retry): %v\n",
					beadID, readyPathAttempts[beadID], maxItemAttempts, showErr)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}
			if showRecord.Status != core.CoarseStatusOpen {
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead_claim_skipped %s status=%s (competing claim won)\n", beadID, showRecord.Status)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}
			// Hydrate from the full ShowBead record (hk-a0htu).
			beadRecord.Labels = showRecord.Labels
			beadRecord.Title = showRecord.Title
			beadRecord.Description = showRecord.Description
		}

		// hk-l5saf: the secondary local-cap guard that previously sat here (post-stamp)
		// was hoisted to before the Phase-3 dispatch stamp (see the guard tagged
		// hk-l5saf above). At this point the item has already been stamped Dispatched
		// and persisted, so deferring here would strand it; the hoisted guard defers
		// pre-stamp instead, and localInFlight cannot have risen since (single dispatch
		// goroutine, increment at ~line 3072), so no cap re-check is needed here.

		// Acquire the claim semaphore before the SQLite write (hk-e61c3.3).
		// The select allows dispatch-halt to abort the acquire so the loop
		// does not block indefinitely on shutdown (hk-2o2i9: use dispatchCtx).
		// Spec ref: specs/execution-model.md §4.11 EM-050 (acquire token before ClaimBead, release after).
		select {
		case claimSem <- struct{}{}:
		case <-dispatchCtx.Done():
			return exitClean()
		}
		claimErr := deps.brAdapter.ClaimBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, claimTID, beadID)
		// Release the semaphore immediately after the write completes.
		<-claimSem
		if claimErr != nil {
			if dispatchCtx.Err() != nil {
				return exitClean()
			}

			// Queue-path: detect bead-level blocking (hk-n91y0).
			//
			// When ClaimBead fails because the bead has open dependencies, reverting
			// to pending and retrying creates a live-lock that starves the remaining
			// pending items in the wave group.
			//
			// Detection: check both the error message from br claim (which includes
			// "cannot claim blocked issue" when deps are open) AND the ShowBead
			// status. A bead can be status=open but still unclaimable due to deps.
			if queueItemIndex >= 0 && deps.queueStore != nil && queueIDField != nil && queueGroupIdxFd != nil {
				claimErrStr := claimErr.Error()
				isBlocked := strings.Contains(claimErrStr, "cannot claim blocked issue") ||
					strings.Contains(claimErrStr, "blocked")
				if !isBlocked {
					if showRecord, showErr := deps.brAdapter.ShowBead(ctx, beadID); showErr == nil &&
						showRecord.Status == core.CoarseStatusBlocked {
						isBlocked = true
					}
				}
				if isBlocked {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ClaimBead %s bead is blocked (deps or status) — failing queue item (hk-n91y0)\n", beadID)
					evaluateGroupAdvanceWithOutcome(ctx, deps, capturedQueueName, *queueIDField, *queueGroupIdxFd, queueItemIndex, false)
					continue
				}
			}

			// Claim conflict or transient error — surface to stderr and retry.
			fmt.Fprintf(os.Stderr, "daemon: workloop: ClaimBead %s error (will retry): %v\n", beadID, claimErr)
			// hk-6pspu: increment br-ready path attempt counter on claim failure.
			if queueItemIndex < 0 {
				readyPathAttempts[beadID]++
			}
			// hk-rnsjs: if the bead is blocked by stale dependencies already in
			// main, auto-close them so the next workloop retry can claim the bead.
			autoCloseStaleBlockersOnClaimFailure(ctx, deps, beadID)
			// On queue-path: revert the item back to pending so the loop can retry.
			// NQ-B1: target the selected queue by name (capturedQueueName).
			if queueItemIndex >= 0 && deps.queueStore != nil {
				lq := deps.queueStore.LockForMutation()
				liveQ := lq.LockedQueueByName(capturedQueueName)
				if liveQ != nil {
					for gi := range liveQ.Groups {
						if queueGroupIdxFd != nil && liveQ.Groups[gi].GroupIndex != *queueGroupIdxFd {
							continue
						}
						if queueItemIndex < len(liveQ.Groups[gi].Items) {
							liveQ.Groups[gi].Items[queueItemIndex].Status = queue.ItemStatusPending
							liveQ.Groups[gi].Items[queueItemIndex].RunID = nil
							// hk-6pspu: record claim failure reason; do NOT reset Attempts (monotonic).
							liveQ.Groups[gi].Items[queueItemIndex].LastFailureReason = claimErr.Error()
						}
					}
					lq.LockedSetQueueByName(capturedQueueName, liveQ)
					// Persist the claim-failure revert (hk-xsutm).
					if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
						fmt.Fprintf(os.Stderr, "daemon: workloop: Persist claim-revert queueID=%s: %v\n",
							liveQ.QueueID, persistErr)
					}
				}
				lq.Done()
			}
			if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
				return exitClean()
			}
			continue
		}

		// Queue-path label hydration (hk-a0htu): queue Item carries only BeadID;
		// call ShowBead now (after claim) to populate Labels for resolveWorkflowMode
		// and ResolveModelPreference.  The br-ready path hydrated labels earlier
		// from its pre-claim ShowBead response; this block handles the queue path.
		// Hydration failure is non-fatal: log to stderr and proceed with nil labels
		// (resolveWorkflowMode falls through to tier-3/4 as before the fix).
		if queueItemIndex >= 0 {
			showRecord, showErr := deps.brAdapter.ShowBead(ctx, beadID)
			if showErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead label-hydrate %s error (labels nil, falling through): %v\n", beadID, showErr)
			} else {
				beadRecord.Labels = showRecord.Labels
				beadRecord.Title = showRecord.Title
				beadRecord.Description = showRecord.Description
			}
		}

		// Capture queue context for the goroutine (may be nil for non-queue dispatch).
		capturedQueueID := queueIDField
		capturedQueueGroupIdx := queueGroupIdxFd
		capturedItemIndex := queueItemIndex
		// Per-item overrides captured here; empty for br-ready path.
		capturedCtx := capturedExtraContext              // hk-boiwe
		capturedWFMode := capturedItemWFMode             // hk-hiqrl
		capturedWFRef := capturedItemWFRef               // hk-qo9pq
		capturedTmplParams := capturedItemTemplateParams // hk-55zv2 / WG-045
		// hk-f10xl [L5 Move 2]: per-queue routing gate captured for the goroutine.
		capturedLocalOnly := capturedQueueLocalOnly
		capturedWorkerTarget := capturedQueueWorkerTarget
		capturedDefaultHarness := capturedQueueDefaultHarness

		// Register the run and spawn a goroutine to handle it end-to-end.
		// The goroutine owns Unregister on exit; the outer loop may proceed to
		// claim the next bead immediately (up to effectiveMax).
		//
		// hk-0z5x: create a per-run derived context with its own cancel function
		// so the never-spawned reaper (StaleWatcher.fireNeverSpawnedReaper) can
		// abort THIS run without affecting the daemon or other concurrent runs.
		// The cancel is stored in RunHandle.Cancel for the stale watcher to call.
		runCtx, runCancel := context.WithCancel(ctx)
		// hk-y3frr: hold cacheReapMu.RLock for the duration of Register so the
		// reaper's WLock cannot be acquired while a new run is being inserted into
		// the registry — and so Register blocks while a reap holds the WLock.
		if deps.cacheReapMu != nil {
			deps.cacheReapMu.RLock()
		}
		dispatchedHandle := &RunHandle{
			BeadID: beadID,
			// QueueName tags the run with its dispatching queue so the per-queue
			// capacity tally (LenForQueue/LenForQueueLocal) bounds this queue
			// independently of the global ceiling (NQ-B1). Empty for
			// br-ready-fallback runs.
			QueueName: capturedQueueName,
			// hk-mdus1: denormalize the durable queue coordinates so the
			// force-reap watchdog can advance the owning queue item terminal
			// when this run's goroutine wedges and never runs the completion
			// path itself.
			QueueID:         capturedQueueID,
			QueueGroupIndex: capturedQueueGroupIdx,
			QueueItemIndex:  capturedItemIndex,
			Labels:          beadRecord.Labels,
			StartedAt:       time.Now(),
			Cancel:          runCancel,
		}
		deps.runRegistry.Register(runID, dispatchedHandle)
		if deps.cacheReapMu != nil {
			deps.cacheReapMu.RUnlock()
		}

		// hk-hs7ex: hoist SelectWorker to dispatch time (before goroutine start) so
		// the split gate at Step 2 sees the correct local-vs-remote count on the next
		// tick. Pre-select a worker based on per-item routing flags. If non-nil, this
		// run executes remotely and does NOT increment localInFlight.
		var preSelectedWorker *workers.Worker
		if !capturedLocalOnly && deps.workerRegistry != nil {
			if capturedWorkerTarget != "" {
				preSelectedWorker = deps.workerRegistry.SelectWorkerByName(capturedWorkerTarget)
			} else {
				preSelectedWorker = deps.workerRegistry.SelectWorker()
			}
		}
		isLocalDispatch := preSelectedWorker == nil
		if isLocalDispatch && deps.localInFlight != nil {
			deps.localInFlight.Add(1)
		} else if !isLocalDispatch {
			// hk-4tjt6: tag as remote so LenForQueueLocal excludes it from the
			// per-queue Workers ceiling gate in selectNextQueue.
			dispatchedHandle.Remote.Store(true)
		}

		wg.Add(1)
		// NQ-B1: capture the dispatching queue's name so the completion path can
		// resolve the right queue by name (evaluateGroupAdvanceWithOutcome) and
		// the review-loop-failure budget (beadRunOne) updates the right queue.
		// Without this both default to the main-only shim and a non-"main" queue
		// never marks its item terminal → the group stalls forever (hk-tigaf.4).
		go func(runID core.RunID, beadRecord core.BeadRecord, qname string, qid *string, qgidx *int, itemIdx int, extraCtx, itemWFMode, itemWFRef string, tmplParams map[string]string, localOnly bool, workerTarget string, queueDefaultHarness core.AgentType, preSelected *workers.Worker, localSlotHeld bool) {
			defer wg.Done()
			defer runCancel() // always release the per-run context, even on panic
			defer deps.runRegistry.Unregister(runID)
			// The run outcome is the Run machine's terminal state, returned by
			// beadRunOne (RSM-022) and read here for EM-015f group-advance.
			// RSM-010: build the per-run value bundle from THIS goroutine's
			// explicitly-captured parameters, not the loop variables, so the
			// capture guard the parameter list exists for still holds.
			env := deps.runEnv(runID, beadRecord, qname, qid, qgidx, itemIdx,
				itemWFMode, itemWFRef, tmplParams, localOnly, workerTarget, queueDefaultHarness)
			rp, handles := deps.buildRunBundles(env)
			runOK := beadRunOne(runCtx, env, rp, handles, extraCtx, preSelected, localSlotHeld)
			// EM-015f: after run terminal, evaluate queue group advance.
			if itemIdx >= 0 && deps.queueStore != nil && qid != nil && qgidx != nil {
				// hk-ly0hg Fix-1: if the daemon context was cancelled (shutdown),
				// beadRunOne reopened the bead and returned without emitting
				// run_failed. Leave the queue item as 'dispatched' so QM-002a at
				// next startup reverts it to pending (bead is open) rather than
				// permanently recording a false fail.
				if ctx.Err() != nil {
					// Item stays 'dispatched'; QM-002a handles recovery on restart.
				} else {
					evaluateGroupAdvanceWithOutcome(ctx, deps, qname, *qid, *qgidx, itemIdx, runOK)
				}
			}
			// hk-f722 flywheel V9 §5.4 B: on Phase-1 success, emit a staged
			// deploy+verify bead for any Phase-2 class the completed bead belongs to.
			if runOK && ctx.Err() == nil {
				stagedBeadGeneratorEval(ctx, deps, beadRecord.BeadID, beadRecord.Labels)
			}
		}(runID, beadRecord, capturedQueueName, capturedQueueID, capturedQueueGroupIdx, capturedItemIndex, capturedCtx, capturedWFMode, capturedWFRef, capturedTmplParams, capturedLocalOnly, capturedWorkerTarget, capturedDefaultHarness, preSelectedWorker, isLocalDispatch)
	}
}

// autoCloseStaleBlockersOnClaimFailure is called after a ClaimBead failure to
// detect and auto-close stale blocker beads whose implementations have already
// landed on main. When br rejects a claim because the target bead is "blocked"
// (has open dependencies not yet closed in Beads), but those dependencies were
// already merged to main, the bead cannot be claimed until the stale blocker
// records are closed.
//
// The function:
//  1. Calls ShowBead to confirm the bead's current status is CoarseStatusBlocked.
//  2. Collects all bead IDs referenced in the bead's edge list (both directions).
//  3. For each candidate blocker, calls shared.MainHistoryHasRefsTrailer.
//  4. If subsumed, calls SweepCloseBead to close the stale record.
//
// On the next workloop retry the bead should no longer be blocked and
// ClaimBead will succeed.
//
// No-op when deps.staleBlockerCloser is nil (backward-compat for test stubs
// that do not set this field).
//
// Bead ref: hk-rnsjs.
func autoCloseStaleBlockersOnClaimFailure(ctx context.Context, deps workLoopDeps, beadID core.BeadID) {
	if deps.staleBlockerCloser == nil {
		return
	}
	record, err := deps.brAdapter.ShowBead(ctx, beadID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: autoCloseStaleBlockers ShowBead %s: %v\n", beadID, err)
		return
	}
	if record.Status != core.CoarseStatusBlocked {
		return
	}
	// Collect unique bead IDs referenced in edges that are not beadID itself.
	// Both directions are scanned: ShowBead encodes Dependencies (beads this
	// bead blocks; edge From=beadID) and Dependents (beads that block this
	// bead; edge To=beadID). Any bead appearing in either direction is a
	// candidate stale blocker.
	seen := make(map[core.BeadID]struct{})
	for _, edge := range record.Edges {
		if edge.FromBeadID != beadID {
			seen[edge.FromBeadID] = struct{}{}
		}
		if edge.ToBeadID != beadID {
			seen[edge.ToBeadID] = struct{}{}
		}
	}
	for blockerID := range seen {
		if !shared.MainHistoryHasRefsTrailer(ctx, deps.projectDir, blockerID) {
			continue
		}
		fmt.Fprintf(os.Stderr, "daemon: workloop: claim-failure auto-close stale blocker %s (subsumed in main, unblocks %s)\n", blockerID, beadID)
		if closeErr := deps.staleBlockerCloser.SweepCloseBead(ctx, deps.brTimeoutCfg, blockerID); closeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: SweepCloseBead stale blocker %s: %v\n", blockerID, closeErr)
		}
	}
}

// drainCancelledQueue transitions all active queues (if any) to
// QueueStatusCancelled and archives their files so that the next harmonik run
// invocation can proceed without the QM-027 "already active" guard blocking it.
//
// Prior to hk-u6m4l this function only drained the "main" queue via the
// backward-compat lq.Queue() shim, leaving named queues (e.g. "cp") on disk
// with status=active after shutdown. The fix iterates all queues in the store
// via AllQueues() so every active named queue is archived on exit.
//
// This is called on every clean exit of runWorkLoop — when ctx is cancelled due
// to SIGINT, SIGTERM, or a timeout — after wg.Wait() ensures all in-flight
// goroutines have completed. The function is a no-op when:
//   - deps.queueStore is nil (no queue surface in use).
//   - All in-memory queues are nil or already in a terminal state
//     (paused-by-failure, completed, cancelled) — evaluateGroupAdvanceWithOutcome
//     already transitioned them.
//
// Uses context.Background() because ctx is always cancelled by the time this
// runs; queue.CancelQueueOnShutdown needs a non-cancelled context for Persist.
//
// Errors are logged to stderr but do not block shutdown; other queues are still
// drained even if one fails.
//
// Spec ref: specs/queue-model.md §8 (shutdown drain).
// Bead ref: hk-ppt32, hk-u6m4l.
func drainCancelledQueue(ctx context.Context, deps workLoopDeps) {
	if deps.queueStore == nil {
		return
	}
	// Snapshot all queues under the read lock. drainCancelledQueue is called
	// after wg.Wait() so there are no concurrent mutations; AllQueues is safe
	// here and avoids holding the write lock across I/O.
	snapshot := deps.queueStore.AllQueues()
	for name, q := range snapshot {
		if q == nil || q.Status != queue.QueueStatusActive {
			continue
		}
		// Queue is still active: transition to cancelled and archive.
		if err := queue.CancelQueueOnShutdown(ctx, deps.projectDir, q); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: drainCancelledQueue queueID=%s name=%q: %v\n",
				q.QueueID, name, err)
			// Continue draining other queues even if one fails.
		}
		// Clear in-memory state for this queue.
		deps.queueStore.ClearQueueByName(name)
	}
}

// workloopSleep sleeps for d or until ctx is cancelled. Returns a non-nil
// error only when ctx is cancelled. wakeC may be nil: receive from a nil
// channel blocks forever, so the nil case is never selected and the function
// degrades to a plain timer sleep. Bead ref: hk-24xn1 (wakeC parameter).
//
//nolint:unparam // pre-existing: Seam A moved this code out of workloop.go unchanged
func workloopSleep(ctx context.Context, d time.Duration, wakeC <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	case <-wakeC:
		return nil
	}
}

// workloopIdleWait blocks indefinitely until a queue-submit wake signal
// arrives on wakeC or ctx is cancelled. Unlike workloopSleep there is no
// periodic re-query timer — the daemon waits without spinning until the
// socket layer delivers a signal. Used for queue-loaded idle states per
// PL-013 (retired-with-stub): idle (queue absent/completed/paused or no
// eligible items) MUST NOT busy-poll; it MUST block until queue-submit or
// shutdown.
//
// wakeC may be nil (e.g. in tests that do not wire QueueStore.WakeCh):
// receive from a nil channel blocks forever, so the function degrades to
// waiting only for ctx cancellation.
//
// Bead ref: hk-dji5z (T71).
func workloopIdleWait(ctx context.Context, wakeC <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wakeC:
		return nil
	}
}

// scheduleAwareIdleWait is the schedule-aware variant of workloopIdleWait used
// at the queues-loaded-but-idle dispatch point. When a schedule with at least
// one enabled job is loaded the daemon MUST re-tick periodically to fire due
// jobs, so this bounds the wait by workloopPollInterval (and selects on the
// schedule wake channel so a CLI mutation wakes the loop immediately). When no
// schedule is armed it degrades to the indefinite block of workloopIdleWait,
// preserving the no-busy-poll idle contract (PL-013).
//
// Bead ref: hk-0es.
func scheduleAwareIdleWait(ctx context.Context, deps workLoopDeps) error {
	if deps.scheduleStore == nil || !hasEnabledScheduledJob(deps.scheduleStore) {
		return workloopIdleWait(ctx, deps.submitWakeC)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(workloopPollInterval):
		return nil
	case <-deps.submitWakeC:
		return nil
	case <-deps.scheduleWakeC:
		return nil
	}
}

// hasEnabledScheduledJob reports whether the store holds at least one enabled
// job. Used to decide whether the idle wait must be bounded.
func hasEnabledScheduledJob(s *schedule.Store) bool {
	for _, j := range s.List() {
		if j.Enabled {
			return true
		}
	}
	return false
}

// activateFirstPendingGroupLocked bootstraps the first group of a
// freshly-submitted or freshly-loaded queue on the multi-queue dispatch path
// (NQ-B1). The caller already holds the QueueStore write lock (lq) and supplies
// the specific queue q to bootstrap.
//
// Without it, group 0 never transitions pending → active: the only other caller
// of AdvanceGroup is evaluateGroupAdvanceWithOutcome, which fires on a PRIOR
// run's completion, so absent any prior run the work loop idle-waits forever
// (hk-veoht). It advances q's lowest-index pending group pending →
// active, persists (QM-063), writes the mutated queue back via
// LockedSetQueueByName, and returns true plus the resulting queue_group_started
// events for the CALLER to emit AFTER releasing the lock (preserving the
// EV-002a emit-after-persist-and-unlock idiom).
//
// Returns (false, nil) when q has an active group already, no pending group, or
// AdvanceGroup is a no-op (QM-031 guard).
//
// Spec ref: specs/queue-model.md §5 QM-031; §8 QM-063.
// Bead ref: hk-tigaf.4 (NQ-B1).
func activateFirstPendingGroupLocked(ctx context.Context, deps workLoopDeps, lq *queuewiring.LockedQueueStore, q *queue.Queue) (bool, []core.Event) {
	if q == nil {
		return false, nil
	}
	for i := range q.Groups {
		if q.Groups[i].Status == queue.GroupStatusActive {
			return false, nil
		}
	}
	groupPos := -1
	for i := range q.Groups {
		if q.Groups[i].Status == queue.GroupStatusPending {
			groupPos = i
			break
		}
	}
	if groupPos < 0 {
		return false, nil
	}

	newStatus, events, advErr := queue.AdvanceGroup(ctx, &q.Groups[groupPos], q.Status, q.QueueID, time.Now())
	if advErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: activateFirstPendingGroupLocked AdvanceGroup queueID=%s groupIndex=%d: %v\n",
			q.QueueID, q.Groups[groupPos].GroupIndex, advErr)
		return false, nil
	}
	if newStatus != queue.GroupStatusActive {
		return false, nil
	}

	q.Groups[groupPos].Status = newStatus
	if err := queue.Persist(ctx, deps.projectDir, q); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: activateFirstPendingGroupLocked Persist queueID=%s: %v\n",
			q.QueueID, err)
		events = nil // describe only durable state
	}
	lq.LockedSetQueueByName(queue.NormaliseQueueName(q.Name), q)

	// Events are returned for the caller to emit AFTER releasing the QueueStore
	// write lock (EV-002a emit-after-persist-and-unlock idiom, matching
	// evaluateGroupAdvanceWithOutcome).
	return true, events
}

// markQueueItemFailureReason stamps LastFailureReason on one queue item without
// touching its Status. It is called immediately BEFORE
// evaluateGroupAdvanceWithOutcome(..., false), which sets the terminal status
// and persists — so the reason lands in queue.json on the same write, giving an
// operator reading `harmonik queue status` the WHY behind a failed item.
//
// No-op when the queue, group, or item cannot be resolved: the caller's
// evaluateGroupAdvanceWithOutcome applies the same guards and is the load-
// bearing half of the pair.
//
// Bead ref: hk-pina9.
func markQueueItemFailureReason(_ context.Context, deps workLoopDeps, queueName string, groupIndex, itemIdx int, beadID core.BeadID, reason string) {
	if deps.queueStore == nil {
		return
	}
	lq := deps.queueStore.LockForMutation()
	defer lq.Done()
	q := lq.LockedQueueByName(queue.NormaliseQueueName(queueName))
	if q == nil {
		return
	}
	for gi := range q.Groups {
		if q.Groups[gi].GroupIndex != groupIndex {
			continue
		}
		if itemIdx < len(q.Groups[gi].Items) && q.Groups[gi].Items[itemIdx].BeadID == beadID {
			q.Groups[gi].Items[itemIdx].LastFailureReason = reason
		}
	}
	lq.LockedSetQueueByName(queue.NormaliseQueueName(queueName), q)
}

// evaluateGroupAdvance — EM-015f group-advance gate (hk-45ude)
// ─────────────────────────────────────────────────────────────────────────────

// evaluateGroupAdvanceWithOutcome is called from the per-run goroutine after a run that
// the run's success outcome from the goroutine wrapper in runWorkLoop.
//
// It marks the queue item terminal (completed/failed), calls AdvanceGroup, and
// emits the resulting group events. If the group transitions to complete-success,
// it also activates the next group (pending → active). If complete-with-failures,
// it marks the queue status as paused-by-failure.
//
// queueName identifies the queue the run was dispatched from (NQ-B1). It is
// the normalised name captured at dispatch time (capturedQueueName). The
// completion path MUST resolve the queue by name — using the main-only
// lq.Queue() shim instead would, for a non-"main" queue, return the wrong
// queue (or nil), trip the QueueID guard, and return early WITHOUT marking the
// item terminal, stalling that queue's group forever (hk-tigaf.4).
//
// Spec ref: specs/execution-model.md §4.3.EM-015f.
// Bead ref: hk-45ude, hk-tigaf.4.
//
//nolint:gocognit,cyclop,funlen,gocritic // pre-existing: Seam A moved this code out of workloop.go unchanged
func evaluateGroupAdvanceWithOutcome(ctx context.Context, deps workLoopDeps, queueName string, queueID string, groupIndex int, itemIdx int, success bool) {
	if deps.queueStore == nil {
		return
	}

	lq := deps.queueStore.LockForMutation()

	// NQ-B1: resolve the queue BY NAME (capturedQueueName), mirroring the
	// dispatch path's LockedQueueByName usage. queueName is already normalised
	// (the round-robin selector reads it from the QueueStore's map keys), so it
	// is passed straight through. The QueueID equality check is retained as a
	// staleness guard: it rejects a completion whose queue was cleared and a new
	// queue installed at the same name slot before this goroutine ran.
	q := lq.LockedQueueByName(queue.NormaliseQueueName(queueName))
	if q == nil || q.QueueID != queueID {
		lq.Done()
		return
	}

	// Locate the target group.
	groupPos := -1
	for i := range q.Groups {
		if q.Groups[i].GroupIndex == groupIndex {
			groupPos = i
			break
		}
	}
	if groupPos < 0 || itemIdx >= len(q.Groups[groupPos].Items) {
		lq.Done()
		return
	}

	// Mark the item terminal.
	if success {
		q.Groups[groupPos].Items[itemIdx].Status = queue.ItemStatusCompleted
	} else {
		q.Groups[groupPos].Items[itemIdx].Status = queue.ItemStatusFailed
	}

	// Evaluate group-advance gate (EM-015f all-terminal rule).
	newStatus, events, advErr := queue.AdvanceGroup(ctx, &q.Groups[groupPos], q.Status, queueID, time.Now())
	if advErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: AdvanceGroup queueID=%s groupIndex=%d: %v\n",
			queueID, groupIndex, advErr)
		// NQ-B1: write back to the same name slot we resolved from.
		lq.LockedSetQueueByName(queue.NormaliseQueueName(queueName), q)
		lq.Done()
		return
	}

	// Apply new group status.
	q.Groups[groupPos].Status = newStatus

	// If group reached complete-with-failures → queue transitions to paused-by-failure.
	// (M5 slice 3C: pure classification via orchestrator; the mutation stays here.)
	if orchestrator.GroupFailurePausesQueue(string(newStatus)) {
		q.Status = queue.QueueStatusPausedByFailure
	}

	// If group reached complete-success → activate the next group. The pure
	// predicate decides WHICH group to activate (first still-pending); the
	// effectful queue.AdvanceGroup call — which mutates the group AND produces
	// order-appended events — stays daemon-side (M5 slice 3C).
	if orchestrator.GroupReachedSuccess(string(newStatus)) {
		groupStatuses := make([]string, len(q.Groups))
		for i := range q.Groups {
			groupStatuses[i] = string(q.Groups[i].Status)
		}
		if i := orchestrator.FirstPendingGroupIndex(groupStatuses); i >= 0 {
			nextStatus, nextEvents, nextErr := queue.AdvanceGroup(ctx, &q.Groups[i], q.Status, queueID, time.Now())
			if nextErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: AdvanceGroup next group queueID=%s groupIndex=%d: %v\n",
					queueID, q.Groups[i].GroupIndex, nextErr)
			} else {
				q.Groups[i].Status = nextStatus
				events = append(events, nextEvents...)
			}
		}
	}

	// Determine whether the queue has completed: all groups reached
	// complete-success (hk-xsutm). This is the sole condition that triggers
	// CompleteAndUnlink (QM-003). A paused-by-failure queue retains queue.json
	// for operator-driven resume or reset; only the happy-path full-success case
	// removes it. (M5 slice 3C: pure scan via orchestrator.AllGroupsSucceeded.)
	postStatuses := make([]string, len(q.Groups))
	for i := range q.Groups {
		postStatuses[i] = string(q.Groups[i].Status)
	}
	allSucceeded := orchestrator.AllGroupsSucceeded(postStatuses)

	if allSucceeded {
		// All groups complete-success → CompleteAndUnlink (QM-003 / QM-053).
		// This internally sets q.Status = completed and persists before
		// unlinking queue.json (hk-xsutm).
		if err := queue.CompleteAndUnlink(ctx, deps.projectDir, q); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: CompleteAndUnlink queueID=%s: %v\n",
				queueID, err)
			// Fall through: still clear in-memory state so the loop isn't stuck.
		}
		lq.Done()
		// Release the write lock before ClearQueueByName (which acquires its own
		// lock). NQ-B1: clear the slot for THIS queue's name, not the main-only
		// ClearQueue shim — otherwise a completed non-"main" queue lingers in the
		// store and the round-robin selector keeps re-scanning a drained queue.
		deps.queueStore.ClearQueueByName(queue.NormaliseQueueName(queueName))
		// hk-icecw: if a drain-cancel is registered (harmonik run path), cancel
		// the daemon context now so the work loop exits cleanly instead of
		// idle-spinning waiting for more work.
		if deps.cancelOnQueueDrain != nil {
			deps.cancelOnQueueDrain()
		}
		// hk-8jh26 Fix 1: if a queue-exit cancel is registered, fire it on the
		// success path too (covers the case where only cancelOnQueueExit is set).
		if deps.cancelOnQueueExit != nil {
			deps.cancelOnQueueExit()
		}
	} else {
		// Intermediate state or paused-by-failure: persist the updated queue.json
		// so on-disk state matches in-memory after each item completion (hk-xsutm).
		if err := queue.Persist(ctx, deps.projectDir, q); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: Persist queueID=%s after item completion: %v\n",
				queueID, err)
			// Non-fatal: in-memory state is still updated; file will resync on next persist.
			// Suppress group-advance events — they describe state not yet durable on disk.
			events = nil
		}
		pausedByFailure := q.Status == queue.QueueStatusPausedByFailure
		// NQ-B1: write back to the same name slot we resolved from.
		lq.LockedSetQueueByName(queue.NormaliseQueueName(queueName), q)
		lq.Done()
		// hk-nbjht Gap 2: wake the idle dispatch loop after every run completion.
		// lq.SetQueue (the LockedQueueStore no-wake variant) does NOT fire wakeC,
		// so without this the loop stays parked in workloopIdleWait and never runs
		// its §2.8 deferred-item re-evaluation — a chained stream queue would stall
		// permanently once its head bead completes. Wake touches only wakeC (no
		// queue mutation, no second persist), so there is no double-persist race
		// with the SetQueue above. Fired unconditionally on run_completed: the
		// woken loop re-runs EligibleItems + ReevaluateDeferred, which is cheap and
		// idempotent if no item un-defers.
		deps.queueStore.Wake()
		// hk-8jh26 Fix 1: if the queue is now paused-by-failure and an exit-cancel
		// is registered (harmonik run path), cancel the daemon context so the work
		// loop exits promptly instead of idle-spinning waiting for more work.
		// pausedByFailure is captured before lq.Done() to avoid a data race with
		// another goroutine that may call CompleteAndUnlink (which writes q.Status)
		// after acquiring the lock we just released.
		if pausedByFailure && deps.cancelOnQueueExit != nil {
			deps.cancelOnQueueExit()
		}
	}

	// Emit the queued events (after lock release above). Bus.Emit is non-blocking
	// per EV-002a so ordering relative to the lock release is acceptable.
	for _, evt := range events {
		raw, err := json.Marshal(evt.Payload)
		if err != nil {
			raw = evt.Payload
		}
		_ = deps.bus.Emit(ctx, core.EventType(evt.Type), raw) //nolint:errcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
	}

	// EM-062: eager-refill fires AFTER all terminal-event processing (merge,
	// reviewer-launch, CloseBead, group-advance evaluation) completes for this
	// run. Finishing in-flight work takes priority over pulling new work.
	//
	// Spec ref: specs/execution-model.md §4.13 EM-062.
	// Bead ref: hk-9321v.
	eagerRefillEval(ctx, deps)
}

// ── hk-o85ye: run-session adoption helpers ───────────────────────────────────

// extractTmuxAdapterFromSubstrate returns the tmux.Adapter from the substrate
// if it implements substrateWithAdapter, or nil otherwise.
func extractTmuxAdapterFromSubstrate(sub handler.Substrate) tmuxpkg.Adapter {
	if sa, ok := sub.(substrateWithAdapter); ok {
		return sa.tmuxAdapter()
	}
	return nil
}

// adoptLiveRunSession monitors a surviving run session that was alive when
// runWorkLoop started. When the session exits (Claude finishes), it resets the
// bead to open and reverts the queue item to pending so the dispatch loop can
// re-dispatch the work. This handles the "daemon SIGKILL'd while Claude ran in
// an independent session" recovery path (hk-o85ye).
//
// The goroutine exits without action when the daemon context is cancelled
// (another daemon shutdown) — the next boot's adoption pass handles it again.
//
//nolint:gocognit,cyclop // pre-existing: Seam A moved this code out of workloop.go unchanged
func adoptLiveRunSession(ctx context.Context, deps workLoopDeps, rec runpkg.Record, adapter tmuxpkg.Adapter) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return // daemon shutting down; leave for next boot's adoption pass
		case <-ticker.C:
		}
		sessions, listErr := adapter.ListSessions(ctx)
		if listErr != nil {
			continue // transient error; retry on next tick
		}
		found := false
		for _, s := range sessions {
			if s == rec.SessionName {
				found = true
				break
			}
		}
		if !found {
			break // session gone — Claude has exited
		}
	}

	if ctx.Err() != nil {
		return
	}

	// Session is dead. Reset bead to open so the dispatch loop can re-dispatch.
	bgCtx := context.Background()
	runUUID, parseErr := uuid.Parse(rec.RunID)
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: adoptLiveRunSession: parse runID %q: %v\n", rec.RunID, parseErr)
	} else {
		adoptRunID := core.RunID(runUUID)
		reopenTID, _ := deps.tidGen.Next()                                                                                                                                                     //nolint:errcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
		if reopenErr := deps.brAdapter.ReopenBead(bgCtx, deps.intentLogDir, deps.brTimeoutCfg, adoptRunID, reopenTID, core.BeadID(rec.BeadID), "run_session_adopted_dead"); reopenErr != nil { //nolint:contextcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
			fmt.Fprintf(os.Stderr, "daemon: adoptLiveRunSession: ReopenBead %s: %v\n", rec.BeadID, reopenErr)
		}
	}

	// Revert the queue item from dispatched → pending so the dispatch loop picks it up.
	if rec.QueueName != "" && rec.QueueID != "" && rec.GroupIndex >= 0 && rec.ItemIndex >= 0 && deps.queueStore != nil {
		qname := queue.NormaliseQueueName(rec.QueueName)
		lq := deps.queueStore.LockForMutation()
		q := lq.LockedQueueByName(qname)
		if q != nil && rec.GroupIndex < len(q.Groups) && rec.ItemIndex < len(q.Groups[rec.GroupIndex].Items) {
			item := &q.Groups[rec.GroupIndex].Items[rec.ItemIndex]
			if string(item.BeadID) == rec.BeadID && item.Status == queue.ItemStatusDispatched {
				item.Status = queue.ItemStatusPending
				item.RunID = nil
				if persistErr := queue.Persist(bgCtx, deps.projectDir, q); persistErr != nil { //nolint:contextcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
					fmt.Fprintf(os.Stderr, "daemon: adoptLiveRunSession: persist queue %q: %v\n", rec.QueueName, persistErr)
				} else {
					deps.queueStore.Wake()
				}
			}
		}
		lq.Done()
	}

	// Remove the registry entry now that the session is gone.
	if deps.projectDir != "" {
		_ = runpkg.Remove(deps.projectDir, rec.RunID) //nolint:errcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
	}
}

// strandedBeadHasOnDiskRun reports whether any record in .harmonik/runs/ is
// associated with beadID. An on-disk record means an adoptLiveRunSession
// goroutine is monitoring the independent tmux session; resetting the bead
// in that case would race the live session, so the stranded-bead auto-reset
// (hk-l2xd1) must skip.
//
// On a runpkg.List error the on-disk state is unknown, not empty — treat
// that as race-conservative (report true, i.e. "assume a run may exist")
// so the caller skips the reset rather than risking a race with a live
// adoptLiveRunSession goroutine it failed to see (hk-r9edj).
func strandedBeadHasOnDiskRun(projectDir string, beadID core.BeadID) bool {
	if projectDir == "" {
		return false
	}
	recs, err := runpkg.List(projectDir)
	if err != nil {
		return true
	}
	for _, r := range recs {
		if r.BeadID == string(beadID) {
			return true
		}
	}
	return false
}
