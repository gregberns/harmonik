package main

// init_config_schema_version_test.go — regression test: the .harmonik/config.yaml
// that `harmonik init` generates must be ACCEPTED by the daemon's project-config
// loader.
//
// # Bug under regression
//
// configYAMLContent used to emit a top-level `version: 1` key, but the daemon's
// loader (internal/daemon, rawProjectConfig) reads `schema_version`. The generated
// file carries a daemon: block, so the empty-file escape hatch did not apply and
// the loader rejected the file with:
//
//	daemon: project config: unsupported schema_version 0 in <path> (want 1)
//
// i.e. the daemon could not start right after a fresh `harmonik init`. This test
// renders configYAMLContent exactly as init writes it, feeds it to the real daemon
// loader, and asserts it loads without error.
//
// Found while bootstrapping harmonik on a real project.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestInitConfigYAML_AcceptedByDaemonLoader renders the init config template and
// confirms the daemon's LoadProjectConfig accepts it (no ErrUnsupportedConfigVersion).
func TestInitConfigYAML_AcceptedByDaemonLoader(t *testing.T) {
	rendered := fmt.Sprintf(configYAMLContent, "main")

	// Belt-and-suspenders: the template must carry the schema_version key the
	// daemon reads, not the legacy `version` key.
	if !strings.Contains(rendered, "schema_version: 1") {
		t.Fatalf("generated config.yaml must contain `schema_version: 1`; got:\n%s", rendered)
	}
	if strings.Contains(rendered, "\nversion: 1") || strings.HasPrefix(rendered, "version: 1") {
		t.Errorf("generated config.yaml still emits the legacy top-level `version:` key:\n%s", rendered)
	}

	// Write it exactly where init does and load it with the real daemon loader.
	repoRoot := t.TempDir()
	harmonikDir := filepath.Join(repoRoot, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	cfgPath := filepath.Join(harmonikDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(rendered), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	cfg, err := daemon.LoadProjectConfig(repoRoot)
	if err != nil {
		t.Fatalf("daemon.LoadProjectConfig rejected init-generated config.yaml: %v\nconfig:\n%s", err, rendered)
	}

	// The daemon: block must have been parsed (target_branch is what init wrote).
	if cfg.Daemon.TargetBranch != "main" {
		t.Errorf("daemon target_branch = %q, want %q (daemon: block not parsed)", cfg.Daemon.TargetBranch, "main")
	}
}
