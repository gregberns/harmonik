package core

// Stress tests for TransitionIDGenerator per execution-model.md §4.4 EM-018a.
//
// Coverage matrix:
//
//	TestTransitionID_IntraProcessMonotonicity  — 100 000-iteration tight-loop; EM-018a
//	TestTransitionID_SameMillisecondLoad       — same-ms injection at stress volume; EM-018a tiebreaker
//	TestTransitionID_ConcurrentMonotonicity    — N goroutines × M calls; no duplicates; EM-018a
//	TestTransitionID_CrossRestart              — generator state-rewind simulating daemon restart; EM-018a
//	TestTransitionID_ClockRegressionStress     — repeated rollback injections; EM-018a RFC 9562 §6.2 method 1
//	TestTransitionID_ShapeConformance          — every output is a valid UUIDv7; EM-018a
//
// Helper prefix: transitionIDFixture (per implementer-protocol.md helper-prefix discipline, bead hk-b3f.23).

import (
	"bytes"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// transitionIDFixtureLT returns true when a is strictly less than b as big-endian 128-bit unsigned integers.
func transitionIDFixtureLT(a, b uuid.UUID) bool {
	for i := 0; i < 16; i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false // equal
}

// transitionIDFixtureSortAndDedup sorts ids in-place (lexicographic) and returns the first
// pair of indices where ids[i] >= ids[i+1] after sorting; (-1, -1) means all strictly
// increasing. Used by the concurrent test to report collision context.
func transitionIDFixtureSortAndDedup(ids []TransitionID) (lo, hi int) {
	sort.Slice(ids, func(i, j int) bool {
		ui := uuid.UUID(ids[i])
		uj := uuid.UUID(ids[j])
		return bytes.Compare(ui[:], uj[:]) < 0
	})
	for i := 1; i < len(ids); i++ {
		prev := uuid.UUID(ids[i-1])
		curr := uuid.UUID(ids[i])
		if bytes.Compare(prev[:], curr[:]) >= 0 {
			return i - 1, i
		}
	}
	return -1, -1
}

// TestTransitionID_IntraProcessMonotonicity generates 100 000 consecutive TransitionIDs
// from a single TransitionIDGenerator backed by the real wall clock and asserts each is
// strictly greater than the previous.
//
// Spec: execution-model.md §4.4 EM-018a.
func TestTransitionID_IntraProcessMonotonicity(t *testing.T) {
	const n = 100_000

	g := NewTransitionIDGenerator()

	prev, err := g.Next()
	if err != nil {
		t.Fatalf("EM-018a stress: first Next(): %v", err)
	}

	for i := 1; i < n; i++ {
		curr, err := g.Next()
		if err != nil {
			t.Fatalf("EM-018a stress: Next() at iteration %d: %v", i, err)
		}
		up := uuid.UUID(prev)
		uc := uuid.UUID(curr)
		if bytes.Compare(up[:], uc[:]) >= 0 {
			t.Fatalf("EM-018a stress: intra-process monotonicity violated at iteration %d: prev=%v >= curr=%v",
				i, prev, curr)
		}
		prev = curr
	}
}

// TestTransitionID_SameMillisecondLoad injects a fixed UUIDv7 value for every newV7
// call, simulating 50 000 back-to-back same-millisecond emissions. Asserts strict
// monotonicity is maintained via the increment path throughout.
//
// Spec: execution-model.md §4.4 EM-018a tiebreaker under same-ms load.
func TestTransitionID_SameMillisecondLoad(t *testing.T) {
	const n = 50_000

	fixed, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("EM-018a same-ms load: uuid.NewV7(): %v", err)
	}

	g := &TransitionIDGenerator{
		newV7: func() (uuid.UUID, error) { return fixed, nil },
	}

	prev, err := g.Next()
	if err != nil {
		t.Fatalf("EM-018a same-ms load: first Next(): %v", err)
	}

	for i := 1; i < n; i++ {
		curr, err := g.Next()
		if err != nil {
			t.Fatalf("EM-018a same-ms load: Next() at iteration %d: %v", i, err)
		}
		up := uuid.UUID(prev)
		uc := uuid.UUID(curr)
		if bytes.Compare(up[:], uc[:]) >= 0 {
			t.Fatalf("EM-018a same-ms load: monotonicity violated at iteration %d: prev=%v >= curr=%v",
				i, prev, curr)
		}
		prev = curr
	}
}

// TestTransitionID_ConcurrentMonotonicity spawns runtime.NumCPU() goroutines (min 4,
// max 8), each calling Next() 5 000 times on a shared TransitionIDGenerator. Collects
// all outputs, asserts there are no duplicates and no pair (i, i+1) in sorted order
// where ids[i] >= ids[i+1].
//
// On failure the test reports the two colliding/inverted UUIDs and their sorted positions.
//
// Spec: execution-model.md §4.4 EM-018a.
func TestTransitionID_ConcurrentMonotonicity(t *testing.T) {
	numG := runtime.NumCPU()
	if numG < 4 {
		numG = 4
	}
	if numG > 8 {
		numG = 8
	}
	const callsEach = 5_000

	total := numG * callsEach
	g := NewTransitionIDGenerator()

	errCh := make(chan error, numG)

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		all = make([]TransitionID, 0, total)
	)

	for i := 0; i < numG; i++ {
		wg.Add(1)
		go func(goroutineIdx int) {
			defer wg.Done()
			local := make([]TransitionID, 0, callsEach)
			for j := 0; j < callsEach; j++ {
				id, err := g.Next()
				if err != nil {
					errCh <- fmt.Errorf("EM-018a concurrent: goroutine %d call %d: %w",
						goroutineIdx, j, err)
					return
				}
				local = append(local, id)
			}
			mu.Lock()
			all = append(all, local...)
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("EM-018a concurrent: %v", err)
	}

	if len(all) != total {
		t.Fatalf("EM-018a concurrent: collected %d IDs, want %d", len(all), total)
	}

	if lo, hi := transitionIDFixtureSortAndDedup(all); lo != -1 {
		t.Fatalf("EM-018a concurrent: duplicate or inversion in sorted output at positions [%d,%d]: %v >= %v",
			lo, hi, all[lo], all[hi])
	}
}

// TestTransitionID_CrossRestart simulates a daemon restart by:
//  1. Running a generator for a few calls to obtain the "last emitted" value.
//  2. Recording that value as the simulated HWM (what EM-018a requires the daemon to seed).
//  3. Constructing a new generator seeded with that HWM value (simulating what the daemon
//     does on startup: "ensure every new transition_id is strictly greater than the HWM").
//  4. Calling Next() on the new generator with a newV7 that returns a value LESS than the
//     HWM (simulating a clock regression post-restart).
//  5. Asserting the first post-restart TransitionID is strictly greater than the pre-restart HWM.
//
// The TransitionIDGenerator.last field is package-accessible (unexported, same package).
//
// Spec: execution-model.md §4.4 EM-018a.
func TestTransitionID_CrossRestart(t *testing.T) {
	// Phase 1: pre-restart process — generate a few IDs.
	preRestart := NewTransitionIDGenerator()
	var hwm TransitionID
	for i := 0; i < 10; i++ {
		id, err := preRestart.Next()
		if err != nil {
			t.Fatalf("EM-018a cross-restart: pre-restart Next() %d: %v", i, err)
		}
		hwm = id
	}

	// Phase 2: simulate daemon restart.
	// Build a new generator seeded with the HWM (as a daemon startup would do).
	// The generator's `last` field is the mechanism: set it to the HWM so that
	// any fresh UUID that is not strictly greater triggers the increment path.
	postRestart := &TransitionIDGenerator{
		last: uuid.UUID(hwm),
		// Inject a newV7 that returns a value strictly less than the HWM
		// (simulating NTP regression or VM pause/resume post-restart).
		newV7: func() (uuid.UUID, error) {
			// Decrement the HWM by 1 to produce a "regressed" clock value.
			regressed := uuid.UUID(hwm)
			for i := 15; i >= 0; i-- {
				if regressed[i] > 0 {
					regressed[i]--
					break
				}
				regressed[i] = 0xff
			}
			return regressed, nil
		},
	}

	firstPost, err := postRestart.Next()
	if err != nil {
		t.Fatalf("EM-018a cross-restart: post-restart Next(): %v", err)
	}

	hwmU := uuid.UUID(hwm)
	postU := uuid.UUID(firstPost)
	if !transitionIDFixtureLT(hwmU, postU) {
		t.Fatalf("EM-018a cross-restart: first post-restart TransitionID (%v) is not strictly greater than HWM (%v)",
			firstPost, hwm)
	}
}

// TestTransitionID_ClockRegressionStress verifies that 10 000 consecutive clock
// regressions (where newV7 always returns a value strictly less than the current last)
// are each resolved by the increment path, maintaining strict monotonicity throughout.
//
// Spec: execution-model.md §4.4 EM-018a / RFC 9562 §6.2 method 1.
func TestTransitionID_ClockRegressionStress(t *testing.T) {
	const n = 10_000

	// Seed the generator with a known starting value.
	seed, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("EM-018a clock-regression stress: uuid.NewV7() seed: %v", err)
	}

	// callCount tracks how many times the injected newV7 has been called.
	// On call k, return seed decremented by k so the clock always appears to
	// roll back relative to the current last value.
	var callCount int
	var mu sync.Mutex

	g := &TransitionIDGenerator{
		newV7: func() (uuid.UUID, error) {
			mu.Lock()
			k := callCount
			callCount++
			mu.Unlock()
			// Return seed - (k+1), which is always strictly less than seed,
			// ensuring the clock regression path fires on every single call.
			v := seed
			for sub := k + 1; sub > 0; sub-- {
				for i := 15; i >= 0; i-- {
					if v[i] > 0 {
						v[i]--
						break
					}
					v[i] = 0xff
				}
			}
			return v, nil
		},
	}

	prev, err := g.Next()
	if err != nil {
		t.Fatalf("EM-018a clock-regression stress: first Next(): %v", err)
	}

	for i := 1; i < n; i++ {
		curr, err := g.Next()
		if err != nil {
			t.Fatalf("EM-018a clock-regression stress: Next() at iteration %d: %v", i, err)
		}
		up := uuid.UUID(prev)
		uc := uuid.UUID(curr)
		if bytes.Compare(up[:], uc[:]) >= 0 {
			t.Fatalf("EM-018a clock-regression stress: monotonicity violated at iteration %d: prev=%v >= curr=%v",
				i, prev, curr)
		}
		prev = curr
	}
}

// TestTransitionID_ShapeConformance generates 1 000 TransitionIDs via the real
// wall-clock generator and asserts every output passes TransitionID.IsUUIDv7().
//
// Note: in the clock-rollback path the version nibble MAY be perturbed (per the
// generator's documented RFC 9562 §6.2 method 1 allowance), so this test uses
// the normal (non-injected) generator to exercise the conformance path under real
// wall-clock advancement.
//
// Spec: execution-model.md §4.4 EM-018a.
func TestTransitionID_ShapeConformance(t *testing.T) {
	const n = 1_000

	g := NewTransitionIDGenerator()

	for i := 0; i < n; i++ {
		id, err := g.Next()
		if err != nil {
			t.Fatalf("EM-018a shape: Next() at iteration %d: %v", i, err)
		}
		if !id.IsUUIDv7() {
			t.Fatalf("EM-018a shape: iteration %d produced non-v7 TransitionID: %v", i, id)
		}
	}
}
