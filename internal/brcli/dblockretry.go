package brcli

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DBLockedRetryMax is the BI-025c default maximum number of retry attempts
// when `br` returns BrDbLocked (exit code 3 — SQLite busy).
//
// Spec ref: specs/beads-integration.md §4.8a BI-025c (step 4c: "Retry up to 3 times").
const DBLockedRetryMax = 3

// DBLockedRetryBase is the BI-025c initial backoff duration for BrDbLocked
// retries (exponential; doubles each attempt, capped at DBLockedRetryCap).
//
// Spec ref: specs/beads-integration.md §4.8a BI-025c (step 4c: "initial 100ms").
const DBLockedRetryBase = 100 * time.Millisecond

// DBLockedRetryCap is the BI-025c maximum backoff duration for a single
// BrDbLocked retry sleep.
//
// Spec ref: specs/beads-integration.md §4.8a BI-025c (step 4c: "max 1s").
const DBLockedRetryCap = 1 * time.Second

// RunWithDBLockedRetry invokes RunWithTimeout and, on a BrDbLocked result
// (exit code 3 — SQLite WAL write contention), retries up to maxRetries
// times with exponential backoff starting at base and capped at cap_.
//
// This is the BI-025e concurrent-invocation retry path: the adapter MAY
// invoke `br` concurrently; SQLite WAL serializes writes; no adapter-side
// mutex is used. When a concurrent write produces BrDbLocked, this wrapper
// implements the BI-025c retry policy (step 4c).
//
// On every retry the full RunWithTimeout discipline applies: per-invocation
// wall-clock timeout, SIGTERM-then-SIGKILL termination per HC-018, and
// BrUnavailable classification on timeout.
//
// After maxRetries consecutive BrDbLocked results the call is escalated to
// BrUnavailable and an error wrapping BrUnavailable is returned, signalling
// that the infrastructure is persistently unavailable and the daemon should
// route to Cat 0 recovery per BI §8.
//
// If ctx is canceled at any point during a sleep or a RunWithTimeout call,
// RunWithDBLockedRetry returns immediately with the context error.
//
// Callers MUST pass the BI-025c defaults (DBLockedRetryMax, DBLockedRetryBase,
// DBLockedRetryCap) unless operator configuration overrides them.
//
// Spec refs: specs/beads-integration.md §4.8a BI-025c (step 4c);
// specs/beads-integration.md §4.8a BI-025e.
func (a *Adapter) RunWithDBLockedRetry(
	ctx context.Context,
	cfg TimeoutConfig,
	kind CommandKind,
	maxRetries int,
	base time.Duration,
	cap_ time.Duration,
	args ...string,
) (Result, error) {
	backoff := base

	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err := a.RunWithTimeout(ctx, cfg, kind, args...)
		if err != nil {
			// Propagate exec / timeout / context errors directly.
			return Result{}, err
		}

		if result.BrErr != BrDbLocked {
			// Success or non-DbLocked error: return as-is.
			return result, nil
		}

		// BrDbLocked: if this was the last allowed attempt, escalate to
		// BrUnavailable per BI-025c step 4c.
		if attempt == maxRetries {
			return Result{}, fmt.Errorf(
				"brcli: BrDbLocked persisted after %d retries: %w",
				maxRetries,
				BrUnavailable,
			)
		}

		// Sleep for the current backoff, then double (capped at cap_).
		// Respect context cancellation during the sleep.
		select {
		case <-ctx.Done():
			return Result{}, fmt.Errorf("brcli: context canceled during DbLocked backoff: %w", ctx.Err())
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > cap_ {
			backoff = cap_
		}
	}

	// Unreachable: the loop always returns on the last iteration.
	return Result{}, errors.New("brcli: RunWithDBLockedRetry: internal invariant violation")
}
