package runloop

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

type ownedSubscriptionNoopEmitter struct{}

func (ownedSubscriptionNoopEmitter) Emit(context.Context, core.EventType, []byte) error {
	return nil
}

func (ownedSubscriptionNoopEmitter) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return nil
}

func ownedSubscriptionRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

func newOwnedSubscriptionTap(t *testing.T) *PerRunEventTap {
	t.Helper()
	return &PerRunEventTap{
		underlying: ownedSubscriptionNoopEmitter{},
		runID:      ownedSubscriptionRunID(t),
	}
}

func TestPerRunEventTap_SubscribeOwnedLifecycle(t *testing.T) {
	t.Parallel()

	tap := newOwnedSubscriptionTap(t)
	sub := tap.SubscribeOwned()

	tap.mu.Lock()
	registered := len(tap.subs)
	tap.mu.Unlock()
	if registered != 1 {
		t.Fatalf("registered subscriptions = %d, want 1", registered)
	}
	select {
	case <-sub.Done():
		t.Fatal("Done closed before Unsubscribe")
	default:
	}

	if err := tap.Emit(context.Background(), core.EventTypeAgentHeartbeat, nil); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	select {
	case event := <-sub.Events():
		if event.Type != string(core.EventTypeAgentHeartbeat) {
			t.Fatalf("event type = %q, want %q", event.Type, core.EventTypeAgentHeartbeat)
		}
	case <-time.After(time.Second):
		t.Fatal("owned subscription did not receive event")
	}

	if err := sub.Unsubscribe(context.Background()); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	select {
	case <-sub.Done():
	default:
		t.Fatal("Done remained open after Unsubscribe returned")
	}

	tap.mu.Lock()
	registered = len(tap.subs)
	tap.mu.Unlock()
	if registered != 0 {
		t.Fatalf("registered subscriptions after Unsubscribe = %d, want 0", registered)
	}
	if sub.tap != nil {
		t.Fatal("unsubscribed handle retained its tap")
	}

	select {
	case _, ok := <-sub.Events():
		if ok {
			t.Fatal("Events remained open after Unsubscribe returned")
		}
	default:
		t.Fatal("Events was not closed synchronously by Unsubscribe")
	}

	// Unsubscribe is idempotent, including with an already-cancelled teardown
	// context. Cleanup must not be skipped merely because the owner is stopping.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sub.Unsubscribe(cancelled); err != nil {
		t.Fatalf("idempotent Unsubscribe: %v", err)
	}
}

func TestPerRunEventTap_NoDeliveryAfterUnsubscribeReturns(t *testing.T) {
	t.Parallel()

	tap := newOwnedSubscriptionTap(t)
	sub := tap.SubscribeOwned()
	if err := sub.Unsubscribe(context.Background()); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	for i := 0; i < 10; i++ {
		if err := tap.Emit(context.Background(), core.EventTypeAgentReady, nil); err != nil {
			t.Fatalf("Emit after Unsubscribe: %v", err)
		}
	}

	select {
	case _, ok := <-sub.Events():
		if ok {
			t.Fatal("received an event emitted after Unsubscribe returned")
		}
	case <-time.After(time.Second):
		t.Fatal("Events did not close after Unsubscribe")
	}
}

func TestPerRunEventTap_UnsubscribeRacesEmit(t *testing.T) {
	t.Parallel()

	tap := newOwnedSubscriptionTap(t)
	sub := tap.SubscribeOwned()
	start := make(chan struct{})

	const (
		emitterCount      = 8
		emitsPerEmitter   = 1_000
		unsubscriberCount = 8
	)

	var emitWG sync.WaitGroup
	emitWG.Add(emitterCount)
	for range emitterCount {
		go func() {
			defer emitWG.Done()
			<-start
			for range emitsPerEmitter {
				if err := tap.Emit(context.Background(), core.EventTypeAgentHeartbeat, nil); err != nil {
					t.Errorf("Emit: %v", err)
					return
				}
			}
		}()
	}

	var unsubscribeWG sync.WaitGroup
	unsubscribeWG.Add(unsubscriberCount)
	for range unsubscriberCount {
		go func() {
			defer unsubscribeWG.Done()
			<-start
			if err := sub.Unsubscribe(context.Background()); err != nil {
				t.Errorf("Unsubscribe: %v", err)
			}
		}()
	}

	close(start)
	unsubscribeWG.Wait()

	// This event is emitted strictly after every concurrent Unsubscribe call
	// returned. It must never appear in the closed subscription's buffer.
	if err := tap.Emit(context.Background(), core.EventTypeAgentReady, nil); err != nil {
		t.Fatalf("post-Unsubscribe Emit: %v", err)
	}
	emitWG.Wait()

	for event := range sub.Events() {
		if event.Type == string(core.EventTypeAgentReady) {
			t.Fatal("received event emitted after Unsubscribe returned")
		}
	}

	tap.mu.Lock()
	registered := len(tap.subs)
	tap.mu.Unlock()
	if registered != 0 {
		t.Fatalf("registered subscriptions after concurrent Unsubscribe = %d, want 0", registered)
	}
}

func TestPerRunEventTap_SubscribeCompatibilityWrapper(t *testing.T) {
	t.Parallel()

	tap := newOwnedSubscriptionTap(t)
	events := tap.Subscribe()

	if err := tap.Emit(context.Background(), core.EventTypeAgentHeartbeat, nil); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	select {
	case event := <-events:
		if event.Type != string(core.EventTypeAgentHeartbeat) {
			t.Fatalf("event type = %q, want %q", event.Type, core.EventTypeAgentHeartbeat)
		}
	case <-time.After(time.Second):
		t.Fatal("compatibility subscription did not receive event")
	}
}
