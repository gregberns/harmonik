package daemon

// handlerpause_autoresume_0otqs.go — auto-resume on timed backoff (hk-0otqs).
//
// This file adds the Schedule(agentType, after) primitive to HandlerPauseController,
// fulfilling the post-MVH auto-resume surface from specs/handler-pause.md §1.2.
//
// Design:
//   - Schedule registers a timed auto-resume for a paused handler type.
//   - Before resuming, the controller calls Adapter.Diagnose; if Healthy=false
//     (or the adapter is absent), the resume is skipped.
//   - Hysteresis: if the handler gets re-paused quickly after an auto-resume
//     (within autoResumeFlapWindow), the flap counter in handlerEntry increments.
//     Subsequent Schedule calls apply exponential backoff against the caller-
//     supplied `after` duration, capped at AutoResumeConfig.MaxBackoff.
//   - Operator can disable auto-resume per handler type via SetAutoResumeConfig.
//
// Spec ref: specs/handler-pause.md §1.2 (post-MVH auto-resume item).
// Bead ref: hk-0otqs.

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// autoResumeFlapWindow is the duration after an auto-resume during which a
// re-pause is classified as a flap.  If Pause is called within this window
// after an auto-resume, the flap counter is incremented.
//
// 5 minutes is a conservative default: a handler that got paused again within
// 5 minutes of an auto-resume has not recovered.
const autoResumeFlapWindow = 5 * time.Minute

// autoResumeDefaultMaxBackoff is the maximum per-attempt backoff duration when
// AutoResumeConfig.MaxBackoff is zero.
const autoResumeDefaultMaxBackoff = 30 * time.Minute

// AutoResumeConfig configures auto-resume behaviour for a single handler type.
// Set via HandlerPauseController.SetAutoResumeConfig.
type AutoResumeConfig struct {
	// Disabled, when true, makes Schedule a no-op for this handler type.
	// Allows operators to opt out of automatic resumption per handler.
	Disabled bool

	// MaxBackoff is the maximum duration between auto-resume attempts when
	// flapping is detected.  When zero, autoResumeDefaultMaxBackoff (30m) applies.
	MaxBackoff time.Duration
}

// effectiveMaxBackoff returns the effective max-backoff duration, applying the
// default when cfg.MaxBackoff is zero.
func (cfg AutoResumeConfig) effectiveMaxBackoff() time.Duration {
	if cfg.MaxBackoff <= 0 {
		return autoResumeDefaultMaxBackoff
	}
	return cfg.MaxBackoff
}

// ---------------------------------------------------------------------------
// Schedule — register a timed auto-resume attempt
// ---------------------------------------------------------------------------

// Schedule registers an auto-resume attempt for agentType after the given
// duration.  When the timer fires the controller:
//
//  1. Verifies the handler is still paused with the same epoch (guard against
//     a superseding manual resume or a newer pause).
//  2. Calls Adapter.Diagnose.  If Healthy=false the attempt is abandoned.
//     If the adapter is absent or returns ErrDeterministic, the check is
//     skipped and the resume proceeds.
//  3. Calls Resume(ctx, agentType, HandlerResumedByAutoBackoff).
//
// Hysteresis: if the handler was recently re-paused after a prior auto-resume
// (within autoResumeFlapWindow), the effective delay is doubled for each
// consecutive flap, capped at AutoResumeConfig.MaxBackoff.
//
// Schedule is a no-op when:
//   - AutoResumeConfig.Disabled is true for agentType.
//   - agentType is not currently paused (guard: caller should only call Schedule
//     immediately after Pause; the timer guard in doAutoResume handles the
//     epoch-mismatch case robustly).
//
// The provided ctx governs the goroutine's lifetime.  Callers SHOULD pass the
// daemon's lifetime context so the goroutine exits when the daemon stops.
//
// Safe for concurrent use.
func (c *HandlerPauseController) Schedule(ctx context.Context, agentType core.AgentType, after time.Duration) {
	if !agentType.Valid() {
		return
	}
	if after <= 0 {
		return
	}

	c.mu.Lock()

	cfg := c.autoResumeCfgLocked(agentType)
	if cfg.Disabled {
		c.mu.Unlock()
		return
	}

	entry := c.getOrCreate(agentType)
	if entry.status != pauseStatusPaused {
		// Handler is not paused; nothing to schedule.
		c.mu.Unlock()
		return
	}

	// Apply exponential backoff for flapping handlers.
	effective := c.backoffDurationLocked(after, entry.autoResumeAttempts, cfg)

	// Cancel any existing pending auto-resume for this agent type.
	if entry.scheduledResumeCancel != nil {
		entry.scheduledResumeCancel()
		entry.scheduledResumeCancel = nil
	}

	pausedEpoch := entry.pausedEpoch

	resumeCtx, cancel := context.WithCancel(ctx)
	entry.scheduledResumeCancel = cancel

	c.mu.Unlock()

	go func() {
		select {
		case <-time.After(effective):
			c.doAutoResume(resumeCtx, agentType, pausedEpoch)
		case <-resumeCtx.Done():
			// Cancelled: a new pause, a superseding Schedule, or operator Resume
			// has cleared this timer.
		}
	}()
}

// backoffDurationLocked computes the effective backoff duration given the base
// `after`, the number of consecutive flap attempts, and the config.
//
// Formula: effective = after * 2^attempts, capped at cfg.MaxBackoff.
// MUST be called while mu is held (reads attempts from entry, which is mutable).
func (c *HandlerPauseController) backoffDurationLocked(after time.Duration, attempts int, cfg AutoResumeConfig) time.Duration {
	if attempts <= 0 {
		return after
	}
	maxBackoff := cfg.effectiveMaxBackoff()
	// Shift left by attempts, but cap to avoid overflow.
	shifted := after
	for i := 0; i < attempts && shifted < maxBackoff; i++ {
		next := shifted * 2
		if next <= 0 || next > maxBackoff { // overflow guard
			shifted = maxBackoff
			break
		}
		shifted = next
	}
	if shifted > maxBackoff {
		shifted = maxBackoff
	}
	return shifted
}

// ---------------------------------------------------------------------------
// doAutoResume — the timed-backoff resume logic
// ---------------------------------------------------------------------------

// doAutoResume is the goroutine body invoked after the Schedule timer fires.
//
// Steps:
//  1. Re-acquire the lock; confirm agentType is still paused with the same
//     epoch (guard against a superseding manual resume or a newer pause).
//  2. Release the lock; call Adapter.Diagnose.
//  3. If Diagnose returns Healthy=false, abandon the attempt.
//  4. Re-acquire lock; confirm epoch is still valid (Diagnose can block).
//  5. Record lastAutoResumedAt and clear scheduledResumeCancel.
//  6. Call Resume(ctx, agentType, HandlerResumedByAutoBackoff).
func (c *HandlerPauseController) doAutoResume(ctx context.Context, agentType core.AgentType, pausedEpoch int) {
	// Step 1: epoch guard under read lock.
	c.mu.RLock()
	entry, exists := c.handlers[agentType]
	if !exists || entry.status != pauseStatusPaused || entry.pausedEpoch != pausedEpoch {
		c.mu.RUnlock()
		return // superseded
	}
	c.mu.RUnlock()

	// Step 2: call Diagnose (may block on I/O; do not hold mu).
	if report, ok := c.runDiagnose(ctx); ok && !report.Healthy {
		// Step 3: adapter says condition is not cleared; abandon.
		return
	}

	// Step 4: re-confirm epoch under write lock before mutating state.
	c.mu.Lock()
	entry, exists = c.handlers[agentType]
	if !exists || entry.status != pauseStatusPaused || entry.pausedEpoch != pausedEpoch {
		c.mu.Unlock()
		return // superseded between Diagnose and lock re-acquisition
	}

	// Step 5: record auto-resume time (for flap detection in next Pause call)
	// and clear the cancel func so Resume's cleanup doesn't double-call it.
	entry.lastAutoResumedAt = time.Now()
	entry.scheduledResumeCancel = nil // the goroutine IS the cancel target; clear it

	c.mu.Unlock()

	// Step 6: call Resume.  Resume acquires mu internally, so we must not hold
	// it here.  HandlerResumedByAutoBackoff is the initiator discriminator.
	_ = c.Resume(ctx, agentType, core.HandlerResumedByAutoBackoff)
}
