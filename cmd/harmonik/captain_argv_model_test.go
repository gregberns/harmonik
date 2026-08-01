package main

import "testing"

func TestCaptainLaunchArgvIncludesPinnedModel(t *testing.T) {
	t.Parallel()
	cmd := buildCaptainTmuxCmd("captain", "captain-session", "session-id", "project")
	assertArgvHasValue(t, cmd.Args, "--model", captainModel)
}

func TestCaptainRespawnArgvIncludesPinnedModel(t *testing.T) {
	t.Parallel()
	cmd := buildCaptainRespawnWindowCmd("captain", "captain-session:agent", "session-id", "project")
	assertArgvHasValue(t, cmd.Args, "--model", captainModel)
}

func assertArgvHasValue(t *testing.T, argv []string, flag, want string) {
	t.Helper()
	for i, arg := range argv {
		if arg != flag {
			continue
		}
		if i+1 == len(argv) {
			t.Fatalf("%s has no value in argv %q", flag, argv)
		}
		if got := argv[i+1]; got != want {
			t.Fatalf("%s value = %q, want %q in argv %q", flag, got, want, argv)
		}
		return
	}
	t.Fatalf("argv %q has no %s", argv, flag)
}
