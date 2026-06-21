package main

// keeper_hold_cmd_hk9waz_test.go — CLI tests for `harmonik keeper hold|release`
// (hk-9waz / codename:keeper-hold). The hold verb requires a trustworthy live .sid
// (a lowercase UUIDv4) so the writer keys the marker by the SAME session-id the
// gate readers resolve. release is idempotent. Both reject a positional argument
// (flag-only contract) with exit 2.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// validUUIDv4 is a lowercase UUIDv4 that isPrimarySID accepts.
const validUUIDv4 = "11111111-1111-4111-8111-111111111111"

// writeSidForCmd writes <agent>.sid under the keeper dir (models the SessionStart
// hook) so SetHold finds a trustworthy live session id.
func writeSidForCmd(t *testing.T, projectDir, agent, sid string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir keeper dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, agent+".sid"), []byte(sid+"\n"), 0o600); err != nil {
		t.Fatalf("write sid: %v", err)
	}
}

// TestRunKeeperHold_HappyPath: with a valid .sid, hold exits 0 and IsHeld is true.
func TestRunKeeperHold_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "captain"
	writeSidForCmd(t, dir, agent, validUUIDv4)

	code := runKeeperHold([]string{"--project", dir, "--agent", agent})
	if code != 0 {
		t.Fatalf("runKeeperHold happy path: want exit 0, got %d", code)
	}
	if !keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Error("IsHeld: want true after `keeper hold`")
	}
}

// TestRunKeeperHold_NoSid: without a .sid the verb cannot establish a trustworthy
// live session and exits 1.
func TestRunKeeperHold_NoSid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	code := runKeeperHold([]string{"--project", dir, "--agent", "captain"})
	if code != 1 {
		t.Errorf("runKeeperHold with no .sid: want exit 1, got %d", code)
	}
}

// TestRunKeeperHold_MissingAgent: omitting --agent exits 1.
func TestRunKeeperHold_MissingAgent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	code := runKeeperHold([]string{"--project", dir})
	if code != 1 {
		t.Errorf("runKeeperHold with no agent: want exit 1, got %d", code)
	}
}

// TestRunKeeperHold_PositionalRejected: a positional argument (flag-only contract)
// exits 2.
func TestRunKeeperHold_PositionalRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	code := runKeeperHold([]string{"--project", dir, "captain"})
	if code != 2 {
		t.Errorf("runKeeperHold with positional: want exit 2, got %d", code)
	}
}

// TestRunKeeperRelease_HappyPath: release after a hold exits 0 and clears IsHeld.
func TestRunKeeperRelease_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "captain"
	writeSidForCmd(t, dir, agent, validUUIDv4)

	if _, err := keeper.SetHold(dir, agent); err != nil {
		t.Fatalf("setup SetHold: %v", err)
	}
	code := runKeeperRelease([]string{"--project", dir, "--agent", agent})
	if code != 0 {
		t.Fatalf("runKeeperRelease: want exit 0, got %d", code)
	}
	if keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Error("IsHeld: want false after `keeper release`")
	}
}

// TestRunKeeperRelease_IdempotentWhenAbsent: release on a clear agent exits 0.
func TestRunKeeperRelease_IdempotentWhenAbsent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	code := runKeeperRelease([]string{"--project", dir, "--agent", "no-hold-agent"})
	if code != 0 {
		t.Errorf("runKeeperRelease on absent hold: want exit 0, got %d", code)
	}
}

// TestRunKeeperRelease_MissingAgent: omitting --agent exits 1.
func TestRunKeeperRelease_MissingAgent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	code := runKeeperRelease([]string{"--project", dir})
	if code != 1 {
		t.Errorf("runKeeperRelease with no agent: want exit 1, got %d", code)
	}
}

// TestRunKeeperRelease_PositionalRejected: a positional argument exits 2.
func TestRunKeeperRelease_PositionalRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	code := runKeeperRelease([]string{"--project", dir, "captain"})
	if code != 2 {
		t.Errorf("runKeeperRelease with positional: want exit 2, got %d", code)
	}
}

// TestRunKeeperHoldRelease_Roundtrip: hold → IsHeld → release → IsHeld via the CLI.
func TestRunKeeperHoldRelease_Roundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "crew-paul"
	writeSidForCmd(t, dir, agent, validUUIDv4)

	if keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Fatal("IsHeld: want false before hold")
	}
	if code := runKeeperHold([]string{"--project", dir, "--agent", agent}); code != 0 {
		t.Fatalf("hold: want exit 0, got %d", code)
	}
	if !keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Error("IsHeld: want true after hold")
	}
	if code := runKeeperRelease([]string{"--project", dir, "--agent", agent}); code != 0 {
		t.Fatalf("release: want exit 0, got %d", code)
	}
	if keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Error("IsHeld: want false after release")
	}
}
