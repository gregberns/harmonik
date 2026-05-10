package core

// Evaluator carries the evaluation strategy for a ControlPoint: either a
// mechanism-tagged PolicyExpression or a cognition-tagged DelegationPath
// (specs/control-points.md §6.1 RECORD Evaluator).
//
//	RECORD Evaluator:
//	    mode            : ModeTag                 -- mechanism | cognition
//	    expression      : PolicyExpression | None -- set when mode = mechanism
//	    delegation_path : DelegationPath | None   -- set when mode = cognition (see §6.1.5)
//
// Exactly one of Expression and DelegationPath MUST be non-nil, matching Mode.
// Valid() enforces this invariant.
type Evaluator struct {
	// Mode discriminates the evaluation strategy. Must be ModeTagMechanism or
	// ModeTagCognition.
	Mode ModeTag `json:"mode"`

	// Expression is the policy expression evaluated by the mechanism engine
	// (specs/control-points.md §6.4, CP-034). MUST be non-nil when Mode =
	// ModeTagMechanism; MUST be nil when Mode = ModeTagCognition.
	Expression *PolicyExpression `json:"expression,omitempty"`

	// DelegationPath names the cognition-tagged evaluation target per §6.1.5.
	// MUST be non-nil when Mode = ModeTagCognition; MUST be nil when Mode =
	// ModeTagMechanism.
	DelegationPath *DelegationPath `json:"delegation_path,omitempty"`
}

// Valid reports whether e satisfies the structural invariants from
// specs/control-points.md §6.1:
//
//   - Mode must be a recognised ModeTag constant.
//   - When Mode = ModeTagMechanism: Expression must be non-nil and non-empty;
//     DelegationPath must be nil.
//   - When Mode = ModeTagCognition: DelegationPath must be non-nil and valid;
//     Expression must be nil.
func (e Evaluator) Valid() bool {
	if !e.Mode.Valid() {
		return false
	}
	switch e.Mode {
	case ModeTagMechanism:
		if e.Expression == nil || !e.Expression.Valid() {
			return false
		}
		if e.DelegationPath != nil {
			return false
		}
	case ModeTagCognition:
		if e.DelegationPath == nil || !e.DelegationPath.Valid() {
			return false
		}
		if e.Expression != nil {
			return false
		}
	}
	return true
}
