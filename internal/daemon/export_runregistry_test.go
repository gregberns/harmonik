package daemon

// export_runregistry_test.go — test-seam exports for internal/daemon run
// registry, run-wait and per-run event-tap fan-out (RT19.18 split of
// export_test.go): runregistry.go, workloopeventsource.go and the captured-
// spawn-proof seams. package daemon test file; see export_test.go header for the
// seam rationale. Bead: hk-ecrxy.

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/runloop"
)

// ExportedNewRunRegistry creates a fresh RunRegistry for tests.
//
// Bead ref: hk-guez.
func ExportedNewRunRegistry() *RunRegistry {
	return NewRunRegistry()
}

// ExportedRunHandleIsAborted returns true if the RunHandle's aborted flag is set.
// Used by the never-spawned reaper tests (hk-0z5x).
func ExportedRunHandleIsAborted(h *RunHandle) bool {
	return h.aborted.Load()
}

// ExportedRunRegistryRegister registers a handle under runID for tests (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedRunRegistryRegister(r *RunRegistry, runID core.RunID, handle *RunHandle) {
	r.Register(runID, handle)
}

// ExportedPerRunEventTap wraps the per-run fan-out event
// tap so the competing-consumer race regression test can construct one and
// register multiple independent subscribers (hk-37giq).
type ExportedPerRunEventTap struct {
	*runloop.PerRunEventTap
}

// noopExportedEmitter is a no-op handlercontract.EventEmitter used as the tap's
// underlying bus in the fan-out regression test: it discards all emits so the
// test exercises ONLY the per-subscriber fan-out behaviour.
type noopExportedEmitter struct{}

func (noopExportedEmitter) Emit(context.Context, core.EventType, []byte) error { return nil }

func (noopExportedEmitter) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return nil
}

// ExportedNewPerRunEventTap constructs a perRunEventTap backed by a no-op
// underlying emitter and returns the tap plus its initial subscriber channel
// (the same channel newChanAgentEventSource/waitAgentReady consumes in
// production). Additional independent subscribers are obtained via
// tap.ExportedSubscribe (hk-37giq).
func ExportedNewPerRunEventTap(runID core.RunID) (tap *ExportedPerRunEventTap, events <-chan core.EventEnvelope) {
	inner, events := runloop.NewPerRunEventTap(noopExportedEmitter{}, runID)
	return &ExportedPerRunEventTap{PerRunEventTap: inner}, events
}

// ExportedSubscribe registers and returns a new independent subscriber channel
// on the tap (hk-37giq).
func (t *ExportedPerRunEventTap) ExportedSubscribe() <-chan core.EventEnvelope {
	return t.Subscribe()
}

// ExportedEmit fans an event of eventType out to every subscriber via the tap's
// production Emit path (hk-37giq).
func (t *ExportedPerRunEventTap) ExportedEmit(ctx context.Context, eventType core.EventType) error {
	return t.Emit(ctx, eventType, nil)
}

// ExportedNewCapturedSpawnProof exposes newCapturedSpawnProof (hk-47u9z) so the
// regression test drives the PRODUCTION spawn-proof closure rather than a
// restatement of it. A test that rebuilds the closure itself passes with the
// production wiring reverted — verified, and it is how the first draft of the
// hk-47u9z test was a false green.
func ExportedNewCapturedSpawnProof(ctx context.Context, emitter handlercontract.EventEmitter, runID core.RunID) func() {
	tap, _ := runloop.NewPerRunEventTap(emitter, runID)
	return newCapturedSpawnProof(ctx, tap, runID)
}
