package core

import "fmt"

// GateResolutionSignal is the typed enum for the three kinds of signal that
// resolve a run's gate-pending sub-state (execution-model.md §4.10.EM-042a;
// control-points.md §6.2).
//
// When a Gate evaluator returns [GateActionDeny], the run enters the
// gate-pending sub-state of `running`. The daemon MUST wait for exactly one
// of these three resolution signals before re-evaluating the cascade:
//
//   - [GateResolutionSignalContextChange]: a policy-driven context change has
//     been applied to the run (the gate's decision may be context-dependent).
//   - [GateResolutionSignalOperatorOverride]: an operator has explicitly
//     commanded the daemon to re-evaluate or bypass the gate.
//   - [GateResolutionSignalTimeout]: the gate's per-policy timeout has elapsed;
//     if the gate still denies, the run fails with class `structural` per §8.2.
//
// A reader observing an unknown GateResolutionSignal MUST reject the enclosing
// GatePendingRecord; no silent fallback is permitted.
type GateResolutionSignal string

// GateResolutionSignal values per execution-model.md §4.10.EM-042a and
// control-points.md §6.2.
const (
	// GateResolutionSignalContextChange indicates that a policy-driven context
	// update has been applied to the run since the gate denied. The daemon MUST
	// re-evaluate the cascade; the gate may now permit given the updated context.
	GateResolutionSignalContextChange GateResolutionSignal = "context-change"

	// GateResolutionSignalOperatorOverride indicates that an operator has
	// explicitly overridden the gate denial. The daemon MUST re-evaluate the
	// cascade per the override's declared policy.
	GateResolutionSignalOperatorOverride GateResolutionSignal = "operator-override"

	// GateResolutionSignalTimeout indicates that the gate's per-policy timeout
	// has elapsed. On re-evaluation, if the gate still denies, the run MUST
	// fail with failure class `structural` per execution-model.md §8.2.
	GateResolutionSignalTimeout GateResolutionSignal = "timeout"
)

// Valid reports whether s is one of the three declared GateResolutionSignal
// constants. Unknown values are not tolerated.
func (s GateResolutionSignal) Valid() bool {
	switch s {
	case GateResolutionSignalContextChange, GateResolutionSignalOperatorOverride, GateResolutionSignalTimeout:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so GateResolutionSignal
// serialises correctly in JSON and YAML documents.
// It rejects any value that is not one of the three declared constants.
func (s GateResolutionSignal) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("gateresolutionsignal: unknown value %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the three declared constants.
// Callers MUST NOT silently degrade to a default signal kind.
func (s *GateResolutionSignal) UnmarshalText(text []byte) error {
	v := GateResolutionSignal(text)
	if !v.Valid() {
		return fmt.Errorf(
			"gateresolutionsignal: unknown value %q; must be one of context-change, operator-override, timeout",
			string(text),
		)
	}
	*s = v
	return nil
}
