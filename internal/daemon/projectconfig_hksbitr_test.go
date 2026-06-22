package daemon_test

// projectconfig_hksbitr_test.go — tests for the watchdog: config block (hk-sbitr):
// the ctx-watchdog schedule gate read from .harmonik/config.yaml.
//
// Verifies:
//   - absent watchdog block → Enabled defaults to true (default-ON per operator brief);
//   - explicit watchdog.enabled: false → Enabled is false (operator opt-out);
//   - explicit watchdog.enabled: true → Enabled is true.
//
// Bead: hk-sbitr.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestWatchdogConfig_Absent_DefaultsTrue verifies that an absent watchdog: block
// resolves to Enabled=true (the "cheap to leave on" default per the operator brief).
func TestWatchdogConfig_Absent_DefaultsTrue(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  workflow_mode: review-loop
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if !cfg.Watchdog.Enabled {
		t.Errorf("absent watchdog block: Enabled = false; want true (default-ON)")
	}
}

// TestWatchdogConfig_EnabledFalse verifies that watchdog.enabled: false is honoured.
func TestWatchdogConfig_EnabledFalse(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
watchdog:
  enabled: false
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Watchdog.Enabled {
		t.Errorf("watchdog.enabled: false: Enabled = true; want false")
	}
}

// TestWatchdogConfig_EnabledTrue verifies that an explicit watchdog.enabled: true
// is parsed correctly (matches the default but confirms the key is recognized).
func TestWatchdogConfig_EnabledTrue(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
watchdog:
  enabled: true
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if !cfg.Watchdog.Enabled {
		t.Errorf("watchdog.enabled: true: Enabled = false; want true")
	}
}
