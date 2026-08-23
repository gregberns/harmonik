package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

type cpBodyKey struct {
	Kind      Kind        `json:"kind"`
	Trigger   Trigger     `json:"trigger"`
	Evaluator Evaluator   `json:"evaluator"`
	Payload   KindPayload `json:"payload"`
}

type cpRegistryEntry struct {
	cp    ControlPoint
	order int // registration insertion index; monotonically increasing
}

// ErrDivergentBody is returned by MapRegistry.Register when a ControlPoint with
// the same name but a different body is already registered.
//
// specs/control-points.md §4.9.CP-044: "registering a different body under an
// existing name MUST fail at startup with a specific error code."
var ErrDivergentBody = errors.New("cpregistry: duplicate registration with divergent body (CP-044)")

// ErrInvalidControlPoint is returned by MapRegistry.Register when cp fails
// cp.Valid() (CP-001) or carries an unknown Kind.
var ErrInvalidControlPoint = errors.New("cpregistry: structurally invalid ControlPoint (CP-001)")

// ErrCognitionGuard is returned by MapRegistry.Register when a Guard
// ControlPoint with a cognition-tagged evaluator is submitted for registration.
//
// specs/control-points.md §4.4.CP-020: "A Guard MUST be mechanism-tagged; a
// cognition-tagged Guard MUST fail registration."
var ErrCognitionGuard = errors.New("cpregistry: cognition-tagged Guard forbidden (CP-020)")

// ErrCognitionBudget is returned by MapRegistry.Register when a Budget
// ControlPoint with a cognition-tagged evaluator is submitted for registration.
//
// specs/control-points.md §4.1.CP-005 boundary-classification table: Budget
// MUST be mechanism-tagged. Cognition-tagged Budget evaluators are forbidden
// because Budget enforcement (counter comparison, threshold check) is a
// deterministic operation that must not delegate to a model.
var ErrCognitionBudget = errors.New("cpregistry: cognition-tagged Budget forbidden (CP-005 boundary rule)")

// MapRegistry is the concrete in-process implementation of [Registry].
//
// It implements §6.1.7 INTERFACE Registry using a Go map keyed by name, owned
// conceptually by S02 (Policy Engine) per §4.9.CP-043. MapRegistry is not safe
// for concurrent use without external synchronisation; the daemon populates it
// at startup and treats it as read-only during the main loop, so no internal
// lock is needed.
//
// Name uniqueness (CP-002) is enforced at Register time: each name is recorded
// exactly once; a second registration under the same name requires body
// equality (CP-044).
//
// Tags: mechanism
type MapRegistry struct {
	entries   map[string]cpRegistryEntry // keyed by ControlPoint.Name
	nextOrder int                        // monotonic insertion counter
}

// NewMapRegistry constructs an empty MapRegistry ready for registration.
func NewMapRegistry() *MapRegistry {
	return &MapRegistry{
		entries: make(map[string]cpRegistryEntry),
	}
}

// Register adds cp to the registry, enforcing name uniqueness (CP-002) and
// the full registration contract of §4.9.
//
// Returns nil on success. Returns an error when:
//   - cp.Valid() is false → [ErrInvalidControlPoint]
//   - cp.Kind is KindGuard and cp.Evaluator.Mode is ModeTagCognition → [ErrCognitionGuard]
//   - cp.Kind is KindBudget and cp.Evaluator.Mode is ModeTagCognition → [ErrCognitionBudget]
//   - cp.Name is already registered with a divergent body → [ErrDivergentBody]
//
// Re-registration with an identical body (same name + same body) succeeds
// silently per CP-044.
func (r *MapRegistry) Register(cp ControlPoint) error {
	if !cp.Valid() {
		return fmt.Errorf("%w: name=%q", ErrInvalidControlPoint, cp.Name)
	}

	if !cp.Kind.AllowsCognition() && cp.Evaluator.Mode == ModeTagCognition {
		switch cp.Kind {
		case KindGuard:
			return fmt.Errorf("%w: name=%q", ErrCognitionGuard, cp.Name)
		case KindBudget:
			return fmt.Errorf("%w: name=%q", ErrCognitionBudget, cp.Name)
		}
	}

	existing, exists := r.entries[cp.Name]
	if exists {
		existingHash, err := cpCanonicalBody(existing.cp)
		if err != nil {
			return fmt.Errorf("cpregistry: body serialisation of existing entry %q: %w", cp.Name, err)
		}
		incomingHash, err := cpCanonicalBody(cp)
		if err != nil {
			return fmt.Errorf("cpregistry: body serialisation of incoming entry %q: %w", cp.Name, err)
		}
		if string(existingHash) == string(incomingHash) {
			return nil
		}
		return fmt.Errorf("%w: name=%q", ErrDivergentBody, cp.Name)
	}

	cp.DeclarationIndex = r.nextOrder
	r.entries[cp.Name] = cpRegistryEntry{cp: cp, order: r.nextOrder}
	r.nextOrder++
	return nil
}

// LookupByName returns the ControlPoint registered under name and true, or a
// zero ControlPoint and false when name is not registered.
//
// Deterministic per CP-046: identical registry state and name always produce
// the same result.
func (r *MapRegistry) LookupByName(name string) (ControlPoint, bool) {
	entry, ok := r.entries[name]
	if !ok {
		return ControlPoint{}, false
	}
	return entry.cp, true
}

// LookupByTrigger returns all Hooks and Gates whose trigger name matches
// trigger, sorted by Name ascending (CP-046).
//
// Returns an empty (non-nil) slice when no ControlPoints match.
func (r *MapRegistry) LookupByTrigger(trigger string) []ControlPoint {
	var out []ControlPoint
	for _, entry := range r.entries {
		cp := entry.cp
		if (cp.Kind == KindGate || cp.Kind == KindHook) && cp.Trigger.Name == trigger {
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if out == nil {
		return []ControlPoint{}
	}
	return out
}

// LookupByAttachPoint returns all Gates registered at attachPoint, sorted by
// declaration order (registration order) per §4.1.CP-007.
//
// Returns an empty (non-nil) slice when no Gates are registered at attachPoint.
func (r *MapRegistry) LookupByAttachPoint(attachPoint AttachPoint) []ControlPoint {
	var entries []cpRegistryEntry
	for _, entry := range r.entries {
		if entry.cp.Kind == KindGate {
			payload := entry.cp.Payload.Gate
			if payload != nil && payload.AttachPoint == attachPoint {
				entries = append(entries, entry)
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].order < entries[j].order })
	out := make([]ControlPoint, len(entries))
	for i, e := range entries {
		out[i] = e.cp
	}
	return out
}

// All returns every registered ControlPoint sorted by Name ascending (CP-046).
//
// Returns an empty (non-nil) slice when the registry is empty.
func (r *MapRegistry) All() []ControlPoint {
	out := make([]ControlPoint, 0, len(r.entries))
	for _, entry := range r.entries {
		out = append(out, entry.cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cpCanonicalBody(cp ControlPoint) ([]byte, error) {
	key := cpBodyKey{
		Kind:      cp.Kind,
		Trigger:   cp.Trigger,
		Evaluator: cp.Evaluator,
		Payload:   cp.Payload,
	}
	return json.Marshal(key)
}
