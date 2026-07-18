package daemon_test

// l5saf_localonly_strand_test.go — regression test for hk-l5saf.
//
// # The bug (now fixed)
//
// The Step-2 split gate (workloop.go) admits in "remote bypass" mode when
// localInFlight >= gateMax but workerRegistry.HasFreeSlot()==true, expecting the
// selected bead to route to a remote worker. selectNextQueue could then pick a
// LOCAL-ONLY queue item. In the buggy code, Phase 3 stamped that item
// ItemStatusDispatched + a placeholder RunID and PERSISTED queue.json; only THEN
// did a secondary local-cap guard (capturedQueueLocalOnly && localInFlight >=
// gateMax) fire sleep+continue WITHOUT reverting the stamp. The item was left
// Dispatched with no run/goroutine — never re-selected (only Pending is
// projected), wedging the group until daemon restart.
//
// # The fix
//
// The guard was HOISTED to BEFORE the Phase-3 stamp (search workloop.go for
// hk-l5saf). In the bypass + local-only + saturated case the item is now deferred
// WITHOUT ever being stamped — it stays ItemStatusPending.
//
// # Why the sibling test missed it
//
// split_gate_hkhs7ex_test.go's TestSplitGate_LocalOnlyBypassFix only mirrors the
// boolean guard condition in the test body; it never runs the work loop, so it
// cannot observe the persisted queue-item state that the stranding bug corrupts.
// This test drives one real tick of runWorkLoop and asserts on the item status.
//
// Bead ref: hk-l5saf.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/workers"
)

// TestL5saf_LocalOnlyItemNotStrandedByCapGuard drives one tick of the real work
// loop with a saturated local sub-cap, a worker that has a free slot (remote
// bypass), and a LOCAL-ONLY queue holding a single Pending item. After the tick
// the item MUST still be ItemStatusPending (deferred, not stamped): the fixed
// pre-stamp guard defers without ever writing ItemStatusDispatched + a
// placeholder RunID. The pre-fix code would leave it Dispatched with a nil run,
// stranding it — which this test would catch.
//
// Bead ref: hk-l5saf.
func TestL5saf_LocalOnlyItemNotStrandedByCapGuard(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-l5saf-localonly-bead"
	const gateMax = 1 // MaxConcurrent=1 → effectiveMax=1 → gateMax=1

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// LOCAL-ONLY queue: one Active group, one Pending item. LocalOnly=true makes
	// selectNextQueue set capturedQueueLocalOnly=true for the picked item.
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "l5saf-queue-id",
		// Left unnamed → normalised to the default "main" slot, so the
		// backward-compatible QueueStore.Queue() accessor returns it below.
		LocalOnly:   true,
		SubmittedAt: time.Now().UTC(),
		Status:      queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			CreatedAt:  time.Now().UTC(),
			Items: []queue.Item{{
				BeadID: beadID,
				Status: queue.ItemStatusPending,
			}},
		}},
	}
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	// Worker with a free slot → HasFreeSlot()==true drives the remote-bypass
	// branch of the Step-2 split gate.
	reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
		Name:     "gb-mbp",
		Host:     "gb-mbp.local",
		Enabled:  true,
		MaxSlots: 6,
	}}})

	// Empty ledger: NoAutoPull=true means the br-ready fallback never fires, so
	// dispatch input comes exclusively from the local-only queue above.
	ledger := &countingLedger{readyResult: []core.BeadRecord{}}
	bus := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:       qs,
		WorkerRegistry:   reg,
		MaxConcurrent:    gateMax,
		NoAutoPull:       true,
	})

	// Preload local saturation: localInFlight == gateMax. The split gate then
	// admits ONLY via the remote-bypass branch (HasFreeSlot), setting up the
	// exact condition the secondary local-cap guard must handle.
	daemon.ExportedStoreLocalInFlight(deps, gateMax)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Observe for several poll ticks, then snapshot the queue WHILE THE LOOP IS
	// STILL ALIVE. In the buggy code the FIRST tick stamps the item Dispatched +
	// placeholder RunID and persists; the fixed code defers it pre-stamp so it
	// stays Pending. We must read before cancelling: the shutdown-drain
	// (drainCancelledQueue) transitions active queues to cancelled and clears the
	// in-memory store on ctx-cancel, which would erase the state under test.
	time.Sleep(600 * time.Millisecond)
	got := qs.Queue()

	cancel()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("l5saf: work loop did not exit within 5s after context cancel")
	}

	// The guard must have deferred WITHOUT ever claiming/dispatching the bead.
	if claims := ledger.claimCalls.Load(); claims != 0 {
		t.Fatalf("l5saf: ClaimBead called %d time(s); want 0 — the local-only item "+
			"should have been deferred pre-stamp, never dispatched", claims)
	}

	// Assert the mid-flight snapshot: the local-only item was NOT stranded.
	if got == nil {
		t.Fatal("l5saf: QueueStore.Queue() returned nil mid-flight — queue was not loaded")
	}
	if len(got.Groups) != 1 || len(got.Groups[0].Items) != 1 {
		t.Fatalf("l5saf: unexpected queue shape: %+v", got)
	}
	item := got.Groups[0].Items[0]

	if item.Status == queue.ItemStatusDispatched {
		t.Fatalf("l5saf REGRESSION: local-only item was stamped %q with run_id=%v "+
			"then deferred without revert — stranded forever (bug hk-l5saf). "+
			"Want %q (deferred pre-stamp).",
			item.Status, item.RunID, queue.ItemStatusPending)
	}
	if item.Status != queue.ItemStatusPending {
		t.Fatalf("l5saf: item status = %q, want %q (the guard must defer the "+
			"local-only item without stamping it)", item.Status, queue.ItemStatusPending)
	}
	if item.RunID != nil {
		t.Fatalf("l5saf: pending item carries a non-nil RunID %v — a run was "+
			"stamped where none should exist", *item.RunID)
	}

	// Corroborate: the split gate never routed remotely (SelectWorker not called),
	// and the local counter is untouched.
	if inFlight := reg.InFlight(); inFlight != 0 {
		t.Fatalf("l5saf: workerRegistry.InFlight()=%d, want 0 — SelectWorker was "+
			"called for a local-only item that should have been deferred", inFlight)
	}
}
