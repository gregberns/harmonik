package brcli

import (
	"errors"
	"testing"
)

func TestClassifyClaimRefusalUsesStructuredTerminalResult(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   error
	}{
		{"dependency refusal", "new prefix: cannot claim blocked issue hk-a: new suffix", ErrClaimDependencyBlocked},
		{"assigned refusal", "new prefix: issue hk-a already assigned to crew-a: new suffix", ErrClaimAlreadyAssigned},
		{"unrelated use of blocked", "database operation blocked by lock timeout", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			terminalErr := &TerminalWriteError{
				Op:     "claim",
				Result: Result{Stderr: []byte(tc.stderr), ExitCode: 4, BrErr: BrSchemaMismatch},
			}
			got := classifyClaimRefusal(terminalErr)
			if tc.want != nil && !errors.Is(got, tc.want) {
				t.Fatalf("errors.Is(%v) = false, want %v", got, tc.want)
			}
			if tc.want == nil && (errors.Is(got, ErrClaimDependencyBlocked) || errors.Is(got, ErrClaimAlreadyAssigned)) {
				t.Fatalf("unrelated failure was classified as a claim refusal: %v", got)
			}
			var preserved *TerminalWriteError
			if !errors.As(got, &preserved) {
				t.Fatalf("structured terminal result was lost: %v", got)
			}
			if preserved.Result.ExitCode != 4 || preserved.Result.BrErr != BrSchemaMismatch || string(preserved.Result.Stderr) != tc.stderr {
				t.Fatalf("preserved result = %+v", preserved.Result)
			}
		})
	}
}
