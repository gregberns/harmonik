package daemon_test

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

	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
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

	reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
		Name:     "gb-mbp",
		Host:     "gb-mbp.local",
		Enabled:  true,
		MaxSlots: 6,
	}}})

	ledger := &countingLedger{readyResult: []core.BeadRecord{}}
	bus := &stubEventCollector{}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
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

	daemon.ExportedStoreLocalInFlight(deps, gateMax)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel, error unactionable here
	}()

	time.Sleep(600 * time.Millisecond)
	got := qs.Queue()

	cancel()
	awaitLoopTeardown(t, loopDone, "work loop")

	if claims := ledger.claimCalls.Load(); claims != 0 {
		t.Fatalf("l5saf: ClaimBead called %d time(s); want 0 — the local-only item "+
			"should have been deferred pre-stamp, never dispatched", claims)
	}

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

	if inFlight := reg.InFlight(); inFlight != 0 {
		t.Fatalf("l5saf: workerRegistry.InFlight()=%d, want 0 — SelectWorker was "+
			"called for a local-only item that should have been deferred", inFlight)
	}
}
