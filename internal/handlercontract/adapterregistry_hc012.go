package handlercontract

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// adapterRegistry — per-bead helper prefix for test helpers in
// adapterregistry_hc012_test.go (implementer-protocol.md §Helper-prefix
// discipline; bead hk-8i31.14).

// AdapterRegistry enforces the HC-012 invariant: the Agent Runner (S04) exposes
// exactly ONE Adapter per registered agent_type.
//
// S04 MUST register every agent_type it supports before the daemon begins
// dispatching sessions. Duplicate registrations and lookups of unregistered
// agent types both surface as errors; neither silently succeeds.
//
// # No per-session state in the registry
//
// The registry stores Adapter instances, not session state. Per HC-012, per-
// session state MUST live entirely inside the watcher's stack or closure
// (§4.3.HC-011); adapters MUST NOT hold per-session state or spawn per-session
// goroutines.
//
// # Sealed after first lookup
//
// The registry is sealed implicitly on the first ForAgent call: the daemon
// begins dispatching when it starts looking up adapters, so any registration
// after the first lookup would be a race condition. Sealed state is stored in
// a boolean; further Register calls after sealing return an error.
//
// # Zero value not usable
//
// Callers MUST use NewAdapterRegistry; the zero-value registry panics on
// every method call.
//
// Spec: specs/handler-contract.md §4.3.HC-012.
type AdapterRegistry struct {
	sealed   bool
	adapters map[core.AgentType]Adapter
}

// NewAdapterRegistry creates an empty, unsealed AdapterRegistry.
//
// Spec: specs/handler-contract.md §4.3.HC-012.
func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{
		adapters: make(map[core.AgentType]Adapter),
	}
}

// Register adds adapter for agentType to the registry.
//
// Returns an error if:
//   - agentType is not a valid AgentType (does not match AR-027 regex)
//   - adapter is nil
//   - agentType is already registered (duplicate — HC-012 requires exactly one adapter per type)
//   - the registry has been sealed (ForAgent was already called)
//
// Callers MUST register all agent types before the first ForAgent call.
//
// Spec: specs/handler-contract.md §4.3.HC-012.
func (r *AdapterRegistry) Register(agentType core.AgentType, adapter Adapter) error {
	if r.adapters == nil {
		panic("handlercontract: AdapterRegistry.Register called on zero-value registry; use NewAdapterRegistry")
	}
	if r.sealed {
		return fmt.Errorf(
			"handlercontract: AdapterRegistry.Register(%q): registry is sealed (ForAgent was already called); "+
				"all agent types must be registered before dispatching begins — daemon defect",
			agentType,
		)
	}
	if !agentType.Valid() {
		return fmt.Errorf(
			"handlercontract: AdapterRegistry.Register: invalid agent_type %q; "+
				"must match ^[a-z][a-z0-9-]{1,62}$ (AR-027)",
			agentType,
		)
	}
	if adapter == nil {
		return fmt.Errorf(
			"handlercontract: AdapterRegistry.Register(%q): adapter is nil — daemon defect",
			agentType,
		)
	}
	if _, ok := r.adapters[agentType]; ok {
		return fmt.Errorf(
			"handlercontract: AdapterRegistry.Register(%q): duplicate registration; "+
				"HC-012 requires exactly one Adapter per agent_type",
			agentType,
		)
	}
	r.adapters[agentType] = adapter
	return nil
}

// ForAgent returns the Adapter registered for agentType and seals the registry.
//
// Returns (adapter, nil) on success. Returns (nil, error) if agentType has no
// registered adapter (unregistered agent type — the daemon assembled a session
// for an unknown type, which is a daemon defect).
//
// The first ForAgent call seals the registry: subsequent Register calls fail.
//
// Spec: specs/handler-contract.md §4.3.HC-012.
func (r *AdapterRegistry) ForAgent(agentType core.AgentType) (Adapter, error) {
	if r.adapters == nil {
		panic("handlercontract: AdapterRegistry.ForAgent called on zero-value registry; use NewAdapterRegistry")
	}
	// Seal on first lookup — dispatching has begun; no further registrations are allowed.
	r.sealed = true
	adapter, ok := r.adapters[agentType]
	if !ok {
		return nil, fmt.Errorf(
			"handlercontract: AdapterRegistry.ForAgent(%q): no adapter registered for this agent_type; "+
				"S04 must Register all agent types at startup before dispatching (HC-012)",
			agentType,
		)
	}
	return adapter, nil
}

// RegisteredTypes returns the set of agent types that have a registered adapter.
//
// The returned slice is a snapshot; mutations to the registry after this call
// are not reflected in the returned value.
// The order is unspecified.
//
// Spec: specs/handler-contract.md §4.3.HC-012.
func (r *AdapterRegistry) RegisteredTypes() []core.AgentType {
	if r.adapters == nil {
		panic("handlercontract: AdapterRegistry.RegisteredTypes called on zero-value registry; use NewAdapterRegistry")
	}
	types := make([]core.AgentType, 0, len(r.adapters))
	for at := range r.adapters {
		types = append(types, at)
	}
	return types
}

// Sealed reports whether the registry has been sealed (i.e., ForAgent has been
// called at least once).
//
// Once sealed, Register calls fail.  Sealed state is observable for daemon
// health-check and introspection purposes.
func (r *AdapterRegistry) Sealed() bool {
	if r.adapters == nil {
		panic("handlercontract: AdapterRegistry.Sealed called on zero-value registry; use NewAdapterRegistry")
	}
	return r.sealed
}
