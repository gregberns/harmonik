package main

import (
	"flag"
	"io"
	"testing"
)

// hk-t1wd / hk-5da7 — KEEPER parser-parity. These tests pin the FLAG-ONLY
// contract (hk-5da7) shared by the keeper subcommands in keeper_cmd.go (watcher,
// set-dispatching, clear-dispatching, restart-now, ping): the target agent is
// supplied ONLY via --agent; ANY positional argument is rejected with exit 2
// (positionals were the recurring restart-now failure mode). Every pre-existing
// recognized flag still parses, and an unrecognized flag also exits 2.

// TestKeeperMarkerArgsParity exercises the real shared parser used by
// set-dispatching, clear-dispatching, and restart-now. parseKeeperMarkerArgs
// performs no file I/O (only flag parsing + os.Getwd), so it is safe to call
// directly across the resolution matrix.
func TestKeeperMarkerArgsParity(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantAgent string
		wantCode  int
	}{
		{"flag", []string{"--agent", "alpha"}, "alpha", 0},
		{"positional-rejected", []string{"alpha"}, "", 2},
		{"flag-then-stray-positional-rejected", []string{"--agent", "flagwin", "posval"}, "", 2},
		{"project-flag-kept-then-positional-rejected", []string{"--project", "/tmp/x", "beta"}, "", 2},
		{"project-flag-kept-then-agent-flag", []string{"--project", "/tmp/x", "--agent", "beta"}, "beta", 0},
		{"missing-agent", []string{"--project", "/tmp/x"}, "", 1},
		{"leading-dash-bogus", []string{"--bogus"}, "", 2},
		{"positional-then-bogus", []string{"alpha", "--bogus"}, "", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent, _, code := parseKeeperMarkerArgs("keeper test", tc.args)
			if agent != tc.wantAgent || code != tc.wantCode {
				t.Fatalf("args %v: got (agent=%q code=%d), want (agent=%q code=%d)",
					tc.args, agent, code, tc.wantAgent, tc.wantCode)
			}
		})
	}
}

// TestKeeperSubcommandsRejectBogusFlag asserts that an unrecognized leading-dash
// token exits 2 at the real command boundary for all subcommands. Every
// command short-circuits on the stray flag BEFORE any file I/O, so these calls
// have no side effects.
func TestKeeperSubcommandsRejectBogusFlag(t *testing.T) {
	runners := map[string]func([]string) int{
		"set-dispatching":   runKeeperSetDispatching,
		"clear-dispatching": runKeeperClearDispatching,
		"restart-now":       runKeeperRestartNow,
		"watcher":           runKeeperSubcommand,
	}
	for name, run := range runners {
		run := run
		t.Run(name+"/leading", func(t *testing.T) {
			if code := run([]string{"--bogus"}); code != 2 {
				t.Fatalf("%s --bogus: want exit 2, got %d", name, code)
			}
		})
		t.Run(name+"/trailing-after-positional", func(t *testing.T) {
			if code := run([]string{"someagent", "--bogus"}); code != 2 {
				t.Fatalf("%s someagent --bogus: want exit 2, got %d", name, code)
			}
		})
	}
}

// TestKeeperWatcherRecognizedFlagsStillParse guards the ENUMERATE-AND-KEEP
// invariant: every pre-existing watcher flag must still parse. With all flags
// present but no agent supplied the watcher returns 1 (missing agent) — NOT 2,
// which would indicate a flag was dropped/unrecognized.
func TestKeeperWatcherRecognizedFlagsStillParse(t *testing.T) {
	args := []string{
		"--tmux", "sess:0",
		"--warn-pct", "85",
		"--act-pct", "95",
		"--window-size", "100000",
		"--warn-abs-tokens", "111",
		"--act-abs-tokens", "222",
		"--respawn-cmd", "echo hi",
	}
	if code := runKeeperSubcommand(args); code != 1 {
		t.Fatalf("watcher with all recognized flags but no agent: want exit 1, got %d", code)
	}
}

// TestKeeperWatcherAgentResolution proves the watcher resolves an agent ONLY
// from --agent (flag-only, hk-5da7): a positional argument is rejected with exit
// 2. Uses the shared resolveKeeperAgent helper and a flag set that mirrors the
// watcher's registration, avoiding the full watcher (which would acquire a lock).
func TestKeeperWatcherAgentResolution(t *testing.T) {
	build := func(args []string) (*flag.FlagSet, string) {
		fs := flag.NewFlagSet("keeper", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		agent := fs.String("agent", "", "")
		fs.String("tmux", "", "")
		if err := fs.Parse(args); err != nil {
			t.Fatalf("parse %v: %v", args, err)
		}
		return fs, *agent
	}
	cases := []struct {
		args     []string
		want     string
		wantCode int
	}{
		{[]string{"--agent", "w1"}, "w1", 0},
		{[]string{"--tmux", "s:0", "--agent", "w1"}, "w1", 0},
		{[]string{"w1"}, "", 2},                           // positional rejected
		{[]string{"--agent", "flagwin", "posval"}, "", 2}, // stray positional rejected
	}
	for _, tc := range cases {
		fs, agentFlag := build(tc.args)
		got, code := resolveKeeperAgent(fs, "keeper", agentFlag)
		if got != tc.want || code != tc.wantCode {
			t.Fatalf("args %v: got (%q,%d), want (%q,%d)", tc.args, got, code, tc.want, tc.wantCode)
		}
	}
}
