package queuewiring

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
	// Used as the base path for QueueStore.Transact (QM-001).
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
			Types: map[core.EventType]struct{}{
				core.EventTypeOperatorPauseStatus: {},
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
			Types: map[core.EventType]struct{}{
				core.EventTypeOperatorResuming: {},
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

func (c *QueueOperatorEventConsumer) handleOperatorPauseStatus(ctx context.Context, evt core.Event) error {
	var payload core.OperatorPauseStatusPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("queue-operator-drain: pause: unmarshal: %w", err)
	}
	if !payload.Valid() {
		return nil // silently skip invalid payloads
	}

	switch payload.Status {
	case core.OperatorPauseStatusValuePausing, core.OperatorPauseStatusValuePaused:
		return c.transitionToPausedByDrain(ctx, payload.QueueName)
	}
	return nil
}

func (c *QueueOperatorEventConsumer) handleOperatorResuming(ctx context.Context, evt core.Event) error {
	var payload core.OperatorResumingPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("queue-operator-drain: resume: unmarshal: %w", err)
	}
	return c.transitionToActive(ctx, payload.QueueName)
}

func (c *QueueOperatorEventConsumer) transitionToPausedByDrain(ctx context.Context, queueName string) error {
	for _, name := range c.matchedQueueNames(queueName) {
		q, transitioned, err := c.transitionQueue(ctx, name, queue.OperationPause, false, queue.QueueStatusActive, queue.PauseQueueForDrain)
		if err != nil {
			return fmt.Errorf("queue-operator-drain: pause[%s]: %w", name, err)
		}
		if !transitioned {
			continue
		}

		activeGroupIndex := 0
		for _, g := range q.Groups {
			if g.Status == queue.GroupStatusActive {
				activeGroupIndex = g.GroupIndex
				break
			}
		}

		pausedPayload := core.QueuePausedPayload{
			QueueID:    q.QueueID,
			GroupIndex: activeGroupIndex,
			FailCount:  0, // operator-drain: no failures contributed
			PausedAt:   time.Now().UTC().Format(time.RFC3339),
			Reason:     "operator_drain",
		}
		payloadBytes, err := json.Marshal(pausedPayload)
		if err != nil {
			return fmt.Errorf("queue-operator-drain: pause[%s]: marshal queue_paused payload: %w", name, err)
		}
		if emitErr := c.cfg.Bus.Emit(ctx, core.EventTypeQueuePaused, payloadBytes); emitErr != nil {
			return fmt.Errorf("queue-operator-drain: pause[%s]: emit queue_paused: %w", name, emitErr)
		}
	}

	return nil
}

func (c *QueueOperatorEventConsumer) transitionToActive(ctx context.Context, queueName string) error {
	for _, name := range c.matchedQueueNames(queueName) {
		_, _, err := c.transitionQueue(ctx, name, queue.OperationResume, true, queue.QueueStatusPausedByDrain, queue.ResumeQueueFromDrain)
		if err != nil {
			return fmt.Errorf("queue-operator-drain: resume[%s]: %w", name, err)
		}
	}

	return nil
}

func (c *QueueOperatorEventConsumer) matchedQueueNames(queueName string) []string {
	if queueName != "" {
		return []string{queue.NormaliseQueueName(queueName)}
	}
	queues := c.cfg.QueueStore.AllQueues()
	names := make([]string, 0, len(queues))
	for name := range queues {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *QueueOperatorEventConsumer) transitionQueue(
	ctx context.Context,
	name string,
	kind queue.OperationKind,
	wake bool,
	expected queue.QueueStatus,
	mutate func(*queue.Queue) error,
) (*queue.Queue, bool, error) {
	if c.cfg.ProjectDir == "" {
		locked := c.cfg.QueueStore.LockForMutation()
		live := locked.LockedQueueByName(name)
		if live == nil || live.Status != expected {
			locked.Done()
			return nil, false, nil
		}
		candidate := queue.CloneQueue(live)
		if err := mutate(candidate); err != nil {
			locked.Done()
			return nil, false, err
		}
		locked.LockedSetQueueByName(name, candidate)
		locked.Done()
		if wake {
			c.cfg.QueueStore.Wake()
		}
		return candidate, true, nil
	}

	snapshot := c.cfg.QueueStore.Snapshot(name)
	if snapshot.Queue == nil || snapshot.Queue.Status != expected {
		return nil, false, nil
	}
	result := c.cfg.QueueStore.Transact(ctx, queue.TransactionRequest{
		Snapshot:      snapshot,
		ProjectDir:    c.cfg.ProjectDir,
		OperationKind: kind,
		WakeRequired:  wake,
		Mutate:        mutate,
	})
	if !result.Committed() {
		return nil, false, result.Err
	}
	if result.CleanupErr != nil {
		return nil, false, result.CleanupErr
	}
	return result.Snapshot.Queue, true, nil
}
