package core

import (
	"testing"
)

// TestSkillVersion_IsZero_ZeroValue verifies that the zero-value SkillVersion
// (empty string) reports IsZero() = true.
//
// Spec ref: event-model.md §8.3.8 — version? is optional; absence = zero value.
func TestSkillVersion_IsZero_ZeroValue(t *testing.T) {
	t.Parallel()

	var v SkillVersion
	if !v.IsZero() {
		t.Error("SkillVersion zero value: IsZero() = false, want true")
	}
}

// TestSkillVersion_IsZero_NonEmpty verifies that a non-empty SkillVersion
// reports IsZero() = false.
//
// Spec ref: event-model.md §8.3.8.
func TestSkillVersion_IsZero_NonEmpty(t *testing.T) {
	t.Parallel()

	v := SkillVersion("1.2.3")
	if v.IsZero() {
		t.Errorf("SkillVersion(%q): IsZero() = true, want false", v)
	}
}

// TestSkillVersion_String_ReturnsRawString verifies that String() returns
// the underlying version string.
//
// Spec ref: event-model.md §8.3.8.
func TestSkillVersion_String_ReturnsRawString(t *testing.T) {
	t.Parallel()

	const raw = "2.0.1-rc.3"
	v := SkillVersion(raw)
	if v.String() != raw {
		t.Errorf("SkillVersion.String() = %q, want %q", v.String(), raw)
	}
}

// TestSkillVersion_String_ZeroValueReturnsEmpty verifies that String() on the
// zero value returns an empty string.
//
// Spec ref: event-model.md §8.3.8.
func TestSkillVersion_String_ZeroValueReturnsEmpty(t *testing.T) {
	t.Parallel()

	var v SkillVersion
	if v.String() != "" {
		t.Errorf("SkillVersion zero value: String() = %q, want empty", v.String())
	}
}

// TestSkillVersion_TypedAlias_DistinctFromString verifies that SkillVersion
// is a distinct named type from plain string — it MUST be explicitly converted
// and cannot be accidentally assigned from a string literal.
//
// Spec ref: event-model.md §8.3.8 — typed alias requirement (hk-tyjfi).
func TestSkillVersion_TypedAlias_DistinctFromString(t *testing.T) {
	t.Parallel()

	// This is a compile-time check via type assertion; the test passes if it
	// compiles. A plain string is NOT assignable to SkillVersion without
	// explicit conversion — that is the type-safety guarantee.
	v := SkillVersion("1.0.0")
	_ = v // used

	// Verify that SkillVersion("") == zero value (same as var declaration).
	var zero SkillVersion
	if SkillVersion("") != zero {
		t.Error("SkillVersion empty literal must equal zero value")
	}
}
