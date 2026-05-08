package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// EventPayload is the marker interface every event type's payload struct
// satisfies. It is intentionally empty; payload types declare themselves as
// EventPayload by satisfying the interface.
//
// Spec ref: event-model.md §6.3 EV-032 — "Event types in Go MUST be represented
// as a tagged-union: a top-level Event envelope struct carrying common fields
// plus Payload json.RawMessage; a per-type constructor registry
// map[EventType]func() EventPayload decodes Payload keyed by Event.type."
type EventPayload interface{}

// ErrUnknownEventType is returned by DecodePayload when Event.Type has no
// registered constructor.
//
// TODO(hk-hqwn.33): EV-033 — callers that are observational consumers MUST skip
// the event; synchronous consumers MUST fail with a structured error. Not
// implemented here; the dispatch layer owns that policy.
var ErrUnknownEventType = errors.New("core: unknown event type")

// ErrDuplicateEventType is returned by RegisterEventType when the same type
// name is registered more than once.
var ErrDuplicateEventType = errors.New("core: event type already registered")

// eventRegistry holds the per-type constructor map guarded by a mutex.
//
// Registration is startup-time per EV-034 (see TODO below), but the mutex
// prevents data-race during init-order weirdness where multiple init() calls
// arrive concurrently (e.g., test binaries with parallel package-level inits).
//
// TODO(hk-hqwn.41/EV-034): startup-time sealing — registry MUST be sealed
// (writes forbidden) after the first event is emitted; that sealing logic
// belongs in the bus layer and is out of scope for this bead.
//
// TODO(hk-hqwn.41/EV-034a): source_subsystem identifier registration is a
// separate concern; see EV-034a. Not implemented here.
//
// TODO(hk-hqwn.41/EV-036): compile-time secret-prefix scan of registered
// payload types (EV-036) is a separate bead; not implemented here.
type eventRegistry struct {
	mu           sync.Mutex
	constructors map[string]func() EventPayload // TODO(hk-hqwn.59.82): hoist registry key from string to EventType when the enum lands. Non-breaking — string-constant assignment to EventType is assignable.
}

var globalEventRegistry = &eventRegistry{
	constructors: make(map[string]func() EventPayload),
}

// RegisterEventType registers a constructor for the given event type name.
// The constructor is called by DecodePayload to obtain a fresh zero-value
// target for JSON unmarshaling.
//
// Returns ErrDuplicateEventType if typeName has already been registered.
// Thread-safe.
func RegisterEventType(typeName string, constructor func() EventPayload) error {
	if typeName == "" {
		return fmt.Errorf("core: RegisterEventType: typeName must not be empty")
	}
	if constructor == nil {
		return fmt.Errorf("core: RegisterEventType: constructor must not be nil")
	}
	r := globalEventRegistry
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.constructors[typeName]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateEventType, typeName)
	}
	r.constructors[typeName] = constructor
	return nil
}

// DecodePayload looks up the constructor for e.Type, instantiates a fresh
// payload value, and JSON-unmarshals e.Payload into it.
//
// Returns:
//   - (payload, nil) on success.
//   - (nil, ErrUnknownEventType) when e.Type has no registered constructor.
//   - (nil, <unmarshal error>) when JSON decoding fails (e.g., json.SyntaxError).
//
// TODO(hk-hqwn.33/EV-033): deterministic dispatch policy (skip vs. fail on
// ErrUnknownEventType) belongs in the dispatch layer, not here.
func (e Event) DecodePayload() (EventPayload, error) {
	r := globalEventRegistry
	r.mu.Lock()
	constructor, ok := r.constructors[e.Type]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownEventType, e.Type)
	}
	payload := constructor()
	if err := json.Unmarshal(e.Payload, payload); err != nil {
		return nil, err
	}
	return payload, nil
}
