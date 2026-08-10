package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/workers"
)

// errReleaseE2EClaimRefused is what the fake ledger returns from ClaimBead.
//
// DO NOT put the word "blocked" in this text. runWorkLoop's dependency-blocked
// detector is a bare strings.Contains(claimErr.Error(), "blocked"), so any error
// carrying that substring routes the item through evaluateGroupAdvanceWithOutcome
// instead of the claim-failure release this test exists to drive.
var errReleaseE2EClaimRefused = errors.New("release-writefail fake: claim refused")

// breakQueuesOnClaimLedger lets the reservation commit and then takes write
// access to the queues directory away from inside ClaimBead, so the release that
// follows is the FIRST write that cannot reach disk.
type breakQueuesOnClaimLedger struct {
	queuesDir  string
	claimCalls atomic.Int64
}

func (l *breakQueuesOnClaimLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil
}

func (l *breakQueuesOnClaimLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *breakQueuesOnClaimLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	l.claimCalls.Add(1)
	// The reservation has already committed by this point, so the queue file
	// exists and the item is durably dispatched. Breaking the directory here is
	// what makes the release — and only the release — fail its write.
	//nolint:gosec,errcheck // the test needs a readable but non-writable directory; a chmod failure surfaces as the missing event the assertions check for
	_ = os.Chmod(l.queuesDir, 0o500)
	return errReleaseE2EClaimRefused
}

func (l *breakQueuesOnClaimLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *breakQueuesOnClaimLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// waitFor blocks until cond holds, or fails the test naming what it was waiting
// for. A poll beats a fixed sleep here: it is faster on an idle machine, it does
// not flake on a loaded one, and a starved work loop reports as a timeout on a
// named condition instead of as a zero-event assertion failure.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after 20s waiting for %s", what)
}

// A release whose write does not reach disk must be as loud as a reservation
// whose write does not reach disk. The raw revert this replaces logged the
// persist error to stderr and carried on, so the operator had no event at all.
//
// The unit tests pin the verdict releaseReservation returns. This one drives the
// real work loop and pins what the loop DOES with that verdict, which is the
// part a dropped case would break silently — and silently is the whole failure
// mode, because nothing ever re-selects a dispatched item.
//
// It deliberately asserts on EVENTS, not on the queue file. Transact clones the
// candidate from the in-memory store and never reads disk, so any later
// transaction rewrites the file from memory and heals a memory-only revert. An
// on-disk assertion here would pass whether or not the release is durable.
//
// Spec ref: specs/queue-model.md §9.1 QM-059, §3.1 QM-001.
// Bead ref: hk-mk4cl.
func TestReleaseWriteFailure_ReportsTheFailedWrite(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-release-writefail-bead"

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o700); err != nil {
		t.Fatalf("mkdir queues: %v", err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(queuesDir, 0o700); chmodErr != nil { //nolint:gosec // restoring so t.TempDir cleanup can remove it
			t.Logf("restore queues dir permissions: %v", chmodErr)
		}
	})

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

	reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
		Name:     "gb-mbp",
		Host:     "gb-mbp.local",
		Enabled:  true,
		MaxSlots: 6,
	}}})
	ledger := &breakQueuesOnClaimLedger{queuesDir: queuesDir}
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

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
	}()

	// Wait for the work the assertions describe, rather than for a fixed span of
	// wall clock. internal/daemon is a heavily parallel package on a machine that
	// may be loaded, and a sleep long enough to be safe there is a sleep every
	// future run pays for. Polling also turns a starved loop into a named
	// timeout instead of a confusing zero-event failure.
	//
	// This does NOT exercise the once-per-queue report bound, and waiting longer
	// would not: the failed release leaves the item at dispatched, nothing
	// re-selects a dispatched item, and it is the only item, so no second report
	// opportunity ever arises. That unreachability is what makes the event count
	// below trustworthy. The dedup bound is pinned by the reserve-path sibling,
	// TestReservationWriteFailure_NeverClaimsAndNeverLaunches.
	waitFor(t, "the claim to fail and the release to report", func() bool {
		return ledger.claimCalls.Load() > 0 &&
			len(collectEventsByType(bus, string(core.EventTypeInfrastructureUnavailable))) > 0
	})
	// Cheap insurance only. What actually preserves the exactly-one count is that
	// the assertions read the bus AFTER the loop has exited, so every emission is
	// already recorded; reportQueueWriteError also emits its pair back to back on
	// the loop goroutine, so the two cannot be observed apart.
	time.Sleep(200 * time.Millisecond)
	cancel()
	awaitLoopTeardown(t, loopDone, "work loop")

	// Positive control. A run with zero claims never reached the release, and
	// every assertion below would then pass for the wrong reason.
	if claims := ledger.claimCalls.Load(); claims == 0 {
		t.Fatal("ClaimBead was never called — the loop never reached the release this test exists to drive")
	}

	infra := collectEventsByType(bus, string(core.EventTypeInfrastructureUnavailable))
	degradedCount := len(collectEventsByType(bus, string(core.EventTypeDaemonDegraded)))
	if len(infra) != 1 {
		t.Fatalf("emitted %d infrastructure_unavailable events; want exactly 1 — a release whose "+
			"write did not reach disk must report once, and only once", len(infra))
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
