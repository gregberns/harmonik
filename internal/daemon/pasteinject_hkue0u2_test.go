package daemon_test

// pasteinject_hkue0u2_test.go — unit test for PANE-OUTPUT-GROWTH activity
// signal in pasteInjectQuitOnCommit (hk-ue0u2).
//
// Problem addressed (P0 false-kill — read-heavy beads): the hk-az4fd fix
// added worktree-activity as a heartbeat signal so implementers that are
// EDITING files survive the launch-suppression ceiling.  However, a
// READ-HEAVY implementer (e.g. T12 codex-registration) spends >12 minutes
// reading files and planning before the first worktree edit.  During that
// phase git status is clean, so the worktree fingerprint is stable, and
// hk-az4fd does not trigger — the run false-dies at the ceiling.
//
// The fix adds a second signal: pane output growth.  A reading/planning
// Claude streams visible output (tool results, LLM responses) to the tmux
// pane even when no files are being edited.  If the pane's output fingerprint
// (history_size + cursor_y) advances between ticks, the run treats that as
// activity and defers to the 90-minute hard ceiling rather than the 12-minute
// launch-suppression ceiling.
//
// hk-jgxqc / hk-az4fd intent preserved: a pane that reports an active child
// but produces NO progress on EITHER worktree activity OR pane output (both
// signals stable) is still killed at the ceiling — that case is the
// TestPasteInjectActivityAware_StablePaneStillKilledAtCeiling test in
// pasteinject_hkaz4fd_test.go (which must remain green).
//
// Test cases:
//   - TestPasteInjectPaneOutputGrowth_ReadHeavyBeadSurvivesCeiling: pane
//     output grows each tick (simulating a reading/planning Claude), worktree
//     is NEVER touched, no heartbeat → run must NOT be killed at the ceiling;
//     it exits via commit.
//   - TestPasteInjectPaneOutputGrowth_StableOutputStillKilledAtCeiling:
//     worktree stable AND pane output stable → ceiling kill fires (regression
//     guard; ensures the pane-output check does not break the wedge kill).

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

// hkue0u2PaneOutputQuitSender implements quitSender, paneLivenessChecker, AND
// paneOutputSizer.  The alive field controls liveness; outputFP is returned
// by PaneOutputFingerprint and can be advanced between calls to simulate a
// pane that is producing visible output.
type hkue0u2PaneOutputQuitSender struct {
	calls    atomic.Int64
	alive    atomic.Bool
	outputFP atomic.Value // string
}

func (q *hkue0u2PaneOutputQuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

func (q *hkue0u2PaneOutputQuitSender) PaneHasActiveProcess(_ context.Context) bool {
	return q.alive.Load()
}

func (q *hkue0u2PaneOutputQuitSender) PaneOutputFingerprint(_ context.Context) (string, bool) {
	fp, _ := q.outputFP.Load().(string)
	return fp, fp != ""
}

// Compile-time checks: satisfies both exported aliases.
var _ daemon.PaneLivenessCheckerExported = (*hkue0u2PaneOutputQuitSender)(nil)
var _ daemon.PaneOutputSizerExported = (*hkue0u2PaneOutputQuitSender)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectPaneOutputGrowth_ReadHeavyBeadSurvivesCeiling is the hk-ue0u2
// regression: a read-heavy implementer whose pane output grows (streaming
// responses, tool results) but which has NOT touched the worktree and emits
// NO heartbeat must NOT be guillotined at the launch-suppression ceiling.
// Pane output growth is treated as activity, so the run survives past the
// ceiling and exits via commit.
func TestPasteInjectPaneOutputGrowth_ReadHeavyBeadSurvivesCeiling(t *testing.T) {
	restore := hkjgxqcShortCeiling(
		5*time.Millisecond,  // poll interval
		20*time.Millisecond, // launch window (fires quickly → reaches launch branch)
		5*time.Second,       // staleness (irrelevant; suppressed by activity)
		5*time.Second,       // total/commit budget (must NOT bite — extended by activity)
		5*time.Millisecond,  // kill delay
		60*time.Millisecond, // launch-suppression ceiling (OLD code would kill here)
	)
	defer restore()
	origHard := *daemon.ExportedCommitHardCeiling
	*daemon.ExportedCommitHardCeiling = 10 * time.Second
	defer func() { *daemon.ExportedCommitHardCeiling = origHard }()

	wtPath, headSHA := hk7srrdWorktree(t)

	qs := &hkue0u2PaneOutputQuitSender{}
	qs.alive.Store(true)    // pane always has an active child
	qs.outputFP.Store("0 0") // initial fingerprint

	kl := &hkfbydvKiller{}
	noChangeCh := make(chan struct{})
	// Non-nil so heartbeatProvided=true; deliberately NEVER written (simulates
	// the drained-tapCh case where no agent_heartbeat reaches this watcher).
	eventCh := make(chan core.EventEnvelope)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Advance pane output fingerprint every tick — simulating a Claude that is
	// streaming responses and reading files but NOT touching the worktree.  At
	// ~300ms (well past the 60ms ceiling) land a commit so the run exits via
	// the success path rather than running to ctx timeout.
	go func() {
		for i := 1; i <= 12; i++ {
			time.Sleep(25 * time.Millisecond)
			qs.outputFP.Store(string(rune('0'+i)) + " " + string(rune('0'+i)))
		}
		// Worktree is NOT touched during the planning phase — only commit at end.
		hk7srrdGitBg(wtPath, "add", ".")
		_ = os.WriteFile(filepath.Join(wtPath, "plan.md"), []byte("plan"), 0600)
		hk7srrdGitBg(wtPath, "add", ".")
		hk7srrdGitBg(wtPath, "commit", "-m", "read-heavy bead landed")
	}()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	if ctx.Err() != nil {
		t.Fatalf("call did not return before ctx deadline — read-heavy bead was hung instead of exiting on commit")
	}
	// Pane output growth must have bypassed the ceiling: NO kill.
	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill calls: want 0 (read-heavy pane must survive via pane-output activity), got %d", got)
	}
	// noChangeTimeoutCh must NOT be closed.
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh was closed — read-heavy pane was false-killed at launch ceiling (hk-ue0u2 regression)")
	default:
		// expected: clean commit-detected exit
	}
}

// TestPasteInjectPaneOutputGrowth_StableOutputStillKilledAtCeiling guards the
// complement: a pane that is active but produces NO output growth AND NO
// worktree progress must still be killed at the ceiling.  This ensures
// hk-ue0u2 does not accidentally suppress the hk-jgxqc wedge-kill for a
// pane whose output fingerprint never changes (e.g. a truly-wedged claude
// that emits no tokens).
func TestPasteInjectPaneOutputGrowth_StableOutputStillKilledAtCeiling(t *testing.T) {
	restore := hkjgxqcShortCeiling(
		5*time.Millisecond,   // poll interval
		20*time.Millisecond,  // launch window
		5*time.Second,        // staleness (irrelevant)
		10*time.Second,       // total timeout (must NOT free us — ceiling is shorter)
		5*time.Millisecond,   // kill delay
		120*time.Millisecond, // launch-suppression ceiling (the bound under test)
	)
	defer restore()
	origHard := *daemon.ExportedCommitHardCeiling
	*daemon.ExportedCommitHardCeiling = 10 * time.Second
	defer func() { *daemon.ExportedCommitHardCeiling = origHard }()

	wtPath, headSHA := hk7srrdWorktree(t)

	qs := &hkue0u2PaneOutputQuitSender{}
	qs.alive.Store(true)     // pane active forever
	qs.outputFP.Store("0 0") // fingerprint NEVER advances

	kl := &hkfbydvKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope) // never written

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)
	elapsed := time.Since(start)

	if ctx.Err() != nil {
		t.Fatalf("call did not return before ctx deadline — stable-output suppression looped forever (hk-jgxqc / hk-ue0u2 regression)")
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (stable pane+output → ceiling fires), got %d", got)
	}
	select {
	case <-noChangeCh:
		// expected
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after the ceiling-bounded launch kill")
	}
	if elapsed > 2*time.Second {
		t.Errorf("kill fired after %v — far past the 120ms ceiling; stable-output suppression was not bounded", elapsed)
	}
}
