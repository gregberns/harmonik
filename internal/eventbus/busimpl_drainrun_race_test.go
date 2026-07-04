package eventbus_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// TestBusImpl_DrainRunConcurrentEmitNoWaitGroupMisuse is a regression test for
// the load-triggered whole-process fatal
//
//	fatal error: sync: WaitGroup misuse: Add called concurrently with Wait
//
// EmitWithRunID used to call the per-run WaitGroup's Add(1) OUTSIDE
// runDrainersMu; DrainRun calls that same WaitGroup's Wait(). When a run is
// torn down (DrainRun) while a lingering per-run goroutine still emits a
// run-scoped event, Add(1) races Wait() and Go aborts the whole process. This
// only manifests under concurrent dispatch (the daemon's max_concurrent>1
// reviewer/DOT-cascade loop), never at idle.
//
// The fix seals the run under runDrainersMu: once DrainRun begins, further
// EmitWithRunID for that run skips per-run tracking, so Add can never run
// concurrently with Wait.
//
// Without the fix this test aborts the test binary (an unrecoverable fatal, not
// a t.Fatal) within a few iterations; with it, it passes cleanly under -race.
func TestBusImpl_DrainRunConcurrentEmitNoWaitGroupMisuse(t *testing.T) {
	bus := eventbus.NewBusImpl()

	// An asynchronous consumer whose handler lingers briefly, so per-run
	// goroutines are still in-flight when DrainRun starts — the exact window the
	// race needs.
	sub := core.Subscription{
		ConsumerID:    "linger-consumer",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
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

	ctx := context.Background()
	payload := []byte(`{"k":"v"}`)

	// One runID, hammered from two goroutines: a continuous emitter (each emit
	// does runWG.Add(1) then the async goroutine Done()s, so the counter
	// oscillates through 0) racing a continuous DrainRun (runWG.Wait()). This is
	// the run-teardown-while-still-emitting window. Without the seal fix, an
	// Add(1) eventually lands when the counter is 0 with a waiter registered →
	// "fatal error: sync: WaitGroup misuse: Add called concurrently with Wait",
	// aborting the test binary. With the fix, the first DrainRun seals the run
	// and later emits skip per-run tracking, so no Add ever races a Wait.
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	runID := core.RunID(id)

	var stop int32
	var emitters sync.WaitGroup
	for e := 0; e < 4; e++ {
		emitters.Add(1)
		go func() {
			defer emitters.Done()
			for atomic.LoadInt32(&stop) == 0 {
				_ = bus.EmitWithRunID(ctx, runID, core.EventTypeRunCompleted, payload)
			}
		}()
	}

	for i := 0; i < 50000; i++ {
		_ = rd.DrainRun(ctx, runID)
	}
	atomic.StoreInt32(&stop, 1)
	emitters.Wait()
}
