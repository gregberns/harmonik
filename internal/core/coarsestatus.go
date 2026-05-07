package core

import "fmt"

// CoarseStatus is the read-surface status enum owned by Beads (beads-integration.md §6.1).
// Harmonik MUST tolerate any value Beads exposes, including future extensions beyond the
// eight values live at Beads v0.1.45.  Harmonik's WRITE surface is a strict subset;
// see beads-integration.md §6.1 HarmonikWriteStatus.
type CoarseStatus string

// CoarseStatus values per beads-integration.md §6.1 ENUM CoarseStatus (Beads v0.1.45).
const (
	CoarseStatusOpen       CoarseStatus = "open"
	CoarseStatusInProgress CoarseStatus = "in_progress"
	CoarseStatusBlocked    CoarseStatus = "blocked"
	CoarseStatusDeferred   CoarseStatus = "deferred"
	CoarseStatusDraft      CoarseStatus = "draft"
	CoarseStatusClosed     CoarseStatus = "closed"
	CoarseStatusTombstone  CoarseStatus = "tombstone"
	CoarseStatusPinned     CoarseStatus = "pinned"
)

// Valid reports whether s is one of the eight declared CoarseStatus constants at Beads v0.1.45.
// Future Beads extensions are NOT rejected here; callers that need pass-through behaviour
// for unknown values should check Valid() and treat false as an acceptable unknown rather
// than an error.
func (s CoarseStatus) Valid() bool {
	switch s {
	case CoarseStatusOpen, CoarseStatusInProgress, CoarseStatusBlocked,
		CoarseStatusDeferred, CoarseStatusDraft, CoarseStatusClosed,
		CoarseStatusTombstone, CoarseStatusPinned:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so CoarseStatus serialises
// correctly in JSON and YAML.
func (s CoarseStatus) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("coarsestatus: unknown value %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the eight declared constants at Beads v0.1.45.
// Per beads-integration.md §4.5 BI-013/BI-014, harmonik must tolerate future extensions;
// callers handling forward-compatibility MUST accept unknown values separately.
func (s *CoarseStatus) UnmarshalText(text []byte) error {
	v := CoarseStatus(text)
	if !v.Valid() {
		return fmt.Errorf(
			"coarsestatus: unknown value %q; must be one of open, in_progress, blocked, deferred, draft, closed, tombstone, pinned",
			string(text),
		)
	}
	*s = v
	return nil
}
