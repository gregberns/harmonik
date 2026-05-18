package daemon

// queue_operatoreventconsumer_7urls.go — queue operator-event consumer (hk-7urls).
//
// QueueOperatorEventConsumer subscribes to daemon operator lifecycle events and
// drives queue-level active ↔ paused-by-drain transitions per
// specs/queue-model.md §8.5 QM-054 and §8.6 QM-055.
//
// Subscribed events:
//
//   - operator_pause_status (§8.7.6) — transitions queue active → paused-by-drain
//     on both the pausing and paused status values.  Idempotent when the queue is
//     already paused-by-drain.
//   - operator_resuming (§8.7.7) — transitions queue paused-by-drain → active.
//     Idempotent when the queue is already active or absent.
//
// On entry to paused-by-drain the consumer:
//  1. Transitions Queue.Status from active → paused-by-drain.
//  2. Persists via queue.Persist (QM-001) — persist-before-emit per QM-063.
//  3. Emits queue_paused{reason: "operator_drain"} (QM-054 step 2).
//
// QM-055 — persisted pause survives restart: the persistence step above writes
// paused-by-drain to queue.json; queue.Load (QM-002 startup path) preserves the
// status unchanged.  No additional startup logic is required.
//
// Architecture placement: internal/daemon/ — the consumer needs QueueStore and
// the event bus, both of which are daemon composition-root concerns (same
// reasoning as HandlerPausePolicyGoroutine).
//
// Spec ref: specs/queue-model.md §8.5 QM-054, §8.6 QM-055.
// Bead ref: hk-7urls.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
)

// QueueOperatorEventConsumerConfig carries the parameters for
// NewQueueOperatorEventConsumer.
type QueueOperatorEventConsumerConfig struct {
	// QueueStore is the daemon-singleton queue store. Required; must not be nil.
	QueueStore *QueueStore

	// ProjectDir is the harmonik project directory (e.g. "/path/to/project").
	// Used as the base path for queue.Persist (QM-001).
	// When empty the consumer still transitions in-memory state but skips
	// the persist step (unit-test mode without a filesystem).
	ProjectDir string

	// Bus is the event bus used to emit queue_paused events.
	// Required; must not be nil.
	Bus eventbus.EventBus
}

// QueueOperatorEventConsumer watches operator lifecycle events and drives
// queue-level active ↔ paused-by-drain transitions.
//
// Lifecycle: call Subscribe(bus) before bus.Seal to wire the event consumers.
// The consumer uses the bus's asynchronous delivery; no separate goroutine is
// required.
type QueueOperatorEventConsumer struct {
	cfg QueueOperatorEventConsumerConfig
}

// NewQueueOperatorEventConsumer creates a new QueueOperatorEventConsumer.
// Subscribe must be called before bus.Seal (EV-009).
func NewQueueOperatorEventConsumer(cfg QueueOperatorEventConsumerConfig) *QueueOperatorEventConsumer {
	return &QueueOperatorEventConsumer{cfg: cfg}
}

// Subscribe registers the consumer's event handlers with the bus.
//
// Must be called before bus.Seal (EV-009). Registers two asynchronous consumers:
//
//   - operator_pause_status — drives active → paused-by-drain
//   - operator_resuming     — drives paused-by-drain → active
func (c *QueueOperatorEventConsumer) Subscribe(bus eventbus.EventBus) error {
	pauseSub := core.Subscription{
		ConsumerID:    "queue-operator-drain-pause",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[string]struct{}{
				string(core.EventTypeOperatorPauseStatus): {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: c.handleOperatorPauseStatus,
	}
	if _, err := bus.Subscribe(pauseSub); err != nil {
		return fmt.Errorf("QueueOperatorEventConsumer.Subscribe: pause consumer: %w", err)
	}

	resumeSub := core.Subscription{
		ConsumerID:    "queue-operator-drain-resume",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[string]struct{}{
				string(core.EventTypeOperatorResuming): {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: c.handleOperatorResuming,
	}
	if _, err := bus.Subscribe(resumeSub); err != nil {
		return fmt.Errorf("QueueOperatorEventConsumer.Subscribe: resume consumer: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// handleOperatorPauseStatus — active → paused-by-drain on pause events
// ---------------------------------------------------------------------------

// handleOperatorPauseStatus processes operator_pause_status events and
// transitions the queue from active → paused-by-drain (QM-054).
//
// Both "pausing" and "paused" status values trigger the transition.  The
// transition is idempotent: if the queue is already paused-by-drain, this is
// a no-op (duplicate event safety).
func (c *QueueOperatorEventConsumer) handleOperatorPauseStatus(ctx context.Context, evt core.Event) error {
	var payload core.OperatorPauseStatusPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("queue-operator-drain: pause: unmarshal: %w", err)
	}
	if !payload.Valid() {
		return nil // silently skip invalid payloads
	}

	// Both "pausing" and "paused" drive the transition: QM-054 says "when the
	// daemon enters operator-pause" — the pausing phase is the entry point, but
	// subscribing to both is idempotent and defends against missed events.
	switch payload.Status {
	case core.OperatorPauseStatusValuePausing, core.OperatorPauseStatusValuePaused:
		return c.transitionToPausedByDrain(ctx)
	}
	return nil
}

// ---------------------------------------------------------------------------
// handleOperatorResuming — paused-by-drain → active on resume events
// ---------------------------------------------------------------------------

// handleOperatorResuming processes operator_resuming events and transitions the
// queue from paused-by-drain → active.
//
// Idempotent: if the queue is already active or absent, this is a no-op.
func (c *QueueOperatorEventConsumer) handleOperatorResuming(ctx context.Context, _ core.Event) error {
	return c.transitionToActive(ctx)
}

// ---------------------------------------------------------------------------
// transition helpers
// ---------------------------------------------------------------------------

// transitionToPausedByDrain transitions the in-memory queue from active →
// paused-by-drain, persists, and emits queue_paused{reason: "operator_drain"}.
//
// Per QM-054 steps:
//  1. Transition Queue.Status from active → paused-by-drain.
//  2. Persist via QM-001 (queue.Persist). Persist-before-emit per QM-063.
//  3. Emit queue_paused{reason: "operator_drain"}.
//
// No-op when no queue is loaded or queue is already paused-by-drain.
func (c *QueueOperatorEventConsumer) transitionToPausedByDrain(ctx context.Context) error {
	lq := c.cfg.QueueStore.LockForMutation()
	defer lq.Done()

	q := lq.Queue()
	if q == nil {
		return nil // no active queue — nothing to pause
	}
	if q.Status != queue.QueueStatusActive {
		return nil // already paused or completed — idempotent no-op
	}

	q.Status = queue.QueueStatusPausedByDrain
	lq.SetQueue(q)

	// QM-063: persist BEFORE emitting the queue_paused event.
	if c.cfg.ProjectDir != "" {
		if err := queue.Persist(ctx, c.cfg.ProjectDir, q); err != nil {
			return fmt.Errorf("queue-operator-drain: pause: persist: %w", err)
		}
	}

	// Find the currently active group index for the queue_paused payload (QM-054
	// step 2). Use the first group with an active status if present; fall back to
	// the last group index when no group is currently advancing (e.g. all pending
	// in a multi-wave queue).
	activeGroupIndex := 0
	for _, g := range q.Groups {
		if g.Status == queue.GroupStatusActive {
			activeGroupIndex = g.GroupIndex
			break
		}
	}

	// Emit queue_paused{reason: "operator_drain"} per QM-054.
	pausedPayload := core.QueuePausedPayload{
		QueueID:    q.QueueID,
		GroupIndex: activeGroupIndex,
		FailCount:  0, // operator-drain: no failures contributed
		PausedAt:   time.Now().UTC().Format(time.RFC3339),
		Reason:     "operator_drain",
	}
	payloadBytes, err := json.Marshal(pausedPayload)
	if err != nil {
		return fmt.Errorf("queue-operator-drain: pause: marshal queue_paused payload: %w", err)
	}
	if emitErr := c.cfg.Bus.Emit(ctx, core.EventTypeQueuePaused, payloadBytes); emitErr != nil {
		return fmt.Errorf("queue-operator-drain: pause: emit queue_paused: %w", emitErr)
	}

	return nil
}

// transitionToActive transitions the in-memory queue from paused-by-drain →
// active and persists.
//
// No-op when no queue is loaded or queue is not paused-by-drain.
// The spec does not define a queue-level resume event; the transition is
// observable only through queue-status responses.
func (c *QueueOperatorEventConsumer) transitionToActive(ctx context.Context) error {
	lq := c.cfg.QueueStore.LockForMutation()
	defer lq.Done()

	q := lq.Queue()
	if q == nil {
		return nil // no active queue — nothing to resume
	}
	if q.Status != queue.QueueStatusPausedByDrain {
		return nil // not paused-by-drain — idempotent no-op
	}

	q.Status = queue.QueueStatusActive
	lq.SetQueue(q)

	// Persist the resumed status.
	if c.cfg.ProjectDir != "" {
		if err := queue.Persist(ctx, c.cfg.ProjectDir, q); err != nil {
			return fmt.Errorf("queue-operator-drain: resume: persist: %w", err)
		}
	}

	return nil
}
