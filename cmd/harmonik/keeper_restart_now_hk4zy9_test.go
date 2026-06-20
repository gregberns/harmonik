package main

import (
	"os"
	"path/filepath"
	"testing"
)

// keeper_restart_now_hk4zy9_test.go — CLI-boundary tests for the SIMPLIFIED
// restart-now (hk-5da7). restart-now no longer writes a marker; it drives the
// ack→/clear→/session-resume injection in-process. These tests exercise the
// argument contract (flag-only) and the loud-failure path. The full injection
// behaviour is covered by internal/keeper/restartnow_test.go (unit, recording
// injector) and the live smoke test.

// writeHandoffWithNonce creates a HANDOFF-<agent>.md in the given project dir.
// The simplified path no longer requires a KEEPER nonce in the handoff, but a
// handoff file must exist and be fresh.
func writeHandoffWithNonce(t *testing.T, projectDir, agent, _ string) {
	t.Helper()
	p := filepath.Join(projectDir, "HANDOFF-"+agent+".md")
	if err := os.WriteFile(p, []byte("# Handoff\n"), 0o644); err != nil { //nolint:gosec
		t.Fatalf("writeHandoffWithNonce: %v", err)
	}
}

// TestRunKeeperRestartNow_FlagOnly verifies the FLAG-ONLY contract: --agent is
// the only accepted way to name the agent. A bare positional is rejected (exit 2).
func TestRunKeeperRestartNow_FlagOnly(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "captain"
	writeHandoffWithNonce(t, projectDir, agent, "")

	// Positional form is now REJECTED with exit 2 (flag-only).
	if code := runKeeperRestartNow([]string{"--project", projectDir, agent}); code != 2 {
		t.Fatalf("positional form: want exit 2 (flag-only), got %d", code)
	}
}

// TestRunKeeperRestartNow_MissingAgent verifies that omitting --agent returns 1.
func TestRunKeeperRestartNow_MissingAgent(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	if code := runKeeperRestartNow([]string{"--project", projectDir}); code != 1 {
		t.Errorf("missing agent: want exit 1, got %d", code)
	}
}

// TestRunKeeperRestartNow_NoPane_FailsLoudly verifies the silent-no-op bug is
// dead: with --agent set but no resolvable tmux pane / no verified session, the
// command exits NON-ZERO rather than printing success and doing nothing.
func TestRunKeeperRestartNow_NoPane_FailsLoudly(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "captain-no-pane-xyz"
	writeHandoffWithNonce(t, projectDir, agent, "")

	// No .sid/.ctx and no live tmux session for this agent → must fail (exit 1),
	// NOT exit 0. This is the regression guard for the silent no-op.
	if code := runKeeperRestartNow([]string{"--project", projectDir, "--agent", agent}); code == 0 {
		t.Fatalf("restart-now with no verified session/pane: want NON-ZERO exit, got 0 (silent no-op regression)")
	}
}

// TestRunKeeperPing_FlagOnly verifies ping is flag-only and fails loudly with no pane.
func TestRunKeeperPing_FlagOnly(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	// Positional rejected.
	if code := runKeeperPing([]string{"--project", projectDir, "someagent"}); code != 2 {
		t.Fatalf("ping positional: want exit 2 (flag-only), got %d", code)
	}
	// Missing agent.
	if code := runKeeperPing([]string{"--project", projectDir}); code != 1 {
		t.Fatalf("ping missing agent: want exit 1, got %d", code)
	}
	// With agent but no pane → loud failure (non-zero), never a silent success.
	if code := runKeeperPing([]string{"--project", projectDir, "--agent", "ping-no-pane-xyz"}); code == 0 {
		t.Fatalf("ping with no pane: want NON-ZERO exit, got 0")
	}
}
