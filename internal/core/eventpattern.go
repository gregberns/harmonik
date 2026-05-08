package core

import (
	"encoding/json"
	"errors"
	"fmt"
)

// EventPattern specifies which event types a Subscription matches (event-model.md §6.1).
//
// Exactly one mode is active:
//
//   - Wildcard mode (Wildcard == true): matches every current and future EventType.
//     Types MUST be empty in this mode.
//   - Explicit mode (Wildcard == false): matches exactly the types listed in Types.
//     Types MUST be non-empty in this mode.
//
// Invariants (enforced by Validate):
//   - Wildcard == true  → len(Types) == 0
//   - Wildcard == false → len(Types) >= 1
//
// Types uses string as the element type. The hoist to EventType is non-breaking
// once the enum defined by hk-hqwn.59.82 lands (event-model.md §8.3 EventType table).
//
// TODO(hk-hqwn.59.82): replace string elements with core.EventType once the enum is defined.
type EventPattern struct {
	// Wildcard, when true, matches every current and future EventType.
	// Types MUST be empty when Wildcard is true.
	Wildcard bool

	// Types is the explicit set of event type strings this pattern matches.
	// Empty when Wildcard is true; non-empty when Wildcard is false.
	//
	// TODO(hk-hqwn.59.82): element type is string pending core.EventType (event-model.md §8.3).
	Types map[string]struct{}
}

// Validate reports whether p satisfies the EventPattern invariants from event-model.md §6.1:
//   - Wildcard == true  → Types must be empty
//   - Wildcard == false → Types must be non-empty
func (p EventPattern) Validate() error {
	if p.Wildcard && len(p.Types) != 0 {
		return errors.New("eventpattern: Types must be empty when Wildcard is true")
	}
	if !p.Wildcard && len(p.Types) == 0 {
		return errors.New("eventpattern: Types must not be empty when Wildcard is false")
	}
	return nil
}

// MatchesType reports whether p matches the given event type string.
// It does not call Validate; callers should validate before relying on match semantics.
func (p EventPattern) MatchesType(eventType string) bool {
	if p.Wildcard {
		return true
	}
	_, ok := p.Types[eventType]
	return ok
}

// eventPatternJSON is the wire shape used for JSON marshal/unmarshal.
// Types is serialised as a sorted array for deterministic output.
type eventPatternJSON struct {
	Wildcard bool     `json:"wildcard"`
	Types    []string `json:"types"`
}

// MarshalJSON implements json.Marshaler.
// Types is serialised as a JSON array (order is not guaranteed to be stable).
func (p EventPattern) MarshalJSON() ([]byte, error) {
	types := make([]string, 0, len(p.Types))
	for t := range p.Types {
		types = append(types, t)
	}
	return json.Marshal(eventPatternJSON{
		Wildcard: p.Wildcard,
		Types:    types,
	})
}

// UnmarshalJSON implements json.Unmarshaler.
// Duplicate type strings in the array are silently deduplicated (set semantics).
func (p *EventPattern) UnmarshalJSON(data []byte) error {
	var wire eventPatternJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("eventpattern: json decode: %w", err)
	}
	types := make(map[string]struct{}, len(wire.Types))
	for _, t := range wire.Types {
		types[t] = struct{}{}
	}
	p.Wildcard = wire.Wildcard
	p.Types = types
	return nil
}
