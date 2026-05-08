package core

import "fmt"

// DaemonStatus is the daemon-status enum consumed by the daemon-status event
// ([event-model.md §8.7.2]) and by `harmonik status`.  The full
// operator-control state machine is owned by [operator-nfr.md §4.3]; this
// type covers the seven declared statuses across both the
// `starting → reconciling → ready` prefix (owned by this spec) and the
// operator-control suffix (`paused`, `draining`, `stopped`).
//
// The harmonik-side type is closed: Valid() returns false for unknown values
// and MarshalText/UnmarshalText reject them.  Wire consumers that must
// tolerate unknown statuses per [operator-nfr.md §4.5 ON-018] N-1
// compatibility MUST check Valid() after assignment or use a tolerant decode
// path at the call-site; the type itself does not widen to accept future
// values.
//
// Post-`ready` degradation (RTO breach, health aggregation failures,
// silent-hang fan-out) is emitted via `daemon_degraded` but does NOT
// correspond to a transition in this enum (OQ-PL-009).
type DaemonStatus string

// Daemon-status constants per process-lifecycle.md §6.1.
const (
	// DaemonStatusStarting: pidfile locked; orphan sweep not yet complete.
	DaemonStatusStarting DaemonStatus = "starting"

	// DaemonStatusReconciling: Cat 0 passed; reconciliation dispatch in progress.
	DaemonStatusReconciling DaemonStatus = "reconciling"

	// DaemonStatusDegraded: Cat 0 prerequisite failing; classification halted;
	// pre-ready only (see §PL-010).
	DaemonStatusDegraded DaemonStatus = "degraded"

	// DaemonStatusReady: §PL-009 criteria met; normal dispatch active.
	DaemonStatusReady DaemonStatus = "ready"

	// DaemonStatusPaused: operator-initiated pause; in-flight runs drained
	// per [operator-nfr.md §4.3].
	DaemonStatusPaused DaemonStatus = "paused"

	// DaemonStatusDraining: graceful-shutdown sequence active per §PL-011.
	DaemonStatusDraining DaemonStatus = "draining"

	// DaemonStatusStopped: terminal; pidfile released per [operator-nfr.md §4.3].
	DaemonStatusStopped DaemonStatus = "stopped"
)

// Valid reports whether s is one of the seven declared DaemonStatus constants.
// The daemon-status enum is harmonik-owned and closed; unknown values are
// never valid.
func (s DaemonStatus) Valid() bool {
	switch s {
	case DaemonStatusStarting, DaemonStatusReconciling, DaemonStatusDegraded,
		DaemonStatusReady, DaemonStatusPaused, DaemonStatusDraining, DaemonStatusStopped:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so DaemonStatus serialises
// correctly in JSON and YAML.
func (s DaemonStatus) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("daemonstatus: unknown value %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the seven declared constants.
// Wire consumers that must tolerate unknown statuses per ON-018 N-1
// compatibility MUST check Valid() after assignment rather than relying on
// this method to accept future values.
func (s *DaemonStatus) UnmarshalText(text []byte) error {
	v := DaemonStatus(text)
	if !v.Valid() {
		return fmt.Errorf(
			"daemonstatus: unknown value %q; must be one of starting, reconciling, degraded, ready, paused, draining, stopped",
			string(text),
		)
	}
	*s = v
	return nil
}
