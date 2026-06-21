package daemon_test

// projectconfig_hkigpg_test.go — tests for the daemon.remote_control_prefix
// config field (hk-igpg): the per-project Claude Code Remote-Control session
// LABEL prefix read from .harmonik/config.yaml's daemon: block.
//
// Verifies:
//   - absent daemon block / absent key → empty prefix (bare label, backward compat);
//   - a present remote_control_prefix is parsed onto DaemonConfig;
//   - the field is tolerant (an unknown sibling key under daemon: does NOT error,
//     per PL-004b daemon-block tolerance) — guarded indirectly via the field
//     coexisting with the existing daemon keys.
//
// Bead: hk-igpg.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

func TestRemoteControlPrefix_Absent_Empty(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  workflow_mode: review-loop
  max_concurrent: 4
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Daemon.RemoteControlPrefix != "" {
		t.Errorf("absent remote_control_prefix: got %q; want empty (bare label)", cfg.Daemon.RemoteControlPrefix)
	}
}

func TestRemoteControlPrefix_Present_Parsed(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  target_branch: main
  remote_control_prefix: hk
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Daemon.RemoteControlPrefix != "hk" {
		t.Errorf("remote_control_prefix=hk: got %q; want %q", cfg.Daemon.RemoteControlPrefix, "hk")
	}
}

// TestRemoteControlPrefix_OnlyKey_LoadsWithVersion guards that a daemon block
// carrying ONLY remote_control_prefix is recognised as present (not mistaken for
// the empty-file fast path) and parses cleanly under schema_version: 1.
func TestRemoteControlPrefix_OnlyKey_LoadsWithVersion(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  remote_control_prefix: mproj
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Daemon.RemoteControlPrefix != "mproj" {
		t.Errorf("remote_control_prefix=mproj: got %q; want %q", cfg.Daemon.RemoteControlPrefix, "mproj")
	}
}
