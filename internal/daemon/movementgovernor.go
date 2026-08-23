package daemon

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
	if err := core.RegisterPayloadCompatEntry(core.PayloadCompatEntry{
		TypeName:          core.EventTypeGovernorSignal,
		CurrentVersion:    1,
		CompatWindowHolds: true,
		AdditiveOnly:      true,
	}); err != nil {
		panic("daemon: register governor_signal compatibility: " + err.Error()) //nolint:forbidigo // init-time registry wiring: a duplicate or bad registration is a build-time bug, and there is no caller to return an error to.
	}
}

type governorPort struct {
	state         *sentinel.GovernorState
	config        sentinel.Config
	mode          string
	phase2Classes []string
}

type governorInputPort struct {
	projectDir string
	brPath     string
	ledger     beadLedger
}

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

func (g *movementGovernor) halted() bool {
	return g != nil && g.haltRequested
}

func (g *movementGovernor) dispatchBlocked(dispatchGates dispatchGatesPort) bool {
	if g == nil {
		return false
	}
	return dispatchGates.decisionBlocker != nil && dispatchGates.decisionBlocker.IsQueueBlocked(sentinelSubjectIDACT)
}

func (g *movementGovernor) tick(ctx context.Context, input governorInputPort, schedule schedulePort, dispatchGates dispatchGatesPort) {
	if g == nil {
		return
	}
	now := time.Now()
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

func (g *movementGovernor) tickObserve(ctx context.Context, input governorInputPort, dispatchGates dispatchGatesPort, now time.Time) {
	in, _ := g.gatherInput(ctx, input, dispatchGates, now)
	sig := sentinel.Evaluate(ctx, g.port.state, in, g.port.config)
	governorEmitSignal(ctx, dispatchGates, sig)
}

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

func (g *movementGovernor) onTrip(ctx context.Context, input governorInputPort, schedule schedulePort, dispatchGates dispatchGatesPort, now time.Time, readyBeadIDs []string, hasUndeployedTail bool) {
	if g.pendingAckToken != "" {
		if externallyAcked, checkErr := sentinel.IsTripAcknowledged(input.projectDir, g.pendingAckToken); checkErr == nil && externallyAcked {
			if dispatchGates.decisionBlocker != nil {
				dispatchGates.decisionBlocker.Acknowledge(decisionAckSubjectKindQueue, sentinelSubjectIDACT, g.pendingAckToken)
			}
			g.pendingAckToken = ""
		}
	}
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

func (g *movementGovernor) gatherInput(ctx context.Context, source governorInputPort, dispatchGates dispatchGatesPort, now time.Time) (input sentinel.GovernorInput, readyBeadIDs []string) {
	if readyRecs, readyErr := source.ledger.Ready(ctx); readyErr == nil {
		for _, rec := range readyRecs {
			readyBeadIDs = append(readyBeadIDs, string(rec.BeadID))
		}
	}

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

func governorEmitSignal(ctx context.Context, dispatchGates dispatchGatesPort, sig sentinel.GovernorSignal) {
	raw, mErr := json.Marshal(sig)
	if mErr != nil {
		return
	}
	if dispatchGates.bus != nil {
		_ = dispatchGates.bus.Emit(ctx, core.EventTypeGovernorSignal, raw) //nolint:errcheck // best-effort observability emit
	}
}
