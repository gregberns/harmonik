package daemon

// export_meters_pause_test.go — test-seam exports for internal/daemon spend
// meters and pause controllers (RT19.14 split of export_test.go): the
// handlerpause_policy_37zy8.go, spendmeter_hkk3f8g.go,
// perqueuespendmeter_tigaf11.go, operatorpause.go, handlerpause_9hwbw.go and
// handlerpause_autoresume_0otqs.go seams. package daemon test file; see
// export_test.go header for the seam rationale. Bead: hk-ecrxy.

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

// ExportedHandlerPausePolicyConfig is a type alias for HandlerPausePolicyConfig
// so tests in package daemon_test can reference the type directly.
//
// Bead ref: hk-37zy8.
type ExportedHandlerPausePolicyConfig = HandlerPausePolicyConfig

// ExportedNewHandlerPausePolicyGoroutine exposes NewHandlerPausePolicyGoroutine
// for tests in package daemon_test.
//
// Bead ref: hk-37zy8.
var ExportedNewHandlerPausePolicyGoroutine = NewHandlerPausePolicyGoroutine

// ExportedPolicyHandleRateLimitStatus invokes the unexported
// handleRateLimitStatus method on a HandlerPausePolicyGoroutine for tests in
// package daemon_test.
//
// Bead ref: hk-37zy8.
func ExportedPolicyHandleRateLimitStatus(p *HandlerPausePolicyGoroutine, ctx context.Context, evt core.Event) error {
	return p.handleRateLimitStatus(ctx, evt)
}

// ExportedPolicyHandleBudgetExhausted invokes the unexported
// handleBudgetExhausted method on a HandlerPausePolicyGoroutine for tests in
// package daemon_test.
//
// Bead ref: hk-37zy8.
func ExportedPolicyHandleBudgetExhausted(p *HandlerPausePolicyGoroutine, ctx context.Context, evt core.Event) error {
	return p.handleBudgetExhausted(ctx, evt)
}

// ExportedNewDaemonSpendMeter constructs a DaemonSpendMeter backed by the
// given bus for tests in package daemon_test.
//
// Bead ref: hk-k3f8g.
func ExportedNewDaemonSpendMeter(bus eventbus.EventBus) *DaemonSpendMeter {
	return NewDaemonSpendMeter(bus)
}

// ExportedSpendMeterHandleRunStarted invokes the unexported handleRunStarted
// method on a DaemonSpendMeter for tests in package daemon_test.
//
// Bead ref: hk-k3f8g.
func ExportedSpendMeterHandleRunStarted(m *DaemonSpendMeter, ctx context.Context, evt core.Event) error {
	return m.handleRunStarted(ctx, evt)
}

// ExportedSpendMeterHandleBudgetAccrual invokes the unexported handleBudgetAccrual
// method on a DaemonSpendMeter for tests in package daemon_test.
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
// package daemon_test (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedNewPerQueueSpendMeter(reg *RunRegistry, store *queuewiring.QueueStore, projectDir string) *PerQueueSpendMeter {
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

// ExportedNewOperatorPauseController exposes NewOperatorPauseController for
// tests in package daemon_test.
//
// Bead ref: hk-ry8q1.
var ExportedNewOperatorPauseController = NewOperatorPauseController

// ExportedAutoResumeConfig is a type alias for AutoResumeConfig so tests in
// package daemon_test can reference the type directly.
//
// Bead ref: hk-0otqs.
type ExportedAutoResumeConfig = AutoResumeConfig

// ExportedHandlerPauseControllerSchedule exposes HandlerPauseController.Schedule
// for tests in package daemon_test.
//
// Bead ref: hk-0otqs.
func ExportedHandlerPauseControllerSchedule(c *HandlerPauseController, ctx context.Context, agentType core.AgentType, after time.Duration) {
	c.Schedule(ctx, agentType, after)
}

// ExportedHandlerPauseControllerSetAutoResumeCfg exposes
// HandlerPauseController.SetAutoResumeConfig for tests in package daemon_test.
//
// Bead ref: hk-0otqs.
func ExportedHandlerPauseControllerSetAutoResumeCfg(c *HandlerPauseController, agentType core.AgentType, cfg AutoResumeConfig) {
	c.SetAutoResumeConfig(agentType, cfg)
}
