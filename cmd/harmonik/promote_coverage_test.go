package main

// promote_coverage_test.go — small pure-logic gaps in `harmonik promote` not
// already covered by promote_cmd_hkpk3p1_test.go / promote_cmd_b2a_subsystem_test.go:
// the top-level dispatcher's help branch and its parse-error exit code. The
// push/PR execution paths (git worktree, cherry-pick, gh) require a live repo
// and are covered by the existing subsystem tests, not here.

import (
	"testing"
)

func TestRunPromoteSubcommand_HelpAndParseError(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no args prints usage", nil, 0},
		{"help long", []string{"--help"}, 0},
		{"help short", []string{"-h"}, 0},
		// Unknown flag surfaces as a parse error → exit 1, before any git shell-out.
		{"unknown flag", []string{"--bogus"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := runPromoteSubcommand(tc.args); got != tc.want {
				t.Errorf("runPromoteSubcommand(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}
