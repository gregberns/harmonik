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
// false` means the governor is never constructed and none of it runs. A nil
// *movementGovernor is the OFF state and every method tolerates it.
//
// # Why it is switchable
//
// Observe mode ran on every production daemon before this subsystem existed,
// costing a `br` shell-out plus an O(events.jsonl) scan every eval cadence — and
// governor_signal has no Go consumer. Neither the governor nor anything it
// touches is in the core set (CHARTER §3), so it must be absent when switched
// off.
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

func init() {
	if err := core.RegisterEventType(core.EventTypeGovernorSignal, func() core.EventPayload { return &sentinel.GovernorSignal{} }); err != nil {
		panic("daemon: register governor_signal: " + err.Error()) //nolint:forbidigo // init-time registry wiring: a duplicate or bad registration is a build-time bug, and there is no caller to return an error to.
	}
}

// governorPort is the movement governor's configuration and per-daemon mutable
// state. It is built at boot and held by loopMaintenance, not legacy aggregate.
type governorPort struct {
	state         *sentinel.GovernorState
	config        sentinel.Config
	mode          string
	phase2Classes []string
}

// governorInputPort is the small read-only input to one governor tick.
type governorInputPort struct {
	projectDir string
	brPath     string
	ledger     beadLedger
}

// movementGovernor is the movement governor's per-loop mutable state. It is
// owned solely by the runWorkLoop goroutine — no locking.
//
// A nil *movementGovernor means the subsystem is OFF and every method below is
// a no-op on it.
type movementGovernor struct {
	port governorPort

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

// newMovementGovernorIfEnabled builds the governor's per-loop state. The
// enabled value comes only from the movement-governor subsystem switch.
func newMovementGovernorIfEnabled(port governorPort, enabled bool, logW io.Writer) *movementGovernor {
	if logW == nil {
		logW = os.Stderr
	}
	if !enabled {
		fmt.Fprintf(logW, "daemon: subsystem %q disabled by .harmonik/config.yaml; movement governor not constructed\n", //nolint:errcheck // best-effort stderr status log
			projectconfig.SubsystemMovementGovernor)
		return nil
	}
	return &movementGovernor{port: port, logW: logW}
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
func (g *movementGovernor) dispatchBlocked(dispatchGates dispatchGatesPort) bool {
	if g == nil {
		return false
	}
	return dispatchGates.decisionBlocker != nil && dispatchGates.decisionBlocker.IsQueueBlocked(sentinelSubjectIDACT)
}

// tick performs at most one governor evaluation, honouring the configured
// evaluation cadence. No-op on a nil (absent) governor.
//
// Cadence-gating is load-bearing, not politeness: each evaluation scans
// events.jsonl, and running it on every 2 s poll tick cost 25–50% daemon CPU on
// large logs (hk-usn8o).
func (g *movementGovernor) tick(ctx context.Context, input governorInputPort, schedule schedulePort, dispatchGates dispatchGatesPort) {
	if g == nil {
		return
	}
	now := time.Now()
	// Modes are mutually exclusive and share one cadence clock. An unrecognised
	// mode string evaluates nothing — same as the two inline guards it replaces.
	switch g.port.mode {
	case "", "observe":
		if !g.dueForEval(now) {
			return
		}
		g.tickObserve(ctx, input, dispatchGates, now)
	case "act":
		if !g.dueForEval(now) {
			return
		}
		g.tickAct(ctx, input, schedule, dispatchGates, now)
	}
}

// dueForEval reports whether the eval cadence has elapsed, stamping lastEval
// when it has. Zero or negative configured cadence falls back to the compiled
// default.
func (g *movementGovernor) dueForEval(now time.Time) bool {
	cadence := g.port.config.EvalCadence
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
func (g *movementGovernor) tickObserve(ctx context.Context, input governorInputPort, dispatchGates dispatchGatesPort, now time.Time) {
	in, _ := g.gatherInput(ctx, input, dispatchGates, now)
	sig := sentinel.Evaluate(ctx, g.port.state, in, g.port.config)
	governorEmitSignal(ctx, dispatchGates, sig)
}

// tickAct is FW3: evaluate, emit governor_signal, then act on the activation
// level — halt on a doom-loop, trip on sustained low movement with opportunity,
// clear the trip when real movement returns.
//
// Trip/clear state is durable: EmitTrip writes an ack-state file under
// .harmonik/decision_acks/ (the EV-043a anchor) AND updates the in-memory
// DecisionBlocker, so dispatchBlocked() gates all dispatch while a trip is
// pending. Config default is "observe"; operators opt into "act" explicitly.
func (g *movementGovernor) tickAct(ctx context.Context, input governorInputPort, schedule schedulePort, dispatchGates dispatchGatesPort, now time.Time) {
	in, readyBeadIDs := g.gatherInput(ctx, input, dispatchGates, now)
	sig := sentinel.Evaluate(ctx, g.port.state, in, g.port.config)
	governorEmitSignal(ctx, dispatchGates, sig)

	switch {
	case sig.Level == sentinel.ActivationHalt:
		g.onHalt(ctx, dispatchGates, sig)
	case sig.Level == sentinel.ActivationActive && sig.SuppressedBy == "":
		g.onTrip(ctx, input, schedule, dispatchGates, now, readyBeadIDs, in.HasUndeployedTail)
	case sig.Level == sentinel.ActivationDormant && g.pendingAckToken != "":
		g.onClear(ctx, input, dispatchGates, now)
	}
}

// onHalt fires the G-liveness doom-loop self-kill: emit the liveness_halt page
// event and arm haltRequested so the next loop iteration drains and exits.
func (g *movementGovernor) onHalt(ctx context.Context, dispatchGates dispatchGatesPort, sig sentinel.GovernorSignal) {
	g.haltRequested = true
	haltPayload, _ := json.Marshal(core.LivenessHaltPayload{ConsecutiveZeroCycles: sig.ConsecutiveZeroCycles, LivenessNoProgressN: g.port.config.LivenessNoProgressN}) //nolint:errcheck,errchkjson // fixed typed values cannot fail to marshal
	if dispatchGates.bus != nil {
		_ = dispatchGates.bus.Emit(ctx, core.EventTypeLivenessHalt, haltPayload) //nolint:errcheck // best-effort page emit; the halt proceeds regardless
	}
	fmt.Fprintf(g.logW, //nolint:errcheck // best-effort stderr status log
		"daemon: workloop: sentinel: G-liveness halt fired after %d zero-progress cycles (threshold=%d); halting dispatch\n",
		sig.ConsecutiveZeroCycles, g.port.config.LivenessNoProgressN)
}

// onTrip emits a decision_required trip for sustained low movement with
// opportunity, then spawns the adjudicating adversary crew.
//
// AC4 (hk-jvul): before tripping, reconcile the in-memory pending token against
// the on-disk ack file. A captain legitimate-halt (record-halt CLI) may have
// externally acknowledged it between ticks; when that happened, clear the
// in-memory token and the DecisionBlocker so this pass emits a fresh trip for
// re-adjudication (spec §2.2 clause 2).
func (g *movementGovernor) onTrip(ctx context.Context, input governorInputPort, schedule schedulePort, dispatchGates dispatchGatesPort, now time.Time, readyBeadIDs []string, hasUndeployedTail bool) {
	if g.pendingAckToken != "" {
		if externallyAcked, checkErr := sentinel.IsTripAcknowledged(input.projectDir, g.pendingAckToken); checkErr == nil && externallyAcked {
			if dispatchGates.decisionBlocker != nil {
				dispatchGates.decisionBlocker.Acknowledge(decisionAckSubjectKindQueue, sentinelSubjectIDACT, g.pendingAckToken)
			}
			g.pendingAckToken = ""
		}
	}
	// Idempotent: EmitTrip returns the existing ack_token if one is pending.
	if g.pendingAckToken == "" {
		tok, tripErr := sentinel.EmitTrip(ctx, sentinel.TripInput{
			ProjectDir:        input.projectDir,
			ReadyBeadIDs:      readyBeadIDs,
			HasUndeployedTail: hasUndeployedTail,
			Now:               now,
		})
		if tripErr != nil {
			fmt.Fprintf(g.logW, "daemon: workloop: sentinel: EmitTrip failed (non-fatal): %v\n", tripErr) //nolint:errcheck // best-effort stderr status log
		} else if tok != "" {
			g.pendingAckToken = tok
			if dispatchGates.decisionBlocker != nil {
				dispatchGates.decisionBlocker.AddQueueBlock(sentinelSubjectIDACT, tok)
			}
		}
	}
	g.spawnAdversary(ctx, input.projectDir, schedule)
}

// spawnAdversary is FW4 (hk-jsvc): spawn a fresh-context adversary crew to
// adjudicate the trip. It reviews captain comms/commits as a foreign artifact
// and emits its own sentinel trip if it confirms the governor's verdict.
//
// The SchedulePort holds both non-core dependencies. Its crew handler is nil
// whenever the socket subtree is absent. Its comms querier is nil in unit-test
// mode. A nil crew handler means no adversary. A nil comms querier gives an
// empty online set, so overlap checks fail open as they did before extraction.
func (g *movementGovernor) spawnAdversary(ctx context.Context, projectDir string, schedule schedulePort) {
	if schedule.crewHandler == nil {
		return
	}
	var onlineAgents map[string]struct{}
	if schedule.commsWhoQuerier != nil {
		if agents, whoErr := schedule.commsWhoQuerier(ctx); whoErr == nil {
			onlineAgents = agents
		}
	}
	if onlineAgents == nil {
		onlineAgents = map[string]struct{}{}
	}
	if _, spawnErr := sentinel.SpawnAdversary(ctx, sentinel.AdversaryInput{
		ProjectDir: projectDir,
	}, schedule.crewHandler, onlineAgents); spawnErr != nil {
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
func (g *movementGovernor) onClear(ctx context.Context, input governorInputPort, dispatchGates dispatchGatesPort, now time.Time) {
	alreadyAcked, _ := sentinel.IsTripAcknowledged(input.projectDir, g.pendingAckToken) //nolint:errcheck // an unreadable ack file reads as "not acknowledged"; ClearTrip below is then the authority
	if !alreadyAcked {
		if clearErr := sentinel.ClearTrip(ctx, input.projectDir, g.pendingAckToken, now); clearErr != nil {
			fmt.Fprintf(g.logW, "daemon: workloop: sentinel: ClearTrip failed (non-fatal): %v\n", clearErr) //nolint:errcheck // best-effort stderr status log
			return                                                                                          // preserve pendingAckToken for retry on the next eval
		}
	}
	if dispatchGates.decisionBlocker != nil {
		dispatchGates.decisionBlocker.Acknowledge(decisionAckSubjectKindQueue, sentinelSubjectIDACT, g.pendingAckToken)
	}
	g.pendingAckToken = ""
}

// gatherInput assembles one GovernorInput, and returns the ready-bead
// IDs alongside it because the ACT-mode trip payload needs them (observe mode
// discards them).
//
// Both signals fail soft: a `br` error means "no ready beads" / "no undeployed
// tail" rather than an aborted evaluation, matching the inline behaviour.
func (g *movementGovernor) gatherInput(ctx context.Context, source governorInputPort, dispatchGates dispatchGatesPort, now time.Time) (input sentinel.GovernorInput, readyBeadIDs []string) {
	// hasReadyBeads: ≥1 unblocked open bead exists (flywheel-motion.md §1.3).
	// Left nil (not an empty slice) when there is nothing ready, so the ACT-mode
	// trip payload marshals ready_bead_ids exactly as it did inline.
	if readyRecs, readyErr := source.ledger.Ready(ctx); readyErr == nil {
		for _, rec := range readyRecs {
			readyBeadIDs = append(readyBeadIDs, string(rec.BeadID))
		}
	}

	// HasUndeployedTail: a closed Phase-2-class bead is present (§1.3, §5.2).
	// Skip the br call entirely when no Phase-2 classes are configured.
	var hasUndeployedTail bool
	if len(g.port.phase2Classes) > 0 && source.brPath != "" {
		hasUndeployedTail, _ = digest.BuildHasUndeployedTail(ctx, source.brPath, g.port.phase2Classes) //nolint:errcheck // fails soft: a br error reads as "no undeployed tail", never an aborted evaluation
	}

	return sentinel.GovernorInput{
		ProjectDir:        source.projectDir,
		Now:               now,
		HasReadyBeads:     len(readyBeadIDs) > 0,
		HasUndeployedTail: hasUndeployedTail,
		OperatorPaused:    dispatchGates.operatorPauseCtrl != nil && dispatchGates.operatorPauseCtrl.IsPaused(),
	}, readyBeadIDs
}

// governorEmitSignal publishes one governor_signal. Best-effort: a marshal or
// emit failure never interrupts the evaluation.
func governorEmitSignal(ctx context.Context, dispatchGates dispatchGatesPort, sig sentinel.GovernorSignal) {
	raw, mErr := json.Marshal(sig)
	if mErr != nil {
		return
	}
	if dispatchGates.bus != nil {
		_ = dispatchGates.bus.Emit(ctx, core.EventTypeGovernorSignal, raw) //nolint:errcheck // best-effort observability emit
	}
}
