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

// ExportedMarkRunAborted sets the abort latch on h — the same latch every
// StaleWatcher reaper sets immediately before it calls h.Cancel (the
// never-spawned reaper, the kill-consumer backstop and the fast dead-process
// reap all do exactly this pair). A test that drives an abort through a live run
// needs the pair, and the latch is what tells the run path an abort apart from a
// daemon-wide shutdown.
//
// It is a setter on a handle the caller already owns, not a door into daemon
// state. Driving a real StaleWatcher reaper would prove more — that a reaper
// reaches the run at all — but that is hk-0z5x's claim, not the claim of the
// test that uses this. If a second caller ever appears, prefer standing up
// NewStaleWatcher with a short threshold and driving ExportedStalewatchScan.
//
// Bead ref: hk-0z5x, hk-aekon.
func ExportedMarkRunAborted(h *RunHandle) {
	h.aborted.Store(true)
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
