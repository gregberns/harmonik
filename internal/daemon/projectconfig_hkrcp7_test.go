package daemon_test

// projectconfig_hkrcp7_test.go — unit tests for the PL-004b daemon: block
// parser added to LoadProjectConfig (hk-rcp7).
//
// Covers:
//   - daemon block absent → DaemonConfig zero value (no error).
//   - daemon.workflow_mode: review-loop → WorkflowMode = review-loop.
//   - daemon.workflow_mode: dot → WorkflowMode = dot.
//   - daemon.workflow_mode: single → *ErrWorkflowModeFloorViolation (PL-004a floor).
//   - daemon.workflow_mode: <invalid> → *ErrMalformedConfigYAML.
//   - daemon.max_concurrent: 4 → MaxConcurrent = 4.
//   - daemon.max_concurrent: 0 → MaxConcurrent = 0 (not configured).
//   - daemon.max_concurrent: -1 → MaxConcurrent = 0 (not configured).
//   - daemon.target_branch: integration → TargetBranch = "integration" (stored but observability only).
//   - daemon block only (no agents) + schema_version: 1 → parsed correctly.
//   - Existing agents block still loads alongside daemon block.
//   - Empty daemon block (no keys set) → DaemonConfig zero value (no error).
//
// Load-bearing safety-floor assertion per hk-rcp7 acceptance criteria:
//   - daemon.workflow_mode: single MUST return *ErrWorkflowModeFloorViolation,
//     ensuring config.yaml can never lower the workflow mode below review-loop.
//
// Helper prefix: daemonBlkFixture (implementer-protocol.md §Helper-prefix discipline).
//
// Spec refs:
//   - specs/process-lifecycle.md §4.1 PL-004a — review floor.
//   - specs/process-lifecycle.md §4.1 PL-004b — flag > config > default chain.
//
// Bead: hk-rcp7.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// daemonBlkFixtureDir reuses projCfgFixtureDir to create a temp dir with
// .harmonik/ and optionally a config.yaml.
func daemonBlkFixtureDir(t *testing.T, yamlContent string) string {
	t.Helper()
	return projCfgFixtureDir(t, yamlContent)
}

// ─────────────────────────────────────────────────────────────────────────────
// Daemon block absent / empty
// ─────────────────────────────────────────────────────────────────────────────

func TestDaemonBlock_AbsentBlock_ZeroValue(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    model: sonnet
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Daemon != (daemon.ExportedDaemonConfig{}) {
		t.Errorf("absent daemon block: want zero DaemonConfig; got %+v", cfg.Daemon)
	}
}

func TestDaemonBlock_EmptyBlock_ZeroValue(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
daemon: {}
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Daemon != (daemon.ExportedDaemonConfig{}) {
		t.Errorf("empty daemon block: want zero DaemonConfig; got %+v", cfg.Daemon)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// workflow_mode parsing
// ─────────────────────────────────────────────────────────────────────────────

func TestDaemonBlock_WorkflowMode_ReviewLoop(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
daemon:
  workflow_mode: review-loop
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Daemon.WorkflowMode != core.WorkflowModeReviewLoop {
		t.Errorf("workflow_mode=review-loop: got %q; want %q", cfg.Daemon.WorkflowMode, core.WorkflowModeReviewLoop)
	}
}

func TestDaemonBlock_WorkflowMode_Dot(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
daemon:
  workflow_mode: dot
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Daemon.WorkflowMode != core.WorkflowModeDot {
		t.Errorf("workflow_mode=dot: got %q; want %q", cfg.Daemon.WorkflowMode, core.WorkflowModeDot)
	}
}

// TestDaemonBlock_WorkflowMode_Single_FloorViolation is the load-bearing
// safety-floor test per hk-rcp7 acceptance criteria.
//
// MUST assert that daemon.workflow_mode: single in config.yaml is NOT reachable
// past the PL-004a review floor.  Any failure here means the config surface can
// lower the effective workflow mode to the no-review single shape — exactly the
// failure mode that caused the ~117-bead unreviewed-merge incident.
func TestDaemonBlock_WorkflowMode_Single_FloorViolation(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
daemon:
  workflow_mode: single
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatal("LoadProjectConfig with workflow_mode:single: expected *ErrWorkflowModeFloorViolation; got nil error")
	}
	var fve *daemon.ExportedErrWorkflowModeFloorViolation
	if !errors.As(err, &fve) {
		t.Errorf("LoadProjectConfig with workflow_mode:single: error type = %T (%v); want *ErrWorkflowModeFloorViolation", err, err)
	}
	// Value field must reflect the forbidden value.
	if fve != nil && fve.Value != "single" {
		t.Errorf("ErrWorkflowModeFloorViolation.Value = %q; want %q", fve.Value, "single")
	}
}

func TestDaemonBlock_WorkflowMode_Invalid_MalformedError(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
daemon:
  workflow_mode: turbo-review
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatal("LoadProjectConfig with invalid workflow_mode: expected error; got nil")
	}
	var mfe *daemon.ExportedErrMalformedConfigYAML
	if !errors.As(err, &mfe) {
		t.Errorf("LoadProjectConfig invalid workflow_mode: error type = %T (%v); want *ErrMalformedConfigYAML", err, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// max_concurrent parsing
// ─────────────────────────────────────────────────────────────────────────────

func TestDaemonBlock_MaxConcurrent_Positive(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
daemon:
  max_concurrent: 4
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Daemon.MaxConcurrent != 4 {
		t.Errorf("max_concurrent=4: got %d; want 4", cfg.Daemon.MaxConcurrent)
	}
}

func TestDaemonBlock_MaxConcurrent_Zero_NotConfigured(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
daemon:
  max_concurrent: 0
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Daemon.MaxConcurrent != 0 {
		t.Errorf("max_concurrent=0: got %d; want 0 (not configured)", cfg.Daemon.MaxConcurrent)
	}
}

func TestDaemonBlock_MaxConcurrent_Negative_NotConfigured(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
daemon:
  max_concurrent: -1
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Daemon.MaxConcurrent != 0 {
		t.Errorf("max_concurrent=-1: got %d; want 0 (treated as not configured)", cfg.Daemon.MaxConcurrent)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// target_branch parsing (observability only)
// ─────────────────────────────────────────────────────────────────────────────

func TestDaemonBlock_TargetBranch_Stored(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
daemon:
  target_branch: integration
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	// target_branch is stored for observability/symmetry; callers use branching.Load()
	// for the authoritative value. We assert the field is populated, not that it
	// wins any resolution race.
	if cfg.Daemon.TargetBranch != "integration" {
		t.Errorf("target_branch: got %q; want %q (observability field)", cfg.Daemon.TargetBranch, "integration")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Combined: daemon block alongside agents block
// ─────────────────────────────────────────────────────────────────────────────

func TestDaemonBlock_CoexistsWithAgents(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    model: opus
    effort: high
daemon:
  workflow_mode: review-loop
  max_concurrent: 2
  target_branch: main
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}

	// Agents block still loads.
	model, effort := cfg.LookupAgent(core.AgentTypeClaudeCode)
	if model != "opus" {
		t.Errorf("agents[claude-code].model = %q; want %q", model, "opus")
	}
	if effort != "high" {
		t.Errorf("agents[claude-code].effort = %q; want %q", effort, "high")
	}

	// Daemon block populated.
	if cfg.Daemon.WorkflowMode != core.WorkflowModeReviewLoop {
		t.Errorf("daemon.workflow_mode = %q; want %q", cfg.Daemon.WorkflowMode, core.WorkflowModeReviewLoop)
	}
	if cfg.Daemon.MaxConcurrent != 2 {
		t.Errorf("daemon.max_concurrent = %d; want 2", cfg.Daemon.MaxConcurrent)
	}
	if cfg.Daemon.TargetBranch != "main" {
		t.Errorf("daemon.target_branch = %q; want %q", cfg.Daemon.TargetBranch, "main")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// daemon block only (no agents)
// ─────────────────────────────────────────────────────────────────────────────

func TestDaemonBlock_OnlyDaemonBlock_NoAgents(t *testing.T) {
	t.Parallel()

	root := daemonBlkFixtureDir(t, `
schema_version: 1
daemon:
  workflow_mode: dot
  max_concurrent: 8
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig (daemon-only): unexpected error: %v", err)
	}
	if cfg.Daemon.WorkflowMode != core.WorkflowModeDot {
		t.Errorf("daemon.workflow_mode = %q; want %q", cfg.Daemon.WorkflowMode, core.WorkflowModeDot)
	}
	if cfg.Daemon.MaxConcurrent != 8 {
		t.Errorf("daemon.max_concurrent = %d; want 8", cfg.Daemon.MaxConcurrent)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Empty-file sentinel still works with daemon block present elsewhere
// ─────────────────────────────────────────────────────────────────────────────

func TestDaemonBlock_EmptyFile_SentinelPreserved(t *testing.T) {
	t.Parallel()

	// projCfgFixtureDir("") creates .harmonik/ but does not write config.yaml.
	// Write the file explicitly with empty content so we test the empty-file path.
	root := daemonBlkFixtureDir(t, "")
	cfgPath := filepath.Join(root, ".harmonik", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile empty: %v", err)
	}

	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig (empty file): unexpected error: %v", err)
	}
	if cfg.Daemon != (daemon.ExportedDaemonConfig{}) {
		t.Errorf("empty file: want zero DaemonConfig; got %+v", cfg.Daemon)
	}
}
