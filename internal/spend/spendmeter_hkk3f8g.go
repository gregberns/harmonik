package spend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

const (
	defaultMaxRunsPerDay = 200

	envMaxRunsPerDay = "HARMONIK_MAX_RUNS_PER_DAY"

	envFlywheelBudgetUSDPerDay = "FLYWHEEL_BUDGET_USD_PER_DAY"

	defaultDailyBudgetUSD = 20.0

	bytesPerUSD float64 = 100_000

	daemonDailyBudgetRef core.BudgetRef = "daemon-daily"
)

// DaemonSpendMeter tracks daemon-spawned claude session cost (via budget_accrual
// events) and run count (via run_started events) per day, emitting
// budget_exhausted{budget_scope=handler_account} when either ceiling is reached.
//
// All exported methods are safe for concurrent use.
type DaemonSpendMeter struct {
	mu sync.Mutex

	// per-day state — reset on UTC midnight rollover.
	dayKey     string  // YYYY-MM-DD
	runsToday  int     // count of run_started events this day
	bytesToday float64 // accumulated output_bytes from budget_accrual this day
	exhausted  bool    // true once budget_exhausted emitted this day

	// configuration — immutable after construction.
	maxRunsPerDay int
	dailyCapBytes float64 // 0 = bytes-cap disabled (FLYWHEEL_BUDGET_USD_PER_DAY=unlimited)
	bus           eventbus.EventBus
}

// NewDaemonSpendMeter constructs a DaemonSpendMeter, reading caps from the
// process environment:
//
//   - HARMONIK_MAX_RUNS_PER_DAY: positive integer; default 200.
//   - FLYWHEEL_BUDGET_USD_PER_DAY: positive float or "unlimited"; default 20 USD.
//     "unlimited" disables the bytes proxy ceiling (max-runs ceiling still active).
func NewDaemonSpendMeter(bus eventbus.EventBus) *DaemonSpendMeter {
	maxRuns := defaultMaxRunsPerDay
	if v := os.Getenv(envMaxRunsPerDay); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxRuns = n
		}
	}

	var dailyCapBytes float64
	budgetEnv := os.Getenv(envFlywheelBudgetUSDPerDay)
	switch budgetEnv {
	case "", "unlimited":
		if budgetEnv == "unlimited" {
			dailyCapBytes = 0
		} else {
			dailyCapBytes = defaultDailyBudgetUSD * bytesPerUSD
		}
	default:
		if capUSD, err := strconv.ParseFloat(budgetEnv, 64); err == nil && capUSD > 0 {
			dailyCapBytes = capUSD * bytesPerUSD
		} else {
			dailyCapBytes = defaultDailyBudgetUSD * bytesPerUSD
		}
	}

	return &DaemonSpendMeter{
		dayKey:        spendMeterTodayKey(),
		maxRunsPerDay: maxRuns,
		dailyCapBytes: dailyCapBytes,
		bus:           bus,
	}
}

// Subscribe registers the meter's two asynchronous consumers with the bus.
// Must be called before bus.Seal per EV-009.
//
// Consumers registered:
//   - daemon-spend-meter-run-started (run_started) — CL-090a max-runs counter.
//   - daemon-spend-meter-budget-accrual (budget_accrual) — CL-090 bytes proxy.
func (m *DaemonSpendMeter) Subscribe(bus eventbus.EventBus) error {
	runStartedSub := core.Subscription{
		ConsumerID:    "daemon-spend-meter-run-started",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				core.EventTypeRunStarted: {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: m.handleRunStarted,
	}
	if _, err := bus.Subscribe(runStartedSub); err != nil {
		return fmt.Errorf("DaemonSpendMeter.Subscribe: run_started consumer: %w", err)
	}

	accrualSub := core.Subscription{
		ConsumerID:    "daemon-spend-meter-budget-accrual",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				core.EventTypeBudgetAccrual: {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: m.handleBudgetAccrual,
	}
	if _, err := bus.Subscribe(accrualSub); err != nil {
		return fmt.Errorf("DaemonSpendMeter.Subscribe: budget_accrual consumer: %w", err)
	}

	return nil
}

func (m *DaemonSpendMeter) handleRunStarted(ctx context.Context, _ core.Event) error {
	m.mu.Lock()
	m.rolloverIfNewDayLocked()
	if m.exhausted {
		m.mu.Unlock()
		return nil
	}
	m.runsToday++
	runs := m.runsToday
	maxRuns := m.maxRunsPerDay
	m.mu.Unlock()

	if runs >= maxRuns {
		spentUSD := float64(runs)
		capUSD := float64(maxRuns)
		return m.emitExhausted(ctx, spentUSD, capUSD)
	}
	return nil
}

func (m *DaemonSpendMeter) handleBudgetAccrual(ctx context.Context, evt core.Event) error {
	if m.dailyCapBytes <= 0 {
		return nil // bytes-cap disabled (FLYWHEEL_BUDGET_USD_PER_DAY=unlimited)
	}

	var payload core.BudgetAccrualPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return nil
	}
	if payload.CostBasis != core.CostBasisOutputBytes {
		return nil // only accumulate output_bytes at this layer
	}
	if payload.CostUnits <= 0 {
		return nil
	}

	m.mu.Lock()
	m.rolloverIfNewDayLocked()
	if m.exhausted {
		m.mu.Unlock()
		return nil
	}
	m.bytesToday += payload.CostUnits
	bytes := m.bytesToday
	capBytes := m.dailyCapBytes
	m.mu.Unlock()

	if bytes >= capBytes {
		spentUSD := bytes / bytesPerUSD
		capUSD := capBytes / bytesPerUSD
		return m.emitExhausted(ctx, spentUSD, capUSD)
	}
	return nil
}

func (m *DaemonSpendMeter) emitExhausted(ctx context.Context, spentUSD, capUSD float64) error {
	m.mu.Lock()
	if m.exhausted {
		m.mu.Unlock()
		return nil // idempotent
	}
	m.exhausted = true
	m.mu.Unlock()

	scope := core.BudgetScopeHandlerAccount
	payload := core.BudgetExhaustedEventPayload{
		BudgetRef:   daemonDailyBudgetRef,
		BudgetScope: &scope,
		SpentUSD:    &spentUSD,
		CapUSD:      &capUSD,
	}
	payloadJSON, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return fmt.Errorf("DaemonSpendMeter.emitExhausted: marshal: %w", marshalErr)
	}
	if emitErr := m.bus.Emit(ctx, core.EventTypeBudgetExhausted, payloadJSON); emitErr != nil {
		return fmt.Errorf("DaemonSpendMeter.emitExhausted: emit budget_exhausted: %w", emitErr)
	}
	return nil
}

func (m *DaemonSpendMeter) rolloverIfNewDayLocked() {
	today := spendMeterTodayKey()
	if today != m.dayKey {
		m.dayKey = today
		m.runsToday = 0
		m.bytesToday = 0
		m.exhausted = false
	}
}

func spendMeterTodayKey() string {
	return time.Now().UTC().Format("2006-01-02")
}
