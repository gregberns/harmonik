package core

import (
	"testing"

	"github.com/google/uuid"
)

func budgetExhaustedPayloadFixture(t *testing.T) BudgetExhaustedPayload {
	t.Helper()
	return BudgetExhaustedPayload{
		RunID:          RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:     mustParseWorkflowID(t, uuid.Must(uuid.NewV7()).String()),
		BudgetSeconds:  300,
		ElapsedSeconds: 301,
	}
}

func TestBudgetExhaustedPayloadValid_AllValid(t *testing.T) {
	t.Parallel()

	p := budgetExhaustedPayloadFixture(t)
	if !p.Valid() {
		t.Error("Valid() = false for fully-populated payload, want true")
	}
}

func TestBudgetExhaustedPayloadValid_ZeroRunID(t *testing.T) {
	t.Parallel()

	p := budgetExhaustedPayloadFixture(t)
	p.RunID = RunID(uuid.Nil)
	if p.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

func TestBudgetExhaustedPayloadValid_ZeroWorkflowID(t *testing.T) {
	t.Parallel()

	p := budgetExhaustedPayloadFixture(t)
	p.WorkflowID = WorkflowID("")
	if p.Valid() {
		t.Error("Valid() = true with zero WorkflowID, want false")
	}
}

func TestBudgetExhaustedPayloadValid_ZeroBudgetSeconds(t *testing.T) {
	t.Parallel()

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

func TestBudgetExhaustedPayloadValid_ZeroElapsedSeconds(t *testing.T) {
	t.Parallel()

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

func TestBudgetExhaustedPayloadValid_ElapsedExceedsBudget(t *testing.T) {
	t.Parallel()

	p := budgetExhaustedPayloadFixture(t)
	p.BudgetSeconds = 100
	p.ElapsedSeconds = 200
	if !p.Valid() {
		t.Error("Valid() = false when ElapsedSeconds > BudgetSeconds, want true")
	}
}
