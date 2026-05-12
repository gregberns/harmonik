package daemon

import (
	"io"

	"github.com/gregberns/harmonik/internal/eventbus"
)

// Config holds the startup configuration for the harmonik daemon.
//
// At MVH the struct is intentionally minimal: subsystem-specific fields are
// added by the per-registry beads (hk-8mup.62, hk-8i31.83) as each registry
// is wired into [Start].
//
// Spec ref: specs/process-lifecycle.md §4.6 PL-020 — internal/daemon is the
// composition root; Config is its public configuration surface.
type Config struct {
	// LogWriter is the destination for structured daemon log output.
	// A nil LogWriter silences all log output (useful in tests).
	LogWriter io.Writer
}

// Start is the composition-root entry point for the harmonik daemon.
//
// It executes the deterministic startup sequence defined by
// specs/process-lifecycle.md §4.2 PL-005, beginning with step 0:
// instantiate all cross-subsystem registries (event bus, control-point
// registry, handler registry, skill registry, policy registry) in-process
// per AR-INV-007 and PL-020a.
//
// Step 0 wiring as of hk-8mup.62:
//   - Instantiates the EventBus (eventbus.NewBusImpl) per EV-035.
//
// The RedactionRegistry (hk-8i31.83) will refactor bus construction to pass
// the registry in; at that point NewBusImpl is superseded by a registry-aware
// constructor.
//
// Spec ref: specs/process-lifecycle.md §4.6 PL-020, PL-020a, PL-005 step 0.
func Start(_ Config) error {
	// Step 0 (PL-005): bootstrap cross-subsystem registries.

	// Instantiate the EventBus (EV-035; hk-8mup.62).
	// The bus is not yet Seal()ed here because subsystems that Subscribe have
	// not yet been wired. Seal() will be called once all consumers register.
	_ = eventbus.NewBusImpl()

	return nil
}
