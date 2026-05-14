package core

import (
	"errors"
	"fmt"
	"sync"
)

// ErrDuplicateSourceSubsystem is returned by RegisterSourceSubsystem when the
// same source_subsystem identifier is registered more than once.
//
// Spec ref: event-model.md §4.9 EV-034a — "duplicates MUST fail startup with a
// typed error."
var ErrDuplicateSourceSubsystem = errors.New("core: source_subsystem identifier already registered")

// subsystemRegistry holds the set of registered source_subsystem identifiers.
//
// Registration is startup-time per EV-034a; the mutex prevents data-race
// during init-order scenarios where multiple init() calls arrive concurrently
// (e.g., test binaries with parallel package-level inits).
type subsystemRegistry struct {
	mu          sync.Mutex
	identifiers map[string]struct{}
}

var globalSubsystemRegistry = &subsystemRegistry{
	identifiers: make(map[string]struct{}),
}

// RegisterSourceSubsystem registers a source_subsystem identifier at daemon
// init time. Each subsystem MUST call this exactly once with its Go-package
// identifier string per EV-034a.
//
// The identifier MUST be a non-empty Go-package-identifier string (e.g.,
// "github.com/harmonik/internal/orchestrator"). The registry is layout-open
// per EV-004: no fixed set is enumerated; callers declare their own identifier.
//
// Returns ErrDuplicateSourceSubsystem if the identifier has already been
// registered. This causes daemon startup to fail, catching typos and preventing
// two subsystems from sharing an identifier.
//
// Thread-safe.
func RegisterSourceSubsystem(id string) error {
	if id == "" {
		return fmt.Errorf("core: RegisterSourceSubsystem: id must not be empty")
	}
	r := globalSubsystemRegistry
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.identifiers[id]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateSourceSubsystem, id)
	}
	r.identifiers[id] = struct{}{}
	return nil
}

// subsystemRegistryReset replaces the global subsystem registry with an empty
// one. MUST be called only from test cleanup (t.Cleanup) to restore state.
// Not exported — visible only to tests in the same package (package core).
func subsystemRegistryReset() {
	globalSubsystemRegistry.mu.Lock()
	defer globalSubsystemRegistry.mu.Unlock()
	globalSubsystemRegistry.identifiers = make(map[string]struct{})
}
