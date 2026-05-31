package daemon

// spendmeter_hkk3f8g.go — daemon-side per-day spend meter (hk-k3f8g).
//
// DaemonSpendMeter implements the daemon-side half of the CL-090 / CL-090a
// unified spend ceiling:
//
//   - CL-090a (max-runs backstop): counts run_started events since the last
//     UTC midnight; when runsToday >= maxRunsPerDay the meter emits
//     budget_exhausted{budget_scope=handler_account} so the existing HP-012
//     handler-pause policy pauses the claude handler type and halts dispatch.
//
//   - CL-090 (bytes proxy): accumulates budget_accrual output_bytes per day as
//     a proxy for USD spend; when bytesToday >= dailyCapBytes (derived from
//     FLYWHEEL_BUDGET_USD_PER_DAY × bytesPerUSD) the same budget_exhausted event
//     is emitted.  Because bytes are an imprecise proxy for USD the max-runs
//     ceiling is the loss-proof backstop for this bead; exact USD attribution is
//     deferred to a Pi-side unified meter (CL-090 full spec).
//
// Lifecycle: construct with NewDaemonSpendMeter, call Subscribe(bus) before
// bus.Seal(). The meter is asynchronous: trip and emission happen inside the
// bus worker-pool goroutine that delivers the triggering event, not on the
// emitting path.
//
// Day boundary: UTC midnight, tracked as a YYYY-MM-DD key. State resets
// automatically (runs, bytes, exhausted flag) on the first event processed
// after a day rollover. There is no separate reset goroutine.
//
// Idempotency: budget_exhausted is emitted at most once per calendar day.
// Subsequent events within the same day are no-ops once exhausted=true.
//
// Spec ref: specs/cognition-loop.md §4.11 CL-090, CL-090a.
// Spec ref: specs/handler-pause.md §11a, HP-012.
// Spec ref: specs/event-model.md §8.4.3.
// Bead ref: hk-k3f8g.

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
	// defaultMaxRunsPerDay is the max-runs ceiling applied when
	// HARMONIK_MAX_RUNS_PER_DAY is not set.  Finite by design per CL-090a.
	defaultMaxRunsPerDay = 200

	// envMaxRunsPerDay is the environment variable an operator can set to
	// override the per-day max-runs ceiling.  Value must be a positive integer;
	// "unlimited" is not accepted (per-day ceiling must be finite per CL-090a).
	envMaxRunsPerDay = "HARMONIK_MAX_RUNS_PER_DAY"

	// envFlywheelBudgetUSDPerDay is the existing env var that controls the
	// per-day USD cap on the Pi side; the daemon meter reads the same variable
	// so operators tune one knob for both layers.
	envFlywheelBudgetUSDPerDay = "FLYWHEEL_BUDGET_USD_PER_DAY"

	// defaultDailyBudgetUSD is the USD cap applied when
	// FLYWHEEL_BUDGET_USD_PER_DAY is not set.  Mirrors the flywheel default.
	defaultDailyBudgetUSD = 20.0

	// bytesPerUSD is the rough output-bytes → USD conversion rate used to
	// translate the USD cap into a bytes-per-day ceiling.
	//
	// Derivation: claude-sonnet-4-6 output pricing ≈ $3/1M output tokens
	// × ~500 bytes/token = $3/500k bytes ≈ $1/166k bytes → 166_000 bytes/USD.
	// Using a conservative 100_000 to trip slightly early rather than late.
	//
	// Operators who rely on precise USD enforcement should prefer the max-runs
	// ceiling (HARMONIK_MAX_RUNS_PER_DAY) which does not require unit conversion.
	bytesPerUSD float64 = 100_000

	// daemonDailyBudgetRef is the BudgetRef carried in budget_exhausted events
	// emitted by DaemonSpendMeter.  Distinct from per-run budget refs so
	// consumers can discriminate by budget_ref if needed.
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
			// Operator explicitly opted out of the USD ceiling.
			dailyCapBytes = 0
		} else {
			// Unset → use default.
			dailyCapBytes = defaultDailyBudgetUSD * bytesPerUSD
		}
	default:
		if capUSD, err := strconv.ParseFloat(budgetEnv, 64); err == nil && capUSD > 0 {
			dailyCapBytes = capUSD * bytesPerUSD
		} else {
			// Unparseable value — fall back to default (fail-closed).
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
			Types: map[string]struct{}{
				string(core.EventTypeRunStarted): {},
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
			Types: map[string]struct{}{
				string(core.EventTypeBudgetAccrual): {},
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

// handleRunStarted increments the daily run counter and trips the meter when
// runsToday >= maxRunsPerDay (CL-090a).
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

// handleBudgetAccrual accumulates output_bytes and trips the meter when
// bytesToday >= dailyCapBytes (CL-090 bytes proxy).
func (m *DaemonSpendMeter) handleBudgetAccrual(ctx context.Context, evt core.Event) error {
	if m.dailyCapBytes <= 0 {
		return nil // bytes-cap disabled (FLYWHEEL_BUDGET_USD_PER_DAY=unlimited)
	}

	var payload core.BudgetAccrualPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		// Malformed payload — skip; bus dead-letter path handles persistent failures.
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

// emitExhausted emits budget_exhausted{budget_scope=handler_account} exactly
// once per day. Concurrent callers are serialised by the exhausted flag under mu.
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

// rolloverIfNewDayLocked resets per-day counters when the UTC date has changed.
// MUST be called while m.mu is held.
func (m *DaemonSpendMeter) rolloverIfNewDayLocked() {
	today := spendMeterTodayKey()
	if today != m.dayKey {
		m.dayKey = today
		m.runsToday = 0
		m.bytesToday = 0
		m.exhausted = false
	}
}

// spendMeterTodayKey returns the current UTC date as "YYYY-MM-DD".
func spendMeterTodayKey() string {
	return time.Now().UTC().Format("2006-01-02")
}
