package core

import (
	"context"

	"github.com/google/uuid"
)

// Subscription is the 7-field registration record a consumer supplies to the
// EventBus at startup (event-model.md §6.1 RECORD Subscription).
//
// Subscription is declared at registration time, not inferred from runtime
// behaviour (EV-009). The bus validates the record at Subscribe() time per
// EV-014; post-seal Subscribe calls fail.
//
// # Consumer classes
//
// ConsumerClass controls how the bus dispatches events to this consumer.
// The three classes (synchronous / asynchronous / observer) are specified by
// EV-010 / EV-011 / EV-012.
//
// # Replay
//
// When Since is non-nil, the bus replays JSONL from that event_id before
// resuming live delivery (EV-014d). OffsetCheckpointEventID records the
// consumer's last durably processed event_id; consumers SHOULD persist this
// in their own store and supply it as Since on restart to close the gap
// described by EV-INV-002.
//
// # Panic policy
//
// OnPanic controls what the bus does when the consumer's goroutine panics.
// The three policies (recover_and_log / quarantine_consumer / fail_daemon)
// are specified by OQ-EV-007.
//
// # Declared emission surface (EV-010 acyclicity)
//
// DeclaredEmitTypes lists every EventType this consumer's Handler may emit
// back to the bus. The bus uses this at subscription-registration time to
// verify acyclicity across synchronous consumers: if a synchronous consumer
// subscribed to T declares it emits U, and another synchronous consumer
// subscribed to U (directly or transitively) emits T, the registration MUST
// fail-closed with a typed error (EV-010). Non-synchronous consumers are not
// subject to the acyclicity check. Nil/empty means "emits nothing".
type Subscription struct {
	// ConsumerID is the opaque identifier for this consumer, unique per bus.
	// The bus enforces uniqueness at Subscribe() time. Required (non-empty).
	ConsumerID string

	// ConsumerClass is the dispatch class for this consumer.
	// Must be one of: ConsumerClassSynchronous, ConsumerClassAsynchronous,
	// ConsumerClassObserver per EV-010 / EV-011 / EV-012.
	ConsumerClass ConsumerClass

	// EventPattern specifies which event types this consumer receives.
	// Wildcard ("*") or an explicit set of EventType strings per §6.1.
	EventPattern EventPattern

	// DeclaredEmitTypes lists every EventType this consumer's Handler MAY
	// emit back to the bus. Used by the bus at registration time to verify
	// acyclicity across synchronous consumers (EV-010 / EV-INV-003).
	// Nil or empty means "emits nothing". Only consulted for synchronous consumers.
	//
	// Spec ref: specs/event-model.md §4.2 EV-010, §5.3 EV-INV-003.
	DeclaredEmitTypes []EventType

	// Since is the optional replay offset event_id. When non-nil, the bus
	// replays JSONL events strictly after this event_id before live delivery
	// per EV-014d. Optional; nil means start from the live stream.
	Since *EventID

	// OffsetCheckpointEventID is the consumer's last durably processed
	// event_id. Consumers SHOULD persist this in their own store and supply
	// it as Since on restart to minimise the gap described by EV-INV-002.
	// Optional; nil means no checkpoint is available.
	OffsetCheckpointEventID *EventID

	// OnPanic is the policy for consumer-goroutine panics per OQ-EV-007.
	// Must be one of: OnPanicRecoverAndLog (default), OnPanicQuarantineConsumer,
	// OnPanicFailDaemon.
	OnPanic OnPanic

	// Handler is the consumer-supplied callback invoked for each matched
	// event. Required (non-nil).
	Handler func(context.Context, Event) error
}

// Valid reports whether all required fields carry valid values.
//
// Rules:
//   - ConsumerID is non-empty
//   - ConsumerClass.Valid() is true
//   - EventPattern satisfies EventPattern.Validate()
//   - Since, when non-nil, must not be EventID(uuid.Nil)
//   - OffsetCheckpointEventID, when non-nil, must not be EventID(uuid.Nil)
//   - OnPanic.Valid() is true
//   - Handler is non-nil
func (s Subscription) Valid() bool {
	if s.ConsumerID == "" {
		return false
	}
	if !s.ConsumerClass.Valid() {
		return false
	}
	if err := s.EventPattern.Validate(); err != nil {
		return false
	}
	if s.Since != nil && uuid.UUID(*s.Since) == uuid.Nil {
		return false
	}
	if s.OffsetCheckpointEventID != nil && uuid.UUID(*s.OffsetCheckpointEventID) == uuid.Nil {
		return false
	}
	if !s.OnPanic.Valid() {
		return false
	}
	if s.Handler == nil {
		return false
	}
	return true
}
