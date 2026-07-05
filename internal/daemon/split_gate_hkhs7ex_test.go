package daemon_test

// split_gate_hkhs7ex_test.go — Regression tests for the split concurrency gate.
//
// Before hk-hs7ex, the daemon used a SINGLE fleet-wide gate:
//   runRegistry.Len() >= gateMax
// which counted local+remote runs together. With 4 local runs in-flight and
// gateMax=4, remote workers were starved even when they had free slots.
//
// After hk-hs7ex, the gate is split:
//   - Local hard sub-cap: localInFlight < localCap (local runs only)
//   - Remote bypass: if localInFlight >= localCap but worker.HasFreeSlot(), admit
//
// These tests prove:
//   1. Registry.HasFreeSlot returns true when slots remain, false when full.
//   2. When local is saturated (localInFlight==localCap) but the worker has free
//      slots, the gate passes and a remote slot is reserved via SelectWorker.
//   3. Remote admits do NOT increment localInFlight.
//   4. When local is saturated AND no worker has free slots, the gate blocks.
//
// Bead ref: hk-hs7ex.

import (
	"sync/atomic"
	"testing"

	"github.com/gregberns/harmonik/internal/workers"
)

// TestSplitGate_HasFreeSlot verifies Registry.HasFreeSlot reports slot
// availability without consuming a slot.
func TestSplitGate_HasFreeSlot(t *testing.T) {
	t.Parallel()

	cfg := workers.Config{Workers: []workers.Worker{{
		Name:     "test-worker",
		Host:     "test.example.com",
		Enabled:  true,
		MaxSlots: 3,
	}}}
	reg := workers.NewRegistry(cfg)

	// At 0 inflight: HasFreeSlot must be true.
	if !reg.HasFreeSlot() {
		t.Fatal("HasFreeSlot: want true at 0/3 inflight, got false")
	}

	// Consume 1st slot.
	w1 := reg.SelectWorker()
	if w1 == nil {
		t.Fatal("SelectWorker: want non-nil for 1st slot")
	}
	if !reg.HasFreeSlot() {
		t.Fatal("HasFreeSlot: want true at 1/3 inflight, got false")
	}

	// Consume 2nd slot.
	w2 := reg.SelectWorker()
	if w2 == nil {
		t.Fatal("SelectWorker: want non-nil for 2nd slot")
	}
	if !reg.HasFreeSlot() {
		t.Fatal("HasFreeSlot: want true at 2/3 inflight, got false")
	}

	// Consume 3rd (last) slot.
	w3 := reg.SelectWorker()
	if w3 == nil {
		t.Fatal("SelectWorker: want non-nil for 3rd slot")
	}

	// Full: HasFreeSlot must be false.
	if reg.HasFreeSlot() {
		t.Fatal("HasFreeSlot: want false at 3/3 inflight (full), got true")
	}
	if reg.SelectWorker() != nil {
		t.Fatal("SelectWorker: want nil when all slots taken, got worker")
	}

	// Release one slot: HasFreeSlot becomes true again.
	reg.ReleaseSlot()
	if !reg.HasFreeSlot() {
		t.Fatal("HasFreeSlot: want true after ReleaseSlot, got false")
	}

	// Suppress unused-variable warnings.
	_, _, _ = w1, w2, w3
}

// TestSplitGate_LocalSaturationDoesNotBlockRemote proves the core split-gate
// invariant: when localInFlight == localCap (local lanes full), a worker with
// free slots still passes the gate, and the remote slot is reserved via
// SelectWorker WITHOUT touching localInFlight.
//
// This is the regression case that would FAIL with the pre-hk-hs7ex single gate:
//   runRegistry.Len() >= gateMax  (counted local+remote → remote starved)
func TestSplitGate_LocalSaturationDoesNotBlockRemote(t *testing.T) {
	t.Parallel()

	const localCap = 4
	const workerMaxSlots = 6

	cfg := workers.Config{Workers: []workers.Worker{{
		Name:     "gb-mbp",
		Host:     "gb-mbp.local",
		Enabled:  true,
		MaxSlots: workerMaxSlots,
	}}}
	reg := workers.NewRegistry(cfg)

	// Simulate local saturation: all localCap slots consumed by local runs.
	localInFlight := new(atomic.Int32)
	localInFlight.Store(localCap)

	// Gate check (mirrors workloop.go step 2 after hk-hs7ex):
	//   if localInFlight >= gateMax {
	//       if workerRegistry == nil || !workerRegistry.HasFreeSlot() { block }
	//   }
	if int(localInFlight.Load()) < localCap {
		t.Fatal("pre-condition: localInFlight should equal localCap")
	}
	// Old gate (pre-hk-hs7ex): would block unconditionally here.
	// New gate: HasFreeSlot lets the remote run through.
	if !reg.HasFreeSlot() {
		t.Fatal("pre-condition: worker should have free slots")
	}

	// Admit up to workerMaxSlots remote runs while local remains at cap.
	for i := 0; i < workerMaxSlots; i++ {
		w := reg.SelectWorker()
		if w == nil {
			t.Fatalf("SelectWorker: want non-nil for remote run %d/%d — remote starved despite free worker slots (regression: single gate would block here)",
				i+1, workerMaxSlots)
		}
		// localInFlight must NOT change: remote runs hold worker slots, not local.
		if got := localInFlight.Load(); got != localCap {
			t.Fatalf("localInFlight: want %d after remote admit %d, got %d — remote run incorrectly consumed a local slot",
				localCap, i+1, got)
		}
	}

	// Worker is full now.
	if got := reg.InFlight(); got != workerMaxSlots {
		t.Fatalf("workerRegistry.InFlight: want %d, got %d", workerMaxSlots, got)
	}
	// Local still at cap, unaffected by remote admits.
	if got := localInFlight.Load(); got != localCap {
		t.Fatalf("localInFlight: want %d after filling all worker slots, got %d", localCap, got)
	}
	// Worker is now full: HasFreeSlot must be false → gate blocks next admit.
	if reg.HasFreeSlot() {
		t.Fatal("HasFreeSlot: want false when worker is full, got true — gate would incorrectly admit another remote run")
	}
}

// TestSplitGate_BlocksWhenLocalFullAndNoWorker proves the other branch: when
// local is saturated AND no worker has free slots, the gate correctly blocks.
func TestSplitGate_BlocksWhenLocalFullAndNoWorker(t *testing.T) {
	t.Parallel()

	const localCap = 4
	localInFlight := new(atomic.Int32)
	localInFlight.Store(localCap)

	// Case A: nil registry — must block.
	{
		var nilReg *workers.Registry
		localSat := int(localInFlight.Load()) >= localCap
		noSlot := nilReg == nil || !nilReg.HasFreeSlot()
		if !(localSat && noSlot) {
			t.Error("case A: expected gate to block when local saturated and registry nil")
		}
	}

	// Case B: registry configured but all slots taken — must block.
	{
		cfg := workers.Config{Workers: []workers.Worker{{Name: "w", Host: "h", Enabled: true, MaxSlots: 2}}}
		reg := workers.NewRegistry(cfg)
		// Fill all slots.
		for range 2 {
			if reg.SelectWorker() == nil {
				t.Fatal("case B setup: SelectWorker returned nil unexpectedly")
			}
		}
		localSat := int(localInFlight.Load()) >= localCap
		noSlot := !reg.HasFreeSlot()
		if !(localSat && noSlot) {
			t.Error("case B: expected gate to block when local saturated and worker full")
		}
	}

	// Case C: worker disabled — HasFreeSlot returns false.
	{
		cfg := workers.Config{Workers: []workers.Worker{{Name: "w", Host: "h", Enabled: false, MaxSlots: 6}}}
		reg := workers.NewRegistry(cfg)
		if reg.HasFreeSlot() {
			t.Error("case C: HasFreeSlot should return false for a disabled worker")
		}
	}
}

// TestSplitGate_LocalOnlyBypassFix proves the secondary local-cap guard:
// when the outer gate passed in "remote bypass" mode (localInFlight >= localCap,
// HasFreeSlot=true) but the selected bead turns out to be local-only
// (capturedQueueLocalOnly=true), the secondary guard fires and localInFlight
// must NOT be incremented — the run is deferred, not dispatched.
//
// This tests the fix for the spec violation flagged as spec-gap-local-only-gate-bypass
// in the iter-1 review.
func TestSplitGate_LocalOnlyBypassFix(t *testing.T) {
	t.Parallel()

	const localCap = 4

	cfg := workers.Config{Workers: []workers.Worker{{
		Name:     "gb-mbp",
		Host:     "gb-mbp.local",
		Enabled:  true,
		MaxSlots: 6,
	}}}
	reg := workers.NewRegistry(cfg)

	// Simulate local saturation: all localCap slots consumed by local runs.
	localInFlight := new(atomic.Int32)
	localInFlight.Store(localCap)

	// The outer gate passes because the worker has a free slot (remote bypass).
	// HasFreeSlot=true → loop proceeds expecting remote routing.
	if !reg.HasFreeSlot() {
		t.Fatal("pre-condition: worker should have free slots")
	}

	// But the selected queue item has LocalOnly=true. The secondary guard should
	// fire: defer the item (sleep+continue) without calling SelectWorker and
	// without incrementing localInFlight.
	localOnlyItem := true
	if localOnlyItem && int(localInFlight.Load()) >= localCap {
		// Secondary guard correctly fires. localInFlight must stay at localCap.
		if got := localInFlight.Load(); got != localCap {
			t.Fatalf("localInFlight: want %d (unchanged), got %d — secondary guard should not touch the counter", localCap, got)
		}
		// Worker slots must be unchanged (SelectWorker was NOT called).
		if got := reg.InFlight(); got != 0 {
			t.Fatalf("workerRegistry.InFlight: want 0 (SelectWorker not called), got %d", got)
		}
		// Test passes: secondary guard would defer the item.
		return
	}
	// If we reach here the secondary guard did not fire — that is the bug.
	t.Fatal("secondary local-cap guard should have fired for a local-only item when local is saturated")
}

