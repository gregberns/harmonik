package core

import (
	"testing"

	"github.com/google/uuid"
)

// budgetExhaustedPayloadFixture returns a fully-populated BudgetExhaustedPayload
// with all required fields set to valid values. Tests mutate individual fields
// to probe Valid().
func budgetExhaustedPayloadFixture(t *testing.T) BudgetExhaustedPayload {
	t.Helper()
	return BudgetExhaustedPayload{
		RunID:          RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:     WorkflowID(uuid.Must(uuid.NewV7())),
		BudgetSeconds:  300,
		ElapsedSeconds: 301,
	}
}

// --- AllValid ---

func TestBudgetExhaustedPayloadValid_AllValid(t *testing.T) {
	t.Parallel()

	p := budgetExhaustedPayloadFixture(t)
	if !p.Valid() {
		t.Error("Valid() = false for fully-populated payload, want true")
	}
}

// --- RunID ---

func TestBudgetExhaustedPayloadValid_ZeroRunID(t *testing.T) {
	t.Parallel()

	p := budgetExhaustedPayloadFixture(t)
	p.RunID = RunID(uuid.Nil)
	if p.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

// --- WorkflowID ---

func TestBudgetExhaustedPayloadValid_ZeroWorkflowID(t *testing.T) {
	t.Parallel()

	p := budgetExhaustedPayloadFixture(t)
	p.WorkflowID = WorkflowID(uuid.Nil)
	if p.Valid() {
		t.Error("Valid() = true with zero WorkflowID, want false")
	}
}

// --- BudgetSeconds ---

func TestBudgetExhaustedPayloadValid_ZeroBudgetSeconds(t *testing.T) {
	t.Parallel()

	// Zero is allowed (non-negative).
	p := budgetExhaustedPayloadFixture(t)
	p.BudgetSeconds = 0
	if !p.Valid() {
		t.Error("Valid() = false with BudgetSeconds=0, want true (zero is a valid non-negative value)")
	}
}

func TestBudgetExhaustedPayloadValid_NegativeBudgetSeconds(t *testing.T) {
	t.Parallel()

	p := budgetExhaustedPayloadFixture(t)
	p.BudgetSeconds = -1
	if p.Valid() {
		t.Error("Valid() = true with negative BudgetSeconds, want false")
	}
}

// --- ElapsedSeconds ---

func TestBudgetExhaustedPayloadValid_ZeroElapsedSeconds(t *testing.T) {
	t.Parallel()

	// Zero is allowed (non-negative).
	p := budgetExhaustedPayloadFixture(t)
	p.ElapsedSeconds = 0
	if !p.Valid() {
		t.Error("Valid() = false with ElapsedSeconds=0, want true (zero is a valid non-negative value)")
	}
}

func TestBudgetExhaustedPayloadValid_NegativeElapsedSeconds(t *testing.T) {
	t.Parallel()

	p := budgetExhaustedPayloadFixture(t)
	p.ElapsedSeconds = -1
	if p.Valid() {
		t.Error("Valid() = true with negative ElapsedSeconds, want false")
	}
}

// --- ElapsedSeconds may exceed BudgetSeconds ---

func TestBudgetExhaustedPayloadValid_ElapsedExceedsBudget(t *testing.T) {
	t.Parallel()

	// elapsed > budget is the normal exhaustion case; Valid() does not
	// enforce a budget <= elapsed ordering constraint.
	p := budgetExhaustedPayloadFixture(t)
	p.BudgetSeconds = 100
	p.ElapsedSeconds = 200
	if !p.Valid() {
		t.Error("Valid() = false when ElapsedSeconds > BudgetSeconds, want true")
	}
}
