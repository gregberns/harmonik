package daemon_test

// branching_hk3r3_test.go — unit tests for target_repo parsing and
// CrossRepoUnsupportedError (hk-3r3: cross-repo dispatch guard).
//
// Helper prefix: crossRepo3r3 (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-3r3).

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// crossRepo3r3Body builds a bead description with a ## Branching section
// containing the given YAML content.
func crossRepo3r3Body(yamlContent string) string {
	return "## Summary\n\nSome work.\n\n## Branching\n\n```yaml\n" + yamlContent + "\n```\n"
}

// TestParseBranchingSection_TargetRepo_3r3 verifies that target_repo is
// extracted from the ## Branching section and propagated into BranchingConfig.
func TestParseBranchingSection_TargetRepo_3r3(t *testing.T) {
	t.Parallel()

	body := crossRepo3r3Body("target_repo: /Users/gb/github/kerf\ntarget_branch: main\n")
	cfg, err := daemon.ExportedParseBranchingSection(body)
	if err != nil {
		t.Fatalf("parseBranchingSection: unexpected error: %v", err)
	}
	if cfg.TargetRepo != "/Users/gb/github/kerf" {
		t.Errorf("TargetRepo: got %q, want %q", cfg.TargetRepo, "/Users/gb/github/kerf")
	}
	if cfg.LandsOn != "main" {
		t.Errorf("LandsOn: got %q, want %q", cfg.LandsOn, "main")
	}
}

// TestParseBranchingSection_TargetRepo_Absent_3r3 verifies that a ## Branching
// section without target_repo leaves BranchingConfig.TargetRepo empty.
func TestParseBranchingSection_TargetRepo_Absent_3r3(t *testing.T) {
	t.Parallel()

	body := crossRepo3r3Body("start_from: main\n")
	cfg, err := daemon.ExportedParseBranchingSection(body)
	if err != nil {
		t.Fatalf("parseBranchingSection: unexpected error: %v", err)
	}
	if cfg.TargetRepo != "" {
		t.Errorf("TargetRepo: got %q, want empty", cfg.TargetRepo)
	}
}

// TestCrossRepoUnsupportedError_3r3 verifies CrossRepoUnsupportedError.Error()
// includes both the target repo and the supervised project dir.
func TestCrossRepoUnsupportedError_3r3(t *testing.T) {
	t.Parallel()

	err := &daemon.CrossRepoUnsupportedError{
		TargetRepo: "/Users/gb/github/kerf",
		ProjectDir: "/Users/gb/github/harmonik",
	}
	msg := err.Error()
	if !strings.Contains(msg, "/Users/gb/github/kerf") {
		t.Errorf("error message missing target_repo: %q", msg)
	}
	if !strings.Contains(msg, "/Users/gb/github/harmonik") {
		t.Errorf("error message missing projectDir: %q", msg)
	}
	if !strings.Contains(msg, "hk-3r3") {
		t.Errorf("error message missing bead ref hk-3r3: %q", msg)
	}
}
