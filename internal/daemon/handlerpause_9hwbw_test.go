package daemon_test

// handlerpause_9hwbw_test.go — unit tests for HandlerPauseController (hk-9hwbw).
//
// Acceptance criteria per bead spec:
//   - pause records freeze-list correctly
//   - resume clears freeze-list
//   - concurrent Pause calls serialize (only one wins, the rest are no-ops)
//   - IsPaused / IsHandlerPaused reflect state correctly
//   - ResolvedAgentType returns a valid agent type
//   - Status snapshot matches state
//   - ErrHandlerNotPaused returned on Resume of a live handler
//   - paused_epoch increments monotonically across pause→resume cycles
//
// Bead ref: hk-9hwbw.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// makePauseCause constructs a minimal valid HandlerPauseCause for tests.
func makePauseCause(runID, beadID string) core.HandlerPauseCause {
	return core.HandlerPauseCause{
		FailureClass: core.FailureClassTransient,
		SubReason:    "rate_limit",
		SourceRunID:  runID,
		SourceBeadID: beadID,
		TrippedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// newTestController returns a HandlerPauseController backed by a real in-memory
// event bus.  The bus is not sealed, so Emit calls succeed without needing a
// JSONL writer.
func newTestController(t *testing.T) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	// Seal the bus so Emit works (sealed → live-delivery mode).
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	return daemon.NewHandlerPauseController(bus, nil)
}

// ---------------------------------------------------------------------------
// Basic pause / resume cycle
// ---------------------------------------------------------------------------

func TestHandlerPauseController_PauseThenResume(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newTestController(t)
	at := core.AgentTypeClaudeCode

	// Initially live.
	if ctrl.IsPaused(at) {
		t.Fatal("expected handler to be live before first Pause")
	}

	cause := makePauseCause("run-001", "hk-abc01")
	inFlight := []daemon.InFlightBeadRecord{
		{RunID: "run-001", BeadID: "hk-abc01", DispatchedAt: time.Now().UTC().Format(time.RFC3339Nano)},
		{RunID: "run-002", BeadID: "hk-abc02", DispatchedAt: time.Now().UTC().Format(time.RFC3339Nano)},
	}

	if err := ctrl.Pause(ctx, at, cause, inFlight); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	// Now paused.
	if !ctrl.IsPaused(at) {
		t.Fatal("expected handler to be paused after Pause")
	}

	// Status snapshot should reflect the freeze-list.
	snaps := ctrl.Status(at)
	if len(snaps) != 1 {
		t.Fatalf("Status returned %d snapshots, want 1", len(snaps))
	}
	snap := snaps[0]
	if !snap.Paused {
		t.Error("Status.Paused should be true")
	}
	if len(snap.InFlightAtPause) != 2 {
		t.Errorf("InFlightAtPause len = %d, want 2", len(snap.InFlightAtPause))
	}
	if snap.PausedEpoch != 1 {
		t.Errorf("PausedEpoch = %d, want 1", snap.PausedEpoch)
	}

	// Resume.
	if err := ctrl.Resume(ctx, at, core.HandlerResumedByOperator); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// Back to live.
	if ctrl.IsPaused(at) {
		t.Fatal("expected handler to be live after Resume")
	}

	snaps = ctrl.Status(at)
	if len(snaps) != 1 {
		t.Fatalf("Status returned %d snapshots after resume, want 1", len(snaps))
	}
	snap = snaps[0]
	if snap.Paused {
		t.Error("Status.Paused should be false after Resume")
	}
	if len(snap.InFlightAtPause) != 0 {
		t.Errorf("InFlightAtPause should be empty after Resume, got %d entries", len(snap.InFlightAtPause))
	}
	// Epoch is preserved across resume (monotonic).
	if snap.PausedEpoch != 1 {
		t.Errorf("PausedEpoch after resume = %d, want 1 (epoch is monotonic, not reset)", snap.PausedEpoch)
	}
}

// ---------------------------------------------------------------------------
// Freeze-list correctness
// ---------------------------------------------------------------------------

func TestHandlerPauseController_FreezeListRecordedCorrectly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newTestController(t)
	at := core.AgentTypeClaudeCode

	inFlight := []daemon.InFlightBeadRecord{
		{RunID: "run-aaa", BeadID: "hk-aaa01", DispatchedAt: "2026-05-18T14:00:00Z"},
		{RunID: "run-bbb", BeadID: "hk-bbb02", DispatchedAt: "2026-05-18T14:01:00Z"},
		{RunID: "run-ccc", BeadID: "hk-ccc03", DispatchedAt: "2026-05-18T14:02:00Z"},
	}
	cause := makePauseCause("run-aaa", "hk-aaa01")
	if err := ctrl.Pause(ctx, at, cause, inFlight); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	snaps := ctrl.Status(at)
	if len(snaps) != 1 {
		t.Fatalf("want 1 snapshot, got %d", len(snaps))
	}
	fl := snaps[0].InFlightAtPause
	if len(fl) != 3 {
		t.Fatalf("want 3 freeze-list entries, got %d", len(fl))
	}
	// Verify defensive copy: mutating the original slice does not affect the stored freeze-list.
	inFlight[0].RunID = "MUTATED"
	fl2 := ctrl.Status(at)[0].InFlightAtPause
	if fl2[0].RunID == "MUTATED" {
		t.Error("freeze-list was not defensively copied: mutation of caller slice affected stored list")
	}
}

// ---------------------------------------------------------------------------
// paused_epoch increments across pause→resume cycles
// ---------------------------------------------------------------------------

func TestHandlerPauseController_PausedEpochMonotonic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newTestController(t)
	at := core.AgentTypeClaudeCode

	for cycle := 1; cycle <= 3; cycle++ {
		cause := makePauseCause("run-x", "hk-x")
		if err := ctrl.Pause(ctx, at, cause, nil); err != nil {
			t.Fatalf("cycle %d Pause: %v", cycle, err)
		}
		snap := ctrl.Status(at)[0]
		if snap.PausedEpoch != cycle {
			t.Errorf("cycle %d: PausedEpoch = %d, want %d", cycle, snap.PausedEpoch, cycle)
		}
		if err := ctrl.Resume(ctx, at, core.HandlerResumedByOperator); err != nil {
			t.Fatalf("cycle %d Resume: %v", cycle, err)
		}
		// Epoch is monotonic: stays at 'cycle' after resume, not reset.
		snap = ctrl.Status(at)[0]
		if snap.PausedEpoch != cycle {
			t.Errorf("cycle %d: PausedEpoch after resume = %d, want %d", cycle, snap.PausedEpoch, cycle)
		}
	}
}

// ---------------------------------------------------------------------------
// Concurrent Pause calls serialize (only one wins)
// ---------------------------------------------------------------------------

func TestHandlerPauseController_ConcurrentPauseSerializes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newTestController(t)
	at := core.AgentTypeClaudeCode

	const goroutines = 20
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cause := makePauseCause("run-concurrent", "hk-c")
			cause.SubReason = "rate_limit"
			if err := ctrl.Pause(ctx, at, cause, nil); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent Pause returned error: %v", err)
	}

	// Exactly one pause should have been recorded.
	snaps := ctrl.Status(at)
	if len(snaps) != 1 {
		t.Fatalf("want 1 snapshot, got %d", len(snaps))
	}
	if !snaps[0].Paused {
		t.Error("expected handler to be paused after concurrent Pause calls")
	}
	// Only one epoch increment should have occurred.
	if snaps[0].PausedEpoch != 1 {
		t.Errorf("PausedEpoch = %d, want 1 (only first Pause should take effect)", snaps[0].PausedEpoch)
	}
}

// ---------------------------------------------------------------------------
// Resume of a live handler → ErrHandlerNotPaused
// ---------------------------------------------------------------------------

func TestHandlerPauseController_ResumeLiveHandler(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newTestController(t)
	at := core.AgentTypeClaudeCode

	err := ctrl.Resume(ctx, at, core.HandlerResumedByOperator)
	if err == nil {
		t.Fatal("expected error resuming a live handler, got nil")
	}
	var notPaused *daemon.ErrHandlerNotPaused
	if !errors.As(err, &notPaused) {
		t.Errorf("expected *daemon.ErrHandlerNotPaused, got %T: %v", err, err)
	}
	if notPaused.AgentType != at {
		t.Errorf("ErrHandlerNotPaused.AgentType = %q, want %q", notPaused.AgentType, at)
	}
}

// ---------------------------------------------------------------------------
// HandlerPauseChecker interface (queue.HandlerPauseChecker)
// ---------------------------------------------------------------------------

func TestHandlerPauseController_HandlerPauseCheckerInterface(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newTestController(t)
	at := core.AgentTypeClaudeCode

	// Before pause: IsHandlerPaused returns false.
	paused, err := ctrl.IsHandlerPaused(ctx, at)
	if err != nil {
		t.Fatalf("IsHandlerPaused (live): %v", err)
	}
	if paused {
		t.Error("IsHandlerPaused should be false before Pause")
	}

	// ResolvedAgentType should return a valid agent type.
	resolved, err := ctrl.ResolvedAgentType(ctx, core.BeadID("hk-test01"))
	if err != nil {
		t.Fatalf("ResolvedAgentType: %v", err)
	}
	if !resolved.Valid() {
		t.Errorf("ResolvedAgentType returned invalid AgentType %q", resolved)
	}

	// Pause then verify IsHandlerPaused.
	cause := makePauseCause("run-chk", "hk-chk01")
	if err := ctrl.Pause(ctx, at, cause, nil); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	paused, err = ctrl.IsHandlerPaused(ctx, at)
	if err != nil {
		t.Fatalf("IsHandlerPaused (paused): %v", err)
	}
	if !paused {
		t.Error("IsHandlerPaused should be true after Pause")
	}
}

// ---------------------------------------------------------------------------
// Status with empty agent type returns all known handler types
// ---------------------------------------------------------------------------

func TestHandlerPauseController_StatusAllHandlers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newTestController(t)

	// Pause two different agent types.
	for _, at := range []core.AgentType{core.AgentTypeClaudeCode, core.AgentTypePi} {
		cause := makePauseCause("run-multi", string(at))
		if err := ctrl.Pause(ctx, at, cause, nil); err != nil {
			t.Fatalf("Pause %q: %v", at, err)
		}
	}

	// Status("") should return both.
	snaps := ctrl.Status("")
	if len(snaps) != 2 {
		t.Errorf("Status(\"\") returned %d snapshots, want 2", len(snaps))
	}
	for _, s := range snaps {
		if !s.Paused {
			t.Errorf("Status for %q should be paused", s.AgentType)
		}
	}
}

// ---------------------------------------------------------------------------
// InFlightBeadRecordFromRunHandle helper
// ---------------------------------------------------------------------------

func TestInFlightBeadRecordFromRunHandle(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC)
	handle := &daemon.RunHandle{
		BeadID:    core.BeadID("hk-abc99"),
		StartedAt: ts,
	}
	runID := core.RunID{} // zero-value for test

	rec := daemon.InFlightBeadRecordFromRunHandle(runID, handle)
	if rec.BeadID != "hk-abc99" {
		t.Errorf("BeadID = %q, want hk-abc99", rec.BeadID)
	}
	if rec.DispatchedAt == "" {
		t.Error("DispatchedAt should not be empty")
	}
}
