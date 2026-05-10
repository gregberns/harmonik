package core

import (
	"encoding/json"
	"testing"
)

// errorCategoryFixture returns a valid ErrorCategory for structural tests
// (hk-b3f.109). Helper prefix: errorCategory per bead concept.
func errorCategoryFixture() ErrorCategory {
	return ErrorCategoryStructural
}

// TestErrorCategory_AllValuesValid verifies that every declared ErrorCategory
// constant passes Valid(), covering the full nine-value set per
// event-model.md §3, handler-contract.md §4.5, and the bus-internal values
// added by event-model.md §8.8.2 and §6.1 (hk-nptq0).
func TestErrorCategory_AllValuesValid(t *testing.T) {
	t.Parallel()

	categories := []ErrorCategory{
		ErrorCategoryTransient,
		ErrorCategoryStructural,
		ErrorCategoryDeterministic,
		ErrorCategoryCanceled,
		ErrorCategoryBudget,
		ErrorCategorySkillProvisioningFailed,
		ErrorCategoryProtocolMismatch,
		ErrorCategoryOverflow,
		ErrorCategoryPanic,
	}
	for _, c := range categories {
		if !c.Valid() {
			t.Errorf("ErrorCategory %q: Valid() = false, want true", c)
		}
	}
}

// TestErrorCategory_Valid_EmptyRejected verifies that an empty ErrorCategory
// string is not a valid value.
func TestErrorCategory_Valid_EmptyRejected(t *testing.T) {
	t.Parallel()

	c := ErrorCategory("")
	if c.Valid() {
		t.Error("ErrorCategory(\"\").Valid() = true, want false")
	}
}

// TestErrorCategory_Valid_UnknownRejected verifies that an unrecognised string
// does not pass Valid().
func TestErrorCategory_Valid_UnknownRejected(t *testing.T) {
	t.Parallel()

	unknown := ErrorCategory("ErrUnknown")
	if unknown.Valid() {
		t.Errorf("ErrorCategory(%q).Valid() = true, want false", unknown)
	}
}

// TestErrorCategory_JSONRoundTrip verifies that a declared ErrorCategory value
// survives a JSON marshal/unmarshal round-trip with its string value intact.
func TestErrorCategory_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := ErrorCategoryTransient
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got ErrorCategory
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got != orig {
		t.Errorf("round-trip: got %q, want %q", got, orig)
	}
}

// TestErrorCategory_JSONUnmarshalRejectsUnknown verifies that UnmarshalText
// rejects unknown string values, which includes empty strings and arbitrary
// strings not in the declared set.
func TestErrorCategory_JSONUnmarshalRejectsUnknown(t *testing.T) {
	t.Parallel()

	var c ErrorCategory
	err := json.Unmarshal([]byte(`"ErrBogus"`), &c)
	if err == nil {
		t.Error("json.Unmarshal of unknown ErrorCategory value: expected error, got nil")
	}
}

// TestErrorCategory_JSONMarshalRejectsInvalid verifies that MarshalText
// returns an error for an invalid (unrecognised) ErrorCategory.
func TestErrorCategory_JSONMarshalRejectsInvalid(t *testing.T) {
	t.Parallel()

	invalid := ErrorCategory("ErrBogus")
	_, err := json.Marshal(invalid)
	if err == nil {
		t.Error("json.Marshal of invalid ErrorCategory: expected error, got nil")
	}
}

// TestErrorCategory_StringValues verifies the wire-form string values of each
// constant match the names declared in handler-contract.md §4.5 / event-model.md §3,
// and that the bus-internal values use their lowercase wire forms per §8.8.2 / §6.1.
func TestErrorCategory_StringValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		cat  ErrorCategory
		want string
	}{
		{ErrorCategoryTransient, "ErrTransient"},
		{ErrorCategoryStructural, "ErrStructural"},
		{ErrorCategoryDeterministic, "ErrDeterministic"},
		{ErrorCategoryCanceled, "ErrCanceled"},
		{ErrorCategoryBudget, "ErrBudget"},
		{ErrorCategorySkillProvisioningFailed, "ErrSkillProvisioningFailed"},
		{ErrorCategoryProtocolMismatch, "ErrProtocolMismatch"},
		{ErrorCategoryOverflow, "overflow"},
		{ErrorCategoryPanic, "panic"},
	}
	for _, tc := range cases {
		if string(tc.cat) != tc.want {
			t.Errorf("ErrorCategory string value: got %q, want %q", string(tc.cat), tc.want)
		}
	}
}

// TestErrorCategory_BusInternalDistinctFromHandlerSentinels verifies that the
// two bus-internal categories (overflow, panic) are distinct from all seven
// handler-contract sentinel values and from each other, per event-model.md
// §8.8.2 and §6.1 (hk-nptq0).
func TestErrorCategory_BusInternalDistinctFromHandlerSentinels(t *testing.T) {
	t.Parallel()

	handlerSentinels := []ErrorCategory{
		ErrorCategoryTransient,
		ErrorCategoryStructural,
		ErrorCategoryDeterministic,
		ErrorCategoryCanceled,
		ErrorCategoryBudget,
		ErrorCategorySkillProvisioningFailed,
		ErrorCategoryProtocolMismatch,
	}
	busInternal := []ErrorCategory{
		ErrorCategoryOverflow,
		ErrorCategoryPanic,
	}

	for _, bi := range busInternal {
		for _, hs := range handlerSentinels {
			if bi == hs {
				t.Errorf("bus-internal %q must be distinct from handler sentinel %q", bi, hs)
			}
		}
	}
	if ErrorCategoryOverflow == ErrorCategoryPanic {
		t.Error("ErrorCategoryOverflow and ErrorCategoryPanic must be distinct values")
	}
}

// TestErrorCategory_SubSentinelsWrapStructural documents the spec relationship
// that ErrSkillProvisioningFailed and ErrProtocolMismatch wrap ErrStructural per
// handler-contract.md §4.5 (HC-021, HC-022). At the typed-alias level these are
// distinct ErrorCategory values; the wrapping relationship lives in the handler
// Go error types (not in core/). This test asserts the distinct string values so
// consumers can dispatch narrowest-first per the spec.
func TestErrorCategory_SubSentinelsWrapStructural(t *testing.T) {
	t.Parallel()

	// Sub-sentinels are distinct from the base structural category.
	if ErrorCategorySkillProvisioningFailed == ErrorCategoryStructural {
		t.Error("ErrSkillProvisioningFailed and ErrStructural must be distinct ErrorCategory values")
	}
	if ErrorCategoryProtocolMismatch == ErrorCategoryStructural {
		t.Error("ErrProtocolMismatch and ErrStructural must be distinct ErrorCategory values")
	}
	if ErrorCategorySkillProvisioningFailed == ErrorCategoryProtocolMismatch {
		t.Error("ErrSkillProvisioningFailed and ErrProtocolMismatch must be distinct ErrorCategory values")
	}
}
