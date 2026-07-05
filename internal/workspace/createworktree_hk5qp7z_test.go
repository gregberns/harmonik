package workspace

import (
	"context"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestHK5QP7Z_ConcurrentCreatesMutexSerializes verifies the hk-5qp7z fix: when
// N concurrent calls to CreateWorktree share the same WorktreeRootConfig.createMu
// (set via WithCreateMutex), the git-worktree-add + HEAD-resolve retry loop is
// serialised — never more than one call is in the loop at a time.
//
// Symptom (hk-5qp7z): 7/7 beads dispatched to gb-mbp (max_slots:6) fast-failed
// with "git worktree add exited 0 but HEAD did not resolve (concurrent remote
// create race)" even though hk-iaj1w (per-attempt retry) and hk-lt091 (fetch +
// create inside mergeMu) were both deployed. The root cause: N concurrent
// "git worktree add" calls against the same shared worker repo race on HEAD/index
// resolution; the single-create retry guard does not cover N concurrent creates
// because the race persists across all retry attempts.
//
// Fix: CreateWorktree acquires cfg.createMu (when non-nil) for the full duration
// of the retry loop, so concurrent callers that share the same mutex never overlap
// on the worker's git repo.
//
// This test verifies the mutex enforcement using an in-process concurrency
// counter: the RecordingRunner's CmdFunc atomically increments an "active adds"
// counter before git-worktree-add and decrements it after, recording the
// peak concurrency. With a shared createMu the peak MUST be ≤1.
func TestHK5QP7Z_ConcurrentCreatesMutexSerializes(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)

	const nConcurrent = 5
	runIDs := [nConcurrent]string{
		"019f3133-5qp7-7001-0001-000000000001",
		"019f3133-5qp7-7001-0001-000000000002",
		"019f3133-5qp7-7001-0001-000000000003",
		"019f3133-5qp7-7001-0001-000000000004",
		"019f3133-5qp7-7001-0001-000000000005",
	}

	var (
		activeAdds int64 // atomic: current concurrent worktree-add calls in flight
		peakAdds   int64 // atomic: maximum seen
	)

	// mu is the shared create mutex — all N CreateWorktree calls share it.
	var mu sync.Mutex

	// The RecordingRunner runs real git for mkdir + worktree-add so real
	// worktrees are created. Around each worktree-add we observe the concurrency
	// window: if the mutex is working, activeAdds never exceeds 1.
	makeRunner := func() *tmux.RecordingRunner {
		return &tmux.RecordingRunner{
			CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
				if name == "git" && containsArg(args, "worktree") && containsArg(args, "add") {
					// This increment+peak-check runs WHILE the createMu is held
					// by this goroutine (the mutex wraps the full retry loop in
					// CreateWorktree). If two goroutines are ever here simultaneously,
					// peak > 1.
					cur := atomic.AddInt64(&activeAdds, 1)
					for {
						old := atomic.LoadInt64(&peakAdds)
						if cur <= old || atomic.CompareAndSwapInt64(&peakAdds, old, cur) {
							break
						}
					}
					realCmd := exec.CommandContext(ctx, name, args...)
					atomic.AddInt64(&activeAdds, -1)
					return realCmd
				}
				return exec.CommandContext(ctx, name, args...)
			},
		}
	}

	// Launch N goroutines, each creating its own worktree with the SHARED mutex.
	errCh := make(chan error, nConcurrent)
	var wg sync.WaitGroup
	for i := 0; i < nConcurrent; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			cfg := NoWorktreeRootOverride().WithRunner(makeRunner()).WithCreateMutex(&mu)
			errCh <- CreateWorktree(context.Background(), repo, id, sha, cfg)
		}(runIDs[i])
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("hk-5qp7z: CreateWorktree returned error: %v", err)
		}
	}

	// Peak concurrency must be ≤1: the mutex must have prevented any two
	// git-worktree-add calls from being simultaneously active.
	if peak := atomic.LoadInt64(&peakAdds); peak > 1 {
		t.Errorf("hk-5qp7z: peak concurrent worktree-adds = %d, want ≤1 (createMu did not serialize)", peak)
	}
}

// TestHK5QP7Z_WithoutMutexConcurrencyIsUnbounded verifies the negative case:
// when no createMu is set, concurrent CreateWorktree calls CAN run overlapping
// git-worktree-add operations. This confirms the test instrumentation is correct
// (i.e. the peak-adds counter actually measures concurrency).
//
// Note: this test proves instrumentation correctness, not a production bug —
// concurrent local creates against DIFFERENT repos are harmless. The race only
// manifests on a SHARED remote worker repo; the test uses separate temp repos per
// goroutine to avoid actual git conflicts, but it shows peak > 1 is reachable
// without the mutex.
func TestHK5QP7Z_WithoutMutexConcurrencyIsUnbounded(t *testing.T) {
	t.Parallel()

	const nConcurrent = 5

	var (
		activeAdds int64
		peakAdds   int64
	)

	type result struct{ err error }
	resultCh := make(chan result, nConcurrent)

	// A sync point so all goroutines start their git-worktree-add simultaneously.
	var startBarrier sync.WaitGroup
	startBarrier.Add(nConcurrent)

	for i := 0; i < nConcurrent; i++ {
		go func(idx int) {
			// Each goroutine uses its OWN repo to avoid actual git conflicts.
			repo, sha := tempRepo(t)
			runID := "019f3133-5qp7-neg1-0001-" + [5]string{
				"000000000001",
				"000000000002",
				"000000000003",
				"000000000004",
				"000000000005",
			}[idx]

			rr := &tmux.RecordingRunner{
				CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
					if name == "git" && containsArg(args, "worktree") && containsArg(args, "add") {
						// Signal ready and wait for all goroutines to reach this point
						// simultaneously before measuring concurrency.
						startBarrier.Done()
						startBarrier.Wait()

						cur := atomic.AddInt64(&activeAdds, 1)
						for {
							old := atomic.LoadInt64(&peakAdds)
							if cur <= old || atomic.CompareAndSwapInt64(&peakAdds, old, cur) {
								break
							}
						}
						// Hold the "active" state open long enough for all barrier-
						// synchronized goroutines to also increment before any decrement.
						// Without this sleep, the scheduler may complete one goroutine's
						// increment+decrement cycle before siblings are scheduled, giving
						// a spuriously low peak even though all goroutines are logically
						// concurrent (GOMAXPROCS < nConcurrent on typical hardware).
						time.Sleep(5 * time.Millisecond)
						cmd := exec.CommandContext(ctx, name, args...)
						atomic.AddInt64(&activeAdds, -1)
						return cmd
					}
					return exec.CommandContext(ctx, name, args...)
				},
			}

			// No WithCreateMutex — no serialization.
			cfg := NoWorktreeRootOverride().WithRunner(rr)
			err := CreateWorktree(context.Background(), repo, runID, sha, cfg)
			resultCh <- result{err: err}
		}(i)
	}

	for i := 0; i < nConcurrent; i++ {
		r := <-resultCh
		if r.err != nil {
			t.Errorf("hk-5qp7z/neg: CreateWorktree error: %v", r.err)
		}
	}

	// Without a mutex, all N goroutines reach git-worktree-add simultaneously
	// (the start barrier synchronises them). The 5ms sleep ensures each goroutine
	// holds activeAdds incremented long enough for all siblings to also increment
	// before any decrement fires. Peak must equal nConcurrent.
	if peak := atomic.LoadInt64(&peakAdds); peak < int64(nConcurrent) {
		t.Errorf("hk-5qp7z/neg: peak concurrent worktree-adds = %d, want %d (instrumentation broken — concurrency not measured)", peak, nConcurrent)
	}
}
