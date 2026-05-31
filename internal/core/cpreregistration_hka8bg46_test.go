package core

// cpreregistration_hka8bg46_test.go — CP-044 body-equality exclusion tests
//
// Covers specs/control-points.md §4.9.CP-044:
//
//	"Body for equality purposes = (kind, trigger, evaluator, payload);
//	name, axes, and schema_version are NOT part of the body (a schema-version
//	bump on an otherwise-identical ControlPoint MUST NOT reject as divergent)."
//
// These tests verify that MapRegistry.Register treats re-registrations that
// differ ONLY in excluded fields (SchemaVersion, Axes) as identical-body and
// succeeds silently — i.e., they do NOT produce ErrDivergentBody.
//
// The positive path (identical body succeeds) and the negative path (divergent
// body fails) are covered in cpregistry_hka8bg2_test.go. This file is scoped
// to the exclusion invariant: fields outside the body tuple must be invisible
// to the equality check.
//
// Refs: hk-a8bg.46

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// CP-044: excluded fields do not contribute to body equality
// ---------------------------------------------------------------------------

// TestMapRegistry_SchemaVersionBumpNotDivergent verifies that registering a
// ControlPoint with the same name and identical body tuple (Kind, Trigger,
// Evaluator, Payload) but a different SchemaVersion succeeds silently.
//
// specs/control-points.md §4.9.CP-044: "a schema-version bump on an
// otherwise-identical ControlPoint MUST NOT reject as divergent."
func TestMapRegistry_SchemaVersionBumpNotDivergent(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	original := cp002FixtureGate(t, "versioned-gate")
	original.SchemaVersion = 1

	if err := reg.Register(original); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Re-register with SchemaVersion bumped but body tuple unchanged.
	bumped := original
	bumped.SchemaVersion = 2

	if err := reg.Register(bumped); err != nil {
		t.Errorf("Register with SchemaVersion bump: got error %v, want nil (CP-044 exclusion)", err)
	}

	// Registry must still contain exactly one entry.
	all := reg.All()
	if len(all) != 1 {
		t.Errorf("All() len = %d after schema-version-bump re-registration, want 1", len(all))
	}
}

// TestMapRegistry_AxesChangeNotDivergent verifies that registering a
// ControlPoint with the same name and identical body tuple but different Axes
// succeeds silently.
//
// specs/control-points.md §4.9.CP-044: axes is NOT part of the body.
func TestMapRegistry_AxesChangeNotDivergent(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	original := cp002FixtureGate(t, "axes-gate")
	original.Axes = BaselineAxisTags

	if err := reg.Register(original); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Re-register with a non-baseline Axes value but body tuple unchanged.
	nonBaseline := original
	nonBaseline.Axes = AxisTags{
		LLMFreedom:    LLMFreedomBounded,
		IODeterminism: IODeterminismDeterministic,
		ReplaySafety:  ReplaySafetySafe,
		Idempotency:   AxisIdempotencyIdempotent,
	}

	if err := reg.Register(nonBaseline); err != nil {
		t.Errorf("Register with Axes change: got error %v, want nil (CP-044 exclusion)", err)
	}

	// Registry must still contain exactly one entry.
	all := reg.All()
	if len(all) != 1 {
		t.Errorf("All() len = %d after axes-change re-registration, want 1", len(all))
	}
}

// TestMapRegistry_SchemaVersionAndAxesBothChangedNotDivergent verifies that
// simultaneous changes to both SchemaVersion and Axes on an otherwise-identical
// ControlPoint succeed silently.
//
// This is the combined exclusion case: neither excluded field alone nor both
// together should be treated as a body divergence.
func TestMapRegistry_SchemaVersionAndAxesBothChangedNotDivergent(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	original := cp002FixtureGate(t, "combined-excluded-gate")
	original.SchemaVersion = 1
	original.Axes = BaselineAxisTags

	if err := reg.Register(original); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Re-register with both excluded fields changed.
	modified := original
	modified.SchemaVersion = 5
	modified.Axes = AxisTags{
		LLMFreedom:    LLMFreedomUnbounded,
		IODeterminism: IODeterminismDeterministic,
		ReplaySafety:  ReplaySafetySafe,
		Idempotency:   AxisIdempotencyIdempotent,
	}

	if err := reg.Register(modified); err != nil {
		t.Errorf("Register with SchemaVersion+Axes changed: got error %v, want nil (CP-044 exclusion)", err)
	}

	all := reg.All()
	if len(all) != 1 {
		t.Errorf("All() len = %d after combined-exclusion re-registration, want 1", len(all))
	}
}

// TestMapRegistry_BodyChangeAfterSchemaVersionBump verifies that a genuine
// body-tuple difference (different Trigger) still returns ErrDivergentBody
// even when SchemaVersion is also bumped.
//
// This guards against accidentally over-relaxing the equality check: excluded
// fields being ignored must not mask a real body divergence.
func TestMapRegistry_BodyChangeAfterSchemaVersionBump(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	original := cp002FixtureGate(t, "bump-plus-diverge-gate")
	original.SchemaVersion = 1

	if err := reg.Register(original); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Different Trigger (body tuple divergence) AND bumped SchemaVersion.
	divergent := original
	divergent.SchemaVersion = 2
	divergent.Trigger = Trigger{Name: "different-trigger"}

	err := reg.Register(divergent)
	if err == nil {
		t.Fatal("Register with divergent body + schema-version bump: expected ErrDivergentBody, got nil")
	}
	if !errors.Is(err, ErrDivergentBody) {
		t.Errorf("Register with divergent body + schema-version bump: got %v, want ErrDivergentBody", err)
	}
}
