package main

import "testing"

func TestRunGoalkeeper_UnknownFlag(t *testing.T) {
	vgSilenceStd(t)
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
	for _, a := range []string{"-h", "--help"} {
		if code := runGoalkeeperSubcommand([]string{a}); code != 0 {
			t.Errorf("%s: exit code = %d, want 0", a, code)
		}
	}
}
