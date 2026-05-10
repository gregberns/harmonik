package operatornfr

import "errors"

// CommitHash is the build-time embedded source-commit hash injected by the
// linker via:
//
//	-ldflags "-X github.com/gregberns/harmonik/internal/operatornfr.CommitHash=<sha>"
//
// When a harmonik binary is built without this ldflags stamp, CommitHash
// remains the zero string (""). The integrity gate (ON-005a) treats an empty
// CommitHash as "binary-stamp-missing" and MUST fail the upgrade immediately.
//
// Spec ref: operator-nfr.md §4.2 ON-005a — "The daemon's `actual_hash` MUST be
// computed from a build-time embedded ldflags stamp; binaries lacking the
// embedded stamp MUST fail the integrity gate immediately."
var CommitHash string //nolint:gochecknoglobals // required ldflags target; must be package-level var

// IntegrityCheckResult holds the outcome of a single commit-hash integrity gate
// evaluation per ON-005 / ON-005a.
//
// Spec ref: operator-nfr.md §4.2 ON-005 — "The check MUST fail-closed: on
// mismatch or missing hash, the daemon MUST NOT exec-replace and MUST remain in
// the `paused` state with an `operator_upgrade_rejected` event emitted."
type IntegrityCheckResult struct {
	// Allowed is true when the gate passes (hashes match and stamp is present).
	Allowed bool

	// FailureMode is populated on rejection. Two values are declared:
	//   - "binary-stamp-missing" — ActualHash was empty (ON-005a).
	//   - "hash-mismatch"        — ActualHash != ExpectedHash (ON-005).
	// Empty when Allowed is true.
	FailureMode string

	// ExitCode is the §8 exit code to return on rejection. Always 14
	// ("upgrade-hash-mismatch") for both failure modes. Zero when Allowed.
	ExitCode int
}

// ErrIntegrityGateRejected is returned by CommitHashIntegrityCheck when the
// gate rejects the upgrade. Callers that need the structured failure details
// MUST use IntegrityCheckResult instead; this sentinel is provided for callers
// that only need a go-error return.
var ErrIntegrityGateRejected = errors.New("operatornfr: commit-hash integrity gate rejected upgrade")

// CommitHashIntegrityCheck evaluates the ON-005 / ON-005a commit-hash
// integrity gate.
//
// Rules (in priority order):
//  1. ON-005a: if actualHash is empty (no ldflags stamp in binary), the gate
//     MUST reject with failure_mode="binary-stamp-missing" and §8 code 14.
//  2. ON-005: if actualHash != expectedHash, the gate MUST reject with
//     failure_mode="hash-mismatch" and §8 code 14.
//  3. Otherwise the gate passes and Allowed is true.
//
// The returned error is non-nil (ErrIntegrityGateRejected) on rejection. It
// is nil on success. The IntegrityCheckResult is always populated regardless of
// the error value.
//
// Usage in the upgrade path:
//
//	result, err := operatornfr.CommitHashIntegrityCheck(
//	    operatornfr.CommitHash,   // embedded ldflags stamp
//	    operatorSuppliedHash,
//	)
//	if err != nil {
//	    // emit operator_upgrade_rejected{failure_mode=result.FailureMode}
//	    // return result.ExitCode
//	}
//
// Spec ref: operator-nfr.md §4.2 ON-005 — "verify the to-be-installed binary's
// source-commit hash against an operator-supplied expected hash before the
// daemon's exec-replacement step."
//
// Spec ref: operator-nfr.md §4.2 ON-005a — "Binaries lacking the embedded
// stamp MUST fail the integrity gate immediately on `harmonik upgrade`
// invocation with §8 code 14 (`upgrade-hash-mismatch`); the failure mode
// `failure_mode=binary-stamp-missing` distinguishes this case."
func CommitHashIntegrityCheck(actualHash, expectedHash string) (IntegrityCheckResult, error) {
	// ON-005a: missing stamp always rejects before operator-hash comparison.
	if actualHash == "" {
		r := IntegrityCheckResult{
			Allowed:     false,
			FailureMode: "binary-stamp-missing",
			ExitCode:    14,
		}
		return r, ErrIntegrityGateRejected
	}

	// ON-005: compare hashes.
	if actualHash != expectedHash {
		r := IntegrityCheckResult{
			Allowed:     false,
			FailureMode: "hash-mismatch",
			ExitCode:    14,
		}
		return r, ErrIntegrityGateRejected
	}

	return IntegrityCheckResult{Allowed: true}, nil
}
