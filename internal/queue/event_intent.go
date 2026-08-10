package queue

import (
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// EventIntent is a queue-owned request to emit one event after the queue state
// that produced it is durable. The event bus owns envelope identity and time.
type EventIntent struct {
	Type    core.EventType
	Payload json.RawMessage
}

// NewEventIntent converts a typed payload to detached JSON bytes. It performs
// no I/O and creates no event envelope identity or timestamp.
func NewEventIntent(eventType core.EventType, payload core.EventPayload) (EventIntent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return EventIntent{}, fmt.Errorf("queue: NewEventIntent: marshal payload for %q: %w", eventType, err)
	}
	return EventIntent{Type: eventType, Payload: raw}, nil
}
