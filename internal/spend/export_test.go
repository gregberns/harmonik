// Package spend tracks daemon-spawned claude session cost — daemon-wide daily
// budget accounting and per-queue budget pausing — and pauses the daemon or an
// individual queue when its ceiling is reached.
package spend

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/runregistry"
)

// ExportedNewDaemonSpendMeter constructs a DaemonSpendMeter backed by the
// given bus for tests in package spend_test.
//
// Bead ref: hk-k3f8g.
func ExportedNewDaemonSpendMeter(bus eventbus.EventBus) *DaemonSpendMeter {
	return NewDaemonSpendMeter(bus)
}

// ExportedSpendMeterHandleRunStarted invokes the unexported handleRunStarted
// method on a DaemonSpendMeter for tests in package spend_test.
//
// Bead ref: hk-k3f8g.
func ExportedSpendMeterHandleRunStarted(m *DaemonSpendMeter, ctx context.Context, evt core.Event) error {
	return m.handleRunStarted(ctx, evt)
}

// ExportedSpendMeterHandleBudgetAccrual invokes the unexported handleBudgetAccrual
// method on a DaemonSpendMeter for tests in package spend_test.
//
// Bead ref: hk-k3f8g.
func ExportedSpendMeterHandleBudgetAccrual(m *DaemonSpendMeter, ctx context.Context, evt core.Event) error {
	return m.handleBudgetAccrual(ctx, evt)
}

// ExportedSpendMeterSetMaxRunsPerDay overrides the meter's maxRunsPerDay for
// deterministic test scenarios (avoids process-env mutation).
//
// Bead ref: hk-k3f8g.
func ExportedSpendMeterSetMaxRunsPerDay(m *DaemonSpendMeter, n int) {
	m.mu.Lock()
	m.maxRunsPerDay = n
	m.mu.Unlock()
}

// ExportedSpendMeterSetDailyCapBytes overrides the meter's dailyCapBytes for
// deterministic test scenarios.
//
// Bead ref: hk-k3f8g.
func ExportedSpendMeterSetDailyCapBytes(m *DaemonSpendMeter, b float64) {
	m.mu.Lock()
	m.dailyCapBytes = b
	m.mu.Unlock()
}

// ExportedNewPerQueueSpendMeter constructs a PerQueueSpendMeter for tests in
// package spend_test (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedNewPerQueueSpendMeter(reg *runregistry.RunRegistry, store *queuewiring.QueueStore, projectDir string) *PerQueueSpendMeter {
	return NewPerQueueSpendMeter(reg, store, projectDir)
}

// ExportedPerQueueSpendMeterHandleBudgetAccrual invokes the unexported
// handleBudgetAccrual method on a PerQueueSpendMeter for tests (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedPerQueueSpendMeterHandleBudgetAccrual(m *PerQueueSpendMeter, ctx context.Context, evt core.Event) error {
	return m.handleBudgetAccrual(ctx, evt)
}

// ExportedPerQueueSpendMeterSetDayKey overrides the meter's UTC day key to a
// past value so the next handled event forces a rollover (which resets counters
// and un-pauses paused-by-budget queues). Used to deterministically exercise the
// rollover un-pause path without waiting for real midnight (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedPerQueueSpendMeterSetDayKey(m *PerQueueSpendMeter, key string) {
	m.mu.Lock()
	m.dayKey = key
	m.mu.Unlock()
}

// ExportedPerQueueSpendMeterSetGlobalCapUSD overrides the meter's globalCapUSD
// for deterministic oversubscription-warning tests (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedPerQueueSpendMeterSetGlobalCapUSD(m *PerQueueSpendMeter, usd float64) {
	m.mu.Lock()
	m.globalCapUSD = usd
	m.mu.Unlock()
}
