package workspace

// reviewverdict_local_retry_hk1hgjr_test.go — tests for ReadReviewVerdictLocalRetry
// (hk-1hgjr), the LOCAL twin of the remote read-retry (hk-qts7r).
//
// Scenario: the reviewloop finalize read reads the reviewer's box-A-local
// review.json with a nil runner. If the daemon reads at the instant the
// reviewer's claude is still flushing / has not yet made the write durable, a
// local os.ReadFile observes a truncated file → parseReviewVerdict returns
// ErrMalformed → the run false-fails fast because the plain ReadReviewVerdict does
// NOT retry. ReadReviewVerdictLocalRetry closes that gap: retry-until-valid ONLY
// on ErrMalformed with a ctx/deadline-bounded budget.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestReadReviewVerdict_LocalNoRetry_TruncatedFailsFast is the RED baseline: it
// pins the CURRENT non-retrying local behavior — a transiently-truncated
// review.json read via the plain ReadReviewVerdict (and, equivalently, the
// ReadReviewVerdictVia nil/local branch) returns ErrMalformed immediately with no
// retry. This is exactly the false-fail the finalize read suffered before
// hk-1hgjr.
func TestReadReviewVerdict_LocalNoRetry_TruncatedFailsFast(t *testing.T) {
	t.Parallel()

	workspacePath := reviewVerdictFixtureWrite(t, truncatedVerdictJSON(t))

	// Plain local read: no retry — fails fast on the truncated file.
	v, err := ReadReviewVerdict(workspacePath)
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("ReadReviewVerdict(truncated-local) err = %v; want ErrMalformed (no-retry local path)", err)
	}
	if v != nil {
		t.Errorf("verdict = %+v; want nil", v)
	}

	// The Via nil/local branch is byte-identical no-retry (NFR7).
	v2, err2 := ReadReviewVerdictVia(context.Background(), nil, workspacePath)
	if !errors.Is(err2, ErrMalformed) {
		t.Fatalf("ReadReviewVerdictVia(nil, truncated-local) err = %v; want ErrMalformed (byte-identical local path)", err2)
	}
	if v2 != nil {
		t.Errorf("verdict (Via nil) = %+v; want nil", v2)
	}
}

// TestReadReviewVerdictLocalRetry_TruncatedThenValid is the RED→GREEN case: the
// review.json is truncated at first read, then a concurrent writer atomically
// replaces it with a complete, durable verdict. ReadReviewVerdictLocalRetry MUST
// retry past the transient ErrMalformed and recover the valid verdict.
//
// Before hk-1hgjr the finalize read used ReadReviewVerdictVia(ctx, nil, …) which
// returns on the FIRST truncated read (proved above) — so this recovery is only
// possible with the retrying local reader.
func TestReadReviewVerdictLocalRetry_TruncatedThenValid(t *testing.T) {
	// NOT t.Parallel(): mutates the package-level retry-budget vars.
	origBudget := reviewVerdictRemoteRetryBudget
	origBase := reviewVerdictRemoteBaseBackoff
	reviewVerdictRemoteRetryBudget = 2 * time.Second
	reviewVerdictRemoteBaseBackoff = 2 * time.Millisecond
	t.Cleanup(func() {
		reviewVerdictRemoteRetryBudget = origBudget
		reviewVerdictRemoteBaseBackoff = origBase
	})

	// Start with a truncated file.
	workspacePath := reviewVerdictFixtureWrite(t, truncatedVerdictJSON(t))
	target := ReviewVerdictPath(workspacePath)

	// Concurrently, after a short delay, atomically replace it with the valid
	// verdict (temp write + rename → a read never sees a partial of the new write).
	go func() {
		time.Sleep(20 * time.Millisecond)
		tmp := filepath.Join(filepath.Dir(target), "review.json.tmp")
		//nolint:gosec // G306: test fixture
		if err := os.WriteFile(tmp, reviewVerdictFixtureValidJSON(t), 0o644); err != nil {
			return
		}
		_ = os.Rename(tmp, target)
	}()

	v, err := ReadReviewVerdictLocalRetry(context.Background(), workspacePath)
	if err != nil {
		t.Fatalf("ReadReviewVerdictLocalRetry(truncated-then-valid): %v; want recovery via retry", err)
	}
	if v == nil || v.Verdict != ReviewVerdictApprove {
		t.Fatalf("verdict = %+v; want a valid APPROVE recovered via retry", v)
	}
}

// TestReadReviewVerdictLocalRetry_AlwaysTruncated proves the retry is bounded: a
// review.json that stays malformed returns ErrMalformed once the deadline budget
// is spent (no infinite loop, no false success — only bounded extra latency).
func TestReadReviewVerdictLocalRetry_AlwaysTruncated(t *testing.T) {
	// NOT t.Parallel(): mutates the package-level retry-budget vars.
	origBudget := reviewVerdictRemoteRetryBudget
	origBase := reviewVerdictRemoteBaseBackoff
	reviewVerdictRemoteRetryBudget = 60 * time.Millisecond
	reviewVerdictRemoteBaseBackoff = 5 * time.Millisecond
	t.Cleanup(func() {
		reviewVerdictRemoteRetryBudget = origBudget
		reviewVerdictRemoteBaseBackoff = origBase
	})

	workspacePath := reviewVerdictFixtureWrite(t, truncatedVerdictJSON(t))

	start := time.Now()
	v, err := ReadReviewVerdictLocalRetry(context.Background(), workspacePath)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("ReadReviewVerdictLocalRetry(always-truncated) err = %v; want ErrMalformed after the retry budget", err)
	}
	if v != nil {
		t.Errorf("verdict = %+v; want nil", v)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %v; want a bounded deadline (no unbounded retry)", elapsed)
	}
}

// TestReadReviewVerdictLocalRetry_AbsentShortCircuits proves an absent verdict
// returns (nil,nil) immediately with no retry latency (WM-027a §(e)).
func TestReadReviewVerdictLocalRetry_AbsentShortCircuits(t *testing.T) {
	// NOT t.Parallel(): mutates the package-level retry-budget vars.
	origBudget := reviewVerdictRemoteRetryBudget
	reviewVerdictRemoteRetryBudget = 10 * time.Second // huge — a retry would hang the test
	t.Cleanup(func() { reviewVerdictRemoteRetryBudget = origBudget })

	workspacePath := t.TempDir() // no .harmonik/review.json

	start := time.Now()
	v, err := ReadReviewVerdictLocalRetry(context.Background(), workspacePath)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ReadReviewVerdictLocalRetry(absent) err = %v; want nil (inconclusive)", err)
	}
	if v != nil {
		t.Errorf("verdict = %+v; want nil for an absent file", v)
	}
	if elapsed > time.Second {
		t.Errorf("took %v; want an immediate short-circuit for an absent file (no retry)", elapsed)
	}
}

// TestReadReviewVerdictLocalRetry_ValidShortCircuits proves a clean valid read
// short-circuits immediately (no added latency for the common case).
func TestReadReviewVerdictLocalRetry_ValidShortCircuits(t *testing.T) {
	t.Parallel()

	workspacePath := reviewVerdictFixtureWrite(t, reviewVerdictFixtureValidJSON(t))

	v, err := ReadReviewVerdictLocalRetry(context.Background(), workspacePath)
	if err != nil {
		t.Fatalf("ReadReviewVerdictLocalRetry(valid) err = %v; want nil", err)
	}
	if v == nil || v.Verdict != ReviewVerdictApprove {
		t.Fatalf("verdict = %+v; want a valid APPROVE", v)
	}
}

// TestReadReviewVerdictLocalRetry_CtxCancel verifies ctx cancellation is honored
// in the inter-attempt wait after a transient ErrMalformed read.
func TestReadReviewVerdictLocalRetry_CtxCancel(t *testing.T) {
	// NOT t.Parallel(): mutates the package-level retry-budget vars.
	origBudget := reviewVerdictRemoteRetryBudget
	origBase := reviewVerdictRemoteBaseBackoff
	reviewVerdictRemoteRetryBudget = 10 * time.Second
	reviewVerdictRemoteBaseBackoff = 50 * time.Millisecond
	t.Cleanup(func() {
		reviewVerdictRemoteRetryBudget = origBudget
		reviewVerdictRemoteBaseBackoff = origBase
	})

	workspacePath := reviewVerdictFixtureWrite(t, truncatedVerdictJSON(t))

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after the first read enters the backoff wait.
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := ReadReviewVerdictLocalRetry(ctx, workspacePath)
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadReviewVerdictLocalRetry(cancel-mid-retry) err = %v; want context.Canceled", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %v; want prompt return on ctx cancel", elapsed)
	}
}
