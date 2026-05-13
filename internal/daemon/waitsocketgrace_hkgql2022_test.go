package daemon_test

// waitsocketgrace_hkgql2022_test.go — unit tests for waitWithSocketGrace (hk-gql20.22).
//
// Covers:
//   1. Stop hook arrives before Wait returns → outcome returned, no grace wait.
//   2. Wait returns first, outcome arrives within grace → outcome returned.
//   3. Wait returns first, no outcome within grace → nil outcome with valid exitInfo (CHB-020 branch 3).
//   4. ctx cancel kills the session and the function returns with the cancel-path exitInfo.
//
// Helper prefix: waitGraceFixture (implementer-protocol.md §Helper-prefix discipline;
// bead hk-gql20.22).
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.7 CHB-020 (terminal-event mapping)
//   - specs/claude-hook-bridge.md §4.10 CHB-025 (last-received-wins)
//
// Bead ref: hk-gql20.22.

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Session stub
// ─────────────────────────────────────────────────────────────────────────────

// waitGraceFixtureSession is a minimal stub of handler.Session for use in
// waitWithSocketGrace tests.  It reports a configurable exit code and can be
// told to block Wait until an external signal.
type waitGraceFixtureSession struct {
	mu       sync.Mutex
	exitCode int
	waitErr  error
	killed   bool

	// readyCh is closed by the test to unblock Wait.
	readyCh chan struct{}
}

func waitGraceFixtureNewSession(exitCode int, waitErr error) *waitGraceFixtureSession {
	return &waitGraceFixtureSession{
		exitCode: exitCode,
		waitErr:  waitErr,
		readyCh:  make(chan struct{}),
	}
}

// unblockWait closes readyCh, allowing Wait to return.
func (s *waitGraceFixtureSession) unblockWait() {
	select {
	case <-s.readyCh:
	default:
		close(s.readyCh)
	}
}

func (s *waitGraceFixtureSession) SendInput(_ context.Context, _ string) error { return nil }
func (s *waitGraceFixtureSession) Attach(_ context.Context) (io.Reader, error) {
	return io.NopCloser(nil), nil
}

func (s *waitGraceFixtureSession) Kill(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.killed = true
	s.unblockWait()
	return nil
}

func (s *waitGraceFixtureSession) Wait(_ context.Context) error {
	<-s.readyCh
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waitErr
}

func (s *waitGraceFixtureSession) Outcome() handler.Outcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	return handler.Outcome{ExitCode: s.exitCode}
}

func (s *waitGraceFixtureSession) Stdout() io.Reader   { return nil }
func (s *waitGraceFixtureSession) Stderr() io.Reader   { return nil }
func (s *waitGraceFixtureSession) CloseStdin() error   { return nil }
func (s *waitGraceFixtureSession) ID() core.SessionID  { return "" }
func (s *waitGraceFixtureSession) LogLocation() string { return "" }

// wasKilled returns true if Kill was called.
func (s *waitGraceFixtureSession) wasKilled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.killed
}

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// waitGraceFixtureMakePayload returns a JSON outcome_emitted payload for the
// given kind.
func waitGraceFixtureMakePayload(t *testing.T, kind string) json.RawMessage {
	t.Helper()
	pl, err := json.Marshal(map[string]string{"kind": kind})
	if err != nil {
		t.Fatalf("waitGraceFixtureMakePayload: marshal: %v", err)
	}
	return pl
}

// waitGraceFixtureDeliverOutcome delivers an outcome_emitted envelope to the
// store for the given (runID, sessionID) pair and fails the test on error.
func waitGraceFixtureDeliverOutcome(t *testing.T, store *daemon.HookSessionStoreExported, runID, sessionID, kind string) {
	t.Helper()
	payload := waitGraceFixtureMakePayload(t, kind)
	env := daemon.HookRelayEnvelopeExported{
		Type:            "outcome_emitted",
		RunID:           runID,
		ClaudeSessionID: sessionID,
		Payload:         payload,
	}
	status, reason := daemon.ExportedHookDispatch(store, env)
	if status != "ok" {
		t.Fatalf("waitGraceFixtureDeliverOutcome: dispatch status=%q reason=%q", status, reason)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: Stop hook arrives before Wait → outcome returned, no grace wait
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitWithSocketGrace_HookBeforeWait verifies the fast path: when the
// Stop hook delivers outcome_emitted BEFORE Wait returns, the outcome is
// present in the store on the LatestOutcome check and no grace window is needed.
func TestWaitWithSocketGrace_HookBeforeWait(t *testing.T) {
	const runID = "run-wsg-hook-before-01"
	const sessionID = "claude-sess-wsg-hook-before-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	sess := waitGraceFixtureNewSession(0, nil)
	watcher, closeDone := handlercontract.NewWatcherForTest()

	// Deliver outcome before signalling watcher done.
	waitGraceFixtureDeliverOutcome(t, store, runID, sessionID, "WORK_COMPLETE")

	// Close watcher (signals session exit) and unblock Wait concurrently.
	closeDone()
	sess.unblockWait()

	outcome, ei := daemon.ExportedWaitWithSocketGrace(
		t.Context(), store, watcher, sess, runID, sessionID,
	)

	if outcome == nil {
		t.Fatal("expected non-nil outcome from fast path, got nil")
	}
	if outcome.Kind != "WORK_COMPLETE" {
		t.Errorf("outcome.Kind=%q, want WORK_COMPLETE", outcome.Kind)
	}
	if ei.ExitCode != 0 {
		t.Errorf("exitCode=%d, want 0", ei.ExitCode)
	}
	if ei.WaitErr != nil {
		t.Errorf("waitErr=%v, want nil", ei.WaitErr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: Wait returns first, outcome within grace → outcome returned
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitWithSocketGrace_OutcomeWithinGrace verifies the slow path: Wait
// returns before the Stop hook, but the outcome arrives within the 3 s grace
// window.  The function must return the outcome, not nil.
func TestWaitWithSocketGrace_OutcomeWithinGrace(t *testing.T) {
	const runID = "run-wsg-grace-01"
	const sessionID = "claude-sess-wsg-grace-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	sess := waitGraceFixtureNewSession(0, nil)
	watcher, closeDone := handlercontract.NewWatcherForTest()

	// Signal watcher done and unblock Wait immediately.
	closeDone()
	sess.unblockWait()

	// Deliver the outcome after a short delay (simulating relay round-trip).
	go func() {
		time.Sleep(50 * time.Millisecond)
		waitGraceFixtureDeliverOutcome(t, store, runID, sessionID, "WORK_COMPLETE")
	}()

	outcome, ei := daemon.ExportedWaitWithSocketGrace(
		t.Context(), store, watcher, sess, runID, sessionID,
	)

	if outcome == nil {
		t.Fatal("expected non-nil outcome (arrived within grace), got nil")
	}
	if outcome.Kind != "WORK_COMPLETE" {
		t.Errorf("outcome.Kind=%q, want WORK_COMPLETE", outcome.Kind)
	}
	if ei.ExitCode != 0 {
		t.Errorf("exitCode=%d, want 0", ei.ExitCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: Wait returns first, no outcome within grace → nil outcome + exitInfo
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitWithSocketGrace_NoOutcomeAfterGrace verifies CHB-020 branch 3: when
// the process exits without any Stop hook delivery and the grace window expires,
// waitWithSocketGrace returns nil outcome with the correct exitInfo so the
// caller can emit agent_failed.
func TestWaitWithSocketGrace_NoOutcomeAfterGrace(t *testing.T) {
	const runID = "run-wsg-nograce-01"
	const sessionID = "claude-sess-wsg-nograce-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	// Non-zero exit code simulates a crash.
	sess := waitGraceFixtureNewSession(1, nil)
	watcher, closeDone := handlercontract.NewWatcherForTest()

	closeDone()
	sess.unblockWait()

	// Use a short-timeout context to force the grace window to be reached
	// quickly.  We override the test's context with a 200 ms deadline so this
	// test doesn't wait the full 3 s in CI.
	//
	// NOTE: the grace window is internal to waitWithSocketGrace (stopHookGrace
	// constant = 3 s).  To keep this test fast we rely on the fact that no
	// goroutine will deliver an outcome — WaitForOutcome will time out on the
	// internal graceCtx, not the caller's ctx.  The test timeout (set via
	// t.Context()) is a safety net only.
	outcome, ei := daemon.ExportedWaitWithSocketGrace(
		t.Context(), store, watcher, sess, runID, sessionID,
	)

	// Branch 3: no outcome should be returned.
	if outcome != nil {
		t.Errorf("expected nil outcome (branch 3), got Kind=%q", outcome.Kind)
	}
	// Exit code must be propagated.
	if ei.ExitCode != 1 {
		t.Errorf("exitCode=%d, want 1", ei.ExitCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4: ctx cancel kills session and propagates
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitWithSocketGrace_CtxCancel verifies that when the caller's context is
// cancelled before the watcher fires, the function calls sess.Kill and still
// returns promptly once the watcher's done channel closes.
func TestWaitWithSocketGrace_CtxCancel(t *testing.T) {
	const runID = "run-wsg-cancel-01"
	const sessionID = "claude-sess-wsg-cancel-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	sess := waitGraceFixtureNewSession(0, nil)
	watcher, closeDone := handlercontract.NewWatcherForTest()

	ctx, cancel := context.WithCancel(t.Context())

	type result struct {
		outcome *handler.ExportedOutcomeEmittedPayload
		ei      daemon.ExitInfoExported
	}
	resultCh := make(chan result, 1)

	go func() {
		o, ei := daemon.ExportedWaitWithSocketGrace(ctx, store, watcher, sess, runID, sessionID)
		resultCh <- result{outcome: o, ei: ei}
	}()

	// Give the goroutine time to enter the watcher select.
	time.Sleep(20 * time.Millisecond)

	// Cancel the context — Kill is triggered; the stub unblocks Wait internally.
	cancel()

	// The Kill stub closes readyCh; the test must also close the watcher done
	// channel to allow the function to proceed past <-watcher.Done().
	time.Sleep(10 * time.Millisecond)
	closeDone()

	select {
	case res := <-resultCh:
		// Kill must have been called.
		if !sess.wasKilled() {
			t.Error("expected sess.Kill to be called on ctx cancel, but it was not")
		}
		// No outcome was delivered → branch 3.
		if res.outcome != nil {
			t.Errorf("expected nil outcome after cancel, got Kind=%q", res.outcome.Kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waitWithSocketGrace did not return after ctx cancel")
	}
}
