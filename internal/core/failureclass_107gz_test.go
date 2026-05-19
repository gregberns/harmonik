package core

import "testing"

// failureclassFixture107gz is the test helper prefix for this bead.

// failureclassFixture107gzTaxonomyRow holds one row from the closed
// handler-fatal taxonomy table under test.
type failureclassFixture107gzTaxonomyRow struct {
	desc      string
	class     FailureClass
	subReason HandlerFatalSubReason
	wantFatal HandlerFatalClass
	wantOK    bool
}

// failureclassFixture107gzRows returns the full set of §8 class × sub-reason
// rows that must be classified correctly per HC-020a.
func failureclassFixture107gzRows() []failureclassFixture107gzTaxonomyRow {
	return []failureclassFixture107gzTaxonomyRow{
		// ── Handler-fatal entries (wantOK = true) ────────────────────────
		{
			desc:      "transient/rate_limit is handler-fatal",
			class:     FailureClassTransient,
			subReason: HandlerFatalSubReasonRateLimit,
			wantFatal: HandlerFatalClassRateLimit,
			wantOK:    true,
		},
		{
			desc:      "budget_exhausted/handler-account is handler-fatal",
			class:     FailureClassBudgetExhausted,
			subReason: HandlerFatalSubReasonHandlerAccount,
			wantFatal: HandlerFatalClassBudgetAccount,
			wantOK:    true,
		},

		// ── Non-handler-fatal entries (wantOK = false) ───────────────────

		// transient without a handler-fatal sub-reason is per-bead.
		{
			desc:      "transient/generic is not handler-fatal",
			class:     FailureClassTransient,
			subReason: "generic",
			wantOK:    false,
		},
		{
			desc:      "transient with empty sub-reason is not handler-fatal",
			class:     FailureClassTransient,
			subReason: "",
			wantOK:    false,
		},

		// structural is always per-bead (different beads fail for different
		// reasons; one structural failure does not predict the next).
		{
			desc:      "structural is not handler-fatal",
			class:     FailureClassStructural,
			subReason: "",
			wantOK:    false,
		},
		{
			desc:      "structural with any sub-reason is not handler-fatal",
			class:     FailureClassStructural,
			subReason: "rate_limit",
			wantOK:    false,
		},

		// deterministic is single-bead by definition.
		{
			desc:      "deterministic is not handler-fatal",
			class:     FailureClassDeterministic,
			subReason: "",
			wantOK:    false,
		},

		// canceled is an operator action; not a handler problem.
		{
			desc:      "canceled is not handler-fatal",
			class:     FailureClassCanceled,
			subReason: "",
			wantOK:    false,
		},

		// budget_exhausted with a per-run (non-account) scope is per-bead.
		{
			desc:      "budget_exhausted/per-run is not handler-fatal",
			class:     FailureClassBudgetExhausted,
			subReason: "per-run",
			wantOK:    false,
		},
		{
			desc:      "budget_exhausted with empty sub-reason is not handler-fatal",
			class:     FailureClassBudgetExhausted,
			subReason: "",
			wantOK:    false,
		},

		// compilation_loop is a daemon-observed traversal cap; the handler is
		// fine.
		{
			desc:      "compilation_loop is not handler-fatal",
			class:     FailureClassCompilationLoop,
			subReason: "",
			wantOK:    false,
		},
	}
}

// TestClassifyHandlerFatal_Taxonomy exercises every §8 class × known
// sub-reason combination against the HC-020a taxonomy table.
func TestClassifyHandlerFatal_Taxonomy(t *testing.T) {
	t.Parallel()

	for _, row := range failureclassFixture107gzRows() {
		row := row
		t.Run(row.desc, func(t *testing.T) {
			t.Parallel()

			got, ok := ClassifyHandlerFatal(row.class, row.subReason)
			if ok != row.wantOK {
				t.Fatalf("ClassifyHandlerFatal(%q, %q): ok = %v, want %v",
					row.class, row.subReason, ok, row.wantOK)
			}
			if ok && got != row.wantFatal {
				t.Fatalf("ClassifyHandlerFatal(%q, %q): fatal class = %q, want %q",
					row.class, row.subReason, got, row.wantFatal)
			}
			if !ok && got != "" {
				t.Fatalf("ClassifyHandlerFatal(%q, %q): returned non-empty class %q on ok=false",
					row.class, row.subReason, got)
			}
		})
	}
}

// TestHandlerFatalClassConstants_Strings verifies the wire values of the two
// MVH HandlerFatalClass constants.
func TestHandlerFatalClassConstants_Strings(t *testing.T) {
	t.Parallel()

	if got, want := string(HandlerFatalClassRateLimit), "transient/rate_limit"; got != want {
		t.Errorf("HandlerFatalClassRateLimit = %q, want %q", got, want)
	}
	if got, want := string(HandlerFatalClassBudgetAccount), "budget_exhausted/handler-account"; got != want {
		t.Errorf("HandlerFatalClassBudgetAccount = %q, want %q", got, want)
	}
}

// TestClassifyHandlerFatal_FalseOnUnknown verifies that an entirely unknown
// FailureClass value does not panic and returns ok=false.
func TestClassifyHandlerFatal_FalseOnUnknown(t *testing.T) {
	t.Parallel()

	got, ok := ClassifyHandlerFatal("unknown_class", "anything")
	if ok {
		t.Fatalf("expected ok=false for unknown class, got true (fatal=%q)", got)
	}
}
