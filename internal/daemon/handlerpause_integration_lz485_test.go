//go:build integration

package daemon_test

// handlerpause_integration_lz485_test.go — T3 integration test: HandlerPausePolicyGoroutine
// wired before bus.Seal (hk-lz485).
//
// # What this test covers
//
// Integration tier (//go:build integration) assertion that the composition root
// in daemon.Start calls HandlerPausePolicyGoroutine.Subscribe(bus) before
// bus.Seal().  This is the T3-track version (integration tests with build-tag
// gating) of the always-running composition check in
// handlerpause_policy_composition_37zy8_test.go.
//
// Catches: hk-37zy8 class — policy goroutine existed and was unit-tested but
// was never Subscribe()d in the production composition root.  This test would
// have caught the regression before merge.
//
// Two assertions are made:
//
//  1. Subscription count: WithBusObserver fires before bus.Seal(); the bus has
//     exactly wantSubscriptions consumers registered at that moment.  A count
//     below wantSubscriptions means at least one goroutine's Subscribe call is
//     missing from daemon.Start.
//
//  2. Behavioural wiring: a synthetic budget_exhausted event is injected on the
//     captured bus; the test asserts that exactly one handler_paused event is
//     emitted with the expected agent_type and failure_class.  This exercises the
//     same code path as a real twin emitting via its NDJSON stdout stream.
//
// Helper prefix: t3hp (bead hk-lz485, per implementer-protocol §Helper-prefix
// discipline).
//
// Run via:
//
//	go test -race -tags=integration ./internal/daemon/...
//
// Spec refs: specs/handler-pause.md §4 HP-ENV-001, §5.2 HP-012, §7.1 HP-030;
// specs/execution-model.md §4.6; specs/scenario-harness.md §4.
// T3 track source: ~/.kerf/projects/gregberns-harmonik/testing-strategy-uplift/03-components.md §T3.
// Bead: hk-lz485.

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

// t3hpMakeRunID returns a UUIDv7-based RunID for use in synthetic events.
func t3hpMakeRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("t3hpMakeRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// t3hpBudgetExhaustedPayload builds a minimal budget_exhausted event payload
// matching core.BudgetExhaustedEventPayload.Valid() constraints.
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

// ─────────────────────────────────────────────────────────────────────────────
// TestIntegration_HandlerPausePolicyGoroutineWiredBeforeSeal
// ─────────────────────────────────────────────────────────────────────────────

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

	// handlerPausedCh receives payloads of handler_paused events emitted after
	// the budget_exhausted injection.  Buffered with capacity 4 so the async
	// dispatch goroutine never blocks.
	handlerPausedCh := make(chan core.HandlerPausedPayload, 4)

	// captureBus holds the EventBus extracted from the WithBusObserver hook.
	// Populated before bus.Seal() in startWithHooks, so Emit calls after Start
	// returns are on the sealed (fully-wired) bus.
	var captureBus eventbus.EventBus

	// captureBusSet is closed once captureBus is populated so the emitter
	// goroutine can proceed after StartForTesting exits.
	captureBusSet := make(chan struct{})

	// WithBusObserver fires inside startWithHooks after all pre-Seal
	// subscriptions are registered and BEFORE bus.Seal() is called.
	//
	// Assertion 1 — subscription count:
	// Exactly wantSubscriptions consumers must be registered at this point.
	// A regression that removes HandlerPausePolicyGoroutine.Subscribe drops
	// the count by 2 (agent_rate_limit_status + budget_exhausted) and fails here.
	//
	// wantSubscriptions is 5:
	//   1. agent_rate_limit_status — HandlerPausePolicyGoroutine rate-limit hysteresis (hk-37zy8)
	//   2. budget_exhausted        — HandlerPausePolicyGoroutine budget-exhausted logic (hk-37zy8)
	//   3. operator_pause_status   — QueueOperatorEventConsumer pause → paused-by-drain (hk-7urls)
	//   4. operator_resuming       — QueueOperatorEventConsumer resume → active (hk-7urls)
	//   5. * (wildcard)            — SubscribeHub fans events to socket 'subscribe' op (hk-6ynv4)
	const wantSubscriptions = 5

	busObserver := func(bus eventbus.EventBus) {
		count := eventbus.BusSubscriptionCount(bus)
		if count != wantSubscriptions {
			// Cannot call t.Fatalf from a non-test goroutine; panic so the
			// test framework recovers and reports a failure.
			panic("t3hp: pre-Seal subscription count mismatch: " +
				"got handler_paused consumer not registered; " +
				"HandlerPausePolicyGoroutine.Subscribe() may be missing from daemon.Start (hk-37zy8)")
		}

		captureBus = bus

		// Subscribe a test observer for handler_paused events.  This consumer
		// receives any handler_paused event emitted by the policy goroutine
		// after budget_exhausted is injected below.
		sub := core.Subscription{
			ConsumerID:    "test-t3hp-handler-paused-observer-lz485",
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
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
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
	case <-time.After(10 * time.Second):
		t.Fatal("t3hp: daemon.StartForTesting did not return within 10s in no-op mode")
	}

	// Assertion 2 — behavioural wiring:
	// Inject a synthetic budget_exhausted event on the captured (sealed) bus.
	// HandlerPausePolicyGoroutine must handle it and emit handler_paused.
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
