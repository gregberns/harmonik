package daemon_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/runregistry"
)

func makePauseCause(runID, beadID string) core.HandlerPauseCause {
	return core.HandlerPauseCause{
		FailureClass: core.FailureClassTransient,
		SubReason:    "rate_limit",
		SourceRunID:  runID,
		SourceBeadID: beadID,
		TrippedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func newTestController(t *testing.T) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	return daemon.NewHandlerPauseController(bus, nil)
}

func TestHandlerPauseController_PauseThenResume(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newTestController(t)
	at := core.AgentTypeClaudeCode

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

	if !ctrl.IsPaused(at) {
		t.Fatal("expected handler to be paused after Pause")
	}

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

	if err := ctrl.Resume(ctx, at, core.HandlerResumedByOperator); err != nil {
		t.Fatalf("Resume: %v", err)
	}

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
	if snap.PausedEpoch != 1 {
		t.Errorf("PausedEpoch after resume = %d, want 1 (epoch is monotonic, not reset)", snap.PausedEpoch)
	}
}

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
	inFlight[0].RunID = "MUTATED"
	fl2 := ctrl.Status(at)[0].InFlightAtPause
	if fl2[0].RunID == "MUTATED" {
		t.Error("freeze-list was not defensively copied: mutation of caller slice affected stored list")
	}
}

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
		snap = ctrl.Status(at)[0]
		if snap.PausedEpoch != cycle {
			t.Errorf("cycle %d: PausedEpoch after resume = %d, want %d", cycle, snap.PausedEpoch, cycle)
		}
	}
}

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

	snaps := ctrl.Status(at)
	if len(snaps) != 1 {
		t.Fatalf("want 1 snapshot, got %d", len(snaps))
	}
	if !snaps[0].Paused {
		t.Error("expected handler to be paused after concurrent Pause calls")
	}
	if snaps[0].PausedEpoch != 1 {
		t.Errorf("PausedEpoch = %d, want 1 (only first Pause should take effect)", snaps[0].PausedEpoch)
	}
}

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

func TestHandlerPauseController_HandlerPauseCheckerInterface(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newTestController(t)
	at := core.AgentTypeClaudeCode

	paused, err := ctrl.IsHandlerPaused(ctx, at)
	if err != nil {
		t.Fatalf("IsHandlerPaused (live): %v", err)
	}
	if paused {
		t.Error("IsHandlerPaused should be false before Pause")
	}

	resolved, err := ctrl.ResolvedAgentType(ctx, core.BeadID("hk-test01"))
	if err != nil {
		t.Fatalf("ResolvedAgentType: %v", err)
	}
	if !resolved.Valid() {
		t.Errorf("ResolvedAgentType returned invalid AgentType %q", resolved)
	}

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

func TestHandlerPauseController_StatusAllHandlers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newTestController(t)

	for _, at := range []core.AgentType{core.AgentTypeClaudeCode, core.AgentTypePi} {
		cause := makePauseCause("run-multi", string(at))
		if err := ctrl.Pause(ctx, at, cause, nil); err != nil {
			t.Fatalf("Pause %q: %v", at, err)
		}
	}

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

// TestHandlerPauseController_ConcurrentReadersWhileWriting verifies HP-035:
// multiple goroutines calling IsPaused simultaneously while another goroutine
// cycles through Pause / Resume must not deadlock and must always observe
// a consistent (boolean) pause state.
//
// Bead ref: hk-87u3q.
func TestHandlerPauseController_ConcurrentReadersWhileWriting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newTestController(t)
	at := core.AgentTypeClaudeCode

	const readers = 16
	const cycles = 50

	writerDone := make(chan struct{})

	var readerWg sync.WaitGroup
	for range readers {
		readerWg.Add(1)
		go func() {
			defer readerWg.Done()
			for {
				select {
				case <-writerDone:
					return
				default:
					_ = ctrl.IsPaused(at)
					_, _ = ctrl.PausedEpochFor(at)
				}
			}
		}()
	}

	for i := range cycles {
		cause := makePauseCause("run-rw", "hk-rw01")
		if err := ctrl.Pause(ctx, at, cause, nil); err != nil {
			close(writerDone)
			t.Fatalf("cycle %d Pause: %v", i, err)
		}
		if err := ctrl.Resume(ctx, at, core.HandlerResumedByOperator); err != nil {
			close(writerDone)
			t.Fatalf("cycle %d Resume: %v", i, err)
		}
	}

	close(writerDone)
	readerWg.Wait()

	if ctrl.IsPaused(at) {
		t.Error("expected handler to be live after all Pause/Resume cycles")
	}
	snaps := ctrl.Status(at)
	if len(snaps) != 1 {
		t.Fatalf("want 1 snapshot, got %d", len(snaps))
	}
	if snaps[0].PausedEpoch != cycles {
		t.Errorf("PausedEpoch = %d, want %d", snaps[0].PausedEpoch, cycles)
	}
}

func TestInFlightBeadRecordFromRunHandle(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC)
	handle := &runregistry.RunHandle{
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
