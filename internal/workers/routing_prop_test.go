package workers_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"pgregory.net/rapid"

	"github.com/gregberns/harmonik/internal/workers"
)

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

func TestProp_Routing_LocalOnly_NeverSelectsWorker(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := drawWorkerName(rt, "worker")
		maxSlots := rapid.IntRange(1, 8).Draw(rt, "max_slots")
		enabled := rapid.Bool().Draw(rt, "enabled")
		r := workers.NewRegistry(makeWorkerCfg(name, enabled, maxSlots))

		if got := r.SelectWorkerByName(""); got != nil {
			rt.Fatalf("SelectWorkerByName empty target: expected nil (local), got worker %q", got.Name)
		}
		if inFlight := r.InFlight(); inFlight != 0 {
			rt.Fatalf("localOnly gate: expected 0 in-flight after empty-target call, got %d", inFlight)
		}
	})
}

func TestProp_Routing_WorkerTarget_MatchYieldsWorker(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := drawWorkerName(rt, "worker")
		r := workers.NewRegistry(makeWorkerCfg(name, true, 4))

		w := r.SelectWorkerByName(name)
		if w == nil {
			rt.Fatalf("WorkerTarget exact match: expected non-nil, got nil")
			return
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
		if name == other {
			other += "x"
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

		for i := 0; i < maxSlots; i++ {
			w := r.SelectWorker()
			if w == nil {
				rt.Fatalf("slot %d/%d: expected non-nil, got nil", i+1, maxSlots)
			}
		}
		if w := r.SelectWorker(); w != nil {
			rt.Fatalf("exhausted slots: SelectWorker expected nil, got %q", w.Name)
		}
		if w := r.SelectWorkerByName(name); w != nil {
			rt.Fatalf("exhausted slots: SelectWorkerByName expected nil, got %q", w.Name)
		}
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

func TestProp_Routing_MixedLocalAndRemote_SlotAccountingCorrect(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		maxSlots := rapid.IntRange(2, 8).Draw(rt, "max_slots")
		name := drawWorkerName(rt, "worker")
		r := workers.NewRegistry(makeWorkerCfg(name, true, maxSlots))

		localRuns := rapid.IntRange(1, 4).Draw(rt, "local_runs")
		remoteRuns := rapid.IntRange(1, maxSlots).Draw(rt, "remote_runs")

		if inFlight := r.InFlight(); inFlight != 0 {
			rt.Fatalf("before any run: expected 0 in-flight, got %d", inFlight)
		}
		_ = localRuns // local runs consume no slots

		acquired := 0
		for i := 0; i < remoteRuns; i++ {
			w := r.SelectWorker()
			if w == nil {
				break // slots exhausted — expected when remoteRuns > maxSlots
			}
			acquired++
		}

		if inFlight := r.InFlight(); inFlight != acquired {
			rt.Fatalf("in-flight=%d != acquired=%d", inFlight, acquired)
		}
		if acquired > maxSlots {
			rt.Fatalf("acquired %d > MaxSlots %d", acquired, maxSlots)
		}

		for i := 0; i < acquired; i++ {
			r.ReleaseSlot()
		}
		if inFlight := r.InFlight(); inFlight != 0 {
			rt.Fatalf("after release: expected 0 in-flight, got %d", inFlight)
		}
	})
}
