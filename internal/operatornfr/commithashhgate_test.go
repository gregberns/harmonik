package operatornfr_test

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// commitHashFixtureScenario models one upgrade scenario for the ON-005 /
// ON-005a commit-hash integrity gate tests.
//
// Spec ref: operator-nfr.md §4.2 ON-005 — "verify the to-be-installed binary's
// source-commit hash against an operator-supplied expected hash before the
// daemon's exec-replacement step. The check MUST fail-closed: on mismatch or
// missing hash, the daemon MUST NOT exec-replace and MUST remain in the `paused`
// state with an `operator_upgrade_rejected` event emitted."
type commitHashFixtureScenario struct {
	Name             string // human-readable name for t.Run labelling
	ActualHash       string // embedded build-time stamp (empty = no stamp)
	ExpectedHash     string // operator-supplied expected hash
	WantAllowed      bool   // true = gate MUST pass; false = gate MUST reject
	WantFailureMode  string // populated on rejection; "binary-stamp-missing" or "hash-mismatch"
	WantEmittedEvent string // event the gate emits on rejection
	WantExitCode     int    // §8 exit code on rejection (0 = success)
}

// commitHashFixtureScenarios is the fixture table for ON-005 and ON-005a
// commit-hash integrity gate scenarios.
//
// Spec ref: operator-nfr.md §4.2 ON-005, ON-005a:
//
//   - Matching hash → gate passes, upgrade proceeds.
//   - Mismatched hash → gate rejects, `operator_upgrade_rejected` emitted, §8
//     code 14 returned, daemon remains `paused`.
//   - Missing stamp → gate rejects with `failure_mode=binary-stamp-missing`,
//     §8 code 14 returned.
var commitHashFixtureScenarios = []commitHashFixtureScenario{
	{
		// ON-005 happy path: operator-supplied hash matches binary stamp.
		Name:         "matching-hash",
		ActualHash:   "abc123def456abc123def456abc123def456abc123",
		ExpectedHash: "abc123def456abc123def456abc123def456abc123",
		WantAllowed:  true,
	},
	{
		// ON-005 rejection path: operator-supplied hash does NOT match stamp.
		// Spec ref: operator-nfr.md §4.2 ON-005 — "on mismatch … MUST emit
		// `operator_upgrade_rejected`"; §8 code 14 `upgrade-hash-mismatch`.
		Name:             "mismatched-hash",
		ActualHash:       "abc123def456abc123def456abc123def456abc123",
		ExpectedHash:     "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		WantAllowed:      false,
		WantFailureMode:  "hash-mismatch",
		WantEmittedEvent: "operator_upgrade_rejected",
		WantExitCode:     14,
	},
	{
		// ON-005a: binary has no embedded ldflags stamp.
		// Spec ref: operator-nfr.md §4.2 ON-005a — "Binaries lacking the embedded
		// stamp MUST fail the integrity gate immediately … with §8 code 14; the
		// failure mode `failure_mode=binary-stamp-missing` distinguishes this case."
		Name:             "binary-stamp-missing",
		ActualHash:       "", // empty = no ldflags stamp embedded
		ExpectedHash:     "abc123def456abc123def456abc123def456abc123",
		WantAllowed:      false,
		WantFailureMode:  "binary-stamp-missing",
		WantEmittedEvent: "operator_upgrade_rejected",
		WantExitCode:     14,
	},
	{
		// ON-005a: both stamp and expected hash are empty — still rejected
		// (no stamp = gate rejects regardless of operator input).
		Name:             "binary-stamp-missing-empty-expected",
		ActualHash:       "",
		ExpectedHash:     "",
		WantAllowed:      false,
		WantFailureMode:  "binary-stamp-missing",
		WantEmittedEvent: "operator_upgrade_rejected",
		WantExitCode:     14,
	},
}

// commitHashFixtureEvaluate applies the commit-hash integrity gate logic
// modelled in specs/operator-nfr.md §4.2 ON-005 / ON-005a and returns
// (allowed, failureMode). This is the fixture-level gate simulation; production
// code lives in the as-yet-unimplemented daemon upgrade path.
func commitHashFixtureEvaluate(actualHash, expectedHash string) (allowed bool, failureMode string) {
	// ON-005a: missing stamp always rejects before operator-hash comparison.
	if actualHash == "" {
		return false, "binary-stamp-missing"
	}
	// ON-005: compare hashes.
	if actualHash != expectedHash {
		return false, "hash-mismatch"
	}
	return true, ""
}

// TestON005_CommitHashGate_MatchingHash verifies that the integrity gate passes
// when the binary's embedded stamp equals the operator-supplied hash.
//
// Spec ref: operator-nfr.md §4.2 ON-005.
func TestON005_CommitHashGate_MatchingHash(t *testing.T) {
	t.Parallel()

	sc := commitHashFixtureScenarios[0] // "matching-hash"
	allowed, failureMode := commitHashFixtureEvaluate(sc.ActualHash, sc.ExpectedHash)
	if !allowed {
		t.Errorf("ON-005: matching-hash scenario: gate rejected, failureMode=%q; MUST allow upgrade when hashes match", failureMode)
	}
	if failureMode != "" {
		t.Errorf("ON-005: matching-hash scenario: failureMode = %q, want empty (no failure on matching hash)", failureMode)
	}
}

// TestON005_CommitHashGate_MismatchedHash verifies that the integrity gate
// rejects when hashes differ and emits §8 code 14 with `operator_upgrade_rejected`.
//
// Spec ref: operator-nfr.md §4.2 ON-005 — "on mismatch, MUST remain in the
// `paused` state with an `operator_upgrade_rejected` event"; §8 code 14.
func TestON005_CommitHashGate_MismatchedHash(t *testing.T) {
	t.Parallel()

	sc := commitHashFixtureScenarios[1] // "mismatched-hash"
	allowed, failureMode := commitHashFixtureEvaluate(sc.ActualHash, sc.ExpectedHash)
	if allowed {
		t.Error("ON-005: mismatched-hash scenario: gate allowed upgrade; MUST reject on hash mismatch")
	}
	if failureMode != sc.WantFailureMode {
		t.Errorf("ON-005: mismatched-hash scenario: failureMode = %q, want %q", failureMode, sc.WantFailureMode)
	}

	// Verify that §8 code 14 is the taxonomy entry for this failure mode.
	e, ok := operatornfr.LookupExitCode(sc.WantExitCode)
	if !ok {
		t.Fatalf("ON-005: §8 taxonomy missing code %d (upgrade-hash-mismatch)", sc.WantExitCode)
	}
	if e.Category != "upgrade-hash-mismatch" {
		t.Errorf("ON-005: §8 code %d category = %q, want %q", sc.WantExitCode, e.Category, "upgrade-hash-mismatch")
	}
	if e.Event != sc.WantEmittedEvent {
		t.Errorf("ON-005: §8 code %d emitted_event = %q, want %q", sc.WantExitCode, e.Event, sc.WantEmittedEvent)
	}
}

// TestON005a_BinaryStampMissing verifies that the integrity gate rejects with
// `failure_mode=binary-stamp-missing` when the binary has no embedded ldflags
// stamp.
//
// Spec ref: operator-nfr.md §4.2 ON-005a — "Binaries lacking the embedded stamp
// MUST fail the integrity gate immediately on `harmonik upgrade` invocation with
// §8 code 14 (`upgrade-hash-mismatch`); the failure mode
// `failure_mode=binary-stamp-missing` distinguishes this case."
func TestON005a_BinaryStampMissing(t *testing.T) {
	t.Parallel()

	for _, sc := range commitHashFixtureScenarios {
		sc := sc
		if sc.ActualHash != "" {
			continue // only stamp-missing scenarios
		}
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()

			allowed, failureMode := commitHashFixtureEvaluate(sc.ActualHash, sc.ExpectedHash)
			if allowed {
				t.Errorf("ON-005a: %s: gate allowed upgrade for binary with no stamp; MUST reject", sc.Name)
			}
			if failureMode != "binary-stamp-missing" {
				t.Errorf("ON-005a: %s: failureMode = %q, want %q", sc.Name, failureMode, "binary-stamp-missing")
			}

			// The rejection exit code is still §8 code 14 (upgrade-hash-mismatch)
			// even for the stamp-missing sub-case.
			e, ok := operatornfr.LookupExitCode(14)
			if !ok {
				t.Fatal("ON-005a: §8 taxonomy missing code 14 (upgrade-hash-mismatch)")
			}
			if e.Category != "upgrade-hash-mismatch" {
				t.Errorf("ON-005a: §8 code 14 category = %q, want %q", e.Category, "upgrade-hash-mismatch")
			}
		})
	}
}

// TestON005a_StampMissingDistinguishedFromOperatorMismatch verifies that the
// stamp-missing failure mode is textually distinct from operator-supplied-hash
// mismatch, per ON-005a's "distinguishes this case" clause.
//
// Spec ref: operator-nfr.md §4.2 ON-005a.
func TestON005a_StampMissingDistinguishedFromOperatorMismatch(t *testing.T) {
	t.Parallel()

	_, stampMissingMode := commitHashFixtureEvaluate("", "abc123")
	_, hashMismatchMode := commitHashFixtureEvaluate("actual_hash_value", "expected_hash_value")

	if stampMissingMode == hashMismatchMode {
		t.Errorf("ON-005a: stamp-missing failureMode %q and hash-mismatch failureMode %q are identical; they MUST be distinct", stampMissingMode, hashMismatchMode)
	}
	if stampMissingMode != "binary-stamp-missing" {
		t.Errorf("ON-005a: stamp-missing failureMode = %q, want %q", stampMissingMode, "binary-stamp-missing")
	}
	if hashMismatchMode != "hash-mismatch" {
		t.Errorf("ON-005a: hash-mismatch failureMode = %q, want %q", hashMismatchMode, "hash-mismatch")
	}
}

// TestON005_AllScenarios is a table-driven test that runs every scenario in
// commitHashFixtureScenarios and asserts gate outcome, failure mode, and §8
// taxonomy alignment.
//
// Spec ref: operator-nfr.md §4.2 ON-005, ON-005a; §10.2 — "Upgrade scenario
// tests with matching and mismatched commit hashes; verify `operator_upgrade_rejected`
// on mismatch with §8 code 14."
func TestON005_AllScenarios(t *testing.T) {
	t.Parallel()

	for _, sc := range commitHashFixtureScenarios {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()

			allowed, failureMode := commitHashFixtureEvaluate(sc.ActualHash, sc.ExpectedHash)

			if allowed != sc.WantAllowed {
				t.Errorf("ON-005: scenario %q: allowed = %v, want %v", sc.Name, allowed, sc.WantAllowed)
			}
			if failureMode != sc.WantFailureMode {
				t.Errorf("ON-005: scenario %q: failureMode = %q, want %q", sc.Name, failureMode, sc.WantFailureMode)
			}

			// On rejection, the §8 taxonomy MUST have an entry for the expected exit code.
			if !sc.WantAllowed && sc.WantExitCode != 0 {
				e, ok := operatornfr.LookupExitCode(sc.WantExitCode)
				if !ok {
					t.Errorf("ON-005: scenario %q: §8 taxonomy missing code %d", sc.Name, sc.WantExitCode)
					return
				}
				if e.Event != sc.WantEmittedEvent {
					t.Errorf("ON-005: scenario %q: §8 code %d emitted_event = %q, want %q", sc.Name, sc.WantExitCode, e.Event, sc.WantEmittedEvent)
				}
			}
		})
	}
}

// TestON006_PostMVHSigningIsAdditive verifies that ON-006's deferral of full
// binary signing does not invalidate the commit-hash gate contract. Signing is
// additive — its absence MUST NOT change the ON-005 gate outcome.
//
// Spec ref: operator-nfr.md §4.2 ON-006 — "Conforming MVH implementations MUST
// NOT be required to verify signatures beyond the commit-hash match of §4.2.ON-005.
// Post-MVH introduction of signing is additive and does NOT invalidate MVH
// conformance."
func TestON006_PostMVHSigningIsAdditive(t *testing.T) {
	t.Parallel()

	// The MVH gate is hash-only. Verify that the gate function does not check
	// for a signature field and that adding a signature header (hypothetically)
	// does not change the outcome of a matching-hash scenario.
	actualHash := "abc123def456abc123def456abc123def456abc123"
	expectedHash := "abc123def456abc123def456abc123def456abc123"

	allowed, failureMode := commitHashFixtureEvaluate(actualHash, expectedHash)
	if !allowed {
		t.Errorf("ON-006: MVH hash-only gate rejected a matching-hash upgrade (failureMode=%q); post-MVH signing MUST be additive, not a pre-existing requirement", failureMode)
	}

	// The §8 taxonomy MUST NOT require a signing-related category for MVH.
	// Verify codes 1–23 do not include a signing-verification code.
	for code := 1; code <= 23; code++ {
		e, ok := operatornfr.LookupExitCode(code)
		if !ok {
			continue
		}
		if strings.Contains(e.Category, "signing") || strings.Contains(e.Category, "signature") {
			t.Errorf("ON-006: §8 taxonomy code %d category %q references signing; binary signing is post-MVH per ON-006 and MUST NOT appear in the MVH exit-code taxonomy", code, e.Category)
		}
	}
}

// TestON005_UpgradeRequiresPausedIsDistinctFromHashMismatch verifies that the
// "daemon not paused" rejection (§8 code 13) is distinct from the hash-mismatch
// rejection (§8 code 14). Both emit `operator_upgrade_rejected` but for
// different reasons.
//
// Spec ref: operator-nfr.md §8 — code 13 `upgrade-requires-paused` vs code 14
// `upgrade-hash-mismatch`; §4.6.ON-020 — "upgrade is only valid when daemon is
// `paused`."
func TestON005_UpgradeRequiresPausedIsDistinctFromHashMismatch(t *testing.T) {
	t.Parallel()

	paused, ok13 := operatornfr.LookupExitCode(13)
	if !ok13 {
		t.Fatal("ON-005: §8 taxonomy missing code 13 (upgrade-requires-paused)")
	}
	mismatch, ok14 := operatornfr.LookupExitCode(14)
	if !ok14 {
		t.Fatal("ON-005: §8 taxonomy missing code 14 (upgrade-hash-mismatch)")
	}

	if paused.Category == mismatch.Category {
		t.Errorf("ON-005: code 13 and code 14 have the same category %q; they MUST be distinct", paused.Category)
	}
	// Both legitimately emit operator_upgrade_rejected per §4.6.ON-020 and §4.2.ON-005.
	if paused.Event != "operator_upgrade_rejected" {
		t.Errorf("ON-005: code 13 emitted_event = %q, want %q", paused.Event, "operator_upgrade_rejected")
	}
	if mismatch.Event != "operator_upgrade_rejected" {
		t.Errorf("ON-005: code 14 emitted_event = %q, want %q", mismatch.Event, "operator_upgrade_rejected")
	}
}
