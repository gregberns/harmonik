package evalexpreval_test

import (
	"errors"
	"testing"

	evalexpreval "github.com/gregberns/harmonik/evaltasks/eval-expr-eval"
)

func TestEval(t *testing.T) {
	t.Parallel()

	t.Run("precedence_mul_over_add", func(t *testing.T) {
		t.Parallel()
		got, err := evalexpreval.Eval("2+3*4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 14 {
			t.Errorf("2+3*4 = %v, want 14", got)
		}
	})

	t.Run("parens_override_precedence", func(t *testing.T) {
		t.Parallel()
		got, err := evalexpreval.Eval("(2+3)*4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 20 {
			t.Errorf("(2+3)*4 = %v, want 20", got)
		}
	})

	t.Run("left_assoc_subtraction", func(t *testing.T) {
		t.Parallel()
		// 10-3-2 must evaluate left-to-right: (10-3)-2 = 5, not 10-(3-2) = 9.
		got, err := evalexpreval.Eval("10-3-2")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 5 {
			t.Errorf("10-3-2 = %v, want 5", got)
		}
	})

	t.Run("div_by_zero", func(t *testing.T) {
		t.Parallel()
		_, err := evalexpreval.Eval("6/0")
		if !errors.Is(err, evalexpreval.ErrDivisionByZero) {
			t.Errorf("6/0: err = %v, want ErrDivisionByZero", err)
		}
	})

	t.Run("unbalanced_paren", func(t *testing.T) {
		t.Parallel()
		_, err := evalexpreval.Eval("(1+2")
		if err == nil {
			t.Fatal("(1+2: expected parse error, got nil")
		}
	})

	t.Run("unary_minus", func(t *testing.T) {
		t.Parallel()
		got, err := evalexpreval.Eval("-5")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != -5 {
			t.Errorf("-5 = %v, want -5", got)
		}
	})
}
