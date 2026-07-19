package brcli

import (
	"bytes"
	"fmt"
)

// BrError is the closed-set adapter error taxonomy for harmonik's Beads CLI
// adapter. Every `br` invocation result MUST be classified into one of these
// values per BI-025a.
//
// The enum is harmonik-owned and closed: exactly the values listed in
// specs/beads-integration.md §6.1a. No speculative additions are permitted;
// extensions require a spec amendment.
//
// Spec ref: specs/beads-integration.md §6.1a.
type BrError string

// BrError values per specs/beads-integration.md §6.1a.
const (
	// BrOK indicates a successful br invocation (exit code 0).
	BrOK BrError = "OK"

	// BrNotFound indicates the target bead-id was not found (exit code 1).
	// Routes to reconciliation Cat 3 (generic) per BI §8: Beads-vs-harmonik
	// divergence; investigator dispatch.
	BrNotFound BrError = "NotFound"

	// BrConflict indicates a concurrent claim or status collision (exit code 2).
	// Routes to reconciliation Cat 3a (torn-write) per BI §8: concurrent-claim
	// race; idempotency recovery per BI §4.10.
	BrConflict BrError = "Conflict"

	// BrDbLocked indicates SQLite is busy beyond its internal timeout (exit code 3).
	// Routes to reconciliation Cat 0 (infrastructure) per BI §8: bounded retry;
	// if persistent → daemon exit code 8.
	BrDbLocked BrError = "DbLocked"

	// BrSchemaMismatch indicates the Beads schema or output format is outside
	// harmonik's compatibility window (exit code 4).
	// Routes to reconciliation Cat 0 → daemon startup failure per BI §8: exit
	// code 8 (beads-unavailable); operator must align harmonik release with
	// Beads version.
	BrSchemaMismatch BrError = "SchemaMismatch"

	// BrUnavailable indicates br is not reachable: subprocess wall-clock timeout
	// per BI-025c, or br not on PATH / fork failure (exec error).
	// Routes to reconciliation Cat 0 (infrastructure) per BI §8: bounded retry
	// per PL-010 cadence.
	BrUnavailable BrError = "Unavailable"

	// BrOther indicates an unrecognized exit code that cannot be authoritatively
	// classified. The adapter MUST emit store_divergence_detected per BI-025a
	// with reason=authority_unavailable when this value is produced.
	// Routes to reconciliation Cat 3 (generic) per BI §8: divergence-detected;
	// investigator dispatch.
	BrOther BrError = "Other"
)

// Valid reports whether e is one of the seven declared BrError constants.
// The BrError taxonomy is harmonik-owned and closed; unknown values are never valid.
func (e BrError) Valid() bool {
	switch e {
	case BrOK, BrNotFound, BrConflict, BrDbLocked, BrSchemaMismatch, BrUnavailable, BrOther:
		return true
	default:
		return false
	}
}

// String implements fmt.Stringer.
func (e BrError) String() string { return string(e) }

// Error implements the error interface so BrError constants can be used as
// sentinel errors in errors.Is chains (e.g. errors.Is(err, BrUnavailable)).
func (e BrError) Error() string { return "brcli: " + string(e) }

// MarshalText implements encoding.TextMarshaler so BrError serialises
// correctly in JSON and YAML.
// It rejects any value that is not one of the seven declared constants.
func (e BrError) MarshalText() ([]byte, error) {
	if !e.Valid() {
		return nil, fmt.Errorf("brerror: unknown value %q", string(e))
	}
	return []byte(e), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the seven declared constants.
func (e *BrError) UnmarshalText(text []byte) error {
	v := BrError(text)
	if !v.Valid() {
		return fmt.Errorf(
			"brerror: unknown value %q; must be one of OK, NotFound, Conflict, DbLocked, SchemaMismatch, Unavailable, Other",
			string(text),
		)
	}
	*e = v
	return nil
}

// BrErrorFromExitCode maps a br subprocess exit code to a BrError value per
// the illustrative table in specs/beads-integration.md §6.1a.
//
// The table is illustrative; the pinned Beads version's exit-code surface is
// the authoritative source per BI-025a. This function MUST be re-validated
// whenever BI-024's pinned version changes.
//
// Mapping:
//
//	0 → BrOK           (success)
//	1 → BrNotFound     (bead-id not found)
//	2 → BrConflict     (concurrent claim or status collision)
//	3 → BrDbLocked     (SQLite busy beyond timeout)
//	4 → BrSchemaMismatch (schema or output format outside compatibility window)
//	other → BrOther    (unrecognized; caller MUST emit store_divergence_detected per BI-025a)
//
// Timeout and exec errors (BrUnavailable) are NOT classifiable by exit code
// alone; callers observing a subprocess that was terminated by SIGTERM/SIGKILL
// or that failed to launch MUST classify directly as BrUnavailable without
// invoking this function.
//
// Spec ref: specs/beads-integration.md §6.1a, §4.8a BI-025a.
func BrErrorFromExitCode(code int) BrError {
	switch code {
	case 0:
		return BrOK
	case 1:
		return BrNotFound
	case 2:
		return BrConflict
	case 3:
		return BrDbLocked
	case 4:
		return BrSchemaMismatch
	default:
		return BrOther
	}
}

// BrErrorFromExit classifies a br subprocess outcome using both its exit code
// and captured stderr. It is the stderr-aware refinement of the illustrative
// §6.1a exit-code table and is the classifier that adapter call sites MUST use;
// BrErrorFromExitCode remains the pure code→BrError table (spec §6.1a) and is
// retained for callers that legitimately have only the exit code.
//
// The refinement fixes a defect where a *generic* br exit-1 failure (any
// non-zero-arg error br surfaces on exit 1 that is NOT a missing bead-id) was
// unconditionally classified as BrNotFound, producing a spurious
// Beads-vs-harmonik divergence signal (reconciliation Cat 3, investigator
// dispatch per BI §8). Exit code 1 is a generic failure code in the pinned
// Beads CLI; only when br's stderr genuinely indicates a not-found condition is
// the outcome authoritatively a missing bead-id.
//
// Refinement rule (exit 1 only; all other codes defer to the table):
//
//	empty stderr            → BrNotFound  (BI-025d empty-stderr rule: classify per the table)
//	stderr says "not found" → BrNotFound  (genuine missing bead-id)
//	stderr present, other   → BrOther     (generic failure; NOT a divergence)
//
// Classifying an unrecognized exit-1 failure as BrOther is the intended
// outcome: BrOther callers emit divergence_inconclusive with
// reason=authority_unavailable (BI-025a), signalling "br could not be
// authoritatively classified" rather than the false-positive "the bead is
// missing." Using stderr content to *classify an error* (never to derive bead
// state) follows the BI-025d precedent that already routes the clap argparse
// exit and the Rust-panic exit to BrOther by inspecting stderr.
//
// Spec ref: specs/beads-integration.md §6.1a, §4.8d BI-025d.
func BrErrorFromExit(code int, stderr []byte) BrError {
	base := BrErrorFromExitCode(code)
	if code == 1 && len(bytes.TrimSpace(stderr)) > 0 && !stderrIndicatesNotFound(stderr) {
		return BrOther
	}
	return base
}

// stderrIndicatesNotFound reports whether captured br stderr genuinely signals a
// missing bead-id (as opposed to any other exit-1 failure). It matches the
// case-insensitive substring "not found", which the pinned Beads CLI emits for
// missing-id errors (e.g. "Error: Issue not found: <id>"). Matching is
// deliberately conservative: a false negative degrades to BrOther (a valid,
// investigator-dispatched classification), never to a wrong terminal state.
func stderrIndicatesNotFound(stderr []byte) bool {
	return bytes.Contains(bytes.ToLower(stderr), []byte("not found"))
}
