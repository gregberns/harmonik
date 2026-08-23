package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/policy"
)

const autoResumeFlapWindow = 5 * time.Minute

// AutoResumeConfig configures auto-resume behaviour for a single handler type.
// Set via HandlerPauseController.SetAutoResumeConfig.
//
// This type stays in the daemon because it is entangled with the controller: it
// is stored per handler type (autoResumeCfgs map) and carries the operator's
// Disabled opt-out.  Only the pure backoff arithmetic it feeds lives in
// internal/policy (policy.BackoffDuration); the zero-MaxBackoff default is
// policy.DefaultAutoResumeMaxBackoff (30m).
type AutoResumeConfig struct {
	// Disabled, when true, makes Schedule a no-op for this handler type.
	// Allows operators to opt out of automatic resumption per handler.
	Disabled bool

	// MaxBackoff is the maximum duration between auto-resume attempts when
	// flapping is detected.  When zero, policy.DefaultAutoResumeMaxBackoff (30m)
	// applies.
	MaxBackoff time.Duration
}

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
		c.mu.Unlock()
		return
	}

	effective := c.backoffDurationLocked(after, entry.autoResumeAttempts, cfg)

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
		}
	}()
}

func (c *HandlerPauseController) backoffDurationLocked(after time.Duration, attempts int, cfg AutoResumeConfig) time.Duration {
	return policy.BackoffDuration(policy.AutoResumeParams{
		Base:       after,
		Attempts:   attempts,
		MaxBackoff: cfg.MaxBackoff,
	})
}

func (c *HandlerPauseController) doAutoResume(ctx context.Context, agentType core.AgentType, pausedEpoch int) {
	c.mu.RLock()
	entry, exists := c.handlers[agentType]
	if !exists || entry.status != pauseStatusPaused || entry.pausedEpoch != pausedEpoch {
		c.mu.RUnlock()
		return // superseded
	}
	c.mu.RUnlock()

	if report, ok := c.runDiagnose(ctx); ok && !report.Healthy {
		return
	}

	c.mu.Lock()
	entry, exists = c.handlers[agentType]
	if !exists || entry.status != pauseStatusPaused || entry.pausedEpoch != pausedEpoch {
		c.mu.Unlock()
		return // superseded between Diagnose and lock re-acquisition
	}

	entry.lastAutoResumedAt = time.Now()
	entry.scheduledResumeCancel = nil // the goroutine IS the cancel target; clear it

	c.mu.Unlock()

	if resumeErr := c.Resume(ctx, agentType, core.HandlerResumedByAutoBackoff); resumeErr != nil {
		slog.WarnContext(ctx, "daemon: auto-resume of paused handler failed", "err", resumeErr, "agent_type", string(agentType))
	}
}
