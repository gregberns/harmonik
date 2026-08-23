package daemon

import (
	"context"
	"sync/atomic"
	"time"
)

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
