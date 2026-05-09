package core

import "fmt"

// StaleDivergenceReason classifies which store advanced since the snapshot,
// causing the staleness check to fail (specs/reconciliation/schemas.md §6.1
// ENUM StaleDivergenceReason). Consumed by StaleVerdictPayload (RC-024).
//
// A reader observing an unknown StaleDivergenceReason MUST reject the
// enclosing StaleVerdictPayload; no silent fallback is permitted.
type StaleDivergenceReason string

// StaleDivergenceReason values per specs/reconciliation/schemas.md §6.1
// ENUM StaleDivergenceReason.
const (
	// StaleDivergenceReasonGitBranchAdvanced indicates the target run's task
	// branch has a new commit since the snapshot was captured.
	StaleDivergenceReasonGitBranchAdvanced StaleDivergenceReason = "git-branch-advanced"

	// StaleDivergenceReasonBeadsAuditAdvanced indicates the target bead's
	// Beads audit entries have advanced since the snapshot was captured.
	StaleDivergenceReasonBeadsAuditAdvanced StaleDivergenceReason = "beads-audit-advanced"
)

// Valid reports whether r is one of the two declared StaleDivergenceReason
// constants. Unknown values are NOT tolerated — a reader observing an unknown
// StaleDivergenceReason MUST reject the enclosing record per
// specs/reconciliation/schemas.md §6.1.
func (r StaleDivergenceReason) Valid() bool {
	switch r {
	case StaleDivergenceReasonGitBranchAdvanced, StaleDivergenceReasonBeadsAuditAdvanced:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so StaleDivergenceReason
// serialises correctly in JSON and YAML documents
// (specs/reconciliation/schemas.md §6.1). It rejects any value that is not
// one of the two declared constants.
func (r StaleDivergenceReason) MarshalText() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("staledivergencereason: unknown value %q", string(r))
	}
	return []byte(r), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the two declared constants.
// Per specs/reconciliation/schemas.md §6.1, unknown StaleDivergenceReason
// values must be rejected; callers MUST NOT silently degrade to a default.
func (r *StaleDivergenceReason) UnmarshalText(text []byte) error {
	v := StaleDivergenceReason(text)
	if !v.Valid() {
		return fmt.Errorf(
			"staledivergencereason: unknown value %q; must be one of git-branch-advanced, beads-audit-advanced",
			string(text),
		)
	}
	*r = v
	return nil
}
