package main

// captain_rcprefix_hkdhe6_test.go — integration tests verifying that the
// per-project RC label prefix flows correctly through runCaptainLaunchWithOps
// (hk-dhe6 / codename:rc-prefix epic).
//
// The builder-function tests (buildCaptainTmuxCmd / buildCaptainRespawnWindowCmd)
// live in captain_rcprefix_hkw8ex_test.go. The config-field parse tests live in
// internal/daemon/projectconfig_hkigpg_test.go. What was missing was an
// end-to-end test threading the two together through the full launch path:
//
//   config.yaml daemon.remote_control_prefix
//       ↓ LoadProjectConfig
//   runCaptainLaunchWithOps (flag-not-passed → config fallback)
//       ↓ buildCaptainTmuxCmd(rcPrefix=...)
//   tmux argv --remote-control <prefix>-captain
//
// Three scenarios guarded here:
//  1. Config carries remote_control_prefix → label is prefixed.
//  2. Explicit --rc-prefix flag wins over config.
//  3. Explicit --rc-prefix "" forces bare label even when config has a prefix
//     (the sentinel-vs-empty design from captain.go:375).

import (
	"os"
	"path/filepath"
	"testing"
)

// writeCaptainRCPrefixConfigHkdhe6 creates a minimal .harmonik/config.yaml with
// the given remote_control_prefix value under the daemon: block.
func writeCaptainRCPrefixConfigHkdhe6(t *testing.T, projectDir, prefix string) {
	t.Helper()
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("writeCaptainRCPrefixConfigHkdhe6: MkdirAll: %v", err)
	}
	content := "schema_version: 1\ndaemon:\n  remote_control_prefix: " + prefix + "\n"
	if err := os.WriteFile(filepath.Join(harmonikDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writeCaptainRCPrefixConfigHkdhe6: WriteFile: %v", err)
	}
}

// TestCaptainLaunch_RcPrefixFromConfig_hkdhe6 verifies that when --rc-prefix is
// NOT passed, runCaptainLaunchWithOps reads remote_control_prefix from config and
// builds the --remote-control label as "<prefix>-<name>".
// HARMONIK_AGENT must remain bare (prefix is cosmetic, RC-label-only).
func TestCaptainLaunch_RcPrefixFromConfig_hkdhe6(t *testing.T) {
	proj := t.TempDir()
	writeCaptainRCPrefixConfigHkdhe6(t, proj, "hk")

	run, captured := captureRunHkly0n()
	code := runCaptainLaunchWithOps(
		[]string{"--project", proj},
		run, noopKeeperHkly0n, &fakeCaptainOps{},
	)
	if code != 0 {
		t.Fatalf("runCaptainLaunchWithOps exit = %d, want 0", code)
	}
	argv := argvHkly0n(*captured)

	if got := flagValueHkly0n(argv, "--remote-control"); got != "hk-captain" {
		t.Errorf("--remote-control = %q, want %q (prefix from config)", got, "hk-captain")
	}
	// HARMONIK_AGENT stays bare — the prefix is cosmetic, not an identity key.
	if got := flagValueHkly0n(argv, "-e"); got != "HARMONIK_AGENT=captain" {
		t.Errorf("HARMONIK_AGENT = %q, want bare %q (prefix must NOT bleed into identity)", got, "HARMONIK_AGENT=captain")
	}
}

// TestCaptainLaunch_RcPrefixFromFlag_hkdhe6 verifies that an explicit --rc-prefix
// flag value wins and is folded into the --remote-control label.
func TestCaptainLaunch_RcPrefixFromFlag_hkdhe6(t *testing.T) {
	run, captured := captureRunHkly0n()
	code := runCaptainLaunchWithOps(
		[]string{"--rc-prefix", "mp", "--project", t.TempDir()},
		run, noopKeeperHkly0n, &fakeCaptainOps{},
	)
	if code != 0 {
		t.Fatalf("runCaptainLaunchWithOps exit = %d, want 0", code)
	}
	if got := flagValueHkly0n(argvHkly0n(*captured), "--remote-control"); got != "mp-captain" {
		t.Errorf("--remote-control = %q, want %q (prefix from flag)", got, "mp-captain")
	}
}

// TestCaptainLaunch_RcPrefixFlagEmptyOverridesConfig_hkdhe6 verifies that an
// explicit --rc-prefix "" forces a BARE label even when config carries a prefix.
// This is the sentinel design: "\x00" = unset (→ read config), "" = explicit bare.
func TestCaptainLaunch_RcPrefixFlagEmptyOverridesConfig_hkdhe6(t *testing.T) {
	proj := t.TempDir()
	writeCaptainRCPrefixConfigHkdhe6(t, proj, "hk")

	run, captured := captureRunHkly0n()
	code := runCaptainLaunchWithOps(
		[]string{"--rc-prefix", "", "--project", proj},
		run, noopKeeperHkly0n, &fakeCaptainOps{},
	)
	if code != 0 {
		t.Fatalf("runCaptainLaunchWithOps exit = %d, want 0", code)
	}
	if got := flagValueHkly0n(argvHkly0n(*captured), "--remote-control"); got != "captain" {
		t.Errorf("--remote-control = %q, want bare %q (explicit empty flag must override config prefix)", got, "captain")
	}
}
