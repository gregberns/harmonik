package brcli

import (
	"encoding/json"
	"testing"
)

// brErrorFixtureAll returns all declared BrError constants in definition order.
// Used by exhaustive-coverage tests.
func brErrorFixtureAll() []BrError {
	return []BrError{
		BrOK,
		BrNotFound,
		BrConflict,
		BrDbLocked,
		BrSchemaMismatch,
		BrUnavailable,
		BrOther,
	}
}

// brErrorFixtureExitCodeTable returns the complete §6.1a exit-code mapping
// (illustrative codes only; BrUnavailable has no numeric code).
func brErrorFixtureExitCodeTable() []struct {
	code     int
	expected BrError
} {
	return []struct {
		code     int
		expected BrError
	}{
		{0, BrOK},
		{1, BrNotFound},
		{2, BrConflict},
		{3, BrDbLocked},
		{4, BrSchemaMismatch},
	}
}

// TestBrErrorValid verifies that Valid returns true for all declared constants
// and false for unknown values.
func TestBrErrorValid(t *testing.T) {
	for _, e := range brErrorFixtureAll() {
		if !e.Valid() {
			t.Errorf("BrError(%q).Valid() = false; want true", string(e))
		}
	}

	unknown := []BrError{"", "unknown", "ok", "notfound", "other"}
	for _, e := range unknown {
		if e.Valid() {
			t.Errorf("BrError(%q).Valid() = true; want false", string(e))
		}
	}
}

// TestBrErrorString verifies that String() returns the underlying string value.
func TestBrErrorString(t *testing.T) {
	cases := []struct {
		e    BrError
		want string
	}{
		{BrOK, "OK"},
		{BrNotFound, "NotFound"},
		{BrConflict, "Conflict"},
		{BrDbLocked, "DbLocked"},
		{BrSchemaMismatch, "SchemaMismatch"},
		{BrUnavailable, "Unavailable"},
		{BrOther, "Other"},
	}
	for _, tc := range cases {
		if got := tc.e.String(); got != tc.want {
			t.Errorf("BrError(%q).String() = %q; want %q", string(tc.e), got, tc.want)
		}
	}
}

// TestBrErrorMarshalText verifies round-trip marshaling for all declared
// constants and that unknown values are rejected.
func TestBrErrorMarshalText(t *testing.T) {
	for _, e := range brErrorFixtureAll() {
		b, err := e.MarshalText()
		if err != nil {
			t.Errorf("BrError(%q).MarshalText() error = %v; want nil", string(e), err)
			continue
		}
		if string(b) != string(e) {
			t.Errorf("BrError(%q).MarshalText() = %q; want %q", string(e), string(b), string(e))
		}
	}

	unknown := BrError("bogus")
	if _, err := unknown.MarshalText(); err == nil {
		t.Error("BrError(\"bogus\").MarshalText() error = nil; want non-nil")
	}
}

// TestBrErrorUnmarshalText verifies round-trip unmarshaling for all declared
// constants and that unknown values are rejected.
func TestBrErrorUnmarshalText(t *testing.T) {
	for _, e := range brErrorFixtureAll() {
		var got BrError
		if err := got.UnmarshalText([]byte(string(e))); err != nil {
			t.Errorf("BrError.UnmarshalText(%q) error = %v; want nil", string(e), err)
			continue
		}
		if got != e {
			t.Errorf("BrError.UnmarshalText(%q) = %q; want %q", string(e), string(got), string(e))
		}
	}

	var got BrError
	if err := got.UnmarshalText([]byte("bogus")); err == nil {
		t.Error(`BrError.UnmarshalText("bogus") error = nil; want non-nil`)
	}
}

// TestBrErrorJSONRoundTrip verifies that BrError survives a JSON encode→decode
// cycle correctly for all declared constants.
func TestBrErrorJSONRoundTrip(t *testing.T) {
	for _, e := range brErrorFixtureAll() {
		b, err := json.Marshal(e)
		if err != nil {
			t.Errorf("json.Marshal(BrError(%q)) error = %v; want nil", string(e), err)
			continue
		}
		var got BrError
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("json.Unmarshal BrError(%q) error = %v; want nil", string(e), err)
			continue
		}
		if got != e {
			t.Errorf("JSON round-trip BrError(%q): got %q", string(e), string(got))
		}
	}
}

// TestBrErrorFromExitCode_specTable verifies all §6.1a illustrative exit codes.
// This test calls BrErrorFromExitCode directly (process scar #3).
func TestBrErrorFromExitCode_specTable(t *testing.T) {
	for _, tc := range brErrorFixtureExitCodeTable() {
		got := BrErrorFromExitCode(tc.code)
		if got != tc.expected {
			t.Errorf("BrErrorFromExitCode(%d) = %q; want %q", tc.code, string(got), string(tc.expected))
		}
	}
}

// TestBrErrorFromExitCode_unknown verifies that unrecognized exit codes map to
// BrOther. Per BI-025a the caller must emit store_divergence_detected for these;
// that emission is the caller's responsibility, not this function's.
func TestBrErrorFromExitCode_unknown(t *testing.T) {
	unknownCodes := []int{-1, 5, 6, 7, 8, 127, 255, 1000}
	for _, code := range unknownCodes {
		got := BrErrorFromExitCode(code)
		if got != BrOther {
			t.Errorf("BrErrorFromExitCode(%d) = %q; want %q (BrOther)", code, string(got), string(BrOther))
		}
	}
}

// TestBrErrorFromExitCode_noUnavailableFromCode confirms that BrUnavailable is
// NOT produced by BrErrorFromExitCode: timeout and exec-error paths MUST
// classify directly as BrUnavailable without invoking this function.
func TestBrErrorFromExitCode_noUnavailableFromCode(t *testing.T) {
	// There is no numeric exit code in §6.1a that maps to BrUnavailable.
	// Scan all spec-listed codes and arbitrary unknown codes to confirm.
	allCodes := []int{0, 1, 2, 3, 4, 5, 127, 255}
	for _, code := range allCodes {
		got := BrErrorFromExitCode(code)
		if got == BrUnavailable {
			t.Errorf("BrErrorFromExitCode(%d) = BrUnavailable; BrUnavailable must never be returned by exit-code mapping", code)
		}
	}
}
