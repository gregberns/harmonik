package codexinput

import "github.com/gregberns/harmonik/internal/substrate"

// FakeEffector and SyntheticSource are the codexinput instantiations of the
// generic substrate test doubles. They are type ALIASES (=), not defined types,
// so composite literals and reflect.DeepEqual over []codexinput.Action at call
// sites keep compiling (RS-021; substrate-design §2.2).

// FakeEffector records every Action it receives; safe for concurrent use.
type FakeEffector = substrate.FakeEffector[Action]

// SyntheticSource delivers a fixed []Event slice from a pre-closed channel.
type SyntheticSource = substrate.SyntheticSource[Event]

// NewSyntheticSource constructs a SyntheticSource over events.
func NewSyntheticSource(events []Event) *SyntheticSource {
	return substrate.NewSyntheticSource(events)
}
