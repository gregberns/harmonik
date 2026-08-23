package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/queue"
)

// OperatorControlHandler is the interface for handling operator pause/resume
// requests received from the daemon's Unix socket.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-007–ON-010.
// Bead ref: hk-tigaf.6 (queueName parameter for per-queue scoping).
type OperatorControlHandler interface {
	// HandleOperatorPause initiates an operator pause. queueName scopes the
	// pause to a single named queue; empty queueName is a global pause that
	// affects all queues and sets the EM-067 br-ready gate.
	//
	// Emits operator_pause_status events (event-model.md §8.7.6).
	HandleOperatorPause(ctx context.Context, queueName string) error

	// HandleOperatorResume clears an operator pause. queueName scopes the
	// resume to a single named queue; empty queueName is a global resume.
	//
	// Emits operator_resuming (event-model.md §8.7.7).
	HandleOperatorResume(ctx context.Context, queueName string) error
}

// OperatorPauseController tracks daemon operator-pause state and emits the
// corresponding lifecycle events on the event bus.
//
// Concurrent-safe: IsPaused, HandleOperatorPause, and HandleOperatorResume
// may be called from different goroutines (socket handler goroutines vs the
// workloop poll goroutine). mu serialises the Load→emit→Store sequence in
// HandleOperatorPause and HandleOperatorResume so that concurrent calls cannot
// double-emit events.
type OperatorPauseController struct {
	mu     sync.Mutex
	paused bool
	bus    handlercontract.EventEmitter

	// verdicts is the RC-027 operator verdict-override rendezvous. It parks
	// reconciliation runs whose policy sets confirm_required: true and delivers
	// the operator's confirm/veto decision (routed via HandleVerdictOverride).
	// See verdictoverride.go.
	verdicts *VerdictConfirmationRegistry

	// queues reads live queue status so a per-queue resume can refuse a
	// failure-parked queue instead of reporting a success that dispatches
	// nothing. Nil until SetQueueStates is called.
	queues QueuePauseStateReader
}

// NewOperatorPauseController constructs an OperatorPauseController wired to bus.
// bus must be non-nil and Sealed before HandleOperatorPause/HandleOperatorResume
// are called (EV-009).
func NewOperatorPauseController(bus handlercontract.EventEmitter) *OperatorPauseController {
	return &OperatorPauseController{bus: bus, verdicts: NewVerdictConfirmationRegistry()}
}

// IsPaused reports whether the daemon is currently in an operator-pause state.
// Safe for concurrent access.
func (c *OperatorPauseController) IsPaused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

// HandleOperatorPause implements OperatorControlHandler.
//
// When queueName is empty (global pause): emits operator_pause_status{pausing}
// with no queue_name, sets the internal paused flag (EM-067 br-ready gate), then
// emits operator_pause_status{paused}. Triggers QueueOperatorEventConsumer to
// transition ALL active queues to paused-by-drain (QM-054). Idempotent when
// already globally paused.
//
// When queueName is non-empty (per-queue pause): emits operator_pause_status
// events scoped to that queue name WITHOUT setting the global paused flag. The
// br-ready gate is unaffected; only the named queue is drained by the consumer.
//
// Concurrent calls for the same scope are serialised by mu.
//
// Spec ref: specs/event-model.md §8.7.6; specs/operator-nfr.md §4.3 ON-007–ON-010.
// Bead ref: hk-tigaf.6.
func (c *OperatorPauseController) HandleOperatorPause(ctx context.Context, queueName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.refuseUnknownQueueLocked("pause", queueName); err != nil {
		return err
	}

	if queueName == "" {
		if c.paused {
			return nil // already paused — idempotent
		}

		ts := msTimestamp()
		if err := c.emitPauseStatusLocked(ctx, core.OperatorPauseStatusValuePausing, ts, ""); err != nil {
			return fmt.Errorf("operator-pause: emit pausing: %w", err)
		}

		c.paused = true

		ts = msTimestamp()
		if err := c.emitPauseStatusLocked(ctx, core.OperatorPauseStatusValuePaused, ts, ""); err != nil {
			return fmt.Errorf("operator-pause: emit paused: %w", err)
		}
	} else {
		ts := msTimestamp()
		if err := c.emitPauseStatusLocked(ctx, core.OperatorPauseStatusValuePausing, ts, queueName); err != nil {
			return fmt.Errorf("operator-pause[%s]: emit pausing: %w", queueName, err)
		}
		ts = msTimestamp()
		if err := c.emitPauseStatusLocked(ctx, core.OperatorPauseStatusValuePaused, ts, queueName); err != nil {
			return fmt.Errorf("operator-pause[%s]: emit paused: %w", queueName, err)
		}
	}

	return nil
}

// HandleOperatorResume implements OperatorControlHandler.
//
// When queueName is empty (global resume): clears the paused flag and emits
// operator_resuming with no queue_name. Triggers QueueOperatorEventConsumer to
// transition ALL paused-by-drain queues back to active. Idempotent: no-op when
// not globally paused.
//
// When queueName is non-empty (per-queue resume): emits operator_resuming scoped
// to that queue name WITHOUT touching the global paused flag.
//
// Concurrent calls are serialised by mu.
//
// Spec ref: specs/event-model.md §8.7.7; specs/operator-nfr.md §4.3.
// Bead ref: hk-tigaf.6.
func (c *OperatorPauseController) HandleOperatorResume(ctx context.Context, queueName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.refuseUnknownQueueLocked("resume", queueName); err != nil {
		return err
	}

	if err := c.refuseFailureParkedLocked(queueName); err != nil {
		return err
	}

	if queueName == "" {
		if !c.paused {
			return nil // not paused — idempotent
		}
		c.paused = false
	}

	payload := core.OperatorResumingPayload{
		ResumedAt: msTimestamp(),
		QueueName: queueName,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("operator-resume: marshal: %w", err)
	}
	if emitErr := c.bus.Emit(ctx, core.EventTypeOperatorResuming, raw); emitErr != nil {
		return fmt.Errorf("operator-resume: emit: %w", emitErr)
	}

	return nil
}

// QueuePauseStateReader reports the live status of one named queue. It is the
// consumer-owned slice of the queue registry that operator-resume needs to tell
// a drain pause from a failure pause. *queuewiring.QueueStore satisfies it.
type QueuePauseStateReader interface {
	QueueByName(name string) *queue.Queue
}

// SetQueueStates gives the controller a way to read queue status. Without it,
// operator-resume cannot see a failure-parked queue and falls back to the
// pre-existing behaviour, which reports success and dispatches nothing.
//
// Call it once at the composition root, before Serve.
func (c *OperatorPauseController) SetQueueStates(reader QueuePauseStateReader) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queues = reader
}

func (c *OperatorPauseController) refuseFailureParkedLocked(queueName string) error {
	if queueName == "" || c.queues == nil {
		return nil
	}
	q := c.queues.QueueByName(queueName)
	if q == nil || q.Status != queue.QueueStatusPausedByFailure {
		return nil
	}
	return &queue.ResumeRefusedError{
		NormalizedName: queue.NormaliseQueueName(queueName),
		QueueID:        q.QueueID,
		ObservedStatus: q.Status,
	}
}

type queueNameLister interface {
	AllQueues() map[string]*queue.Queue
}

func (c *OperatorPauseController) refuseUnknownQueueLocked(verb, queueName string) error {
	if queueName == "" || c.queues == nil {
		return nil
	}
	normalized := queue.NormaliseQueueName(queueName)
	if c.queues.QueueByName(normalized) != nil {
		return nil
	}
	return &queue.UnknownQueueError{
		Verb:           verb,
		NormalizedName: normalized,
		KnownNames:     c.knownQueueNamesLocked(),
	}
}

func (c *OperatorPauseController) knownQueueNamesLocked() []string {
	lister, ok := c.queues.(queueNameLister)
	if !ok {
		return nil
	}
	all := lister.AllQueues()
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *OperatorPauseController) emitPauseStatusLocked(ctx context.Context, status core.OperatorPauseStatusValue, changedAt, queueName string) error {
	payload := core.OperatorPauseStatusPayload{
		Status:    status,
		ChangedAt: changedAt,
		QueueName: queueName,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return c.bus.Emit(ctx, core.EventTypeOperatorPauseStatus, raw)
}

func msTimestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
