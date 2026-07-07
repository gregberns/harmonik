package workspace

// reviewverdict_retry_injected_hkvv10r_test.go — direct unit tests for
// retryVerdictReadOnMalformed via injected verdictRead funcs (per hk-vv10r).
//
// These tests exercise the retry state-machine WITHOUT any filesystem or
// network I/O — the verdictRead function is injected directly via the
// verdictRead type. This isolates retry logic from I/O variability and
// runs in well under one millisecond per case.
//
// Distinct from the two integration-flavored retry test files:
//   - reviewverdict_via_retry_hkclrts_test.go: exercises the REMOTE path via
//     a real sequenceCatRunner (cat-over-file, non-local CommandRunner).
//   - reviewverdict_local_retry_hk1hgjr_test.go: exercises the LOCAL retrying
//     reader via a real .harmonik/review.json on disk.
//
// Both use real I/O; these are purely in-process.
//
// Also covers: truncated JSON prefix at every byte offset N (1..len−1) MUST
// return ErrMalformed and NEVER return an APPROVE verdict — a regression guard
// for the false-positive path in the retry loop.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture helpers (prefix: retryInjectedFixture)
// ─────────────────────────────────────────────────────────────────────────────

// retryInjectedFixtureStep holds the (verdict, error) pair returned by one
// call to the injected verdictRead func.
type retryInjectedFixtureStep struct {
	v   *ReviewVerdict
	err error
}

// retryInjectedFixture is a sequence-controlled verdictRead implementer.
// The Nth call (0-indexed) returns seq[min(N, len(seq)-1)], so the final entry
// repeats once the sequence is exhausted — enabling "always malformed" stubs.
type retryInjectedFixture struct {
	calls int
	seq   []retryInjectedFixtureStep
}

func newRetryInjectedFixture(steps ...retryInjectedFixtureStep) *retryInjectedFixture {
	return &retryInjectedFixture{seq: steps}
}

func (f *retryInjectedFixture) read(_ context.Context) (*ReviewVerdict, error) {
	idx := f.calls
	if idx >= len(f.seq) {
		idx = len(f.seq) - 1
	}
	f.calls++
	return f.seq[idx].v, f.seq[idx].err
}

// retryInjectedFixtureMalformed returns a step that wraps ErrMalformed —
// simulating a mid-write truncated read that parseReviewVerdict would produce.
func retryInjectedFixtureMalformed() retryInjectedFixtureStep {
	return retryInjectedFixtureStep{err: fmt.Errorf("%w: simulated truncated read", ErrMalformed)}
}

// retryInjectedFixtureAbsent returns a step for an absent verdict (nil,nil) —
// the inconclusive condition per WM-027a §(e).
func retryInjectedFixtureAbsent() retryInjectedFixtureStep {
	return retryInjectedFixtureStep{}
}

// retryInjectedFixtureApprove returns a step with a complete valid APPROVE verdict.
func retryInjectedFixtureApprove() retryInjectedFixtureStep {
	return retryInjectedFixtureStep{
		v: &ReviewVerdict{
			SchemaVersion: ReviewVerdictSchemaVersion,
			Verdict:       ReviewVerdictApprove,
			Flags:         []string{},
			Notes:         "Looks good.",
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: retryVerdictReadOnMalformed via injected verdictRead
// ─────────────────────────────────────────────────────────────────────────────

// TestRetryVerdictReadOnMalformed_Injected_MalformedThenValid is the core
// recovery case (hk-vv10r): the injected func returns ErrMalformed on the first
// call (simulating a partial-write observation) then a valid APPROVE verdict on
// the second call. The retry loop MUST recover and return the valid verdict.
func TestRetryVerdictReadOnMalformed_Injected_MalformedThenValid(t *testing.T) {
	// NOT t.Parallel(): mutates package-level retry-budget vars.
	origBudget := reviewVerdictRemoteRetryBudget
	origBase := reviewVerdictRemoteBaseBackoff
	reviewVerdictRemoteRetryBudget = 2 * time.Second
	reviewVerdictRemoteBaseBackoff = 2 * time.Millisecond
	t.Cleanup(func() {
		reviewVerdictRemoteRetryBudget = origBudget
		reviewVerdictRemoteBaseBackoff = origBase
	})

	f := newRetryInjectedFixture(retryInjectedFixtureMalformed(), retryInjectedFixtureApprove())
	v, err := retryVerdictReadOnMalformed(context.Background(), f.read)
	if err != nil {
		t.Fatalf("retryVerdictReadOnMalformed(malformed-then-valid) err = %v; want nil (recovery via retry)", err)
	}
	if v == nil || v.Verdict != ReviewVerdictApprove {
		t.Fatalf("verdict = %+v; want APPROVE recovered via retry", v)
	}
	if f.calls != 2 {
		t.Errorf("read calls = %d; want 2 (one malformed + one valid)", f.calls)
	}
}

// TestRetryVerdictReadOnMalformed_Injected_MultipleMalformedThenValid verifies
// the loop retries across several consecutive ErrMalformed reads before
// recovering on the eventual valid read — deadline-bounded, not fixed-count.
func TestRetryVerdictReadOnMalformed_Injected_MultipleMalformedThenValid(t *testing.T) {
	// NOT t.Parallel(): mutates package-level retry-budget vars.
	origBudget := reviewVerdictRemoteRetryBudget
	origBase := reviewVerdictRemoteBaseBackoff
	reviewVerdictRemoteRetryBudget = 2 * time.Second
	reviewVerdictRemoteBaseBackoff = 2 * time.Millisecond
	t.Cleanup(func() {
		reviewVerdictRemoteRetryBudget = origBudget
		reviewVerdictRemoteBaseBackoff = origBase
	})

	f := newRetryInjectedFixture(
		retryInjectedFixtureMalformed(),
		retryInjectedFixtureMalformed(),
		retryInjectedFixtureMalformed(),
		retryInjectedFixtureApprove(),
	)
	v, err := retryVerdictReadOnMalformed(context.Background(), f.read)
	if err != nil {
		t.Fatalf("retryVerdictReadOnMalformed(3x-malformed-then-valid) err = %v; want recovery", err)
	}
	if v == nil || v.Verdict != ReviewVerdictApprove {
		t.Fatalf("verdict = %+v; want APPROVE", v)
	}
	if f.calls != 4 {
		t.Errorf("read calls = %d; want 4 (three malformed + one valid)", f.calls)
	}
}

// TestRetryVerdictReadOnMalformed_Injected_AlwaysMalformedExhaustedBudget proves
// the retry is bounded: when every attempt returns ErrMalformed, the loop
// terminates once the deadline budget is spent and returns ErrMalformed (no
// infinite loop, no false success).
func TestRetryVerdictReadOnMalformed_Injected_AlwaysMalformedExhaustedBudget(t *testing.T) {
	// NOT t.Parallel(): mutates package-level retry-budget vars.
	origBudget := reviewVerdictRemoteRetryBudget
	origBase := reviewVerdictRemoteBaseBackoff
	reviewVerdictRemoteRetryBudget = 60 * time.Millisecond
	reviewVerdictRemoteBaseBackoff = 5 * time.Millisecond
	t.Cleanup(func() {
		reviewVerdictRemoteRetryBudget = origBudget
		reviewVerdictRemoteBaseBackoff = origBase
	})

	f := newRetryInjectedFixture(retryInjectedFixtureMalformed()) // repeats forever
	start := time.Now()
	v, err := retryVerdictReadOnMalformed(context.Background(), f.read)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v; want ErrMalformed after budget exhaustion", err)
	}
	if v != nil {
		t.Errorf("verdict = %+v; want nil", v)
	}
	if f.calls < 2 {
		t.Errorf("read calls = %d; want ≥2 (at least one retry before budget)", f.calls)
	}
	if elapsed > 2*time.Second {
		t.Errorf("elapsed = %v; want bounded by the retry budget (no unbounded loop)", elapsed)
	}
}

// TestRetryVerdictReadOnMalformed_Injected_AbsentShortCircuits verifies that
// an absent result (nil,nil) short-circuits immediately without retry — the
// inconclusive condition per WM-027a §(e) is not retryable.
func TestRetryVerdictReadOnMalformed_Injected_AbsentShortCircuits(t *testing.T) {
	// NOT t.Parallel(): mutates package-level retry-budget vars.
	origBudget := reviewVerdictRemoteRetryBudget
	reviewVerdictRemoteRetryBudget = 10 * time.Second // large: a retry would stall the test
	t.Cleanup(func() { reviewVerdictRemoteRetryBudget = origBudget })

	f := newRetryInjectedFixture(retryInjectedFixtureAbsent())
	start := time.Now()
	v, err := retryVerdictReadOnMalformed(context.Background(), f.read)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("err = %v; want nil (absent = inconclusive, no retry)", err)
	}
	if v != nil {
		t.Errorf("verdict = %+v; want nil for absent", v)
	}
	if f.calls != 1 {
		t.Errorf("read calls = %d; want 1 (absent short-circuits immediately)", f.calls)
	}
	if elapsed > time.Second {
		t.Errorf("elapsed = %v; want an immediate short-circuit for absent (no retry)", elapsed)
	}
}

// TestRetryVerdictReadOnMalformed_Injected_ValidShortCircuits verifies that a
// clean valid read short-circuits immediately without consuming any retry budget.
func TestRetryVerdictReadOnMalformed_Injected_ValidShortCircuits(t *testing.T) {
	t.Parallel()

	f := newRetryInjectedFixture(retryInjectedFixtureApprove())
	v, err := retryVerdictReadOnMalformed(context.Background(), f.read)
	if err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if v == nil || v.Verdict != ReviewVerdictApprove {
		t.Fatalf("verdict = %+v; want APPROVE", v)
	}
	if f.calls != 1 {
		t.Errorf("read calls = %d; want 1 (valid short-circuits, no retry)", f.calls)
	}
}

// TestRetryVerdictReadOnMalformed_Injected_CtxAlreadyCancelled verifies that a
// pre-cancelled context causes the retry wait to short-circuit after the first
// ErrMalformed read, returning context.Canceled rather than burning the budget.
func TestRetryVerdictReadOnMalformed_Injected_CtxAlreadyCancelled(t *testing.T) {
	// NOT t.Parallel(): mutates package-level retry-budget vars.
	origBudget := reviewVerdictRemoteRetryBudget
	origBase := reviewVerdictRemoteBaseBackoff
	reviewVerdictRemoteRetryBudget = 10 * time.Second
	reviewVerdictRemoteBaseBackoff = 50 * time.Millisecond
	t.Cleanup(func() {
		reviewVerdictRemoteRetryBudget = origBudget
		reviewVerdictRemoteBaseBackoff = origBase
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel before the call

	f := newRetryInjectedFixture(retryInjectedFixtureMalformed())
	start := time.Now()
	_, err := retryVerdictReadOnMalformed(ctx, f.read)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want context.Canceled (pre-cancelled ctx)", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("elapsed = %v; want prompt return on pre-cancelled ctx", elapsed)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Property test: truncated JSON at every byte boundary never returns APPROVE
// ─────────────────────────────────────────────────────────────────────────────

// TestParseReviewVerdict_TruncatedAtNBoundariesNeverApprove is a regression
// property test: for every byte offset N from 1 to len(fullJSON)−1, a JSON
// prefix truncated at N MUST produce ErrMalformed and MUST NOT produce an
// APPROVE verdict.
//
// Motivation: the retry loop in ReadReviewVerdictVia and
// ReadReviewVerdictLocalRetry relies on ErrMalformed being the ONLY outcome
// for any truncated read. If any byte-prefix silently returned APPROVE the
// false-positive guard would be defeated — a partial write could be misread
// as a reviewer approval.
func TestParseReviewVerdict_TruncatedAtNBoundariesNeverApprove(t *testing.T) {
	t.Parallel()

	full := reviewVerdictFixtureValidJSON(t)
	if len(full) < 4 {
		t.Fatalf("fixture JSON too short to truncate: %q", full)
	}

	for n := 1; n < len(full); n++ {
		n := n
		t.Run(fmt.Sprintf("offset_%d_of_%d", n, len(full)), func(t *testing.T) {
			t.Parallel()
			truncated := full[:n]
			v, err := parseReviewVerdict(truncated, "<test-boundary>")
			if err == nil {
				t.Errorf("parseReviewVerdict(truncated@%d) returned nil error; want ErrMalformed", n)
				return
			}
			if !errors.Is(err, ErrMalformed) {
				t.Errorf("parseReviewVerdict(truncated@%d) err = %v; want errors.Is(err, ErrMalformed)", n, err)
			}
			if v != nil && v.Verdict == ReviewVerdictApprove {
				t.Errorf("parseReviewVerdict(truncated@%d) returned APPROVE verdict; want no false-positive", n)
			}
		})
	}
}
