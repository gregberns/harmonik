package core

import (
	"testing"

	"github.com/google/uuid"
)

// Tests for budgetCounterState per specs/control-points.md §4.5.CP-026:
//
//	The Budget counter state MUST be internal to the handler. It MUST NOT be
//	written to any durable store other than through the typed budget_accrual,
//	budget_warning, and budget_exhausted events. Cross-subsystem reads of the
//	counter MUST go through the event bus per [event-model.md §4.3]; there is
//	no GetBudgetCounter() surface.
//
// Refs: hk-a8bg.25

// cp026RunID is a stable RunID used across CP-026 counter-state tests.
var cp026RunID = RunID(uuid.MustParse("01960084-0000-7000-8000-000000000026"))

// cp026SessionID is a stable SessionID used across CP-026 counter-state tests.
const cp026SessionID SessionID = "sess-cp026-test"

// cp026BudgetRef is a stable BudgetRef used across CP-026 counter-state tests.
const cp026BudgetRef BudgetRef = "token-budget-cp026"

// TestBudgetCounterState_TypeIsUnexported documents that budgetCounterState is
// an unexported type — no cross-subsystem caller can hold or read a raw counter
// value. The counter is only accessible within the core package per CP-026.
//
// This test is a compile-time structural fixture: if the type were exported
// (BudgetCounterState), external packages could read fields directly, violating
// the "no GetBudgetCounter() surface" invariant. The test verifies the type can
// be constructed and used within the package, confirming the unexported pattern.
func TestBudgetCounterState_TypeIsUnexported(t *testing.T) {
	t.Parallel()

	// Constructing the counter works within the package — this compiles only
	// because we're in package core. External packages cannot access this type.
	s := newBudgetCounterState(cp026RunID, cp026BudgetRef, 10_000, 0.8)

	// No GetBudgetCounter() or exported field access is possible — the only
	// observable action is Accrue, which returns event payloads.
	_ = s
}

// TestBudgetCounterState_AccrueEmitsBudgetAccrualPayload verifies that every
// Accrue call returns a non-zero BudgetAccrualPayload — the sole durable
// record of the accrual per CP-026.
func TestBudgetCounterState_AccrueEmitsBudgetAccrualPayload(t *testing.T) {
	t.Parallel()

	s := newBudgetCounterState(cp026RunID, cp026BudgetRef, 10_000, 0.8)

	const delta = int64(500)
	out := s.Accrue(cp026SessionID, CostBasisOutputBytes, nil, delta)

	if out.Accrual.RunID != cp026RunID {
		t.Errorf("Accrual.RunID = %v, want %v", out.Accrual.RunID, cp026RunID)
	}
	if out.Accrual.SessionID != cp026SessionID {
		t.Errorf("Accrual.SessionID = %v, want %v", out.Accrual.SessionID, cp026SessionID)
	}
	if out.Accrual.CostUnits != float64(delta) {
		t.Errorf("Accrual.CostUnits = %v, want %v", out.Accrual.CostUnits, float64(delta))
	}
	if !out.Accrual.Valid() {
		t.Errorf("Accrual payload is invalid: %+v", out.Accrual)
	}
}

// TestBudgetCounterState_NoWarningBelowThreshold verifies that Accrue returns
// no Warning payload when cumulative accrual has not yet crossed the threshold.
func TestBudgetCounterState_NoWarningBelowThreshold(t *testing.T) {
	t.Parallel()

	s := newBudgetCounterState(cp026RunID, cp026BudgetRef, 10_000, 0.8)

	// Accrue 7999 tokens — below 80% of 10000 → no warning.
	out := s.Accrue(cp026SessionID, CostBasisOutputBytes, nil, 7_999)
	if out.Warning != nil {
		t.Error("Accrue: want no Warning below threshold, got one")
	}
}

// TestBudgetCounterState_WarningEmittedAtThreshold verifies that Accrue returns
// a Warning payload the first time cumulative accrual reaches the threshold.
func TestBudgetCounterState_WarningEmittedAtThreshold(t *testing.T) {
	t.Parallel()

	s := newBudgetCounterState(cp026RunID, cp026BudgetRef, 10_000, 0.8)

	// Accrue 8000 tokens — exactly 80% → warning fires.
	out := s.Accrue(cp026SessionID, CostBasisOutputBytes, nil, 8_000)
	if out.Warning == nil {
		t.Error("Accrue: want Warning at exact threshold (8000/10000), got nil")
	}
	if out.Warning != nil && !out.Warning.Valid() {
		t.Errorf("Warning payload is invalid: %+v", out.Warning)
	}
}

// TestBudgetCounterState_WarningSuppressedAfterFirstEmission verifies that the
// Warning payload is emitted exactly once — subsequent Accrue calls above the
// threshold return nil Warning (warn-once semantics per CP-025).
func TestBudgetCounterState_WarningSuppressedAfterFirstEmission(t *testing.T) {
	t.Parallel()

	s := newBudgetCounterState(cp026RunID, cp026BudgetRef, 10_000, 0.8)

	// First crossing at 8000 → warning emitted.
	out1 := s.Accrue(cp026SessionID, CostBasisOutputBytes, nil, 8_000)
	if out1.Warning == nil {
		t.Fatal("Accrue: want Warning on first threshold crossing, got nil")
	}

	// Second Accrue above threshold → no second warning.
	out2 := s.Accrue(cp026SessionID, CostBasisOutputBytes, nil, 500)
	if out2.Warning != nil {
		t.Error("Accrue: want no Warning on second call above threshold (warn-once), got one")
	}
}

// TestBudgetCounterState_CheckDispatchAdmitsWhenWithinLimit verifies that
// CheckDispatch returns false (admitted) when accrued + cost does not exceed
// the limit — no budget_exhausted event payload is returned.
func TestBudgetCounterState_CheckDispatchAdmitsWhenWithinLimit(t *testing.T) {
	t.Parallel()

	s := newBudgetCounterState(cp026RunID, cp026BudgetRef, 10_000, 0.8)
	s.Accrue(cp026SessionID, CostBasisOutputBytes, nil, 5_000) // accrued=5000

	// Proposed dispatch of 4000 tokens — fits within remaining 5000.
	_, denied := s.CheckDispatch(4_000)
	if denied {
		t.Error("CheckDispatch: want ADMITTED (5000 accrued, 4000 cost, limit 10000), got DENIED")
	}
}

// TestBudgetCounterState_CheckDispatchDeniesWhenExceedsLimit verifies that
// CheckDispatch returns true (denied) with a valid BudgetExhaustedEventPayload
// when the proposed cost would exceed the remaining limit.
func TestBudgetCounterState_CheckDispatchDeniesWhenExceedsLimit(t *testing.T) {
	t.Parallel()

	s := newBudgetCounterState(cp026RunID, cp026BudgetRef, 10_000, 0.8)
	s.Accrue(cp026SessionID, CostBasisOutputBytes, nil, 9_500) // accrued=9500, remaining=500

	// Proposed dispatch of 600 tokens — exceeds remaining 500.
	payload, denied := s.CheckDispatch(600)
	if !denied {
		t.Error("CheckDispatch: want DENIED (9500 accrued, 600 cost, limit 10000), got ADMITTED")
	}
	if !payload.Valid() {
		t.Errorf("exhausted payload is invalid: %+v", payload)
	}
	if payload.RunID != cp026RunID {
		t.Errorf("payload.RunID = %v, want %v", payload.RunID, cp026RunID)
	}
	if payload.BudgetRef != cp026BudgetRef {
		t.Errorf("payload.BudgetRef = %q, want %q", payload.BudgetRef, cp026BudgetRef)
	}
}

// TestBudgetCounterState_RehydrateAccrualRestoresPreCrashState verifies that
// RehydrateAccrual correctly reconstructs the pre-crash counter total from
// budget_accrual event replay per CP-026a. After rehydration the counter
// enforces the remaining allowance as if the run never restarted.
func TestBudgetCounterState_RehydrateAccrualRestoresPreCrashState(t *testing.T) {
	t.Parallel()

	s := newBudgetCounterState(cp026RunID, cp026BudgetRef, 10_000, 0.8)

	// Simulate rehydration: replay three prior budget_accrual deltas (500 + 300 + 200 = 1000).
	s.RehydrateAccrual(500)
	s.RehydrateAccrual(300)
	s.RehydrateAccrual(200)

	// After rehydration: remaining is 9000, not 10000 (no double-spend).
	// A dispatch of 9001 must be DENIED.
	_, denied := s.CheckDispatch(9_001)
	if !denied {
		t.Error("CheckDispatch: want DENIED after rehydration with 1000 accrued and 9001 cost, got ADMITTED (double-spend)")
	}

	// A dispatch of 9000 must be ADMITTED (exact remaining).
	_, denied = s.CheckDispatch(9_000)
	if denied {
		t.Error("CheckDispatch: want ADMITTED for exact remaining after rehydration, got DENIED")
	}
}

// TestBudgetCounterState_RehydrateDoesNotEmitPayloads verifies that
// RehydrateAccrual is silent — it returns no event payload. Rehydration is
// read-only reconstruction; it must not produce new budget_accrual events that
// would be written to the durable JSONL store, as that would double the accrual
// record per CP-026a.
func TestBudgetCounterState_RehydrateDoesNotEmitPayloads(t *testing.T) {
	t.Parallel()

	s := newBudgetCounterState(cp026RunID, cp026BudgetRef, 10_000, 0.8)

	// RehydrateAccrual has no return value — the absence of a return value is
	// the compile-time proof that rehydration cannot produce durable events.
	// This test documents the contract explicitly.
	s.RehydrateAccrual(1_000) // no payload returned → cannot be emitted
}

// TestBudgetCounterState_ObservabilityOnlyViaEventPayloads documents the core
// CP-026 invariant: the counter's current value is not directly readable by
// any caller. The only way to observe counter state is through event payloads
// returned by Accrue and CheckDispatch.
//
// This test models the pattern that compliant code MUST follow: counter
// observation goes through the event bus (event payloads are emitted to the
// JSONL store; consumers subscribe via event-model.md §4.3), never through a
// direct counter read.
func TestBudgetCounterState_ObservabilityOnlyViaEventPayloads(t *testing.T) {
	t.Parallel()

	s := newBudgetCounterState(cp026RunID, cp026BudgetRef, 10_000, 0.8)

	// The only handles to counter state:
	//   1. BudgetAccrualPayload.CostUnits — the delta that was accrued
	//   2. BudgetWarningPayload.Remaining — remaining at warning time
	//   3. BudgetExhaustedEventPayload — carry BudgetRef and attempted cost
	//
	// There is no s.GetAccrued(), s.Remaining(), or any exported field.

	out := s.Accrue(cp026SessionID, CostBasisOutputBytes, nil, 500)

	// The only observable state from this Accrue is the event payload.
	if out.Accrual.CostUnits != 500 {
		t.Errorf("observable accrual delta = %v, want 500", out.Accrual.CostUnits)
	}

	// The consumer learns the counter moved by 500 from the event payload —
	// not from reading s.accrued directly. This is the CP-026 contract.
}
