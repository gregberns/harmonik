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

func hpScenarioMakeRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hpScenarioMakeRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

func hpScenarioBudgetExhaustedPayload(t *testing.T, runID core.RunID) []byte {
	t.Helper()
	payload := core.BudgetExhaustedEventPayload{
		RunID:                 runID,
		BudgetRef:             core.BudgetRef("handler-account"),
		AttemptedDispatchCost: 0.01,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("hpScenarioBudgetExhaustedPayload: marshal: %v", err)
	}
	return b
}

// TestScenario_HandlerPause_EventTripsPolicy is the end-to-end scenario test for
// the HandlerPause policy goroutine wired in daemon.Start (hk-6f1uj).
//
// Catches the hk-37zy8 half-built-systems pattern: policy goroutine existed and
// was unit-tested but was never Subscribe()d in the composition root. A full-stack
// test that boots daemon.Start, injects a budget_exhausted event, and asserts
// handler_paused is emitted would have caught this before merge.
//
// Spec refs: specs/handler-pause.md §4, §5.2 HP-012, §7.1 HP-030;
// specs/execution-model.md §4.6; specs/scenario-harness.md §4.
// Bead: hk-6f1uj.
func TestScenario_HandlerPause_EventTripsPolicy(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	handlerPausedCh := make(chan core.HandlerPausedPayload, 4)

	var captureBus eventbus.EventBus

	captureBusSet := make(chan struct{})

	busObserver := func(bus eventbus.EventBus) {
		captureBus = bus

		sub := core.Subscription{
			ConsumerID:    "test-handler-paused-observer-hk6f1uj",
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
					return nil // silently skip malformed payloads in test consumer
				}
				select {
				case handlerPausedCh <- payload:
				default:
				}
				return nil
			},
		}
		if _, err := bus.Subscribe(sub); err != nil {
			panic("hpScenario: bus.Subscribe handler_paused: " + err.Error())
		}

		close(captureBusSet)
	}

	cfg := daemon.Config{
		BrPath:              "", // no work loop; no bead ledger required
		ProjectDir:          "", // no filesystem-dependent paths
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
		t.Fatal("hpScenario: WithBusObserver did not fire within 5s; daemon.startWithHooks may be stalled")
	}

	select {
	case startErr := <-startDone:
		if startErr != nil {
			t.Fatalf("hpScenario: daemon.Start returned error: %v", startErr)
		}
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Fatalf("hpScenario: daemon.Start did not return within %s in no-op mode", daemon.ExportedDaemonExitHangBudget)
	}

	runID := hpScenarioMakeRunID(t)
	payloadBytes := hpScenarioBudgetExhaustedPayload(t, runID)

	emitCtx, emitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer emitCancel()

	if err := captureBus.Emit(emitCtx, core.EventTypeBudgetExhausted, payloadBytes); err != nil {
		t.Fatalf("hpScenario: emit budget_exhausted: %v", err)
	}

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()

	if err := captureBus.Drain(drainCtx); err != nil {
		t.Fatalf("hpScenario: bus.Drain: %v", err)
	}

	select {
	case got := <-handlerPausedCh:
		if got.AgentType != core.AgentTypeClaudeCode {
			t.Errorf("hpScenario: handler_paused.agent_type=%q; want %q",
				got.AgentType, core.AgentTypeClaudeCode)
		}
		if got.Cause.FailureClass != core.FailureClassBudgetExhausted {
			t.Errorf("hpScenario: handler_paused.cause.failure_class=%q; want %q",
				got.Cause.FailureClass, core.FailureClassBudgetExhausted)
		}
		if got.Cause.SubReason != "budget_exhausted_handler_account" {
			t.Errorf("hpScenario: handler_paused.cause.sub_reason=%q; want %q",
				got.Cause.SubReason, "budget_exhausted_handler_account")
		}
		if got.PausedEpoch < 1 {
			t.Errorf("hpScenario: handler_paused.paused_epoch=%d; want >= 1", got.PausedEpoch)
		}
		t.Logf("hpScenario PASS: handler_paused received — agent_type=%q failure_class=%q sub_reason=%q epoch=%d",
			got.AgentType, got.Cause.FailureClass, got.Cause.SubReason, got.PausedEpoch)

	case <-time.After(3 * time.Second):
		t.Error("hpScenario FAIL: handler_paused event never received after budget_exhausted injection + Drain; " +
			"HandlerPausePolicyGoroutine.Subscribe() may be missing from daemon.Start composition (hk-37zy8 regression)")
	}
}
