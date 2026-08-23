//go:build integration

package daemon_test

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

func t3hpMakeRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("t3hpMakeRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

func t3hpBudgetExhaustedPayload(t *testing.T, runID core.RunID) []byte {
	t.Helper()
	payload := core.BudgetExhaustedEventPayload{
		RunID:                 runID,
		BudgetRef:             core.BudgetRef("handler-account"),
		AttemptedDispatchCost: 0.01,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("t3hpBudgetExhaustedPayload: marshal: %v", err)
	}
	return b
}

// TestIntegration_HandlerPausePolicyGoroutineWiredBeforeSeal is the T3
// integration test for HandlerPausePolicyGoroutine composition-root wiring
// (hk-lz485).
//
// Catches hk-37zy8 class: policy goroutine existed, was unit-tested in
// isolation, but was never Subscribe()d inside daemon.Start.  A budget_exhausted
// event therefore never reached the goroutine in production.
//
// Two checks in one test:
//
//  1. Subscription-count check (pre-Seal): the bus must have exactly
//     wantSubscriptions consumers before Seal.  Any missing Subscribe call
//     drops the count and fails here, before the behavioural check.
//
//  2. Behavioural check: a synthetic budget_exhausted event must produce
//     exactly one handler_paused event with the expected fields.  This guards
//     against subscribe-but-wrong-handler regressions that the count check
//     alone cannot detect.
//
// Spec refs: specs/handler-pause.md §4, §5.2 HP-012, §7.1 HP-030;
// specs/execution-model.md §4.6; specs/scenario-harness.md §4.
// Bead: hk-lz485.
func TestIntegration_HandlerPausePolicyGoroutineWiredBeforeSeal(t *testing.T) {
	t.Parallel()

	handlerPausedCh := make(chan core.HandlerPausedPayload, 4)

	var captureBus eventbus.EventBus

	captureBusSet := make(chan struct{})

	const wantSubscriptions = 5

	busObserver := func(bus eventbus.EventBus) {
		count := eventbus.BusSubscriptionCount(bus)
		if count != wantSubscriptions {
			panic("t3hp: pre-Seal subscription count mismatch: " +
				"got handler_paused consumer not registered; " +
				"HandlerPausePolicyGoroutine.Subscribe() may be missing from daemon.Start (hk-37zy8)")
		}

		captureBus = bus

		sub := core.Subscription{
			ConsumerID:    "test-t3hp-handler-paused-observer-lz485",
			ConsumerClass: core.ConsumerClassAsynchronous,
			EventPattern: core.EventPattern{
				Types: map[core.EventType]struct{}{
					core.EventTypeHandlerPaused: {},
				},
			},
			OnPanic: core.OnPanicRecoverAndLog,
			Handler: func(_ context.Context, evt core.Event) error {
				var payload core.HandlerPausedPayload
				if err := json.Unmarshal(evt.Payload, &payload); err != nil {
					return nil
				}
				select {
				case handlerPausedCh <- payload:
				default:
				}
				return nil
			},
		}
		if _, err := bus.Subscribe(sub); err != nil {
			panic("t3hp: bus.Subscribe handler_paused observer: " + err.Error())
		}

		close(captureBusSet)
	}

	cfg := daemon.Config{
		BrPath:              "", // no work loop; no bead ledger required
		ProjectDir:          "", // no filesystem-dependent paths (pidfile, socket, WAL)
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.StartForTesting(context.Background(), cfg,
			daemon.WithBusObserver(busObserver),
		)
	}()

	select {
	case <-captureBusSet:
	case <-time.After(5 * time.Second):
		t.Fatal("t3hp: WithBusObserver did not fire within 5s; daemon.startWithHooks may be stalled")
	}

	select {
	case startErr := <-startDone:
		if startErr != nil {
			t.Fatalf("t3hp: daemon.StartForTesting returned error: %v", startErr)
		}
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Fatalf("t3hp: daemon.StartForTesting did not return within %s in no-op mode", daemon.ExportedDaemonExitHangBudget)
	}

	runID := t3hpMakeRunID(t)
	payloadBytes := t3hpBudgetExhaustedPayload(t, runID)

	emitCtx, emitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer emitCancel()

	if err := captureBus.Emit(emitCtx, core.EventTypeBudgetExhausted, payloadBytes); err != nil {
		t.Fatalf("t3hp: emit budget_exhausted: %v", err)
	}

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()

	if err := captureBus.Drain(drainCtx); err != nil {
		t.Fatalf("t3hp: bus.Drain: %v", err)
	}

	select {
	case got := <-handlerPausedCh:
		if got.AgentType != core.AgentTypeClaudeCode {
			t.Errorf("t3hp: handler_paused.agent_type=%q; want %q",
				got.AgentType, core.AgentTypeClaudeCode)
		}
		if got.Cause.FailureClass != core.FailureClassBudgetExhausted {
			t.Errorf("t3hp: handler_paused.cause.failure_class=%q; want %q",
				got.Cause.FailureClass, core.FailureClassBudgetExhausted)
		}
		if got.Cause.SubReason != "budget_exhausted_handler_account" {
			t.Errorf("t3hp: handler_paused.cause.sub_reason=%q; want %q",
				got.Cause.SubReason, "budget_exhausted_handler_account")
		}
		if got.PausedEpoch < 1 {
			t.Errorf("t3hp: handler_paused.paused_epoch=%d; want >= 1", got.PausedEpoch)
		}
		t.Logf("t3hp PASS: handler_paused received — agent_type=%q failure_class=%q sub_reason=%q epoch=%d",
			got.AgentType, got.Cause.FailureClass, got.Cause.SubReason, got.PausedEpoch)

	case <-time.After(3 * time.Second):
		t.Error("t3hp FAIL: handler_paused event never received after budget_exhausted injection + Drain; " +
			"HandlerPausePolicyGoroutine.Subscribe() may be missing from daemon.Start composition (hk-37zy8 regression)")
	}
}
