package core

// Trace is the durable AlphaGo decision record produced on every durable state
// transition (architecture.md §4.3.AR-012; execution-model.md §4.1.EM-004).
//
// A Trace is distinct from an event: events MAY project traces for streaming
// consumers, but events MUST NOT replace traces as the audit-fidelity source of
// truth per AR-012. The record is stored as a JSON sibling file at the path
// ".harmonik/transitions/<run_id>/<transition_id>.json" per EM-018.
//
// The 11-field shape is normative per AR-012. No 12th field is admitted without
// a foundation amendment per architecture.md §4.6.
//
// Several fields use placeholder types pending typed-alias beads:
//   - ActorRole  → TODO(hk-zs0.55): hoist to core.ActorRole once that bead lands.
//   - CandidateActions / ChosenAction → TODO(hk-zs0.56): hoist to []core.ActionDescriptor /
//     core.ActionDescriptor once that bead lands.
//   - PolicyVersion → TODO(hk-zs0.57): hoist to core.PolicyVersion once that bead lands.
type Trace struct {
	// PriorState is the run state immediately before the transition (AR-012 field 1).
	// Required (all subfields must be non-zero per State.Valid()).
	PriorState State

	// ActorRole is the role-name of the actor that produced this decision
	// (AR-012 field 2; architecture.md §4.8.AR-032).
	// The seven declared role names are Planner, Researcher, Builder, Reviewer,
	// Verifier, Scheduler, Governor. Daemon-synthesized transitions use
	// "daemon" or "reconciliation" per execution-model.md §4.10.EM-046.
	// Required (non-empty).
	//
	// TODO(hk-zs0.55): hoist to core.ActorRole once the typed alias lands.
	ActorRole string

	// CandidateActions is the full set of actions the actor considered before
	// choosing (AR-012 field 3; execution-model.md §6.1 candidate_actions).
	// Required (non-nil; MAY be empty for deterministic-dispatch transitions where
	// no alternative actions were considered).
	//
	// TODO(hk-zs0.56): hoist element type to core.ActionDescriptor once that bead lands.
	CandidateActions []string

	// ChosenAction is the action the actor selected from CandidateActions
	// (AR-012 field 4; execution-model.md §6.1 chosen_action).
	// Required (non-empty).
	//
	// TODO(hk-zs0.56): hoist to core.ActionDescriptor once that bead lands.
	ChosenAction string

	// PolicyVersion identifies the policy snapshot under which the decision was
	// made (AR-012 field 5; execution-model.md §6.1 policy_version).
	// Required (non-empty).
	//
	// TODO(hk-zs0.57): hoist to core.PolicyVersion once the typed alias lands.
	PolicyVersion string

	// ParameterVector holds the agent's parameter state at decision time
	// (AR-012 field 6). Structured; key-value map. MAY be nil when the actor
	// carries no tunable parameters (e.g., deterministic-dispatch transitions).
	ParameterVector map[string]any

	// Evidence is structured input the actor used to reach its decision
	// (AR-012 field 7; execution-model.md §6.1 evidence).
	// Reserved keys: sub_workflow_pin (EM-034c), synthesized_outcome (EM-023a).
	// MAY be nil when no structured evidence was produced.
	Evidence map[string]any

	// Outcome is the result status of the transition (AR-012 field 8).
	// Uses the OutcomeStatus enum defined in execution-model.md §6.1.
	// Required (must satisfy OutcomeStatus.Valid()).
	Outcome OutcomeStatus

	// VerifierMetrics holds structured metrics emitted by the verification step,
	// if any, that accompanied this transition (AR-012 field 9;
	// execution-model.md §6.1 verifier_metrics). MAY be nil when no verification
	// step ran.
	VerifierMetrics map[string]any

	// NextState is the run state immediately after the transition (AR-012 field 10).
	// Required (all subfields must be non-zero per State.Valid()).
	NextState State

	// Confidence is the actor's self-reported confidence in the chosen action,
	// expressed as a probability in [0.0, 1.0] (AR-012 field 11).
	// Optional (nil is valid for deterministic-dispatch transitions that produce
	// no confidence score). When non-nil, MUST be in [0.0, 1.0].
	Confidence *float64
}

// Valid reports whether all required fields carry non-zero/non-empty values and
// all optional pointer fields, when non-nil, satisfy their structural constraints.
//
// Rules:
//   - PriorState satisfies State.Valid()
//   - ActorRole is non-empty
//   - CandidateActions is non-nil (may be empty slice)
//   - ChosenAction is non-empty
//   - PolicyVersion is non-empty
//   - Outcome satisfies OutcomeStatus.Valid()
//   - NextState satisfies State.Valid()
//   - Confidence, when non-nil, is in [0.0, 1.0]
//
// ParameterVector, Evidence, and VerifierMetrics are optional map fields; nil
// is valid for each. CandidateActions being an empty (but non-nil) slice is
// valid for deterministic-dispatch transitions.
func (tr Trace) Valid() bool {
	if !tr.PriorState.Valid() {
		return false
	}
	if tr.ActorRole == "" {
		return false
	}
	if tr.CandidateActions == nil {
		return false
	}
	if tr.ChosenAction == "" {
		return false
	}
	if tr.PolicyVersion == "" {
		return false
	}
	if !tr.Outcome.Valid() {
		return false
	}
	if !tr.NextState.Valid() {
		return false
	}
	if tr.Confidence != nil && (*tr.Confidence < 0.0 || *tr.Confidence > 1.0) {
		return false
	}
	return true
}
