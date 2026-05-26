package core

// GateDecisionPayload is the kind-specific payload carried by an Outcome when
// kind = gate_decision (execution-model.md §4.1.EM-005b; control-points.md
// §4.13 CP-058; §6.1.8 RECORD GateDecisionPayload).
//
// The five fields capture both the evaluator's verdict and the audit trail
// required to interpret the decision under replay per CP-058.
//
// # Status coupling
//
// A gate_decision Outcome's status MUST be SUCCESS regardless of the Decision
// field value (a deny is a successfully-evaluated Gate per CP-058). A handler
// that cannot evaluate the Gate MUST return status=FAIL with no payload instead.
//
// # ResolutionSignalID coupling
//
// ResolutionSignalID MUST be non-nil when Decision == GateActionEscalateToHuman,
// and MUST be nil when Decision ∈ {allow, deny}. Valid() enforces this invariant.
//
// # DecisionActor values
//
// For mechanism-tagged Gate evaluators DecisionActor MUST be the literal string
// "mechanism". For cognition-tagged evaluators DecisionActor MUST be the role
// name from the DelegationPath (e.g. "reviewer"). This follows CP-058 §3.
type GateDecisionPayload struct {
	// PolicyID is the gate's registry name per control-points.md §4.1 CP-002.
	// Required (non-empty). Identifies which policy produced this decision.
	PolicyID string `json:"policy_id"`

	// Decision is the gate evaluator's verdict. One of {allow, deny,
	// escalate-to-human} per §6.1.6 GateAction.
	Decision GateAction `json:"decision"`

	// DecisionActor names the actor that produced the decision. For
	// mechanism-tagged evaluators MUST be "mechanism"; for cognition-tagged
	// evaluators MUST be the role name from the DelegationPath per CP-058 §3.
	// Required (non-empty).
	DecisionActor string `json:"decision_actor"`

	// DecisionEvidenceRef is an optional audit pointer. For cognition-tagged
	// evaluators MUST be the path to the persisted GateVerdictRecord (keyed by
	// gate_name in the Transition evidence). For mechanism-tagged evaluators MAY
	// be nil when no auxiliary evidence is produced.
	DecisionEvidenceRef *string `json:"decision_evidence_ref,omitempty"`

	// ResolutionSignalID names the resolution signal the run is waiting on when
	// Decision == GateActionEscalateToHuman. The run enters quarantine pending
	// external resolution per CP-009. MUST be nil when Decision ∈ {allow, deny}.
	ResolutionSignalID *string `json:"resolution_signal_id,omitempty"`
}

// Valid reports whether p is a well-formed GateDecisionPayload.
//
// Rules per control-points.md §6.1.8 CP-058:
//   - PolicyID must be non-empty.
//   - Decision must be a declared GateAction value (via GateAction.Valid).
//   - DecisionActor must be non-empty.
//   - ResolutionSignalID must be non-nil when Decision == GateActionEscalateToHuman.
//   - ResolutionSignalID must be nil when Decision ∈ {allow, deny}.
func (p GateDecisionPayload) Valid() bool {
	if p.PolicyID == "" {
		return false
	}
	if !p.Decision.Valid() {
		return false
	}
	if p.DecisionActor == "" {
		return false
	}
	switch p.Decision {
	case GateActionEscalateToHuman:
		if p.ResolutionSignalID == nil || *p.ResolutionSignalID == "" {
			return false
		}
	default:
		if p.ResolutionSignalID != nil {
			return false
		}
	}
	return true
}
