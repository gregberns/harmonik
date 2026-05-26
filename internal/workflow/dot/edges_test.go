package dot

// edges_test.go — tests for the edge-condition evaluator (EvalCondition).
//
// Spec coverage:
//   WG-013  — restricted equality dialect; &&-conjunction
//   WG-014  — LHS whitelist; out-of-whitelist → ErrDeterministic
//   WG-015  — RHS literal types (string comparison)
//   WG-016  — dialect is distinct from guard predicates (evaluator is narrow)
//
// Tags: mechanism

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func ptrStr(s string) *string { return &s }

func ptrFC(fc core.FailureClass) *core.FailureClass { return &fc }

// makeOutcome builds a minimal valid Outcome with Status=SUCCESS and no extras.
func makeOutcome(status core.OutcomeStatus) core.Outcome {
	return core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
}

// ── nil condition (unconditional edge) ───────────────────────────────────────

func TestEvalCondition_NilIsAlwaysTrue(t *testing.T) {
	ok, err := EvalCondition(nil, makeOutcome(core.OutcomeStatusSuccess), nil)
	if err != nil || !ok {
		t.Fatalf("nil condition: want (true, nil), got (%v, %v)", ok, err)
	}
}

// ── outcome.status ───────────────────────────────────────────────────────────

func TestEvalCondition_OutcomeStatus_Match(t *testing.T) {
	cond := &Condition{Clauses: []Equality{{LHS: "outcome.status", Op: "==", RHS: "SUCCESS"}}}
	ok, err := EvalCondition(cond, makeOutcome(core.OutcomeStatusSuccess), nil)
	if err != nil || !ok {
		t.Fatalf("want (true, nil), got (%v, %v)", ok, err)
	}
}

func TestEvalCondition_OutcomeStatus_NoMatch(t *testing.T) {
	cond := &Condition{Clauses: []Equality{{LHS: "outcome.status", Op: "==", RHS: "FAIL"}}}
	ok, err := EvalCondition(cond, makeOutcome(core.OutcomeStatusSuccess), nil)
	if err != nil || ok {
		t.Fatalf("want (false, nil), got (%v, %v)", ok, err)
	}
}

func TestEvalCondition_OutcomeStatus_NotEqual(t *testing.T) {
	cond := &Condition{Clauses: []Equality{{LHS: "outcome.status", Op: "!=", RHS: "FAIL"}}}
	ok, err := EvalCondition(cond, makeOutcome(core.OutcomeStatusSuccess), nil)
	if err != nil || !ok {
		t.Fatalf("want (true, nil), got (%v, %v)", ok, err)
	}
}

// ── outcome.preferred_label ──────────────────────────────────────────────────

func TestEvalCondition_PreferredLabel_Match(t *testing.T) {
	o := makeOutcome(core.OutcomeStatusSuccess)
	o.PreferredLabel = ptrStr("APPROVE")
	cond := &Condition{Clauses: []Equality{{LHS: "outcome.preferred_label", Op: "==", RHS: "APPROVE"}}}
	ok, err := EvalCondition(cond, o, nil)
	if err != nil || !ok {
		t.Fatalf("want (true, nil), got (%v, %v)", ok, err)
	}
}

func TestEvalCondition_PreferredLabel_Nil_NoMatch(t *testing.T) {
	o := makeOutcome(core.OutcomeStatusSuccess)
	cond := &Condition{Clauses: []Equality{{LHS: "outcome.preferred_label", Op: "==", RHS: "APPROVE"}}}
	ok, err := EvalCondition(cond, o, nil)
	if err != nil || ok {
		t.Fatalf("nil preferred_label: want (false, nil), got (%v, %v)", ok, err)
	}
}

// ── outcome.failure_class ────────────────────────────────────────────────────

func TestEvalCondition_FailureClass_Match(t *testing.T) {
	o := core.Outcome{Status: core.OutcomeStatusFail, Kind: core.OutcomeKindDefault, FailureClass: ptrFC(core.FailureClassTransient)}
	cond := &Condition{Clauses: []Equality{{LHS: "outcome.failure_class", Op: "==", RHS: "transient"}}}
	ok, err := EvalCondition(cond, o, nil)
	if err != nil || !ok {
		t.Fatalf("want (true, nil), got (%v, %v)", ok, err)
	}
}

func TestEvalCondition_FailureClass_Nil_NoMatch(t *testing.T) {
	o := makeOutcome(core.OutcomeStatusSuccess)
	cond := &Condition{Clauses: []Equality{{LHS: "outcome.failure_class", Op: "==", RHS: "transient"}}}
	ok, err := EvalCondition(cond, o, nil)
	if err != nil || ok {
		t.Fatalf("nil failure_class: want (false, nil), got (%v, %v)", ok, err)
	}
}

// ── outcome.kind ─────────────────────────────────────────────────────────────

func TestEvalCondition_OutcomeKind_Match(t *testing.T) {
	o := makeOutcome(core.OutcomeStatusSuccess)
	cond := &Condition{Clauses: []Equality{{LHS: "outcome.kind", Op: "==", RHS: "default"}}}
	ok, err := EvalCondition(cond, o, nil)
	if err != nil || !ok {
		t.Fatalf("want (true, nil), got (%v, %v)", ok, err)
	}
}

// ── context.<key> ────────────────────────────────────────────────────────────

func TestEvalCondition_ContextKey_Match(t *testing.T) {
	ctx := map[string]string{"pr_url": "https://example.com/1"}
	cond := &Condition{Clauses: []Equality{{LHS: "context.pr_url", Op: "==", RHS: "https://example.com/1"}}}
	ok, err := EvalCondition(cond, makeOutcome(core.OutcomeStatusSuccess), ctx)
	if err != nil || !ok {
		t.Fatalf("want (true, nil), got (%v, %v)", ok, err)
	}
}

func TestEvalCondition_ContextKey_Missing_NoMatch(t *testing.T) {
	cond := &Condition{Clauses: []Equality{{LHS: "context.absent", Op: "==", RHS: "x"}}}
	ok, err := EvalCondition(cond, makeOutcome(core.OutcomeStatusSuccess), nil)
	if err != nil || ok {
		t.Fatalf("missing context key: want (false, nil), got (%v, %v)", ok, err)
	}
}

// ── &&-conjunction ───────────────────────────────────────────────────────────

func TestEvalCondition_Conjunction_BothTrue(t *testing.T) {
	o := core.Outcome{Status: core.OutcomeStatusFail, Kind: core.OutcomeKindDefault, FailureClass: ptrFC(core.FailureClassTransient)}
	cond := &Condition{Clauses: []Equality{
		{LHS: "outcome.status", Op: "==", RHS: "FAIL"},
		{LHS: "outcome.failure_class", Op: "==", RHS: "transient"},
	}}
	ok, err := EvalCondition(cond, o, nil)
	if err != nil || !ok {
		t.Fatalf("conjunction all-true: want (true, nil), got (%v, %v)", ok, err)
	}
}

func TestEvalCondition_Conjunction_OneFalse(t *testing.T) {
	o := core.Outcome{Status: core.OutcomeStatusFail, Kind: core.OutcomeKindDefault, FailureClass: ptrFC(core.FailureClassStructural)}
	cond := &Condition{Clauses: []Equality{
		{LHS: "outcome.status", Op: "==", RHS: "FAIL"},
		{LHS: "outcome.failure_class", Op: "==", RHS: "transient"},
	}}
	ok, err := EvalCondition(cond, o, nil)
	if err != nil || ok {
		t.Fatalf("conjunction one-false: want (false, nil), got (%v, %v)", ok, err)
	}
}

// ── out-of-whitelist LHS → ErrDeterministic ──────────────────────────────────

func TestEvalCondition_OutOfWhitelistLHS_ErrDeterministic(t *testing.T) {
	cond := &Condition{Clauses: []Equality{{LHS: "outcome.notes", Op: "==", RHS: "anything"}}}
	ok, err := EvalCondition(cond, makeOutcome(core.OutcomeStatusSuccess), nil)
	if !errors.Is(err, ErrDeterministic) {
		t.Fatalf("out-of-whitelist LHS: want ErrDeterministic, got err=%v ok=%v", err, ok)
	}
	if ok {
		t.Fatalf("out-of-whitelist LHS: matched must be false")
	}
}

func TestEvalCondition_ContextEmptyKey_ErrDeterministic(t *testing.T) {
	cond := &Condition{Clauses: []Equality{{LHS: "context.", Op: "==", RHS: "x"}}}
	_, err := EvalCondition(cond, makeOutcome(core.OutcomeStatusSuccess), nil)
	if !errors.Is(err, ErrDeterministic) {
		t.Fatalf("empty context key: want ErrDeterministic, got %v", err)
	}
}

// ── determinism: equal inputs → equal outputs ────────────────────────────────

func TestEvalCondition_Determinism(t *testing.T) {
	o := makeOutcome(core.OutcomeStatusSuccess)
	cond := &Condition{Clauses: []Equality{{LHS: "outcome.status", Op: "==", RHS: "SUCCESS"}}}
	for i := 0; i < 5; i++ {
		ok, err := EvalCondition(cond, o, nil)
		if err != nil || !ok {
			t.Fatalf("iteration %d: want (true, nil), got (%v, %v)", i, ok, err)
		}
	}
}
