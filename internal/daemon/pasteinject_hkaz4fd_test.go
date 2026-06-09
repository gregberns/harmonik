package daemon_test

// pasteinject_hkaz4fd_test.go — unit test for ACTIVITY-AWARE launch suppression
// in pasteInjectQuitOnCommit (hk-az4fd).
//
// Problem addressed (P0 false-kill): the hk-jgxqc launch-suppression ceiling is
// an ABSOLUTE bound on how long an active-but-heartbeat-less pane may suppress
// the launch-verification kill.  Past that ceiling the daemon /quits the
// implementer UNCONDITIONALLY — even when the pane has an active child that is
// making REAL progress (editing files in the worktree) but simply has not
// committed yet, and whose agent_heartbeat events were drained by a competing
// tapCh reader under concurrency so firstHeartbeatSeen never flipped.  Any bead
// taking >ceiling to its first commit was false-killed mid-work.
//
// The fix: before applying the ceiling, consult a worktree-activity fingerprint
// (HEAD + `git status --porcelain`).  If the pane is active AND the fingerprint
// has advanced since the last check, treat that as a heartbeat — clear
// firstHeartbeatSeen so the launch branch is permanently bypassed and the run
// defers to the per-progress commit budget bounded by the 90-minute hard ceiling
// (hk-9vp51), instead of dying at the launch-suppression ceiling.
//
// hk-jgxqc intent preserved: a pane with an active child but ZERO worktree
// progress (stable fingerprint) still gets killed at the ceiling — that case is
// covered by TestPasteInjectLaunchSuppressionTerminates_ActivePaneForever in
// pasteinject_hkjgxqc_test.go (no worktree mutation there → fingerprint stable →
// ceiling fires), which must remain green alongside this test.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestPasteInjectActivityAware_ProgressingPaneSurvivesCeiling is the hk-az4fd
// regression: an implementer that is PROGRESSING (active pane + churning working
// tree) but has NOT committed and emits NO heartbeat must NOT be guillotined when
// the launch-suppression ceiling elapses.  The worktree activity is treated as a
// heartbeat, so the run survives well past the (short) ceiling and only ends via
// the normal commit-detected success path — with NO kill.
//
// Under the OLD code the launch-suppression ceiling would fire at ~ceiling and
// call killer.Kill, closing noChangeTimeoutCh; this test asserts neither happens.
func TestPasteInjectActivityAware_ProgressingPaneSurvivesCeiling(t *testing.T) {
	restore := hkjgxqcShortCeiling(
		5*time.Millisecond,  // poll interval
		20*time.Millisecond, // launch window (fires quickly → reaches launch branch)
		5*time.Second,       // staleness (irrelevant; suppressed by activity)
		5*time.Second,       // total/commit budget (must NOT bite — extended by activity)
		5*time.Millisecond,  // kill delay
		60*time.Millisecond, // launch-suppression ceiling (OLD code would kill here)
	)
	defer restore()
	// Keep the absolute hard ceiling far away so it never fires in this window.
	origHard := *daemon.ExportedCommitHardCeiling
	*daemon.ExportedCommitHardCeiling = 10 * time.Second
	defer func() { *daemon.ExportedCommitHardCeiling = origHard }()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hkfbydvLivenessQuitSender{}
	qs.alive.Store(true) // pane ALWAYS reports an active child process — never flips
	kl := &hkfbydvKiller{}
	noChangeCh := make(chan struct{})
	// Non-nil so heartbeatProvided=true; deliberately NEVER written (simulates the
	// drained-tapCh case where no heartbeat is ever observed by this watcher).
	eventCh := make(chan core.EventEnvelope)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Churn the working tree across ticks so the activity fingerprint keeps
	// advancing — past the 60ms ceiling — WITHOUT committing.  Each write changes
	// `git status --porcelain`, which the activity-aware launch branch reads.  At
	// ~300ms (well past the ceiling) land a commit so the run exits via the
	// success path rather than running to ctx timeout.
	go func() {
		for i := 0; i < 12; i++ {
			_ = os.WriteFile(
				filepath.Join(wtPath, "work.txt"),
				[]byte("progress "+time.Now().String()),
				0600,
			)
			time.Sleep(25 * time.Millisecond)
		}
		// Stage + commit to trigger the commit-detected success exit.
		hk7srrdGitBg(wtPath, "add", ".")
		hk7srrdGitBg(wtPath, "commit", "-m", "work landed")
	}()

	// Runs synchronously; returns when the commit is detected (success path).
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	if ctx.Err() != nil {
		t.Fatalf("call did not return before ctx deadline — the run hung instead of exiting on commit")
	}
	// The activity-aware path must have bypassed the launch ceiling: NO kill.
	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill calls: want 0 (progressing pane must survive the launch ceiling and exit via commit), got %d", got)
	}
	// noChangeTimeoutCh must NOT be closed (no noChange kill happened).
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh was closed — a progressing pane was false-killed at the launch ceiling (hk-az4fd regression)")
	default:
		// expected: clean commit-detected exit
	}
}

// TestPasteInjectActivityAware_StablePaneStillKilledAtCeiling is the
// complementary guard: a pane that is active but makes ZERO worktree progress
// (no file edits, no commit) MUST still be killed once the launch-suppression
// ceiling elapses — the hk-jgxqc intent is preserved.  This mirrors the
// hk-jgxqc ActivePaneForever test but is asserted here explicitly to lock the
// activity-aware change against accidentally suppressing the wedge kill.
func TestPasteInjectActivityAware_StablePaneStillKilledAtCeiling(t *testing.T) {
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
	qs := &hkfbydvLivenessQuitSender{}
	qs.alive.Store(true) // pane active forever, but the worktree is NEVER touched
	kl := &hkfbydvKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope) // never written

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)
	elapsed := time.Since(start)

	if ctx.Err() != nil {
		t.Fatalf("call did not return before ctx deadline — stable-pane suppression looped forever (hk-jgxqc regression)")
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (stable pane → ceiling fires despite active pane), got %d", got)
	}
	select {
	case <-noChangeCh:
		// expected
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after the ceiling-bounded launch kill")
	}
	// Returned roughly at the ceiling, not at the 10s total timeout.
	if elapsed > 2*time.Second {
		t.Errorf("kill fired after %v — far past the 120ms ceiling; stable-pane suppression was not bounded", elapsed)
	}
}
