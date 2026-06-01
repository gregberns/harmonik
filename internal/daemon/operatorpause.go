package daemon

// operatorpause.go — OperatorPauseController (hk-ry8q1, extended hk-tigaf.6).
//
// OperatorPauseController implements OperatorControlHandler for the socket
// dispatcher and exposes IsPaused for the workloop br-ready dispatch gate.
//
// Lifecycle:
//   - HandleOperatorPause(ctx, ""): global pause — emits operator_pause_status{pausing}
//     (no queue_name) → sets paused=true → emits operator_pause_status{paused}.
//     The QueueOperatorEventConsumer reacts to drain ALL queues (QM-054).
//   - HandleOperatorPause(ctx, queueName): per-queue pause — emits
//     operator_pause_status{pausing, queue_name=queueName} without touching the
//     global paused flag. Only the named queue is transitioned by the consumer.
//   - HandleOperatorResume(ctx, ""): global resume — clears paused=false → emits
//     operator_resuming (no queue_name). Consumer resumes ALL paused-by-drain queues.
//   - HandleOperatorResume(ctx, queueName): per-queue resume — emits
//     operator_resuming{queue_name=queueName} without touching the global paused flag.
//   - IsPaused: consulted by the workloop br-ready gate before every dispatch.
//     Returns true ONLY for the global pause; per-queue pauses do not affect it.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-007–ON-010.
// Spec ref: specs/event-model.md §8.7.6 (operator_pause_status), §8.7.7 (operator_resuming).
// Bead ref: hk-ry8q1, hk-tigaf.6.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
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
}

// NewOperatorPauseController constructs an OperatorPauseController wired to bus.
// bus must be non-nil and Sealed before HandleOperatorPause/HandleOperatorResume
// are called (EV-009).
func NewOperatorPauseController(bus handlercontract.EventEmitter) *OperatorPauseController {
	return &OperatorPauseController{bus: bus}
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

	if queueName == "" {
		// Global pause: gate with the paused flag for idempotency.
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
		// Per-queue pause: does not touch the global paused flag.
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

	if queueName == "" {
		// Global resume: gate with paused flag for idempotency.
		if !c.paused {
			return nil // not paused — idempotent
		}
		c.paused = false
	}
	// Per-queue resume: no global flag change; always emit (idempotency is in consumer).

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

// emitPauseStatusLocked emits an operator_pause_status event with the given
// status, changedAt timestamp, and optional queueName scope. Caller must hold mu.
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

// msTimestamp returns the current UTC time formatted as RFC3339 with
// millisecond resolution per event-model.md §8.9(h).
func msTimestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
