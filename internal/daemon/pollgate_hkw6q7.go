package daemon

// pollgate_hkw6q7.go — INACTIVE poll gate for SS-007 watcher arming.
//
// PollGate is the shared gate that StaleWatcher and BandwidthTuner check before
// running their scan/tick.  When the fleet ActivityLabel is INACTIVE both watchers
// return early (the SS-007 "OFF at INACTIVE" requirement).
//
// startPollGate starts a background goroutine (one per daemon instance) that
// re-evaluates the label every pollGateInterval via LiveStateBuilder.Build and
// calls gate.SetInactive accordingly.
//
// Spec ref: specs/system-state.md §4.3 SS-007 (poll-arming is mechanism).
// Bead ref: hk-w6q7 (P2-b: system-state fold + poll-gating).

import (
	"context"
	"sync/atomic"
	"time"
)

// pollGateInterval is how often startPollGate re-evaluates ActivityLabel.
// Matches staleWatchScanInterval so one label evaluation fires per potential
// StaleWatcher scan tick.
const pollGateInterval = staleWatchScanInterval // 30 s

// PollGate is the shared INACTIVE gate for watchers listed as OFF at INACTIVE in
// the SS-007 poll-arming table (StaleWatcher, BandwidthTuner).  When inactive the
// watchers return early from their scan/tick without doing work.
//
// The gate is updated by startPollGate.  Zero value is ungated (watchers run
// normally), which is the correct default for unit-test mode where startPollGate
// is not called.  Safe for concurrent use via atomic.Bool.
type PollGate struct {
	inactive atomic.Bool
}

// SetInactive stores whether the fleet label is currently INACTIVE.
// Safe to call from any goroutine.
func (g *PollGate) SetInactive(v bool) { g.inactive.Store(v) }

// IsInactive returns true when the fleet label is INACTIVE (watcher should skip).
func (g *PollGate) IsInactive() bool { return g.inactive.Load() }

// startPollGate starts a background goroutine that evaluates the fleet
// ActivityLabel every pollGateInterval (via builder.Build) and updates gate.
// builder must not be nil.  The goroutine exits when ctx is cancelled.
//
// Only called from daemon.Start when cfg.ProjectDir is set (the same condition
// that creates the LiveStateBuilder).  In unit-test mode the gate stays at its
// zero value (IsInactive==false) so watchers always run.
func startPollGate(ctx context.Context, gate *PollGate, builder *LiveStateBuilder) {
	go func() {
		ticker := time.NewTicker(pollGateInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				snap := builder.Build(ctx)
				gate.SetInactive(snap.ActivityLabel == ActivityInactive)
			}
		}
	}()
}
