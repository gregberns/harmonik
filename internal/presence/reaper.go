package presence

// reaper.go — the hitl-decisions orphan reaper (component K5, bead hk-061).
//
// # What it does (the N9 normative contract)
//
// ReapOrphanedDecisions iterates the current OPEN-decision set (the K3
// projection) and, for each open decision whose blocked_agent is TRULY GONE,
// emits decision_withdrawn(reason=orphaned, by="keeper"). It is invoked from the
// session-keeper WATCH TICK (internal/keeper/watcher.go), NOT from the daemon's
// 1-hour reconciliation sweep — so orphan latency is bounded by ≤ Offline-cutoff
// (~10min) + one keeper tick, never ~1h (SPEC §5, latency bound NORMATIVE).
//
// # The "truly gone" predicate (N9 — load-bearing)
//
// Presence has only Online / Stale / Offline and NO "waiting-on-decision"
// signal. A genuinely-waiting blocked agent stays Online via its §4 subscribe
// stream heartbeat — so a merely-Stale agent (TTL ≤ quiet < StaleCutoff, i.e.
// quiet < 10min) is presumed STILL BLOCKED and is NOT reaped (the TOCTOU trap).
// The only sound "gone" signals are:
//
//	(a) the agent emitted an explicit `leave` beat, OR
//	(b) the agent is OFFLINE — past the ~10-min StaleCutoff.
//
// presence.GetState collapses both into StateOffline (a leave beat short-circuits
// to StateOffline; an age ≥ StaleCutoff also yields StateOffline), so the single
// predicate is `GetState(rec) == StateOffline`. A Stale agent yields StateStale
// → NOT reaped.
//
// An agent with NO presence record at all (never seen) is NOT reaped: absence of
// a record is no evidence the agent is gone, and reaping on bare absence would
// withdraw freshly-raised decisions whose agent simply hasn't beaten yet. (This
// matches the K4 list-flag's read-side treatment of unknown agents.)
//
// # Sole emitter + idempotency (N9 / N3)
//
// The keeper tick (this function) is the SOLE emitter of orphaned withdrawals;
// `decisions list` (K4) only FLAGS orphaned-pending, read-pure. The emit is
// idempotent: ReapOrphanedDecisions re-reads the open set fresh each call, so a
// decision that was answered/withdrawn between two ticks has already left the
// open set and is not re-withdrawn (handles the answer-just-landed-while-reaping
// race — N3). Even if a stale duplicate withdraw were emitted, the projection's
// delete-on-absent-key fold makes a second decision_withdrawn for the same
// decision_id a no-op.
//
// # Why it does NOT call answer/wake anything
//
// The blocked agent, being gone, never needed the answer → withdrawing leaves no
// zombie (SPEC §5 G6 / S7a). The withdrawal simply removes the decision from the
// open set so the operator's `decisions list` queue is not polluted by a dead
// agent's question.
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/hitl-decisions/SPEC.md §5 (orphan
// reaper predicate + cadence/latency bound), §6 N9 (predicate + single-writer),
// §6 N3 (idempotent withdraw).
// Bead ref: hk-061 (component K5).

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

	// Compute presence ONCE per pass (single scan), reused for every decision.
	registry := ComputeRegistry(eventsPath)

	for _, dec := range open {
		// A decision with no blocked_agent has no agent to liveness-check — it
		// cannot be orphaned by the agent-gone predicate, so skip it.
		if dec.BlockedAgent == "" {
			continue
		}
		rec, known := registry[dec.BlockedAgent]
		if !known {
			// No presence evidence at all: NOT reaped (bare absence ≠ gone).
			continue
		}
		// The N9 predicate: Offline (leave beat OR age ≥ StaleCutoff). Stale and
		// Online both fail this — a Stale agent is presumed still-blocked.
		if GetState(rec) != StateOffline {
			continue
		}

		// Emit decision_withdrawn(orphaned, by=keeper). On emit error, record
		// nothing for this decision and keep going — a transient emit failure on
		// one decision must not abort the whole pass; the next tick retries.
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
			// Leave it for the next tick; a partial pass is fine (idempotent).
			continue
		}
		res.Reaped++
		res.DecisionIDs = append(res.DecisionIDs, dec.DecisionID)
	}

	return res, nil
}
