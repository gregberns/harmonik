package daemon_test

// hknddg1_crewqueue_provenance_test.go — regression test for hk-nddg1.
//
// Bug (now fixed): daemon.loadQueueProvenance built the orphan-sweep provenance
// sets (QueueOwned / QueueDispatched) by reading ONLY the main queue
// (queue.Load(ctx, projectDir, QueueNameMain)). But beads are dispatched from
// ALL named queues, including crew queues like queues/paul.json. After a daemon
// SIGKILL, a bead X dispatched via a crew queue is in_progress with an
// owned-sentinel, paul.json still records it Status=dispatched, and its run
// session is dead. On restart SweepStaleInProgressBeads listed X and reset it
// in_progress->open — the (a-queue) exclusion did NOT fire because X was absent
// from the main-only QueueDispatched set — and LoadStartupQueues then
// re-dispatched X from its still-"dispatched" crew queue = double-dispatch.
//
// Fix (bootreconcile.go, hk-nddg1): loadQueueProvenance now enumerates ALL
// queues via queue.EnumerateQueueNames and aggregates each into the provenance
// sets, so a crew-queue dispatched bead lands in QueueDispatched and the sweep's
// (a-queue) exclusion protects it.
//
// This test proves the fix: it seeds a crew queue (paul.json) with X dispatched,
// an owned-sentinel for X, and a ledger showing X in_progress — with NO claim
// intent and NO run dir, so the ONLY thing that can protect X is the
// queue-provenance (a-queue) exclusion. It asserts (1) loadQueueProvenance
// aggregates X from paul.json into QueueDispatched, and (2) the headline: the
// orphan sweep does NOT reset X.
//
// Helper prefix: hknddg1 (bead-namespace convention).
// Bead ref: hk-nddg1.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
)

const hknddg1BeadID core.BeadID = "hk-nddg1-crewqueue-bead-001"

// hknddg1FakeLedger reports a fixed set of in_progress beads to the sweep.
type hknddg1FakeLedger struct{ beads []core.BeadRecord }

func (f *hknddg1FakeLedger) ListInFlightBeads(_ context.Context) ([]core.BeadRecord, error) {
	return f.beads, nil
}

// hknddg1FakeResetter records every ResetBead call so the test can assert the
// crew-queue bead is NOT reset.
type hknddg1FakeResetter struct{ called []core.BeadID }

func (f *hknddg1FakeResetter) ResetBead(
	_ context.Context,
	_ string,
	_ brcli.TimeoutConfig,
	beadID core.BeadID,
	_ core.ProjectHash,
	_ int64,
) error {
	f.called = append(f.called, beadID)
	return nil
}

// hknddg1FakeHandlerLister / hknddg1FakeBrLister return no orphan PIDs, keeping
// RunOrphanSweep from falling back to the real `ps`-based OS listers.
type hknddg1FakeHandlerLister struct{}

func (hknddg1FakeHandlerLister) ListOrphanHandlerPIDs(_ context.Context, _ core.ProjectHash) ([]int, error) {
	return nil, nil
}

type hknddg1FakeBrLister struct{}

func (hknddg1FakeBrLister) ListOrphanBrPIDs(_ context.Context) ([]int, error) { return nil, nil }

// TestHKnddg1_CrewQueueDispatchedBeadNotResetBySweep is the hk-nddg1 regression
// test: a bead dispatched via a crew queue (paul.json) — not the main queue —
// must not be reset in_progress->open by the boot orphan sweep, because
// loadQueueProvenance now aggregates dispatched provenance across ALL named
// queues so the (a-queue) exclusion fires.
func TestHKnddg1_CrewQueueDispatchedBeadNotResetBySweep(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("hk-nddg1: MkdirAll .harmonik: %v", err)
	}

	// (a) Crew queue paul.json records X as dispatched. Main queue absent.
	paulQueue := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "hk-nddg1-paul-queue",
		Name:          "paul",
		Groups: []queue.Group{{
			GroupIndex: 0,
			Items:      []queue.Item{{BeadID: hknddg1BeadID, Status: queue.ItemStatusDispatched}},
			CreatedAt:  time.Now().UTC(),
		}},
	}
	if err := queue.Persist(context.Background(), projectDir, paulQueue); err != nil {
		t.Fatalf("hk-nddg1: persist paul.json: %v", err)
	}

	// (b) Owned-sentinel for X. This is the ONLY provenance signal (no claim
	// intent, no run dir), so the sweep would otherwise consider X eligible for
	// reset. The (a-queue) exclusion must be what protects it.
	ownedDir := lifecycle.BeadsOwnedDir(projectDir)
	if err := os.MkdirAll(ownedDir, 0o755); err != nil {
		t.Fatalf("hk-nddg1: MkdirAll beads-owned: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ownedDir, string(hknddg1BeadID)), []byte{}, 0o600); err != nil {
		t.Fatalf("hk-nddg1: write owned-sentinel: %v", err)
	}

	// Primary assertion: loadQueueProvenance aggregates X across ALL named
	// queues (paul.json), not just main. Under the bug X was absent here.
	queueDispatched, queueOwned := daemon.ExportedLoadQueueProvenance(context.Background(), projectDir)
	if _, ok := queueDispatched[hknddg1BeadID]; !ok {
		t.Fatalf("hk-nddg1: bead %s absent from QueueDispatched aggregated across named queues; loadQueueProvenance did not read the crew queue paul.json (main-only regression)", hknddg1BeadID)
	}
	if _, ok := queueOwned[hknddg1BeadID]; !ok {
		t.Errorf("hk-nddg1: bead %s absent from QueueOwned aggregated across named queues", hknddg1BeadID)
	}

	// Headline: drive the boot orphan sweep with the aggregated provenance and
	// assert X is NOT reset. This is the exact path bootreconcile.go wires
	// (st.queueDispatched -> OrphanSweepConfig.QueueDispatched).
	ledger := &hknddg1FakeLedger{beads: []core.BeadRecord{{
		BeadID:        hknddg1BeadID,
		Title:         "crew-queue dispatched bead",
		BeadType:      "task",
		Status:        core.CoarseStatusInProgress,
		AuditTrailRef: string(hknddg1BeadID),
	}}}
	resetter := &hknddg1FakeResetter{}
	daemonStart := time.Now()

	result, err := daemon.RunOrphanSweep(
		context.Background(),
		projectDir,
		lifecycle.ComputeProjectHash(projectDir),
		daemonStart,
		daemon.OrphanSweepConfig{
			HandlerLister:   hknddg1FakeHandlerLister{},
			BrLister:        hknddg1FakeBrLister{},
			BeadLedger:      ledger,
			BeadResetter:    resetter,
			BeadProvenance:  lifecycle.NewSentinelFileProvenanceChecker(ownedDir),
			QueueDispatched: queueDispatched,
			QueueOwned:      queueOwned,
			IntentLogDir:    filepath.Join(projectDir, ".harmonik", "beads-intents"),
			DaemonStartNS:   daemonStart.UnixNano(),
		},
	)
	if err != nil {
		// git worktree prune fails on a non-git temp dir; non-fatal (mirrors the
		// sibling PL-006 sweep tests).
		t.Logf("hk-nddg1: RunOrphanSweep error (possibly worktree prune on non-git dir): %v", err)
	}

	if result.BeadInProgressReset != 0 {
		t.Errorf("hk-nddg1: BeadInProgressReset = %d, want 0 — a crew-queue dispatched bead was reset (a-queue exclusion did not fire)", result.BeadInProgressReset)
	}
	if len(resetter.called) != 0 {
		t.Errorf("hk-nddg1: ResetBead called for %v — crew-queue dispatched bead double-dispatch-reset regression reintroduced", resetter.called)
	}
}
