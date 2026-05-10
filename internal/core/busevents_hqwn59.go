package core

import "github.com/google/uuid"

// busevents_hqwn59.go — event-bus payload types for §8.8 observability and
// bus-internal events:
//   - metric               (§8.8.1)
//   - dead_letter_enqueued (§8.8.3)
//   - bus_overflow         (§8.8.4)
//
// NOTE: consumer_failed (§8.8.2) is blocked on hk-hqwn.14 (Asynchronous consumer
// class). redaction_failed (§8.8.5) is blocked on hk-hqwn.45 (Redaction registry).
// Both are declared in separate beads (hk-hqwn.59.75, hk-hqwn.59.78) and will be
// implemented once their blocker beads are closed.
//
// Spec ref: specs/event-model.md §8.8, §6.3.
// Bead refs: hk-hqwn.59.74, hk-hqwn.59.76, hk-hqwn.59.77.

// ---------------------------------------------------------------------------
// Enum types for §8.8 payload discriminators
// ---------------------------------------------------------------------------

// BusOverflowShedPolicy is the typed discriminator for the `shed_policy` field
// of the bus_overflow event (event-model.md §8.8.4; EV-011a).
//
// The shed policy tells consumers why the event was shed without requiring
// cross-reference to §8 for the event's durability class.
//
// TODO(hk-hqwn.72): hoist to a typed ShedPolicy alias in a dedicated type file
// when that type lands; currently inline here per the typed-alias-deferral
// pattern.
type BusOverflowShedPolicy string

const (
	// BusOverflowShedPolicyFsyncSpilled indicates a fsync-boundary (F-class)
	// event could not queue to the consumer; it was redirected to the spill file
	// at .harmonik/events/spill-<consumer>.jsonl per EV-011a.
	// Overflow handlers seeing this value SHOULD check the spill file for
	// reconciliation.
	BusOverflowShedPolicyFsyncSpilled BusOverflowShedPolicy = "fsync-spilled"

	// BusOverflowShedPolicyOrdinaryDropped indicates an ordinary (O-class) event
	// could not queue; the event was shed (dropped). Loss is accepted per
	// EV-017 / EV-INV-002.
	BusOverflowShedPolicyOrdinaryDropped BusOverflowShedPolicy = "ordinary-dropped"

	// BusOverflowShedPolicyLossyDropped indicates a lossy-tail-ok (L-class) event
	// could not queue to an observer; the event was shed (dropped). Loss is
	// accepted per EV-017 / EV-INV-002.
	BusOverflowShedPolicyLossyDropped BusOverflowShedPolicy = "lossy-dropped"
)

// Valid reports whether p is one of the three declared BusOverflowShedPolicy constants.
func (p BusOverflowShedPolicy) Valid() bool {
	switch p {
	case BusOverflowShedPolicyFsyncSpilled,
		BusOverflowShedPolicyOrdinaryDropped,
		BusOverflowShedPolicyLossyDropped:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Payload structs for §8.8 events
// ---------------------------------------------------------------------------

// MetricPayload is the typed event payload for the metric event
// (event-model.md §8.8.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: L (lossy-tail-ok — per §8.8.1; metrics are observability
// signals that tolerate loss under back-pressure per EV-014c).
//
// The metric event is the §8.9(g) escape-hatch exception: its use is free
// (no sibling-spec emission citation required) but payload-shape-bounded by
// this struct. Any subsystem may emit metric events.
//
// # Payload fields (event-model.md §8.8.1)
//
//   - metric_name — the metric identifier string
//   - value       — the metric value (float64)
//   - unit        — optional unit string (e.g., "ms", "bytes", "count")
//   - labels      — optional key-value label map for dimensionality
type MetricPayload struct {
	// MetricName is the metric identifier string. Required (non-empty).
	//
	// TODO(hk-hqwn.72): hoist to typed MetricName alias when that type lands.
	MetricName string `json:"metric_name"`

	// Value is the metric value. Required (any finite float64; NaN and Inf are
	// not valid metric values).
	Value float64 `json:"value"`

	// Unit is the optional unit string for the metric value. Corresponds to
	// unit? in event-model.md §8.8.1. Nil when no unit is declared.
	//
	// TODO(hk-hqwn.72): hoist to typed MetricUnit alias when that type lands.
	Unit *string `json:"unit,omitempty"`

	// Labels is the optional key-value label map for dimensionality. Corresponds
	// to labels? in event-model.md §8.8.1. Nil when no labels are provided.
	//
	// TODO(hk-hqwn.72): hoist to typed MetricLabels alias when that type lands.
	Labels map[string]string `json:"labels,omitempty"`
}

// Valid reports whether p is a well-formed MetricPayload.
//
// Rules per event-model.md §8.8.1:
//   - MetricName must be non-empty.
func (p MetricPayload) Valid() bool {
	return p.MetricName != ""
}

// DeadLetterEnqueuedPayload is the typed event payload for the
// dead_letter_enqueued event (event-model.md §8.8.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit; dead-letter signals
// are important but do not require fsync-backed durability).
//
// Emitted by the bus (bus-internal) when an asynchronous consumer's retry
// policy is exhausted and the event is moved to the dead-letter queue per
// event-model.md §6.1 EV-011 / §6.2. The companion to this event is the
// dead-letter queue entry itself (per-consumer JSONL at .harmonik/events/).
//
// # Payload fields (event-model.md §8.8.3)
//
//   - consumer_name       — the name of the consumer that failed to process the event
//   - event_type          — the §8 event type string of the dead-lettered event
//   - original_event_id   — the EventID of the dead-lettered event
//   - retries_attempted   — number of delivery retries attempted before dead-lettering
//   - enqueued_at         — RFC 3339 wall-clock timestamp at dead-letter enqueue
type DeadLetterEnqueuedPayload struct {
	// ConsumerName is the name of the consumer that exhausted its retry policy.
	// Required (non-empty).
	ConsumerName string `json:"consumer_name"`

	// EventType is the §8 event type string of the dead-lettered event.
	// Required (non-empty).
	EventType EventType `json:"event_type"`

	// OriginalEventID is the EventID of the dead-lettered event.
	// Required (must not be uuid.Nil).
	OriginalEventID EventID `json:"original_event_id"`

	// RetriesAttempted is the number of delivery retries attempted before the
	// event was moved to the dead-letter queue. Required (must be >= 0).
	RetriesAttempted int `json:"retries_attempted"`

	// EnqueuedAt is the RFC 3339 wall-clock timestamp at dead-letter enqueue.
	// Required (non-empty).
	EnqueuedAt string `json:"enqueued_at"`
}

// Valid reports whether p is a well-formed DeadLetterEnqueuedPayload.
//
// Rules per event-model.md §8.8.3:
//   - ConsumerName must be non-empty.
//   - EventType must be non-empty (Valid EventType).
//   - OriginalEventID must not be uuid.Nil.
//   - RetriesAttempted must be >= 0.
//   - EnqueuedAt must be non-empty.
func (p DeadLetterEnqueuedPayload) Valid() bool {
	if p.ConsumerName == "" {
		return false
	}
	if !p.EventType.Valid() {
		return false
	}
	if uuid.UUID(p.OriginalEventID) == uuid.Nil {
		return false
	}
	if p.RetriesAttempted < 0 {
		return false
	}
	if p.EnqueuedAt == "" {
		return false
	}
	return true
}

// BusOverflowPayload is the typed event payload for the bus_overflow event
// (event-model.md §8.8.4; §6.3 bus_overflow block; EV-011a).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit; promoted to F via
// direct-JSONL-append fallback when the observer reservation slot is exhausted,
// per EV-011a).
//
// Emitted by the bus (bus-internal) when a per-consumer queue is full and an
// event is shed or spilled per EV-011a. The bus MUST reserve a capacity-1 slot
// in every observer queue for bus_overflow to avoid recursive fill checks.
//
// When even the reservation slot is exhausted, the bus falls back to direct
// JSONL append with fsync-boundary semantics (promoted from O to F at write
// time); the promotion MUST be recorded in the structured-log channel.
//
// # Payload fields (event-model.md §8.8.4; §6.3)
//
//   - consumer_name — the name of the consumer whose queue was full
//   - event_type    — the §8 event type of the shed/spilled event
//   - event_id      — the EventID of the shed/spilled event
//   - queue_depth   — the consumer's queue depth at the time of overflow
//   - shed_at       — RFC 3339 wall-clock timestamp at the shed/spill
//   - shed_policy   — how the event was handled (fsync-spilled / ordinary-dropped / lossy-dropped)
type BusOverflowPayload struct {
	// ConsumerName is the name of the consumer whose queue was full.
	// Required (non-empty).
	ConsumerName string `json:"consumer_name"`

	// EventType is the §8 event type string of the shed/spilled event.
	// Required (non-empty).
	EventType EventType `json:"event_type"`

	// EventID is the EventID of the shed/spilled event.
	// Required (must not be uuid.Nil).
	EventID EventID `json:"event_id"`

	// QueueDepth is the consumer's queue depth at the time of overflow.
	// Required (must be >= 0).
	QueueDepth int `json:"queue_depth"`

	// ShedAt is the RFC 3339 wall-clock timestamp at the shed/spill.
	// Required (non-empty).
	ShedAt string `json:"shed_at"`

	// ShedPolicy describes how the event was handled. Required; must be a valid
	// BusOverflowShedPolicy constant. This field lets consumers attribute the shed
	// without cross-referencing §8 for the event's durability class.
	ShedPolicy BusOverflowShedPolicy `json:"shed_policy"`
}

// Valid reports whether p is a well-formed BusOverflowPayload.
//
// Rules per event-model.md §8.8.4 and §6.3:
//   - ConsumerName must be non-empty.
//   - EventType must be non-empty (Valid EventType).
//   - EventID must not be uuid.Nil.
//   - QueueDepth must be >= 0.
//   - ShedAt must be non-empty.
//   - ShedPolicy must be a valid BusOverflowShedPolicy constant.
func (p BusOverflowPayload) Valid() bool {
	if p.ConsumerName == "" {
		return false
	}
	if !p.EventType.Valid() {
		return false
	}
	if uuid.UUID(p.EventID) == uuid.Nil {
		return false
	}
	if p.QueueDepth < 0 {
		return false
	}
	if p.ShedAt == "" {
		return false
	}
	if !p.ShedPolicy.Valid() {
		return false
	}
	return true
}
