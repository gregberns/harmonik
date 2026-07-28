package daemon

// movementgovernor.go — the sentinel movement governor as a SWITCHABLE
// subsystem, lifted out of runWorkLoop's body.
//
// # What this is
//
// The governor watches for "the fleet has stopped moving": it scans events.jsonl
// on a cadence, asks `br ready` whether there is anything to do, and emits a
// governor_signal. In ACT mode it additionally trips a dispatch-blocking entry on
// the DecisionBlocker, spawns an adversary crew to adjudicate the trip, and can
// halt the daemon outright on a doom-loop.
//
// Two large blocks of exactly that logic used to be welded inline into
// runWorkLoop's poll loop — one per mode. They are here now, behind
// newMovementGovernorIfEnabled, so that `subsystems.movement_governor.enabled:
// false` means the governor's per-loop state is NEVER CONSTRUCTED and none of it
// runs. A nil *movementGovernor is the OFF state and every method tolerates it;
// that is the whole nil-guard surface, answered once and documented, rather than
// repeated at five call sites in the loop.
//
// # Why it is switchable
//
// Observe mode ran on every production daemon before this subsystem existed:
// seedGovernorDeps allocated governorState for any non-empty ProjectDir, costing
// a `br` shell-out plus an O(events.jsonl) scan every eval cadence — and
// governor_signal has no Go consumer. Neither the governor nor anything it
// touches is in the core set (CHARTER §3), so it must be absent when switched
// off. seedGovernorDeps now reads the same switch and skips the allocation
// entirely; see newMovementGovernorIfEnabled on why both gates exist.
//
// # NOT part of this subsystem
//
// sentinel.ComputeSnapshot and sentinel.DetectLayerA are unrelated per-run stall
// detectors that merely share the package name; nothing here reaches them and
// switching the governor off does not affect them.
//
// Spec ref: docs/flywheel-motion.md §§1, 2, 6.1 (FW2 observe, FW3 act).
// Bead ref: hk-z1lr (FW2), hk-4toh (FW3), hk-usn8o (cadence gate), hk-jvul (AC4),
// hk-jsvc (FW4 adversary).
// Codename: subsystem-partition-01.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/digest"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/sentinel"
)

// movementGovernor is the movement governor's per-loop mutable state. It is
// owned solely by the runWorkLoop goroutine — no locking.
//
// A nil *movementGovernor means the subsystem is OFF (or was never seeded) and
// every method below is a no-op on it.
type movementGovernor struct {
	// lastEval is the wall clock of the most recent sentinel.Evaluate call.
	// Zero makes the first tick fire immediately.
	lastEval time.Time

	// pendingAckToken is the ack_token of the in-flight ACT-mode trip; empty
	// when no trip is pending. Persists across ticks so the dormant transition
	// clears the correct token.
	pendingAckToken string

	// haltRequested records that the G-liveness self-kill gate fired. The loop
	// reads it at the top of each iteration via halted() and drains.
	haltRequested bool

	// logW receives the governor's non-fatal status lines. Never nil after
	// construction.
	logW io.Writer
}

// newMovementGovernorIfEnabled builds the governor's per-loop state, or returns
// nil when the subsystem must be ABSENT.
//
// It returns nil in two cases, and the distinction matters:
//
//   - `subsystems.movement_governor.enabled: false` — the operator partitioned it
//     away. Said loudly on the log writer, because a silent partition is
//     indistinguishable from a config that did not take effect.
//   - deps.governorState is nil — nothing seeded the governor (unit-test mode, an
//     empty ProjectDir, or the same subsystem switch read earlier at boot). This
//     is the pre-existing `governorState != nil` guard the inline blocks carried,
//     kept at the same seam.
//
// ORDER IS LOAD-BEARING: a disabled subsystem now trips BOTH branches, because
// bootState.seedGovernorDeps reads the same switch and returns before allocating
// GovernorState (hk-e3y8x — it also guards a FATAL sentinel-config read, so an
// off subsystem must not be able to refuse the daemon's boot). The Enabled check
// must therefore stay FIRST, or the loud "disabled by config" line is replaced by
// the silent nil-state return and a partition becomes indistinguishable from a
// config that never took effect. That is the whole reason the two are separate.
//
// An absent subsystems: block enables the governor, so a deployment without one
// behaves exactly as it did before the block existed.
func newMovementGovernorIfEnabled(deps workLoopDeps, logW io.Writer) *movementGovernor {
	if logW == nil {
		logW = os.Stderr
	}
	if !deps.projectCfg.Subsystems.Enabled(projectconfig.SubsystemMovementGovernor) {
		fmt.Fprintf(logW, "daemon: subsystem %q disabled by .harmonik/config.yaml; movement governor not constructed\n", //nolint:errcheck // best-effort stderr status log
			projectconfig.SubsystemMovementGovernor)
		return nil
	}
	if deps.governorState == nil {
		return nil
	}
	return &movementGovernor{logW: logW}
}

// halted reports whether the G-liveness gate has fired and the loop should drain
// and exit. False on a nil (absent) governor — nothing else can request the halt.
func (g *movementGovernor) halted() bool {
	return g != nil && g.haltRequested
}

// dispatchBlocked reports whether an ACT-mode governor trip is currently gating
// dispatch (EV-043 queue-level gate, subject "sentinel").
//
// It returns false when the governor is ABSENT, and that is deliberate rather
// than incidental. The "sentinel" queue block has exactly one writer — this
// subsystem's ACT mode — plus a boot-time restore of that same writer's ack file
// (LoadDecisionAckState, EV-043a), and exactly one clearer: this subsystem's
// dormant branch. Keeping the READ while removing the subsystem would leave a
// gate no code path can open: one stale pending trip file in
// .harmonik/decision_acks/ would wedge every dispatch on every subsequent boot,
// forever. A subsystem that is off does not get to hold the dispatcher shut.
func (g *movementGovernor) dispatchBlocked(deps workLoopDeps) bool {
	if g == nil {
		return false
	}
	return deps.decisionBlocker != nil && deps.decisionBlocker.IsQueueBlocked(sentinelSubjectIDACT)
}

// tick performs at most one governor evaluation, honouring the configured
// evaluation cadence. No-op on a nil (absent) governor.
//
// Cadence-gating is load-bearing, not politeness: each evaluation scans
// events.jsonl, and running it on every 2 s poll tick cost 25–50% daemon CPU on
// large logs (hk-usn8o).
func (g *movementGovernor) tick(ctx context.Context, deps workLoopDeps) {
	if g == nil {
		return
	}
	now := time.Now()
	// Modes are mutually exclusive and share one cadence clock. An unrecognised
	// mode string evaluates nothing — same as the two inline guards it replaces.
	switch deps.sentinelMode {
	case "", "observe":
		if !g.dueForEval(deps, now) {
			return
		}
		g.tickObserve(ctx, deps, now)
	case "act":
		if !g.dueForEval(deps, now) {
			return
		}
		g.tickAct(ctx, deps, now)
	}
}

// dueForEval reports whether the eval cadence has elapsed, stamping lastEval
// when it has. Zero or negative configured cadence falls back to the compiled
// default.
func (g *movementGovernor) dueForEval(deps workLoopDeps, now time.Time) bool {
	cadence := deps.governorCfg.EvalCadence
	if cadence <= 0 {
		cadence = sentinel.DefaultSentinelEvalCadence
	}
	if now.Sub(g.lastEval) < cadence {
		return false
	}
	g.lastEval = now
	return true
}

// tickObserve is FW2: evaluate and emit governor_signal, nothing more.
//
// OBSERVE-ONLY CONTRACT: no trip, no halt, no dispatch side-effects.
func (g *movementGovernor) tickObserve(ctx context.Context, deps workLoopDeps, now time.Time) {
	in, _ := governorGatherInput(ctx, deps, now)
	sig := sentinel.Evaluate(ctx, deps.governorState, in, deps.governorCfg)
	governorEmitSignal(ctx, deps, sig)
}

// tickAct is FW3: evaluate, emit governor_signal, then act on the activation
// level — halt on a doom-loop, trip on sustained low movement with opportunity,
// clear the trip when real movement returns.
//
// Trip/clear state is durable: EmitTrip writes an ack-state file under
// .harmonik/decision_acks/ (the EV-043a anchor) AND updates the in-memory
// DecisionBlocker, so dispatchBlocked() gates all dispatch while a trip is
// pending. Config default is "observe"; operators opt into "act" explicitly.
func (g *movementGovernor) tickAct(ctx context.Context, deps workLoopDeps, now time.Time) {
	in, readyBeadIDs := governorGatherInput(ctx, deps, now)
	sig := sentinel.Evaluate(ctx, deps.governorState, in, deps.governorCfg)
	governorEmitSignal(ctx, deps, sig)

	switch {
	case sig.Level == sentinel.ActivationHalt:
		g.onHalt(ctx, deps, sig)
	case sig.Level == sentinel.ActivationActive && sig.SuppressedBy == "":
		g.onTrip(ctx, deps, now, readyBeadIDs, in.HasUndeployedTail)
	case sig.Level == sentinel.ActivationDormant && g.pendingAckToken != "":
		g.onClear(ctx, deps, now)
	}
}

// onHalt fires the G-liveness doom-loop self-kill: emit the liveness_halt page
// event and arm haltRequested so the next loop iteration drains and exits.
func (g *movementGovernor) onHalt(ctx context.Context, deps workLoopDeps, sig sentinel.GovernorSignal) {
	g.haltRequested = true
	haltPayload, _ := json.Marshal(map[string]interface{}{ //nolint:errcheck,errchkjson // a fixed map of two ints cannot fail to marshal
		"consecutive_zero_cycles": sig.ConsecutiveZeroCycles,
		"liveness_no_progress_n":  deps.governorCfg.LivenessNoProgressN,
	})
	_ = deps.bus.Emit(ctx, core.EventTypeLivenessHalt, haltPayload) //nolint:errcheck // best-effort page emit; the halt proceeds regardless
	fmt.Fprintf(g.logW,                                             //nolint:errcheck // best-effort stderr status log
		"daemon: workloop: sentinel: G-liveness halt fired after %d zero-progress cycles (threshold=%d); halting dispatch\n",
		sig.ConsecutiveZeroCycles, deps.governorCfg.LivenessNoProgressN)
}

// onTrip emits a decision_required trip for sustained low movement with
// opportunity, then spawns the adjudicating adversary crew.
//
// AC4 (hk-jvul): before tripping, reconcile the in-memory pending token against
// the on-disk ack file. A captain legitimate-halt (record-halt CLI) may have
// externally acknowledged it between ticks; when that happened, clear the
// in-memory token and the DecisionBlocker so this pass emits a fresh trip for
// re-adjudication (spec §2.2 clause 2).
func (g *movementGovernor) onTrip(ctx context.Context, deps workLoopDeps, now time.Time, readyBeadIDs []string, hasUndeployedTail bool) {
	if g.pendingAckToken != "" {
		if externallyAcked, checkErr := sentinel.IsTripAcknowledged(deps.projectDir, g.pendingAckToken); checkErr == nil && externallyAcked {
			if deps.decisionBlocker != nil {
				deps.decisionBlocker.Acknowledge(decisionAckSubjectKindQueue, sentinelSubjectIDACT, g.pendingAckToken)
			}
			g.pendingAckToken = ""
		}
	}
	// Idempotent: EmitTrip returns the existing ack_token if one is pending.
	if g.pendingAckToken == "" {
		tok, tripErr := sentinel.EmitTrip(ctx, sentinel.TripInput{
			ProjectDir:        deps.projectDir,
			ReadyBeadIDs:      readyBeadIDs,
			HasUndeployedTail: hasUndeployedTail,
			Now:               now,
		})
		if tripErr != nil {
			fmt.Fprintf(g.logW, "daemon: workloop: sentinel: EmitTrip failed (non-fatal): %v\n", tripErr) //nolint:errcheck // best-effort stderr status log
		} else if tok != "" {
			g.pendingAckToken = tok
			if deps.decisionBlocker != nil {
				deps.decisionBlocker.AddQueueBlock(sentinelSubjectIDACT, tok)
			}
		}
	}
	g.spawnAdversary(ctx, deps)
}

// spawnAdversary is FW4 (hk-jsvc): spawn a fresh-context adversary crew to
// adjudicate the trip. It reviews captain comms/commits as a foreign artifact
// and emits its own sentinel trip if it confirms the governor's verdict.
//
// Both dependencies it needs are NON-CORE and may legitimately be nil — the crew
// handler is nil whenever the socket subtree is absent, and the comms querier is
// nil in unit-test mode. Nil crew handler means no adversary at all; nil comms
// querier means an empty online set, which SpawnAdversary reads as "not already
// online" (the overlap-skip fails open, matching the pre-extraction behaviour).
func (g *movementGovernor) spawnAdversary(ctx context.Context, deps workLoopDeps) {
	if deps.crewHandler == nil {
		return
	}
	var onlineAgents map[string]struct{}
	if deps.commsWhoQuerier != nil {
		if agents, whoErr := deps.commsWhoQuerier(ctx); whoErr == nil {
			onlineAgents = agents
		}
	}
	if onlineAgents == nil {
		onlineAgents = map[string]struct{}{}
	}
	if _, spawnErr := sentinel.SpawnAdversary(ctx, sentinel.AdversaryInput{
		ProjectDir: deps.projectDir,
	}, deps.crewHandler, onlineAgents); spawnErr != nil {
		fmt.Fprintf(g.logW, "daemon: workloop: sentinel: SpawnAdversary failed (non-fatal): %v\n", spawnErr) //nolint:errcheck // best-effort stderr status log
	}
}

// onClear releases a pending trip because real movement returned. Only governor
// movement (bead_closed / run_completed / HEAD-advance) clears a trip — not the
// captain's say-so alone.
//
// AC4 (hk-jvul): when the ack was already acknowledged externally (e.g.
// RecordLegitimateHalt), skip ClearTrip so we do not stack a spurious
// governor_movement event on top of the existing legitimate_halt clear — but
// always release the in-memory token and the DecisionBlocker so dispatch resumes.
func (g *movementGovernor) onClear(ctx context.Context, deps workLoopDeps, now time.Time) {
	alreadyAcked, _ := sentinel.IsTripAcknowledged(deps.projectDir, g.pendingAckToken) //nolint:errcheck // an unreadable ack file reads as "not acknowledged"; ClearTrip below is then the authority
	if !alreadyAcked {
		if clearErr := sentinel.ClearTrip(ctx, deps.projectDir, g.pendingAckToken, now); clearErr != nil {
			fmt.Fprintf(g.logW, "daemon: workloop: sentinel: ClearTrip failed (non-fatal): %v\n", clearErr) //nolint:errcheck // best-effort stderr status log
			return                                                                                          // preserve pendingAckToken for retry on the next eval
		}
	}
	if deps.decisionBlocker != nil {
		deps.decisionBlocker.Acknowledge(decisionAckSubjectKindQueue, sentinelSubjectIDACT, g.pendingAckToken)
	}
	g.pendingAckToken = ""
}

// governorGatherInput assembles one GovernorInput, and returns the ready-bead
// IDs alongside it because the ACT-mode trip payload needs them (observe mode
// discards them).
//
// Both signals fail soft: a `br` error means "no ready beads" / "no undeployed
// tail" rather than an aborted evaluation, matching the inline behaviour.
func governorGatherInput(ctx context.Context, deps workLoopDeps, now time.Time) (input sentinel.GovernorInput, readyBeadIDs []string) {
	// hasReadyBeads: ≥1 unblocked open bead exists (flywheel-motion.md §1.3).
	// Left nil (not an empty slice) when there is nothing ready, so the ACT-mode
	// trip payload marshals ready_bead_ids exactly as it did inline.
	if readyRecs, readyErr := deps.brAdapter.Ready(ctx); readyErr == nil {
		for _, rec := range readyRecs {
			readyBeadIDs = append(readyBeadIDs, string(rec.BeadID))
		}
	}

	// HasUndeployedTail: a closed Phase-2-class bead is present (§1.3, §5.2).
	// Skip the br call entirely when no Phase-2 classes are configured.
	var hasUndeployedTail bool
	if len(deps.sentinelPhase2Classes) > 0 && deps.brPath != "" {
		hasUndeployedTail, _ = digest.BuildHasUndeployedTail(ctx, deps.brPath, deps.sentinelPhase2Classes) //nolint:errcheck // fails soft: a br error reads as "no undeployed tail", never an aborted evaluation
	}

	return sentinel.GovernorInput{
		ProjectDir:        deps.projectDir,
		Now:               now,
		HasReadyBeads:     len(readyBeadIDs) > 0,
		HasUndeployedTail: hasUndeployedTail,
		OperatorPaused:    deps.operatorPauseCtrl != nil && deps.operatorPauseCtrl.IsPaused(),
	}, readyBeadIDs
}

// governorEmitSignal publishes one governor_signal. Best-effort: a marshal or
// emit failure never interrupts the evaluation.
func governorEmitSignal(ctx context.Context, deps workLoopDeps, sig sentinel.GovernorSignal) {
	raw, mErr := json.Marshal(sig)
	if mErr != nil {
		return
	}
	_ = deps.bus.Emit(ctx, core.EventTypeGovernorSignal, raw) //nolint:errcheck // best-effort observability emit
}
