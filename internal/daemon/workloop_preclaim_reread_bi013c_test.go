package daemon_test

// workloop_preclaim_reread_bi013c_test.go — BI-013c pre-claim status re-read (hk-79x3v).
//
// Spec ref: specs/beads-integration.md §4.5a BI-013c.
//
// Between the dispatcher's selection of a queue item and the claim write to
// Beads, the daemon MUST re-read the bead's status via br show and confirm
// status = open. If the re-read returns a non-open status, the daemon MUST:
//   - skip the claim (no ClaimBead call),
//   - emit bead_claim_skipped{bead_id, observed_status, reason="status_changed_between_select_and_claim"},
//   - return the item with status deferred-for-ledger-dep.
//
// This file contains two binding tests:
//  1. TestBI013c_InProgressBeadSkipped — bead transitions to in_progress
//     (simulating an external claim) between selection and claim.
//  2. TestBI013c_ClosedBeadSkipped — bead transitions to closed (terminal)
//     between selection and claim.
//
// Helper prefix: bi013c (per implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// bi013cLedger is a stub BeadLedger that returns a configurable non-open status
// from ShowBead, simulating a bead that changed between selection and claim.
// ClaimBead records calls so the test can assert it was never invoked.
type bi013cLedger struct {
	mu           sync.Mutex
	returnStatus core.CoarseStatus
	claimCalls   atomic.Int64
	showCalls    atomic.Int64
	showSeen     chan struct{}
	showOnce     sync.Once
}

func newBI013cLedger(returnStatus core.CoarseStatus) *bi013cLedger {
	return &bi013cLedger{
		returnStatus: returnStatus,
		showSeen:     make(chan struct{}),
	}
}

func (l *bi013cLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil // queue-only dispatch
}

func (l *bi013cLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	l.showCalls.Add(1)
	l.showOnce.Do(func() { close(l.showSeen) })
	return core.BeadRecord{BeadID: id, Status: l.returnStatus}, nil
}

func (l *bi013cLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	l.claimCalls.Add(1)
	return nil
}

func (l *bi013cLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return nil
}

func (l *bi013cLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// bi013cRunTest is the shared test body for BI-013c pre-claim guard tests.
// It asserts:
//  1. ClaimBead is never called.
//  2. The queue item transitions to deferred-for-ledger-dep with Attempts == 0.
//  3. A bead_claim_skipped event is emitted with observed_status == wantStatus
//     and reason == "status_changed_between_select_and_claim".
func bi013cRunTest(t *testing.T, beadID core.BeadID, wantStatus core.CoarseStatus) {
	t.Helper()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "bi013c-test-" + string(wantStatus),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadID, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := newBI013cLedger(wantStatus)
	bus := &stubEventCollector{}

	loopCtx, cancelLoop := context.WithCancel(context.Background())
	defer cancelLoop()

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:       qs,
		MaxConcurrent:    1,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(loopCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// Wait for ShowBead to be called — confirms the guard fired.
	select {
	case <-ledger.showSeen:
	case <-time.After(15 * time.Second):
		t.Fatalf("ShowBead was never called within 15s — BI-013c guard did not fire (status=%s)", wantStatus)
	}

	// Poll for deferred-for-ledger-dep (write lock completes asynchronously).
	var item queue.Item
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if liveQ := daemon.ExportedQueueStoreOf(deps).Queue(); liveQ != nil &&
			len(liveQ.Groups) > 0 && len(liveQ.Groups[0].Items) > 0 {
			item = liveQ.Groups[0].Items[0]
			if item.Status == queue.ItemStatusDeferredForLedgerDep {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if item.Status != queue.ItemStatusDeferredForLedgerDep {
		t.Errorf("item status = %q; want %q — BI-013c: non-open bead must produce deferred-for-ledger-dep",
			item.Status, queue.ItemStatusDeferredForLedgerDep)
	}
	if item.Attempts != 0 {
		t.Errorf("item.Attempts = %d; want 0 — BI-013c guard must not consume attempts", item.Attempts)
	}

	cancelLoop()
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runWorkLoop did not exit after context cancel")
	}

	// ── Assert ClaimBead was never called ────────────────────────────────────

	if n := ledger.claimCalls.Load(); n != 0 {
		t.Errorf("ClaimBead call count = %d; want 0 — BI-013c: non-open bead must not be claimed", n)
	}

	// ── Assert bead_claim_skipped event was emitted ───────────────────────────

	var found bool
	for _, evt := range bus.allEvents() {
		if evt.EventType != string(core.EventTypeBeadClaimSkipped) {
			continue
		}
		var p core.BeadClaimSkippedPayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			t.Errorf("bead_claim_skipped payload unmarshal error: %v", err)
			continue
		}
		if p.BeadID != string(beadID) {
			continue
		}
		if p.ObservedStatus != string(wantStatus) {
			t.Errorf("bead_claim_skipped observed_status = %q; want %q (BI-013c)",
				p.ObservedStatus, wantStatus)
		}
		const wantReason = "status_changed_between_select_and_claim"
		if p.Reason != wantReason {
			t.Errorf("bead_claim_skipped reason = %q; want %q (BI-013c)", p.Reason, wantReason)
		}
		if p.DetectedAt == "" {
			t.Error("bead_claim_skipped detected_at is empty (BI-013c)")
		}
		found = true
		break
	}
	if !found {
		t.Errorf("bead_claim_skipped event for bead %s (status=%s) not found in bus (BI-013c)",
			beadID, wantStatus)
	}
}

// TestBI013c_InProgressBeadSkipped verifies that a bead already in_progress
// (external claim) triggers the BI-013c guard: no claim, bead_claim_skipped
// event emitted, item set to deferred-for-ledger-dep.
func TestBI013c_InProgressBeadSkipped(t *testing.T) {
	t.Parallel()
	bi013cRunTest(t, "hk-79x3v-in-progress", core.CoarseStatusInProgress)
}

// TestBI013c_ClosedBeadSkipped verifies that a closed bead triggers the
// BI-013c guard: no claim, bead_claim_skipped event emitted, item set to
// deferred-for-ledger-dep.
func TestBI013c_ClosedBeadSkipped(t *testing.T) {
	t.Parallel()
	bi013cRunTest(t, "hk-79x3v-closed", core.CoarseStatusClosed)
}
