package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
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
	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, capBus)
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
	got := reconcileOrphanedRunsOnResume(context.Background(), jsonlPath, capBus)
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
	got := reconcileOrphanedRunsOnResume(context.Background(), "", capBus)
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
	got := reconcileOrphanedRunsOnResume(ctx, jsonlPath, capBus)
	if got != 0 {
		t.Errorf("got %d, want 0 when all runs have terminal events", got)
	}
	if len(capBus.events) != 0 {
		t.Errorf("got %d emitted events, want 0", len(capBus.events))
	}
}
