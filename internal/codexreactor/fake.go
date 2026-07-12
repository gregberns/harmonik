package codexreactor

import (
	"context"
	"sync"
)

// ─── FakeEffector ────────────────────────────────────────────────────────────

// FakeEffector is a test-only Effector that records every Action it receives.
// It is safe for concurrent use (Execute may be called from the reactor loop
// goroutine while Actions is read from the test goroutine).
type FakeEffector struct {
	mu      sync.Mutex
	actions []Action
}

// Execute records a into the effector's action log.
func (f *FakeEffector) Execute(_ context.Context, a Action) error {
	f.mu.Lock()
	f.actions = append(f.actions, a)
	f.mu.Unlock()
	return nil
}

// Actions returns a copy of every Action recorded so far, in order.
func (f *FakeEffector) Actions() []Action {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Action, len(f.actions))
	copy(out, f.actions)
	return out
}

// Reset clears the recorded action log.
func (f *FakeEffector) Reset() {
	f.mu.Lock()
	f.actions = f.actions[:0]
	f.mu.Unlock()
}

// ─── SyntheticSource ─────────────────────────────────────────────────────────

// SyntheticSource is a test-only EventSource that delivers a fixed []Event
// slice. Events returns a pre-filled, immediately closed channel — callers
// drain it synchronously without blocking.
type SyntheticSource struct {
	events []Event
}

// NewSyntheticSource constructs a SyntheticSource that will deliver events.
func NewSyntheticSource(events []Event) *SyntheticSource {
	return &SyntheticSource{events: events}
}

// Events returns a buffered channel loaded with the configured events and
// immediately closed. ctx cancellation is respected: if ctx is already done
// when Events is called, the returned channel is empty and closed.
func (s *SyntheticSource) Events(ctx context.Context) <-chan Event {
	ch := make(chan Event, len(s.events))
	if ctx.Err() == nil {
		for _, ev := range s.events {
			ch <- ev
		}
	}
	close(ch)
	return ch
}
