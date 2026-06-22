package daemon_test

// spendmeter_hkk3f8g_test.go — unit tests for DaemonSpendMeter (hk-k3f8g).
//
// Test coverage:
//
//   - TestDaemonSpendMeter_MaxRunsTripOnThreshold    — run count reaches max → budget_exhausted emitted
//   - TestDaemonSpendMeter_MaxRunsNoTripBelowThreshold — run count below max → no event
//   - TestDaemonSpendMeter_BytesTripOnThreshold      — bytes reach cap → budget_exhausted emitted
//   - TestDaemonSpendMeter_BytesNoTripBelowThreshold — bytes below cap → no event
//   - TestDaemonSpendMeter_IdempotentExhausted       — second trigger after exhausted → no second event
//   - TestDaemonSpendMeter_ExhaustedEventFields      — event carries handler_account scope + budget ref
//   - TestDaemonSpendMeter_NonOutputBytesIgnored     — accrual with other CostBasis → no accumulation
//
// Tests use synthetic event delivery (direct handler invocation via exported
// seams) to avoid coupling to live bus lifecycle.
//
// Bead ref: hk-k3f8g.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────────────────────

// smSetup constructs an in-memory bus, a DaemonSpendMeter, and a collector for
// budget_exhausted events. All subscriptions are registered before bus.Seal
// per EV-009. The caller receives (bus, meter, *collected) ready for test use.
func smSetup(t *testing.T, maxRuns int, capBytes float64) (eventbus.EventBus, *daemon.DaemonSpendMeter, *[]core.BudgetExhaustedEventPayload) {
	t.Helper()
	bus := eventbus.NewBusImpl()

	// 1. Build the meter (does NOT subscribe yet — just holds state).
	meter := daemon.ExportedNewDaemonSpendMeter(bus)
	daemon.ExportedSpendMeterSetMaxRunsPerDay(meter, maxRuns)
	daemon.ExportedSpendMeterSetDailyCapBytes(meter, capBytes)

	// 2. Register the test collector BEFORE Seal (EV-009).
	var collected []core.BudgetExhaustedEventPayload
	sub := core.Subscription{
		ConsumerID:    "test-budget-exhausted-collector-" + t.Name(),
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				core.EventTypeBudgetExhausted: {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			var p core.BudgetExhaustedEventPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				t.Errorf("collector: unmarshal: %v", err)
				return nil
			}
			collected = append(collected, p)
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("smSetup: Subscribe collector: %v", err)
	}

	// 3. Seal after all subscribers are registered.
	if err := bus.Seal(); err != nil {
		t.Fatalf("smSetup: bus.Seal: %v", err)
	}
	return bus, meter, &collected
}

// smMakeRunStartedEvent builds a minimal synthetic run_started event.
func smMakeRunStartedEvent(t *testing.T) core.Event {
	t.Helper()
	evID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("smMakeRunStartedEvent: uuid: %v", err)
	}
	return core.Event{
		EventID:       core.EventID(evID),
		SchemaVersion: 1,
		Type:          string(core.EventTypeRunStarted),
		TimestampWall: time.Now(),
		// Payload intentionally empty — handleRunStarted does not inspect it.
		Payload: json.RawMessage(`{}`),
	}
}

// smMakeAccrualEvent builds a synthetic budget_accrual event with the given
// CostBasis and CostUnits.
func smMakeAccrualEvent(t *testing.T, basis core.CostBasis, units float64) core.Event {
	t.Helper()
	runUUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("smMakeAccrualEvent: uuid run: %v", err)
	}
	evID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("smMakeAccrualEvent: uuid event: %v", err)
	}
	payload := core.BudgetAccrualPayload{
		RunID:     core.RunID(runUUID),
		SessionID: core.SessionID("synth-session"),
		CostUnits: units,
		CostBasis: basis,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("smMakeAccrualEvent: marshal: %v", err)
	}
	return core.Event{
		EventID:       core.EventID(evID),
		SchemaVersion: 1,
		Type:          string(core.EventTypeBudgetAccrual),
		TimestampWall: time.Now(),
		Payload:       json.RawMessage(payloadJSON),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — max-runs ceiling (CL-090a)
// ─────────────────────────────────────────────────────────────────────────────

// TestDaemonSpendMeter_MaxRunsNoTripBelowThreshold verifies that fewer than
// maxRuns run_started events do NOT cause a budget_exhausted emission.
func TestDaemonSpendMeter_MaxRunsNoTripBelowThreshold(t *testing.T) {
	t.Parallel()

	const maxRuns = 3
	_, meter, collected := smSetup(t, maxRuns, 0 /* bytes-cap disabled */)

	ctx := context.Background()
	for i := 0; i < maxRuns-1; i++ {
		evt := smMakeRunStartedEvent(t)
		if err := daemon.ExportedSpendMeterHandleRunStarted(meter, ctx, evt); err != nil {
			t.Fatalf("handleRunStarted[%d]: %v", i, err)
		}
	}

	if len(*collected) != 0 {
		t.Errorf("got %d budget_exhausted events below threshold; want 0", len(*collected))
	}
}

// TestDaemonSpendMeter_MaxRunsTripOnThreshold verifies that run_started count
// reaching maxRuns causes exactly one budget_exhausted emission.
func TestDaemonSpendMeter_MaxRunsTripOnThreshold(t *testing.T) {
	t.Parallel()

	const maxRuns = 3
	_, meter, collected := smSetup(t, maxRuns, 0 /* bytes-cap disabled */)

	ctx := context.Background()
	for i := 0; i < maxRuns; i++ {
		evt := smMakeRunStartedEvent(t)
		if err := daemon.ExportedSpendMeterHandleRunStarted(meter, ctx, evt); err != nil {
			t.Fatalf("handleRunStarted[%d]: %v", i, err)
		}
	}

	if len(*collected) != 1 {
		t.Errorf("got %d budget_exhausted events at threshold; want 1", len(*collected))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — bytes proxy ceiling (CL-090)
// ─────────────────────────────────────────────────────────────────────────────

// TestDaemonSpendMeter_BytesNoTripBelowThreshold verifies that output_bytes
// below dailyCapBytes does NOT trigger exhaustion.
func TestDaemonSpendMeter_BytesNoTripBelowThreshold(t *testing.T) {
	t.Parallel()

	const capBytes = 1000.0
	_, meter, collected := smSetup(t, 9999 /* effectively unlimited */, capBytes)

	ctx := context.Background()
	evt := smMakeAccrualEvent(t, core.CostBasisOutputBytes, capBytes-1)
	if err := daemon.ExportedSpendMeterHandleBudgetAccrual(meter, ctx, evt); err != nil {
		t.Fatalf("handleBudgetAccrual: %v", err)
	}

	if len(*collected) != 0 {
		t.Errorf("got %d budget_exhausted events below bytes cap; want 0", len(*collected))
	}
}

// TestDaemonSpendMeter_BytesTripOnThreshold verifies that output_bytes
// reaching dailyCapBytes triggers exactly one budget_exhausted emission.
func TestDaemonSpendMeter_BytesTripOnThreshold(t *testing.T) {
	t.Parallel()

	const capBytes = 1000.0
	_, meter, collected := smSetup(t, 9999 /* effectively unlimited */, capBytes)

	ctx := context.Background()
	evt := smMakeAccrualEvent(t, core.CostBasisOutputBytes, capBytes)
	if err := daemon.ExportedSpendMeterHandleBudgetAccrual(meter, ctx, evt); err != nil {
		t.Fatalf("handleBudgetAccrual: %v", err)
	}

	if len(*collected) != 1 {
		t.Errorf("got %d budget_exhausted events at bytes cap; want 1", len(*collected))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — idempotency
// ─────────────────────────────────────────────────────────────────────────────

// TestDaemonSpendMeter_IdempotentExhausted verifies that triggering the meter
// twice on the same day emits budget_exhausted exactly once.
func TestDaemonSpendMeter_IdempotentExhausted(t *testing.T) {
	t.Parallel()

	const maxRuns = 1
	_, meter, collected := smSetup(t, maxRuns, 0 /* bytes-cap disabled */)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		evt := smMakeRunStartedEvent(t)
		if err := daemon.ExportedSpendMeterHandleRunStarted(meter, ctx, evt); err != nil {
			t.Fatalf("handleRunStarted[%d]: %v", i, err)
		}
	}

	if len(*collected) != 1 {
		t.Errorf("got %d budget_exhausted events for 3 runs above maxRuns=1; want exactly 1", len(*collected))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — event payload correctness
// ─────────────────────────────────────────────────────────────────────────────

// TestDaemonSpendMeter_ExhaustedEventFields verifies that the emitted
// budget_exhausted event carries the correct BudgetScope and BudgetRef.
func TestDaemonSpendMeter_ExhaustedEventFields(t *testing.T) {
	t.Parallel()

	const maxRuns = 1
	_, meter, collected := smSetup(t, maxRuns, 0)

	ctx := context.Background()
	evt := smMakeRunStartedEvent(t)
	if err := daemon.ExportedSpendMeterHandleRunStarted(meter, ctx, evt); err != nil {
		t.Fatalf("handleRunStarted: %v", err)
	}

	if len(*collected) != 1 {
		t.Fatalf("expected 1 budget_exhausted event, got %d", len(*collected))
	}
	p := (*collected)[0]

	if p.BudgetScope == nil || *p.BudgetScope != core.BudgetScopeHandlerAccount {
		t.Errorf("BudgetScope = %v, want BudgetScopeHandlerAccount", p.BudgetScope)
	}
	if p.BudgetRef == "" {
		t.Error("BudgetRef is empty")
	}
	if p.SpentUSD == nil {
		t.Error("SpentUSD is nil; expected a value")
	}
	if p.CapUSD == nil {
		t.Error("CapUSD is nil; expected a value")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — non-output-bytes accrual ignored
// ─────────────────────────────────────────────────────────────────────────────

// TestDaemonSpendMeter_NonOutputBytesIgnored verifies that budget_accrual events
// with a CostBasis other than output_bytes do not contribute to the cap.
func TestDaemonSpendMeter_NonOutputBytesIgnored(t *testing.T) {
	t.Parallel()

	const capBytes = 500.0
	_, meter, collected := smSetup(t, 9999, capBytes)

	ctx := context.Background()
	// Send a large accrual with a non-output-bytes basis — should NOT count.
	evt := smMakeAccrualEvent(t, core.CostBasis("input_tokens"), capBytes*10)
	if err := daemon.ExportedSpendMeterHandleBudgetAccrual(meter, ctx, evt); err != nil {
		t.Fatalf("handleBudgetAccrual: %v", err)
	}

	if len(*collected) != 0 {
		t.Errorf("got %d budget_exhausted events for non-output_bytes accrual; want 0", len(*collected))
	}
}
