package brcli

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
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

// UnavailableRetryMax is the BI-025c maximum number of retry attempts for
// terminal-transition writes when `br` returns a wall-clock-timeout
// BrUnavailable (step 4c-transient: transient SQLite contention burst).
//
// This is wider than DBLockedRetryMax (3) because terminal-transition writes
// have the BI-030 intent-log backing idempotency across retries, and because
// dogfood run hk-75rij showed that 3 retries are insufficient under concurrent
// kerf/agent activity (hk-ekz5v).
//
// Applies only to CloseBead, ClaimBead, ReopenBead, and ResetBead paths.
// Non-terminal-transition reads use DBLockedRetryMax.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step (4c-transient).
const UnavailableRetryMax = 10

// UnavailableRetryBase is the BI-025c initial backoff duration for
// terminal-transition BrUnavailable transient retries.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step (4c-transient: "initial 50ms").
const UnavailableRetryBase = 50 * time.Millisecond

// UnavailableRetryCap is the maximum backoff duration per sleep for
// terminal-transition BrUnavailable transient retries.
//
// Set to 15s because each `br` attempt may hold .write.lock for up to ~15s
// under the 10s write timeout plus the 5s SIGTERM-grace window (HC-018). The
// cap must exceed that window so a retry attempt does not queue behind a
// still-dying prior attempt, which was the root cause of the 19s apparent
// latency diagnosed in hk-5dewt (retry-loop self-contention on .write.lock).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step (4c-transient).
// Root cause: hk-5dewt / hk-5ce5n.
const UnavailableRetryCap = 15 * time.Second

// RunWithDBLockedRetry invokes RunWithTimeout and retries transient failures
// with exponential backoff starting at base and capped at maxBackoff. Retries fire
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
// Terminal-transition writes (ClaimBead, CloseBead, ReopenBead, ResetBead,
// SweepCloseBead) are additionally serialized via Adapter.terminalMu to prevent
// concurrent br invocations from racing on the SQLite .write.lock (hk-hdbls).
// Read paths (ShowBead, ListByStatus) are not gated and remain concurrent.
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
	maxBackoff time.Duration,
	args ...string,
) (Result, error) {
	result, _, err := a.runWithDBLockedRetryTimeoutKills(ctx, cfg, kind, maxRetries, base, maxBackoff, args...)
	return result, err
}

// runWithDBLockedRetryTimeoutKills is RunWithDBLockedRetry plus the number of
// attempts it lost to a WALL-CLOCK TIMEOUT KILL. RunWithDBLockedRetry delegates
// to it and drops the count.
//
// The count matters to the claim path. A caller that sees `br` REFUSE a write
// cannot always tell whether an EARLIER attempt of the same call already
// landed. The two transient classes that force a retry carry OPPOSITE evidence,
// and only one of them creates that doubt:
//
//   - Timeout kill (BrUnavailable). `br` was killed at the wall-clock deadline.
//     It may have committed the write before it died, because a commit and its
//     acknowledgement are not atomic (hk-5dewt / hk-yjsk8). A later refusal may
//     be `br` rejecting OUR OWN landed write. This is the doubt. It is counted.
//   - Locked database (BrDbLocked, exit 3). `br` gave up waiting for the write
//     lock and wrote NOTHING. A later refusal cannot be our own write. There is
//     no doubt here, so this is NOT counted.
//
// Counting attempts instead of timeout kills would conflate the two and credit
// a claim after ordinary SQLite contention, which re-opens the takeover the
// claim gate exists to stop. Count only the kills.
//
// The count is 0 for a call whose attempts all produced a definite answer from
// `br`, however many attempts that took.
func (a *Adapter) runWithDBLockedRetryTimeoutKills(
	ctx context.Context,
	cfg TimeoutConfig,
	kind CommandKind,
	maxRetries int,
	base time.Duration,
	maxBackoff time.Duration,
	args ...string,
) (Result, int, error) {
	backoff := base

	// Diagnostic counters: track how many attempts hit each failure class.
	var countDbLocked, countUnavailable int

	// lastResult and lastErr hold the outcome of the most-recent transient
	// attempt so the escalation message can surface them verbatim.
	var lastResult Result
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err := a.RunWithTimeout(ctx, cfg, kind, args...)

		// Classify outcome into one of: success, non-transient error,
		// transient (DbLocked Result or BrUnavailable-wrapped err).
		switch {
		case err == nil && result.BrErr != BrDbLocked:
			// Success or non-DbLocked Result: return as-is.
			return result, countUnavailable, nil
		case err != nil && errors.Is(err, context.Canceled):
			// Context cancellation is never a transient retry target.
			return Result{}, countUnavailable, err
		case err != nil && errors.Is(err, context.DeadlineExceeded):
			return Result{}, countUnavailable, err
		case err != nil && !errors.Is(err, BrUnavailable):
			// Exec / fork error that is NOT a wall-clock timeout: propagate.
			return Result{}, countUnavailable, err
		}

		// Transient: record outcome for diagnostics.
		lastResult = result
		lastErr = err
		if err != nil {
			countUnavailable++
		} else {
			countDbLocked++
		}

		// If this was the last allowed attempt, escalate with full diagnostics.
		if attempt == maxRetries {
			// Capture the last 200 bytes of stderr for the diagnostic message.
			stderrSnippet := lastResult.Stderr
			if len(stderrSnippet) > 200 {
				stderrSnippet = stderrSnippet[len(stderrSnippet)-200:]
			}

			totalAttempts := maxRetries + 1
			if lastErr != nil {
				return Result{}, countUnavailable, fmt.Errorf(
					"brcli: BrUnavailable persisted after %d retries"+
						" (%d/%d BrUnavailable, %d/%d BrDbLocked)"+
						" last attempt: brErr=%s exit=%d stderr=%q: %w",
					maxRetries,
					countUnavailable, totalAttempts,
					countDbLocked, totalAttempts,
					lastResult.BrErr.String(), lastResult.ExitCode, stderrSnippet,
					BrUnavailable,
				)
			}
			return Result{}, countUnavailable, fmt.Errorf(
				"brcli: BrDbLocked persisted after %d retries"+
					" (%d/%d BrUnavailable, %d/%d BrDbLocked)"+
					" last attempt: brErr=%s exit=%d stderr=%q: %w",
				maxRetries,
				countUnavailable, totalAttempts,
				countDbLocked, totalAttempts,
				lastResult.BrErr.String(), lastResult.ExitCode, stderrSnippet,
				BrUnavailable,
			)
		}

		// Sleep for the current backoff, then double (capped at cap_).
		// Add up to 25% jitter before capping to reduce thundering herd under
		// concurrent terminal writes (hk-cw4sx: N workers retry simultaneously).
		// Respect context cancellation during the sleep.
		select {
		case <-ctx.Done():
			return Result{}, countUnavailable, fmt.Errorf("brcli: context canceled during transient-failure backoff: %w", ctx.Err())
		case <-time.After(backoff):
		}

		backoff *= 2
		if jitterRange := int64(backoff / 4); jitterRange > 0 {
			backoff += time.Duration(rand.Int63n(jitterRange)) //nolint:gosec // G404: non-crypto jitter for backoff scheduling
		}
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	// Unreachable: the loop always returns on the last iteration.
	return Result{}, countUnavailable, errors.New("brcli: RunWithDBLockedRetry: internal invariant violation")
}
