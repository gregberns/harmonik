package main

// crew_keeper_autoarm_hkxxcv9_test.go — RED-then-GREEN coverage for auto-arming
// the keeper on crew start (hk-xxcv9).
//
// Verifies, via the injectable enableKeeper seam in runCrewStartCore, that:
//   - keeper enable is called by default, pre-RPC, with the crew's name and
//     resolved project dir, and a settings path ending in .claude/settings.json;
//   - a keeper-enable failure WARNS but does NOT block the crew start (exit 17
//     from no-daemon is the expected terminal code, not 1 from keeper failure).
//
// Exit code 17 is expected throughout: no daemon is running in unit tests, so
// crewDialAndSend returns 17 (socket missing / ECONNREFUSED). The tests assert
// that the keeper enable seam fires BEFORE the RPC — if it did not, the function
// would have returned 17 before calling the seam, and keeperCalled would remain false.

import (
	"io"
	"strings"
	"testing"
)

// captureCrewKeeperHkxxcv9 returns a keeper-enable seam that records the config
// it is handed (and a call counter), returning rc. Mirrors captureKeeperHkigek
// in captain_keeper_hkigek_test.go.
func captureCrewKeeperHkxxcv9(rc int) (fn keeperEnableFn, calls *int, got *enableConfig) {
	var n int
	var cfg enableConfig
	return func(c enableConfig, _, _ io.Writer) int {
		n++
		cfg = c
		return rc
	}, &n, &cfg
}

// TestCrewStart_WiresKeeperByDefault_hkxxcv9 verifies that runCrewStartCore
// calls enableKeeper with the crew's name and resolved project dir before
// attempting the daemon RPC.
func TestCrewStart_WiresKeeperByDefault_hkxxcv9(t *testing.T) {
	proj := t.TempDir()
	keeper, calls, gotCfg := captureCrewKeeperHkxxcv9(0)

	// No daemon running → exit 17 expected. Keeper must fire before the RPC.
	code := runCrewStartCore([]string{"alpha", "--project", proj}, keeper)
	if code != 17 {
		t.Fatalf("expected exit 17 (no daemon); got %d", code)
	}
	if *calls != 1 {
		t.Fatalf("keeper enable called %d times, want exactly 1", *calls)
	}
	if gotCfg.agentName != "alpha" {
		t.Errorf("keeper cfg.agentName = %q, want %q", gotCfg.agentName, "alpha")
	}
	if gotCfg.projectDir != proj {
		t.Errorf("keeper cfg.projectDir = %q, want %q", gotCfg.projectDir, proj)
	}
	if !strings.HasSuffix(gotCfg.settingsPath, "/.claude/settings.json") {
		t.Errorf("keeper cfg.settingsPath = %q, want a path ending in /.claude/settings.json", gotCfg.settingsPath)
	}
}

// TestCrewStart_KeeperFailureDoesNotBlockCrewStart_hkxxcv9 verifies that a
// non-zero keeper enable return does not cause crew start to exit with that
// code — it WARNS and continues to the RPC (which returns 17 in tests).
func TestCrewStart_KeeperFailureDoesNotBlockCrewStart_hkxxcv9(t *testing.T) {
	proj := t.TempDir()
	keeper, calls, _ := captureCrewKeeperHkxxcv9(1) // keeper enable fails

	code := runCrewStartCore([]string{"beta", "--project", proj}, keeper)
	if *calls != 1 {
		t.Fatalf("keeper enable called %d times, want 1", *calls)
	}
	// Keeper failure must NOT produce exit 1 — the RPC should still be attempted.
	if code == 1 {
		t.Errorf("keeper enable failure produced exit 1 (blocked crew start); want 17 (no daemon)")
	}
	if code != 17 {
		t.Fatalf("expected exit 17 (no daemon after warn-only keeper failure); got %d", code)
	}
}

// TestBuildCrewKeeperConfig_hkxxcv9 verifies that buildCrewKeeperConfig
// produces an enableConfig with the correct agent name and project dir.
func TestBuildCrewKeeperConfig_hkxxcv9(t *testing.T) {
	proj := t.TempDir()
	cfg, err := buildCrewKeeperConfig("delta", proj)
	if err != nil {
		t.Fatalf("buildCrewKeeperConfig: %v", err)
	}
	if cfg.agentName != "delta" {
		t.Errorf("agentName = %q, want %q", cfg.agentName, "delta")
	}
	if cfg.projectDir != proj {
		t.Errorf("projectDir = %q, want %q", cfg.projectDir, proj)
	}
	if !strings.HasSuffix(cfg.settingsPath, "/.claude/settings.json") {
		t.Errorf("settingsPath = %q, want a path ending in /.claude/settings.json", cfg.settingsPath)
	}
}
