package main

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
