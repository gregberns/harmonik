package handlercontract

import (
	"fmt"
	"sync"

	"github.com/gregberns/harmonik/internal/core"
)

// HarnessRegistry maps a core.AgentType to its concrete Harness implementation.
//
// Exactly one Harness may be registered per agent_type. The daemon MUST register
// every harness it supports before it begins dispatching (i.e., before the first
// ForAgent lookup, which seals the registry).
//
// # No per-run state in the registry
//
// The registry stores Harness instances, not per-run state. Harness methods take
// a RunCtx; per-run state lives in the caller's stack, not in the Harness or the
// registry.
//
// # Sealed after first lookup
//
// The registry is sealed implicitly on the first ForAgent call. Register calls
// after sealing return an error, mirroring AdapterRegistry's race-prevention
// contract.
//
// # Zero value not usable
//
// Callers MUST use NewHarnessRegistry; the zero-value registry panics on every
// method call.
type HarnessRegistry struct {
	mu        sync.RWMutex
	sealed    bool
	harnesses map[core.AgentType]Harness
}

// NewHarnessRegistry creates an empty, unsealed HarnessRegistry.
func NewHarnessRegistry() *HarnessRegistry {
	return &HarnessRegistry{
		harnesses: make(map[core.AgentType]Harness),
	}
}

// Register adds harness for agentType to the registry.
//
// Returns an error if:
//   - agentType is not a valid AgentType (does not match AR-025 regex)
//   - harness is nil
//   - agentType is already registered (duplicate — exactly one Harness per type)
//   - the registry has been sealed (ForAgent was already called)
//
// Callers MUST register all agent types before the first ForAgent call.
func (r *HarnessRegistry) Register(agentType core.AgentType, harness Harness) error {
	if r.harnesses == nil {
		panic("handlercontract: HarnessRegistry.Register called on zero-value registry; use NewHarnessRegistry")
	}
	if !agentType.Valid() {
		return fmt.Errorf(
			"handlercontract: HarnessRegistry.Register: invalid agent_type %q; "+
				"must match ^[a-z][a-z0-9-]{1,62}$ (AR-025)",
			agentType,
		)
	}
	if harness == nil {
		return fmt.Errorf(
			"handlercontract: HarnessRegistry.Register(%q): harness is nil — daemon defect",
			agentType,
		)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return fmt.Errorf(
			"handlercontract: HarnessRegistry.Register(%q): registry is sealed (ForAgent was already called); "+
				"all agent types must be registered before dispatching begins — daemon defect",
			agentType,
		)
	}
	if _, ok := r.harnesses[agentType]; ok {
		return fmt.Errorf(
			"handlercontract: HarnessRegistry.Register(%q): duplicate registration; "+
				"exactly one Harness per agent_type",
			agentType,
		)
	}
	r.harnesses[agentType] = harness
	return nil
}

// ForAgent returns the Harness registered for agentType and seals the registry.
//
// Returns (harness, nil) on success. Returns (nil, error) if agentType has no
// registered harness (unregistered agent type — the daemon resolved a harness for
// an agent_type that was never registered, which is a daemon defect or a config
// pointing at a harness not yet wired in).
//
// The first ForAgent call seals the registry: subsequent Register calls fail.
func (r *HarnessRegistry) ForAgent(agentType core.AgentType) (Harness, error) {
	if r.harnesses == nil {
		panic("handlercontract: HarnessRegistry.ForAgent called on zero-value registry; use NewHarnessRegistry")
	}
	r.mu.Lock()
	r.sealed = true
	harness, ok := r.harnesses[agentType]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf(
			"handlercontract: HarnessRegistry.ForAgent(%q): no harness registered for this agent_type; "+
				"the daemon must Register all supported harnesses at startup before dispatching",
			agentType,
		)
	}
	return harness, nil
}

// RegisteredTypes returns the set of agent types that have a registered harness.
//
// The returned slice is a snapshot; mutations to the registry after this call are
// not reflected. The order is unspecified.
func (r *HarnessRegistry) RegisteredTypes() []core.AgentType {
	if r.harnesses == nil {
		panic("handlercontract: HarnessRegistry.RegisteredTypes called on zero-value registry; use NewHarnessRegistry")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]core.AgentType, 0, len(r.harnesses))
	for at := range r.harnesses {
		types = append(types, at)
	}
	return types
}

// Sealed reports whether the registry has been sealed (i.e., ForAgent has been
// called at least once). Once sealed, Register calls fail.
func (r *HarnessRegistry) Sealed() bool {
	if r.harnesses == nil {
		panic("handlercontract: HarnessRegistry.Sealed called on zero-value registry; use NewHarnessRegistry")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sealed
}
