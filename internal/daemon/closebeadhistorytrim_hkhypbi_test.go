package daemon

// closebeadhistorytrim_hkhypbi_test.go — unit tests for the pre-close
// .br_history trim and BrUnavailable-as-success-after-merge paths (hk-hypbi).
//
// These tests live in package daemon (not daemon_test) so they can access
// unexported symbols directly.
//
// Bead ref: hk-hypbi.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// ─────────────────────────────────────────────────────────────────────────────
// Stubs
// ─────────────────────────────────────────────────────────────────────────────

// closeCaptureAdapter records CloseBead calls so tests can count invocations.
type closeCaptureAdapter struct {
	closeCalls int
	closeErr   error // returned on every CloseBead call
}

func (a *closeCaptureAdapter) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return nil, nil
}
func (a *closeCaptureAdapter) ShowBead(_ context.Context, _ core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{Status: "closed"}, nil
}
func (a *closeCaptureAdapter) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}
func (a *closeCaptureAdapter) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	a.closeCalls++
	return a.closeErr
}
func (a *closeCaptureAdapter) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func hkhypbiMakeRunID() core.RunID {
	return core.RunID(uuid.New())
}

// makeMinimalDeps returns a *workLoopDeps with only the fields needed by
// closeBeadWithHistoryTrim wired up.
func makeMinimalDeps(adapter *closeCaptureAdapter, projectDir string, skipRotation bool) *workLoopDeps {
	return &workLoopDeps{
		brAdapter:             adapter,
		projectDir:            projectDir,
		skipBrHistoryRotation: skipRotation,
		intentLogDir:          os.TempDir(),
		brTimeoutCfg:          brcli.TimeoutConfig{},
		tidGen:                core.NewTransitionIDGenerator(),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// closeBeadWithHistoryTrim tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCloseBeadWithHistoryTrim_SkipsRotationWhenFlagSet verifies that when
// skipBrHistoryRotation is true, runBrHistoryRotationPreflight is NOT called
// (even a non-existent projectDir does not cause an error — the trim is skipped).
func TestCloseBeadWithHistoryTrim_SkipsRotationWhenFlagSet(t *testing.T) {
	adapter := &closeCaptureAdapter{}
	deps := makeMinimalDeps(adapter, "/nonexistent-project-dir", true)

	runID := hkhypbiMakeRunID()
	tid, _ := deps.tidGen.Next()
	beadID := core.BeadID("hk-test-skip")

	err := deps.closeBeadWithHistoryTrim(context.Background(), runID, tid, beadID, false)
	if err != nil {
		t.Fatalf("closeBeadWithHistoryTrim: unexpected error: %v", err)
	}
	if adapter.closeCalls != 1 {
		t.Fatalf("expected 1 CloseBead call, got %d", adapter.closeCalls)
	}
}

// TestCloseBeadWithHistoryTrim_TrimsBeforeClose verifies that when
// skipBrHistoryRotation is false and a real .br_history directory exists with
// more than brHistoryCloseTrimKeep entries, the excess entries are archived
// before CloseBead is called.
func TestCloseBeadWithHistoryTrim_TrimsBeforeClose(t *testing.T) {
	projectDir := t.TempDir()
	historyDir := filepath.Join(projectDir, ".beads", ".br_history")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Populate historyDir with brHistoryCloseTrimKeep+5 entries.
	total := brHistoryCloseTrimKeep + 5
	for i := 0; i < total; i++ {
		name := filepath.Join(historyDir, strings.Repeat("0", 8)+string(rune('a'+i))+".json")
		if err := os.WriteFile(name, []byte("{}"), 0o600); err != nil {
			t.Fatalf("WriteFile %d: %v", i, err)
		}
		// Small sleep so mtime ordering is deterministic on fast filesystems.
		time.Sleep(2 * time.Millisecond)
	}

	adapter := &closeCaptureAdapter{}
	deps := makeMinimalDeps(adapter, projectDir, false)

	runID := hkhypbiMakeRunID()
	tid, _ := deps.tidGen.Next()
	beadID := core.BeadID("hk-test-trim")

	if err := deps.closeBeadWithHistoryTrim(context.Background(), runID, tid, beadID, false); err != nil {
		t.Fatalf("closeBeadWithHistoryTrim: %v", err)
	}

	// After the call, the history dir should hold ≤ brHistoryCloseTrimKeep entries.
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) > brHistoryCloseTrimKeep {
		t.Errorf("history dir has %d entries after trim; want ≤ %d", len(entries), brHistoryCloseTrimKeep)
	}

	// CloseBead must have been called exactly once.
	if adapter.closeCalls != 1 {
		t.Fatalf("expected 1 CloseBead call, got %d", adapter.closeCalls)
	}
}

// TestCloseBeadWithHistoryTrim_BrUnavailablePassedThrough verifies that a
// BrUnavailable error from CloseBead is returned as-is so callers can apply
// the correct emitDone branch.
func TestCloseBeadWithHistoryTrim_BrUnavailablePassedThrough(t *testing.T) {
	adapter := &closeCaptureAdapter{closeErr: brcli.BrUnavailable}
	deps := makeMinimalDeps(adapter, t.TempDir(), true)

	runID := hkhypbiMakeRunID()
	tid, _ := deps.tidGen.Next()
	beadID := core.BeadID("hk-test-unavail")

	err := deps.closeBeadWithHistoryTrim(context.Background(), runID, tid, beadID, false)
	if !errors.Is(err, brcli.BrUnavailable) {
		t.Fatalf("expected BrUnavailable sentinel; got: %v", err)
	}
}
