package brcli

import (
	"errors"
	"fmt"
	"testing"
)

// routeBrErrFixtureTable returns the exhaustive §8 routing table for use by
// BrErrReconciliationCategory tests.  Each entry states the BrError sentinel,
// a human-readable label, and the expected reconciliation category code.
func routeBrErrFixtureTable() []struct {
	brErr BrError
	label string
	want  ReconciliationCategory
} {
	return []struct {
		brErr BrError
		label string
		want  ReconciliationCategory
	}{
		{BrNotFound, "BrNotFound", RecCat3},
		{BrConflict, "BrConflict", RecCat3a},
		{BrDbLocked, "BrDbLocked", RecCat0},
		{BrSchemaMismatch, "BrSchemaMismatch", RecCat0},
		{BrUnavailable, "BrUnavailable", RecCat0},
		{BrOther, "BrOther", RecCat3},
	}
}

// TestBrErrReconciliationCategory_directSentinels verifies that all six
// BrError sentinels declared in specs/beads-integration.md §8 map to the
// correct reconciliation category when passed as a bare BrError (which itself
// implements the error interface).
func TestBrErrReconciliationCategory_directSentinels(t *testing.T) {
	t.Parallel()

	for _, tc := range routeBrErrFixtureTable() {
		tc := tc // capture
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()

			got := BrErrReconciliationCategory(tc.brErr)
			if got != tc.want {
				t.Errorf(
					"BrErrReconciliationCategory(%s) = %q; want %q",
					tc.label, got, tc.want,
				)
			}
		})
	}
}

// TestBrErrReconciliationCategory_wrappedErrors verifies that errors wrapping
// a BrError via fmt.Errorf("...: %w", ...) resolve correctly through errors.Is.
// This is the primary use-case: adapter code wraps BrError values with context
// before passing them up the call stack.
func TestBrErrReconciliationCategory_wrappedErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range routeBrErrFixtureTable() {
		tc := tc // capture
		t.Run("wrapped-"+tc.label, func(t *testing.T) {
			t.Parallel()

			wrapped := fmt.Errorf("adapter: br invocation failed: %w", tc.brErr)

			// Verify the sentinel is reachable via errors.Is (pre-condition).
			if !errors.Is(wrapped, tc.brErr) {
				t.Fatalf(
					"errors.Is(wrapped, %s) = false; wrapping did not preserve sentinel",
					tc.label,
				)
			}

			got := BrErrReconciliationCategory(wrapped)
			if got != tc.want {
				t.Errorf(
					"BrErrReconciliationCategory(wrapped %s) = %q; want %q",
					tc.label, got, tc.want,
				)
			}
		})
	}
}

// TestBrErrReconciliationCategory_doublyWrappedErrors verifies that a BrError
// nested two levels deep is still resolved via errors.Is chain walking.
func TestBrErrReconciliationCategory_doublyWrappedErrors(t *testing.T) {
	t.Parallel()

	inner := fmt.Errorf("br: beads locked: %w", BrDbLocked)
	outer := fmt.Errorf("adapter: startup recovery: %w", inner)

	got := BrErrReconciliationCategory(outer)
	if got != RecCat0 {
		t.Errorf(
			"BrErrReconciliationCategory(doubly-wrapped BrDbLocked) = %q; want %q",
			got, RecCat0,
		)
	}
}

// TestBrErrReconciliationCategory_nil verifies that a nil error returns the
// empty string (""), indicating no reconciliation category.
// BrOK is the success sentinel; callers should not route it through this
// function, but nil is the zero-value analog for the error interface.
func TestBrErrReconciliationCategory_nil(t *testing.T) {
	t.Parallel()

	got := BrErrReconciliationCategory(nil)
	if got != "" {
		t.Errorf("BrErrReconciliationCategory(nil) = %q; want %q (empty — no category)", got, "")
	}
}

// TestBrErrReconciliationCategory_brOK verifies that BrOK (the success sentinel)
// returns the empty string — a successful br invocation carries no reconciliation
// category per BI §8 (BrOK is absent from the §8 routing table).
func TestBrErrReconciliationCategory_brOK(t *testing.T) {
	t.Parallel()

	got := BrErrReconciliationCategory(BrOK)
	if got != "" {
		t.Errorf(
			"BrErrReconciliationCategory(BrOK) = %q; want %q (BrOK is not in the §8 routing table)",
			got, "",
		)
	}
}

// TestBrErrReconciliationCategory_unknownNonBrError verifies that an error
// that does not wrap any BrError value returns "Cat 6a" (integrity violation,
// LLM-triageable).  A non-BrError at this call site signals an unexpected
// caller state rather than a Beads-side divergence; Cat 6a is the safest
// escalation per reconciliation/spec.md §8.11.
func TestBrErrReconciliationCategory_unknownNonBrError(t *testing.T) {
	t.Parallel()

	plainErr := errors.New("something completely unrelated")

	got := BrErrReconciliationCategory(plainErr)
	if got != RecCat6a {
		t.Errorf(
			"BrErrReconciliationCategory(non-BrError) = %q; want %q (Cat 6a escalation)",
			got, RecCat6a,
		)
	}
}

// TestBrErrReconciliationCategory_routingTableCoverage is an exhaustive
// coverage guard: it verifies that every non-OK BrError constant produces a
// non-empty category and that BrOK produces the empty string.  This test will
// fail if a new BrError constant is added to brerror.go without updating
// brErrRoutingTable in classifyreconciliation.go.
func TestBrErrReconciliationCategory_routingTableCoverage(t *testing.T) {
	t.Parallel()

	// All BrError values from the canonical fixture (includes BrOK).
	all := brErrorFixtureAll()

	for _, e := range all {
		e := e
		t.Run("coverage-"+string(e), func(t *testing.T) {
			t.Parallel()

			got := BrErrReconciliationCategory(e)

			if e == BrOK {
				// BrOK → success path; no reconciliation category.
				if got != "" {
					t.Errorf(
						"BrErrReconciliationCategory(BrOK) = %q; want empty string",
						got,
					)
				}
				return
			}

			// All six error sentinels must map to a non-empty category.
			if got == "" {
				t.Errorf(
					"BrErrReconciliationCategory(%q) = empty string; want a non-empty category code",
					string(e),
				)
			}
		})
	}
}

// TestBrErrReconciliationCategory_specTable asserts the exact category for
// each §8 table row, providing a readable reference tied to spec text.
//
// Spec ref: specs/beads-integration.md §8.
func TestBrErrReconciliationCategory_specTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		brErr BrError
		label string
		want  ReconciliationCategory
		note  string
	}{
		{BrNotFound, "BrNotFound", RecCat3, "Beads-vs-harmonik divergence; investigator dispatch"},
		{BrConflict, "BrConflict", RecCat3a, "concurrent-claim race; idempotency recovery per BI §4.10"},
		{BrDbLocked, "BrDbLocked", RecCat0, "bounded retry; if persistent → exit code 8"},
		{BrSchemaMismatch, "BrSchemaMismatch", RecCat0, "exit code 8 (beads-unavailable); operator must align versions"},
		{BrUnavailable, "BrUnavailable", RecCat0, "bounded retry per PL-010 cadence"},
		{BrOther, "BrOther", RecCat3, "divergence-detected; investigator dispatch"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()

			got := BrErrReconciliationCategory(tc.brErr)
			if got != tc.want {
				t.Errorf(
					"BI §8: BrErrReconciliationCategory(%s) = %q; want %q (%s)",
					tc.label, got, tc.want, tc.note,
				)
			}
		})
	}
}
