package daemon_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

func t4FixtureSetup(t *testing.T) string {
	t.Helper()
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)
	return projectDir
}

func t4FixtureDeps(t *testing.T, projectDir string, ledger *t4StubLedger, handlerBinary string, handlerArgs []string) daemon.TestRuntimeParams {
	t.Helper()
	return daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    handlerBinary,
		HandlerArgs:      handlerArgs,
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	}
}

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

	// closeCallCount counts CloseBead invocations, including the ones that return
	// closeErr. closed[] cannot stand in for it: the error path returns before
	// appending, so an empty closed[] is the same observation whether CloseBead
	// failed or was never called at all. Telling those apart is the whole point
	// of the T4-S4 assertions below (hk-vzxg5).
	closeCallCount int

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
	return []core.BeadRecord{{BeadID: id, Labels: workloopFixtureSingleLabels}}, nil
}

func (s *t4StubLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Labels: workloopFixtureSingleLabels}, nil
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
	s.closeCallCount++
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

func (s *t4StubLedger) getCloseCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeCallCount
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

	deps := daemon.ExportedTestRuntime(t4FixtureDeps(t, projectDir, ledger, "/bin/sh", []string{"-c", "exit 0"}))

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	time.Sleep(250 * time.Millisecond)

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

	cancel()
	if err := awaitLoopTeardownErr(t, done, "T4-S1 work loop"); err != nil {
		t.Errorf("T4-S1: work loop returned non-nil error on ctx cancel: %v", err)
	} else {
		t.Log("T4-S1: PASS — loop exited cleanly on cancel with empty queue")
	}

	if count := ledger.getReadyCallCount(); count == 0 {
		t.Error("T4-S1: FINDING: Ready was never called — loop did not poll with empty queue")
	} else {
		t.Logf("T4-S1: Ready called %d time(s) before cancel", count)
	}
}

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

	const beadID = core.BeadID("t4-claim-conflict-001")

	ledger := &t4StubLedger{}
	ledger.ready = []core.BeadID{beadID}
	ledger.claimErr = errors.New("t4: simulated external claim conflict")

	collector := &stubEventCollector{}
	depsParams := daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	}
	deps := daemon.ExportedTestRuntime(depsParams)

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	time.Sleep(300 * time.Millisecond)

	claimAttempts := ledger.getClaimCallCount()
	if claimAttempts == 0 {
		t.Log("T4-S2: claim not yet attempted at 300ms — may still be in Ready poll")
	} else {
		t.Logf("T4-S2: claim attempted %d time(s) at 300ms", claimAttempts)
	}

	if len(ledger.getClosedIDs()) > 0 {
		t.Errorf("T4-S2: FINDING: bead was closed despite ClaimBead failure: %v", ledger.getClosedIDs())
	}
	if len(ledger.getReopenedIDs()) > 0 {
		t.Logf("T4-S2: ReopenBead called after ClaimBead failure: %v — investigating intent", ledger.getReopenedIDs())
	}

	eventTypes := collector.eventTypes()
	for _, et := range eventTypes {
		if et == string(core.EventTypeRunStarted) {
			t.Errorf("T4-S2: FINDING: run_started emitted before successful claim — spec violation; events: %v", eventTypes)
			break
		}
	}

	cancel()
	if err := awaitLoopTeardownErr(t, done, "T4-S2 work loop"); err != nil {
		t.Errorf("T4-S2: loop returned error after claim conflict: %v", err)
	} else {
		t.Log("T4-S2: PASS — loop exited cleanly after claim conflict + ctx cancel")
	}
}

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

	requeueLedger := &t4RequeueLedger{
		inner: &t4StubLedger{
			ready: []core.BeadID{beadID},
		},
	}

	handlerDir := t.TempDir()
	handlerScript := handlerDir + "/handler.sh"
	counterFile := handlerDir + "/counter"
	gitPath, gitErr := exec.LookPath("git")
	if gitErr != nil {
		t.Fatalf("T4-S3: git not found on PATH: %v", gitErr)
	}
	handlerContent := `#!/bin/sh
exec 1>&2
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
` + gitPath + ` commit -q --allow-empty -m 'test: agent advanced HEAD'
exit 0
`
	if err := writeTestFile(t, handlerScript, handlerContent, 0o755); err != nil {
		t.Fatalf("T4-S3: write handler script: %v", err)
	}

	collector := &stubEventCollector{}
	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        requeueLedger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{handlerScript},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	for len(requeueLedger.getClosedIDs()) == 0 {
		select {
		case <-ctx.Done():
			t.Logf("T4-S3: bead not closed within the budget; reopened=%v closed=%v events=%v",
				requeueLedger.getReopenedIDs(), requeueLedger.getClosedIDs(), collector.eventTypes())
			t.Error("T4-S3: FINDING: bead was not closed after reopen+redispatch")
			goto done
		case <-time.After(50 * time.Millisecond):
		}
	}
done:

	reopened := requeueLedger.getReopenedIDs()
	if len(reopened) == 0 {
		t.Error("T4-S3: FINDING: ReopenBead was never called after non-zero handler exit")
	} else {
		t.Logf("T4-S3: ReopenBead called for: %v", reopened)
	}

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
	if err := awaitLoopTeardownErr(t, done, "T4-S3 work loop"); err != nil {
		t.Errorf("T4-S3: loop returned error after reopen+redispatch: %v", err)
	}
}

// TestT4_CloseBeadError confirms the work loop continues processing after a run
// fails. The loop must not crash, must not hang, and must attempt to process the
// next available bead.
//
// It also confirms the terminal event on that path. A CloseBead error fails the
// run: internal/runexec/run.go stepRunFinalizing takes the CloseError branch to
// doneClosed(..., false), and workloop.go emitRunCompleted maps success=false to
// run_failed. That is deliberate (hk-wfbxf) and normative --
// specs/execution-model.md EM-052 step 6 requires run_failed, not run_completed,
// on a CloseBead error -- because a bead left in_progress while the event log
// claims the run completed is split-brain. On the ORDINARY close ladder -- the
// one this fixture exercises -- a hard close error must also NOT reopen the bead
// (hk-c1ah6 / hk-hypbi): reopening would re-dispatch work whose ledger state
// nobody can trust. Scope that claim, because the same function contains a
// deliberate exception: stepRunFinalizing's AttentionClose branch (the review-loop
// budget-exhausted ladder, which cites the same beads) DOES reopen on a hard close
// error, before emitting the failed terminal. This fixture never sets
// AttentionClose, so the unconditional reading below would be wrong as a general
// rule and is right as a statement about this path.
//
// THE HANDLER MUST COMMIT, and that is load-bearing (hk-vzxg5). This test used to
// run `sh -c "exit 0"`, which exits clean without advancing HEAD. Every run then
// failed its pre-close guard -- "exited without advancing HEAD past <sha>" -- and
// took the reopen spine, so CloseBead was NEVER CALLED and the injected closeErr
// was inert. The test still passed, because its only observation of the terminal
// was a t.Logf that asserted nothing and its "no closed IDs" check is satisfied
// just as well by never closing anything. A committing WorktreeFactory does not
// fix it either; the commit has to come from the handler, inside the run.
//
// Finding candidates:
//   - Does the loop crash or hang when CloseBead returns an error?
//   - Does the loop continue to the next bead after a CloseBead failure?
//   - Which terminal does a close error produce, and is the bead reopened?
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
	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      workloopFixtureAdvanceHeadHandlerArgs(t),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	terminals := func() int {
		n := 0
		for _, et := range collector.eventTypes() {
			if et == string(core.EventTypeRunCompleted) || et == string(core.EventTypeRunFailed) {
				n++
			}
		}
		return n
	}
	for ledger.getCloseCallCount() < 2 || terminals() < 2 {
		select {
		case <-ctx.Done():
			t.Logf("T4-S4: %d claim(s), %d close attempt(s), %d terminal(s) before the budget ran out; loop may have crashed",
				ledger.getClaimCallCount(), ledger.getCloseCallCount(), terminals())
			t.Error("T4-S4: FINDING: work loop did not carry both beads to CloseBead and a terminal")
			goto doneS4
		case <-time.After(50 * time.Millisecond):
		}
	}

doneS4:
	events := collector.eventTypes()
	t.Logf("T4-S4: events emitted: %v", events)

	completedCount := 0
	failedCount := 0
	for _, et := range events {
		switch et {
		case string(core.EventTypeRunCompleted):
			completedCount++
		case string(core.EventTypeRunFailed):
			failedCount++
		}
	}

	if completedCount != 0 {
		t.Errorf("T4-S4: FINDING: run_completed emitted %d time(s) despite CloseBead failing; the bead is not closed and the event log says otherwise (hk-wfbxf, EM-052 step 6); events=%v", completedCount, events)
	}
	if failedCount < 2 {
		t.Errorf("T4-S4: FINDING: expected a run_failed terminal for each of the 2 beads, got %d; events=%v", failedCount, events)
	}

	if ids := ledger.getClosedIDs(); len(ids) != 0 {
		t.Errorf("T4-S4: expected no closed IDs since closeErr injected; got: %v", ids)
	}

	if opened := ledger.getReopenedIDs(); len(opened) != 0 {
		t.Errorf("T4-S4: FINDING: hard CloseBead error reopened %v; a bead whose close failed must be left alone for operator triage (hk-c1ah6/hk-hypbi)", opened)
	}

	cancel()
	if err := awaitLoopTeardownErr(t, done, "T4-S4 work loop"); err != nil {
		t.Errorf("T4-S4: FINDING: loop returned fatal error after CloseBead failures: %v", err)
	} else {
		t.Log("T4-S4: PASS — loop exited cleanly despite CloseBead errors")
	}
}

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

	const beadID = core.BeadID("t4-concurrent-001")

	sharedLedger := &t4StubLedger{
		ready: []core.BeadID{beadID},
	}

	collector1 := &stubEventCollector{}
	collector2 := &stubEventCollector{}

	deps1 := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        sharedLedger,
		Bus:              collector1,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})
	deps2 := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        sharedLedger,
		Bus:              collector2,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	done1 := make(chan error, 1)
	done2 := make(chan error, 1)
	go func() { done1 <- daemon.ExportedRunWorkLoop(ctx, deps1) }()
	go func() { done2 <- daemon.ExportedRunWorkLoop(ctx, deps2) }()

	for len(sharedLedger.getClosedIDs()) == 0 {
		select {
		case <-ctx.Done():
			t.Log("T4-S5: no bead closed within the budget with two concurrent loops")
			goto doneS5
		case <-time.After(50 * time.Millisecond):
		}
	}

doneS5:
	closedIDs := sharedLedger.getClosedIDs()
	t.Logf("T4-S5: CloseBead called %d time(s) total; IDs: %v", len(closedIDs), closedIDs)

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

	claimCount := sharedLedger.getClaimCallCount()
	t.Logf("T4-S5: ClaimBead called %d time(s)", claimCount)
	if claimCount > 1 {
		t.Logf("T4-S5: FINDING: ClaimBead called %d times for a single bead — "+
			"production SQLite `br update --claim` is atomic and prevents double-claim; "+
			"stub does not enforce this, so test uses stub to document the gap", claimCount)
	}

	cancel()
	for _, ch := range []chan error{done1, done2} {
		if err := awaitLoopTeardownErr(t, ch, "T4-S5 work loop"); err != nil {
			t.Errorf("T4-S5: loop returned error: %v", err)
		}
	}
}

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

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		// The handler commits while it runs, so HEAD advances past the node
		// baseline and the run reaches CloseBead. A bare `exit 0` leaves HEAD at
		// the baseline, which the node's no-advance guard treats as a failure
		// (run_failed + ReopenBead), and this scenario probes the close-success
		// ordering. The commit cannot live in a worktree factory — see
		// workloopFixtureAdvanceHeadHandlerArgs.
		HandlerArgs:      workloopFixtureAdvanceHeadHandlerArgs(t),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	for ledger.closeCallCount() == 0 {
		select {
		case <-ctx.Done():
			t.Error("T4-S6: CloseBead was not called within the budget")
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
		r.inner.mu.Lock()
		r.inner.ready = append(r.inner.ready, id)
		r.inner.mu.Unlock()
	}
	return err
}

func (r *t4RequeueLedger) getClosedIDs() []core.BeadID   { return r.inner.getClosedIDs() }
func (r *t4RequeueLedger) getReopenedIDs() []core.BeadID { return r.inner.getReopenedIDs() }

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

func writeTestFile(t *testing.T, path, content string, mode uint32) error {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), os.FileMode(mode)); err != nil {
		return err
	}
	return nil
}
