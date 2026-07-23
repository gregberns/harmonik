package main

// goalkeeper_cmd_test.go — behavior tests for `harmonik goal-keeper` argument
// parsing (flywheel V6, hk-owz1). The success path shells out to `harmonik
// comms log` and reads/writes goal-state, which requires a live comms ledger;
// only the flag-parse and validation branches are exercised here.

import "testing"

func TestRunGoalkeeper_UnknownFlag(t *testing.T) {
	vgSilenceStd(t)
	// flag.ContinueOnError returns a parse error => exit 1.
	if code := runGoalkeeperSubcommand([]string{"--bogus"}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for unknown flag", code)
	}
}

func TestRunGoalkeeper_UnexpectedPositional(t *testing.T) {
	vgSilenceStd(t)
	if code := runGoalkeeperSubcommand([]string{"extra"}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for unexpected positional argument", code)
	}
}

func TestRunGoalkeeper_Help(t *testing.T) {
	vgSilenceStd(t)
	// flag's -h/--help produce flag.ErrHelp, which the command maps to exit 0.
	for _, a := range []string{"-h", "--help"} {
		if code := runGoalkeeperSubcommand([]string{a}); code != 0 {
			t.Errorf("%s: exit code = %d, want 0", a, code)
		}
	}
}
