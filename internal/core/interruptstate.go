package core

import "fmt"

// InterruptState records whether an in-flight workspace has been externally
// interrupted.  The enum is defined in workspace-model.md §6.1.
//
// Default value is [InterruptStateNone] (no interruption).
//
// InterruptState is orthogonal to [WorkspaceState]: it applies only while a
// workspace is in an in-flight state, per WM-037 / WM-037a.
//
// Cross-references:
//   - Operator-control vocabulary maps to this enum per operator-nfr.md §4.3 ON-011.
//   - The reconciliation Cat 6 detector consults this field per reconciliation/spec.md §8.11.
//
// The enum is harmonik-owned and closed: unknown values are never tolerated.
type InterruptState string

// InterruptState constants per workspace-model.md §6.1.
const (
	// InterruptStateNone is the default: no interruption is in effect.
	InterruptStateNone InterruptState = "none"

	// InterruptStateOperatorPaused indicates the operator has paused the workspace.
	// Maps to the operator-control pause signal per operator-nfr.md §4.3 ON-011.
	InterruptStateOperatorPaused InterruptState = "operator-paused"

	// InterruptStateOperatorStoppedGraceful indicates the operator issued a graceful
	// stop signal.  Maps to the operator-control stop --graceful signal per
	// operator-nfr.md §4.3 ON-011.
	InterruptStateOperatorStoppedGraceful InterruptState = "operator-stopped-graceful"

	// InterruptStateOperatorStoppedImmediate indicates the operator issued an
	// immediate stop signal.  Maps to the operator-control stop --immediate signal
	// per operator-nfr.md §4.3 ON-011.
	InterruptStateOperatorStoppedImmediate InterruptState = "operator-stopped-immediate"

	// InterruptStateDaemonCrashSuspected indicates the daemon may have crashed
	// while the workspace was in-flight.  Detected by the reconciliation Cat 6
	// detector per reconciliation/spec.md §8.11.
	InterruptStateDaemonCrashSuspected InterruptState = "daemon-crash-suspected"
)

// Valid reports whether s is one of the five declared InterruptState constants.
// The enum is harmonik-owned and closed; unknown values are never valid.
func (s InterruptState) Valid() bool {
	switch s {
	case InterruptStateNone,
		InterruptStateOperatorPaused,
		InterruptStateOperatorStoppedGraceful,
		InterruptStateOperatorStoppedImmediate,
		InterruptStateDaemonCrashSuspected:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so InterruptState serialises
// correctly in JSON and YAML.
func (s InterruptState) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("interruptstate: unknown value %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the five declared constants.
func (s *InterruptState) UnmarshalText(text []byte) error {
	v := InterruptState(text)
	if !v.Valid() {
		return fmt.Errorf(
			"interruptstate: unknown value %q; must be one of none, operator-paused, operator-stopped-graceful, operator-stopped-immediate, daemon-crash-suspected",
			string(text),
		)
	}
	*s = v
	return nil
}
