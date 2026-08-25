package spend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/runregistry"
)

type perQueueCounters struct {
	spentUSD float64 // accumulated attributed USD this day (bytes / bytesPerUSD)
	paused   bool    // true once this queue was paused-by-budget this day (idempotency guard)
}

// PerQueueSpendMeter tracks per-queue daemon-spawned claude spend (via
// budget_accrual events, attributed back to a queue through the runregistry.RunRegistry) and
// pauses ONLY a queue whose attributed daily spend reaches its own
// Queue.SpendCapUSD ceiling (NQ-X1). The global DaemonSpendMeter remains the
// daemon-wide ceiling.
//
// All methods are safe for concurrent use.
type PerQueueSpendMeter struct {
	mu sync.Mutex

	// per-day state — reset on UTC midnight rollover.
	dayKey   string                       // YYYY-MM-DD
	counters map[string]*perQueueCounters // keyed by queue name

	// oversubLogged records queue names for which the cap>global oversubscription
	// warning has already been emitted, so it is logged at most once per queue.
	oversubLogged map[string]struct{}

	// collaborators — immutable after construction.
	reg        *runregistry.RunRegistry
	store      *queuewiring.QueueStore
	projectDir string

	// globalCapUSD is the daemon-wide USD ceiling (derived the same way as
	// DaemonSpendMeter's dailyCapBytes: FLYWHEEL_BUDGET_USD_PER_DAY, default 20,
	// 0 when "unlimited"). Used ONLY to detect per-queue cap>global
	// oversubscription for the once-per-queue warning; it does NOT itself gate
	// admission here (the global DaemonSpendMeter owns that). 0 = global USD
	// ceiling disabled → no oversubscription is possible.
	globalCapUSD float64
}

// NewPerQueueSpendMeter constructs a PerQueueSpendMeter. reg is the shared
// *runregistry.RunRegistry used to attribute a budget_accrual chunk to its queue; store is
// the QueueStore whose Queue.Status this meter mutates on cap-trip and rollover;
// projectDir is the persist root (empty disables persistence, e.g. in tests).
//
// Bead ref: hk-tigaf.11.
func NewPerQueueSpendMeter(reg *runregistry.RunRegistry, store *queuewiring.QueueStore, projectDir string) *PerQueueSpendMeter {
	return &PerQueueSpendMeter{
		dayKey:        spendMeterTodayKey(),
		counters:      make(map[string]*perQueueCounters),
		oversubLogged: make(map[string]struct{}),
		reg:           reg,
		store:         store,
		projectDir:    projectDir,
		globalCapUSD:  spendMeterGlobalCapUSD(),
	}
}

// Subscribe registers the meter's asynchronous budget_accrual consumer with the
// bus. Must be called before bus.Seal per EV-009.
//
// Bead ref: hk-tigaf.11.
func (m *PerQueueSpendMeter) Subscribe(bus eventbus.EventBus) error {
	accrualSub := core.Subscription{
		ConsumerID:    "per-queue-spend-meter-budget-accrual",
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
		return fmt.Errorf("PerQueueSpendMeter.Subscribe: budget_accrual consumer: %w", err)
	}
	return nil
}

func (m *PerQueueSpendMeter) handleBudgetAccrual(ctx context.Context, evt core.Event) error {
	var payload core.BudgetAccrualPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return nil
	}
	if payload.CostBasis != core.CostBasisOutputBytes {
		return nil // only accumulate output_bytes at this layer (mirrors the global meter)
	}
	if payload.CostUnits <= 0 {
		return nil
	}

	if m.reg == nil {
		return nil
	}
	handle, ok := m.reg.Get(payload.RunID)
	if !ok {
		return nil
	}
	queueName := handle.QueueName
	if queueName == "" {
		return nil
	}

	q := m.store.QueueByName(queueName)
	if q == nil {
		return nil // queue not loaded (completed/cleared) — nothing to pause.
	}
	capUSD := q.SpendCapUSD
	if capUSD <= 0 {
		return nil // no per-queue cap on this queue.
	}

	m.maybeLogOversubscription(queueName, capUSD)

	chunkUSD := payload.CostUnits / bytesPerUSD

	m.mu.Lock()
	m.rolloverIfNewDayLocked(ctx)
	c := m.counters[queueName]
	if c == nil {
		c = &perQueueCounters{}
		m.counters[queueName] = c
	}
	if c.paused {
		m.mu.Unlock()
		return nil
	}
	c.spentUSD += chunkUSD
	tripped := c.spentUSD >= capUSD
	if tripped {
		c.paused = true
	}
	m.mu.Unlock()

	if tripped {
		return m.pauseQueueByBudget(ctx, queueName)
	}
	return nil
}

func (m *PerQueueSpendMeter) pauseQueueByBudget(ctx context.Context, queueName string) error {
	lq := m.store.LockForMutation()
	q := lq.LockedQueueByName(queueName)
	if q == nil {
		lq.Done()
		return nil // queue cleared between accrual and pause — nothing to do.
	}
	if q.Status != queue.QueueStatusActive {
		lq.Done()
		return nil
	}
	q.Status = queue.QueueStatusPausedByBudget
	lq.LockedSetQueueByName(queueName, q)

	if m.projectDir != "" {
		if err := queue.Persist(ctx, m.projectDir, q); err != nil {
			lq.Done()
			return fmt.Errorf("PerQueueSpendMeter.pauseQueueByBudget[%s]: persist: %w", queueName, err)
		}
	}
	lq.Done()
	return nil
}

func (m *PerQueueSpendMeter) rolloverIfNewDayLocked(ctx context.Context) {
	today := spendMeterTodayKey()
	if today == m.dayKey {
		return
	}
	m.dayKey = today
	m.counters = make(map[string]*perQueueCounters)
	m.unpauseBudgetPausedQueues(ctx)
}

func (m *PerQueueSpendMeter) unpauseBudgetPausedQueues(ctx context.Context) {
	lq := m.store.LockForMutation()
	var resumed bool
	for _, name := range lq.LockedAllQueueNames() {
		q := lq.LockedQueueByName(name)
		if q == nil || q.Status != queue.QueueStatusPausedByBudget {
			continue
		}
		q.Status = queue.QueueStatusActive
		lq.LockedSetQueueByName(name, q)
		resumed = true
		if m.projectDir != "" {
			if err := queue.Persist(ctx, m.projectDir, q); err != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: per-queue-spend-meter: rollover resume[%s]: persist failed (in-memory active): %v\n",
					name, err)
			}
		}
	}
	lq.Done()

	if resumed && m.store != nil {
		m.store.Wake()
	}
}

func (m *PerQueueSpendMeter) maybeLogOversubscription(queueName string, capUSD float64) {
	if m.globalCapUSD <= 0 || capUSD <= m.globalCapUSD {
		return
	}
	m.mu.Lock()
	if _, done := m.oversubLogged[queueName]; done {
		m.mu.Unlock()
		return
	}
	m.oversubLogged[queueName] = struct{}{}
	m.mu.Unlock()
	fmt.Fprintf(os.Stderr,
		"daemon: per-queue spend cap: queue %q spend_cap_usd=%.2f oversubscribes global daily budget=%.2f USD; global ceiling still applies (NQ-X1)\n",
		queueName, capUSD, m.globalCapUSD)
}

func spendMeterGlobalCapUSD() float64 {
	budgetEnv := os.Getenv(envFlywheelBudgetUSDPerDay)
	switch budgetEnv {
	case "":
		return defaultDailyBudgetUSD
	case "unlimited":
		return 0
	default:
		if capUSD, err := strconv.ParseFloat(budgetEnv, 64); err == nil && capUSD > 0 {
			return capUSD
		}
		return defaultDailyBudgetUSD
	}
}
