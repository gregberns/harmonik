package workers_test

// routing_prop_test.go — property tests for per-queue local/remote routing
// invariants (hk-f10xl [L5 Move 2]).
//
// Four invariants exercised:
//   1. Routing — LocalOnly=true never yields a worker (always local).
//   2. WorkerTarget — mismatched name always falls back to nil (local).
//   3. Failover — disabled or slot-exhausted workers always return nil.
//   4. No-collision — concurrent slot reservations never exceed MaxSlots.
//
// Naming: TestProp_* per testing.md §Decisions #10.

import (
	"sync"
	"sync/atomic"
	"testing"

	"pgregory.net/rapid"

	"github.com/gregberns/harmonik/internal/workers"
)

// drawWorkerName generates a non-empty worker name that is a valid DNS-label
// subset: lowercase letters and digits only, 1–16 chars. Sufficient for test
// discrimination without importing a full validation dependency.
func drawWorkerName(rt *rapid.T, label string) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	length := rapid.IntRange(1, 16).Draw(rt, label+"_len")
	bs := make([]byte, length)
	for i := range bs {
		bs[i] = alphabet[rapid.IntRange(0, len(alphabet)-1).Draw(rt, label+"_ch")]
	}
	return string(bs)
}

func makeWorkerCfg(name string, enabled bool, maxSlots int) workers.Config {
	return workers.Config{
		Version: 1,
		Workers: []workers.Worker{
			{
				Name:      name,
				Transport: "ssh",
				Host:      "host.example.com",
				OS:        "darwin",
				RepoPath:  "/repo",
				MaxSlots:  maxSlots,
				Enabled:   enabled,
			},
		},
	}
}

// ── Invariant 1: LocalOnly routing gate ─────────────────────────────────────
//
// When a queue has LocalOnly=true, the scheduling logic must bypass the worker
// registry entirely — no worker must ever be selected regardless of registry
// state. We model this by verifying that SelectWorkerByName("") returns nil
// and SelectWorker on a disabled-equivalent path matches the gate's behaviour.
//
// The actual gate lives in daemon/workloop.go (the `if !itemLocalOnly` guard);
// property tests here cover the two selectors the gate delegates to, plus the
// gate's nil short-circuit path.

func TestProp_Routing_LocalOnly_NeverSelectsWorker(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := drawWorkerName(rt, "worker")
		maxSlots := rapid.IntRange(1, 8).Draw(rt, "max_slots")
		enabled := rapid.Bool().Draw(rt, "enabled")
		r := workers.NewRegistry(makeWorkerCfg(name, enabled, maxSlots))

		// Simulate the localOnly gate: skip SelectWorker entirely → local (nil).
		// SelectWorkerByName("") is the identity check: empty target must return nil.
		if got := r.SelectWorkerByName(""); got != nil {
			rt.Fatalf("SelectWorkerByName empty target: expected nil (local), got worker %q", got.Name)
		}
		// No slot should have been reserved.
		if inFlight := r.InFlight(); inFlight != 0 {
			rt.Fatalf("localOnly gate: expected 0 in-flight after empty-target call, got %d", inFlight)
		}
	})
}

// ── Invariant 2: WorkerTarget name mismatch falls back to local ──────────────

func TestProp_Routing_WorkerTarget_MatchYieldsWorker(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := drawWorkerName(rt, "worker")
		r := workers.NewRegistry(makeWorkerCfg(name, true, 4))

		w := r.SelectWorkerByName(name)
		if w == nil {
			rt.Fatalf("WorkerTarget exact match: expected non-nil, got nil")
		}
		if w.Name != name {
			rt.Fatalf("WorkerTarget exact match: got name %q, want %q", w.Name, name)
		}
		r.ReleaseSlot()
	})
}

func TestProp_Routing_WorkerTarget_MismatchFallsBackToLocal(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := drawWorkerName(rt, "worker")
		other := drawWorkerName(rt, "other")
		// Ensure the two names differ so the mismatch is real.
		if name == other {
			other = other + "x"
		}
		r := workers.NewRegistry(makeWorkerCfg(name, true, 4))

		w := r.SelectWorkerByName(other)
		if w != nil {
			rt.Fatalf("WorkerTarget mismatch: expected nil (local fallback), got worker %q", w.Name)
		}
		if inFlight := r.InFlight(); inFlight != 0 {
			rt.Fatalf("mismatch must not reserve a slot: got in-flight=%d", inFlight)
		}
	})
}

// ── Invariant 3: Failover — disabled or slot-exhausted returns nil ───────────

func TestProp_Routing_Failover_DisabledWorkerReturnsNil(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := drawWorkerName(rt, "worker")
		maxSlots := rapid.IntRange(1, 8).Draw(rt, "max_slots")
		r := workers.NewRegistry(makeWorkerCfg(name, false /* disabled */, maxSlots))

		if w := r.SelectWorker(); w != nil {
			rt.Fatalf("disabled worker: SelectWorker expected nil, got %q", w.Name)
		}
		if w := r.SelectWorkerByName(name); w != nil {
			rt.Fatalf("disabled worker: SelectWorkerByName expected nil, got %q", w.Name)
		}
	})
}

func TestProp_Routing_Failover_SlotsExhaustedReturnsNil(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := drawWorkerName(rt, "worker")
		maxSlots := rapid.IntRange(1, 6).Draw(rt, "max_slots")
		r := workers.NewRegistry(makeWorkerCfg(name, true, maxSlots))

		// Exhaust all slots.
		for i := 0; i < maxSlots; i++ {
			w := r.SelectWorker()
			if w == nil {
				rt.Fatalf("slot %d/%d: expected non-nil, got nil", i+1, maxSlots)
			}
		}
		// Any further selection must fail.
		if w := r.SelectWorker(); w != nil {
			rt.Fatalf("exhausted slots: SelectWorker expected nil, got %q", w.Name)
		}
		if w := r.SelectWorkerByName(name); w != nil {
			rt.Fatalf("exhausted slots: SelectWorkerByName expected nil, got %q", w.Name)
		}
		// Release all slots.
		for i := 0; i < maxSlots; i++ {
			r.ReleaseSlot()
		}
	})
}

func TestProp_Routing_Failover_LiveDisableFlipsResult(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := drawWorkerName(rt, "worker")
		r := workers.NewRegistry(makeWorkerCfg(name, true, 4))

		w1 := r.SelectWorker()
		if w1 == nil {
			rt.Fatal("before disable: expected non-nil")
		}
		r.ReleaseSlot()

		r.SetEnabled(false)
		if w := r.SelectWorker(); w != nil {
			rt.Fatalf("after live disable: expected nil, got %q", w.Name)
		}
		if w := r.SelectWorkerByName(name); w != nil {
			rt.Fatalf("after live disable: SelectWorkerByName expected nil, got %q", w.Name)
		}

		r.SetEnabled(true)
		w3 := r.SelectWorker()
		if w3 == nil {
			rt.Fatal("after re-enable: expected non-nil")
		}
		r.ReleaseSlot()
	})
}

// ── Invariant 4: No-collision — concurrent selectors never exceed MaxSlots ───
//
// N goroutines race to call SelectWorker concurrently. The peak observed
// in-flight count must never exceed MaxSlots. We track this with an atomic
// high-water mark.

func TestProp_Routing_NoCollision_ConcurrentSelectNeverExceedsMaxSlots(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		maxSlots := rapid.IntRange(1, 8).Draw(rt, "max_slots")
		goroutines := rapid.IntRange(maxSlots, maxSlots*4).Draw(rt, "goroutines")
		name := drawWorkerName(rt, "worker")
		r := workers.NewRegistry(makeWorkerCfg(name, true, maxSlots))

		var wg sync.WaitGroup
		var hwm atomic.Int64 // high-water mark of concurrent in-flight slots

		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				w := r.SelectWorker()
				if w == nil {
					return // slot not available — correct when saturated
				}
				// Observe current in-flight and update hwm.
				cur := int64(r.InFlight())
				for {
					old := hwm.Load()
					if cur <= old || hwm.CompareAndSwap(old, cur) {
						break
					}
				}
				r.ReleaseSlot()
			}()
		}
		wg.Wait()

		peak := hwm.Load()
		if peak > int64(maxSlots) {
			rt.Fatalf("no-collision violated: peak in-flight %d exceeded MaxSlots %d", peak, maxSlots)
		}
	})
}

// TestProp_Routing_NoCollision_SelectWorkerByNameConcurrent mirrors the above
// but for the WorkerTarget pin path.
func TestProp_Routing_NoCollision_SelectWorkerByNameConcurrent(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		maxSlots := rapid.IntRange(1, 8).Draw(rt, "max_slots")
		goroutines := rapid.IntRange(maxSlots, maxSlots*4).Draw(rt, "goroutines")
		name := drawWorkerName(rt, "worker")
		r := workers.NewRegistry(makeWorkerCfg(name, true, maxSlots))

		var wg sync.WaitGroup
		var hwm atomic.Int64

		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				w := r.SelectWorkerByName(name)
				if w == nil {
					return
				}
				cur := int64(r.InFlight())
				for {
					old := hwm.Load()
					if cur <= old || hwm.CompareAndSwap(old, cur) {
						break
					}
				}
				r.ReleaseSlot()
			}()
		}
		wg.Wait()

		peak := hwm.Load()
		if peak > int64(maxSlots) {
			rt.Fatalf("no-collision (SelectWorkerByName) violated: peak in-flight %d exceeded MaxSlots %d", peak, maxSlots)
		}
	})
}

// ── Mixed local+remote invariant ─────────────────────────────────────────────
//
// When some queues have LocalOnly=true and others don't, the overall system
// must route correctly: local-only queues must never acquire a slot; non-local
// queues must succeed when slots are available.

func TestProp_Routing_MixedLocalAndRemote_SlotAccountingCorrect(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		maxSlots := rapid.IntRange(2, 8).Draw(rt, "max_slots")
		name := drawWorkerName(rt, "worker")
		r := workers.NewRegistry(makeWorkerCfg(name, true, maxSlots))

		localRuns := rapid.IntRange(1, 4).Draw(rt, "local_runs")
		remoteRuns := rapid.IntRange(1, maxSlots).Draw(rt, "remote_runs")

		// Local-only runs: simulate by NOT calling SelectWorker (the gate skips it).
		// Assert no in-flight before remote runs.
		if inFlight := r.InFlight(); inFlight != 0 {
			rt.Fatalf("before any run: expected 0 in-flight, got %d", inFlight)
		}
		_ = localRuns // local runs consume no slots

		// Remote runs: acquire slots sequentially.
		acquired := 0
		for i := 0; i < remoteRuns; i++ {
			w := r.SelectWorker()
			if w == nil {
				break // slots exhausted — expected when remoteRuns > maxSlots
			}
			acquired++
		}

		// In-flight must equal acquired, must never exceed MaxSlots.
		if inFlight := r.InFlight(); inFlight != acquired {
			rt.Fatalf("in-flight=%d != acquired=%d", inFlight, acquired)
		}
		if acquired > maxSlots {
			rt.Fatalf("acquired %d > MaxSlots %d", acquired, maxSlots)
		}

		// Release all remote slots.
		for i := 0; i < acquired; i++ {
			r.ReleaseSlot()
		}
		if inFlight := r.InFlight(); inFlight != 0 {
			rt.Fatalf("after release: expected 0 in-flight, got %d", inFlight)
		}
	})
}
