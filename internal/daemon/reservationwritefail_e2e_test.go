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

	// Take write access away from the queues directory, so the atomic-write
	// sequence inside the reservation fails. Everything else about the tick is
	// normal: the item is eligible, a slot is free, the disk gate is open.
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
		// Report free disk far above the watermark, or the disk-low gate holds
		// the tick before selection and the test passes without ever reaching
		// the reservation it claims to check.
	})

	// Comfortably longer than waitFor's own 20 s deadline, so a starved loop
	// reports as a named timeout on the condition it missed rather than as a
	// cancelled context with no explanation.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
	}()

	// Anchor on the work the assertions describe, then force further iterations.
	//
	// This replaces a 1200 ms sleep described as "several poll ticks".
	// workloopPollInterval is 2 s, so 1200 ms was less than ONE tick: the loop
	// was still inside its first post-failure sleep when the assertions ran, and
	// every negative assertion below passed because nothing had been given time
	// to happen. A test whose negative assertions cannot fail is not a weak
	// test, it is not a test.
	//
	// The event is the positive anchor. It is emitted only after the loop has
	// selected the item, minted a RunID, attempted the reservation and taken the
	// write-failed branch — so once it exists, one full claim opportunity has
	// demonstrably come and gone without a claim.
	waitFor(t, "the reservation write to fail and be reported", func() bool {
		return len(collectEventsByType(bus, string(core.EventTypeInfrastructureUnavailable))) > 0
	})

	// Further iterations are driven, not waited for. workloopSleep returns as
	// soon as the queue-submit wake channel fires, so waking the store steps the
	// loop immediately instead of paying 2 s of wall clock per tick. Waiting for
	// the buffered wake to drain is what makes the step observable: the channel
	// empties only when the loop has taken the signal.
	//
	// Five iterations, because they are what make the once-per-queue report
	// bound below observable. Every tick re-selects the item and is refused
	// again at the quarantine check, so reportQueueWriteError is reached six
	// times in all and its dedup map suppresses five. Under the sleep this
	// replaces only tick 1 ran, and "exactly one event" was satisfied by there
	// having been only one chance to emit.
	//
	// The per-tick claim check is a cheap positive control on the loop rather
	// than a search: nothing varies across ticks 2 to 6, because the quarantine
	// is sticky and each tick takes the identical branch.
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
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("work loop did not exit within 5s after context cancel")
	}

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

	// QM-001 requires the failure to be loud. It states three MUSTs and no
	// count, so the once-per-queue bound below is not QM-001 — it is the flood
	// control reportQueueWriteError documents for itself, and it is load-bearing
	// here because the loop really does reach that reporter on every tick.
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
