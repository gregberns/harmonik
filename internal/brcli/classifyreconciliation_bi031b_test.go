package brcli

// classifyreconciliation_bi031b_test.go — unit tests for BI-031b schema-mismatch
// divergence_inconclusive emission.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031b.
//
// Helper prefix: schemaMismatchFixture (bead concept: schema-mismatch / BI-031b).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// schemaMismatchFixtureStubBus is a minimal SchemaMismatchEmitter stub that
// records Emit calls for assertion.
type schemaMismatchFixtureStubBus struct {
	calls     int
	lastType  core.EventType
	lastRaw   []byte
	returnErr error
}

func (s *schemaMismatchFixtureStubBus) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	s.calls++
	s.lastType = eventType
	s.lastRaw = payload
	return s.returnErr
}

// TestBrErrReconciliationCategoryWithEmit_schemaMismatchEmitsEvent verifies that
// BrErrReconciliationCategoryWithEmit emits exactly one divergence_inconclusive
// event with reason=authority_unavailable when err resolves to BrSchemaMismatch,
// and still returns RecCat0 (the §8 routing result for BrSchemaMismatch).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031b.
func TestBrErrReconciliationCategoryWithEmit_schemaMismatchEmitsEvent(t *testing.T) {
	t.Parallel()

	stub := &schemaMismatchFixtureStubBus{}
	cat := BrErrReconciliationCategoryWithEmit(
		context.Background(),
		BrSchemaMismatch,
		"test-evidence-ref",
		stub,
	)

	// Classification must still be RecCat0 — emission does not change the routing.
	if cat != RecCat0 {
		t.Errorf("BI-031b: BrErrReconciliationCategoryWithEmit(BrSchemaMismatch) = %q; want %q", cat, RecCat0)
	}

	// Exactly one Emit call must have been made.
	if stub.calls != 1 {
		t.Errorf("BI-031b: bus.Emit call count = %d; want 1 (one divergence_inconclusive per BrSchemaMismatch)", stub.calls)
	}

	// Event type must be divergence_inconclusive.
	if stub.lastType != core.EventTypeDivergenceInconclusive {
		t.Errorf("BI-031b: bus.Emit event type = %q; want %q", stub.lastType, core.EventTypeDivergenceInconclusive)
	}

	// Payload must be valid DivergenceInconclusivePayload with reason=authority_unavailable.
	var got core.DivergenceInconclusivePayload
	if err := json.Unmarshal(stub.lastRaw, &got); err != nil {
		t.Fatalf("BI-031b: payload unmarshal: %v", err)
	}
	if got.Reason != core.DivergenceInconclusiveReasonAuthorityUnavailable {
		t.Errorf("BI-031b: payload.Reason = %q; want %q", got.Reason, core.DivergenceInconclusiveReasonAuthorityUnavailable)
	}
	if got.EvidenceRef == "" {
		t.Error("BI-031b: payload.EvidenceRef is empty; want non-empty evidence reference")
	}
	if !got.Valid() {
		t.Error("BI-031b: DivergenceInconclusivePayload.Valid() = false; payload is malformed")
	}
}

// TestBrErrReconciliationCategoryWithEmit_wrappedSchemaMismatch verifies that a
// BrSchemaMismatch error wrapped via fmt.Errorf("...: %w", BrSchemaMismatch) still
// triggers emission (errors.Is unwrapping must reach the sentinel).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031b.
func TestBrErrReconciliationCategoryWithEmit_wrappedSchemaMismatch(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("adapter: br invocation: %w", BrSchemaMismatch)
	stub := &schemaMismatchFixtureStubBus{}

	cat := BrErrReconciliationCategoryWithEmit(context.Background(), wrapped, "wrapped-evidence", stub)

	if cat != RecCat0 {
		t.Errorf("BI-031b: wrapped BrSchemaMismatch category = %q; want %q", cat, RecCat0)
	}
	if stub.calls != 1 {
		t.Errorf("BI-031b: bus.Emit call count = %d; want 1 for wrapped BrSchemaMismatch", stub.calls)
	}
}

// TestBrErrReconciliationCategoryWithEmit_nonSchemaMismatchNoEmit verifies that
// non-BrSchemaMismatch errors do NOT trigger emission, covering all remaining §8
// routing table entries.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031b.
func TestBrErrReconciliationCategoryWithEmit_nonSchemaMismatchNoEmit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		brErr BrError
		label string
	}{
		{BrNotFound, "BrNotFound"},
		{BrConflict, "BrConflict"},
		{BrDbLocked, "BrDbLocked"},
		{BrUnavailable, "BrUnavailable"},
		{BrOther, "BrOther"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()

			stub := &schemaMismatchFixtureStubBus{}
			BrErrReconciliationCategoryWithEmit(context.Background(), tc.brErr, "evidence", stub)

			if stub.calls != 0 {
				t.Errorf(
					"BI-031b: bus.Emit called %d time(s) for %s; want 0 (only BrSchemaMismatch triggers emission)",
					stub.calls, tc.label,
				)
			}
		})
	}
}

// TestBrErrReconciliationCategoryWithEmit_nilBusNoEmitPanic verifies that passing
// a nil bus does not panic; the function falls back to structured-log per ON-035
// and still returns the correct classification.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031b; operator-nfr.md §4.9 ON-035.
func TestBrErrReconciliationCategoryWithEmit_nilBusNoEmitPanic(t *testing.T) {
	t.Parallel()

	cat := BrErrReconciliationCategoryWithEmit(
		context.Background(),
		BrSchemaMismatch,
		"nil-bus-evidence",
		nil,
	)

	if cat != RecCat0 {
		t.Errorf("BI-031b: nil-bus BrSchemaMismatch category = %q; want %q", cat, RecCat0)
	}
}

// TestBrErrReconciliationCategoryWithEmit_busEmitErrorFallback verifies that when
// bus.Emit returns an error the function falls back to structured-log per ON-035
// (no panic, no propagation) and still returns RecCat0.
//
// Spec ref: operator-nfr.md §4.9 ON-035.
func TestBrErrReconciliationCategoryWithEmit_busEmitErrorFallback(t *testing.T) {
	t.Parallel()

	stub := &schemaMismatchFixtureStubBus{returnErr: errors.New("bus unavailable")}

	cat := BrErrReconciliationCategoryWithEmit(
		context.Background(),
		BrSchemaMismatch,
		"emit-error-evidence",
		stub,
	)

	if cat != RecCat0 {
		t.Errorf("BI-031b: emit-error BrSchemaMismatch category = %q; want %q", cat, RecCat0)
	}
	// Emit was still called once (the error was from the bus, not from us).
	if stub.calls != 1 {
		t.Errorf("BI-031b: bus.Emit call count = %d; want 1 (called, then error fell back to slog)", stub.calls)
	}
}
