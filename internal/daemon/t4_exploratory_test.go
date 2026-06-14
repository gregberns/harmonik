package daemon_test

// t4_exploratory_test.go — T4 bead-state edge cases (exploratory testing wave).
//
// Scope (per EXPLORATORY_TESTING_PLAN.md T4 row):
//   - Empty queue: daemon started with zero ready beads — should idle, not crash.
//   - Bead claimed externally between Ready and ClaimBead — race; loop should handle gracefully.
//   - ReopenBead path: handler exits non-zero — bead should be reopen-able, then dispatchable again.
//   - Bead deleted from DB while in flight — what happens at CloseBead?
//   - Two simultaneous work loops against the same beads.db — does ClaimBead detect conflict?
//
// All tests use the exported test seam (daemon.ExportedRunWorkLoop) so that no
// real `br` binary or Claude API is required. Findings from behaviour observed
// here are recorded in test/exploratory/findings-T4.md.
//
// Helper prefix: t4Fixture (per implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// T4 fixtures
// ─────────────────────────────────────────────────────────────────────────────

// t4FixtureSetup creates a minimal project directory tree and git repo for T4
// tests. Returns the project dir.
func t4FixtureSetup(t *testing.T) string {
	t.Helper()
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)
	return projectDir
}

// t4FixtureDeps constructs ExportedWorkLoopDeps for T4 tests with a
// configurable ledger and handler.
func t4FixtureDeps(t *testing.T, projectDir string, ledger *t4StubLedger, handlerBinary string, handlerArgs []string) daemon.WorkLoopDepsParams {
	t.Helper()
	return daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    handlerBinary,
		HandlerArgs:      handlerArgs,
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// t4StubLedger — configurable stub for T4 edge cases
// ─────────────────────────────────────────────────────────────────────────────

// t4StubLedger is a thread-safe stub bead ledger with configurable fault injection.
type t4StubLedger struct {
	mu sync.Mutex

	// ready is the queue of beads to return from Ready. Dequeued one at a time.
	ready []core.BeadID

	// claimErr, if non-nil, is returned from ClaimBead (simulates external claim race).
	claimErr error

	// closeErr, if non-nil, is returned from CloseBead (simulates deleted-bead scenario).
	closeErr error

	// claimCallCount counts ClaimBead invocations.
	claimCallCount int

	// closed collects bead IDs passed to CloseBead.
	closed []core.BeadID

	// opened collects bead IDs passed to ReopenBead.
	opened []core.BeadID

	// readyCallCount counts Ready invocations.
	readyCallCount int
}

func (s *t4StubLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readyCallCount++
	if len(s.ready) == 0 {
		return []core.BeadRecord{}, nil
	}
	id := s.ready[0]
	s.ready = s.ready[1:]
	return []core.BeadRecord{{BeadID: id}}, nil
}

func (s *t4StubLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (s *t4StubLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimCallCount++
	return s.claimErr
}

func (s *t4StubLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closeErr != nil {
		return s.closeErr
	}
	s.closed = append(s.closed, id)
	return nil
}

func (s *t4StubLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opened = append(s.opened, id)
	return nil
}

func (s *t4StubLedger) getClaimCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claimCallCount
}

func (s *t4StubLedger) getReadyCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readyCallCount
}

func (s *t4StubLedger) getClosedIDs() []core.BeadID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.BeadID, len(s.closed))
	copy(out, s.closed)
	return out
}

func (s *t4StubLedger) getReopenedIDs() []core.BeadID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.BeadID, len(s.opened))
	copy(out, s.opened)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// T4-S1: Empty queue — daemon idles without crashing
// ─────────────────────────────────────────────────────────────────────────────

// TestT4_EmptyQueue confirms the work loop idles cleanly when no beads are
// ready. It must not panic, must not error, and must return nil when the
// context is cancelled.
//
// Finding candidate: if the loop crashes or errors on empty queue.
func TestT4_EmptyQueue(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := t4FixtureSetup(t)

	ledger := &t4StubLedger{
		ready: nil, // no ready beads
	}

	deps := daemon.ExportedWorkLoopDeps(t4FixtureDeps(t, projectDir, ledger, "/bin/sh", []string{"-c", "exit 0"}))

	// Run the loop for a short period — should poll, find nothing, sleep, repeat.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Let it poll a few times (loop sleep is 2s; 3s budget gives at least 1 poll)
	time.Sleep(250 * time.Millisecond)

	// Verify the loop is still alive — not panicked, not exited early.
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("T4-S1: work loop returned error with empty queue: %v", err)
		}
		t.Log("T4-S1: FINDING: work loop exited before context cancel with empty queue")
		return
	default:
		t.Log("T4-S1: loop still alive after 250ms with empty queue — correct")
	}

	// Cancel and wait for clean return.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("T4-S1: work loop returned non-nil error on ctx cancel: %v", err)
		} else {
			t.Log("T4-S1: PASS — loop exited cleanly on cancel with empty queue")
		}
	case <-time.After(3 * time.Second):
		t.Error("T4-S1: FINDING: work loop did not exit within 3s after ctx cancel")
	}

	// Confirm Ready was called at least once.
	if count := ledger.getReadyCallCount(); count == 0 {
		t.Error("T4-S1: FINDING: Ready was never called — loop did not poll with empty queue")
	} else {
		t.Logf("T4-S1: Ready called %d time(s) before cancel", count)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T4-S2: Bead claimed externally between Ready and ClaimBead
// ─────────────────────────────────────────────────────────────────────────────

// TestT4_ClaimConflict simulates the race where another process claims the bead
// between the work loop's Ready poll and its ClaimBead call. The stub returns an
// error from ClaimBead. The loop should back off and retry on the next poll
// rather than crashing or getting stuck.
//
// Finding candidates:
//   - Does the loop retry after a ClaimBead error?
//   - Does it emit a stale run_started event before the claim fails?
func TestT4_ClaimConflict(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := t4FixtureSetup(t)

	// The first ClaimBead call fails (simulates external claim). Subsequent
	// calls succeed (simulates the bead being different next round).
	const beadID = core.BeadID("t4-claim-conflict-001")

	ledger := &t4StubLedger{}
	// Seed the bead for the first Ready call; it will be returned each time
	// we add it back manually. We use a custom approach: overload claimErr.
	ledger.ready = []core.BeadID{beadID}
	ledger.claimErr = errors.New("t4: simulated external claim conflict")

	collector := &stubEventCollector{}
	depsParams := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	}
	deps := daemon.ExportedWorkLoopDeps(depsParams)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait long enough for the loop to attempt the claim and back off.
	// Poll interval is 2s; give it 3s to show claim was attempted + backed off.
	time.Sleep(300 * time.Millisecond)

	// After 300ms: claim should have been attempted and failed.
	claimAttempts := ledger.getClaimCallCount()
	if claimAttempts == 0 {
		t.Log("T4-S2: claim not yet attempted at 300ms — may still be in Ready poll")
	} else {
		t.Logf("T4-S2: claim attempted %d time(s) at 300ms", claimAttempts)
	}

	// No bead should be closed or reopened — the claim failed before dispatch.
	if len(ledger.getClosedIDs()) > 0 {
		t.Errorf("T4-S2: FINDING: bead was closed despite ClaimBead failure: %v", ledger.getClosedIDs())
	}
	if len(ledger.getReopenedIDs()) > 0 {
		t.Logf("T4-S2: ReopenBead called after ClaimBead failure: %v — investigating intent", ledger.getReopenedIDs())
	}

	// run_started must NOT be emitted if claim failed.
	eventTypes := collector.eventTypes()
	for _, et := range eventTypes {
		if et == string(core.EventTypeRunStarted) {
			t.Errorf("T4-S2: FINDING: run_started emitted before successful claim — spec violation; events: %v", eventTypes)
			break
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("T4-S2: loop returned error after claim conflict: %v", err)
		} else {
			t.Log("T4-S2: PASS — loop exited cleanly after claim conflict + ctx cancel")
		}
	case <-time.After(3 * time.Second):
		t.Error("T4-S2: FINDING: work loop did not exit within 3s after ctx cancel")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T4-S3: ReopenBead path — handler exits non-zero, bead reopened, then dispatched again
// ─────────────────────────────────────────────────────────────────────────────

// TestT4_ReopenThenRedispatch confirms that after a non-zero handler exit the
// bead is reopened (ReopenBead called), and when the same bead appears in the
// next Ready result the loop dispatches and closes it on a success exit.
//
// Finding candidates:
//   - Does ReopenBead transition actually allow a subsequent claim?
//   - Does the loop emit run_failed after the non-zero exit?
//   - Does the second dispatch emit run_completed with success=true?
func TestT4_ReopenThenRedispatch(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := t4FixtureSetup(t)

	const beadID = core.BeadID("t4-reopen-redispatch-001")

	// Seed two iterations: first fails (exit 1), second succeeds (exit 0).
	// The second iteration simulates the operator "re-reading" the bead after reopen.
	// We inject the bead a second time into the ready queue after the first failure
	// via the t4RequeueLedger wrapper.

	// requeue-on-reopen ledger
	requeueLedger := &t4RequeueLedger{
		inner: &t4StubLedger{
			ready: []core.BeadID{beadID},
		},
	}

	// Handler: first call exits 1, second call exits 0.
	// We use a temp file to track invocation count.
	handlerDir := t.TempDir()
	handlerScript := handlerDir + "/handler.sh"
	counterFile := handlerDir + "/counter"
	handlerContent := `#!/bin/sh
COUNT_FILE=` + counterFile + `
COUNT=0
if [ -f "$COUNT_FILE" ]; then
  COUNT=$(cat "$COUNT_FILE")
fi
COUNT=$((COUNT + 1))
echo $COUNT > "$COUNT_FILE"
if [ "$COUNT" -le 1 ]; then
  exit 1
fi
exit 0
`
	if err := writeTestFile(t, handlerScript, handlerContent, 0o755); err != nil {
		t.Fatalf("T4-S3: write handler script: %v", err)
	}

	collector := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     requeueLedger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{handlerScript},
		// Advance HEAD via an --allow-empty commit so the single-mode no-commit
		// guard (hk-mmh8f) does not pre-empt this scenario's own failure/success
		// logic. Iteration 1's handler exits 1 (intentional failure → reopen via
		// the non-zero-exit path, unaffected by the advanced HEAD); iteration 2
		// exits 0 and needs HEAD advanced so the run merges to main and the bead
		// is closed (rather than being reopened by the no-commit guard).
		WorktreeFactory:  emptyCommitWorktreeFactory,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	// This scenario runs the work loop through TWO full iterations (iter-1 fails
	// exit 1 → reopen; iter-2 exits 0 → empty-commit merge-to-main → close). Each
	// iteration pays the stopHookGrace (~3s) window plus worktree-create + merge
	// overhead, so the close arrives at ~8s. The poll deadline / ctx budget are
	// sized generously (25s / 30s) so the test isn't flaky under parallel load —
	// it still breaks out of the poll the instant the bead closes (hk-st77j).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until bead is closed (ceiling 25s; the happy path closes at ~8s).
	deadline := time.After(25 * time.Second)
	for {
		if len(requeueLedger.getClosedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Logf("T4-S3: bead not closed within 25s; reopened=%v closed=%v events=%v",
				requeueLedger.getReopenedIDs(), requeueLedger.getClosedIDs(), collector.eventTypes())
			t.Error("T4-S3: FINDING: bead was not closed within 25s after reopen+redispatch")
			goto done
		case <-time.After(50 * time.Millisecond):
		}
	}
done:

	// Verify ReopenBead was called first.
	reopened := requeueLedger.getReopenedIDs()
	if len(reopened) == 0 {
		t.Error("T4-S3: FINDING: ReopenBead was never called after non-zero handler exit")
	} else {
		t.Logf("T4-S3: ReopenBead called for: %v", reopened)
	}

	// Verify run_failed event emitted.
	events := collector.eventTypes()
	foundFailed := false
	for _, et := range events {
		if et == string(core.EventTypeRunFailed) {
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Errorf("T4-S3: FINDING: run_failed event not emitted after non-zero exit; events: %v", events)
	}

	// Verify run_completed (success) emitted for the second dispatch.
	foundCompleted := false
	for _, et := range events {
		if et == string(core.EventTypeRunCompleted) {
			foundCompleted = true
			break
		}
	}
	if !foundCompleted {
		t.Logf("T4-S3: run_completed not yet seen; events: %v (may not have closed in time)", events)
	} else {
		t.Log("T4-S3: run_completed emitted — second dispatch succeeded")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("T4-S3: loop did not exit within 3s after cancel")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T4-S4: Bead deleted from DB while in flight — CloseBead error handling
// ─────────────────────────────────────────────────────────────────────────────

// TestT4_CloseBeadError confirms the work loop continues processing after a
// CloseBead error (simulating "bead deleted from DB while handler was running").
// The loop must not crash, must not hang, and must attempt to process the next
// available bead.
//
// Finding candidates:
//   - Does the loop crash or hang when CloseBead returns an error?
//   - Does the loop continue to the next bead after a CloseBead failure?
//   - Is run_completed still emitted even if CloseBead fails?
func TestT4_CloseBeadError(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := t4FixtureSetup(t)

	const beadID1 = core.BeadID("t4-close-fail-001")
	const beadID2 = core.BeadID("t4-close-fail-002")

	ledger := &t4StubLedger{
		ready:    []core.BeadID{beadID1, beadID2},
		closeErr: errors.New("t4: simulated DB bead-deleted-while-in-flight error"),
	}

	collector := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for both beads to be attempted (CloseBead called twice — both will fail).
	deadline := time.After(7 * time.Second)
	for {
		if ledger.getClaimCallCount() >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Logf("T4-S4: only %d claim(s) seen after 7s; closeErr injected; loop may have crashed",
				ledger.getClaimCallCount())
			t.Error("T4-S4: FINDING: work loop did not continue to next bead after CloseBead error")
			goto doneS4
		case <-time.After(50 * time.Millisecond):
		}
	}

doneS4:
	// run_completed should still be emitted even if CloseBead fails, because the
	// event is emitted BEFORE CloseBead in the current workloop.go code path.
	events := collector.eventTypes()
	t.Logf("T4-S4: events emitted: %v", events)

	completedCount := 0
	for _, et := range events {
		if et == string(core.EventTypeRunCompleted) {
			completedCount++
		}
	}
	if completedCount == 0 {
		t.Logf("T4-S4: run_completed not seen; workloop.go emits run_completed before CloseBead — check ordering")
	}
	t.Logf("T4-S4: run_completed emitted %d time(s) with closeErr injected", completedCount)

	// Closed IDs should be empty since CloseBead always fails.
	if ids := ledger.getClosedIDs(); len(ids) != 0 {
		t.Errorf("T4-S4: expected no closed IDs since closeErr injected; got: %v", ids)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("T4-S4: FINDING: loop returned fatal error after CloseBead failures: %v", err)
		} else {
			t.Log("T4-S4: PASS — loop exited cleanly despite CloseBead errors")
		}
	case <-time.After(3 * time.Second):
		t.Error("T4-S4: FINDING: work loop hung after CloseBead errors and ctx cancel")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T4-S5: Two concurrent work loops against the same bead queue
// ─────────────────────────────────────────────────────────────────────────────

// TestT4_ConcurrentLoops checks whether two work loops running against the
// same beadLedger (stub) cause double-dispatch of the same bead. In production
// two harmonik processes would share the same beads.db; ClaimBead is the
// serialization point. This test uses the stub to probe the protocol-level
// behavior.
//
// Finding candidates:
//   - Can the same bead be claimed and dispatched twice (double-dispatch)?
//   - Does the stub (and by proxy the production adapter) enforce claim exclusion?
//
// Note: with the stub ledger, both loops can claim the same bead because
// ClaimBead is a no-op. This is a KNOWN GAP in the stub, not a bug in production
// (production ClaimBead uses `br update --claim` which is atomic in SQLite).
// This test documents the expected behavior and the stub limitation.
func TestT4_ConcurrentLoops(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := t4FixtureSetup(t)

	// Single bead in the queue. Both loops will see it on their first Ready call.
	const beadID = core.BeadID("t4-concurrent-001")

	// We need a shared ledger so both loops see the same queue state.
	// Use t4StubLedger which drains one bead per Ready call.
	// First Ready call returns [beadID]; second returns [].
	sharedLedger := &t4StubLedger{
		ready: []core.BeadID{beadID},
	}

	collector1 := &stubEventCollector{}
	collector2 := &stubEventCollector{}

	deps1 := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        sharedLedger,
		Bus:              collector1,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})
	deps2 := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        sharedLedger,
		Bus:              collector2,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	done1 := make(chan error, 1)
	done2 := make(chan error, 1)
	go func() { done1 <- daemon.ExportedRunWorkLoop(ctx, deps1) }()
	go func() { done2 <- daemon.ExportedRunWorkLoop(ctx, deps2) }()

	// Wait for at least one close to be recorded.
	deadline := time.After(5 * time.Second)
	for {
		if len(sharedLedger.getClosedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Log("T4-S5: no bead closed within 5s with two concurrent loops")
			goto doneS5
		case <-time.After(50 * time.Millisecond):
		}
	}

doneS5:
	closedIDs := sharedLedger.getClosedIDs()
	t.Logf("T4-S5: CloseBead called %d time(s) total; IDs: %v", len(closedIDs), closedIDs)

	// Count how many times the same bead was closed.
	dupCount := 0
	for _, id := range closedIDs {
		if id == beadID {
			dupCount++
		}
	}
	if dupCount > 1 {
		t.Errorf("T4-S5: FINDING: bead %q was closed %d times (double-dispatch); "+
			"production ClaimBead (SQLite atomic) prevents this, but the stub does not", beadID, dupCount)
	} else if dupCount == 1 {
		t.Log("T4-S5: bead closed exactly once — stub dequeued to only one loop (drain behavior)")
	} else {
		t.Log("T4-S5: bead was not closed in observation window")
	}

	// Check claim counts.
	claimCount := sharedLedger.getClaimCallCount()
	t.Logf("T4-S5: ClaimBead called %d time(s)", claimCount)
	if claimCount > 1 {
		t.Logf("T4-S5: FINDING: ClaimBead called %d times for a single bead — "+
			"production SQLite `br update --claim` is atomic and prevents double-claim; "+
			"stub does not enforce this, so test uses stub to document the gap", claimCount)
	}

	cancel()
	for _, ch := range []chan error{done1, done2} {
		select {
		case err := <-ch:
			if err != nil {
				t.Errorf("T4-S5: loop returned error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("T4-S5: a loop did not exit within 3s after cancel")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T4-S6: CloseBead error ordering — run_completed emitted before or after CloseBead?
// ─────────────────────────────────────────────────────────────────────────────

// TestT4_EventOrderingOnCloseError checks the event ordering when CloseBead
// returns an error. In workloop.go (steps 9 & 10), run_completed is emitted
// in the same conditional block as CloseBead. This test verifies whether
// run_completed is emitted before CloseBead is attempted (so observability is
// retained even on close failure) or after (so run_completed is only emitted
// on successful close).
//
// Spec ref: specs/event-model.md §8.1 (run_completed); workloop.go steps 9 & 10.
func TestT4_EventOrderingOnCloseError(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := t4FixtureSetup(t)

	const beadID = core.BeadID("t4-event-order-001")

	// Intercept close to record whether run_completed was already emitted.
	type orderRecorder struct {
		t4StubLedger
		eventCollector       *stubEventCollector
		completedBeforeClose bool
	}

	collector := &stubEventCollector{}

	ledger := &t4OrderLedger{
		inner:     &t4StubLedger{ready: []core.BeadID{beadID}},
		collector: collector,
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "exit 0"},
		// Advance HEAD via an --allow-empty commit so the single-mode no-commit
		// guard (hk-mmh8f) does NOT reopen the bead before CloseBead is reached.
		// A bare `exit 0` handler leaves HEAD == parent, which the guard treats
		// as a no-commit failure (run_failed + ReopenBead); this scenario probes
		// the close-success ordering, so it needs a real (empty) commit to land.
		WorktreeFactory:  emptyCommitWorktreeFactory,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	// Real buildClaudeLaunchSpec + emptyCommitWorktreeFactory run; the handler
	// exits 0 (HEAD already advanced by the factory's --allow-empty commit),
	// stopHookGrace (~3s) fires, the run-branch merges to main, then CloseBead
	// is called. 15s total budget covers git worktree creation + grace window
	// + CI variability (hk-ngw3d).
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	deadline := time.After(10 * time.Second)
	for {
		if ledger.closeCallCount() > 0 {
			break
		}
		select {
		case <-deadline:
			t.Error("T4-S6: CloseBead was not called within 10s")
			goto doneS6
		case <-time.After(10 * time.Millisecond):
		}
	}

	t.Logf("T4-S6: run_completed emitted before CloseBead: %v", ledger.completedBeforeClose)
	if ledger.completedBeforeClose {
		t.Log("T4-S6: FINDING: run_completed is emitted BEFORE CloseBead — " +
			"observability is retained on close failure, but event precedes the " +
			"authoritative bead transition (spec §8.1 ordering question)")
	} else {
		t.Log("T4-S6: run_completed is emitted AFTER CloseBead — " +
			"run_completed is only observable when close succeeds")
	}

doneS6:
	cancel()
	<-done
}

// ─────────────────────────────────────────────────────────────────────────────
// Helper types for T4-S3 (requeue-on-reopen) and T4-S6 (order recorder)
// ─────────────────────────────────────────────────────────────────────────────

// t4RequeueLedger wraps t4StubLedger and re-queues a bead to ready after ReopenBead.
type t4RequeueLedger struct {
	inner *t4StubLedger
}

func (r *t4RequeueLedger) Ready(ctx context.Context) ([]core.BeadRecord, error) {
	return r.inner.Ready(ctx)
}

func (r *t4RequeueLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	return r.inner.ShowBead(ctx, id)
}

func (r *t4RequeueLedger) ClaimBead(ctx context.Context, dir string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, id core.BeadID) error {
	return r.inner.ClaimBead(ctx, dir, cfg, runID, tid, id)
}

func (r *t4RequeueLedger) CloseBead(ctx context.Context, dir string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, id core.BeadID, needsAttention bool) error {
	return r.inner.CloseBead(ctx, dir, cfg, runID, tid, id, needsAttention)
}

func (r *t4RequeueLedger) ReopenBead(ctx context.Context, dir string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, id core.BeadID, reason string) error {
	err := r.inner.ReopenBead(ctx, dir, cfg, runID, tid, id, reason)
	if err == nil {
		// Re-queue the bead so it can be dispatched again.
		r.inner.mu.Lock()
		r.inner.ready = append(r.inner.ready, id)
		r.inner.mu.Unlock()
	}
	return err
}

func (r *t4RequeueLedger) getClosedIDs() []core.BeadID   { return r.inner.getClosedIDs() }
func (r *t4RequeueLedger) getReopenedIDs() []core.BeadID { return r.inner.getReopenedIDs() }

// t4OrderLedger intercepts CloseBead to check event ordering.
type t4OrderLedger struct {
	inner                *t4StubLedger
	collector            *stubEventCollector
	completedBeforeClose bool
	mu                   sync.Mutex
	callCount            int
}

func (o *t4OrderLedger) Ready(ctx context.Context) ([]core.BeadRecord, error) {
	return o.inner.Ready(ctx)
}

func (o *t4OrderLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	return o.inner.ShowBead(ctx, id)
}

func (o *t4OrderLedger) ClaimBead(ctx context.Context, dir string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, id core.BeadID) error {
	return o.inner.ClaimBead(ctx, dir, cfg, runID, tid, id)
}

func (o *t4OrderLedger) CloseBead(ctx context.Context, dir string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, id core.BeadID, _ bool) error {
	// Check whether run_completed has already been emitted at close time.
	events := o.collector.eventTypes()
	hasCompleted := false
	for _, et := range events {
		if et == string(core.EventTypeRunCompleted) {
			hasCompleted = true
			break
		}
	}
	o.mu.Lock()
	o.completedBeforeClose = hasCompleted
	o.callCount++
	o.mu.Unlock()
	return o.inner.CloseBead(ctx, dir, cfg, runID, tid, id, false)
}

func (o *t4OrderLedger) ReopenBead(ctx context.Context, dir string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, id core.BeadID, reason string) error {
	return o.inner.ReopenBead(ctx, dir, cfg, runID, tid, id, reason)
}

func (o *t4OrderLedger) closeCallCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.callCount
}

// ─────────────────────────────────────────────────────────────────────────────
// writeTestFile helper
// ─────────────────────────────────────────────────────────────────────────────

func writeTestFile(t *testing.T, path, content string, mode uint32) error {
	t.Helper()
	//nolint:gosec // G306: test-only script; chmod required for execution
	if err := os.WriteFile(path, []byte(content), os.FileMode(mode)); err != nil {
		return err
	}
	return nil
}
