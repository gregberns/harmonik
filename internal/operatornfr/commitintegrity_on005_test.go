package operatornfr_test

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// TestON005_CommitHashIntegrityCheck_Match verifies that the production gate
// passes when the binary stamp matches the operator-supplied hash.
//
// Spec ref: operator-nfr.md §4.2 ON-005 — "verify … source-commit hash
// against an operator-supplied expected hash … check MUST fail-closed."
func TestON005_CommitHashIntegrityCheck_Match(t *testing.T) {
	t.Parallel()

	const hash = "abc123def456abc123def456abc123def456abc123"
	result, err := operatornfr.CommitHashIntegrityCheck(hash, hash)
	if err != nil {
		t.Fatalf("ON-005 match: unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Errorf("ON-005 match: Allowed = false, want true (FailureMode=%q)", result.FailureMode)
	}
	if result.FailureMode != "" {
		t.Errorf("ON-005 match: FailureMode = %q, want empty", result.FailureMode)
	}
	if result.ExitCode != 0 {
		t.Errorf("ON-005 match: ExitCode = %d, want 0", result.ExitCode)
	}
}

// TestON005_CommitHashIntegrityCheck_Mismatch verifies that the production
// gate rejects when hashes differ and returns ErrIntegrityGateRejected.
//
// Spec ref: operator-nfr.md §4.2 ON-005 — "on mismatch … MUST remain in the
// `paused` state with an `operator_upgrade_rejected` event emitted"; §8 code 14.
func TestON005_CommitHashIntegrityCheck_Mismatch(t *testing.T) {
	t.Parallel()

	result, err := operatornfr.CommitHashIntegrityCheck(
		"abc123def456abc123def456abc123def456abc123",
		"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	)
	if err == nil {
		t.Fatal("ON-005 mismatch: expected error, got nil")
	}
	if !errors.Is(err, operatornfr.ErrIntegrityGateRejected) {
		t.Errorf("ON-005 mismatch: error %v is not ErrIntegrityGateRejected", err)
	}
	if result.Allowed {
		t.Error("ON-005 mismatch: Allowed = true, want false")
	}
	if result.FailureMode != "hash-mismatch" {
		t.Errorf("ON-005 mismatch: FailureMode = %q, want %q", result.FailureMode, "hash-mismatch")
	}
	if result.ExitCode != 14 {
		t.Errorf("ON-005 mismatch: ExitCode = %d, want 14", result.ExitCode)
	}
}

// TestON005a_CommitHashIntegrityCheck_StampMissing verifies that the production
// gate rejects with failure_mode="binary-stamp-missing" when actualHash is empty.
//
// Spec ref: operator-nfr.md §4.2 ON-005a — "Binaries lacking the embedded
// stamp MUST fail the integrity gate immediately on `harmonik upgrade`
// invocation with §8 code 14 (`upgrade-hash-mismatch`); the failure mode
// `failure_mode=binary-stamp-missing` distinguishes this case."
func TestON005a_CommitHashIntegrityCheck_StampMissing(t *testing.T) {
	t.Parallel()

	result, err := operatornfr.CommitHashIntegrityCheck(
		"", // no ldflags stamp
		"abc123def456abc123def456abc123def456abc123",
	)
	if err == nil {
		t.Fatal("ON-005a stamp-missing: expected error, got nil")
	}
	if !errors.Is(err, operatornfr.ErrIntegrityGateRejected) {
		t.Errorf("ON-005a stamp-missing: error %v is not ErrIntegrityGateRejected", err)
	}
	if result.Allowed {
		t.Error("ON-005a stamp-missing: Allowed = true, want false")
	}
	if result.FailureMode != "binary-stamp-missing" {
		t.Errorf("ON-005a stamp-missing: FailureMode = %q, want %q", result.FailureMode, "binary-stamp-missing")
	}
	if result.ExitCode != 14 {
		t.Errorf("ON-005a stamp-missing: ExitCode = %d, want 14", result.ExitCode)
	}
}

// TestON005a_CommitHashIntegrityCheck_StampMissingEmptyExpected verifies that
// an empty actual hash rejects even when expected is also empty (ON-005a
// priority: stamp-missing gate fires before hash comparison).
//
// Spec ref: operator-nfr.md §4.2 ON-005a.
func TestON005a_CommitHashIntegrityCheck_StampMissingEmptyExpected(t *testing.T) {
	t.Parallel()

	result, err := operatornfr.CommitHashIntegrityCheck("", "")
	if err == nil {
		t.Fatal("ON-005a stamp-missing-empty-expected: expected error, got nil")
	}
	if result.FailureMode != "binary-stamp-missing" {
		t.Errorf("ON-005a stamp-missing-empty-expected: FailureMode = %q, want %q", result.FailureMode, "binary-stamp-missing")
	}
	if result.ExitCode != 14 {
		t.Errorf("ON-005a stamp-missing-empty-expected: ExitCode = %d, want 14", result.ExitCode)
	}
}

// TestON005_CommitHashIntegrityCheck_FailureModeDistinct verifies that
// binary-stamp-missing and hash-mismatch are textually distinct failure modes.
//
// Spec ref: operator-nfr.md §4.2 ON-005a — "the failure mode
// `failure_mode=binary-stamp-missing` distinguishes this case from
// operator-supplied-hash-mismatch."
func TestON005_CommitHashIntegrityCheck_FailureModeDistinct(t *testing.T) {
	t.Parallel()

	stampMissing, _ := operatornfr.CommitHashIntegrityCheck("", "some-hash")
	hashMismatch, _ := operatornfr.CommitHashIntegrityCheck("actual", "expected")

	if stampMissing.FailureMode == hashMismatch.FailureMode {
		t.Errorf("ON-005a: stamp-missing mode %q and hash-mismatch mode %q are identical; MUST be distinct",
			stampMissing.FailureMode, hashMismatch.FailureMode)
	}
}

// TestON005_CommitHashIntegrityCheck_ExitCode14InTaxonomy verifies that §8
// exit code 14 ("upgrade-hash-mismatch") is in the taxonomy, as required by
// the gate's ExitCode field.
//
// Spec ref: operator-nfr.md §8 — "code 14 … upgrade-hash-mismatch."
func TestON005_CommitHashIntegrityCheck_ExitCode14InTaxonomy(t *testing.T) {
	t.Parallel()

	e, ok := operatornfr.LookupExitCode(14)
	if !ok {
		t.Fatal("ON-005: §8 taxonomy missing code 14 (upgrade-hash-mismatch)")
	}
	if e.Category != "upgrade-hash-mismatch" {
		t.Errorf("ON-005: §8 code 14 category = %q, want %q", e.Category, "upgrade-hash-mismatch")
	}
	if e.Event != "operator_upgrade_rejected" {
		t.Errorf("ON-005: §8 code 14 event = %q, want %q", e.Event, "operator_upgrade_rejected")
	}
}

// TestON005_CommitHashIntegrityCheck_FailClosedOnMismatch verifies that the
// returned ExitCode (14) resolves to a §8 taxonomy entry with the expected
// emitted event, confirming the fail-closed property at the taxonomy level.
//
// Spec ref: operator-nfr.md §4.2 ON-005 — "check MUST fail-closed."
func TestON005_CommitHashIntegrityCheck_FailClosedOnMismatch(t *testing.T) {
	t.Parallel()

	result, _ := operatornfr.CommitHashIntegrityCheck("actual", "expected")
	if result.Allowed {
		t.Fatal("ON-005 fail-closed: gate allowed mismatch; cannot verify exit-code taxonomy")
	}

	e, ok := operatornfr.LookupExitCode(result.ExitCode)
	if !ok {
		t.Fatalf("ON-005 fail-closed: §8 taxonomy missing code %d from gate result", result.ExitCode)
	}
	if e.Event != "operator_upgrade_rejected" {
		t.Errorf("ON-005 fail-closed: §8 code %d event = %q, want %q", result.ExitCode, e.Event, "operator_upgrade_rejected")
	}
}
