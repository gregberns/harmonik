package core

import "github.com/google/uuid"

// GateDecisionRecordedPayload is the typed event payload for the
// gate_decision_recorded event.
//
// Tags: mechanism
// Durability class: O (ordinary — observability and audit).
type GateDecisionRecordedPayload struct {
	// RunID is the run whose gate node was evaluated. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// NodeID is the gate node that produced the decision. Required (non-empty).
	NodeID NodeID `json:"node_id"`

	// PolicyID is the gate's registry name from the GateDecisionPayload.
	// Required (non-empty).
	PolicyID string `json:"policy_id"`

	// Decision is the gate evaluator's verdict (allow, deny, escalate-to-human).
	Decision GateAction `json:"decision"`

	// DecisionActor names the actor that produced the decision.
	// Required (non-empty).
	DecisionActor string `json:"decision_actor"`

	// OutcomeStatus is the Outcome.Status value paired with this gate decision.
	// Included for audit completeness — allows log consumers to verify the
	// status/decision coupling without loading the full outcome record.
	OutcomeStatus OutcomeStatus `json:"outcome_status"`
}

// Valid reports whether p is a well-formed GateDecisionRecordedPayload.
func (p GateDecisionRecordedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.NodeID == "" {
		return false
	}
	if p.PolicyID == "" {
		return false
	}
	if !p.Decision.Valid() {
		return false
	}
	if p.DecisionActor == "" {
		return false
	}
	if !p.OutcomeStatus.Valid() {
		return false
	}
	return true
}
