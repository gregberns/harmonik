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

// RunWithDBLockedRetry invokes RunWithTimeout and retries transient failures
// with exponential backoff starting at base and capped at cap_. Retries fire
// on either of two transient classes:
//
//  1. BrDbLocked Result (exit code 3 — SQLite WAL write contention) per
//     BI-025c step 4c.
//  2. Returned error wrapping BrUnavailable from a wall-clock timeout per
//     BI-025c (the subprocess was SIGTERM/SIGKILL'd before completing).
//     Empirically this surfaces when SQLite contention takes the `br close`
//     write past the 10s write budget — same root-cause class as BrDbLocked,
//     just over a longer time horizon (hk-yjsk8). Retrying is correct because
//     terminal-transition writes are idempotent under the BI-029/BI-030
//     intent-log protocol: the intent file is retained across the retry and
//     the deterministic idempotency key prevents double-application.
//
// This is the BI-025e concurrent-invocation retry path: the adapter MAY
// invoke `br` concurrently; SQLite WAL serializes writes; no adapter-side
// mutex is used.
//
// On every retry the full RunWithTimeout discipline applies: per-invocation
// wall-clock timeout, SIGTERM-then-SIGKILL termination per HC-018, and
// BrUnavailable classification on timeout.
//
// After maxRetries consecutive transient failures the call is escalated to
// BrUnavailable and an error wrapping BrUnavailable is returned, signalling
// that the infrastructure is persistently unavailable and the daemon should
// route to Cat 0 recovery per BI §8. Non-transient errors (BrNotFound,
// BrConflict, BrSchemaMismatch, BrOther, context cancellation, fork failure
// without a BrUnavailable wrap) return immediately without retry.
//
// If ctx is canceled at any point during a sleep or a RunWithTimeout call,
// RunWithDBLockedRetry returns immediately with the context error.
//
// Callers MUST pass the BI-025c defaults (DBLockedRetryMax, DBLockedRetryBase,
// DBLockedRetryCap) unless operator configuration overrides them.
//
// Spec refs: specs/beads-integration.md §4.8a BI-025c (step 4c);
// specs/beads-integration.md §4.8a BI-025e.
// Bead ref: hk-yjsk8 (BrUnavailable retry extension).
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

		// Classify outcome into one of: success, non-transient error,
		// transient (DbLocked Result or BrUnavailable-wrapped err).
		switch {
		case err == nil && result.BrErr != BrDbLocked:
			// Success or non-DbLocked Result: return as-is.
			return result, nil
		case err != nil && errors.Is(err, context.Canceled):
			// Context cancellation is never a transient retry target.
			return Result{}, err
		case err != nil && errors.Is(err, context.DeadlineExceeded):
			return Result{}, err
		case err != nil && !errors.Is(err, BrUnavailable):
			// Exec / fork error that is NOT a wall-clock timeout: propagate.
			return Result{}, err
		}

		// Transient: BrDbLocked Result, or wall-clock-timeout error wrapping
		// BrUnavailable. If this was the last allowed attempt, escalate.
		if attempt == maxRetries {
			if err != nil {
				return Result{}, fmt.Errorf(
					"brcli: BrUnavailable persisted after %d retries: %w",
					maxRetries,
					BrUnavailable,
				)
			}
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
			return Result{}, fmt.Errorf("brcli: context canceled during transient-failure backoff: %w", ctx.Err())
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
