package eventbus_test

// busimpl_cascade_drain_test.go — own-tier regression guard for hk-okzy1.
//
// A re-entrant Emit issued from within an observer handler *while Drain is in
// flight* (a cascade) must be tracked by the global in-flight counter and
// delivered to observers before Drain returns. Before the fix (a regression
// from 18a2d221 / H7), Drain set a permanent seal on the eventbus; once sealed,
// addGlobalDrainer stopped tracking new dispatch goroutines, so a re-entrant
// emit-delivery goroutine ran UNTRACKED and Drain returned before the cascade
// tail was delivered. Wildcard observers then deterministically missed it —
// observed downstream as hooksystem CP-016/017/042 ("hook_fired not emitted"),
// where hook_fired is emitted re-entrantly from inside the agent_started handler
// during Drain. The fix (86ff7565) replaced the WaitGroup+seal with an inflight
// counter + sync.Cond so mid-Drain cascade emits bump the counter and are waited
// on. This test exercises that exact window directly at the eventbus tier,
// independent of hooksystem.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

const (
	cascadeParentType core.EventType = "test.busimpl.cascade.parent.v1"
	cascadeChildType  core.EventType = "test.busimpl.cascade.child.v1"
)

func TestBusImpl_ReentrantEmitDuringDrain_IsWaitedAndDelivered(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImpl()

	var mu sync.Mutex
	delivered := map[core.EventType]int{}

	drainStarted := make(chan struct{}) // closed by the test once Drain is in flight
	var cascadeOnce sync.Once

	sub := core.Subscription{
		ConsumerID:    "cascade-observer",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			switch core.EventType(evt.Type) {
			case cascadeParentType:
				// Re-entrant cascade: emit the child only once Drain is waiting,
				// so the child's delivery goroutine is registered mid-Drain —
				// the precise window the seal regression left untracked.
				cascadeOnce.Do(func() {
					<-drainStarted
					_ = bus.Emit(context.Background(), cascadeChildType, cascadePayload(t))
				})
				mu.Lock()
				delivered[cascadeParentType]++
				mu.Unlock()
			case cascadeChildType:
				// A short delay so a broken (seal) Drain — which does not track
				// this goroutine — returns BEFORE this record lands, making the
				// regression deterministically observable rather than racy.
				time.Sleep(20 * time.Millisecond)
				mu.Lock()
				delivered[cascadeChildType]++
				mu.Unlock()
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

	// Emit the parent: this launches its observer goroutine (inflight = 1). The
	// observer blocks in cascadeOnce on drainStarted before emitting the child.
	if err := bus.Emit(context.Background(), cascadeParentType, cascadePayload(t)); err != nil {
		t.Fatalf("Emit parent: %v", err)
	}

	drainReturned := make(chan error, 1)
	go func() { drainReturned <- bus.Drain(context.Background()) }()

	// Give Drain a beat to enter its wait, THEN release the cascade so the child
	// is emitted strictly after Drain has begun draining.
	time.Sleep(50 * time.Millisecond)
	close(drainStarted)

	select {
	case err := <-drainReturned:
		if err != nil {
			t.Fatalf("Drain: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Drain did not return within 5s")
	}

	// After Drain returns at true quiescence, BOTH the parent and the re-entrant
	// child must have been delivered to the observer (hk-okzy1).
	mu.Lock()
	defer mu.Unlock()
	if got := delivered[cascadeParentType]; got != 1 {
		t.Errorf("parent delivered %d times, want 1", got)
	}
	if got := delivered[cascadeChildType]; got != 1 {
		t.Errorf("re-entrant child delivered %d times, want 1 — Drain returned "+
			"before the mid-Drain cascade was flushed (hk-okzy1 seal regression)", got)
	}
}

func cascadePayload(t *testing.T) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"node_id": "n-cascade"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return payload
}
