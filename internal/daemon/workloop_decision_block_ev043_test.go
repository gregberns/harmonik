package daemon_test

// workloop_decision_block_ev043_test.go — EV-043 dispatch-gate workloop tests (hk-a6e24).
//
// Acceptance criteria:
//   - When a bead is blocked by an unacknowledged decision_required event, the
//     br-ready fallback dispatch path does NOT call ClaimBead for that bead.
//   - When a queue item's bead is blocked, the queue dispatch path does NOT call
//     ClaimBead for that bead (item stays pending).
//
// The tests use a claimTrackingLedger to detect ClaimBead calls deterministically
// without requiring a full subprocess dispatch.
//
// Spec ref: specs/event-model.md §4.12 EV-043.
// Bead ref: hk-a6e24.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// claimTrackingLedger wraps stubBeadLedger and sends on claimedCh whenever
// ClaimBead is called. Used to detect whether the EV-043 gate is suppressing
// dispatch without requiring a full subprocess launch.
type claimTrackingLedger struct {
	claimedCh chan<- core.BeadID
	inner     *stubBeadLedger
}

func (c *claimTrackingLedger) Ready(ctx context.Context) ([]core.BeadRecord, error) {
	return c.inner.Ready(ctx)
}

func (c *claimTrackingLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	return c.inner.ShowBead(ctx, id)
}

func (c *claimTrackingLedger) ClaimBead(ctx context.Context, brPath string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, beadID core.BeadID) error {
	select {
	case c.claimedCh <- beadID:
	default:
	}
	return c.inner.ClaimBead(ctx, brPath, cfg, runID, tid, beadID)
}

func (c *claimTrackingLedger) CloseBead(ctx context.Context, brPath string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, beadID core.BeadID, success bool) error {
	return c.inner.CloseBead(ctx, brPath, cfg, runID, tid, beadID, success)
}

func (c *claimTrackingLedger) ReopenBead(ctx context.Context, brPath string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, beadID core.BeadID, reason string) error {
	return c.inner.ReopenBead(ctx, brPath, cfg, runID, tid, beadID, reason)
}

// TestDecisionBlock_BrReadyPath_HoldOnBlocked verifies that when the
// DecisionBlocker has a bead blocked, the br-ready fallback dispatch path does
// NOT call ClaimBead for that bead. The gate must fire after Ready() returns
// the bead but before ClaimBead is invoked.
//
// Spec ref: specs/event-model.md §4.12 EV-043.
// Bead ref: hk-a6e24.
func TestDecisionBlock_BrReadyPath_HoldOnBlocked(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const blockedBeadID = core.BeadID("hk-ev043-brready-blocked")

	claimedCh := make(chan core.BeadID, 4)
	inner := &stubBeadLedger{ready: []core.BeadID{blockedBeadID}}
	ledger := &claimTrackingLedger{claimedCh: claimedCh, inner: inner}
	bus := &stubEventCollector{}

	blocker := daemon.NewDecisionBlocker()
	blocker.AddBeadBlock(blockedBeadID, "tok-ev043-brready-test")

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		DecisionBlocker:  blocker,
		// No QueueStore — uses br-ready fallback path.
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Allow several poll ticks; ClaimBead must NOT be called while blocked.
	time.Sleep(250 * time.Millisecond)

	select {
	case got := <-claimedCh:
		t.Fatalf("ClaimBead called for %s while bead is decision-blocked; EV-043 gate must suppress dispatch", got)
	default:
		// Correct: gate prevented ClaimBead from being reached.
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("workloop did not exit after context cancellation")
	}
}

// TestDecisionBlock_QueuePath_HoldOnBlocked verifies that when a bead in the
// active queue is blocked by an unacknowledged decision_required, the queue
// dispatch path does NOT call ClaimBead for that bead.
//
// Spec ref: specs/event-model.md §4.12 EV-043.
// Bead ref: hk-a6e24.
func TestDecisionBlock_QueuePath_HoldOnBlocked(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const blockedBeadID = core.BeadID("hk-ev043-queue-blocked")

	claimedCh := make(chan core.BeadID, 4)
	inner := &stubBeadLedger{}
	ledger := &claimTrackingLedger{claimedCh: claimedCh, inner: inner}
	bus := &stubEventCollector{}

	qs := daemon.ExportedNewQueueStore()
	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "ev043-queue-path-test",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      []queue.Item{{BeadID: blockedBeadID, Status: queue.ItemStatusPending}},
				CreatedAt:  now,
			},
		},
	}
	qs.SetQueue(q)

	blocker := daemon.NewDecisionBlocker()
	blocker.AddBeadBlock(blockedBeadID, "tok-ev043-queue-test")

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:       qs,
		DecisionBlocker:  blocker,
		NoAutoPull:       true, // queue-only: do not fall through to br-ready path
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Allow several poll ticks; ClaimBead must NOT be called while blocked.
	time.Sleep(250 * time.Millisecond)

	select {
	case got := <-claimedCh:
		t.Fatalf("ClaimBead called for %s while bead is decision-blocked; EV-043 queue-path gate must suppress dispatch", got)
	default:
		// Correct: gate prevented ClaimBead from being reached.
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("workloop did not exit after context cancellation")
	}
}
