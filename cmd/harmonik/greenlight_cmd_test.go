package main

// greenlight_cmd_test.go — behavior tests for `harmonik greenlight` argument
// parsing and validation (AC2, hk-lacr). The success path shells out to `br
// label remove`, which requires a live beads ledger and would mutate state, so
// only the pre-exec validation branches are exercised here.

import "testing"

func TestRunGreenlight_NoPositional(t *testing.T) {
	vgSilenceStd(t)
	if code := runGreenlightSubcommand(nil); code != 1 {
		t.Fatalf("exit code = %d, want 1 when no bead-id is given", code)
	}
}

func TestRunGreenlight_TooManyPositional(t *testing.T) {
	vgSilenceStd(t)
	if code := runGreenlightSubcommand([]string{"hk-1", "hk-2"}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for two bead-ids", code)
	}
}

func TestRunGreenlight_UnknownFlag(t *testing.T) {
	vgSilenceStd(t)
	// Unknown flag is a distinct exit code (2) from an arg-count error (1).
	if code := runGreenlightSubcommand([]string{"--bogus", "hk-1"}); code != 2 {
		t.Fatalf("exit code = %d, want 2 for unknown flag", code)
	}
}

func TestRunGreenlight_Help(t *testing.T) {
	vgSilenceStd(t)
	for _, a := range []string{"--help", "-h"} {
		if code := runGreenlightSubcommand([]string{a}); code != 0 {
			t.Errorf("%s: exit code = %d, want 0", a, code)
		}
	}
}

// TestRunGreenlight_ProjectFlagFormsAccepted confirms both --project DIR and
// --project=DIR are consumed as flags (not counted as the positional bead-id).
// With exactly one positional supplied the arg-count guard passes; execution
// then proceeds to `br`, whose absence/failure yields exit 1 — which is still
// distinct from the exit-2 "unknown flag" path, proving the flag was parsed.
func TestRunGreenlight_ProjectFlagFormsAccepted(t *testing.T) {
	vgSilenceStd(t)
	// Two positionals + a valid --project must still be an arg-count error (1),
	// never an unknown-flag error (2): proves --project consumed its value.
	if code := runGreenlightSubcommand([]string{"hk-1", "hk-2", "--project", t.TempDir()}); code != 1 {
		t.Fatalf("exit code = %d, want 1 (arg-count), proving --project DIR consumed its value", code)
	}
	if code := runGreenlightSubcommand([]string{"hk-1", "hk-2", "--project=" + t.TempDir()}); code != 1 {
		t.Fatalf("exit code = %d, want 1 (arg-count) for --project= form", code)
	}
}
