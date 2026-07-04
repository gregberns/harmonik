package main

// captain_twoproj_hk25bg_test.go — T5 live two-project validation (hk-25bg):
// captain-launch side of the side-by-side scenario.  Verifies that two
// concurrent harmonik projects with distinct slugs produce non-colliding RC
// labels on the captain launch path while HARMONIK_AGENT stays bare, proving
// that crew wake / keeper rebind are unaffected by the RC prefix.
//
// The crew-launch side lives in internal/daemon/crewlaunchspec_twoproj_hk25bg_test.go.
//
// Run: go test ./cmd/harmonik/ -run TwoProject -v

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTwoProjConfig writes a minimal .harmonik/config.yaml with
// daemon.remote_control_prefix to projectDir.
func writeTwoProjConfig(t *testing.T, projectDir, prefix string) {
	t.Helper()
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("writeTwoProjConfig: MkdirAll: %v", err)
	}
	content := "schema_version: 1\ndaemon:\n  remote_control_prefix: " + prefix + "\n"
	if err := os.WriteFile(filepath.Join(harmonikDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writeTwoProjConfig: WriteFile: %v", err)
	}
}

// TestCaptainTwoProjectLabelIsolation_hk25bg verifies that two concurrent
// projects configured with prefixes "hk" and "mp" produce non-colliding
// captain RC labels (hk-captain vs mp-captain) and that HARMONIK_AGENT stays
// bare for both, confirming that keeper rebind and crew wake are unaffected.
func TestCaptainTwoProjectLabelIsolation_hk25bg(t *testing.T) {
	projHK := t.TempDir()
	projMP := t.TempDir()
	writeTwoProjConfig(t, projHK, "hk")
	writeTwoProjConfig(t, projMP, "mp")

	type result struct {
		rcLabel     string
		agentEnvVal string
	}
	launch := func(projectDir string) result {
		run, captured := captureRunHkly0n()
		code := runCaptainLaunchWithOps(
			[]string{"--project", projectDir},
			run, noopKeeperHkly0n, &fakeCaptainOps{},
		)
		if code != 0 {
			t.Fatalf("runCaptainLaunchWithOps --project %q exit = %d, want 0", projectDir, code)
		}
		argv := argvHkly0n(*captured)
		return result{
			rcLabel:     flagValueHkly0n(argv, "--remote-control"),
			agentEnvVal: flagValueHkly0n(argv, "-e"),
		}
	}

	hk := launch(projHK)
	mp := launch(projMP)

	// RC labels must be distinct and correctly prefixed.
	if hk.rcLabel != "hk-captain" {
		t.Errorf("project hk: --remote-control = %q; want %q", hk.rcLabel, "hk-captain")
	}
	if mp.rcLabel != "mp-captain" {
		t.Errorf("project mp: --remote-control = %q; want %q", mp.rcLabel, "mp-captain")
	}
	if hk.rcLabel == mp.rcLabel {
		t.Errorf("collision: both projects produced --remote-control %q; labels must be distinct", hk.rcLabel)
	}

	// HARMONIK_AGENT must be bare for both projects: keeper rebind and crew
	// wake key off the bare name + session-id, not the RC label.
	const wantAgent = "HARMONIK_AGENT=captain"
	if hk.agentEnvVal != wantAgent {
		t.Errorf("project hk: HARMONIK_AGENT env = %q; want %q (prefix must not bleed into identity)", hk.agentEnvVal, wantAgent)
	}
	if mp.agentEnvVal != wantAgent {
		t.Errorf("project mp: HARMONIK_AGENT env = %q; want %q (prefix must not bleed into identity)", mp.agentEnvVal, wantAgent)
	}
}
