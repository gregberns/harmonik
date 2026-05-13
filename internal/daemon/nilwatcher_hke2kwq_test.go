package daemon_test

// nilwatcher_hke2kwq_test.go — unit tests for nil-watcher guards in the substrate path (hk-e2kwq).
//
// When deps.substrate != nil (tmux-hosted sessions), handler.Launch returns
// watcher=nil because the bridge wire is the daemon Unix socket, not stdout.
// workloop.go and waitsocketgrace.go must nil-guard all five watcher call sites.
// A nil dereference panics as SIGSEGV — the RED smoke run (hk-kqdpf.5) hit
// exactly this crash.
//
// These tests verify:
//  1. waitWithSocketGrace(watcher=nil) does not panic and returns correct exitInfo.
//  2. waitWithSocketGrace(watcher=nil, ctx already cancelled) kills session and
//     returns promptly — tests the nil-guard in the ctx.Err() branch.
//
// Helper prefix: nilWatcherFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-e2kwq).
//
// Spec refs:
//   - specs/process-lifecycle.md §4.7 PL-021b (substrate seam)
//   - specs/claude-hook-bridge.md §4.7 CHB-020 (terminal-event mapping)
//
// Bead: hk-e2kwq.

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: nil watcher, normal exit — no panic, correct exitInfo
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitWithSocketGrace_NilWatcher_NoPanic verifies that waitWithSocketGrace
// does not panic when watcher is nil (substrate path). The function must return
// promptly with the correct exit code after sess.Wait() returns.
//
// This is the regression test for the SIGSEGV at workloop.go:672 surfaced by
// the 2026-05-13 RED smoke run (hk-kqdpf.5).
func TestWaitWithSocketGrace_NilWatcher_NoPanic(t *testing.T) {
	const runID = "run-nilw-nopanic-01"
	const sessionID = "claude-sess-nilw-nopanic-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	// Session exits immediately with code 0.
	sess := waitGraceFixtureNewSession(0, nil)
	sess.unblockWait()

	// watcher is nil — substrate path.
	outcome, ei := daemon.ExportedWaitWithSocketGrace(
		t.Context(), store, nil, sess, runID, sessionID,
	)

	// No panic occurred (we reached here). Verify exitInfo.
	if ei.ExitCode != 0 {
		t.Errorf("exitCode=%d, want 0", ei.ExitCode)
	}
	if ei.WaitErr != nil {
		t.Errorf("waitErr=%v, want nil", ei.WaitErr)
	}
	// No outcome was delivered — branch 3 (nil outcome expected).
	if outcome != nil {
		t.Errorf("expected nil outcome (branch 3, no hook delivered), got Kind=%q", outcome.Kind)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: nil watcher, ctx already cancelled — kills session, returns promptly
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitWithSocketGrace_NilWatcher_CtxCancelled verifies that when watcher is
// nil and the context is already cancelled on entry, waitWithSocketGrace calls
// sess.Kill and returns without hanging. This exercises the nil-guard in the
// ctx.Err() != nil branch of the substrate path.
func TestWaitWithSocketGrace_NilWatcher_CtxCancelled(t *testing.T) {
	const runID = "run-nilw-cancel-01"
	const sessionID = "claude-sess-nilw-cancel-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	// Session blocks until Kill is called (stub unblocks Wait on Kill).
	sess := waitGraceFixtureNewSession(0, nil)

	// Cancel context before calling waitWithSocketGrace.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// watcher is nil — substrate path. Kill must unblock Wait.
	outcome, ei := daemon.ExportedWaitWithSocketGrace(
		ctx, store, nil, sess, runID, sessionID,
	)

	// Kill must have been called to unblock the session.
	if !sess.wasKilled() {
		t.Error("expected sess.Kill to be called when ctx is cancelled and watcher is nil")
	}
	// exitCode is 0 (stub reports 0 regardless of kill).
	if ei.ExitCode != 0 {
		t.Errorf("exitCode=%d, want 0", ei.ExitCode)
	}
	// No outcome delivered — branch 3.
	if outcome != nil {
		t.Errorf("expected nil outcome after cancel, got Kind=%q", outcome.Kind)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: nil watcher, outcome arrives via socket — substrate completion path
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitWithSocketGrace_NilWatcher_OutcomeViaSocket verifies that when watcher
// is nil (substrate path), completion detected via HookSessionStore.WaitForOutcome
// works correctly. The outcome delivered to the store after sess.Wait() returns
// should be returned by waitWithSocketGrace (fast path via LatestOutcome).
func TestWaitWithSocketGrace_NilWatcher_OutcomeViaSocket(t *testing.T) {
	const runID = "run-nilw-outcome-01"
	const sessionID = "claude-sess-nilw-outcome-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	sess := waitGraceFixtureNewSession(0, nil)

	// Deliver the outcome before unblocking Wait (fast path: outcome already
	// present in store when LatestOutcome is checked after sess.Wait()).
	waitGraceFixtureDeliverOutcome(t, store, runID, sessionID, "WORK_COMPLETE")
	sess.unblockWait()

	// watcher is nil — substrate path.
	outcome, ei := daemon.ExportedWaitWithSocketGrace(
		t.Context(), store, nil, sess, runID, sessionID,
	)

	if outcome == nil {
		t.Fatal("expected non-nil outcome (socket delivery), got nil")
	}
	if outcome.Kind != "WORK_COMPLETE" {
		t.Errorf("outcome.Kind=%q, want WORK_COMPLETE", outcome.Kind)
	}
	if ei.ExitCode != 0 {
		t.Errorf("exitCode=%d, want 0", ei.ExitCode)
	}
}
