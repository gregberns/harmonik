package core

// KindPayload is the discriminated union of per-Kind typed payload records
// carried on a ControlPoint (specs/control-points.md §6.1 RECORD ControlPoint,
// field payload).
//
// Exactly one of the four fields MUST be non-nil, matching the ControlPoint's
// Kind. The Valid() method enforces this invariant and the per-field validity
// check delegates to each payload type's own Valid() method.
//
//   - KindGate   → Gate is non-nil; Hook, Guard, Budget are nil
//   - KindHook   → Hook is non-nil; Gate, Guard, Budget are nil
//   - KindGuard  → Guard is non-nil; Gate, Hook, Budget are nil
//   - KindBudget → Budget is non-nil; Gate, Hook, Guard are nil
type KindPayload struct {
	// Gate carries the GatePayload when Kind = KindGate.
	// Nil when Kind != KindGate.
	Gate *GatePayload `json:"gate,omitempty"`

	// Hook carries the HookPayload when Kind = KindHook.
	// Nil when Kind != KindHook.
	Hook *HookPayload `json:"hook,omitempty"`

	// Guard carries the GuardPayload when Kind = KindGuard.
	// Nil when Kind != KindGuard.
	Guard *GuardPayload `json:"guard,omitempty"`

	// Budget carries the BudgetPayload when Kind = KindBudget.
	// Nil when Kind != KindBudget.
	Budget *BudgetPayload `json:"budget,omitempty"`
}

// ValidForKind reports whether kp contains exactly the payload expected for k
// and that payload is itself valid.
//
// A zero-value KindPayload (all nil) is never valid. A KindPayload with more
// than one non-nil field is never valid. Callers MUST pass the ControlPoint's
// Kind so that the correct non-nil field is checked.
func (kp KindPayload) ValidForKind(k Kind) bool {
	switch k {
	case KindGate:
		return kp.Gate != nil && kp.Gate.Valid() &&
			kp.Hook == nil && kp.Guard == nil && kp.Budget == nil
	case KindHook:
		return kp.Hook != nil && kp.Hook.Valid() &&
			kp.Gate == nil && kp.Guard == nil && kp.Budget == nil
	case KindGuard:
		return kp.Guard != nil && kp.Guard.Valid() &&
			kp.Gate == nil && kp.Hook == nil && kp.Budget == nil
	case KindBudget:
		return kp.Budget != nil && kp.Budget.Valid() &&
			kp.Gate == nil && kp.Hook == nil && kp.Guard == nil
	default:
		return false
	}
}
