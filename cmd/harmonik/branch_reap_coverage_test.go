package main

// branch_reap_coverage_test.go — behavior tests for the pure-logic slices of
// `harmonik gc branches`: the flag/arg validation exit-code truth table (paths
// that return before lifecycle.ReapBranches shells out to git) and the
// `harmonik gc <verb>` dispatcher. Both entry points take explicit io.Writer
// sinks, so no os.Stdout capture is needed.
//
// The actual reap pass (lifecycle.ReapBranches) requires a live git repo with
// run/* branches and is NOT exercised here — see the report for the reason.

import (
	"bytes"
	"strings"
	"testing"
)

// TestRunBranchReapSubcommand_ParseExitCodes covers the paths that return
// before ReapBranches is invoked: --help, unknown-arg fail-closed, and an
// invalid --max-age duration.
func TestRunBranchReapSubcommand_ParseExitCodes(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		want      int
		stdoutHas string
		stderrHas string
	}{
		{"help long", []string{"--help"}, 0, "harmonik gc branches", ""},
		{"help short", []string{"-h"}, 0, "harmonik gc branches", ""},
		{"unknown arg fails closed", []string{"--dryrun"}, 1, "", "unknown argument"},
		{"invalid max-age", []string{"--max-age", "notaduration"}, 1, "", "invalid --max-age"},
		{"invalid max-age equals-form", []string{"--max-age=12parsecs"}, 1, "", "invalid --max-age"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := runBranchReapSubcommand(tc.args, &stdout, &stderr); got != tc.want {
				t.Errorf("runBranchReapSubcommand(%v) = %d, want %d", tc.args, got, tc.want)
			}
			if tc.stdoutHas != "" && !strings.Contains(stdout.String(), tc.stdoutHas) {
				t.Errorf("stdout missing %q; got:\n%s", tc.stdoutHas, stdout.String())
			}
			if tc.stderrHas != "" && !strings.Contains(stderr.String(), tc.stderrHas) {
				t.Errorf("stderr missing %q; got:\n%s", tc.stderrHas, stderr.String())
			}
		})
	}
}

// TestRunGCSubcommand_Dispatch covers the top-level `harmonik gc <verb>`
// dispatcher's help and unrecognised-verb branches. The "branches" verb path
// falls through to a git reap and is covered only for its parse errors above.
func TestRunGCSubcommand_Dispatch(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no verb prints top usage", nil, 0},
		{"help long", []string{"--help"}, 0},
		{"help short", []string{"-h"}, 0},
		{"unrecognised verb", []string{"widgets"}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := runGCSubcommand(tc.args); got != tc.want {
				t.Errorf("runGCSubcommand(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}
