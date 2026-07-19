package substrate

import (
	"context"
	"sync"
)

// ─── FakeEffector ────────────────────────────────────────────────────────────

// FakeEffector is a generic recorder Effector that records every action it
// receives (RS-006). It is safe for concurrent use (Execute may be called from
// the driver-loop goroutine while Actions is read from the test goroutine). It
// owns no assertion logic — it is the graded artifact, not the grader (RS-020).
type FakeEffector[A any] struct {
	mu      sync.Mutex
	actions []A
}

// Execute records a into the effector's action log.
func (f *FakeEffector[A]) Execute(_ context.Context, a A) error {
	f.mu.Lock()
	f.actions = append(f.actions, a)
	f.mu.Unlock()
	return nil
}

// Actions returns a copy of every action recorded so far, in order.
func (f *FakeEffector[A]) Actions() []A {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]A, len(f.actions))
	copy(out, f.actions)
	return out
}

// Reset clears the recorded action log.
func (f *FakeEffector[A]) Reset() {
	f.mu.Lock()
	f.actions = nil
	f.mu.Unlock()
}

// ─── SyntheticSource ─────────────────────────────────────────────────────────

// SyntheticSource is a generic fixed-slice EventSource that delivers a fixed
// []E slice (RS-007). Events returns a pre-filled, immediately closed channel —
// callers drain it synchronously without blocking.
type SyntheticSource[E any] struct {
	events []E
}

// NewSyntheticSource constructs a SyntheticSource that will deliver events.
func NewSyntheticSource[E any](events []E) *SyntheticSource[E] {
	return &SyntheticSource[E]{events: events}
}

// Events returns a buffered channel loaded with the configured events and
// immediately closed. ctx cancellation is respected: if ctx is already done
// when Events is called, the returned channel is empty and closed.
func (s *SyntheticSource[E]) Events(ctx context.Context) <-chan E {
	ch := make(chan E, len(s.events))
	if ctx.Err() == nil {
		for _, ev := range s.events {
			ch <- ev
		}
	}
	close(ch)
	return ch
}
