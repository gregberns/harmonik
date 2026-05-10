package core

// Trigger is the Kind-specific trigger record carried on a ControlPoint
// (specs/control-points.md §6.1 RECORD ControlPoint, field trigger).
//
// The spec declares Trigger as a "Kind-specific trigger record"; the concrete
// shape differs per Kind:
//
//   - Gate:   fires on transition attempt (pre- or post-selection per AttachPoint)
//   - Hook:   fires on event match (trigger_event from HookPayload.TriggerEvent)
//   - Guard:  fires during edge-evaluation cascade (no additional trigger field)
//   - Budget: fires on dispatch + per-chunk accrual + threshold cross
//
// At MVH the Trigger record is carried as a raw string name for lookup purposes.
// The per-Kind typed trigger records are resolved from the payload fields during
// registration (HookPayload.TriggerEvent for Hook; AttachPoint from GatePayload
// for Gate; implicit for Guard and Budget).
//
// TODO(hk-a8bg): replace with a typed per-Kind union once the CP subsystem
// registration sequence (§7.1) is implemented. The typed-trigger shape is
// deferred because it requires the symbol-table built during Pass 1 of §7.1.
type Trigger struct {
	// Name is the canonical trigger name for lookup by the dispatcher.
	// For Hook ControlPoints this corresponds to HookPayload.TriggerEvent
	// (e.g., "on_agent_started"). For Gate ControlPoints this encodes the
	// attach point. Empty only for Guard ControlPoints (implicit trigger).
	Name string `json:"name,omitempty"`
}

// Valid reports whether t is structurally valid. Guard triggers may have an
// empty name (implicit); all other Kinds must carry a non-empty name.
// Kind-level validation of name semantics is delegated to the registration
// sequence per specs/control-points.md §7.1.
func (t Trigger) Valid() bool {
	// Name may be empty for Guard; structural validity is always true at this level.
	// Registration rejects triggers that are invalid for their Kind per §7.1.
	return true
}
