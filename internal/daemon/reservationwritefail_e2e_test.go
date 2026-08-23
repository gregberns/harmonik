package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/workers"
)

// The load-bearing claim of the reservation transaction is that a dispatch
// whose reservation write does not reach disk never happens: no bead claim, no
// launch. reserveQueueItem's own tests pin the verdict it returns; this one
// drives the real work loop and pins what the loop DOES with that verdict,
// which is the part a wrong `continue` would break silently.
//
// Decision D3: "no more launching and hoping."
//
// Spec ref: specs/queue-model.md §3.1 QM-001.
func TestReservationWriteFailure_NeverClaimsAndNeverLaunches(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-reservation-writefail-bead"

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		SubmittedAt:   time.Now().UTC(),
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			CreatedAt:  time.Now().UTC(),
			Items:      []queue.Item{{BeadID: beadID, Status: queue.ItemStatusPending}},
		}},
	}
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o700); err != nil {
		t.Fatalf("mkdir queues: %v", err)
	}
	if err := os.Chmod(queuesDir, 0o500); err != nil { //nolint:gosec // the test needs a readable but non-writable directory
		t.Fatalf("chmod queues dir: %v", err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(queuesDir, 0o700); chmodErr != nil { //nolint:gosec // restoring so t.TempDir cleanup can remove it
			t.Logf("restore queues dir permissions: %v", chmodErr)
		}
	})

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
		MaxConcurrent:    1,
		NoAutoPull:       true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
	}()

	waitFor(t, "the reservation write to fail and be reported", func() bool {
		return len(collectEventsByType(bus, string(core.EventTypeInfrastructureUnavailable))) > 0
	})

	wakeC := qs.WakeCh()
	for tick := range 5 {
		qs.Wake()
		waitFor(t, fmt.Sprintf("the work loop to take wake %d of 5", tick+1), func() bool {
			return len(wakeC) == 0
		})
		if claims := ledger.claimCalls.Load(); claims != 0 {
			t.Fatalf("ClaimBead called %d time(s) by iteration %d — a retry after a failed "+
				"reservation write must not claim the bead", claims, tick+1)
		}
	}

	got := qs.Queue()
	cancel()
	awaitLoopTeardown(t, loopDone, "work loop")

	if claims := ledger.claimCalls.Load(); claims != 0 {
		t.Errorf("ClaimBead called %d time(s); want 0 — a dispatch whose reservation write "+
			"failed must not claim the bead", claims)
	}
	if got == nil {
		t.Fatal("QueueStore.Queue() returned nil mid-flight — the queue was not loaded")
	}
	item := got.Groups[0].Items[0]
	if item.Status != queue.ItemStatusPending {
		t.Errorf("item status = %q; want %q — the item never started, so the next tick must re-pick it",
			item.Status, queue.ItemStatusPending)
	}
	if item.RunID != nil {
		t.Errorf("item carries run_id %v; no run was started", *item.RunID)
	}

	var infra []stubEmittedEvent
	var degradedCount int
	for _, evt := range bus.allEvents() {
		switch evt.EventType {
		case string(core.EventTypeInfrastructureUnavailable):
			infra = append(infra, evt)
		case string(core.EventTypeDaemonDegraded):
			degradedCount++
		}
	}
	if len(infra) != 1 {
		t.Fatalf("emitted %d infrastructure_unavailable events; want exactly 1", len(infra))
	}
	var payload core.InfrastructureUnavailablePayload
	if err := json.Unmarshal(infra[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal infrastructure_unavailable: %v", err)
	}
	if payload.FailedPrerequisite != core.InfrastructurePrerequisiteQueueWriteError {
		t.Errorf("failed_prerequisite = %q; want %q",
			payload.FailedPrerequisite, core.InfrastructurePrerequisiteQueueWriteError)
	}
	if degradedCount != 1 {
		t.Errorf("emitted %d daemon_degraded events; want exactly 1 — QM-001 also requires the "+
			"degraded transition", degradedCount)
	}
}
