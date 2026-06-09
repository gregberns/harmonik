package daemon_test

// cognition_loop_scenario_hkc7lxc_test.go — scenario test: unified spend meter halts
// dispatch on per-day USD cap and on max-runs ceiling (CL-090 / CL-090a / CL-INV-006).
//
// # What this test covers
//
// The full chain from raw accrual / run-count events through the DaemonSpendMeter
// to the HP-012 handler-pause policy:
//
//   DaemonSpendMeter             HandlerPausePolicyGoroutine
//   budget_accrual ──────────► budget_exhausted ──────────► handler_paused
//   run_started    ──(CL-090a)─┘                            (HP-012 fires)
//
// This is distinct from TestScenario_HandlerPause_EventTripsPolicy (hk-6f1uj),
// which verifies that a *directly injected* budget_exhausted trips HP-012.  This
// test verifies that the DaemonSpendMeter itself is wired in the composition root
// and that it drives budget_exhausted from first-principles events.
//
// Two sub-tests cover the two arms of the unified ceiling (CL-090 §4.11):
//
//   A — USD / bytes-proxy path (CL-090):
//       A single budget_accrual event whose CostUnits equals the bytes cap (set
//       to 1000 bytes via WithSpendMeterObserver) trips the meter; budget_exhausted
//       with budget_scope=handler_account is emitted; handler_paused follows.
//
//   B — max-runs path (CL-090a):
//       Three run_started events with maxRunsPerDay=3 (set via WithSpendMeterObserver)
//       trip the meter; same downstream sequence; SpentUSD = CapUSD = 3 (run-count
//       proxy, not real USD — the meter uses run-count as the unit on this arm).
//
// Observable terminal condition per CL-INV-006:
//   - budget_exhausted{budget_scope=handler_account} record in the event stream.
//   - handler_paused event for the claude handler type.
//   - No second budget_exhausted (idempotent per CL-090 / DaemonSpendMeter).
//
// Helper prefix: clScenario (bead hk-c7lxc, per implementer-protocol §Helper-prefix
// discipline).
//
// Spec refs: specs/cognition-loop.md §4.11 CL-090, CL-090a, CL-INV-006;
// specs/handler-pause.md §5.2 HP-012, §7.1 HP-030, §11a;
// specs/event-model.md §8.4.2, §8.4.3.
// Bead: hk-c7lxc.

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

// clScenarioMakeRunID returns a UUIDv7-based RunID for use in synthetic events.
func clScenarioMakeRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("clScenarioMakeRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// clScenarioMakeAccrualEvent builds a synthetic budget_accrual event with the
// given CostBasis and CostUnits for the bytes-proxy ceiling arm (CL-090).
func clScenarioMakeAccrualEvent(t *testing.T, basis core.CostBasis, units float64) []byte {
	t.Helper()
	runID := clScenarioMakeRunID(t)
	payload := core.BudgetAccrualPayload{
		RunID:     runID,
		SessionID: core.SessionID("clScenario-session"),
		CostUnits: units,
		CostBasis: basis,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("clScenarioMakeAccrualEvent: marshal: %v", err)
	}
	return b
}

// clScenarioCollectHandlerPaused subscribes to handler_paused events on bus and
// returns a channel that receives the first payload.  Must be called BEFORE
// bus.Seal() (i.e., inside the WithBusObserver callback).
func clScenarioCollectHandlerPaused(t *testing.T, bus eventbus.EventBus) <-chan core.HandlerPausedPayload {
	t.Helper()
	ch := make(chan core.HandlerPausedPayload, 4)
	sub := core.Subscription{
		ConsumerID:    "clScenario-handler-paused-" + t.Name(),
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[string]struct{}{
				string(core.EventTypeHandlerPaused): {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			var p core.HandlerPausedPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				return nil
			}
			select {
			case ch <- p:
			default:
			}
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		panic("clScenario: Subscribe handler_paused: " + err.Error())
	}
	return ch
}

// clScenarioCollectBudgetExhausted subscribes to budget_exhausted events on bus
// and returns a channel that receives payloads.  Must be called BEFORE bus.Seal().
func clScenarioCollectBudgetExhausted(t *testing.T, bus eventbus.EventBus) <-chan core.BudgetExhaustedEventPayload {
	t.Helper()
	ch := make(chan core.BudgetExhaustedEventPayload, 4)
	sub := core.Subscription{
		ConsumerID:    "clScenario-budget-exhausted-" + t.Name(),
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[string]struct{}{
				string(core.EventTypeBudgetExhausted): {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			var p core.BudgetExhaustedEventPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				return nil
			}
			select {
			case ch <- p:
			default:
			}
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		panic("clScenario: Subscribe budget_exhausted: " + err.Error())
	}
	return ch
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_CognitionLoop_UsdCapTripsHandlerPause
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_CognitionLoop_UsdCapTripsHandlerPause validates the CL-090 bytes-proxy
// arm of the unified spend meter: a budget_accrual event whose CostUnits equals the
// bytes cap causes DaemonSpendMeter to emit budget_exhausted{handler_account}, which
// the HP-012 policy goroutine turns into a handler_paused event.
//
// The test uses WithSpendMeterObserver to set the bytes cap to 1000 so that a
// single accrual event trips the meter without needing to emit millions of bytes.
//
// Wiring under test:
//
//	budget_accrual ──[DaemonSpendMeter]──► budget_exhausted ──[HP-012]──► handler_paused
//
// Spec refs: specs/cognition-loop.md §4.11 CL-090, CL-INV-006;
// specs/handler-pause.md §11a HP-012, §7.1 HP-030.
// Bead: hk-c7lxc.
func TestScenario_CognitionLoop_UsdCapTripsHandlerPause(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	const capBytes = 1000.0

	var handlerPausedCh <-chan core.HandlerPausedPayload
	var budgetExhaustedCh <-chan core.BudgetExhaustedEventPayload

	// WithSpendMeterObserver fires after DaemonSpendMeter.Subscribe and before
	// bus.Seal.  We set tiny caps and subscribe our test collectors here.
	spendMeterObs := daemon.WithSpendMeterObserver(func(m *daemon.DaemonSpendMeter) {
		daemon.ExportedSpendMeterSetMaxRunsPerDay(m, 9999) // disable max-runs arm
		daemon.ExportedSpendMeterSetDailyCapBytes(m, capBytes)
	})

	var captureBus eventbus.EventBus
	busSetCh := make(chan struct{})

	busObs := daemon.WithBusObserver(func(bus eventbus.EventBus) {
		captureBus = bus
		handlerPausedCh = clScenarioCollectHandlerPaused(t, bus)
		budgetExhaustedCh = clScenarioCollectBudgetExhausted(t, bus)
		close(busSetCh)
	})

	cfg := daemon.Config{
		BrPath:     "",
		ProjectDir: "",
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.StartForTesting(context.Background(), cfg, spendMeterObs, busObs)
	}()

	select {
	case <-busSetCh:
	case <-time.After(5 * time.Second):
		t.Fatal("clScenario USD: WithBusObserver did not fire within 5s")
	}

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("clScenario USD: daemon.StartForTesting: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("clScenario USD: daemon.StartForTesting did not return within 10s")
	}

	// Emit one budget_accrual event whose CostUnits = capBytes (the full cap).
	// DaemonSpendMeter.handleBudgetAccrual will accumulate bytesToday = capBytes
	// and emit budget_exhausted{budget_scope=handler_account}.
	accrualPayload := clScenarioMakeAccrualEvent(t, core.CostBasisOutputBytes, capBytes)
	emitCtx, emitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer emitCancel()

	if err := captureBus.Emit(emitCtx, core.EventTypeBudgetAccrual, accrualPayload); err != nil {
		t.Fatalf("clScenario USD: emit budget_accrual: %v", err)
	}

	// Drain to wait for all async consumers (DaemonSpendMeter → HP-012 →
	// HandlerPauseController → handler_paused emission → test collector).
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()

	if err := captureBus.Drain(drainCtx); err != nil {
		t.Fatalf("clScenario USD: bus.Drain: %v", err)
	}

	// Assert budget_exhausted was emitted with handler_account scope.
	select {
	case got := <-budgetExhaustedCh:
		if got.BudgetScope == nil || *got.BudgetScope != core.BudgetScopeHandlerAccount {
			t.Errorf("clScenario USD: budget_exhausted.budget_scope=%v; want BudgetScopeHandlerAccount", got.BudgetScope)
		}
		if got.BudgetRef == "" {
			t.Error("clScenario USD: budget_exhausted.budget_ref is empty")
		}
		if got.SpentUSD == nil {
			t.Error("clScenario USD: budget_exhausted.spent_usd is nil")
		}
		if got.CapUSD == nil {
			t.Error("clScenario USD: budget_exhausted.cap_usd is nil")
		}
		t.Logf("clScenario USD: budget_exhausted received — scope=%v ref=%q spent=%.4f cap=%.4f",
			got.BudgetScope, got.BudgetRef,
			clScenarioPtrFloat64OrZero(got.SpentUSD), clScenarioPtrFloat64OrZero(got.CapUSD))
	case <-time.After(3 * time.Second):
		t.Error("clScenario USD FAIL: budget_exhausted never received after budget_accrual injection + Drain; " +
			"DaemonSpendMeter.Subscribe() may be missing from daemon.Start composition (CL-090 regression)")
	}

	// Assert handler_paused was emitted — HP-012 must fire on handler_account exhaustion.
	select {
	case got := <-handlerPausedCh:
		if got.AgentType != core.AgentTypeClaudeCode {
			t.Errorf("clScenario USD: handler_paused.agent_type=%q; want %q",
				got.AgentType, core.AgentTypeClaudeCode)
		}
		if got.Cause.FailureClass != core.FailureClassBudgetExhausted {
			t.Errorf("clScenario USD: handler_paused.cause.failure_class=%q; want %q",
				got.Cause.FailureClass, core.FailureClassBudgetExhausted)
		}
		if got.Cause.SubReason != "budget_exhausted_handler_account" {
			t.Errorf("clScenario USD: handler_paused.cause.sub_reason=%q; want %q",
				got.Cause.SubReason, "budget_exhausted_handler_account")
		}
		if got.PausedEpoch < 1 {
			t.Errorf("clScenario USD: handler_paused.paused_epoch=%d; want >= 1", got.PausedEpoch)
		}
		t.Logf("clScenario USD PASS: handler_paused received — agent_type=%q failure_class=%q sub_reason=%q epoch=%d",
			got.AgentType, got.Cause.FailureClass, got.Cause.SubReason, got.PausedEpoch)
	case <-time.After(3 * time.Second):
		t.Error("clScenario USD FAIL: handler_paused never received after budget_accrual + Drain; " +
			"DaemonSpendMeter or HandlerPausePolicyGoroutine wiring broken (CL-090 / HP-012 regression)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_CognitionLoop_MaxRunsTripsHandlerPause
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_CognitionLoop_MaxRunsTripsHandlerPause validates the CL-090a max-runs
// arm of the unified spend meter: N run_started events where N = maxRunsPerDay cause
// DaemonSpendMeter to emit budget_exhausted{handler_account}, which the HP-012 policy
// goroutine turns into a handler_paused event.  No USD is over-spent — the max-runs
// ceiling is a loss-proof backstop (CL-090a).
//
// The test uses WithSpendMeterObserver to set maxRunsPerDay=3 so only three
// run_started events are needed to trip the meter.
//
// Wiring under test:
//
//	run_started × 3 ──[DaemonSpendMeter CL-090a]──► budget_exhausted ──[HP-012]──► handler_paused
//
// Spec refs: specs/cognition-loop.md §4.11 CL-090a, CL-INV-006;
// specs/handler-pause.md §11a HP-012, §7.1 HP-030.
// Bead: hk-c7lxc.
func TestScenario_CognitionLoop_MaxRunsTripsHandlerPause(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	const maxRuns = 3

	var handlerPausedCh <-chan core.HandlerPausedPayload
	var budgetExhaustedCh <-chan core.BudgetExhaustedEventPayload

	spendMeterObs := daemon.WithSpendMeterObserver(func(m *daemon.DaemonSpendMeter) {
		daemon.ExportedSpendMeterSetMaxRunsPerDay(m, maxRuns)
		daemon.ExportedSpendMeterSetDailyCapBytes(m, 0) // disable bytes-proxy arm
	})

	var captureBus eventbus.EventBus
	busSetCh := make(chan struct{})

	busObs := daemon.WithBusObserver(func(bus eventbus.EventBus) {
		captureBus = bus
		handlerPausedCh = clScenarioCollectHandlerPaused(t, bus)
		budgetExhaustedCh = clScenarioCollectBudgetExhausted(t, bus)
		close(busSetCh)
	})

	cfg := daemon.Config{
		BrPath:     "",
		ProjectDir: "",
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.StartForTesting(context.Background(), cfg, spendMeterObs, busObs)
	}()

	select {
	case <-busSetCh:
	case <-time.After(5 * time.Second):
		t.Fatal("clScenario MaxRuns: WithBusObserver did not fire within 5s")
	}

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("clScenario MaxRuns: daemon.StartForTesting: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("clScenario MaxRuns: daemon.StartForTesting did not return within 10s")
	}

	// Emit maxRuns run_started events.  The last one trips the meter.
	emitCtx, emitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer emitCancel()

	for i := 0; i < maxRuns; i++ {
		// run_started payload: handleRunStarted does not inspect the payload, so
		// an empty JSON object is sufficient.
		if err := captureBus.Emit(emitCtx, core.EventTypeRunStarted, json.RawMessage(`{}`)); err != nil {
			t.Fatalf("clScenario MaxRuns: emit run_started[%d]: %v", i, err)
		}
	}

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()

	if err := captureBus.Drain(drainCtx); err != nil {
		t.Fatalf("clScenario MaxRuns: bus.Drain: %v", err)
	}

	// Assert budget_exhausted received with handler_account scope.
	select {
	case got := <-budgetExhaustedCh:
		if got.BudgetScope == nil || *got.BudgetScope != core.BudgetScopeHandlerAccount {
			t.Errorf("clScenario MaxRuns: budget_exhausted.budget_scope=%v; want BudgetScopeHandlerAccount", got.BudgetScope)
		}
		if got.BudgetRef == "" {
			t.Error("clScenario MaxRuns: budget_exhausted.budget_ref is empty")
		}
		// On the max-runs arm SpentUSD and CapUSD are the run counts (float proxy).
		if got.SpentUSD == nil {
			t.Error("clScenario MaxRuns: budget_exhausted.spent_usd is nil")
		} else if *got.SpentUSD < float64(maxRuns) {
			t.Errorf("clScenario MaxRuns: budget_exhausted.spent_usd=%.0f; want >= %d (max-runs proxy)", *got.SpentUSD, maxRuns)
		}
		if got.CapUSD == nil {
			t.Error("clScenario MaxRuns: budget_exhausted.cap_usd is nil")
		} else if *got.CapUSD != float64(maxRuns) {
			t.Errorf("clScenario MaxRuns: budget_exhausted.cap_usd=%.0f; want %.0f (maxRunsPerDay)",
				*got.CapUSD, float64(maxRuns))
		}
		t.Logf("clScenario MaxRuns: budget_exhausted received — scope=%v ref=%q spent=%.0f cap=%.0f",
			got.BudgetScope, got.BudgetRef,
			clScenarioPtrFloat64OrZero(got.SpentUSD), clScenarioPtrFloat64OrZero(got.CapUSD))
	case <-time.After(3 * time.Second):
		t.Error("clScenario MaxRuns FAIL: budget_exhausted never received after run_started events + Drain; " +
			"DaemonSpendMeter run-count wiring broken (CL-090a regression)")
	}

	// Assert handler_paused received — HP-012 must fire.
	select {
	case got := <-handlerPausedCh:
		if got.AgentType != core.AgentTypeClaudeCode {
			t.Errorf("clScenario MaxRuns: handler_paused.agent_type=%q; want %q",
				got.AgentType, core.AgentTypeClaudeCode)
		}
		if got.Cause.FailureClass != core.FailureClassBudgetExhausted {
			t.Errorf("clScenario MaxRuns: handler_paused.cause.failure_class=%q; want %q",
				got.Cause.FailureClass, core.FailureClassBudgetExhausted)
		}
		if got.Cause.SubReason != "budget_exhausted_handler_account" {
			t.Errorf("clScenario MaxRuns: handler_paused.cause.sub_reason=%q; want %q",
				got.Cause.SubReason, "budget_exhausted_handler_account")
		}
		if got.PausedEpoch < 1 {
			t.Errorf("clScenario MaxRuns: handler_paused.paused_epoch=%d; want >= 1", got.PausedEpoch)
		}
		t.Logf("clScenario MaxRuns PASS: handler_paused received — agent_type=%q failure_class=%q sub_reason=%q epoch=%d",
			got.AgentType, got.Cause.FailureClass, got.Cause.SubReason, got.PausedEpoch)
	case <-time.After(3 * time.Second):
		t.Error("clScenario MaxRuns FAIL: handler_paused never received after run_started events + Drain; " +
			"DaemonSpendMeter or HandlerPausePolicyGoroutine wiring broken (CL-090a / HP-012 regression)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// clScenarioPtrFloat64OrZero returns *p if p != nil, else 0.
func clScenarioPtrFloat64OrZero(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
