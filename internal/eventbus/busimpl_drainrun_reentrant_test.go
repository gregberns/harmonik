package eventbus_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// TestBusImpl_DrainRunWaitsForReentrantCascade is the regression guard for
// hk-4hctu: a re-entrant EmitWithRunID issued from within a handler DURING that
// run's DrainRun must be tracked and delivered before DrainRun returns.
//
// The bug: DrainRun used to seal the run and wait on a per-run WaitGroup. Once
// sealed, addRunDrainer returned nil for a re-entrant EmitWithRunID, so the
// cascade's delivery goroutine was tracked ONLY in the global inflight counter,
// not the per-run WaitGroup. DrainRun could therefore return before the
// run-scoped cascade tail was delivered to that run's consumers — the analogous
// seal-orphan to hk-okzy1's global-Drain bug.
//
// The fix replaces the per-run WaitGroup + seal with a per-run in-flight counter
// and a condition variable: a re-entrant emit during DrainRun bumps the counter,
// so DrainRun keeps waiting until the cascade is fully delivered.
func TestBusImpl_DrainRunWaitsForReentrantCascade(t *testing.T) {
	bus := eventbus.NewBusImpl()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	runID := core.RunID(id)
	ctx := context.Background()
	payload := []byte(`{"k":"v"}`)

	var cascadeDelivered int32 // set to 1 when the re-entrant (B) event is delivered

	aStarted := make(chan struct{}) // closed when the initial (A) handler begins
	proceed := make(chan struct{})  // A blocks here until DrainRun is waiting

	sub := core.Subscription{
		ConsumerID:    "cascade-consumer",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			switch evt.Type {
			case string(core.EventTypeRunStarted):
				// Initial event A: announce we're in-flight, wait until DrainRun
				// is active, then emit the run-scoped cascade B while the run is
				// mid-drain — the exact window the seal-orphan bug drops.
				close(aStarted)
				<-proceed
				if reErr := bus.EmitWithRunID(ctx, runID, core.EventTypeRunCompleted, payload); reErr != nil {
					t.Errorf("re-entrant EmitWithRunID: %v", reErr)
				}
			case string(core.EventTypeRunCompleted):
				// Cascade event B: record delivery. A short delay widens the
				// window in which a buggy DrainRun would return early.
				time.Sleep(10 * time.Millisecond)
				atomic.StoreInt32(&cascadeDelivered, 1)
			}
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	rd, ok := bus.(eventbus.RunDrainer)
	if !ok {
		t.Fatal("bus does not implement eventbus.RunDrainer")
	}

	// Emit A. addRunDrainer increments the run counter synchronously before the
	// dispatch goroutine launches, so the run is in-flight the moment Emit
	// returns; the A handler then parks on `proceed`.
	if err := bus.EmitWithRunID(ctx, runID, core.EventTypeRunStarted, payload); err != nil {
		t.Fatalf("EmitWithRunID(A): %v", err)
	}
	<-aStarted

	drainDone := make(chan error, 1)
	go func() {
		drainDone <- rd.DrainRun(ctx, runID)
	}()

	// Give DrainRun time to enter its wait (and, under the old code, to seal the
	// run) before A emits the cascade. This makes the seal-orphan reproduce
	// deterministically on the buggy implementation.
	time.Sleep(20 * time.Millisecond)
	close(proceed)

	select {
	case err := <-drainDone:
		if err != nil {
			t.Fatalf("DrainRun returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DrainRun did not return within 2s")
	}

	if atomic.LoadInt32(&cascadeDelivered) != 1 {
		t.Fatal("DrainRun returned before the re-entrant run-scoped cascade was delivered (hk-4hctu seal-orphan)")
	}
}
