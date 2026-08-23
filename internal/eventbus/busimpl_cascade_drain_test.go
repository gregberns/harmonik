package eventbus_test

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
		Handler: func(ctx context.Context, evt core.Event) error {
			switch evt.Type {
			case cascadeParentType:
				cascadeOnce.Do(func() {
					<-drainStarted
					_ = bus.Emit(ctx, cascadeChildType, cascadePayload(t)) //nolint:errcheck // re-entrant test emit inside sync.Once; delivery is asserted downstream
				})
				mu.Lock()
				delivered[cascadeParentType]++
				mu.Unlock()
			case cascadeChildType:
				time.Sleep(20 * time.Millisecond)
				mu.Lock()
				delivered[cascadeChildType]++
				mu.Unlock()
			default:
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

	if err := bus.Emit(context.Background(), cascadeParentType, cascadePayload(t)); err != nil {
		t.Fatalf("Emit parent: %v", err)
	}

	drainReturned := make(chan error, 1)
	go func() { drainReturned <- bus.Drain(context.Background()) }()

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
