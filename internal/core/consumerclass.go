package core

import "fmt"

// ConsumerClass is the dispatch class for a Subscription, controlling how
// the bus routes events to the consumer and how failures are handled.
// (event-model.md §6.1 ENUM consumer_class, EV-010/011/012).
//
// The enum is closed at MVH; future variants extend via the amendment
// protocol per [architecture.md §4.6].
//
// At most one synchronous consumer per event type is permitted (EV-010);
// asynchronous consumers run off the critical path with retry + dead-letter
// (EV-011); observer failures MUST NOT produce bus events or side effects
// beyond local logging (EV-012).
type ConsumerClass string

// ConsumerClass values per event-model.md §6.1 (EV-010/011/012).
const (
	// ConsumerClassSynchronous places the consumer on the producer's critical
	// path. A failure halts the producer's run and requires operator escalation
	// (EV-010). At most one synchronous consumer per event type is allowed.
	ConsumerClassSynchronous ConsumerClass = "synchronous"

	// ConsumerClassAsynchronous places the consumer off the critical path.
	// Failed deliveries are retried per a bounded policy; exhausted retries
	// enqueue to the dead-letter queue (EV-011).
	ConsumerClassAsynchronous ConsumerClass = "asynchronous"

	// ConsumerClassObserver is a passive consumer whose failures MUST NOT
	// produce bus events or mutate persistent state (EV-012).
	// The default class for in-process subscribers (EV-013).
	ConsumerClassObserver ConsumerClass = "observer"
)

// Valid reports whether c is one of the three declared ConsumerClass constants
// at MVH.
func (c ConsumerClass) Valid() bool {
	switch c {
	case ConsumerClassSynchronous, ConsumerClassAsynchronous, ConsumerClassObserver:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so ConsumerClass serialises
// correctly in JSON and YAML.
// It rejects any value that is not one of the three declared constants at MVH.
func (c ConsumerClass) MarshalText() ([]byte, error) {
	if !c.Valid() {
		return nil, fmt.Errorf("consumerclass: unknown value %q", string(c))
	}
	return []byte(c), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the three declared constants at MVH.
func (c *ConsumerClass) UnmarshalText(text []byte) error {
	v := ConsumerClass(text)
	if !v.Valid() {
		return fmt.Errorf(
			"consumerclass: unknown value %q; must be one of synchronous, asynchronous, observer",
			string(text),
		)
	}
	*c = v
	return nil
}
