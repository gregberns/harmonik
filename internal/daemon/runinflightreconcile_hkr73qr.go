package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// beadStatusReader is the minimal read-only ledger slice used by the orphan
// reconcile to gate the bead reset on current status (hk-mdus1 review B3).
type beadStatusReader interface {
	ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error)
}

// reconcileOrphanedRunsOnResume scans the durable event log for runs that
// emitted run_started but never emitted a terminal event (run_completed or
// run_failed). These runs were active when the daemon was last killed without
// a clean shutdown, so their in-memory RunRegistry state was lost and no
// terminal event was written before exit.
//
// For each orphaned run a run_failed event is emitted so downstream observers
// (e.g. the ops-monitor review-gate) see a terminal event rather than an open
// reviewer_launched/no-verdict state that trips on every restart.
//
// hk-mdus1 — orphan queue-item advance. The emitted run_failed now carries the
// orphaned run's queue_id / queue_group_index (recovered from its run_started
// payload) so the terminal event is queue-attributed rather than queueID=nil.
// More importantly, when a bead resetter is supplied, each orphaned run's bead
// is reset in_progress → open. Because this runs BEFORE LoadQueueAtStartup
// (QM-002a), the subsequent QM-002a Beads cross-check then observes the bead as
// open and reverts its durable queue item dispatched → pending — clearing the
// stuck-'dispatched' state that otherwise makes EM-065 reject re-submission with
// -32015 independent of bead status. Without this reset, an orphan found only in
// the event log (no .harmonik/runs/ record for adoptDeadRunSessions to reset)
// leaves its queue item wedged in 'dispatched' forever.
//
// hk-eaxc5 — the reset is issued for a bead currently open as well as one still
// in_progress. An event-log-only orphan whose bead already reads open (reopened
// by another path, or the crash landed before the claim write) must still have
// the reset issued so the -32015 lock-clearing path runs; gating the reset to
// in_progress only left such orphans stuck across every subsequent restart.
//
// hk-iwu8a — dispatch-tracker-sourced orphans (no run_started, no runs/ record).
// The two loops above only ever visit runs the daemon actually knows about: one
// with a run_started event in the durable log. There is a narrower crash window
// where the durable QUEUE already recorded a bead as dispatched (queue.json
// item status=dispatched) and the bead claim already landed (bead in_progress)
// but the daemon was killed BEFORE run_started was emitted and before the
// .harmonik/runs/ record was written — e.g. a SIGKILL between the queue-claim
// write and the run-launch write. Such an orphan is invisible to both the
// runs/-record sweep (adoptDeadRunSessions) and the event-log scan above, so its
// -32015 dispatch-lock survives every restart. dispatchedBeads (the queue's live
// dispatch-tracker, loaded from a raw pre-QM-002a queue.json read) is the only
// remaining source that still has a record of it. For every bead in
// dispatchedBeads that is NOT already accounted for by an observed run_started
// (started map, regardless of terminal status — hk-mdus1's "do not reset
// legitimately-live dispatched beads" guard) and is NOT still tracked by a live
// runs/ record (liveRunBeadIDs — genuinely in-flight sessions surviving a clean
// restart, handled by adoptLiveRunSession), the same status-independent guarded
// reset is issued so QM-002a can revert the wedged queue item to pending.
//
// Returns the count of run_failed events emitted. Non-fatal: callers MUST NOT
// abort startup on a non-zero return — it is informational only. The
// dispatch-tracker-sourced resets (hk-iwu8a) have no associated run_id to emit a
// run_failed against, so they are not reflected in the returned count.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 (hk-r73qr); queue-model.md §3.2a
// QM-002a (hk-mdus1).
func reconcileOrphanedRunsOnResume(
	ctx context.Context,
	eventsPath string,
	bus handlercontract.EventEmitter,
	resetter runBeadResetter,
	statusLedger beadStatusReader,
	intentLogDir string,
	projectHash core.ProjectHash,
	daemonStartNS int64,
	dispatchedBeads lifecycle.QueueDispatchedSet,
	liveRunBeadIDs map[core.BeadID]struct{},
) int {
	if eventsPath == "" {
		return 0
	}

	type runMeta struct {
		beadID          string
		queueID         *string
		queueGroupIndex *int
	}
	started := make(map[core.RunID]runMeta)
	terminated := make(map[core.RunID]struct{})

	for ev := range eventbus.ScanAfter(eventsPath, core.EventID{}) {
		if ev.RunID == nil {
			continue
		}
		switch core.EventType(ev.Type) {
		case core.EventTypeRunStarted:
			var pl workloopRunStartedPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil || pl.BeadID == "" {
				continue
			}
			started[*ev.RunID] = runMeta{
				beadID:          pl.BeadID,
				queueID:         pl.QueueID,
				queueGroupIndex: pl.QueueGroupIndex,
			}
		case core.EventTypeRunCompleted, core.EventTypeRunFailed:
			terminated[*ev.RunID] = struct{}{}
		}
	}

	count := 0
	for runID, meta := range started {
		if _, done := terminated[runID]; done {
			continue
		}
		// hk-mdus1: thread queue routing into the terminal event.
		emitRunCompleted(ctx, bus, runID, meta.beadID, "", "", false,
			"run orphaned by daemon restart: no terminal event before shutdown",
			meta.queueID, meta.queueGroupIndex, nil)
		count++

		// hk-mdus1: reset the orphaned bead to open so QM-002a (which runs after
		// this pass, in LoadQueueAtStartup) reverts its dispatched queue item to
		// pending. Best-effort — a failed reset is logged; the next boot retries.
		//
		// hk-eaxc5 — status-independent dispatch-lock clear. The reset is not
		// merely a bead-status transition: it is what lets QM-002a observe the
		// bead as open and revert the queue item's dispatched → pending, which is
		// what actually releases the -32015 (bead_already_dispatched) lock. An
		// event-log-only orphan (no .harmonik/runs/ record for adoptDeadRunSessions
		// to reset) whose bead is ALREADY open — e.g. reopened by some other path
		// between restarts, or the daemon crashed before the claim write landed —
		// must still have this reset issued so the lock-clearing path is exercised
		// regardless of the bead's exact current status. Reset fires for both
		// in_progress and open.
		//
		// hk-mdus1 review B3 — landed-bead guard retained. Reset is skipped ONLY
		// when the bead has already landed (closed/tombstone) or is in a state
		// where a reset write is not meaningful (blocked/deferred/draft/pinned).
		// In the narrow window where the daemon crashed AFTER closing a bead but
		// BEFORE emitting run_completed, the run looks orphaned yet the bead
		// already landed; an unconditional reset would false-reopen completed
		// work. When the status ledger is absent or ShowBead fails we SKIP the
		// reset (conservative: never risk reopening a landed bead) — the
		// run_failed above is still emitted for observers.
		if resetter != nil && meta.beadID != "" && statusLedger != nil {
			resetGuardedBead(ctx, resetter, statusLedger, core.BeadID(meta.beadID),
				intentLogDir, projectHash, daemonStartNS,
				fmt.Sprintf("run %s", runID))
		}
	}

	// hk-iwu8a — dispatch-tracker-sourced orphans: beads the durable queue
	// still records as dispatched but for which no CURRENTLY-ORPHANED
	// run_started was observed above (started minus terminated) and no
	// runs/ record is still live (liveRunBeadIDs). See the function doc
	// comment for the crash window this covers. Guarded by the same
	// statusLedger check as the primary loop so a landed
	// (closed/blocked/deferred) bead is never false-reopened.
	//
	// startedBeadIDs is scoped to still-orphaned runs only (not every
	// beadID that ever appears in `started`): a bead whose PRIOR run
	// completed and was later redispatched must still be eligible for this
	// pass, since a completed historical run says nothing about whether the
	// current dispatch's run_started was ever emitted.
	//
	// startedBeadIDs (still-orphaned only) and terminatedBeadIDs (deduped
	// beads with at least one terminated run) are both derived from a single
	// pass over `started`, reused by the two passes below.
	startedBeadIDs := make(map[core.BeadID]struct{}, len(started))
	terminatedBeadIDs := make(map[core.BeadID]struct{})
	for runID, meta := range started {
		if meta.beadID == "" {
			continue
		}
		if _, done := terminated[runID]; done {
			terminatedBeadIDs[core.BeadID(meta.beadID)] = struct{}{}
			continue
		}
		startedBeadIDs[core.BeadID(meta.beadID)] = struct{}{}
	}

	if len(dispatchedBeads) > 0 && resetter != nil && statusLedger != nil {
		for beadID := range dispatchedBeads {
			if _, seen := startedBeadIDs[beadID]; seen {
				continue
			}
			if _, live := liveRunBeadIDs[beadID]; live {
				continue
			}
			resetGuardedBead(ctx, resetter, statusLedger, beadID,
				intentLogDir, projectHash, daemonStartNS,
				"dispatch-tracker orphan")
		}
	}

	// hk-hjvl4 — terminated-but-locked beads. Distinct from the hk-iwu8a pass
	// above: that pass only ever visits beads dispatchedBeads (a raw pre-
	// QM-002a read of queue.json's status=dispatched items, scoped to the
	// main queue) still records as dispatched. A run that DID emit a terminal
	// event (run_completed/run_failed) can still leave its bead's -32015
	// dispatch-lock un-released — e.g. a historical multi-bead queue-run whose
	// durable queue-item status update never landed for this bead — yet such
	// a bead is invisible to the dispatchedBeads scan (it was never a member
	// of that set, or is no longer one), so it was never reset by either pass
	// above. For every beadID with at least one terminated run in the durable
	// log (terminatedBeadIDs) that is NOT already handled by the
	// dispatchedBeads pass (beadID present there) and is NOT live, apply the
	// same guarded reset. Skipping dispatchedBeads members avoids a duplicate
	// ResetBead call for a bead the pass above already reset.
	//
	// Bead ref: hk-hjvl4.
	if resetter != nil && statusLedger != nil {
		for beadID := range terminatedBeadIDs {
			if _, seen := dispatchedBeads[beadID]; seen {
				continue
			}
			if _, live := liveRunBeadIDs[beadID]; live {
				continue
			}
			resetGuardedBead(ctx, resetter, statusLedger, beadID,
				intentLogDir, projectHash, daemonStartNS,
				"terminated-but-locked")
		}
	}

	return count
}

// resetGuardedBead issues the shared B3-guarded ResetBead call: it queries
// statusLedger and resets beadID only when the bead currently reads open or
// in_progress, skipping any bead that has already landed (closed/tombstone)
// or sits in a state where a reset write is not meaningful
// (blocked/deferred/draft/pinned) — never risking a false-reopen. logCtx is a
// short human-readable label (e.g. "run <id>", "dispatch-tracker orphan",
// "terminated-but-locked") identifying which reconcile pass is calling, for
// the stderr diagnostics on failure.
func resetGuardedBead(
	ctx context.Context,
	resetter runBeadResetter,
	statusLedger beadStatusReader,
	beadID core.BeadID,
	intentLogDir string,
	projectHash core.ProjectHash,
	daemonStartNS int64,
	logCtx string,
) {
	rec, showErr := statusLedger.ShowBead(ctx, beadID)
	switch {
	case showErr != nil:
		fmt.Fprintf(os.Stderr,
			"daemon: reconcileOrphanedRunsOnResume: ShowBead %s (%s): %v — skipping reset (will not risk reopening a landed bead)\n",
			beadID, logCtx, showErr)
	case rec.Status != core.CoarseStatusInProgress && rec.Status != core.CoarseStatusOpen:
		// Bead already terminal/blocked/deferred — not a stuck in-flight
		// claim; nothing to reset (avoids false-reopening a closed bead, B3).
	default:
		if resetErr := resetter.ResetBead(
			ctx,
			intentLogDir,
			brcli.TimeoutConfig{},
			beadID,
			projectHash,
			daemonStartNS,
		); resetErr != nil {
			fmt.Fprintf(os.Stderr,
				"daemon: reconcileOrphanedRunsOnResume: ResetBead %s (%s): %v — queue item may stay dispatched; will retry next boot\n",
				beadID, logCtx, resetErr)
		}
	}
}
