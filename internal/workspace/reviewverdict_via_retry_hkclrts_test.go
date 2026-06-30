package workspace

// reviewverdict_via_retry_hkclrts_test.go — tests for the remote read-retry in
// ReadReviewVerdictVia (hk-clrts).
//
// Scenario: a remote DOT-review fast-failed (~11s) at review_correctness with
// `ErrMalformed: unexpected end of JSON input` reading the worker's
// .harmonik/review.json. The file existed but a cat-over-SSH read observed a
// partially-written / not-yet-durable (truncated) copy. The fix retries ONLY on
// ErrMalformed with bounded exponential backoff so a transient truncated read
// recovers, while a genuinely-malformed verdict still fails after the cap.
//
// These tests exercise the REMOTE path only (a non-local CommandRunner). The
// local os.ReadFile path stays byte-identical (NFR7) — see the nil-runner
// regression below.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// sequenceCatRunner is a non-local CommandRunner stub whose successive Command()
// calls cat successive files from paths. The Nth Command() invocation (0-indexed)
// returns `cat paths[min(N, len(paths)-1)]`, so a list like
// [truncated, valid] simulates a worker whose first read is truncated and whose
// retry observes the complete, durable file. Being a distinct (non-LocalRunner)
// type, it is classified non-local by runnerIsLocalFS, so ReadReviewVerdictVia
// routes through it. Concurrent-safe to honor the CommandRunner contract.
type sequenceCatRunner struct {
	mu    sync.Mutex
	paths []string // file cat'd per call, in order; final entry repeats once exhausted
	calls int      // number of Command() invocations so far
}

func (r *sequenceCatRunner) Command(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	r.mu.Lock()
	idx := r.calls
	if idx >= len(r.paths) {
		idx = len(r.paths) - 1
	}
	src := r.paths[idx]
	r.calls++
	r.mu.Unlock()
	//nolint:gosec // G204: src is a test-controlled temp path, not user input
	return exec.CommandContext(ctx, "cat", src)
}

func (r *sequenceCatRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// writeTempVerdictFile writes data to a fresh temp file and returns its path.
func writeTempVerdictFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("writeTempVerdictFile(%s): %v", name, err)
	}
	return p
}

// truncatedVerdictJSON returns a deliberately truncated copy of a valid verdict
// payload — the exact shape a cat-over-SSH read sees mid-write: valid JSON head,
// abrupt end → json: unexpected end of JSON input → ErrMalformed.
func truncatedVerdictJSON(t *testing.T) []byte {
	t.Helper()
	full := reviewVerdictFixtureValidJSON(t)
	if len(full) < 4 {
		t.Fatalf("fixture too short to truncate: %q", full)
	}
	return full[:len(full)/2] // chop the back half: unterminated object
}

// TestReadReviewVerdictVia_Retry_TruncatedThenValid is the RED→GREEN case: the
// first remote cat returns truncated JSON (ErrMalformed), a later attempt returns
// the complete valid verdict. With the retry, ReadReviewVerdictVia MUST succeed.
// (Before the fix this returned ErrMalformed on the single first read.)
func TestReadReviewVerdictVia_Retry_TruncatedThenValid(t *testing.T) {
	t.Parallel()

	truncated := writeTempVerdictFile(t, "truncated.json", truncatedVerdictJSON(t))
	valid := writeTempVerdictFile(t, "valid.json", reviewVerdictFixtureValidJSON(t))

	// box-A workspace has NO local review.json — proves the read is routed.
	boxAWorkspace := t.TempDir()

	runner := &sequenceCatRunner{paths: []string{truncated, valid}}

	v, err := ReadReviewVerdictVia(context.Background(), runner, boxAWorkspace)
	if err != nil {
		t.Fatalf("ReadReviewVerdictVia(truncated-then-valid): %v; want recovery via retry", err)
	}
	if v == nil {
		t.Fatal("ReadReviewVerdictVia returned nil; want the valid verdict from the retry read")
	}
	if v.Verdict != ReviewVerdictApprove {
		t.Errorf("Verdict = %q; want %q", v.Verdict, ReviewVerdictApprove)
	}
	if got := runner.callCount(); got != 2 {
		t.Errorf("cat call count = %d; want 2 (one truncated read + one successful retry)", got)
	}
}

// TestReadReviewVerdictVia_Retry_AlwaysTruncated proves the retry is bounded: when
// every attempt returns truncated JSON, ReadReviewVerdictVia returns ErrMalformed
// once the deadline retry budget is spent (no infinite loop, no false success).
// hk-qts7r converted the fixed 7-attempt cap into a deadline-bounded
// retry-until-valid loop, so the assertion is "ErrMalformed + retried at least
// once + terminated within the budget" rather than an exact attempt count. The
// budget is shrunk so the test resolves in milliseconds.
func TestReadReviewVerdictVia_Retry_AlwaysTruncated(t *testing.T) {
	// NOT t.Parallel(): mutates the package-level retry-budget vars.
	origBudget := reviewVerdictRemoteRetryBudget
	origBase := reviewVerdictRemoteBaseBackoff
	reviewVerdictRemoteRetryBudget = 60 * time.Millisecond
	reviewVerdictRemoteBaseBackoff = 5 * time.Millisecond
	t.Cleanup(func() {
		reviewVerdictRemoteRetryBudget = origBudget
		reviewVerdictRemoteBaseBackoff = origBase
	})

	truncated := writeTempVerdictFile(t, "truncated.json", truncatedVerdictJSON(t))
	boxAWorkspace := t.TempDir()

	runner := &sequenceCatRunner{paths: []string{truncated}} // repeats truncated forever

	start := time.Now()
	v, err := ReadReviewVerdictVia(context.Background(), runner, boxAWorkspace)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("ReadReviewVerdictVia(always-truncated) err = %v; want ErrMalformed after the retry budget", err)
	}
	if v != nil {
		t.Errorf("ReadReviewVerdictVia(always-truncated) verdict = %+v; want nil", v)
	}
	if got := runner.callCount(); got < 2 {
		t.Errorf("cat call count = %d; want ≥2 (retried at least once before the budget elapsed)", got)
	}
	// Terminated within a small multiple of the budget — proves the loop is bounded.
	if elapsed > 2*time.Second {
		t.Errorf("ReadReviewVerdictVia(always-truncated) took %v; want a bounded deadline (no unbounded retry)", elapsed)
	}
}

// TestReadReviewVerdictVia_Retry_MultipleTruncatedThenValid (hk-qts7r) proves the
// deadline-bounded retry-until-valid loop recovers when the worker file is
// observed truncated across SEVERAL successive reads before it finally lands
// complete and durable — as long as that happens within the retry budget.
func TestReadReviewVerdictVia_Retry_MultipleTruncatedThenValid(t *testing.T) {
	// NOT t.Parallel(): mutates the package-level retry-budget vars.
	origBudget := reviewVerdictRemoteRetryBudget
	origBase := reviewVerdictRemoteBaseBackoff
	reviewVerdictRemoteRetryBudget = 2 * time.Second
	reviewVerdictRemoteBaseBackoff = 2 * time.Millisecond
	t.Cleanup(func() {
		reviewVerdictRemoteRetryBudget = origBudget
		reviewVerdictRemoteBaseBackoff = origBase
	})

	truncated := writeTempVerdictFile(t, "truncated.json", truncatedVerdictJSON(t))
	valid := writeTempVerdictFile(t, "valid.json", reviewVerdictFixtureValidJSON(t))
	boxAWorkspace := t.TempDir()

	// Four truncated reads, then the durable valid file.
	runner := &sequenceCatRunner{paths: []string{truncated, truncated, truncated, truncated, valid}}

	v, err := ReadReviewVerdictVia(context.Background(), runner, boxAWorkspace)
	if err != nil {
		t.Fatalf("ReadReviewVerdictVia(multi-truncated-then-valid): %v; want recovery within the budget", err)
	}
	if v == nil || v.Verdict != ReviewVerdictApprove {
		t.Fatalf("verdict = %+v; want a valid APPROVE recovered via retry", v)
	}
	if got := runner.callCount(); got != 5 {
		t.Errorf("cat call count = %d; want 5 (four truncated reads + one successful retry)", got)
	}
}

// cancelAfterFirstCatRunner is a non-local CommandRunner stub that cancels the
// loop context on its first Command() call, then returns a `cat <src>` built on a
// background context so the read itself still SUCCEEDS (yielding a truncated body
// → ErrMalformed). This forces control into the inter-attempt backoff wait with a
// already-Done loop ctx, exercising the ctx-cancellation branch (which a plain
// pre-cancelled ctx cannot, since that fails the cat itself → the absent branch).
type cancelAfterFirstCatRunner struct {
	src    string
	cancel context.CancelFunc
	mu     sync.Mutex
	fired  bool
	calls  int
}

func (r *cancelAfterFirstCatRunner) Command(_ context.Context, _ string, _ ...string) *exec.Cmd {
	r.mu.Lock()
	r.calls++
	if !r.fired {
		r.fired = true
		r.cancel()
	}
	r.mu.Unlock()
	// Background ctx so the cat succeeds despite the now-cancelled loop ctx.
	//nolint:gosec // G204: src is a test-controlled temp path
	return exec.CommandContext(context.Background(), "cat", r.src)
}

// TestReadReviewVerdictVia_Retry_CtxCancel verifies ctx cancellation is honored
// in the inter-attempt wait: after a truncated (ErrMalformed) read, a context
// cancelled before the backoff elapses returns ctx.Err() rather than burning all
// retries.
func TestReadReviewVerdictVia_Retry_CtxCancel(t *testing.T) {
	t.Parallel()

	truncated := writeTempVerdictFile(t, "truncated.json", truncatedVerdictJSON(t))
	boxAWorkspace := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	runner := &cancelAfterFirstCatRunner{src: truncated, cancel: cancel}

	_, err := ReadReviewVerdictVia(ctx, runner, boxAWorkspace)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadReviewVerdictVia(cancel-mid-retry) err = %v; want context.Canceled", err)
	}
	runner.mu.Lock()
	got := runner.calls
	runner.mu.Unlock()
	if got != 1 {
		t.Errorf("cat call count = %d; want 1 (cancelled in the wait after the first read)", got)
	}
}

// TestReadReviewVerdictVia_Retry_LocalPathNoRetry is the NFR7 regression: the
// local path (nil runner) must NOT pick up any remote retry behavior — a local
// malformed file fails immediately via os.ReadFile + parseReviewVerdict, with no
// runner involvement.
func TestReadReviewVerdictVia_Retry_LocalPathNoRetry(t *testing.T) {
	t.Parallel()

	// Write a malformed (truncated) verdict to the LOCAL box-A workspace.
	workspacePath := reviewVerdictFixtureWrite(t, truncatedVerdictJSON(t))

	v, err := ReadReviewVerdictVia(context.Background(), nil, workspacePath)
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("ReadReviewVerdictVia(nil, malformed-local) err = %v; want ErrMalformed (local path, no retry)", err)
	}
	if v != nil {
		t.Errorf("verdict = %+v; want nil for a malformed local file", v)
	}
}
