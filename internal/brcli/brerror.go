package brcli

import "fmt"

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
