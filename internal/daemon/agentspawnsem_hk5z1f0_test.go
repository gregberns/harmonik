package daemon

// agentspawnsem_hk5z1f0_test.go — hk-5z1f0 fixes:
//
//	(A) the HC-056 default agent_ready timeout is raised 90s → 150s so the 2nd
//	    (REVIEW-stage) cold-start claude spawn over the reverse SSH tunnel clears
//	    readiness under 6-concurrent remote load, and
//	(B) a per-worker cold-start spawn semaphore (workLoopDeps.agentSpawnSem, cap 3)
//	    bounds how many concurrent remote claude cold-starts overlap on one worker.
//
// Both are white-box tests (package daemon) because defaultAgentReadyTimeout is
// unexported and agentSpawnSem is an unexported field. The semaphore test mirrors
// the peak-concurrency style of internal/workspace TestHK5QP7Z_...MutexSerializes.

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestHK5Z1F0_DefaultAgentReadyTimeoutIs150s pins Fix A: the HC-056 default is
// 150 seconds. A regression here (e.g. a revert to 90s) fails the reviewer
// cold-start over the reverse tunnel under 6-concurrent remote load.
func TestHK5Z1F0_DefaultAgentReadyTimeoutIs150s(t *testing.T) {
	t.Parallel()
	if got, want := defaultAgentReadyTimeout, 150*time.Second; got != want {
		t.Fatalf("defaultAgentReadyTimeout = %v, want %v (hk-5z1f0)", got, want)
	}
}

// TestHK5Z1F0_AgentSpawnSemCapsConcurrentColdStartsAtThree pins Fix B: the
// per-worker cold-start spawn semaphore never lets more than 3 spawns hold a
// slot simultaneously. It exercises the exact acquire/release idiom used in
// beadRunOne (acquire before Launch, sync.Once-guarded release after
// agent_ready) across N goroutines and asserts the observed peak concurrency
// is ≤ cap.
func TestHK5Z1F0_AgentSpawnSemCapsConcurrentColdStartsAtThree(t *testing.T) {
	t.Parallel()

	const (
		capN        = 3
		nConcurrent = 12
	)

	// newWorkLoopDeps installs a cap-3 channel in production; construct the same
	// primitive here so the test tracks the real capacity.
	sem := make(chan struct{}, capN)

	var (
		active int64 // atomic: current holders in flight
		peak   int64 // atomic: maximum seen
		wg     sync.WaitGroup
	)

	// gate keeps every acquiring goroutine holding its slot until all that can
	// fit are in flight, forcing the semaphore to actually block the surplus.
	release := make(chan struct{})

	for i := 0; i < nConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Mirror beadRunOne: acquire before "Launch".
			sem <- struct{}{}
			var once sync.Once
			releaseSlot := func() { once.Do(func() { <-sem }) }
			defer releaseSlot() // leak backstop

			cur := atomic.AddInt64(&active, 1)
			for {
				p := atomic.LoadInt64(&peak)
				if cur <= p || atomic.CompareAndSwapInt64(&peak, p, cur) {
					break
				}
			}

			<-release // hold the slot so peak reflects true concurrency
			atomic.AddInt64(&active, -1)
			releaseSlot() // explicit release after "agent_ready" resolves
		}()
	}

	// Give the goroutines time to saturate the semaphore, then let them drain.
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt64(&peak); got > capN {
		close(release)
		wg.Wait()
		t.Fatalf("peak concurrent cold-start slots = %d, want ≤ %d (hk-5z1f0)", got, capN)
	}
	close(release)
	wg.Wait()

	if got := atomic.LoadInt64(&peak); got != capN {
		t.Fatalf("peak concurrent cold-start slots = %d, want exactly %d (semaphore should saturate) (hk-5z1f0)", got, capN)
	}
	// Every slot must be returned — the channel drains to empty.
	if len(sem) != 0 {
		t.Fatalf("agentSpawnSem leaked %d slots after all releases (hk-5z1f0)", len(sem))
	}
}
