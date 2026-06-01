package daemon_test

// operatorpause_ry8q1_test.go — unit tests for OperatorPauseController (hk-ry8q1).
//
// Acceptance criteria:
//   - Pause emits operator_pause_status{pausing} then {paused}; sets IsPaused.
//   - Resume emits operator_resuming; clears IsPaused.
//   - Idempotent: second Pause is a no-op (no extra events); second Resume is no-op.
//   - Concurrent Pause calls serialize: only one wins and emits; others are no-ops.
//
// Bead ref: hk-ry8q1.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// newOpPauseController builds an OperatorPauseController backed by a real
// sealed in-memory bus and a shared event collector.
func newOpPauseController(t *testing.T) (*daemon.OperatorPauseController, *stubEventCollector) {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	col := &stubEventCollector{}
	// Wire the collector as a direct emitter facade: wrap bus calls through col.
	// We use the bus as-is and observe events via col wired into it.
	// Actually, pass col directly as the EventEmitter — stubEventCollector
	// already records events and does not emit errors.
	return daemon.ExportedNewOperatorPauseController(col), col
}

// ---------------------------------------------------------------------------
// TestOperatorPauseController_PauseThenResume
// ---------------------------------------------------------------------------

func TestOperatorPauseController_PauseThenResume(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl, col := newOpPauseController(t)

	// Initially not paused.
	if ctrl.IsPaused() {
		t.Fatal("expected not paused before first Pause call")
	}

	if err := ctrl.HandleOperatorPause(ctx, ""); err != nil {
		t.Fatalf("HandleOperatorPause: %v", err)
	}

	if !ctrl.IsPaused() {
		t.Fatal("expected IsPaused=true after HandleOperatorPause")
	}

	// Must have emitted pausing + paused.
	pauseEvents := collectEventsByType(col, string(core.EventTypeOperatorPauseStatus))
	if len(pauseEvents) != 2 {
		t.Fatalf("expected 2 operator_pause_status events; got %d", len(pauseEvents))
	}
	assertPauseStatus(t, pauseEvents[0], core.OperatorPauseStatusValuePausing)
	assertPauseStatus(t, pauseEvents[1], core.OperatorPauseStatusValuePaused)

	// Resume.
	if err := ctrl.HandleOperatorResume(ctx, ""); err != nil {
		t.Fatalf("HandleOperatorResume: %v", err)
	}

	if ctrl.IsPaused() {
		t.Fatal("expected IsPaused=false after HandleOperatorResume")
	}

	resumeEvents := collectEventsByType(col, string(core.EventTypeOperatorResuming))
	if len(resumeEvents) != 1 {
		t.Fatalf("expected 1 operator_resuming event; got %d", len(resumeEvents))
	}
	var resumePayload core.OperatorResumingPayload
	if err := json.Unmarshal(resumeEvents[0].Payload, &resumePayload); err != nil {
		t.Fatalf("unmarshal operator_resuming payload: %v", err)
	}
	if !resumePayload.Valid() {
		t.Fatalf("operator_resuming payload.Valid() = false: %+v", resumePayload)
	}
}

// ---------------------------------------------------------------------------
// TestOperatorPauseController_IdempotentPause
// ---------------------------------------------------------------------------

func TestOperatorPauseController_IdempotentPause(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl, col := newOpPauseController(t)

	if err := ctrl.HandleOperatorPause(ctx, ""); err != nil {
		t.Fatalf("first HandleOperatorPause: %v", err)
	}

	// Second pause: must be a no-op.
	if err := ctrl.HandleOperatorPause(ctx, ""); err != nil {
		t.Fatalf("second HandleOperatorPause: %v", err)
	}

	pauseEvents := collectEventsByType(col, string(core.EventTypeOperatorPauseStatus))
	if len(pauseEvents) != 2 {
		t.Fatalf("idempotent second pause must not emit extra events; got %d operator_pause_status events", len(pauseEvents))
	}
}

// ---------------------------------------------------------------------------
// TestOperatorPauseController_IdempotentResume
// ---------------------------------------------------------------------------

func TestOperatorPauseController_IdempotentResume(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl, col := newOpPauseController(t)

	// Resume on a non-paused controller: no-op and no event.
	if err := ctrl.HandleOperatorResume(ctx, ""); err != nil {
		t.Fatalf("HandleOperatorResume on non-paused: %v", err)
	}

	resumeEvents := collectEventsByType(col, string(core.EventTypeOperatorResuming))
	if len(resumeEvents) != 0 {
		t.Fatalf("resume on non-paused must not emit; got %d events", len(resumeEvents))
	}
}

// ---------------------------------------------------------------------------
// TestOperatorPauseController_ConcurrentPauseSerializes
// ---------------------------------------------------------------------------

// TestOperatorPauseController_ConcurrentPauseSerializes verifies that when
// many goroutines call HandleOperatorPause simultaneously, exactly one wins
// (emitting 2 events) and the rest are idempotent no-ops.
func TestOperatorPauseController_ConcurrentPauseSerializes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl, col := newOpPauseController(t)

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			errs[i] = ctrl.HandleOperatorPause(ctx, "")
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: HandleOperatorPause: %v", i, err)
		}
	}

	if !ctrl.IsPaused() {
		t.Fatal("expected IsPaused=true after concurrent pause storm")
	}

	// Exactly 2 operator_pause_status events regardless of how many goroutines ran.
	pauseEvents := collectEventsByType(col, string(core.EventTypeOperatorPauseStatus))
	if len(pauseEvents) != 2 {
		t.Fatalf("concurrent Pause must emit exactly 2 events (pausing+paused); got %d", len(pauseEvents))
	}
}

// ---------------------------------------------------------------------------
// TestOperatorPauseController_PauseResumeCycle
// ---------------------------------------------------------------------------

// TestOperatorPauseController_PauseResumeCycle verifies a full pause→resume→pause
// cycle emits the correct sequence of events.
func TestOperatorPauseController_PauseResumeCycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl, col := newOpPauseController(t)

	// Cycle 1.
	if err := ctrl.HandleOperatorPause(ctx, ""); err != nil {
		t.Fatalf("cycle1 Pause: %v", err)
	}
	if err := ctrl.HandleOperatorResume(ctx, ""); err != nil {
		t.Fatalf("cycle1 Resume: %v", err)
	}

	// Cycle 2.
	if err := ctrl.HandleOperatorPause(ctx, ""); err != nil {
		t.Fatalf("cycle2 Pause: %v", err)
	}
	if err := ctrl.HandleOperatorResume(ctx, ""); err != nil {
		t.Fatalf("cycle2 Resume: %v", err)
	}

	pauseEvents := collectEventsByType(col, string(core.EventTypeOperatorPauseStatus))
	resumeEvents := collectEventsByType(col, string(core.EventTypeOperatorResuming))
	if len(pauseEvents) != 4 {
		t.Fatalf("two cycles must emit 4 pause-status events; got %d", len(pauseEvents))
	}
	if len(resumeEvents) != 2 {
		t.Fatalf("two cycles must emit 2 resuming events; got %d", len(resumeEvents))
	}

	assertPauseStatus(t, pauseEvents[0], core.OperatorPauseStatusValuePausing)
	assertPauseStatus(t, pauseEvents[1], core.OperatorPauseStatusValuePaused)
	assertPauseStatus(t, pauseEvents[2], core.OperatorPauseStatusValuePausing)
	assertPauseStatus(t, pauseEvents[3], core.OperatorPauseStatusValuePaused)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// collectEventsByType returns all collected events of the given type.
func collectEventsByType(col *stubEventCollector, evtType string) []stubEmittedEvent {
	var out []stubEmittedEvent
	for _, e := range col.allEvents() {
		if e.EventType == evtType {
			out = append(out, e)
		}
	}
	return out
}

// assertPauseStatus verifies that evt is an operator_pause_status event with
// the expected status value.
func assertPauseStatus(t *testing.T, evt stubEmittedEvent, wantStatus core.OperatorPauseStatusValue) {
	t.Helper()
	var p core.OperatorPauseStatusPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		t.Fatalf("unmarshal operator_pause_status payload: %v", err)
	}
	if !p.Valid() {
		t.Fatalf("operator_pause_status payload.Valid()=false: %+v", p)
	}
	if p.Status != wantStatus {
		t.Errorf("operator_pause_status.status = %q; want %q", p.Status, wantStatus)
	}
}
