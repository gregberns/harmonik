package presence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// Emitter is the minimal event-emission surface the orphan reaper needs. It is
// satisfied by internal/keeper.Emitter and by *eventbus.busImpl (via its
// EmitWithRunID method) — so the keeper tick passes its own emitter straight
// through, and a daemon-resident caller could pass the bus. Defined locally so
// internal/presence stays a leaf (it does not import keeper or take an eventbus
// dependency for emission).
type Emitter interface {
	EmitWithRunID(ctx context.Context, runID core.RunID, eventType core.EventType, payload []byte) error
}

// ReapResult summarizes one ReapOrphanedDecisions pass for logging/observability.
type ReapResult struct {
	// Open is the number of open decisions examined on this pass.
	Open int
	// Reaped is the number of decisions withdrawn as orphaned on this pass.
	Reaped int
	// DecisionIDs are the decision_ids withdrawn on this pass (for log/test).
	DecisionIDs []string
}

// ReapOrphanedDecisions runs one orphan-reap pass over the events.jsonl log at
// eventsPath, emitting decision_withdrawn(reason=orphaned, by="keeper") for every
// open decision whose blocked_agent is OFFLINE (the N9 "truly gone" predicate:
// an explicit leave beat OR age ≥ StaleCutoff — never merely Stale).
//
// It is PURE-READ for everything except the withdrawal emits: it reads the open
// set (OpenDecisions) and the presence registry (ComputeRegistry) from the same
// durable log, then emits at most one decision_withdrawn per orphaned decision.
//
// Idempotency (N3): the open set is re-read fresh on every call, so a decision
// answered or withdrawn between ticks is already gone and is not re-withdrawn.
// emitter MUST be non-nil; a nil emitter returns an error without scanning.
//
// The "by" field is always "keeper" — this function is the keeper-tick sole
// emitter of orphaned withdrawals (N9).
func ReapOrphanedDecisions(ctx context.Context, eventsPath string, emitter Emitter) (ReapResult, error) {
	var res ReapResult
	if emitter == nil {
		return res, fmt.Errorf("presence.ReapOrphanedDecisions: nil emitter")
	}
	if eventsPath == "" {
		return res, fmt.Errorf("presence.ReapOrphanedDecisions: empty events path")
	}

	open := OpenDecisions(eventsPath)
	res.Open = len(open)
	if len(open) == 0 {
		return res, nil
	}

	registry := ComputeRegistry(eventsPath)

	for _, dec := range open {
		if dec.BlockedAgent == "" {
			continue
		}
		rec, known := registry[dec.BlockedAgent]
		if !known {
			continue
		}
		if GetState(rec) != StateOffline {
			continue
		}

		p := core.DecisionWithdrawnPayload{
			DecisionID: dec.DecisionID,
			Reason:     core.DecisionWithdrawnReasonOrphaned,
			By:         "keeper",
		}
		if !p.Valid() {
			continue
		}
		payloadBytes, marshalErr := json.Marshal(p)
		if marshalErr != nil {
			continue
		}
		if emitErr := emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeDecisionWithdrawn, payloadBytes); emitErr != nil {
			continue
		}
		res.Reaped++
		res.DecisionIDs = append(res.DecisionIDs, dec.DecisionID)
	}

	return res, nil
}
