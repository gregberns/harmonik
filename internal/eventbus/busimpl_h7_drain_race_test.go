package eventbus_test

// busimpl_h7_drain_race_test.go — race test for H7: the global WaitGroup Add
// performed by every Emit* dispatch must be serialised against Drain's Wait.
// Before the fix, a Drain that observed the counter momentarily at 0 could
// return from Wait concurrently with an Emit doing wg.Add(1), which Go aborts
// with "fatal error: sync: WaitGroup misuse: Add called concurrently with
// Wait" (an unrecoverable process crash). This test hammers concurrent Emit
// against Drain; under -race and repetition it exercises that window. Success
// = the process does not abort.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

func TestBusImpl_ConcurrentEmitAndDrain_NoWaitGroupMisuse(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImpl()
	// An ASYNCHRONOUS consumer forces Emit onto the off-critical-path dispatch
	// branch that does the global wg.Add(1) — the site H7 protects.
	sub := core.Subscription{
		ConsumerID:    "h7-async",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler:       func(_ context.Context, _ core.Event) error { return nil },
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	payload, err := json.Marshal(map[string]any{"node_id": "n-h7"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var wg sync.WaitGroup
	// Many emitters racing against many drains maximises the chance of a Drain
	// observing the counter at 0 exactly as an emitter calls Add.
	const emitters = 16
	const perEmitter = 200
	const drainers = 4

	start := make(chan struct{})
	for i := 0; i < emitters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < perEmitter; j++ {
				_ = bus.Emit(context.Background(), h7EventType, payload)
			}
		}()
	}
	for i := 0; i < drainers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < perEmitter; j++ {
				_ = bus.Drain(context.Background())
			}
		}()
	}
	close(start)
	wg.Wait()

	// Reaching here without a fatal WaitGroup-misuse abort is the assertion.
}

const h7EventType core.EventType = "test.busimpl.h7.v1"
