package projectconfig

// projectconfig_fixture_test.go — shared test fixture for the projectconfig
// leaf (P2 LIFT crit 5). projCfgFixtureDir writes a .harmonik/config.yaml under
// a fresh t.TempDir() and returns the repo root, so every LoadProjectConfig
// test drives the real edge IO path. It was the canonical fixture in the
// daemon-package test suite (defined alongside the ResolveModelPreference tests
// in projectconfig_hkbfvk7_test.go); those model-preference tests test daemon
// behavior and stay in internal/daemon with their own copy, so the leaf carries
// this one.

import (
	"os"
	"path/filepath"
	"testing"
)

// projCfgFixtureDir creates a temp repo root containing .harmonik/config.yaml
// with the given YAML content and returns the root. An empty yamlContent writes
// no file (the "missing config" case).
func projCfgFixtureDir(t *testing.T, yamlContent string) string {
	t.Helper()
	root := t.TempDir()
	harmonikDir := filepath.Join(root, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o750); err != nil {
		t.Fatalf("projCfgFixtureDir: MkdirAll: %v", err)
	}
	if yamlContent != "" {
		cfgPath := filepath.Join(harmonikDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
			t.Fatalf("projCfgFixtureDir: WriteFile: %v", err)
		}
	}
	return root
}
