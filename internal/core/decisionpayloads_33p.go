package core

// decisionpayloads_33p.go — event-bus payload types for the hitl-decisions
// typed events (hitl-decisions SPEC §1, bead hk-33p, component K1):
//
//   - decision_needed    (§1.1) — an agent's request for a human decision
//   - decision_resolved  (§1.2) — a human's answer (chosen option) to a decision
//   - decision_withdrawn (§1.3) — a decision closed without an answer (self-obsoleted or orphaned)
//
// All three ride the standard EV-001 envelope (event.go) and are the agent→human
// dual of agent-comms. They are F-class (fsync-boundary per
// eventbus.fsyncBoundaryEventTypes / SPEC §6 N1) — a lost terminal would leave
// the blocked agent waiting forever (Risk R1, load-bearing). Modeled on
// AgentMessagePayload (agentcommspayloads_djqc9.go).
//
// decision_id is the decision_needed event's own bus-minted event_id (UUIDv7);
// the producing code (component K2) fills it. The two terminals carry it as
// payload.decision_id — distinct from their own event_id (SPEC C7). K1 defines
// the schema only (no raise/answer/projection — those land in later components).
//
// These are DISTINCT from the pre-existing §8.12 decision_required /
// decision_acknowledged daemon-escalation family.
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/hitl-decisions/SPEC.md §1, §6.
// Bead ref: hk-33p.

// DecisionWithdrawnReason is the typed discriminator for the reason field of a
// decision_withdrawn event (hitl-decisions SPEC §1.3). v1 accepts exactly two
// reasons: an agent self-obsoleting its own decision, or the keeper reaping an
// orphaned one.
type DecisionWithdrawnReason string

const (
	// DecisionWithdrawnReasonSelfObsoleted indicates the emitting agent
	// withdrew its own decision because it no longer needs the answer.
	DecisionWithdrawnReasonSelfObsoleted DecisionWithdrawnReason = "self_obsoleted"

	// DecisionWithdrawnReasonOrphaned indicates the keeper reaped a decision
	// whose blocked agent is truly gone (left or Offline). The keeper is the
	// sole emitter of orphaned withdrawals (SPEC §6 N9).
	DecisionWithdrawnReasonOrphaned DecisionWithdrawnReason = "orphaned"
)

// Valid reports whether r is one of the two declared DecisionWithdrawnReason
// constants.
func (r DecisionWithdrawnReason) Valid() bool {
	switch r {
	case DecisionWithdrawnReasonSelfObsoleted, DecisionWithdrawnReasonOrphaned:
		return true
	default:
		return false
	}
}

// DecisionTopicOperatorMailbox is the reserved topic value for the
// operator-mailbox convention (bead hk-pltjs): a decision raised on this
// topic is rendered by `harmonik mailbox` (a thin alias of
// `decisions list --topic operator-mailbox`) and the dashboard mailbox
// section. This EXTENDS the FINALIZED hitl-decisions SPEC (open decision D3)
// rather than adding a second bus — decisions on this topic still ride the
// same decision_needed/decision_resolved/decision_withdrawn lifecycle.
//
// NEEDS OPERATOR SIGN-OFF: this topic + urgency extension has not gone
// through the spec-change gate the FINALIZED hitl-decisions SPEC requires
// for new payload fields (SPEC.md §9). It is implemented additively (both
// fields optional, zero-value = prior wire-compatible behavior) so existing
// callers are unaffected pending sign-off.
const DecisionTopicOperatorMailbox = "operator-mailbox"

// DecisionUrgency is the typed discriminator for the optional urgency field
// of a decision_needed event, added for the operator-mailbox topic
// convention (bead hk-pltjs, pending operator sign-off — see
// DecisionTopicOperatorMailbox).
type DecisionUrgency string

const (
	// DecisionUrgencyBlocker marks a decision that is blocking forward progress.
	DecisionUrgencyBlocker DecisionUrgency = "blocker"
	// DecisionUrgencyQuestion marks a decision that is a non-blocking question.
	DecisionUrgencyQuestion DecisionUrgency = "question"
	// DecisionUrgencyFYI marks a decision that is informational only.
	DecisionUrgencyFYI DecisionUrgency = "fyi"
)

// Valid reports whether u is one of the three declared DecisionUrgency
// constants, or empty (urgency is optional).
func (u DecisionUrgency) Valid() bool {
	switch u {
	case "", DecisionUrgencyBlocker, DecisionUrgencyQuestion, DecisionUrgencyFYI:
		return true
	default:
		return false
	}
}

// DecisionNeededPayload is the typed event payload for the decision_needed
// event (hitl-decisions SPEC §1.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: F (fsync-boundary — the decision-request landmark must be
// durable before the agent blocks on it; see eventbus.fsyncBoundaryEventTypes).
//
// The decision is keyed in the open-set projection (component K3) by this
// event's own bus-minted event_id; that value is returned to the agent as the
// decision_id.
//
// # Payload fields (hitl-decisions SPEC §1.1)
//
//   - question        — the decision the human must make (REQUIRED)
//   - options         — enumerated choices, ≥1 element (REQUIRED)
//   - context_link    — free-form: bead id / work codename / thread / run_id (OPTIONAL)
//   - blocked_agent   — the emitting agent (who is blocked) (OPTIONAL)
//   - value_requested — v1.1 hook: if true the answer MAY carry free-text value;
//     v1 ignores it (OPTIONAL)
type DecisionNeededPayload struct {
	// Question is the decision the human must make. Required (non-empty).
	Question string `json:"question"`

	// Options is the list of enumerated choices. Required (≥1 element). In v1,
	// a decision_resolved's chosen_option MUST be one of these (SPEC N7).
	Options []string `json:"options"`

	// ContextLink is an optional free-form pointer to context for the decision:
	// a bead id, work codename, comms thread, or run_id. Empty means none.
	ContextLink string `json:"context_link,omitempty"`

	// BlockedAgent is the optional name of the emitting agent that is blocked
	// on this decision. Used by the orphan reaper (K5) and keeper seam (K6).
	BlockedAgent string `json:"blocked_agent,omitempty"`

	// ValueRequested is an optional v1.1 hook. When true, a future resolver MAY
	// supply a free-text value in addition to a chosen option. v1 ignores it.
	ValueRequested bool `json:"value_requested,omitempty"`

	// Topic is an optional free-form routing tag. The reserved value
	// DecisionTopicOperatorMailbox ("operator-mailbox") marks a decision as
	// belonging to the operator-mailbox convention (bead hk-pltjs, pending
	// operator sign-off — see DecisionTopicOperatorMailbox). Empty means the
	// decision is untagged (prior wire-compatible behavior).
	Topic string `json:"topic,omitempty"`

	// Urgency is an optional operator-mailbox-flavor hint: blocker | question |
	// fyi (bead hk-pltjs, pending operator sign-off). Empty is valid and means
	// unspecified.
	Urgency DecisionUrgency `json:"urgency,omitempty"`
}

// Valid reports whether p is a well-formed DecisionNeededPayload.
//
// Rules per hitl-decisions SPEC §1.1 (and N7, which requires options ≥1 so
// chosen_option validity is checkable downstream):
//   - Question must be non-empty.
//   - Options must have at least one element.
func (p DecisionNeededPayload) Valid() bool {
	if p.Question == "" {
		return false
	}
	if len(p.Options) < 1 {
		return false
	}
	if !p.Urgency.Valid() {
		return false
	}
	return true
}

// DecisionResolvedPayload is the typed event payload for the decision_resolved
// event (hitl-decisions SPEC §1.2).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: F (fsync-boundary — a lost answer leaves the blocked agent
// waiting forever, SPEC §6 N1 / Risk R1; see eventbus.fsyncBoundaryEventTypes).
//
// Emitted when a human answers an open decision. First-writer-wins on
// decision_id (SPEC §9): the first decision_resolved for a given decision_id is
// authoritative; later ones are no-ops. The chosen_option MUST be one of the
// originating decision_needed.options (SPEC N7); that cross-event check is
// enforced at the answer site (component K4), not at this schema level.
//
// # Payload fields (hitl-decisions SPEC §1.2)
//
//   - decision_id   — the decision_needed event's event_id (REQUIRED)
//   - chosen_option — the picked option (REQUIRED; must be one of options, N7)
//   - value         — optional free-text answer (v1.1; empty in v1) (OPTIONAL)
//   - resolver      — who answered, e.g. "operator" (OPTIONAL)
type DecisionResolvedPayload struct {
	// DecisionID is the event_id of the decision_needed event being resolved.
	// Required (non-empty). Distinct from this event's own event_id (SPEC C7).
	DecisionID string `json:"decision_id"`

	// ChosenOption is the picked option. Required (non-empty). In v1 it MUST be
	// one of the originating decision_needed.options (SPEC N7); that validation
	// happens against the projection at the answer site (K4), not here.
	ChosenOption string `json:"chosen_option"`

	// Value is an optional free-text answer (v1.1 hook). Empty in v1.
	Value string `json:"value,omitempty"`

	// Resolver is the optional name of who answered (e.g. "operator").
	Resolver string `json:"resolver,omitempty"`
}

// Valid reports whether p is a well-formed DecisionResolvedPayload.
//
// Rules per hitl-decisions SPEC §1.2:
//   - DecisionID must be non-empty.
//   - ChosenOption must be non-empty.
func (p DecisionResolvedPayload) Valid() bool {
	if p.DecisionID == "" {
		return false
	}
	if p.ChosenOption == "" {
		return false
	}
	return true
}

// DecisionWithdrawnPayload is the typed event payload for the decision_withdrawn
// event (hitl-decisions SPEC §1.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: F (fsync-boundary — a lost withdrawal could leave a stale
// decision in the open set; see eventbus.fsyncBoundaryEventTypes).
//
// Emitted either by the agent itself (reason=self_obsoleted) when it no longer
// needs the answer, or by the keeper (reason=orphaned, by="keeper") when the
// blocked agent is truly gone. The keeper is the SOLE emitter of orphaned
// withdrawals (SPEC §6 N9).
//
// # Payload fields (hitl-decisions SPEC §1.3)
//
//   - decision_id — the decision_needed event's event_id (REQUIRED)
//   - reason      — "self_obsoleted" or "orphaned" (REQUIRED)
//   - by          — who withdrew: agent name (self_obsoleted) or "keeper"
//     (orphaned) (OPTIONAL)
type DecisionWithdrawnPayload struct {
	// DecisionID is the event_id of the decision_needed event being withdrawn.
	// Required (non-empty). Distinct from this event's own event_id (SPEC C7).
	DecisionID string `json:"decision_id"`

	// Reason is why the decision was withdrawn. Required; must be a valid
	// DecisionWithdrawnReason constant ("self_obsoleted" or "orphaned").
	Reason DecisionWithdrawnReason `json:"reason"`

	// By is the optional name of who withdrew the decision: the agent name for
	// a self_obsoleted withdrawal, or "keeper" for an orphaned one. Empty when
	// unspecified.
	By string `json:"by,omitempty"`
}

// Valid reports whether p is a well-formed DecisionWithdrawnPayload.
//
// Rules per hitl-decisions SPEC §1.3:
//   - DecisionID must be non-empty.
//   - Reason must be a valid DecisionWithdrawnReason constant
//     ("self_obsoleted" or "orphaned").
func (p DecisionWithdrawnPayload) Valid() bool {
	if p.DecisionID == "" {
		return false
	}
	if !p.Reason.Valid() {
		return false
	}
	return true
}
