package daemon_test

// pasteinject_hkz4nif_test.go — tests for the Pi-reviewer pane seed-paste fix
// (hk-z4nif).
//
// # Bug
//
// When a DOT workflow node carries agent_type="reviewer" and the resolved
// harness is Pi (SessionIDCaptured), the DOT cascade calls pasteInjectOnLaunch
// with the perRunSubstrate as the pasteTarget. pasteInjectReviewer fires, posts
// the reviewer kick-off message to the tmux pane, and calls injectAndVerifySeed
// with marker "review-target.md". But the Pi pane shows NDJSON — not the seed
// text — so the marker is never found; after pasteVerifyAttempts the helper
// returns a non-empty failure reason ("unverified"), pasteinject_failed fires,
// and when no review.json appears the run terminates as run_failed.
//
// # Fix (hk-z4nif)
//
// dispatchDotAgenticNode in dot_cascade.go sets pasteTarget = nil for any
// harness whose SessionIDPolicy() == SessionIDCaptured. pasteInjectOnLaunch
// fast-returns when its substrate argument is nil, so no paste injection is
// attempted for Pi/Codex reviewer nodes.
//
// Helper prefix: hkz4nif.
// Bead: hk-z4nif.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// TestPasteInjectReviewer_PiLikePaneNDJSON_SeedMarkerAbsent documents the bug
// trigger: when the pane content resembles Pi NDJSON output (no "review-target.md"
// text), pasteInjectReviewer exhausts all paste attempts and returns a non-empty
// failure reason. This is exactly the failure mode that fires for Pi reviewer
// nodes before the hk-z4nif fix.
func TestPasteInjectReviewer_PiLikePaneNDJSON_SeedMarkerAbsent(t *testing.T) {
	t.Parallel()
	hkzexsjFastTimings(t)

	wtPath := t.TempDir()
	dir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review-target.md"), []byte("# Review target\n"), 0o644); err != nil {
		t.Fatalf("write review-target.md: %v", err)
	}

	// landOnAttempt: 0 — the pane never shows the "review-target.md" seed
	// marker (it shows NDJSON lines instead, as Pi does).
	stub := &hkzexsjPasteStub{landOnAttempt: 0}

	reason := daemon.ExportedPasteInjectReviewer(t.Context(), stub, "01hwxyz-hkz4nif-pi-rev", wtPath)
	if reason == "" {
		t.Fatal("hk-z4nif: expected non-empty failure reason when pane has no seed marker (Pi NDJSON path), got empty")
	}
	if !strings.Contains(reason, "unverified") {
		t.Errorf("hk-z4nif: failure reason should contain 'unverified'; got %q", reason)
	}

	writes, _ := stub.snapshot()
	if want := *daemon.ExportedPasteVerifyAttempts; writes != want {
		t.Errorf("hk-z4nif: want %d paste attempts before giving up, got %d", want, writes)
	}
}

// TestPasteInjectOnLaunch_NilSubstrate_IsNoop verifies the fix effect: when
// dispatchDotAgenticNode sets pasteTarget = nil for a Pi reviewer node,
// pasteInjectOnLaunch closes its briefDelivered channel immediately without
// writing to any pane.
func TestPasteInjectOnLaunch_NilSubstrate_IsNoop(t *testing.T) {
	t.Parallel()

	wtPath := t.TempDir()

	// nil substrate — what the fix produces for SessionIDCaptured harnesses.
	ch := daemon.ExportedPasteInjectOnLaunch(
		t.Context(),
		nil, // pasteTarget = nil → no-op
		"01hwxyz-hkz4nif-nil",
		handlercontract.ReviewLoopPhaseReviewer,
		1,
		wtPath,
	)

	select {
	case <-ch:
		// channel closed immediately — no paste attempted, correct.
	case <-time.After(2 * time.Second):
		t.Fatal("hk-z4nif: pasteInjectOnLaunch with nil substrate did not close briefDelivered promptly")
	}
}
