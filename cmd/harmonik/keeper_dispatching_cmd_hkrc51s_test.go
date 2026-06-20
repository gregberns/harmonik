package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// ── runKeeperSetDispatching ───────────────────────────────────────────────────

// TestRunKeeperSetDispatching_CreatesMarker verifies that the verb writes the
// .dispatching marker and that HoldingDispatch subsequently returns true.
func TestRunKeeperSetDispatching_CreatesMarker(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "test-orchestrator"

	code := runKeeperSetDispatching([]string{"--project", projectDir, "--agent", agent})
	if code != 0 {
		t.Fatalf("runKeeperSetDispatching: want exit 0, got %d", code)
	}

	markerPath := filepath.Join(projectDir, ".harmonik", "keeper", agent+".dispatching")
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("set-dispatching: marker not found at %q: %v", markerPath, err)
	}

	if !keeper.HoldingDispatch(projectDir, agent) {
		t.Error("HoldingDispatch: want true after set-dispatching")
	}
}

// TestRunKeeperSetDispatching_MissingAgent verifies that omitting the agent
// argument returns exit 1.
func TestRunKeeperSetDispatching_MissingAgent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	code := runKeeperSetDispatching([]string{"--project", projectDir})
	if code != 1 {
		t.Errorf("runKeeperSetDispatching with no agent: want exit 1, got %d", code)
	}
}

// TestRunKeeperSetDispatching_InvalidAgent verifies that a path-traversal agent
// name (passed via --agent) returns exit 1. The traversal is caught by
// validateAgent inside SetDispatching; the CLI maps that error to exit 1.
func TestRunKeeperSetDispatching_InvalidAgent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	code := runKeeperSetDispatching([]string{"--project", projectDir, "--agent", "../evil"})
	if code != 1 {
		t.Errorf("runKeeperSetDispatching with traversal agent: want exit 1, got %d", code)
	}
}

// ── runKeeperClearDispatching ─────────────────────────────────────────────────

// TestRunKeeperClearDispatching_RemovesMarker verifies that the verb removes
// the .dispatching marker and that HoldingDispatch subsequently returns false.
func TestRunKeeperClearDispatching_RemovesMarker(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "test-orchestrator"

	if err := keeper.SetDispatching(projectDir, agent); err != nil {
		t.Fatalf("setup SetDispatching: %v", err)
	}

	code := runKeeperClearDispatching([]string{"--project", projectDir, "--agent", agent})
	if code != 0 {
		t.Fatalf("runKeeperClearDispatching: want exit 0, got %d", code)
	}

	if keeper.HoldingDispatch(projectDir, agent) {
		t.Error("HoldingDispatch: want false after clear-dispatching")
	}
}

// TestRunKeeperClearDispatching_IdempotentWhenAbsent verifies that
// clear-dispatching on a project where no marker exists is a no-op (exit 0).
func TestRunKeeperClearDispatching_IdempotentWhenAbsent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "absent-agent"

	code := runKeeperClearDispatching([]string{"--project", projectDir, "--agent", agent})
	if code != 0 {
		t.Errorf("runKeeperClearDispatching on absent marker: want exit 0, got %d", code)
	}
}

// TestRunKeeperClearDispatching_MissingAgent verifies that omitting the agent
// argument returns exit 1.
func TestRunKeeperClearDispatching_MissingAgent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	code := runKeeperClearDispatching([]string{"--project", projectDir})
	if code != 1 {
		t.Errorf("runKeeperClearDispatching with no agent: want exit 1, got %d", code)
	}
}

// TestRunKeeperClearDispatching_InvalidAgent verifies that a path-traversal
// agent name (passed via --agent) returns exit 1. The traversal is caught by
// validateAgent inside ClearDispatching; the CLI maps that error to exit 1.
func TestRunKeeperClearDispatching_InvalidAgent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	code := runKeeperClearDispatching([]string{"--project", projectDir, "--agent", "../evil"})
	if code != 1 {
		t.Errorf("runKeeperClearDispatching with traversal agent: want exit 1, got %d", code)
	}
}

// TestRunKeeperDispatchingRoundtrip verifies the full set → HoldingDispatch →
// clear → HoldingDispatch round trip via the CLI handlers.
func TestRunKeeperDispatchingRoundtrip(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "flywheel"

	if keeper.HoldingDispatch(projectDir, agent) {
		t.Fatal("HoldingDispatch: want false before set-dispatching")
	}

	if code := runKeeperSetDispatching([]string{"--project", projectDir, "--agent", agent}); code != 0 {
		t.Fatalf("set-dispatching: want exit 0, got %d", code)
	}
	if !keeper.HoldingDispatch(projectDir, agent) {
		t.Error("HoldingDispatch: want true after set-dispatching")
	}

	if code := runKeeperClearDispatching([]string{"--project", projectDir, "--agent", agent}); code != 0 {
		t.Fatalf("clear-dispatching: want exit 0, got %d", code)
	}
	if keeper.HoldingDispatch(projectDir, agent) {
		t.Error("HoldingDispatch: want false after clear-dispatching")
	}
}
