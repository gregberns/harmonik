package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// writeSidFile_hksuxt writes the single-writer <agent>.sid identity channel under
// projectDir so the keeperForceRestartFn closure can re-verify identity at the
// moment of firing (the handoff-timeout escalation re-checks .sid via
// NewLiveRecoverViaRespawn, mirroring the live-pane path).
func writeSidFile_hksuxt(t *testing.T, projectDir, agent, sid string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sid dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, agent+".sid"), []byte(sid), 0o644); err != nil {
		t.Fatalf("write .sid: %v", err)
	}
}

// TestKeeperForceRestartFn_FailClosedByDefault_hksuxt asserts the fail-closed
// default (bead requirement (b)): unless the operator BOTH opts in with
// --force-restart AND supplies a --respawn-cmd, the wired ForceRestartFn is nil.
// A nil fn makes the cycle.go handoff-timeout escalation gate
// (ForceRestartFn != nil) false, so behaviour is byte-identical to today.
func TestKeeperForceRestartFn_FailClosedByDefault_hksuxt(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()

	if fn := keeperForceRestartFn(false, projectDir, "touch /tmp/should-not-run"); fn != nil {
		t.Errorf("flag false → want nil ForceRestartFn (escalation dormant), got non-nil")
	}
	if fn := keeperForceRestartFn(true, projectDir, ""); fn != nil {
		t.Errorf("empty respawn-cmd → want nil ForceRestartFn (nothing to launch), got non-nil")
	}
	if fn := keeperForceRestartFn(false, projectDir, ""); fn != nil {
		t.Errorf("neither set → want nil ForceRestartFn, got non-nil")
	}
}

// TestKeeperForceRestartFn_RunsRespawnWhenTrusted_hksuxt asserts the opted-in
// wiring (bead requirement (a), wired half): with --force-restart AND a
// --respawn-cmd, the wired ForceRestartFn is non-nil and, when the bound .sid is
// a valid UUIDv4, it actually runs the respawn command. The handoff-timeout
// firing path itself (fn fires after MaxHandoffTimeouts) is covered by
// TestCycler_ForcedClear_EscalatesAfterNTimeouts in internal/keeper.
func TestKeeperForceRestartFn_RunsRespawnWhenTrusted_hksuxt(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	const agent = "captain-hksuxt"
	writeSidFile_hksuxt(t, projectDir, agent, "11111111-1111-4111-8111-111111111111")

	sentinel := filepath.Join(projectDir, "respawned.sentinel")
	fn := keeperForceRestartFn(true, projectDir, "touch "+sentinel)
	if fn == nil {
		t.Fatal("opted-in (--force-restart + --respawn-cmd) → want non-nil ForceRestartFn, got nil")
	}
	if err := fn(context.Background(), agent); err != nil {
		t.Fatalf("ForceRestartFn with trusted UUIDv4 .sid: unexpected error: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("respawn command did not run (sentinel missing): %v", err)
	}
}

// TestKeeperForceRestartFn_RefusesNonUUIDv4SID_hksuxt asserts bead requirement
// (c): even when opted in, the wired closure refuses (no restart) when the bound
// .sid identity is not a valid UUIDv4 — force-restart is the most destructive
// keeper action, so it re-verifies identity at the moment of firing.
func TestKeeperForceRestartFn_RefusesNonUUIDv4SID_hksuxt(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	const agent = "captain-hksuxt"
	writeSidFile_hksuxt(t, projectDir, agent, "not-a-valid-uuid")

	sentinel := filepath.Join(projectDir, "respawned.sentinel")
	fn := keeperForceRestartFn(true, projectDir, "touch "+sentinel)
	if fn == nil {
		t.Fatal("opted-in → want non-nil ForceRestartFn, got nil")
	}
	err := fn(context.Background(), agent)
	if !errors.Is(err, keeper.ErrLiveRecoverIdentityUntrusted) {
		t.Fatalf("non-UUIDv4 .sid: want ErrLiveRecoverIdentityUntrusted, got %v", err)
	}
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Errorf("respawn command ran despite untrusted identity (sentinel present)")
	}
}
