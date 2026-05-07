package core

import "fmt"

// HarmonikWriteStatus is the five-value subset of Beads's CoarseStatus that
// harmonik MAY write via `br` terminal-transition writes (beads-integration.md §6.1, BI-007).
// Harmonik MUST NOT write any status outside this set.
type HarmonikWriteStatus string

// HarmonikWriteStatus values per beads-integration.md §6.1 ENUM declaration.
const (
	HarmonikWriteStatusOpen       HarmonikWriteStatus = "open"
	HarmonikWriteStatusInProgress HarmonikWriteStatus = "in_progress"
	HarmonikWriteStatusClosed     HarmonikWriteStatus = "closed"
	HarmonikWriteStatusDeferred   HarmonikWriteStatus = "deferred"
	HarmonikWriteStatusTombstone  HarmonikWriteStatus = "tombstone"
)

// Valid reports whether s is one of the five declared HarmonikWriteStatus constants.
func (s HarmonikWriteStatus) Valid() bool {
	switch s {
	case HarmonikWriteStatusOpen, HarmonikWriteStatusInProgress, HarmonikWriteStatusClosed, HarmonikWriteStatusDeferred, HarmonikWriteStatusTombstone:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so HarmonikWriteStatus serialises
// correctly in JSON and YAML workflow definitions.
func (s HarmonikWriteStatus) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("harmonikwritestatus: unknown value %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the five declared constants.
func (s *HarmonikWriteStatus) UnmarshalText(text []byte) error {
	v := HarmonikWriteStatus(text)
	if !v.Valid() {
		return fmt.Errorf("harmonikwritestatus: unknown value %q; must be one of open, in_progress, closed, deferred, tombstone", string(text))
	}
	*s = v
	return nil
}

// CoarseStatus returns the CoarseStatus value corresponding to s.
// Every HarmonikWriteStatus value is a member of CoarseStatus per BI-007 (the
// write surface is a strict subset of the read surface).
func (s HarmonikWriteStatus) CoarseStatus() CoarseStatus {
	return CoarseStatus(string(s))
}

// AsHarmonikWriteStatus returns the HarmonikWriteStatus equivalent of c if c is
// one of the five write-surface values, plus true; otherwise the zero value
// and false. Use this when narrowing a Beads-sourced read value to a value
// harmonik MAY write back per BI-007.
func (c CoarseStatus) AsHarmonikWriteStatus() (HarmonikWriteStatus, bool) {
	w := HarmonikWriteStatus(string(c))
	if !w.Valid() {
		return "", false
	}
	return w, true
}
