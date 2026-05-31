package core

// s02_hka8bg45.go — S02 (Policy Engine) ownership of the ControlPoint registry
//
// Implements specs/control-points.md §4.9.CP-043:
//
//	"The ControlPoint registry MUST be a single in-process table (Go map keyed
//	 by `name`) owned by S02 (Policy Engine). S02 constructs ControlPoint
//	 instances by reading policy YAML per §4.7 and calling the registration
//	 surface defined in §4.9.CP-044. The registry is the single source of truth
//	 for registered ControlPoints within a daemon."
//
// # Design
//
// S02PolicyEngine is the concrete implementation of PolicyEngine for the
// post-MVH CP subsystem. It wraps MapRegistry (the in-process table per
// CP-043) and exposes it via the Registry() accessor declared in PolicyEngine.
//
// At MVH, the composition root wires NoOpPolicyEngine (zero-registry, always
// Permitted). Post-MVH, the composition root substitutes S02PolicyEngine;
// callers hold a PolicyEngine interface and require no changes.
//
// Ownership: S02PolicyEngine holds the authoritative *MapRegistry. No other
// subsystem may hold a writable reference to the registry after daemon init
// (CP-047). Other subsystems (S01, S05) access the registry read-only through
// the Registry() interface.
//
// Registration sequence: daemon startup calls RegisterControlPoints to run the
// §7.1 two-pass sequence. During the main loop the registry is read-only.
// Daemon restart rebuilds from policy YAML (CP-045; no cross-restart
// persistence).
//
// Tags: mechanism
//
// Refs: hk-a8bg.45

// S02PolicyEngine is the concrete PolicyEngine owned by the S02 (Policy Engine)
// subsystem. It holds the authoritative ControlPoint registry and implements
// the evaluation surface used by the EM dispatcher.
//
// Construct via NewS02PolicyEngine. Zero-value is invalid.
//
// Spec ref: specs/control-points.md §4.9.CP-043, §4.10.CP-047.
type S02PolicyEngine struct {
	// registry is the single in-process table of registered ControlPoints,
	// keyed by name (CP-043). It is populated during daemon init and treated as
	// read-only during the main loop.
	registry *MapRegistry
}

// NewS02PolicyEngine constructs an S02PolicyEngine with an empty registry.
//
// Callers MUST invoke RegisterControlPoints during daemon startup to populate
// the registry from policy YAML per §7.1 before the EM dispatcher starts
// serving gate and guard evaluations.
func NewS02PolicyEngine() *S02PolicyEngine {
	return &S02PolicyEngine{
		registry: NewMapRegistry(),
	}
}

// Evaluate implements PolicyEngine.
//
// At the current implementation stage (post-MVH stub), Evaluate always returns
// {Permitted: true, Constraints: nil} — identical to NoOpPolicyEngine. This
// preserves the dispatcher-interface invariant (SH-018) while the full
// expression-evaluation surface (§6.4, §7.2) is being built out.
//
// TODO(hk-a8bg): replace with real expression evaluation once §6.4 + §7.2
// are implemented. The PolicyEvalContext must be widened to the full §6.4
// environment before that landing.
//
// Spec ref: specs/control-points.md §4.2 (Gate), §4.4 (Guard), §6.4;
// specs/scenario-harness.md §4.3.SH-018.
func (s *S02PolicyEngine) Evaluate(_ PolicyEvalContext) PolicyVerdict {
	return PolicyVerdict{Permitted: true, Constraints: nil}
}

// Registry implements PolicyEngine. It returns the in-process ControlPoint
// registry owned by S02.
//
// The returned Registry is the single source of truth for registered
// ControlPoints within this daemon (CP-043). Callers MUST treat it as
// read-only after daemon init; all mutations are performed by S02 itself
// during RegisterControlPoints (§7.1).
//
// Spec ref: specs/control-points.md §4.9.CP-043, §4.10.CP-047, §6.1.7.
func (s *S02PolicyEngine) Registry() Registry {
	return s.registry
}

// RegisterControlPoints runs the §7.1 two-pass ControlPoint registration
// sequence using a pre-parsed set of ControlPoints.
//
// This method is the integration point between policy YAML loading (§4.7) and
// the registry (CP-043/CP-044). CP-035 section validation is enforced by
// S02Registrar.RegisterFromDocument; callers here pass already-constructed
// ControlPoints.
//
// Registration rules enforced (delegated to MapRegistry.Register):
//   - CP-001: structurally invalid ControlPoints fail registration.
//   - CP-020: cognition-tagged Guards fail registration.
//   - CP-044: re-registration with identical body succeeds silently; divergent
//     body under an existing name fails with ErrDivergentBody.
//
// Returns the first error encountered; registration is aborted on error
// (partial-registration is non-atomic; callers should treat the registry as
// invalid on error and not proceed to daemon main loop).
//
// Spec ref: specs/control-points.md §7.1 (registration sequence);
// §4.9.CP-043, CP-044; §4.7.CP-035.
func (s *S02PolicyEngine) RegisterControlPoints(cps []ControlPoint) error {
	for _, cp := range cps {
		if err := s.registry.Register(cp); err != nil {
			return err
		}
	}
	return nil
}
