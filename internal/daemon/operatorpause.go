package daemon

// operatorpause.go — OperatorPauseController (hk-ry8q1).
//
// OperatorPauseController implements OperatorControlHandler for the socket
// dispatcher and exposes IsPaused for the workloop br-ready dispatch gate.
//
// Lifecycle:
//   - HandleOperatorPause: emits operator_pause_status{pausing} →
//     sets paused=true → emits operator_pause_status{paused}.
//     The QueueOperatorEventConsumer reacts to operator_pause_status to
//     transition the active queue active → paused-by-drain (QM-054).
//   - HandleOperatorResume: clears paused=false → emits operator_resuming.
//     The QueueOperatorEventConsumer reacts to operator_resuming to
//     transition paused-by-drain → active.
//   - IsPaused: consulted by the workloop br-ready gate before every dispatch.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-007–ON-010.
// Spec ref: specs/event-model.md §8.7.6 (operator_pause_status), §8.7.7 (operator_resuming).
// Bead ref: hk-ry8q1.

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
type OperatorControlHandler interface {
	// HandleOperatorPause initiates an operator pause: sets paused state and
	// emits operator_pause_status events (event-model.md §8.7.6).
	HandleOperatorPause(ctx context.Context) error

	// HandleOperatorResume clears operator pause: resets state and emits
	// operator_resuming (event-model.md §8.7.7).
	HandleOperatorResume(ctx context.Context) error
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
// Emits operator_pause_status{status:"pausing"} to begin the pause sequence
// (which triggers QueueOperatorEventConsumer to transition the active queue
// active → paused-by-drain per QM-054), sets the internal paused flag, then
// emits operator_pause_status{status:"paused"} to complete the paired-phase
// lifecycle per §8.9(h). Idempotent: no-op when already paused. Concurrent
// calls are serialised by mu so only one wins; others are silent no-ops.
//
// Spec ref: specs/event-model.md §8.7.6; specs/operator-nfr.md §4.3 ON-007–ON-010.
func (c *OperatorPauseController) HandleOperatorPause(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.paused {
		return nil // already paused — idempotent
	}

	ts := msTimestamp()
	if err := c.emitPauseStatusLocked(ctx, core.OperatorPauseStatusValuePausing, ts); err != nil {
		return fmt.Errorf("operator-pause: emit pausing: %w", err)
	}

	c.paused = true

	ts = msTimestamp()
	if err := c.emitPauseStatusLocked(ctx, core.OperatorPauseStatusValuePaused, ts); err != nil {
		return fmt.Errorf("operator-pause: emit paused: %w", err)
	}

	return nil
}

// HandleOperatorResume implements OperatorControlHandler.
//
// Clears the paused flag and emits operator_resuming, which triggers the
// QueueOperatorEventConsumer to transition the active queue
// paused-by-drain → active. Idempotent: no-op when not paused. Concurrent
// calls are serialised by mu so only one wins; others are silent no-ops.
//
// Spec ref: specs/event-model.md §8.7.7; specs/operator-nfr.md §4.3.
func (c *OperatorPauseController) HandleOperatorResume(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.paused {
		return nil // not paused — idempotent
	}

	c.paused = false

	payload := core.OperatorResumingPayload{
		ResumedAt: msTimestamp(),
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
// status and changedAt timestamp. Caller must hold mu.
func (c *OperatorPauseController) emitPauseStatusLocked(ctx context.Context, status core.OperatorPauseStatusValue, changedAt string) error {
	payload := core.OperatorPauseStatusPayload{
		Status:    status,
		ChangedAt: changedAt,
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
