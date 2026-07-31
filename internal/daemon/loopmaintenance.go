package daemon

// loopmaintenance.go — the dispatch loop's cadenced maintenance.
//
// Step 2 of plans/2026-07-27-delete-and-rewrite/DECOMPOSITION-MAP.md §3. Seven
// periodic jobs used to sit inline in runWorkLoop: the schedule tick, the
// coordinator reap, the disk check, the dashboard forcing gate, the eager
// refill, and the sentinel governor's observe and act passes. Their bodies
// already lived in their own files (scheduletick.go, orphansweep.go,
// diskcheck_hksxlb.go, dashboardgate.go, eagerfill_em063.go,
// movementgovernor.go, internal/sentinel). What stayed in the loop was the
// cadence arithmetic, the handles, and the calls. That is what this file now
// owns.
//
// Why it is a real seam, not tidying: these jobs run on a period rather than
// per bead, a failure in one is logged and forgotten rather than reopening a
// bead, and none of them is on the dispatch critical path. The loop keeps only
// the decisions it acts on.
//
// WHAT CROSSES: maintenanceObservation — a plain value. A maintenance pass
// NEVER stops the daemon, sleeps, or dispatches. It reports, and runWorkLoop
// decides. The halt case is the reason this rule is written down: the sentinel
// governor in ACT mode can spawn an adversary crew and ask for the daemon to
// die. That is a dispatch effect raised by a maintenance pass, so the request
// travels back as maintenanceObservation.halt and runWorkLoop alone calls
// exitClean. A maintenance pass that shut the daemon down itself would move
// shutdown ordering — the in-flight drain, the tmux window sweep, the queue
// cancel-drain — out of the one function that owns it.
//
// TWO PASSES, NOT ONE, AND THE SPLIT IS LOAD-BEARING. The loop's local capacity
// gate sits BETWEEN them. When local runs are at the cap and no remote worker has
// a free slot, the loop sleeps and starts the next iteration. The dashboard gate,
// the eager refill and the governor evaluation are all SKIPPED for that tick.
// Folding both passes into one call before the capacity gate would make
// eager-refill and the governor run while the daemon is at capacity, which is a
// behavior change, not a cleanup. Keep tickBeforeDispatch before the gate and
// tickBeforeSelect after it.

import (
	"context"
	"io"
	"time"
)

// periodicCoordinatorReapInterval is the default minimum interval between
// successive periodic coordinator-session reap passes in the work loop
// (hk-t08m). 5 minutes balances prompt cleanup against excess tmux chatter.
// Tests may inject a shorter value via workLoopDeps.coordinatorReapInterval.
const periodicCoordinatorReapInterval = 5 * time.Minute

// loopMaintenanceState holds the periodic-maintenance value fields owned solely
// by the runWorkLoop goroutine (RSM-011). They were lifted off workLoopDeps
// because that bundle is copied by value into every run goroutine, where a
// mutation of a value field is a silent no-op (PF §3 hazard). They now live
// inside loopMaintenance, which runWorkLoop owns and holds by pointer.
type loopMaintenanceState struct {
	// lastCoordinatorReap records when the periodic coordinator reaper last ran.
	// Zero → the first tick always fires (hk-t08m).
	lastCoordinatorReap time.Time

	// lastDiskCheck records when the periodic disk free-space probe last ran.
	// Zero → the first tick fires after diskCheckInterval elapses (hk-sxlb).
	lastDiskCheck time.Time

	// diskLow is true when the most recent disk probe found available space below
	// diskLowWatermarkDefault (or deps.diskLowWatermark). The dispatch loop skips
	// bead claiming while this flag is set (hk-sxlb).
	diskLow bool
}

// maintenanceObservation is everything one maintenance pass reports to the
// dispatch loop. It is a value, and it is the ONLY channel from a maintenance
// pass back into a dispatch decision.
//
// Each pass fills only the fields it can answer for. Read the pass's own doc
// comment for which those are. A zero observation means "carry on".
type maintenanceObservation struct {
	// halt asks runWorkLoop to drain in-flight runs and exit cleanly. Set only
	// by tickBeforeDispatch, and only for a governor halt that was ARMED on an
	// EARLIER tick — see tickBeforeSelect for why the arming tick does not
	// report it.
	halt bool

	// diskLow is true when the most recent disk probe found free space below the
	// watermark (hk-sxlb). The loop skips bead claiming for this tick.
	diskLow bool

	// blockedQueues is the dashboard forcing-gate verdict (hk-xg6rw): queue names
	// present with value true are captain-curated queues withheld from NEW item
	// dispatch this tick. Nil disables the gate, which is also what an absent
	// subsystem returns.
	blockedQueues map[string]bool
}

// loopMaintenance owns every piece of mutable state the cadenced maintenance
// needs: the three timing/latch fields in loopMaintenanceState, plus the two
// switchable subsystems whose per-loop state lives inside them.
//
// One instance per runWorkLoop call, held by pointer, touched only from that
// goroutine. There is exactly one writer (PRINCIPLES §6) and it is the loop.
type loopMaintenance struct {
	// state is the periodic-maintenance timing and latch state (RSM-011).
	state loopMaintenanceState

	// dashGate is the dashboard staleness forcing gate, or nil when
	// `subsystems.dashboard_gate.enabled: false`. Every method tolerates nil.
	dashGate *dashboardGate

	// governor is the sentinel movement governor, or nil when
	// `subsystems.movement_governor.enabled: false` or nothing seeded its state.
	// Every method tolerates nil.
	governor *movementGovernor
}

// newLoopMaintenance builds the loop's maintenance state and constructs the two
// switchable subsystems it drives.
//
// Dashboard forcing gate (hk-xg6rw) — a SWITCHABLE subsystem. nil means
// `subsystems.dashboard_gate.enabled: false`: the gate is never constructed,
// never evaluated, and selectNextQueue is handed a nil blocked-queue set. See
// dashboardgate.go.
//
// Sentinel movement governor (FW2 hk-z1lr / FW3 hk-4toh) — a SWITCHABLE
// subsystem. nil means `subsystems.movement_governor.enabled: false` (or that
// nothing seeded a governor state): no evaluation, no trip, no halt, and no
// sentinel dispatch gate. All its per-loop state — eval cadence, pending ack
// token, halt flag — lives inside it. See movementgovernor.go.
//
// CONSTRUCTION ORDER IS OBSERVABLE: each constructor announces a partitioned
// subsystem on logW, because a silent partition is indistinguishable from a
// config that did not take effect. The gate is announced before the governor,
// which is the order runWorkLoop used when it built the two itself. What holds
// that order is the LEXICAL FIELD ORDER in the composite literal below, because
// Go evaluates the two constructor calls left to right. Reordering those two
// lines to tidy them would silently swap the two announcements.
//
// logW is passed straight through. Both sub-constructors already substitute
// os.Stderr for a nil writer, so a third copy of that guard here would be dead
// code (the reviewer's point, and it also keeps os out of this file's imports).
func newLoopMaintenance(deps workLoopDeps, logW io.Writer) *loopMaintenance {
	return &loopMaintenance{
		dashGate: newDashboardGateIfEnabled(deps.projectCfg, logW),
		governor: newMovementGovernorIfEnabled(deps, logW),
	}
}

// tickBeforeDispatch runs the maintenance that must happen before the loop
// looks at capacity, and reports halt and diskLow.
//
// Order inside the pass is the order runWorkLoop ran these inline, and it is
// load-bearing: an armed halt short-circuits, so no other job runs on the tick
// that drains and exits.
//
// It fills halt and diskLow. blockedQueues is not this pass's question.
func (m *loopMaintenance) tickBeforeDispatch(ctx context.Context, deps *workLoopDeps) maintenanceObservation {
	// G-liveness halt (FW3 hk-4toh): if the governor fired ActivationHalt in ACT
	// mode on a prior tick, the loop must drain in-flight runs and exit cleanly.
	// The liveness_halt page event was already emitted when the halt was armed.
	// This pass only reports it. Always false when the governor subsystem is
	// absent.
	if m.governor.halted() {
		return maintenanceObservation{halt: true}
	}

	// Schedule tick — fire any due recurring jobs (codename:schedule, hk-0es).
	// Runs IN-LOOP (reusing the loop's poll cadence + claim-write serialisation),
	// placed after the dispatch-halt check and before the capacity gate so a fired
	// spawn-crew/command action is independent of the bead-dispatch capacity.
	// No-op when scheduleStore is nil.
	runScheduleTick(ctx, *deps)

	// Periodic coordinator-session reap (hk-t08m).
	m.reapCoordinatorSessions(ctx, deps)

	// Periodic disk watermark check and reactive go-cache reap (hk-sxlb,
	// hk-guez). Reactive only — every diskCheckInterval (default 10 min). When
	// the probe finds available space below the watermark, m.state.diskLow is set
	// true, a disk_low event is emitted, and `go clean -cache` is run immediately
	// (reactive reap) — but ONLY when no merge-build is in flight
	// (runRegistry.Len()==0). If a merge is in flight the reap is skipped and a
	// loud warning is logged instead (hk-guez fix). The loop then skips dispatch
	// for this iteration, which is what the diskLow field below asks for.
	//
	// A second sub-step used to live here — a cadence-based reap that ran
	// `go clean -cache` even when disk was healthy. It was REMOVED and must not
	// be restored (hk-gjbpp); full rationale in the file-level comment on
	// diskcheck_hksxlb.go.
	runPeriodicDiskCheck(ctx, deps, &m.state)

	return maintenanceObservation{diskLow: m.state.diskLow}
}

// reapCoordinatorSessions runs the periodic coordinator-session reap when its
// cadence has elapsed, then stamps the clock (hk-t08m).
//
// The boot-time sweep (RunOrphanSweep) reaped dead flywheel-coordinator sessions
// once at startup, but sessions accumulated across hard supervisor crashes that
// skipped clean shutdown. Running the same predicate periodically here ensures
// leaked sessions are cleaned up without requiring a daemon restart.
//
// Rate-limited by deps.coordinatorReapInterval (default 5 min) so the tmux
// adapter is not called on every 2 s poll tick. The first tick fires immediately
// (lastCoordinatorReap is zero-valued). No-op when coordinatorReapAdapter is nil
// (no tmux substrate).
func (m *loopMaintenance) reapCoordinatorSessions(ctx context.Context, deps *workLoopDeps) {
	interval := deps.coordinatorReapInterval
	if interval <= 0 {
		interval = periodicCoordinatorReapInterval
	}
	if deps.coordinatorReapAdapter != nil && time.Since(m.state.lastCoordinatorReap) >= interval {
		runPeriodicCoordinatorReap(ctx, deps.projectDir, deps.coordinatorReapProjectHash, deps.coordinatorReapAdapter, nil)
		m.state.lastCoordinatorReap = time.Now()
	}
}

// tickBeforeSelect runs the maintenance that happens after the loop has passed
// its capacity gate and before it selects a queue, and reports the forcing-gate
// verdict.
//
// It fills blockedQueues only.
//
// halt IS DELIBERATELY LEFT UNSET HERE, and that is not an omission. The
// governor evaluation below is the pass that can ARM a halt. runWorkLoop today
// finishes the tick it is in — it dispatches once more — and enforces the halt at
// the top of the NEXT iteration, where tickBeforeDispatch reads it. Reporting the
// halt from this pass would make the daemon exit one dispatch earlier than it
// does now. If that one-tick delay is ever judged wrong, change it on purpose and
// say so. Do not acquire it by moving the field.
func (m *loopMaintenance) tickBeforeSelect(ctx context.Context, deps workLoopDeps, now time.Time) maintenanceObservation {
	// Dashboard staleness forcing gate (hk-xg6rw). The gate's own rate limit,
	// transition-edge event emission, and blocked-queue set live in dashboardGate
	// (dashboardgate.go). The verdict is returned below and consulted by
	// selectNextQueue to withhold NEW item dispatch on captain-curated queues
	// only. No-op when the subsystem is switched off.
	m.dashGate.tick(ctx, deps, now)

	// EM-062: eager-refill fires on every poll tick (as well as after every
	// run_terminal event in evaluateGroupAdvanceWithOutcome). This ensures that a
	// deficit opened by a run completion is filled promptly even when the workloop
	// was already idle between terminal events.
	//
	// Spec ref: specs/execution-model.md §4.13 EM-062.
	// Bead ref: hk-9321v.
	eagerRefillEval(ctx, deps)

	// Sentinel movement governor (FW2 hk-z1lr observe / FW3 hk-4toh act). One
	// call: the mode split, the eval cadence gate (hk-usn8o — each evaluation
	// scans events.jsonl), the trip/clear/halt handling and the adversary spawn
	// all live in movementGovernor (movementgovernor.go). No-op — and never
	// constructed — when the subsystem is switched off.
	m.governor.tick(ctx, deps)

	return maintenanceObservation{blockedQueues: m.dashGate.blockedQueueSet()}
}

// sentinelBlocksDispatch reports whether an ACT-mode governor trip is currently
// gating dispatch (EV-043 queue-level gate, subject "sentinel").
//
// This is a DISPATCH-TIME read, not a maintenance observation, which is why it
// is a separate call rather than a maintenanceObservation field. The reason is
// behavior preservation and nothing more: runWorkLoop read this live, at the
// moment it was about to claim a bead, and it still does. This step moves code,
// so it keeps the read where the loop had it.
//
// Do NOT read a race into this. There is no second concurrent writer. The
// "sentinel" queue block is written only by this subsystem's ACT mode, on the
// loop goroutine, plus a boot-time restore of that same writer's ack file
// (LoadDecisionAckState, EV-043a) — see dispatchBlocked in movementgovernor.go,
// which states the writer count and is the authority on it. A captain who
// acknowledges the decision through the CLI writes a file under
// .harmonik/decision_acks/. The governor reconciles that file on a LATER tick
// and calls Acknowledge itself.
//
// Turning this into a snapshot field is therefore arguable on its merits, not
// obviously wrong. It is still a behavior change, so argue it on its own
// evidence and land it on purpose. Do not acquire it as a side effect of moving
// code, which is all this commit does.
//
// False when the governor subsystem is absent. movementgovernor.go documents why
// keeping the read while removing the subsystem would wedge dispatch forever.
func (m *loopMaintenance) sentinelBlocksDispatch(deps workLoopDeps) bool {
	return m.governor.dispatchBlocked(deps)
}
