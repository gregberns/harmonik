package core

type budgetCounterState struct {
	runID            RunID
	budgetRef        BudgetRef
	limit            int64
	warningThreshold float64
	accrued          int64
	warnEmitted      bool
}

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
