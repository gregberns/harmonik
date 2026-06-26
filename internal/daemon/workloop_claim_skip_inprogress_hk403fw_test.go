package daemon_test

// workloop_claim_skip_inprogress_hk403fw_test.go — claim-skip cooldown for
// in_progress beads with an active run (hk-403fw).
//
// Problem: when the pre-claim re-read (BI-013c) observes in_progress AND the
// bead has an active run in the RunRegistry (non-stranded case), the code sets
// the queue item to deferred-for-ledger-dep and sleeps workloopPollInterval.
// But ReevaluateDeferred immediately un-defers the item (no in-group blockers
// → allResolved=true), causing the loop to re-select and re-ShowBead at ~2.5s
// cadence — 153 bead_claim_skipped events in the observed 6.3-min burst.
//
// Fix: after the first in_progress + active-run detection, arm a 5-minute
// cooldown keyed on the bead ID. Subsequent loop iterations hit the cooldown
// check (before ShowBead) and sleep without calling ShowBead or emitting
// bead_claim_skipped, until the cooldown expires or the run terminates.
//
// Test: within a window of 3 poll intervals (~6 s), ShowBead must be called
// exactly once and bead_claim_skipped must be emitted exactly once.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// TestHK403FW_InProgressWithActiveRun_CooldownSuppressesSpin verifies that
// when the pre-claim guard sees in_progress + active run, the cooldown
// suppresses ShowBead and bead_claim_skipped to at most one call in a 3-poll
// observation window. Without the fix, the loop spins at ~2.5s cadence.
func TestHK403FW_InProgressWithActiveRun_CooldownSuppressesSpin(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	beadID := core.BeadID("hk-403fw-cooldown")

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "hk403fw-test",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items:      []queue.Item{{BeadID: beadID, Status: queue.ItemStatusPending}},
				CreatedAt:  now,
			},
		},
	}
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	// ShowBead always returns in_progress — simulating a bead whose run is live.
	ledger := newBI013cLedger(core.CoarseStatusInProgress)
	bus := &stubEventCollector{}

	// Pre-register a fake run so HasBeadRun returns true (non-stranded path).
	reg := daemon.NewRunRegistry()
	rawUUID, _ := uuid.Parse("01900000-0000-7000-8000-000000000099")
	runID := core.RunID(rawUUID)
	reg.Register(runID, &daemon.RunHandle{BeadID: beadID})

	resetter := newHKL2XD1FakeResetter()

	loopCtx, cancelLoop := context.WithCancel(context.Background())
	defer cancelLoop()

	p := daemon.WorkLoopDepsParams{
		BrAdapter:                  ledger,
		Bus:                        bus,
		ProjectDir:                 projectDir,
		HandlerBinary:              "/bin/sh",
		HandlerArgs:                []string{"-c", "exit 0"},
		IntentLogDir:               filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:                 qs,
		MaxConcurrent:              2, // allow past capacity gate with 1 fake run
		AdapterRegistry2:           NewSealedAdapterRegistryForTest(t),
		RunRegistry:                reg,
		StrandedInProgressResetter: resetter,
		StrandedResetDaemonNS:      1_700_000_000_000_000_001,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(loopCtx, 60*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// Wait for the first ShowBead call — confirms BI-013c fired and cooldown was armed.
	select {
	case <-ledger.showSeen:
	case <-time.After(15 * time.Second):
		t.Fatal("ShowBead not called within 15s — BI-013c guard did not fire (hk-403fw)")
	}

	// Observe for 3 poll intervals (~6s). Without the cooldown fix the loop would
	// call ShowBead ~3 more times. With the fix, the cooldown (5 min TTL) suppresses
	// all further ShowBead calls within this window.
	time.Sleep(3 * 2 * time.Second) // 3 × workloopPollInterval (2s)

	cancelLoop()
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runWorkLoop did not exit after context cancel")
	}

	// ── ShowBead call count: must be 1 ───────────────────────────────────────
	// Cooldown suppresses all re-selects after the first detection.
	if n := ledger.showCalls.Load(); n != 1 {
		t.Errorf("ShowBead called %d time(s); want 1 — cooldown must suppress spin-loop (hk-403fw)", n)
	}

	// ── bead_claim_skipped emitted exactly once ──────────────────────────────
	var skipCount int
	for _, evt := range bus.allEvents() {
		if evt.EventType != string(core.EventTypeBeadClaimSkipped) {
			continue
		}
		var p core.BeadClaimSkippedPayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			t.Errorf("bead_claim_skipped payload unmarshal error: %v", err)
			continue
		}
		if p.BeadID == string(beadID) {
			skipCount++
		}
	}
	if skipCount != 1 {
		t.Errorf("bead_claim_skipped event count = %d; want 1 — cooldown must suppress repeated emissions (hk-403fw)", skipCount)
	}

	// ── ResetBead must NOT be called (HasBeadRun = true → not stranded) ──────
	if calls := resetter.called(); len(calls) != 0 {
		t.Errorf("ResetBead called %d time(s) = %v; want 0 — live-run bead must not be auto-reset (hk-403fw)", len(calls), calls)
	}

	// ── ClaimBead must NOT be called ─────────────────────────────────────────
	if n := ledger.claimCalls.Load(); n != 0 {
		t.Errorf("ClaimBead called %d time(s); want 0 — in_progress bead must not be claimed (hk-403fw)", n)
	}
}
