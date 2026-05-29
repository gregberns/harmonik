package core

// budgetcounterstate_hka8bg25.go — CP-026: Budget counter state is internal;
// observable only via events.
//
// Implements the structural constraint from specs/control-points.md §4.5.CP-026:
//
//	The Budget counter state MUST be internal to the handler. It MUST NOT be
//	written to any durable store other than through the typed budget_accrual,
//	budget_warning, and budget_exhausted events. Cross-subsystem reads of the
//	counter MUST go through the event bus per [event-model.md §4.3]; there is
//	no GetBudgetCounter() surface.
//
// budgetCounterState is unexported by design — no other package can hold or
// read a raw counter value directly. All observable counter changes are
// surfaced exclusively through the event payloads returned by Accrue and
// CheckDispatch.
//
// Refs: hk-a8bg.25

// budgetCounterState is the internal per-run, per-budget accrual counter.
// It is intentionally unexported: the counter value is never readable by
// cross-subsystem callers. The handler subsystem owns this type exclusively,
// and all observable state changes are expressed as typed event payloads per
// CP-026.
type budgetCounterState struct {
	runID            RunID
	budgetRef        BudgetRef
	limit            int64
	warningThreshold float64
	accrued          int64
	warnEmitted      bool
}

// newBudgetCounterState constructs a fresh counter starting at zero.
//
// For in-flight runs after a daemon restart, the caller MUST rehydrate the
// counter from budget_accrual event replay before calling Accrue — starting at
// zero when prior accruals exist in the JSONL log violates CP-026a.
func newBudgetCounterState(runID RunID, budgetRef BudgetRef, limit int64, warningThreshold float64) budgetCounterState {
	return budgetCounterState{
		runID:            runID,
		budgetRef:        budgetRef,
		limit:            limit,
		warningThreshold: warningThreshold,
	}
}

// BudgetAccrualOutcome carries the event payloads produced by one Accrue call.
// All non-nil payloads MUST be emitted to the event bus — they are the sole
// durable record of the counter state change per CP-026.
type BudgetAccrualOutcome struct {
	// Accrual is always set — the handler emits a budget_accrual event for
	// every chunk per CP-024.
	Accrual BudgetAccrualPayload

	// Warning is set the first time cumulative accrual crosses the warning
	// threshold per CP-025. It is nil on every subsequent call.
	Warning *BudgetWarningPayload
}

// Accrue records delta cost units against the counter and returns the event
// payloads that MUST be emitted to the event bus. The counter is mutated in
// place; only the returned payloads carry observable state per CP-026.
//
// The caller is responsible for pre-dispatch exhaustion checks via
// CheckDispatch before calling Accrue; Accrue does not enforce the ceiling.
func (s *budgetCounterState) Accrue(
	sessionID SessionID,
	costBasis CostBasis,
	chunkIndex *int,
	delta int64,
) BudgetAccrualOutcome {
	s.accrued += delta

	out := BudgetAccrualOutcome{
		Accrual: BudgetAccrualPayload{
			RunID:      s.runID,
			SessionID:  sessionID,
			ChunkIndex: chunkIndex,
			CostUnits:  float64(delta),
			CostBasis:  costBasis,
		},
	}

	if !s.warnEmitted {
		if warnPayload, fired := CheckBudgetWarningThreshold(
			s.runID,
			s.budgetRef,
			s.limit,
			s.warningThreshold,
			float64(s.accrued),
		); fired {
			s.warnEmitted = true
			out.Warning = &warnPayload
		}
	}

	return out
}

// CheckDispatch evaluates whether a pending dispatch is admissible under the
// current counter state. Returns (payload, true) when DENIED — the caller
// MUST emit the payload as a budget_exhausted event and MUST NOT launch the
// handler. Returns (zero, false) when ADMITTED.
//
// This is the only path through which the counter's remaining allowance
// influences dispatch decisions; there is no GetBudgetCounter() alternative.
func (s *budgetCounterState) CheckDispatch(attemptedCost float64) (BudgetExhaustedEventPayload, bool) {
	return CheckBudgetAtDispatch(s.runID, s.budgetRef, s.limit, s.accrued, attemptedCost)
}

// RehydrateAccrual replays a single budget_accrual delta into the counter
// during daemon-restart rehydration per CP-026a. It does NOT emit any event
// payload — replay is read-only reconstruction, not a new charge. Callers
// MUST invoke this only during the rehydration pass (before any dispatch).
func (s *budgetCounterState) RehydrateAccrual(delta int64) {
	s.accrued += delta
}
