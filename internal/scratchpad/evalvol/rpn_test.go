package evalvol

import (
	"errors"
	"testing"
)

func TestEvalRPN(t *testing.T) {
	tests := []struct {
		name    string
		tokens  []string
		want    float64
		wantErr error
	}{
		{name: "single number", tokens: []string{"42"}, want: 42},
		{name: "addition", tokens: []string{"3", "4", "+"}, want: 7},
		{name: "subtraction", tokens: []string{"10", "3", "-"}, want: 7},
		{name: "multiplication", tokens: []string{"3", "4", "*"}, want: 12},
		{name: "division", tokens: []string{"10", "4", "/"}, want: 2.5},
		{name: "chained ops", tokens: []string{"2", "3", "4", "*", "+"}, want: 14},
		{name: "negative result", tokens: []string{"1", "5", "-"}, want: -4},
		{name: "divide by zero", tokens: []string{"1", "0", "/"}, wantErr: ErrDivideByZero},
		{name: "malformed invalid token", tokens: []string{"a", "b", "+"}, wantErr: ErrMalformed},
		{name: "malformed too few operands", tokens: []string{"1", "+"}, wantErr: ErrMalformed},
		{name: "malformed leftover values", tokens: []string{"1", "2", "3", "+"}, wantErr: ErrMalformed},
		{name: "empty input", tokens: []string{}, wantErr: ErrMalformed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EvalRPN(tc.tokens)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("EvalRPN(%v) error = %v, want %v", tc.tokens, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("EvalRPN(%v) unexpected error: %v", tc.tokens, err)
			}
			if got != tc.want {
				t.Fatalf("EvalRPN(%v) = %v, want %v", tc.tokens, got, tc.want)
			}
		})
	}
}
