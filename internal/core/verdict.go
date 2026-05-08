package core

import "fmt"

// Verdict is the reconciliation investigator's decision enum
// (reconciliation/schemas.md §6.1 ENUM Verdict).
//
// The enum is closed at MVH; future variants extend via the amendment
// protocol per [architecture.md §4.6]. A reader observing an unknown Verdict
// MUST treat the VerdictEvent as malformed per RC-023 (MalformationReason
// unknown-verdict-value) rather than silently falling back to any default.
type Verdict string

// Verdict values per reconciliation/schemas.md §6.1 ENUM Verdict.
const (
	// VerdictResumeHere dispatches the current node with the same or a fresh
	// agent; no context change.
	VerdictResumeHere Verdict = "resume-here"

	// VerdictResumeWithContext dispatches the current node with
	// investigator-supplied context injected (RC-022a).
	VerdictResumeWithContext Verdict = "resume-with-context"

	// VerdictResetToCheckpoint performs an intra-run rollback to a named
	// earlier checkpoint; keeps worktree and run_id.
	VerdictResetToCheckpoint Verdict = "reset-to-checkpoint"

	// VerdictReopenBead marks the bead open; a subsequent claim produces a new
	// run with fresh worktree.
	VerdictReopenBead Verdict = "reopen-bead"

	// VerdictAcceptCloseWithNote closes legitimately; annotates the audit gap;
	// writes bead close if needed.
	VerdictAcceptCloseWithNote Verdict = "accept-close-with-note"

	// VerdictNoOpAccept confirms the current state is legitimate; no mechanical
	// action; run continues.
	VerdictNoOpAccept Verdict = "no-op-accept"

	// VerdictEscalateToHuman signals that the investigator cannot resolve;
	// pauses the affected run and emits an operator-observable event.
	VerdictEscalateToHuman Verdict = "escalate-to-human"
)

// Valid reports whether v is one of the seven declared Verdict constants.
// Unknown values are NOT tolerated — a reader observing an unknown Verdict MUST
// treat the VerdictEvent as malformed per RC-023 (unknown-verdict-value).
func (v Verdict) Valid() bool {
	switch v {
	case VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so Verdict serialises
// correctly in JSON and YAML.
// It rejects any value that is not one of the seven declared constants.
func (v Verdict) MarshalText() ([]byte, error) {
	if !v.Valid() {
		return nil, fmt.Errorf("verdict: unknown value %q", string(v))
	}
	return []byte(v), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the seven declared constants.
// A reader observing an unknown Verdict MUST treat the VerdictEvent as
// malformed per RC-023 (unknown-verdict-value).
func (v *Verdict) UnmarshalText(text []byte) error {
	val := Verdict(text)
	if !val.Valid() {
		return fmt.Errorf(
			"verdict: unknown value %q; must be one of resume-here, resume-with-context,"+
				" reset-to-checkpoint, reopen-bead, accept-close-with-note, no-op-accept, escalate-to-human",
			string(text),
		)
	}
	*v = val
	return nil
}
