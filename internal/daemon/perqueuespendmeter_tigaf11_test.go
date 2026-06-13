package daemon_test

// perqueuespendmeter_tigaf11_test.go — unit tests for PerQueueSpendMeter (NQ-X1, hk-tigaf.11).
//
// Test coverage:
//
//   - TestPerQueueSpendMeter_CappedQueueTrips        — capped queue reaches cap → paused-by-budget
//   - TestPerQueueSpendMeter_SiblingNotStarved       — under-cap sibling keeps active (the core win)
//   - TestPerQueueSpendMeter_NoCapNoChange           — zero cap → never paused (byte-identical default)
//   - TestPerQueueSpendMeter_CapOverGlobalStillTrips — cap > global is allowed; per-queue trip still fires
//   - TestPerQueueSpendMeter_RolloverResetsAndUnpauses — UTC rollover resets counters AND un-pauses
//   - TestPerQueueSpendMeter_AttributionEmptyName     — empty QueueName → no per-queue accrual
//   - TestPerQueueSpendMeter_AttributionGetMiss       — RunRegistry miss → no accrual, no error
//
// Tests invoke the unexported handleBudgetAccrual via exported seams, mirroring
// the DaemonSpendMeter test style. projectDir is "" so no disk persistence runs.
//
// Bead ref: hk-tigaf.11.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// pqBytesPerUSD mirrors the package-internal bytesPerUSD conversion (100_000
// bytes ≈ $1) so tests can size accrual chunks to land just under / over a cap.
const pqBytesPerUSD = 100_000.0

// pqMakeQueue builds a minimal active queue with the given name and spend cap.
func pqMakeQueue(name string, capUSD float64) *queue.Queue {
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "qid-" + name,
		Name:          name,
		Workers:       1,
		SpendCapUSD:   capUSD,
		Status:        queue.QueueStatusActive,
		Groups:        []queue.Group{},
	}
}

// pqMakeAccrualEvent builds a budget_accrual event for runID carrying usdUnits
// worth of output_bytes (usdUnits × pqBytesPerUSD bytes).
func pqMakeAccrualEvent(t *testing.T, runID core.RunID, usdUnits float64) core.Event {
	t.Helper()
	evID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("pqMakeAccrualEvent: uuid event: %v", err)
	}
	payload := core.BudgetAccrualPayload{
		RunID:     runID,
		SessionID: core.SessionID("synth-session"),
		CostUnits: usdUnits * pqBytesPerUSD,
		CostBasis: core.CostBasisOutputBytes,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("pqMakeAccrualEvent: marshal: %v", err)
	}
	return core.Event{
		EventID:       core.EventID(evID),
		SchemaVersion: 1,
		Type:          string(core.EventTypeBudgetAccrual),
		TimestampWall: time.Now(),
		Payload:       json.RawMessage(payloadJSON),
	}
}

// pqRegisterRun mints a run_id, registers a RunHandle for it under queueName,
// and returns the run_id. queueName "" registers a br-ready-fallback run.
func pqRegisterRun(t *testing.T, reg *daemon.RunRegistry, queueName string) core.RunID {
	t.Helper()
	runUUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("pqRegisterRun: uuid: %v", err)
	}
	runID := core.RunID(runUUID)
	daemon.ExportedRunRegistryRegister(reg, runID, &daemon.RunHandle{
		BeadID:    core.BeadID("hk-test"),
		QueueName: queueName,
	})
	return runID
}

// pqSetup wires a RunRegistry, a QueueStore preloaded with the given queues, and
// a PerQueueSpendMeter (projectDir "" → no persistence). globalCapUSD defaults to
// the env-derived value; callers override via the exported seam when needed.
func pqSetup(t *testing.T, queues ...*queue.Queue) (*daemon.RunRegistry, *daemon.QueueStore, *daemon.PerQueueSpendMeter) {
	t.Helper()
	reg := daemon.NewRunRegistry()
	store := daemon.NewQueueStore()
	for _, q := range queues {
		daemon.ExportedQueueStoreSetQueue(store, q)
	}
	meter := daemon.ExportedNewPerQueueSpendMeter(reg, store, "" /* no persistence */)
	return reg, store, meter
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPerQueueSpendMeter_CappedQueueTrips verifies a capped queue whose
// attributed spend reaches its cap transitions to paused-by-budget.
func TestPerQueueSpendMeter_CappedQueueTrips(t *testing.T) {
	t.Parallel()
	reg, store, meter := pqSetup(t, pqMakeQueue("busy", 5.0))
	ctx := context.Background()
	runID := pqRegisterRun(t, reg, "busy")

	// One accrual of $5 reaches the $5 cap.
	if err := daemon.ExportedPerQueueSpendMeterHandleBudgetAccrual(meter, ctx, pqMakeAccrualEvent(t, runID, 5.0)); err != nil {
		t.Fatalf("handleBudgetAccrual: %v", err)
	}

	if got := store.QueueByName("busy").Status; got != queue.QueueStatusPausedByBudget {
		t.Errorf("queue status = %q, want paused-by-budget", got)
	}
}

// TestPerQueueSpendMeter_SiblingNotStarved verifies that when one queue trips,
// a sibling under its own cap stays active (the core anti-starvation win).
func TestPerQueueSpendMeter_SiblingNotStarved(t *testing.T) {
	t.Parallel()
	reg, store, meter := pqSetup(t, pqMakeQueue("busy", 2.0), pqMakeQueue("calm", 100.0))
	ctx := context.Background()
	busyRun := pqRegisterRun(t, reg, "busy")
	calmRun := pqRegisterRun(t, reg, "calm")

	// busy trips at $2; calm accrues only $1 (well under its $100 cap).
	if err := daemon.ExportedPerQueueSpendMeterHandleBudgetAccrual(meter, ctx, pqMakeAccrualEvent(t, busyRun, 2.5)); err != nil {
		t.Fatalf("busy accrual: %v", err)
	}
	if err := daemon.ExportedPerQueueSpendMeterHandleBudgetAccrual(meter, ctx, pqMakeAccrualEvent(t, calmRun, 1.0)); err != nil {
		t.Fatalf("calm accrual: %v", err)
	}

	if got := store.QueueByName("busy").Status; got != queue.QueueStatusPausedByBudget {
		t.Errorf("busy status = %q, want paused-by-budget", got)
	}
	if got := store.QueueByName("calm").Status; got != queue.QueueStatusActive {
		t.Errorf("calm status = %q, want active (must NOT be starved)", got)
	}
}

// TestPerQueueSpendMeter_NoCapNoChange verifies a queue with SpendCapUSD == 0 is
// never paused regardless of accrued spend (byte-identical to pre-NQ-X1 default).
func TestPerQueueSpendMeter_NoCapNoChange(t *testing.T) {
	t.Parallel()
	reg, store, meter := pqSetup(t, pqMakeQueue("uncapped", 0.0))
	ctx := context.Background()
	runID := pqRegisterRun(t, reg, "uncapped")

	// A large accrual ($1000) must NOT pause an uncapped queue.
	if err := daemon.ExportedPerQueueSpendMeterHandleBudgetAccrual(meter, ctx, pqMakeAccrualEvent(t, runID, 1000.0)); err != nil {
		t.Fatalf("handleBudgetAccrual: %v", err)
	}

	if got := store.QueueByName("uncapped").Status; got != queue.QueueStatusActive {
		t.Errorf("uncapped queue status = %q, want active (no cap → no change)", got)
	}
}

// TestPerQueueSpendMeter_CapOverGlobalStillTrips verifies a per-queue cap greater
// than the global daily ceiling is permitted and the per-queue trip still fires
// once the (higher) per-queue cap is reached. The global ceiling binds
// independently in the live daemon (DaemonSpendMeter), so this test only asserts
// the per-queue layer's own behaviour: cap>global is accepted, not rejected.
func TestPerQueueSpendMeter_CapOverGlobalStillTrips(t *testing.T) {
	t.Parallel()
	reg, store, meter := pqSetup(t, pqMakeQueue("rich", 50.0))
	// Force a low global ceiling so the queue cap ($50) oversubscribes it.
	daemon.ExportedPerQueueSpendMeterSetGlobalCapUSD(meter, 20.0)
	ctx := context.Background()
	runID := pqRegisterRun(t, reg, "rich")

	// Below the $50 per-queue cap → still active (per-queue layer does not trip
	// at the lower global value; the global meter owns that ceiling).
	if err := daemon.ExportedPerQueueSpendMeterHandleBudgetAccrual(meter, ctx, pqMakeAccrualEvent(t, runID, 30.0)); err != nil {
		t.Fatalf("under-cap accrual: %v", err)
	}
	if got := store.QueueByName("rich").Status; got != queue.QueueStatusActive {
		t.Fatalf("status after $30 of $50 cap = %q, want active", got)
	}

	// Reaching the $50 per-queue cap → paused-by-budget.
	if err := daemon.ExportedPerQueueSpendMeterHandleBudgetAccrual(meter, ctx, pqMakeAccrualEvent(t, runID, 20.0)); err != nil {
		t.Fatalf("trip accrual: %v", err)
	}
	if got := store.QueueByName("rich").Status; got != queue.QueueStatusPausedByBudget {
		t.Errorf("status at $50 cap = %q, want paused-by-budget", got)
	}
}

// TestPerQueueSpendMeter_RolloverResetsAndUnpauses verifies that a UTC
// day-rollover resets per-queue counters AND un-pauses a paused-by-budget queue
// back to active (the only un-pause path for a budget-paused queue).
func TestPerQueueSpendMeter_RolloverResetsAndUnpauses(t *testing.T) {
	t.Parallel()
	reg, store, meter := pqSetup(t, pqMakeQueue("busy", 3.0))
	ctx := context.Background()
	runID := pqRegisterRun(t, reg, "busy")

	// Trip the queue today.
	if err := daemon.ExportedPerQueueSpendMeterHandleBudgetAccrual(meter, ctx, pqMakeAccrualEvent(t, runID, 3.0)); err != nil {
		t.Fatalf("trip accrual: %v", err)
	}
	if got := store.QueueByName("busy").Status; got != queue.QueueStatusPausedByBudget {
		t.Fatalf("pre-rollover status = %q, want paused-by-budget", got)
	}

	// Force the meter's day key to the past so the next handled event triggers a
	// rollover (resets counters + un-pauses budget-paused queues).
	daemon.ExportedPerQueueSpendMeterSetDayKey(meter, "2000-01-01")

	// A small accrual on the next "day" drives the rollover. After un-pause the
	// queue is active again; the counter was reset, so this $1 < $3 stays active.
	if err := daemon.ExportedPerQueueSpendMeterHandleBudgetAccrual(meter, ctx, pqMakeAccrualEvent(t, runID, 1.0)); err != nil {
		t.Fatalf("post-rollover accrual: %v", err)
	}
	if got := store.QueueByName("busy").Status; got != queue.QueueStatusActive {
		t.Errorf("post-rollover status = %q, want active (rollover must un-pause + reset)", got)
	}
}

// TestPerQueueSpendMeter_AttributionEmptyName verifies a budget_accrual whose run
// has an empty QueueName (br-ready-fallback) is NOT attributed to any queue.
func TestPerQueueSpendMeter_AttributionEmptyName(t *testing.T) {
	t.Parallel()
	reg, store, meter := pqSetup(t, pqMakeQueue("busy", 1.0))
	ctx := context.Background()
	// Run registered with empty QueueName → must accrue to the global meter only.
	fallbackRun := pqRegisterRun(t, reg, "")

	if err := daemon.ExportedPerQueueSpendMeterHandleBudgetAccrual(meter, ctx, pqMakeAccrualEvent(t, fallbackRun, 100.0)); err != nil {
		t.Fatalf("handleBudgetAccrual: %v", err)
	}

	// The capped "busy" queue must be untouched — the chunk was never attributed.
	if got := store.QueueByName("busy").Status; got != queue.QueueStatusActive {
		t.Errorf("busy status = %q, want active (empty-name run must not be attributed)", got)
	}
}

// TestPerQueueSpendMeter_AttributionGetMiss verifies an accrual whose run_id is
// not in the registry (handle Unregister'd before a late tail chunk) is skipped
// with no error and no per-queue effect (lossy-tail OK).
func TestPerQueueSpendMeter_AttributionGetMiss(t *testing.T) {
	t.Parallel()
	_, store, meter := pqSetup(t, pqMakeQueue("busy", 1.0))
	ctx := context.Background()

	// A run_id that was never registered (or already Unregister'd).
	missUUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	missRun := core.RunID(missUUID)

	if err := daemon.ExportedPerQueueSpendMeterHandleBudgetAccrual(meter, ctx, pqMakeAccrualEvent(t, missRun, 100.0)); err != nil {
		t.Fatalf("handleBudgetAccrual returned error on registry miss (want nil): %v", err)
	}

	if got := store.QueueByName("busy").Status; got != queue.QueueStatusActive {
		t.Errorf("busy status = %q, want active (registry-miss chunk must be a no-op)", got)
	}
}
