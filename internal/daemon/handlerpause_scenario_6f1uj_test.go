package daemon_test

// handlerpause_scenario_6f1uj_test.go — scenario test: HandlerPause policy goroutine
// wired end-to-end in daemon.Start (hk-6f1uj).
//
// # What this test covers
//
// Regression guard for the hk-37zy8 half-built-systems pattern: the
// HandlerPausePolicyGoroutine existed and was unit-tested in isolation, but was
// never Subscribe()d to the bus inside daemon.Start.  A budget_exhausted event
// therefore never reached the policy goroutine in production, so
// HandlerPauseController.IsPaused(AgentTypeClaudeCode) would always return false
// after a real handler exhausted its budget.
//
// This test exercises the full composition root via daemon.Start:
//
//   1. daemon.Start is called with BrPath="" (no work loop) and ProjectDir=""
//      (no filesystem dependencies). A TestOnlyBusObserver callback intercepts
//      the sealed bus and subscribes a test consumer for handler_paused events.
//
//   2. After Start returns, a synthetic budget_exhausted event is emitted on the
//      captured bus. The event exercises the same handler-pause policy goroutine
//      code path that a real twin (--scenario budget-exhausted) would trigger via
//      its NDJSON stdout stream.
//
//   3. bus.Drain() waits for all asynchronous consumer goroutines to complete.
//
//   4. The test asserts that exactly one handler_paused event was received, with
//      AgentType=claude-code and FailureClass=budget_exhausted.
//
// If HandlerPausePolicyGoroutine.Subscribe() is ever removed from daemon.Start,
// no handler_paused event will arrive and the test will time-out on the channel
// receive, then fail with "handler_paused event never received".
//
// Helper prefix: hpScenario (bead hk-6f1uj, per implementer-protocol
// §Helper-prefix discipline).
//
// Spec refs: specs/handler-pause.md §4 HP-ENV-001, §5.2 HP-012, §7.1 HP-030;
// specs/execution-model.md §4.6; specs/scenario-harness.md §4.
// Source: docs/scenario-test-gap-audit-2026-05-18.md #1.
// Bead: hk-6f1uj.

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

// hpScenarioMakeRunID returns a UUIDv7-based RunID for use in synthetic events.
func hpScenarioMakeRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hpScenarioMakeRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// hpScenarioBudgetExhaustedPayload builds a minimal budget_exhausted event
// payload matching core.BudgetExhaustedEventPayload.Valid() constraints.
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

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_HandlerPause_EventTripsPolicy
// ─────────────────────────────────────────────────────────────────────────────

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
	t.Parallel()

	// handlerPausedCh receives payloads of handler_paused events delivered to
	// the test's observer subscription. Buffered with capacity 4 so the async
	// dispatch goroutine never blocks.
	handlerPausedCh := make(chan core.HandlerPausedPayload, 4)

	// captureBus holds the EventBus reference extracted from TestOnlyBusObserver.
	// Populated before daemon.Start calls bus.Seal().
	var captureBus eventbus.EventBus

	// captureBusSet is closed once captureBus is populated so the emitter goroutine
	// can proceed after Start exits.
	captureBusSet := make(chan struct{})

	// TestOnlyBusObserver fires inside daemon.Start after all pre-Seal subscriptions
	// are registered and BEFORE bus.Seal() is called. We:
	//   (a) save the bus reference for post-Start event injection;
	//   (b) subscribe our test observer for handler_paused events.
	//
	// This verifies that HandlerPausePolicyGoroutine.Subscribe was called by
	// daemon.Start — if it was not, no handler_paused event will ever arrive.
	busObserver := func(bus eventbus.EventBus) {
		captureBus = bus

		// Subscribe a test observer consumer for handler_paused events.
		// ConsumerClass=Asynchronous matches how daemon.Start wires production
		// consumers; Observer class would also work here but Asynchronous is
		// consistent with the other policy-goroutine tests.
		sub := core.Subscription{
			ConsumerID:    "test-handler-paused-observer-hk6f1uj",
			ConsumerClass: core.ConsumerClassAsynchronous,
			EventPattern: core.EventPattern{
				Types: map[string]struct{}{
					string(core.EventTypeHandlerPaused): {},
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
					// Channel full — test has received enough; drop.
				}
				return nil
			},
		}
		if _, err := bus.Subscribe(sub); err != nil {
			// Cannot call t.Fatalf from a goroutine that is not the test goroutine.
			// Panic here — the test framework recovers and fails the test.
			panic("hpScenario: bus.Subscribe handler_paused: " + err.Error())
		}

		close(captureBusSet)
	}

	// daemon.Start with BrPath="" skips the work loop; ProjectDir="" skips
	// the filesystem-dependent paths (pidfile, socket, WAL checkpoint).
	// This exercises the composition root's bus setup and subscription wiring
	// without requiring a real br binary or project directory.
	cfg := daemon.Config{
		BrPath:              "", // no work loop; no bead ledger required
		ProjectDir:          "", // no filesystem-dependent paths
		TestOnlyBusObserver: busObserver,
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(context.Background(), cfg)
	}()

	// Wait for the observer to fire (captureBus populated) with a generous
	// deadline — Start should complete within milliseconds in unit-test mode.
	select {
	case <-captureBusSet:
		// Observer fired; captureBus is set.
	case <-time.After(5 * time.Second):
		t.Fatal("hpScenario: TestOnlyBusObserver did not fire within 5s; daemon.Start may be stalled")
	}

	// Wait for daemon.Start to return. With BrPath="" and ProjectDir="" it returns
	// promptly after emitting daemon_started.
	select {
	case startErr := <-startDone:
		if startErr != nil {
			t.Fatalf("hpScenario: daemon.Start returned error: %v", startErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("hpScenario: daemon.Start did not return within 10s in no-op mode")
	}

	// Emit a synthetic budget_exhausted event on the captured (sealed) bus.
	// This exercises the same path as harmonik-twin-claude --scenario budget-exhausted:
	// the twin would emit the event on its NDJSON stdout, the watcher would route it
	// to the bus, and the policy goroutine would trip the pause. Here we inject the
	// event directly so the test is self-contained and fast.
	runID := hpScenarioMakeRunID(t)
	payloadBytes := hpScenarioBudgetExhaustedPayload(t, runID)

	emitCtx, emitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer emitCancel()

	if err := captureBus.Emit(emitCtx, core.EventTypeBudgetExhausted, payloadBytes); err != nil {
		t.Fatalf("hpScenario: emit budget_exhausted: %v", err)
	}

	// Drain the bus to wait for all asynchronous consumer goroutines to complete.
	// This includes the HandlerPausePolicyGoroutine's budget_exhausted handler
	// (which calls HandlerPauseController.Pause) and the controller's subsequent
	// handler_paused emission (which our test consumer receives).
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()

	if err := captureBus.Drain(drainCtx); err != nil {
		t.Fatalf("hpScenario: bus.Drain: %v", err)
	}

	// Assert: handler_paused event was received.
	//
	// If HandlerPausePolicyGoroutine.Subscribe() was never called in daemon.Start
	// (the hk-37zy8 bug), no handler_paused event would be emitted and this
	// select would fall through to the timeout branch.
	select {
	case got := <-handlerPausedCh:
		// Validate the payload shape.
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
