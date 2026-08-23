package codexinput

import "github.com/gregberns/harmonik/internal/substrate"

// FakeEffector records every Action it receives; safe for concurrent use.
type FakeEffector = substrate.FakeEffector[Action]

// SyntheticSource delivers a fixed []Event slice from a pre-closed channel.
type SyntheticSource = substrate.SyntheticSource[Event]

// NewSyntheticSource constructs a SyntheticSource over events.
func NewSyntheticSource(events []Event) *SyntheticSource {
	return substrate.NewSyntheticSource(events)
}
