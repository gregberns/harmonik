package main

// supervise_cmd_dispatch_test.go — dispatch-layer tests for
// runSuperviseSubcommand (supervise_cmd.go). Covers ONLY the pure verb-routing
// surface: the help/empty-verb path (exit 0 + usage banner) and the
// unrecognised-verb path (exit 2). The real verbs (start/stop/status/ps/attach/
// restart/logs/pause/resume/reap/_shim) delegate to the supervisecmd package,
// which spawns processes / touches tmux / dials the daemon; those are NOT
// exercised here — they belong to the supervise/ package tests and the existing
// integration tests. No t.Parallel(): captureStdoutDuring/vgSilenceStd mutate
// process globals.

import (
	"strings"
	"testing"
)

// TestRunSuperviseSubcommand_HelpAndEmptyReturnZero verifies the three
// non-verb inputs ("", "--help", "-h") all print the top-level usage banner and
// return exit 0, without touching any real verb handler.
func TestRunSuperviseSubcommand_HelpAndEmptyReturnZero(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"empty", []string{}},
		{"help-long", []string{"--help"}},
		{"help-short", []string{"-h"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var code int
			out := captureStdoutDuring(t, func() {
				code = runSuperviseSubcommand(tc.args)
			})
			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			if !strings.Contains(out, "harmonik supervise — manage the supervisor") {
				t.Fatalf("usage banner missing from stdout:\n%s", out)
			}
			if !strings.Contains(out, "VERBS") {
				t.Fatalf("VERBS section missing from usage:\n%s", out)
			}
		})
	}
}

// TestRunSuperviseSubcommand_UnrecognisedVerbReturnsTwo verifies an unknown
// verb returns exit 2 (the documented "unrecognised verb" code) and names the
// offending verb on stderr.
func TestRunSuperviseSubcommand_UnrecognisedVerbReturnsTwo(t *testing.T) {
	vgSilenceStd(t)
	code := runSuperviseSubcommand([]string{"frobnicate", "--flag"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}
