package daemon

// decisionshandler_xz9.go — DecisionsHandler interface and implementation for
// the hitl-decisions agent-side emit ops (hitl-decisions SPEC §2, component K2,
// bead hk-xz9):
//
//   - decisions-raise    → emit decision_needed, RETURN the minted decision_id
//                          (the decision_needed event's OWN event_id — SPEC §1).
//   - decisions-withdraw → emit decision_withdrawn(reason=self_obsoleted, by=<agent>).
//
// Both ops mirror the comms-send emit handler (commshandler_nbrmf.go): validate
// the request, emit the typed event via the bus's TypedEmitter (which fsyncs
// before returning for F-class events — the three decision_* types are F-class
// per SPEC §6 N1 / Risk R1), and return the minted event_id.
//
// The methods are implemented on *commsSendHandlerImpl (the same value the
// daemon already passes to RunSocketListener as its CommsSendHandler), following
// the CommsPresenceHandler / CommsRecvHandler pattern — socket.go type-asserts
// ch.(DecisionsHandler) for the decisions-* ops, so NO socket-listener signature
// change is needed.
//
// The OPERATOR-side ops (decisions-list, decisions-answer — components K4) and
// the orphan reaper (K5) are SEPARATE later beads. They will live in their own
// files; K2 deliberately implements only the two agent-side emit ops here. The
// client-side `wait` / `raise --wait` blocked-wait is a PURE CLIENT subscribe
// stream (SPEC §4 N8 arm-then-check) with NO daemon op — see cmd/harmonik/decisions.go.
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/hitl-decisions/SPEC.md §2, §1, §6 N6.
// Bead ref: hk-xz9 (component K2).

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// DecisionsHandler is the interface the daemon registers (via the shared
// commsSendHandlerImpl value, type-asserted in socket.go) to process the
// hitl-decisions agent-side emit ops. It mirrors CommsSendHandler in purpose.
//
// A nil DecisionsHandler (or a handler whose bus does not support TypedEmitter)
// causes decisions-* ops to return an error response.
//
// Spec ref: hitl-decisions SPEC §2. Bead ref: hk-xz9 (K2).
type DecisionsHandler interface {
	// HandleDecisionsRaise emits a decision_needed event from the request
	// payload (a DecisionsRaiseRequest) and returns a DecisionsRaiseResult
	// carrying the minted decision_id (= the decision_needed event's own
	// event_id).
	HandleDecisionsRaise(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)

	// HandleDecisionsWithdraw emits a decision_withdrawn event from the request
	// payload (a DecisionsWithdrawRequest) and returns a DecisionsWithdrawResult
	// carrying the minted event_id.
	HandleDecisionsWithdraw(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

// DecisionsRaiseRequest is the wire payload for a "decisions-raise" socket op.
// It maps to the decision_needed event fields (hitl-decisions SPEC §1.1).
type DecisionsRaiseRequest struct {
	// Question is the decision the human must make. REQUIRED.
	Question string `json:"question"`

	// Options is the list of enumerated choices. REQUIRED (≥1).
	Options []string `json:"options"`

	// ContextLink is an optional free-form pointer (bead id / codename / run_id).
	ContextLink string `json:"context_link,omitempty"`

	// BlockedAgent is the optional name of the emitting (blocked) agent.
	BlockedAgent string `json:"blocked_agent,omitempty"`

	// ValueRequested is the optional v1.1 free-text-answer hook (v1 ignores it).
	ValueRequested bool `json:"value_requested,omitempty"`
}

// DecisionsRaiseResult is the SocketResponse.Result payload for a successful
// decisions-raise op. DecisionID is the minted decision_needed event_id — the
// value the agent uses to `wait`, and the two terminals carry as
// payload.decision_id (SPEC §1).
type DecisionsRaiseResult struct {
	DecisionID string `json:"decision_id"`
}

// DecisionsWithdrawRequest is the wire payload for a "decisions-withdraw" socket
// op. It maps to the decision_withdrawn event fields (hitl-decisions SPEC §1.3).
type DecisionsWithdrawRequest struct {
	// DecisionID is the decision_needed event_id to withdraw. REQUIRED.
	DecisionID string `json:"decision_id"`

	// Reason is "self_obsoleted" (agent-initiated). REQUIRED; must be a valid
	// core.DecisionWithdrawnReason. The keeper-only "orphaned" reason is NOT
	// emitted by this agent-side op (SPEC §6 N9 — keeper is the sole emitter of
	// orphaned withdrawals; that is component K5).
	Reason string `json:"reason"`

	// By is the optional agent name recorded as the withdrawer.
	By string `json:"by,omitempty"`
}

// DecisionsWithdrawResult is the SocketResponse.Result payload for a successful
// decisions-withdraw op.
type DecisionsWithdrawResult struct {
	EventID string `json:"event_id"`
}

// HandleDecisionsRaise validates the request, emits a decision_needed event, and
// returns the minted decision_id (the event's own event_id).
//
// Validation (hitl-decisions SPEC §1.1, mirrors DecisionNeededPayload.Valid):
//   - question must be non-empty.
//   - options must have ≥1 element (so chosen_option validity is checkable, N7).
//
// On validation failure: returns an error; no event is emitted. On success: the
// decision_needed event is fsync'd to events.jsonl before this method returns
// (F-class, SPEC §6 N1) so the decision landmark is durable before the agent
// blocks on it.
func (h *commsSendHandlerImpl) HandleDecisionsRaise(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	if h.decisionEmitter == nil {
		return nil, fmt.Errorf("decisions-raise: typed emitter not available")
	}

	var req DecisionsRaiseRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decisions-raise: decode request payload: %w", err)
	}

	p := core.DecisionNeededPayload{
		Question:       req.Question,
		Options:        req.Options,
		ContextLink:    req.ContextLink,
		BlockedAgent:   req.BlockedAgent,
		ValueRequested: req.ValueRequested,
	}
	if !p.Valid() {
		return nil, fmt.Errorf("decisions-raise: invalid decision_needed (question required and options must be ≥1)")
	}

	payloadBytes, marshalErr := json.Marshal(p)
	if marshalErr != nil {
		return nil, fmt.Errorf("decisions-raise: marshal payload: %w", marshalErr)
	}

	eventID, err := h.decisionEmitter.EmitTyped(ctx, core.EventTypeDecisionNeeded, payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("decisions-raise: emit decision_needed: %w", err)
	}

	// The decision_id IS the decision_needed event's own event_id (SPEC §1) in
	// canonical hyphenated string form — the two terminals match on this exact
	// string (K3 key).
	result := DecisionsRaiseResult{DecisionID: eventID.String()}
	resultBytes, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, fmt.Errorf("decisions-raise: marshal result: %w", marshalErr)
	}
	return resultBytes, nil
}

// HandleDecisionsWithdraw validates the request, emits a decision_withdrawn
// event, and returns the minted event_id.
//
// Validation (hitl-decisions SPEC §1.3, mirrors DecisionWithdrawnPayload.Valid):
//   - decision_id must be non-empty.
//   - reason must be a valid core.DecisionWithdrawnReason.
//
// The agent-side op is intended for reason=self_obsoleted (the agent cancelling
// its own decision). The schema-level Valid() also permits "orphaned"; the
// SPEC §6 N9 single-writer rule (keeper is the sole emitter of orphaned
// withdrawals) is enforced at the keeper tick (K5), not re-litigated here — the
// daemon emits whatever valid reason the caller supplies. The CLI defaults
// --reason to self_obsoleted.
//
// On success the decision_withdrawn event is fsync'd before return (F-class).
func (h *commsSendHandlerImpl) HandleDecisionsWithdraw(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	if h.decisionEmitter == nil {
		return nil, fmt.Errorf("decisions-withdraw: typed emitter not available")
	}

	var req DecisionsWithdrawRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decisions-withdraw: decode request payload: %w", err)
	}

	p := core.DecisionWithdrawnPayload{
		DecisionID: req.DecisionID,
		Reason:     core.DecisionWithdrawnReason(req.Reason),
		By:         req.By,
	}
	if !p.Valid() {
		return nil, fmt.Errorf("decisions-withdraw: invalid decision_withdrawn (decision_id required and reason must be self_obsoleted or orphaned)")
	}

	payloadBytes, marshalErr := json.Marshal(p)
	if marshalErr != nil {
		return nil, fmt.Errorf("decisions-withdraw: marshal payload: %w", marshalErr)
	}

	eventID, err := h.decisionEmitter.EmitTyped(ctx, core.EventTypeDecisionWithdrawn, payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("decisions-withdraw: emit decision_withdrawn: %w", err)
	}

	result := DecisionsWithdrawResult{EventID: eventID.String()}
	resultBytes, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, fmt.Errorf("decisions-withdraw: marshal result: %w", marshalErr)
	}
	return resultBytes, nil
}
