package runregistry

import (
	"github.com/gregberns/harmonik/internal/core"
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
