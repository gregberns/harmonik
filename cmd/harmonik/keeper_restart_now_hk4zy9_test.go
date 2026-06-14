package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// writeHandoffWithNonce creates a HANDOFF-<agent>.md with a KEEPER nonce comment
// in the given project directory so runKeeperRestartNow can extract it.
func writeHandoffWithNonce(t *testing.T, projectDir, agent, nonce string) {
	t.Helper()
	content := "# Handoff\n\n<!-- KEEPER:" + nonce + " -->\n\nSome content.\n"
	p := filepath.Join(projectDir, "HANDOFF-"+agent+".md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writeHandoffWithNonce: %v", err)
	}
}

// TestRunKeeperRestartNow_PositionalArg verifies the original positional-argument
// form still works after the --agent flag was added (backward compat).
func TestRunKeeperRestartNow_PositionalArg(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "captain"
	writeHandoffWithNonce(t, projectDir, agent, "testnonce1")

	code := runKeeperRestartNow([]string{"--project", projectDir, agent})
	if code != 0 {
		t.Fatalf("positional form: want exit 0, got %d", code)
	}

	m, err := keeper.ReadRestartNowMarker(projectDir, agent)
	if err != nil {
		t.Fatalf("ReadRestartNowMarker: %v", err)
	}
	if m.Nonce != "testnonce1" {
		t.Errorf("nonce: want %q, got %q", "testnonce1", m.Nonce)
	}
}

// TestRunKeeperRestartNow_FlagArg verifies the new --agent flag form works and
// matches the exact command printed in keeper warn text.
func TestRunKeeperRestartNow_FlagArg(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "captain"
	writeHandoffWithNonce(t, projectDir, agent, "testnonce2")

	// This is the exact form the warn text instructs: harmonik keeper restart-now --agent captain
	code := runKeeperRestartNow([]string{"--project", projectDir, "--agent", agent})
	if code != 0 {
		t.Fatalf("--agent flag form: want exit 0, got %d", code)
	}

	m, err := keeper.ReadRestartNowMarker(projectDir, agent)
	if err != nil {
		t.Fatalf("ReadRestartNowMarker: %v", err)
	}
	if m.Nonce != "testnonce2" {
		t.Errorf("nonce: want %q, got %q", "testnonce2", m.Nonce)
	}
}

// TestRunKeeperRestartNow_FlagTakesPrecedence verifies that when both --agent
// and a positional arg are given, --agent wins.
func TestRunKeeperRestartNow_FlagTakesPrecedence(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	flagAgent := "captain"
	positionalAgent := "other-agent"
	writeHandoffWithNonce(t, projectDir, flagAgent, "flagwins")

	code := runKeeperRestartNow([]string{"--project", projectDir, "--agent", flagAgent, positionalAgent})
	if code != 0 {
		t.Fatalf("flag-wins form: want exit 0, got %d", code)
	}

	// Marker should be written for flagAgent, not positionalAgent.
	m, err := keeper.ReadRestartNowMarker(projectDir, flagAgent)
	if err != nil {
		t.Fatalf("ReadRestartNowMarker(flagAgent): %v", err)
	}
	if m.Nonce != "flagwins" {
		t.Errorf("nonce: want %q, got %q", "flagwins", m.Nonce)
	}

	// No marker should exist for the positional agent.
	markerPath := filepath.Join(projectDir, ".harmonik", "keeper", positionalAgent+".restart-now")
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Errorf("positional agent marker should not exist, got stat err: %v", err)
	}
}

// TestRunKeeperRestartNow_MissingAgent verifies that omitting both --agent and
// positional arg returns exit 1.
func TestRunKeeperRestartNow_MissingAgent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	code := runKeeperRestartNow([]string{"--project", projectDir})
	if code != 1 {
		t.Errorf("missing agent: want exit 1, got %d", code)
	}
}
