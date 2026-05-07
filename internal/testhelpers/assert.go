package testhelpers

import (
	"fmt"
	"testing"
)

// AssertNoError fails the test immediately if err is non-nil.
//
//	testhelpers.AssertNoError(t, err)
func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// AssertEqual fails the test immediately if want != got, formatting both values
// with %v.
//
//	testhelpers.AssertEqual(t, "ready", status)
func AssertEqual[T comparable](t *testing.T, want, got T) {
	t.Helper()
	if want != got {
		t.Fatalf("value mismatch:\n  want: %v\n   got: %v", want, got)
	}
}

// AssertTrue fails the test immediately if cond is false, using msg as the
// failure annotation.
//
//	testhelpers.AssertTrue(t, len(items) > 0, "expected at least one item")
func AssertTrue(t *testing.T, cond bool, msg string, args ...any) {
	t.Helper()
	if !cond {
		t.Fatalf("assertion failed: %s", fmt.Sprintf(msg, args...))
	}
}
