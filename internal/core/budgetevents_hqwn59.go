package core

import "github.com/google/uuid"

// budgetevents_hqwn59.go — event-bus payload types for §8.4 budget lifecycle
// events covered by this implementer wave (hqwn59b):
//   - budget_warning    (§8.4.1)
//   - budget_accrual    (§8.4.2)
//   - budget_exhausted  (§8.4.3)
//
// Spec ref: specs/event-model.md §8.4.
// Bead refs: hk-hqwn.59.34, hk-hqwn.59.35, hk-hqwn.59.36.

// BudgetWarningPayload is the typed event payload for the budget_warning event
// (event-model.md §8.4.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — budget observability; the orchestrator uses
// this to surface warnings to the operator per control-points.md §4.5).
//
// Emitted by the agent-runner (S04) when the budget consumption crosses the
// configured warning threshold (default 80% per control-points.md §4.5.CP-024b).
//
// # Payload fields (event-model.md §8.4.1)
//
//   - run_id             — the run in whose context the budget warning fired
//   - session_id         — optional handler-assigned session identifier (nil for non-session-scoped warnings)
//   - budget_ref         — name of the budget that triggered the warning
//   - threshold_fraction — the fraction at which the warning fired (e.g., 0.8 for 80%)
//   - remaining          — remaining budget units at warning time
type BudgetWarningPayload struct {
	// RunID is the run in whose context the budget warning fired.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// SessionID is the optional handler-assigned session identifier.
	// Corresponds to session_id? in §8.4.1. Nil for non-session-scoped
	// budget warnings (e.g., pre-dispatch budget checks).
	SessionID *SessionID `json:"session_id,omitempty"`

	// BudgetRef names the budget that triggered the warning.
	// Required; must be a valid (non-empty) BudgetRef.
	BudgetRef BudgetRef `json:"budget_ref"`

	// ThresholdFraction is the fraction at which this warning was configured
	// to fire (e.g., 0.8 for 80%). Required (must be in (0, 1]).
	ThresholdFraction float64 `json:"threshold_fraction"`

	// Remaining is the remaining budget units at warning time.
	// Required (must be >= 0).
	Remaining float64 `json:"remaining"`
}

// Valid reports whether p is a well-formed BudgetWarningPayload.
//
// Rules per event-model.md §8.4.1:
//   - RunID must not be uuid.Nil.
//   - BudgetRef must be valid (non-empty).
//   - ThresholdFraction must be in (0, 1].
//   - Remaining must be >= 0.
func (p BudgetWarningPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.BudgetRef.Valid() {
		return false
	}
	if p.ThresholdFraction <= 0 || p.ThresholdFraction > 1 {
		return false
	}
	if p.Remaining < 0 {
		return false
	}
	return true
}

// BudgetAccrualPayload is the typed event payload for the budget_accrual event
// (event-model.md §8.4.2).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: L (lossy-tail-ok — per-chunk accrual; the improvement-loop
// subsystem rehydrates counters from this event stream per control-points.md §4.5
// hk-a8bg.26, but chunk-level loss is tolerable under EV-017/EV-INV-002).
//
// Emitted by the handler subprocess via the daemon watcher for each billable
// chunk. cost_units and cost_basis together allow attribution per
// control-points.md §4.5.
//
// # Payload fields (event-model.md §8.4.2)
//
//   - run_id      — the run in whose context the accrual occurred
//   - session_id  — handler-assigned session identifier
//   - chunk_index — optional zero-based chunk counter correlating with agent_output_chunk
//   - cost_units  — billable cost units consumed by this chunk
//   - cost_basis  — the basis string identifying the cost model (e.g., "input_tokens")
type BudgetAccrualPayload struct {
	// RunID is the run in whose context the accrual occurred.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// SessionID is the handler-assigned session identifier. Required (non-empty).
	// UUIDv7 per handler-contract.md §4.1; opaque to non-handler consumers.
	SessionID SessionID `json:"session_id"`

	// ChunkIndex is the optional zero-based chunk counter correlating this accrual
	// event with the corresponding agent_output_chunk event (§8.3.3).
	// Corresponds to chunk_index? in §8.4.2. Nil when no chunk correlation is available.
	ChunkIndex *int `json:"chunk_index,omitempty"`

	// CostUnits is the number of billable cost units consumed by this chunk.
	// Required (must be >= 0).
	CostUnits float64 `json:"cost_units"`

	// CostBasis is the cost model identifier (e.g., "input_tokens", "output_tokens").
	// Required (non-empty).
	//
	// TODO(hk-hqwn.73): hoist to typed CostBasis alias when that type lands.
	CostBasis string `json:"cost_basis"`
}

// Valid reports whether p is a well-formed BudgetAccrualPayload.
//
// Rules per event-model.md §8.4.2:
//   - RunID must not be uuid.Nil.
//   - SessionID must be non-empty.
//   - CostUnits must be >= 0.
//   - CostBasis must be non-empty.
func (p BudgetAccrualPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.SessionID == "" {
		return false
	}
	if p.CostUnits < 0 {
		return false
	}
	if p.CostBasis == "" {
		return false
	}
	return true
}

// BudgetExhaustedEventPayload is the typed event payload for the budget_exhausted
// event (event-model.md §8.4.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — budget lifecycle; the orchestrator uses this
// to apply the exhaustion protocol per control-points.md §4.5 hk-sx9r.67 and
// enforcement per hk-a8bg.22).
//
// Emitted by the agent-runner (S04) when the budget ceiling is reached and
// the dispatch is halted per control-points.md §4.5.
//
// Note: this type is named BudgetExhaustedEventPayload to avoid collision with
// the existing BudgetExhaustedPayload (defined in budgetexhaustedpayload.go)
// which is a different record shape used internally by the control-points subsystem.
//
// # Payload fields (event-model.md §8.4.3)
//
//   - run_id                 — the run in whose context budget was exhausted
//   - session_id             — optional handler-assigned session identifier
//   - budget_ref             — name of the budget that was exhausted
//   - attempted_dispatch_cost — cost of the dispatch attempt that was denied due to exhaustion
type BudgetExhaustedEventPayload struct {
	// RunID is the run in whose context the budget was exhausted.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// SessionID is the optional handler-assigned session identifier.
	// Corresponds to session_id? in §8.4.3. Nil for pre-dispatch exhaustion checks.
	SessionID *SessionID `json:"session_id,omitempty"`

	// BudgetRef names the budget that was exhausted.
	// Required; must be a valid (non-empty) BudgetRef.
	BudgetRef BudgetRef `json:"budget_ref"`

	// AttemptedDispatchCost is the cost of the dispatch attempt that was
	// denied due to budget exhaustion. Required (must be >= 0).
	AttemptedDispatchCost float64 `json:"attempted_dispatch_cost"`
}

// Valid reports whether p is a well-formed BudgetExhaustedEventPayload.
//
// Rules per event-model.md §8.4.3:
//   - RunID must not be uuid.Nil.
//   - BudgetRef must be valid (non-empty).
//   - AttemptedDispatchCost must be >= 0.
func (p BudgetExhaustedEventPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.BudgetRef.Valid() {
		return false
	}
	if p.AttemptedDispatchCost < 0 {
		return false
	}
	return true
}
