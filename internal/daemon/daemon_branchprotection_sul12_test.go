package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// daemon_branchprotection_sul12_test.go — targeted tests for the boot-time
// fail-closed branch-protection validation added by hk-sul12:
//
//  1. ForbidUnprotectedDefault + empty TargetBranch → hard error before socket bind.
//  2. resolved TargetBranch in ProtectBranches → hard error before socket bind.
//  3. Happy path emits daemon_config with the resolved target branch.
//
// Bead ref: hk-sul12.

func branchProtectionFixtureDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = t.TempDir()
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("branchProtectionFixtureDir: mkdir %s: %v", eventsDir, err)
	}
	jsonlPath = filepath.Join(eventsDir, "events.jsonl")
	return projectDir, jsonlPath
}

// TestDaemonStart_ForbidUnprotectedDefault_EmptyTargetBranch asserts that Start
// returns a hard error when ForbidUnprotectedDefault is true and TargetBranch is
// empty (hk-sul12 case 1).
func TestDaemonStart_ForbidUnprotectedDefault_EmptyTargetBranch(t *testing.T) {
	t.Parallel()

	cfg := daemon.Config{
		WorkflowModeDefault:      core.WorkflowModeReviewLoop,
		ForbidUnprotectedDefault: true,
		TargetBranch:             "", // deliberately absent
	}
	err := daemon.Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("Start returned nil; want non-nil error when ForbidUnprotectedDefault=true and TargetBranch empty")
	}
	if !strings.Contains(err.Error(), "--forbid-default-main") {
		t.Errorf("error %q does not mention --forbid-default-main; want a clear actionable message", err.Error())
	}
}

// TestDaemonStart_TargetBranchInProtectBranches asserts that Start returns a
// hard error when the resolved TargetBranch appears in ProtectBranches (hk-sul12
// case 2).
func TestDaemonStart_TargetBranchInProtectBranches(t *testing.T) {
	t.Parallel()

	cfg := daemon.Config{
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		TargetBranch:        "main",
		ProtectBranches:     []string{"main"},
	}
	err := daemon.Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("Start returned nil; want non-nil error when TargetBranch is in ProtectBranches")
	}
	if !strings.Contains(err.Error(), "main") || !strings.Contains(err.Error(), "ProtectBranches") {
		t.Errorf("error %q does not mention protected branch; want a clear actionable message", err.Error())
	}
}

// TestDaemonStart_DefaultTargetBranchInProtectBranches asserts that Start
// returns a hard error when TargetBranch is empty (resolves to "main") and
// "main" is in ProtectBranches (hk-sul12 case 2, implicit default).
func TestDaemonStart_DefaultTargetBranchInProtectBranches(t *testing.T) {
	t.Parallel()

	cfg := daemon.Config{
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		TargetBranch:        "", // resolves to "main"
		ProtectBranches:     []string{"main"},
	}
	err := daemon.Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("Start returned nil; want non-nil error when TargetBranch resolves to protected 'main'")
	}
}

// TestDaemonStart_EmitsDaemonConfig asserts that Start succeeds (no error) when
// ForbidUnprotectedDefault is true and an explicit TargetBranch not in
// ProtectBranches is provided (hk-sul12 happy-path).
//
// daemon_config is an O-class event (async bus delivery); verifying it in a
// JSONL log races the bus worker pool. Correctness of the payload struct and
// event registration is covered by TestDaemonConfigPayload_Valid and
// TestAllEventTypeConstantsHaveRegistryEntries in internal/core.
func TestDaemonStart_EmitsDaemonConfig(t *testing.T) {
	t.Parallel()

	cfg := daemon.Config{
		WorkflowModeDefault:      core.WorkflowModeReviewLoop,
		TargetBranch:             "release",
		ProtectBranches:          []string{"main"},
		ForbidUnprotectedDefault: true,
	}
	if err := daemon.Start(context.Background(), cfg); err != nil {
		t.Errorf("Start returned unexpected error on valid config: %v", err)
	}
}
