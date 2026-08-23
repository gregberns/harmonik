package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// DecisionsListRequest is the wire payload for a "decisions-list" socket op.
// The list op returns the full open set; `show <id>` filters client-side, so
// no server-side filter field is required in v1. (DecisionID is accepted but
// optional — when set, the daemon may narrow the result to that one decision;
// the CLI also filters client-side, so either path yields the same output.)
type DecisionsListRequest struct {
	// DecisionID, when non-empty, narrows the result to a single decision
	// (the `show <id>` path). Optional — the CLI also filters client-side.
	DecisionID string `json:"decision_id,omitempty"`

	// Topic, when non-empty, narrows the result to decisions raised with this
	// exact topic (e.g. core.DecisionTopicOperatorMailbox for `harmonik
	// mailbox`, bead hk-pltjs, pending operator sign-off). Optional.
	Topic string `json:"topic,omitempty"`
}

// DecisionsListItem is one open decision in the decisions-list result. It mirrors
// the K3 Decision shape (the fields `decisions list`/`show` render): question ·
// options · blocked_agent · context_link · decision_id (SPEC §2).
type DecisionsListItem struct {
	DecisionID     string   `json:"decision_id"`
	Question       string   `json:"question"`
	Options        []string `json:"options"`
	BlockedAgent   string   `json:"blocked_agent,omitempty"`
	ContextLink    string   `json:"context_link,omitempty"`
	ValueRequested bool     `json:"value_requested,omitempty"`
	Topic          string   `json:"topic,omitempty"`
	Urgency        string   `json:"urgency,omitempty"`
}

// DecisionsListResult is the SocketResponse.Result payload for a successful
// decisions-list op: every open decision (the cross-agent what-needs-me queue).
//
// The orphaned-pending flag is NOT carried here — it is a CLIENT-side display
// concern computed from agent presence (only computable in cmd/harmonik), kept
// read-pure (no event). See the file header.
type DecisionsListResult struct {
	Decisions []DecisionsListItem `json:"decisions"`
}

// DecisionsAnswerRequest is the wire payload for a "decisions-answer" socket op.
// It maps to the decision_resolved event fields (hitl-decisions SPEC §1.2).
type DecisionsAnswerRequest struct {
	// DecisionID is the decision_needed event_id being resolved. REQUIRED.
	DecisionID string `json:"decision_id"`

	// ChosenOption is the picked option. REQUIRED; must be one of the open
	// decision's options (N7) when the decision is OPEN.
	ChosenOption string `json:"chosen_option"`

	// Value is the optional free-text answer (v1.1 hook; empty in v1).
	Value string `json:"value,omitempty"`

	// Resolver is the optional name of who answered (e.g. "operator").
	Resolver string `json:"resolver,omitempty"`
}

// DecisionsAnswerResult is the SocketResponse.Result payload for a decisions-answer
// op. EventID is the minted decision_resolved event_id when a resolution was
// emitted; NoOp is true when the decision_id was unknown or already-terminal
// (N3 first-writer-wins) — in which case no event was emitted and EventID is
// empty. Both cases return Ok=true (a no-op is NOT an error per N3).
type DecisionsAnswerResult struct {
	EventID string `json:"event_id,omitempty"`
	NoOp    bool   `json:"noop,omitempty"`
}

// HandleDecisionsList reads the K3 open-decision projection and returns every
// open decision (SPEC §2 — the cross-agent what-needs-me queue). It is a PURE
// READ op: it emits NO event and starts no aggregator process (SPEC §3 / S6).
//
// When the request carries a non-empty decision_id, the result is narrowed to
// that single decision (the `show <id>` path); otherwise all open decisions are
// returned. The orphaned-pending Offline flag is computed CLIENT-side (presence
// is only computable in cmd/harmonik), so this op stays read-pure per N9.
func (h *commsSendHandlerImpl) HandleDecisionsList(_ context.Context, payload json.RawMessage) (json.RawMessage, error) {
	if h.eventsJSONLPath == "" {
		return nil, fmt.Errorf("decisions-list: events JSONL path not configured")
	}

	var req DecisionsListRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("decisions-list: decode request payload: %w", err)
		}
	}

	open := decisionsProjection(h.eventsJSONLPath)

	items := make([]DecisionsListItem, 0, len(open))
	for _, d := range open {
		if req.DecisionID != "" && d.DecisionID != req.DecisionID {
			continue
		}
		if req.Topic != "" && d.Topic != req.Topic {
			continue
		}
		items = append(items, DecisionsListItem{
			DecisionID:     d.DecisionID,
			Question:       d.Question,
			Options:        d.Options,
			BlockedAgent:   d.BlockedAgent,
			ContextLink:    d.ContextLink,
			ValueRequested: d.ValueRequested,
			Topic:          d.Topic,
			Urgency:        string(d.Urgency),
		})
	}

	result := DecisionsListResult{Decisions: items}
	resultBytes, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, fmt.Errorf("decisions-list: marshal result: %w", marshalErr)
	}
	return resultBytes, nil
}

// HandleDecisionsAnswer validates an answer against the K3 open set and, if
// valid, emits decision_resolved (SPEC §1.2). The three outcomes:
//
//   - decision_id is OPEN and chosen_option ∈ its options → emit
//     decision_resolved, return EventID (the happy path).
//   - decision_id is OPEN but chosen_option ∉ its options → ERROR (N7). No emit.
//   - decision_id is UNKNOWN or already-terminal (not in the open set) → NO-OP:
//     Ok=true, NoOp=true, no event, no error (N3 first-writer-wins). This is the
//     load-bearing distinction from the bad-option error: a bad option on an
//     open decision is a user error worth surfacing; an unknown/terminal id is
//     the idempotent replay/second-human case the spec says to swallow silently.
//
// Validation order is deliberate: openness is checked FIRST (via the projection
// lookup), so an unknown id short-circuits to the no-op before any option check —
// option validity is only meaningful for a decision that is actually open.
func (h *commsSendHandlerImpl) HandleDecisionsAnswer(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	if h.decisionEmitter == nil {
		return nil, fmt.Errorf("decisions-answer: typed emitter not available")
	}
	if h.eventsJSONLPath == "" {
		return nil, fmt.Errorf("decisions-answer: events JSONL path not configured")
	}

	var req DecisionsAnswerRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decisions-answer: decode request payload: %w", err)
	}
	if req.DecisionID == "" {
		return nil, fmt.Errorf("decisions-answer: decision_id is required")
	}
	if req.ChosenOption == "" {
		return nil, fmt.Errorf("decisions-answer: chosen_option is required")
	}

	open := decisionsProjection(h.eventsJSONLPath)
	dec, isOpen := open[req.DecisionID]
	if !isOpen {
		result := DecisionsAnswerResult{NoOp: true}
		resultBytes, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return nil, fmt.Errorf("decisions-answer: marshal no-op result: %w", marshalErr)
		}
		return resultBytes, nil
	}

	if !decisionOptionValid(dec.Options, req.ChosenOption) {
		return nil, fmt.Errorf("decisions-answer: chosen_option %q is not one of the decision's options %v (N7)", req.ChosenOption, dec.Options)
	}

	p := core.DecisionResolvedPayload{
		DecisionID:   req.DecisionID,
		ChosenOption: req.ChosenOption,
		Value:        req.Value,
		Resolver:     req.Resolver,
	}
	if !p.Valid() {
		return nil, fmt.Errorf("decisions-answer: invalid decision_resolved (decision_id and chosen_option required)")
	}

	payloadBytes, marshalErr := json.Marshal(p)
	if marshalErr != nil {
		return nil, fmt.Errorf("decisions-answer: marshal payload: %w", marshalErr)
	}

	eventID, err := h.decisionEmitter.EmitTyped(ctx, core.EventTypeDecisionResolved, payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("decisions-answer: emit decision_resolved: %w", err)
	}

	result := DecisionsAnswerResult{EventID: eventID.String()}
	resultBytes, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, fmt.Errorf("decisions-answer: marshal result: %w", marshalErr)
	}
	return resultBytes, nil
}

func decisionOptionValid(options []string, chosen string) bool {
	for _, o := range options {
		if o == chosen {
			return true
		}
	}
	return false
}
