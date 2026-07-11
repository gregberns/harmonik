package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// hkr73qrCapturedEvent holds the type and run_id of a single captured emission.
type hkr73qrCapturedEvent struct {
	Type  core.EventType
	RunID core.RunID
}

// hkr73qrCapturingBus is a handlercontract.EventEmitter that records every
// EmitWithRunID call (type + run_id) for assertion.
type hkr73qrCapturingBus struct {
	events []hkr73qrCapturedEvent
}

func (b *hkr73qrCapturingBus) Emit(_ context.Context, _ core.EventType, _ []byte) error {
	return nil
}

func (b *hkr73qrCapturingBus) EmitWithRunID(_ context.Context, runID core.RunID, eventType core.EventType, _ []byte) error {
	b.events = append(b.events, hkr73qrCapturedEvent{Type: eventType, RunID: runID})
	return nil
}

func hkr73qrNewRunID(t *testing.T) core.RunID {
	t.Helper()
	uid, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hkr73qrNewRunID: %v", err)
	}
	return core.RunID(uid)
}

// hkr73qrWriteRunStarted appends a run_started event to bus for runID/beadID.
func hkr73qrWriteRunStarted(t *testing.T, bus eventbus.EventBus, runID core.RunID, beadID string) {
	t.Helper()
	pl := workloopRunStartedPayload{
		RunID:         runID.String(),
		BeadID:        beadID,
		WorkspacePath: "/tmp/hkr73qr-test",
		StartedAt:     "2026-01-01T00:00:00Z",
	}
	b, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("hkr73qrWriteRunStarted: marshal: %v", err)
	}
	if err := bus.EmitWithRunID(context.Background(), runID, core.EventTypeRunStarted, b); err != nil {
		t.Fatalf("hkr73qrWriteRunStarted: emit: %v", err)
	}
}

// hkr73qrWriteRunFailed appends a run_failed event to bus for runID/beadID.
func hkr73qrWriteRunFailed(t *testing.T, bus eventbus.EventBus, runID core.RunID, beadID string) {
	t.Helper()
	pl := workloopRunCompletedPayload{
		RunID:   runID.String(),
		BeadID:  beadID,
		Success: false,
		Summary: "intentional failure",
		EndedAt: "2026-01-01T01:00:00Z",
	}
	b, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("hkr73qrWriteRunFailed: marshal: %v", err)
	}
	if err := bus.EmitWithRunID(context.Background(), runID, core.EventTypeRunFailed, b); err != nil {
		t.Fatalf("hkr73qrWriteRunFailed: emit: %v", err)
	}
}

// hkr73qrWriteRunCompleted appends a run_completed event to bus for runID/beadID.
func hkr73qrWriteRunCompleted(t *testing.T, bus eventbus.EventBus, runID core.RunID, beadID string) {
	t.Helper()
	pl := workloopRunCompletedPayload{
		RunID:   runID.String(),
		BeadID:  beadID,
		Success: true,
		Summary: "success",
		EndedAt: "2026-01-01T01:00:00Z",
	}
	b, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("hkr73qrWriteRunCompleted: marshal: %v", err)
	}
	if err := bus.EmitWithRunID(context.Background(), runID, core.EventTypeRunCompleted, b); err != nil {
		t.Fatalf("hkr73qrWriteRunCompleted: emit: %v", err)
	}
}

// hkr73qrSetupJSONL creates a temp events.jsonl, writes the supplied events via
// a real eventbus.BusImpl, closes the writer, and returns the path.
func hkr73qrSetupJSONL(t *testing.T, write func(bus eventbus.EventBus)) string {
	t.Helper()
	jsonlPath := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("hkr73qrSetupJSONL: OpenJSONLWriter: %v", err)
	}
	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)
	write(bus)
	if err := writer.Close(); err != nil {
		t.Fatalf("hkr73qrSetupJSONL: writer.Close: %v", err)
	}
	return jsonlPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestReconcileOrphanedRunsOnResume_EmitsRunFailedForOrphanedRuns verifies that
// runs with run_started but no terminal event each receive exactly one run_failed
// emission, and runs that already have a terminal event are skipped.
func TestReconcileOrphanedRunsOnResume_EmitsRunFailedForOrphanedRuns(t *testing.T) {
	ctx := context.Background()

	orphan1 := hkr73qrNewRunID(t)
	orphan2 := hkr73qrNewRunID(t)
	// One run that already completed — must NOT be re-emitted.
	alreadyFailed := hkr73qrNewRunID(t)
	alreadyCompleted := hkr73qrNewRunID(t)

	jsonlPath := hkr73qrSetupJSONL(t, func(bus eventbus.EventBus) {
		hkr73qrWriteRunStarted(t, bus, orphan1, "hk-aaa1")
		hkr73qrWriteRunStarted(t, bus, orphan2, "hk-aaa2")
		hkr73qrWriteRunStarted(t, bus, alreadyFailed, "hk-aaa3")
		hkr73qrWriteRunFailed(t, bus, alreadyFailed, "hk-aaa3")
		hkr73qrWriteRunStarted(t, bus, alreadyCompleted, "hk-aaa4")
		hkr73qrWriteRunCompleted(t, bus, alreadyCompleted, "hk-aaa4")
	})

	capBus := &hkr73qrCapturingBus{}
	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, capBus, nil, nil, "", core.ProjectHash(""), 0, nil, nil)
	if got != 2 {
		t.Errorf("reconcileOrphanedRunsOnResume returned %d, want 2", got)
	}

	// Both orphaned run_ids must appear exactly once as run_failed.
	gotRunIDs := make(map[core.RunID]int)
	for _, ev := range capBus.events {
		if ev.Type != core.EventTypeRunFailed {
			t.Errorf("unexpected event type %q emitted (want run_failed only)", ev.Type)
		}
		gotRunIDs[ev.RunID]++
	}
	for _, id := range []core.RunID{orphan1, orphan2} {
		if gotRunIDs[id] != 1 {
			t.Errorf("run_failed for orphan run %v emitted %d times, want 1", id, gotRunIDs[id])
		}
	}
	if gotRunIDs[alreadyFailed] != 0 {
		t.Errorf("run_failed re-emitted for already-terminated run (run_failed)")
	}
	if gotRunIDs[alreadyCompleted] != 0 {
		t.Errorf("run_failed emitted for already-terminated run (run_completed)")
	}
}

// TestReconcileOrphanedRunsOnResume_EmptyLog returns zero when no events exist.
func TestReconcileOrphanedRunsOnResume_EmptyLog(t *testing.T) {
	// File does not exist — ScanAfter treats a missing file as an empty log.
	jsonlPath := filepath.Join(t.TempDir(), "events.jsonl")
	capBus := &hkr73qrCapturingBus{}
	got := reconcileOrphanedRunsOnResume(context.Background(), jsonlPath, capBus, nil, nil, "", core.ProjectHash(""), 0, nil, nil)
	if got != 0 {
		t.Errorf("got %d, want 0 for absent log", got)
	}
	if len(capBus.events) != 0 {
		t.Errorf("got %d emitted events, want 0", len(capBus.events))
	}
}

// TestReconcileOrphanedRunsOnResume_EmptyEventsPath returns zero for empty path.
func TestReconcileOrphanedRunsOnResume_EmptyEventsPath(t *testing.T) {
	capBus := &hkr73qrCapturingBus{}
	got := reconcileOrphanedRunsOnResume(context.Background(), "", capBus, nil, nil, "", core.ProjectHash(""), 0, nil, nil)
	if got != 0 {
		t.Errorf("got %d, want 0 for empty path", got)
	}
}

// TestReconcileOrphanedRunsOnResume_AllTerminated emits zero when every started
// run has a terminal event.
func TestReconcileOrphanedRunsOnResume_AllTerminated(t *testing.T) {
	ctx := context.Background()

	run1 := hkr73qrNewRunID(t)
	run2 := hkr73qrNewRunID(t)

	jsonlPath := hkr73qrSetupJSONL(t, func(bus eventbus.EventBus) {
		hkr73qrWriteRunStarted(t, bus, run1, "hk-bbb1")
		hkr73qrWriteRunCompleted(t, bus, run1, "hk-bbb1")
		hkr73qrWriteRunStarted(t, bus, run2, "hk-bbb2")
		hkr73qrWriteRunFailed(t, bus, run2, "hk-bbb2")
	})

	capBus := &hkr73qrCapturingBus{}
	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, capBus, nil, nil, "", core.ProjectHash(""), 0, nil, nil)
	if got != 0 {
		t.Errorf("got %d, want 0 when all runs have terminal events", got)
	}
	if len(capBus.events) != 0 {
		t.Errorf("got %d emitted events, want 0", len(capBus.events))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-mdus1 — orphan queue-item advance (bead reset + queue-threaded run_failed)
// ─────────────────────────────────────────────────────────────────────────────

// hkr73qrPayloadBus captures the full run_failed payload so queue-threading can
// be asserted.
type hkr73qrPayloadBus struct {
	failed []workloopRunCompletedPayload
}

func (b *hkr73qrPayloadBus) Emit(_ context.Context, _ core.EventType, _ []byte) error { return nil }
func (b *hkr73qrPayloadBus) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	if eventType == core.EventTypeRunFailed {
		var pl workloopRunCompletedPayload
		if err := json.Unmarshal(payload, &pl); err == nil {
			b.failed = append(b.failed, pl)
		}
	}
	return nil
}

// hkr73qrFakeResetter records every ResetBead call so we can assert the orphan
// bead was reset to open (which lets QM-002a revert its dispatched queue item).
type hkr73qrFakeResetter struct {
	reset []core.BeadID
}

func (r *hkr73qrFakeResetter) ResetBead(_ context.Context, _ string, _ brcli.TimeoutConfig, beadID core.BeadID, _ core.ProjectHash, _ int64) error {
	r.reset = append(r.reset, beadID)
	return nil
}

// hkr73qrFakeStatusLedger returns a per-bead coarse status for the B3 guard.
type hkr73qrFakeStatusLedger struct {
	status map[string]core.CoarseStatus
}

func (l *hkr73qrFakeStatusLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: l.status[string(id)]}, nil
}

// hkr73qrWriteRunStartedQ appends a run_started event carrying queue coordinates.
func hkr73qrWriteRunStartedQ(t *testing.T, bus eventbus.EventBus, runID core.RunID, beadID, queueID string, groupIndex int) {
	t.Helper()
	qid := queueID
	gi := groupIndex
	pl := workloopRunStartedPayload{
		RunID:           runID.String(),
		BeadID:          beadID,
		WorkspacePath:   "/tmp/hkr73qr-q",
		StartedAt:       "2026-01-01T00:00:00Z",
		QueueID:         &qid,
		QueueGroupIndex: &gi,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("hkr73qrWriteRunStartedQ: marshal: %v", err)
	}
	if err := bus.EmitWithRunID(context.Background(), runID, core.EventTypeRunStarted, b); err != nil {
		t.Fatalf("hkr73qrWriteRunStartedQ: emit: %v", err)
	}
}

// TestReconcileOrphanedRunsOnResume_ResetsBeadAndThreadsQueue verifies the
// hk-mdus1 fix: an orphaned run's bead is reset to open (so QM-002a can revert
// its dispatched queue item) and its run_failed carries the queue coordinates.
func TestReconcileOrphanedRunsOnResume_ResetsBeadAndThreadsQueue(t *testing.T) {
	ctx := context.Background()

	orphan := hkr73qrNewRunID(t)
	done := hkr73qrNewRunID(t)

	jsonlPath := hkr73qrSetupJSONL(t, func(bus eventbus.EventBus) {
		hkr73qrWriteRunStartedQ(t, bus, orphan, "hk-mdus1-orphan", "queue-xyz", 3)
		// A non-orphan (terminated) run must NOT be reset.
		hkr73qrWriteRunStartedQ(t, bus, done, "hk-mdus1-done", "queue-xyz", 0)
		hkr73qrWriteRunCompleted(t, bus, done, "hk-mdus1-done")
	})

	payBus := &hkr73qrPayloadBus{}
	resetter := &hkr73qrFakeResetter{}
	// The orphan bead is (still) in_progress → eligible for reset.
	ledger := &hkr73qrFakeStatusLedger{status: map[string]core.CoarseStatus{
		"hk-mdus1-orphan": core.CoarseStatusInProgress,
	}}
	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, payBus, resetter, ledger, "/tmp/intents", core.ProjectHash("ph"), 12345, nil, nil)
	if got != 1 {
		t.Fatalf("reconcileOrphanedRunsOnResume returned %d, want 1", got)
	}

	// The orphan bead — and only it — must have been reset to open.
	if len(resetter.reset) != 1 || resetter.reset[0] != core.BeadID("hk-mdus1-orphan") {
		t.Fatalf("ResetBead calls = %v, want exactly [hk-mdus1-orphan]", resetter.reset)
	}

	// The emitted run_failed must carry the orphan run's queue coordinates.
	if len(payBus.failed) != 1 {
		t.Fatalf("captured %d run_failed payloads, want 1", len(payBus.failed))
	}
	pl := payBus.failed[0]
	if pl.QueueID == nil || *pl.QueueID != "queue-xyz" {
		t.Errorf("run_failed queue_id = %v, want queue-xyz", pl.QueueID)
	}
	if pl.QueueGroupIndex == nil || *pl.QueueGroupIndex != 3 {
		t.Errorf("run_failed queue_group_index = %v, want 3", pl.QueueGroupIndex)
	}
}

// TestReconcileOrphanedRunsOnResume_B3GuardSkipsClosedBead verifies the hk-mdus1
// review-B3 guard: an orphaned run whose bead already LANDED (closed — the
// daemon crashed after close but before run_completed) must NOT be reset to
// open, so completed work is never false-reopened. The run_failed is still
// emitted for observers; only the reset is suppressed.
func TestReconcileOrphanedRunsOnResume_B3GuardSkipsClosedBead(t *testing.T) {
	ctx := context.Background()

	landedOrphan := hkr73qrNewRunID(t) // orphaned run, but bead already closed
	liveOrphan := hkr73qrNewRunID(t)   // orphaned run, bead still in_progress

	jsonlPath := hkr73qrSetupJSONL(t, func(bus eventbus.EventBus) {
		hkr73qrWriteRunStartedQ(t, bus, landedOrphan, "hk-mdus1-landed", "q", 0)
		hkr73qrWriteRunStartedQ(t, bus, liveOrphan, "hk-mdus1-live", "q", 1)
	})

	payBus := &hkr73qrPayloadBus{}
	resetter := &hkr73qrFakeResetter{}
	ledger := &hkr73qrFakeStatusLedger{status: map[string]core.CoarseStatus{
		"hk-mdus1-landed": core.CoarseStatusClosed,
		"hk-mdus1-live":   core.CoarseStatusInProgress,
	}}

	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, payBus, resetter, ledger, "/tmp/intents", core.ProjectHash("ph"), 1, nil, nil)

	// Both orphans get a terminal run_failed (observability unaffected).
	if got != 2 {
		t.Fatalf("emitted %d run_failed, want 2", got)
	}
	// Only the still-in_progress bead is reset; the landed (closed) one is NOT.
	if len(resetter.reset) != 1 || resetter.reset[0] != core.BeadID("hk-mdus1-live") {
		t.Fatalf("ResetBead calls = %v, want exactly [hk-mdus1-live] (closed bead must be skipped)", resetter.reset)
	}
}

// TestReconcileOrphanedRunsOnResume_ResetsOpenBead verifies the hk-eaxc5 fix:
// an event-log-only orphan (no .harmonik/runs/ record) whose bead already reads
// open — not in_progress — must still have ResetBead issued, so the
// status-independent -32015 (bead_already_dispatched) dispatch-lock clearing
// path (QM-002a's subsequent revert) actually runs. Before the fix the reset was
// gated to in_progress only, silently skipping open beads and leaving their
// queue item wedged in 'dispatched' across every restart.
func TestReconcileOrphanedRunsOnResume_ResetsOpenBead(t *testing.T) {
	ctx := context.Background()

	orphan := hkr73qrNewRunID(t)

	jsonlPath := hkr73qrSetupJSONL(t, func(bus eventbus.EventBus) {
		hkr73qrWriteRunStartedQ(t, bus, orphan, "hk-eaxc5-open", "queue-open", 0)
	})

	payBus := &hkr73qrPayloadBus{}
	resetter := &hkr73qrFakeResetter{}
	// The orphan bead already reads open (e.g. reopened by another path, or the
	// crash landed before the claim write) — NOT in_progress.
	ledger := &hkr73qrFakeStatusLedger{status: map[string]core.CoarseStatus{
		"hk-eaxc5-open": core.CoarseStatusOpen,
	}}

	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, payBus, resetter, ledger, "/tmp/intents", core.ProjectHash("ph"), 1, nil, nil)
	if got != 1 {
		t.Fatalf("reconcileOrphanedRunsOnResume returned %d, want 1", got)
	}

	if len(resetter.reset) != 1 || resetter.reset[0] != core.BeadID("hk-eaxc5-open") {
		t.Fatalf("ResetBead calls = %v, want exactly [hk-eaxc5-open] — an open bead must still be reset so the dispatch-lock clears", resetter.reset)
	}
}

// TestReconcileOrphanedRunsOnResume_NilLedgerSkipsReset verifies that when no
// status ledger is supplied the reset is skipped entirely (conservative: never
// risk reopening a landed bead without confirming its status).
func TestReconcileOrphanedRunsOnResume_NilLedgerSkipsReset(t *testing.T) {
	ctx := context.Background()
	orphan := hkr73qrNewRunID(t)
	jsonlPath := hkr73qrSetupJSONL(t, func(bus eventbus.EventBus) {
		hkr73qrWriteRunStartedQ(t, bus, orphan, "hk-mdus1-nil", "q", 0)
	})
	payBus := &hkr73qrPayloadBus{}
	resetter := &hkr73qrFakeResetter{}
	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, payBus, resetter, nil, "/tmp/intents", core.ProjectHash("ph"), 1, nil, nil)
	if got != 1 {
		t.Fatalf("emitted %d run_failed, want 1", got)
	}
	if len(resetter.reset) != 0 {
		t.Fatalf("ResetBead calls = %v, want none (nil ledger ⇒ skip reset)", resetter.reset)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-iwu8a — dispatch-tracker-sourced orphans (no run_started, no runs/ record)
// ─────────────────────────────────────────────────────────────────────────────

// TestReconcileOrphanedRunsOnResume_DispatchTrackerOrphan_NoRunStarted
// reproduces the hk-iwu8a gap: a bead the durable queue records as dispatched
// (queueDispatched / the live dispatch-tracker) for which NO run_started event
// was ever emitted (e.g. the daemon was SIGKILL'd between the queue-claim write
// and the run-launch write) and no runs/ record exists either. Before the fix,
// reconcileOrphanedRunsOnResume enumerated orphans exclusively from the event
// log (the started/terminated maps), so this bead — invisible to that scan —
// was never reset, leaving its -32015 (bead_already_dispatched) lock wedged
// across every restart. The dispatch-tracker pass must still reset it.
func TestReconcileOrphanedRunsOnResume_DispatchTrackerOrphan_NoRunStarted(t *testing.T) {
	ctx := context.Background()

	// The event log is entirely empty for this bead — no run_started, no
	// terminal — only the queue's live dispatch-tracker knows about it.
	jsonlPath := hkr73qrSetupJSONL(t, func(bus eventbus.EventBus) {})

	payBus := &hkr73qrPayloadBus{}
	resetter := &hkr73qrFakeResetter{}
	ledger := &hkr73qrFakeStatusLedger{status: map[string]core.CoarseStatus{
		"hk-iwu8a-orphan": core.CoarseStatusInProgress,
	}}
	dispatched := lifecycle.QueueDispatchedSet{
		core.BeadID("hk-iwu8a-orphan"): struct{}{},
	}

	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, payBus, resetter, ledger, "/tmp/intents", core.ProjectHash("ph"), 1, dispatched, nil)

	// No run_failed is emitted — there is no run_id to attribute it to.
	if got != 0 {
		t.Fatalf("emitted %d run_failed, want 0 (no run_id known for a dispatch-tracker-only orphan)", got)
	}
	if len(resetter.reset) != 1 || resetter.reset[0] != core.BeadID("hk-iwu8a-orphan") {
		t.Fatalf("ResetBead calls = %v, want exactly [hk-iwu8a-orphan] — dispatch-tracker orphan must still be reset", resetter.reset)
	}
}

// TestReconcileOrphanedRunsOnResume_DispatchTrackerOrphan_SkipsLiveRun verifies
// the hk-mdus1 non-regression guard: a bead present in the live dispatch-tracker
// but also present in liveRunBeadIDs (a runs/ record whose tmux session is
// still alive, handled separately by adoptLiveRunSession) must NOT be reset —
// it is a legitimately-live dispatched bead, not an orphan.
func TestReconcileOrphanedRunsOnResume_DispatchTrackerOrphan_SkipsLiveRun(t *testing.T) {
	ctx := context.Background()

	jsonlPath := hkr73qrSetupJSONL(t, func(bus eventbus.EventBus) {})

	payBus := &hkr73qrPayloadBus{}
	resetter := &hkr73qrFakeResetter{}
	ledger := &hkr73qrFakeStatusLedger{status: map[string]core.CoarseStatus{
		"hk-iwu8a-live": core.CoarseStatusInProgress,
	}}
	dispatched := lifecycle.QueueDispatchedSet{
		core.BeadID("hk-iwu8a-live"): struct{}{},
	}
	liveRunBeadIDs := map[core.BeadID]struct{}{
		core.BeadID("hk-iwu8a-live"): {},
	}

	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, payBus, resetter, ledger, "/tmp/intents", core.ProjectHash("ph"), 1, dispatched, liveRunBeadIDs)
	if got != 0 {
		t.Fatalf("emitted %d run_failed, want 0", got)
	}
	if len(resetter.reset) != 0 {
		t.Fatalf("ResetBead calls = %v, want none — a live run must never be reset", resetter.reset)
	}
}

// TestReconcileOrphanedRunsOnResume_DispatchTrackerOrphan_SkipsAlreadyStarted
// verifies that a bead already handled by the event-log scan — it has a
// CURRENTLY-ORPHANED run_started (no terminal event) — is not double-processed
// by the dispatch-tracker pass.
func TestReconcileOrphanedRunsOnResume_DispatchTrackerOrphan_SkipsAlreadyStarted(t *testing.T) {
	ctx := context.Background()

	run := hkr73qrNewRunID(t)
	jsonlPath := hkr73qrSetupJSONL(t, func(bus eventbus.EventBus) {
		hkr73qrWriteRunStartedQ(t, bus, run, "hk-iwu8a-started", "q", 0)
		// No terminal event: this run is itself an orphan, already handled
		// by the primary (event-log) loop above.
	})

	payBus := &hkr73qrPayloadBus{}
	resetter := &hkr73qrFakeResetter{}
	ledger := &hkr73qrFakeStatusLedger{status: map[string]core.CoarseStatus{
		"hk-iwu8a-started": core.CoarseStatusInProgress,
	}}
	dispatched := lifecycle.QueueDispatchedSet{
		core.BeadID("hk-iwu8a-started"): struct{}{},
	}

	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, payBus, resetter, ledger, "/tmp/intents", core.ProjectHash("ph"), 1, dispatched, nil)
	if got != 1 {
		t.Fatalf("emitted %d run_failed, want 1 (the primary loop's own orphan handling)", got)
	}
	if len(resetter.reset) != 1 || resetter.reset[0] != core.BeadID("hk-iwu8a-started") {
		t.Fatalf("ResetBead calls = %v, want exactly [hk-iwu8a-started] once — the dispatch-tracker pass must not double-process a bead the primary loop already reset", resetter.reset)
	}
}

// TestReconcileOrphanedRunsOnResume_DispatchTrackerOrphan_SkipsLandedBead
// verifies the B3 guard is reused for the dispatch-tracker pass: a bead already
// closed must never be reopened even if the queue still records it dispatched.
func TestReconcileOrphanedRunsOnResume_DispatchTrackerOrphan_SkipsLandedBead(t *testing.T) {
	ctx := context.Background()

	jsonlPath := hkr73qrSetupJSONL(t, func(bus eventbus.EventBus) {})

	payBus := &hkr73qrPayloadBus{}
	resetter := &hkr73qrFakeResetter{}
	ledger := &hkr73qrFakeStatusLedger{status: map[string]core.CoarseStatus{
		"hk-iwu8a-landed": core.CoarseStatusClosed,
	}}
	dispatched := lifecycle.QueueDispatchedSet{
		core.BeadID("hk-iwu8a-landed"): struct{}{},
	}

	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, payBus, resetter, ledger, "/tmp/intents", core.ProjectHash("ph"), 1, dispatched, nil)
	if got != 0 {
		t.Fatalf("emitted %d run_failed, want 0", got)
	}
	if len(resetter.reset) != 0 {
		t.Fatalf("ResetBead calls = %v, want none — a landed bead must never be reopened", resetter.reset)
	}
}

// TestReconcileOrphanedRunsOnResume_DispatchTrackerOrphan_RedispatchAfterPriorCompletedRun
// reproduces a gap the initial hk-iwu8a fix introduced: a bead whose PRIOR run
// completed (run_started + run_completed both observed) and was later
// redispatched — the queue records it dispatched again, but the daemon crashed
// before emitting a run_started for this new dispatch — must still be reset.
// A historical completed run says nothing about the current dispatch; scoping
// the "already accounted for" exclusion to any beadID that ever appeared in
// `started` (regardless of terminal status) wrongly treated the redispatch as
// already-handled and left its -32015 lock wedged. FAILS before the fix
// (scopes exclusion to ALL started entries), PASSES after (scopes exclusion to
// only still-orphaned started entries).
func TestReconcileOrphanedRunsOnResume_DispatchTrackerOrphan_RedispatchAfterPriorCompletedRun(t *testing.T) {
	ctx := context.Background()

	priorRun := hkr73qrNewRunID(t)
	jsonlPath := hkr73qrSetupJSONL(t, func(bus eventbus.EventBus) {
		hkr73qrWriteRunStartedQ(t, bus, priorRun, "hk-redispatch", "queue-a", 0)
		hkr73qrWriteRunCompleted(t, bus, priorRun, "hk-redispatch")
		// No new run_started for the redispatch — the daemon crashed before
		// emitting it, which is exactly the hk-iwu8a crash window.
	})

	payBus := &hkr73qrPayloadBus{}
	resetter := &hkr73qrFakeResetter{}
	ledger := &hkr73qrFakeStatusLedger{status: map[string]core.CoarseStatus{
		"hk-redispatch": core.CoarseStatusInProgress,
	}}
	dispatched := lifecycle.QueueDispatchedSet{
		core.BeadID("hk-redispatch"): struct{}{},
	}

	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, payBus, resetter, ledger, "/tmp/intents", core.ProjectHash("ph"), 1, dispatched, nil)
	if got != 0 {
		t.Fatalf("emitted %d run_failed, want 0 (no run_id known for the redispatch)", got)
	}
	if len(resetter.reset) != 1 || resetter.reset[0] != core.BeadID("hk-redispatch") {
		t.Fatalf("ResetBead calls = %v, want exactly [hk-redispatch] — a redispatch after a completed prior run must still be reset", resetter.reset)
	}
}
