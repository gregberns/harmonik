package core

// cp020_guard_mechanism_tagged_test.go — Conformance tests for CP-020
//
// specs/control-points.md §4.4.CP-020:
//
//	A Guard's evaluator MUST be mechanism-tagged. Cognition-tagged Guards are
//	forbidden: they would place cognition inside the selection-logic layer,
//	violating ZFC (the Zone of Forbidden Cognition) per [architecture.md §4.2].
//	Any Guard declaration carrying a cognition-tagged evaluator fails registration.
//
// Tests:
//  1. Cognition-tagged Guard is rejected at registration with ErrCognitionGuard.
//  2. The specific error sentinel ErrCognitionGuard is returned (not a generic error).
//  3. Registry is unmodified after a CP-020 rejection.
//  4. Mechanism-tagged Guard registers successfully (positive path).
//  5. Re-registration of a mechanism Guard with identical body is idempotent (CP-044
//     applies; no CP-020 false-negative on re-registration).
//  6. A second, valid mechanism Guard registers cleanly after a prior CP-020 rejection
//     in the same registry (registry is not poisoned by failed registration).
//
// Refs: hk-a8bg.19

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// CP-020 §1+2: cognition-tagged Guard rejected with correct sentinel
// ---------------------------------------------------------------------------

// TestCP020_CognitionGuardRejectedAtRegistration verifies that a Guard
// ControlPoint with a cognition-tagged evaluator is rejected at
// MapRegistry.Register with ErrCognitionGuard.
//
// specs/control-points.md §4.4.CP-020: "Any Guard declaration carrying a
// cognition-tagged evaluator fails registration."
func TestCP020_CognitionGuardRejectedAtRegistration(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	bad := cp002FixtureCognitionGuard(t, "zfc-guard")

	err := reg.Register(bad)
	if err == nil {
		t.Fatal("CP-020: Register cognition-tagged Guard: expected error, got nil")
	}
	if !errors.Is(err, ErrCognitionGuard) {
		t.Errorf("CP-020: Register cognition-tagged Guard: got %v, want ErrCognitionGuard", err)
	}
}

// ---------------------------------------------------------------------------
// CP-020 §3: registry unmodified after rejection
// ---------------------------------------------------------------------------

// TestCP020_RegistryUnmodifiedAfterCognitionGuardRejection verifies that a
// failed CP-020 registration does not leave a partial entry in the registry.
//
// The registry MUST remain empty; a subsequent LookupByName for the rejected
// name MUST return false.
func TestCP020_RegistryUnmodifiedAfterCognitionGuardRejection(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	bad := cp002FixtureCognitionGuard(t, "zfc-guard-state")

	if err := reg.Register(bad); err == nil {
		t.Fatal("CP-020: expected error registering cognition Guard, got nil")
	}

	if len(reg.All()) != 0 {
		t.Errorf("CP-020: registry has %d entries after rejection, want 0", len(reg.All()))
	}
	if _, ok := reg.LookupByName("zfc-guard-state"); ok {
		t.Error("CP-020: LookupByName returned the rejected cognition Guard — registry must be unmodified")
	}
}

// ---------------------------------------------------------------------------
// CP-020 §4: mechanism-tagged Guard accepted (positive path)
// ---------------------------------------------------------------------------

// TestCP020_MechanismGuardAccepted verifies the positive path adjacent to
// CP-020: a mechanism-tagged Guard registers without error.
func TestCP020_MechanismGuardAccepted(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	good := cp002FixtureGuardMechanism(t, "mech-guard")

	if err := reg.Register(good); err != nil {
		t.Errorf("CP-020: Register mechanism-tagged Guard: unexpected error: %v", err)
	}

	got, ok := reg.LookupByName("mech-guard")
	if !ok {
		t.Fatal("CP-020: mechanism Guard not found after registration")
	}
	if got.Kind != KindGuard {
		t.Errorf("CP-020: registered Kind = %q, want Guard", got.Kind)
	}
	if got.Evaluator.Mode != ModeTagMechanism {
		t.Errorf("CP-020: registered evaluator Mode = %q, want mechanism", got.Evaluator.Mode)
	}
}

// ---------------------------------------------------------------------------
// CP-020 §5: re-registration of mechanism Guard is idempotent (CP-044)
// ---------------------------------------------------------------------------

// TestCP020_MechanismGuardIdempotentReregistration verifies that CP-044
// (identical-body re-registration is silent success) applies to Guards and
// does not trigger a false CP-020 rejection on the second call.
func TestCP020_MechanismGuardIdempotentReregistration(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	good := cp002FixtureGuardMechanism(t, "mech-guard-idem")

	if err := reg.Register(good); err != nil {
		t.Fatalf("CP-020: first Register: %v", err)
	}
	// Second registration with identical body — must succeed silently (CP-044).
	if err := reg.Register(good); err != nil {
		t.Errorf("CP-020: second Register (identical body): unexpected error: %v", err)
	}

	all := reg.All()
	if len(all) != 1 {
		t.Errorf("CP-020: All() len = %d after idempotent re-registration, want 1", len(all))
	}
}

// ---------------------------------------------------------------------------
// CP-020 §6: registry not poisoned by a prior rejection
// ---------------------------------------------------------------------------

// TestCP020_RegistryAcceptsValidGuardAfterRejection verifies that a
// mechanism-tagged Guard registers successfully in the same registry instance
// that previously rejected a cognition Guard, confirming the registry is not
// poisoned by a CP-020 rejection.
func TestCP020_RegistryAcceptsValidGuardAfterRejection(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	bad := cp002FixtureCognitionGuard(t, "bad-guard-poison")
	good := cp002FixtureGuardMechanism(t, "good-guard-after")

	// First: reject the bad Guard.
	if err := reg.Register(bad); err == nil {
		t.Fatal("CP-020: expected error registering cognition Guard")
	}

	// Then: register a valid mechanism Guard — must succeed.
	if err := reg.Register(good); err != nil {
		t.Errorf("CP-020: Register valid Guard after rejection: unexpected error: %v", err)
	}

	if _, ok := reg.LookupByName("good-guard-after"); !ok {
		t.Error("CP-020: valid Guard not found after prior rejection")
	}
}
