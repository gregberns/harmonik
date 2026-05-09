package core

// GatePayload is the kind-specific payload for Gate ControlPoints
// (specs/control-points.md §6.1.1 RECORD GatePayload).
//
// GatePayload lives inside a ControlPoint whose Kind = KindGate. It carries
// the four fields that distinguish Gate behaviour:
//
//	RECORD GatePayload:
//	    subtype          : GateSubtype   -- one of {goal-gate, approval-gate, quality-gate}
//	    attach_point     : AttachPoint   -- one of {node-pre-entry, node-post-exit, edge-before-selection, edge-after-selection}
//	    named_approver   : String | None -- required when subtype = approval-gate
//	    verification_ref : String | None -- required when subtype = quality-gate
//
// # Conditional-field invariants
//
// NamedApprover MUST be non-nil when Subtype = GateSubtypeApproval
// (specs/control-points.md §6.1.1). Valid() enforces this.
//
// VerificationRef MUST be non-nil when Subtype = GateSubtypeQuality
// (specs/control-points.md §6.1.1). Valid() enforces this.
//
// For goal-gate (GateSubtypeGoal), both optional fields MUST be nil;
// Valid() enforces this to reject confused registrations.
type GatePayload struct {
	// Subtype discriminates the operator intent of the gate. Must be one of the
	// three declared GateSubtype constants.
	Subtype GateSubtype `json:"subtype"`

	// AttachPoint declares where in the execution lifecycle this gate fires.
	// Must be one of the four declared AttachPoint constants.
	AttachPoint AttachPoint `json:"attach_point"`

	// NamedApprover identifies the human role or agent role required to approve
	// the transition (specs/control-points.md §6.1.1, architecture.md §4.8).
	// MUST be non-nil when Subtype = GateSubtypeApproval; MUST be nil otherwise.
	NamedApprover *string `json:"named_approver,omitempty"`

	// VerificationRef names a prior verification node whose outcome must satisfy
	// a policy expression (specs/control-points.md §6.1.1).
	// MUST be non-nil when Subtype = GateSubtypeQuality; MUST be nil otherwise.
	VerificationRef *string `json:"verification_ref,omitempty"`
}

// Valid reports whether g is a well-formed GatePayload.
//
// The following invariants are checked:
//   - Subtype must be a declared GateSubtype value (via GateSubtype.Valid)
//   - AttachPoint must be a declared AttachPoint value (via AttachPoint.Valid)
//   - NamedApprover must be non-nil and non-empty when Subtype = GateSubtypeApproval
//   - NamedApprover must be nil when Subtype != GateSubtypeApproval
//   - VerificationRef must be non-nil and non-empty when Subtype = GateSubtypeQuality
//   - VerificationRef must be nil when Subtype != GateSubtypeQuality
//
// These conditional invariants follow specs/control-points.md §6.1.1 directly.
func (g GatePayload) Valid() bool {
	if !g.Subtype.Valid() {
		return false
	}
	if !g.AttachPoint.Valid() {
		return false
	}

	switch g.Subtype {
	case GateSubtypeApproval:
		// NamedApprover required and must be non-empty; VerificationRef must be absent.
		if g.NamedApprover == nil || *g.NamedApprover == "" {
			return false
		}
		if g.VerificationRef != nil {
			return false
		}
	case GateSubtypeQuality:
		// VerificationRef required and must be non-empty; NamedApprover must be absent.
		if g.VerificationRef == nil || *g.VerificationRef == "" {
			return false
		}
		if g.NamedApprover != nil {
			return false
		}
	case GateSubtypeGoal:
		// Neither conditional field applies; both must be absent.
		if g.NamedApprover != nil {
			return false
		}
		if g.VerificationRef != nil {
			return false
		}
	}

	return true
}
