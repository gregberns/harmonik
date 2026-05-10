package core

// ControlPoint is the unified typed record for all four ControlPoint Kinds
// (Gate, Hook, Guard, Budget) registered in the daemon registry
// (specs/control-points.md §6.1 RECORD ControlPoint, §4.1.CP-001, v0.3.2).
//
// A ControlPoint is the single typed primitive parameterised by Kind. The
// common fields — Name, Kind, Trigger, Evaluator, OutcomeAction, Payload,
// Axes, ModeTag, SchemaVersion — are shared by all four Kinds; the per-Kind
// semantics live in the typed Payload (see [KindPayload]).
//
// Registration rules (§4.9):
//   - Name MUST be unique within the daemon registry.
//   - Kind MUST be one of the four declared constants.
//   - An unknown Kind MUST fail registration per CP-001.
//   - Re-registration with an identical body (see body equality below) succeeds
//     silently per CP-044.
//   - Re-registration with a divergent body under an existing name MUST fail
//     per CP-044.
//
// Body equality (§4.9.CP-044): the "body" for equality purposes is the tuple
// (Kind, Trigger, Evaluator, Payload). Name, Axes, and SchemaVersion are NOT
// part of the body — a schema-version bump on an otherwise-identical
// ControlPoint MUST NOT be rejected as divergent.
//
// Determinism (§4.9.CP-046): list-returning registry lookups sort by Name
// ascending; no nondeterministic input (wall-clock, PID, map iteration order)
// is incorporated in registry state.
//
//	RECORD ControlPoint:
//	    name           : String
//	    kind           : Kind
//	    trigger        : Trigger
//	    evaluator      : Evaluator
//	    outcome_action : OutcomeAction
//	    payload        : KindPayload
//	    axes           : AxisTags
//	    mode_tag       : ModeTag
//	    schema_version : Integer
type ControlPoint struct {
	// Name uniquely identifies this ControlPoint within the daemon registry
	// (specs/control-points.md §6.1). Must be non-empty.
	Name string `json:"name"`

	// Kind is the type discriminator parameterising this ControlPoint into one
	// of the four surfaces: Gate, Hook, Guard, Budget (§6.1 ENUM Kind).
	Kind Kind `json:"kind"`

	// Trigger is the Kind-specific trigger record (§6.1 RECORD Trigger).
	// See [Trigger] for the MVH placeholder shape.
	Trigger Trigger `json:"trigger"`

	// Evaluator carries the evaluation strategy — mechanism (PolicyExpression)
	// or cognition (DelegationPath) — per §6.1 RECORD Evaluator and CP-039.
	Evaluator Evaluator `json:"evaluator"`

	// OutcomeAction is the per-Kind declared action enum. Must be Valid() and
	// must match the Kind per [OutcomeAction.ValidForKind] (§6.1).
	OutcomeAction OutcomeAction `json:"outcome_action"`

	// Payload is the discriminated union of per-Kind typed payload records.
	// Exactly the field matching Kind must be non-nil (§6.1).
	Payload KindPayload `json:"payload"`

	// Axes is the four-axis determinism classification tuple (architecture.md
	// §4.1 AR-001) carried on every ControlPoint crossing a subsystem boundary.
	Axes AxisTags `json:"axes"`

	// ModeTag is the mechanism/cognition discriminator (architecture.md §4.2
	// AR-005). Must match Evaluator.Mode.
	ModeTag ModeTag `json:"mode_tag"`

	// SchemaVersion is the policy-document schema version at which this
	// ControlPoint was declared (§4.7.CP-038). Required; must be positive.
	// N-1 readability is enforced by the reader per CP-038, not by this type.
	SchemaVersion int `json:"schema_version"`
}

// Valid reports whether cp is a structurally well-formed ControlPoint.
//
// The following invariants are checked:
//   - Name must be non-empty.
//   - Kind must be a declared constant (via [Kind.Valid]).
//   - Evaluator must be valid (via [Evaluator.Valid]).
//   - OutcomeAction must be valid (via [OutcomeAction.Valid]) and must match
//     Kind (via [OutcomeAction.ValidForKind]).
//   - Payload must contain exactly the field matching Kind (via
//     [KindPayload.ValidForKind]).
//   - Axes must be a valid four-axis tuple (via [AxisTags.Valid]).
//   - ModeTag must be a declared constant (via [ModeTag.Valid]) and must match
//     Evaluator.Mode.
//   - SchemaVersion must be positive.
//
// Guard+cognition evaluator combinations are syntactically valid here; the
// cognition-Guard rejection (CP-020) is enforced at the registration sequence
// level (§7.1), not at the type level.
func (cp ControlPoint) Valid() bool {
	if cp.Name == "" {
		return false
	}
	if !cp.Kind.Valid() {
		return false
	}
	if !cp.Evaluator.Valid() {
		return false
	}
	if !cp.OutcomeAction.Valid() {
		return false
	}
	if !cp.OutcomeAction.ValidForKind(cp.Kind) {
		return false
	}
	if !cp.Payload.ValidForKind(cp.Kind) {
		return false
	}
	if !cp.Axes.Valid() {
		return false
	}
	if !cp.ModeTag.Valid() {
		return false
	}
	if cp.ModeTag != cp.Evaluator.Mode {
		return false
	}
	if cp.SchemaVersion < 1 {
		return false
	}
	return true
}

// Registry is the in-process table of registered ControlPoint instances keyed
// by name, owned by S02 (specs/control-points.md §6.1.7 INTERFACE Registry,
// §4.9.CP-043 through CP-047).
//
// All methods MUST satisfy §4.9.CP-046 (determinism): given identical registry
// state and identical query inputs, methods produce identical outputs. List-
// returning methods apply a total ordering (by Name ascending) before returning.
// The registry MUST NOT incorporate nondeterministic inputs (wall-clock time,
// PID, map iteration order exposed to callers).
//
// The Registry is daemon-scoped per §4.9.CP-045 and is rebuilt on every daemon
// start from the policy YAML chain; there is no cross-restart persistence.
//
//	INTERFACE Registry:
//	    Register(cp) -> error
//	    LookupByName(name) -> (ControlPoint, Bool)
//	    LookupByTrigger(trigger) -> List<ControlPoint>
//	    LookupByAttachPoint(attach_point) -> List<ControlPoint>
//	    All() -> List<ControlPoint>
//
// Tags: mechanism
type Registry interface {
	// Register adds cp to the registry.
	//
	// Re-registration-safe on identical body per §4.9.CP-044: registering a
	// ControlPoint whose Name is already registered and whose body
	// (Kind, Trigger, Evaluator, Payload) is identical succeeds silently.
	//
	// Fails with a non-nil error when cp.Name is already registered with a
	// divergent body, when cp is structurally invalid (cp.Valid() == false),
	// or when cp.Kind is unknown.
	Register(cp ControlPoint) error

	// LookupByName returns the ControlPoint registered under name and true, or
	// a zero ControlPoint and false when name is not registered.
	//
	// Deterministic: identical registry state and name always produce the same
	// result (CP-046).
	LookupByName(name string) (ControlPoint, bool)

	// LookupByTrigger returns all Hooks and Gates whose trigger matches trigger.
	//
	// The returned slice is sorted by Name ascending to satisfy CP-046
	// (deterministic list ordering). An empty slice (not nil) is returned when
	// no ControlPoints match.
	LookupByTrigger(trigger string) []ControlPoint

	// LookupByAttachPoint returns all Gates registered at attachPoint.
	//
	// The returned slice is sorted by declaration order (registration order)
	// per §4.1.CP-007, which requires Gates at the same attach point to be
	// invoked in declaration order. An empty slice (not nil) is returned when
	// no Gates are registered at attachPoint.
	LookupByAttachPoint(attachPoint AttachPoint) []ControlPoint

	// All returns every registered ControlPoint sorted by Name ascending.
	//
	// Used by the §7.1 post-registration audit to enumerate all registered
	// ControlPoints for invariant checking. An empty slice (not nil) is returned
	// when the registry is empty.
	All() []ControlPoint
}
