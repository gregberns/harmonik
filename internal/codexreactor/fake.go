package codexreactor

import "github.com/gregberns/harmonik/internal/substrate"

// FakeEffector and SyntheticSource are the codex instantiations of the generic
// substrate test doubles (substrate/doubles.go). They are type ALIASES (=), not
// defined types, so composite literals (&codexreactor.FakeEffector{}) and
// reflect.DeepEqual over []codexreactor.Action at existing call sites keep
// compiling unchanged — an alias of an instantiated generic type is a plain
// named type resolving to the concrete codex element types (RS-021;
// substrate-design §2.2).

// FakeEffector is a test-only Effector that records every Action it receives.
// It is safe for concurrent use (Execute may be called from the reactor loop
// goroutine while Actions is read from the test goroutine).
type FakeEffector = substrate.FakeEffector[Action]

// SyntheticSource is a test-only EventSource that delivers a fixed []Event
// slice. Events returns a pre-filled, immediately closed channel — callers
// drain it synchronously without blocking.
type SyntheticSource = substrate.SyntheticSource[Event]

// NewSyntheticSource constructs a SyntheticSource that will deliver events.
func NewSyntheticSource(events []Event) *SyntheticSource {
	return substrate.NewSyntheticSource(events)
}
