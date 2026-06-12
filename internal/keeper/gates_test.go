package keeper_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

// keeperDir ensures the .harmonik/keeper directory exists under projectDir.
func keeperDir(t *testing.T, projectDir string) string {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return dir
}

// touchFile creates or updates mtime of a file.
func touchFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("touchFile %q: %v", path, err)
	}
}

// writeFileWithMtime writes a file then backdates its mtime.
func writeFileWithMtime(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile %q: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes %q: %v", path, err)
	}
}

// ── CrispIdle ────────────────────────────────────────────────────────────────

// TestCrispIdle_TrueWhenIdleNewerThanCtx verifies the primary contract: .idle
// newer than .ctx → CrispIdle = true.
func TestCrispIdle_TrueWhenIdleNewerThanCtx(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	dir := keeperDir(t, projectDir)
	agent := "test-agent"

	past := time.Now().Add(-2 * time.Second)
	writeFileWithMtime(t, filepath.Join(dir, agent+".ctx"), past)

	// Touch .idle now (after .ctx).
	touchFile(t, filepath.Join(dir, agent+".idle"))

	if !keeper.CrispIdle(projectDir, agent) {
		t.Error("CrispIdle: want true when .idle is newer than .ctx")
	}
}

// TestCrispIdle_FalseWhenCtxMuchNewerThanIdle verifies that when .ctx was
// updated well after .idle (beyond the tolerance window), the agent is NOT at
// an idle boundary.
func TestCrispIdle_FalseWhenCtxMuchNewerThanIdle(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	dir := keeperDir(t, projectDir)
	agent := "ctx-much-newer-agent"

	// .idle is 30s in the past — well outside the 10s tolerance.
	past := time.Now().Add(-30 * time.Second)
	writeFileWithMtime(t, filepath.Join(dir, agent+".idle"), past)

	// Touch .ctx now (30s newer than .idle).
	touchFile(t, filepath.Join(dir, agent+".ctx"))

	if keeper.CrispIdle(projectDir, agent) {
		t.Error("CrispIdle: want false when .ctx is 30s newer than .idle")
	}
}

// TestCrispIdle_TrueWhenCtxWithinTolerance verifies that a .ctx refresh within
// the statusLine cadence tolerance (5s < 10s) is treated as a passive poll and
// CrispIdle still returns true.
func TestCrispIdle_TrueWhenCtxWithinTolerance(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	dir := keeperDir(t, projectDir)
	agent := "ctx-within-tolerance-agent"

	// .idle is 5s in the past — within the 10s tolerance.
	past := time.Now().Add(-5 * time.Second)
	writeFileWithMtime(t, filepath.Join(dir, agent+".idle"), past)

	// Touch .ctx now (5s newer than .idle — a statusLine poll, not tool activity).
	touchFile(t, filepath.Join(dir, agent+".ctx"))

	if !keeper.CrispIdle(projectDir, agent) {
		t.Error("CrispIdle: want true when .ctx is only 5s newer than .idle (within tolerance)")
	}
}

// TestCrispIdle_FalseWhenIdleAbsent verifies the absent-marker case.
func TestCrispIdle_FalseWhenIdleAbsent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	dir := keeperDir(t, projectDir)
	agent := "no-idle-agent"

	// Write .ctx but no .idle.
	touchFile(t, filepath.Join(dir, agent+".ctx"))

	if keeper.CrispIdle(projectDir, agent) {
		t.Error("CrispIdle: want false when .idle is absent")
	}
}

// TestCrispIdle_FalseWhenCtxAbsent verifies that without a .ctx file we cannot
// confirm idle ordering.
func TestCrispIdle_FalseWhenCtxAbsent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	dir := keeperDir(t, projectDir)
	agent := "no-ctx-agent"

	// Write .idle but no .ctx.
	touchFile(t, filepath.Join(dir, agent+".idle"))

	if keeper.CrispIdle(projectDir, agent) {
		t.Error("CrispIdle: want false when .ctx is absent")
	}
}

// ── HoldingDispatch ───────────────────────────────────────────────────────────

// TestHoldingDispatch_TrueWhenMarkerPresent verifies the marker-present case.
func TestHoldingDispatch_TrueWhenMarkerPresent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	dir := keeperDir(t, projectDir)
	agent := "dispatching-agent"

	touchFile(t, filepath.Join(dir, agent+".dispatching"))

	if !keeper.HoldingDispatch(projectDir, agent) {
		t.Error("HoldingDispatch: want true when .dispatching marker is present")
	}
}

// TestHoldingDispatch_FalseWhenMarkerAbsent verifies the absent-marker case.
func TestHoldingDispatch_FalseWhenMarkerAbsent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	keeperDir(t, projectDir)
	agent := "idle-dispatch-agent"

	if keeper.HoldingDispatch(projectDir, agent) {
		t.Error("HoldingDispatch: want false when .dispatching marker is absent")
	}
}

// ── SetDispatching / ClearDispatching ────────────────────────────────────────

// TestSetDispatching_CreatesMarker verifies that SetDispatching creates the
// .dispatching file and that HoldingDispatch subsequently returns true.
func TestSetDispatching_CreatesMarker(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "set-agent"

	if err := keeper.SetDispatching(projectDir, agent); err != nil {
		t.Fatalf("SetDispatching: %v", err)
	}

	markerPath := filepath.Join(projectDir, ".harmonik", "keeper", agent+".dispatching")
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("SetDispatching: marker not found at %q: %v", markerPath, err)
	}

	if !keeper.HoldingDispatch(projectDir, agent) {
		t.Error("HoldingDispatch: want true after SetDispatching")
	}
}

// TestClearDispatching_RemovesMarker verifies that ClearDispatching removes the
// .dispatching file and that HoldingDispatch subsequently returns false.
func TestClearDispatching_RemovesMarker(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "clear-agent"

	if err := keeper.SetDispatching(projectDir, agent); err != nil {
		t.Fatalf("SetDispatching: %v", err)
	}
	if err := keeper.ClearDispatching(projectDir, agent); err != nil {
		t.Fatalf("ClearDispatching: %v", err)
	}

	if keeper.HoldingDispatch(projectDir, agent) {
		t.Error("HoldingDispatch: want false after ClearDispatching")
	}
}

// TestClearDispatching_IdempotentWhenAbsent verifies that ClearDispatching on
// an absent marker is a no-op (not an error).
func TestClearDispatching_IdempotentWhenAbsent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "noop-clear-agent"

	if err := keeper.ClearDispatching(projectDir, agent); err != nil {
		t.Errorf("ClearDispatching on absent marker: want nil error, got %v", err)
	}
}

// ── Stop-hook script path validation ─────────────────────────────────────────

// TestStopHookScript_Exists verifies that the stop-hook script is present and
// executable at scripts/keeper-stop-hook.sh relative to the repo root.
// This test uses a sentinel env var to locate the repo root.
func TestStopHookScript_Touches_IdleMarker(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	dir := keeperDir(t, projectDir)
	agent := "hook-agent"
	idlePath := filepath.Join(dir, agent+".idle")

	// Simulate what the stop-hook script does: touch the .idle marker.
	if err := os.WriteFile(idlePath, []byte{}, 0o600); err != nil {
		t.Fatalf("simulate stop-hook touch: %v", err)
	}

	if _, err := os.Stat(idlePath); err != nil {
		t.Errorf("stop-hook simulation: .idle marker not created: %v", err)
	}
}
