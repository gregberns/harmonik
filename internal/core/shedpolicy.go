package core

// ShedPolicy is the typed discriminator for the `shed_policy` field of the
// bus_overflow event (event-model.md §8.8.4; §6.3 bus_overflow block; EV-011a).
//
// ShedPolicy tells consumers how the bus handled a queue-full condition for a
// given event, without requiring them to cross-reference §8 for the event's
// durability class.
//
// Spec ref: event-model.md §8.8.4, §6.3.
// Bead ref: hk-hqwn.72.
type ShedPolicy string

// BusOverflowShedPolicy is a type alias for ShedPolicy retained for internal
// compatibility within busevents_hqwn59.go. New code should use ShedPolicy directly.
//
// Deprecated: use ShedPolicy.
type BusOverflowShedPolicy = ShedPolicy

const (
	// ShedPolicyFsyncSpilled indicates a fsync-boundary (F-class) event could not
	// queue to the consumer; it was redirected to the spill file at
	// .harmonik/events/spill-<consumer>.jsonl per EV-011a. Overflow handlers
	// seeing this value SHOULD check the spill file for reconciliation.
	ShedPolicyFsyncSpilled ShedPolicy = "fsync-spilled"

	// ShedPolicyOrdinaryDropped indicates an ordinary (O-class) event could not
	// queue; the event was shed (dropped). Loss is accepted per EV-017 / EV-INV-002.
	ShedPolicyOrdinaryDropped ShedPolicy = "ordinary-dropped"

	// ShedPolicyLossyDropped indicates a lossy-tail-ok (L-class) event could not
	// queue to an observer; the event was shed (dropped). Loss is accepted per
	// EV-017 / EV-INV-002.
	ShedPolicyLossyDropped ShedPolicy = "lossy-dropped"
)

// Valid reports whether p is one of the three declared ShedPolicy constants.
func (p ShedPolicy) Valid() bool {
	switch p {
	case ShedPolicyFsyncSpilled, ShedPolicyOrdinaryDropped, ShedPolicyLossyDropped:
		return true
	default:
		return false
	}
}
