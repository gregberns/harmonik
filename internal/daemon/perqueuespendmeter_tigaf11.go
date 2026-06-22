package daemon

// perqueuespendmeter_tigaf11.go — daemon-side OPTIONAL per-queue spend meter (NQ-X1, hk-tigaf.11).
//
// PerQueueSpendMeter is the per-queue sibling of DaemonSpendMeter
// (spendmeter_hkk3f8g.go). Where DaemonSpendMeter enforces ONE global daemon
// ceiling — tripping it halts ALL dispatch via handler-pause — this meter
// enforces an OPTIONAL, lower, PER-QUEUE ceiling (queue.Queue.SpendCapUSD) and
// pauses ONLY the offending queue, leaving sibling queues (and the global
// ceiling) untouched. A single busy queue can therefore no longer burn the
// whole budget and starve other queues.
//
// Composition with the global meter (the STRICTER ceiling binds):
//
//	A run is admitted iff
//	  global-remaining > 0 AND (queue has no cap OR queue-remaining > 0).
//
// The global DaemonSpendMeter still owns the daemon-wide ceiling and its trip
// (handler-pause) is unchanged. This meter adds the per-queue layer on top. A
// queue with no cap (SpendCapUSD <= 0) is byte-identical to the pre-NQ-X1
// daemon: this meter accrues nothing for it and never pauses it.
//
// Attribution: budget_accrual does NOT carry the queue name (its payload is
// RunID / SessionID / ChunkIndex / CostUnits / CostBasis). To attribute a chunk
// to its queue we look up the run in the shared RunRegistry:
//
//	handle, ok := reg.Get(payload.RunID); handle.QueueName
//
// Edge cases:
//   - empty QueueName (a br-ready-fallback run with no queue) → accrue to the
//     global meter only; this meter skips it (no per-queue attribution).
//   - Get MISS (the handle was Unregister'd before a late tail-chunk arrived) →
//     skip per-queue attribution for that chunk. Lossy-tail is acceptable here
//     (the budget_accrual event is durability class L, lossy-tail-ok per
//     EV-017); we do NOT error.
//
// Cap-trip: when a capped queue's attributed daily spend reaches its cap we set
// that queue's Status = QueueStatusPausedByBudget and Persist it. We do NOT
// route through handler-pause (HandlerPausePolicyGoroutine pauses the entire
// claude handler type — wrong for a single-queue cap). selectNextQueue already
// skips any queue whose Status != QueueStatusActive without blocking siblings,
// so the per-queue pause is honoured with no new dispatch-gating code.
//
// Resume: a budget-paused queue has NO operator un-pause path
// (transitionToActive only clears paused-by-drain). On the UTC day-rollover this
// meter resets its per-day counters AND transitions any queue currently in
// QueueStatusPausedByBudget back to QueueStatusActive, persists, and Wake()s the
// queue store so the idle workloop re-evaluates dispatch immediately.
//
// Day boundary: UTC midnight, tracked as a YYYY-MM-DD key (shared
// spendMeterTodayKey helper). State resets automatically on the first event
// processed after a day rollover; there is no separate reset goroutine.
//
// Spec ref: specs/queue-model.md (NQ-X1 per-queue spend cap).
// Bead ref: hk-tigaf.11.

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
)

// perQueueCounters holds one queue's per-day spend state. Reset on UTC midnight
// rollover. capUSD is a snapshot of Queue.SpendCapUSD observed at first accrual;
// the live cap is always re-read from the queue store at trip time, so the
// snapshot is informational only.
type perQueueCounters struct {
	spentUSD float64 // accumulated attributed USD this day (bytes / bytesPerUSD)
	paused   bool    // true once this queue was paused-by-budget this day (idempotency guard)
}

// PerQueueSpendMeter tracks per-queue daemon-spawned claude spend (via
// budget_accrual events, attributed back to a queue through the RunRegistry) and
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
	reg        *RunRegistry
	store      *QueueStore
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
// *RunRegistry used to attribute a budget_accrual chunk to its queue; store is
// the QueueStore whose Queue.Status this meter mutates on cap-trip and rollover;
// projectDir is the persist root (empty disables persistence, e.g. in tests).
//
// Bead ref: hk-tigaf.11.
func NewPerQueueSpendMeter(reg *RunRegistry, store *QueueStore, projectDir string) *PerQueueSpendMeter {
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

// handleBudgetAccrual attributes an output_bytes chunk to its queue (via the
// RunRegistry), accumulates the queue's per-day USD spend, and pauses the queue
// (paused-by-budget) when its attributed spend reaches Queue.SpendCapUSD.
//
// Edge cases (see file header): empty QueueName → global-only, skip here; Get
// miss → skip here, no error (lossy-tail OK).
func (m *PerQueueSpendMeter) handleBudgetAccrual(ctx context.Context, evt core.Event) error {
	var payload core.BudgetAccrualPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		// Malformed payload — skip; bus dead-letter path handles persistent failures.
		return nil
	}
	if payload.CostBasis != core.CostBasisOutputBytes {
		return nil // only accumulate output_bytes at this layer (mirrors the global meter)
	}
	if payload.CostUnits <= 0 {
		return nil
	}

	// Attribution: budget_accrual carries no queue name. Resolve it via the run.
	if m.reg == nil {
		return nil
	}
	handle, ok := m.reg.Get(payload.RunID)
	if !ok {
		// Handle Unregister'd before this (late tail) chunk — lossy-tail OK, no error.
		return nil
	}
	queueName := handle.QueueName
	if queueName == "" {
		// br-ready-fallback run with no queue — accrues to the global meter only.
		return nil
	}

	// Read the live per-queue cap from the queue store; SpendCapUSD <= 0 means
	// this queue opted out of a per-queue cap → nothing to do here.
	q := m.store.QueueByName(queueName)
	if q == nil {
		return nil // queue not loaded (completed/cleared) — nothing to pause.
	}
	capUSD := q.SpendCapUSD
	if capUSD <= 0 {
		return nil // no per-queue cap on this queue.
	}

	// Log a cap>global oversubscription warning once per queue: the per-queue cap
	// can never bind ahead of the (lower) global ceiling. Mirrors the Workers
	// oversubscription log in queue/rpc.go.
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
		// Already paused-by-budget this day — idempotent no-op (no double-pause).
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

// pauseQueueByBudget transitions queueName to QueueStatusPausedByBudget and
// persists it. Does NOT route through handler-pause (per-queue scope only).
// selectNextQueue honours the paused status by skipping it without blocking
// siblings, so no dispatch-gating change is needed.
func (m *PerQueueSpendMeter) pauseQueueByBudget(ctx context.Context, queueName string) error {
	lq := m.store.LockForMutation()
	q := lq.LockedQueueByName(queueName)
	if q == nil {
		lq.Done()
		return nil // queue cleared between accrual and pause — nothing to do.
	}
	if q.Status != queue.QueueStatusActive {
		// Only pause an active queue; a queue already paused/completed/cancelled
		// is left as-is (the budget trip is informational once non-active).
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

// rolloverIfNewDayLocked resets per-day counters when the UTC date changes AND
// un-pauses any queue currently in QueueStatusPausedByBudget (the only un-pause
// path for a budget-paused queue — operator-resume only clears paused-by-drain).
// MUST be called while m.mu is held.
func (m *PerQueueSpendMeter) rolloverIfNewDayLocked(ctx context.Context) {
	today := spendMeterTodayKey()
	if today == m.dayKey {
		return
	}
	m.dayKey = today
	m.counters = make(map[string]*perQueueCounters)
	// The oversubscription warning is per-queue-stable across days, so it is NOT
	// reset on rollover (avoids re-logging the same warning every midnight).
	m.unpauseBudgetPausedQueues(ctx)
}

// unpauseBudgetPausedQueues transitions every queue currently in
// QueueStatusPausedByBudget back to QueueStatusActive, persists each, and Wake()s
// the queue store once so the idle workloop re-evaluates dispatch immediately.
// Mirrors transitionToActive's persist + Wake pattern
// (queue_operatoreventconsumer_7urls.go).
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
				// Best-effort: log and continue resuming the remaining queues. The
				// in-memory status is already flipped to active so dispatch resumes;
				// the on-disk file will be re-persisted on the next mutation.
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

// maybeLogOversubscription emits a once-per-queue stderr warning when a queue's
// per-queue SpendCapUSD exceeds the global daemon USD ceiling (so the per-queue
// cap can never bind ahead of the global one). No-op when the global ceiling is
// disabled (globalCapUSD <= 0). Mirrors the Workers oversubscription log.
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

// spendMeterGlobalCapUSD derives the daemon-wide daily USD ceiling from the
// environment, matching DaemonSpendMeter's FLYWHEEL_BUDGET_USD_PER_DAY parsing.
// Returns 0 when the operator opts out via "unlimited" (global USD ceiling
// disabled → per-queue oversubscription detection is skipped).
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
		// Unparseable — fall back to default (fail-closed), matching the global meter.
		return defaultDailyBudgetUSD
	}
}
