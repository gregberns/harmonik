package main

// captain_keeper_hkigek_test.go — RED-then-GREEN coverage for wiring the
// `keeper enable` hooks into the bare `harmonik captain` launcher (hk-igek).
//
// The launcher reuses enableConfig + runKeeperEnable. These tests assert, via an
// injected keeper-enable seam (NO real ~/.claude/settings.json mutation), that:
//   - keeper enable is called by default, pre-launch, with the captain's name +
//     resolved project, and a settings path that ends in .claude/settings.json;
//   - --no-keeper skips the keeper call entirely but still launches tmux;
//   - a keeper-enable failure WARNS but does NOT block the tmux launch.
// Helpers carry the hkigek suffix per test-hygiene.

import (
	"io"
	"strings"
	"testing"
)

// captureKeeperHkigek returns a keeper-enable seam that records the config it is
// handed (and a call counter), returning the supplied rc. The captured config is
// read back through the returned pointers.
func captureKeeperHkigek(rc int) (fn keeperEnableFn, calls *int, got *enableConfig) {
	var n int
	var cfg enableConfig
	return func(c enableConfig, _, _ io.Writer) int {
		n++
		cfg = c
		return rc
	}, &n, &cfg
}

func TestCaptainLaunch_WiresKeeperByDefault_hkigek(t *testing.T) {
	run, captured := captureRunHkly0n()
	keeper, calls, gotCfg := captureKeeperHkigek(0)
	proj := t.TempDir()

	code := runCaptainLaunch([]string{"--project", proj}, run, keeper)
	if code != 0 {
		t.Fatalf("runCaptainLaunch exit = %d, want 0", code)
	}
	if *calls != 1 {
		t.Fatalf("keeper enable called %d times, want exactly 1", *calls)
	}
	if gotCfg.agentName != "captain" {
		t.Errorf("keeper cfg.agentName = %q, want %q", gotCfg.agentName, "captain")
	}
	if gotCfg.projectDir != proj {
		t.Errorf("keeper cfg.projectDir = %q, want %q", gotCfg.projectDir, proj)
	}
	if !strings.HasSuffix(gotCfg.settingsPath, "/.claude/settings.json") {
		t.Errorf("keeper cfg.settingsPath = %q, want a path ending in /.claude/settings.json", gotCfg.settingsPath)
	}
	// tmux must still launch after wiring.
	if *captured == nil {
		t.Error("tmux must be launched after keeper wiring")
	}
}

func TestCaptainLaunch_HonorsCustomNameInKeeperCfg_hkigek(t *testing.T) {
	run, _ := captureRunHkly0n()
	keeper, calls, gotCfg := captureKeeperHkigek(0)

	code := runCaptainLaunch([]string{"--name", "skipper", "--project", t.TempDir()}, run, keeper)
	if code != 0 {
		t.Fatalf("runCaptainLaunch exit = %d, want 0", code)
	}
	if *calls != 1 {
		t.Fatalf("keeper enable called %d times, want exactly 1", *calls)
	}
	if gotCfg.agentName != "skipper" {
		t.Errorf("keeper cfg.agentName = %q, want %q", gotCfg.agentName, "skipper")
	}
}

func TestCaptainLaunch_NoKeeperSkipsWiring_hkigek(t *testing.T) {
	run, captured := captureRunHkly0n()
	keeper, calls, _ := captureKeeperHkigek(0)

	code := runCaptainLaunch([]string{"--no-keeper", "--project", t.TempDir()}, run, keeper)
	if code != 0 {
		t.Fatalf("runCaptainLaunch exit = %d, want 0", code)
	}
	if *calls != 0 {
		t.Fatalf("keeper enable called %d times, want 0 under --no-keeper", *calls)
	}
	// tmux must still launch when keeper wiring is skipped.
	if *captured == nil {
		t.Error("tmux must be launched even when --no-keeper is set")
	}
}

func TestCaptainLaunch_KeeperFailureDoesNotBlockLaunch_hkigek(t *testing.T) {
	run, captured := captureRunHkly0n()
	keeper, calls, _ := captureKeeperHkigek(1) // keeper enable fails

	code := runCaptainLaunch([]string{"--project", t.TempDir()}, run, keeper)
	if code != 0 {
		t.Fatalf("runCaptainLaunch exit = %d, want 0 (keeper failure must not block launch)", code)
	}
	if *calls != 1 {
		t.Fatalf("keeper enable called %d times, want exactly 1", *calls)
	}
	if *captured == nil {
		t.Error("tmux must still launch when keeper enable fails")
	}
}
