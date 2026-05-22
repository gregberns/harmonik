package daemon_test

// handlerpause_policy_composition_37zy8_test.go — integration test confirming
// that HandlerPausePolicyGoroutine.Subscribe is called in the production
// composition root (daemon.Start), not only in isolated unit tests.
//
// Bead ref: hk-37zy8.

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// TestDaemonStart_HandlerPausePolicySubscribedInProductionComposition verifies
// that daemon.Start wires HandlerPausePolicyGoroutine.Subscribe(bus) before
// bus.Seal(), so the two policy consumers (agent_rate_limit_status and
// budget_exhausted) are registered in the production event-bus.
//
// The test uses daemon.WithBusObserver (via StartForTesting) to capture the bus
// subscription count immediately before Seal, without modifying the EventBus interface.
// The expected count is 4: 2 from HandlerPausePolicyGoroutine (agent_rate_limit_status
// + budget_exhausted per hk-37zy8) + 2 from QueueOperatorEventConsumer
// (operator_pause_status + operator_resuming per hk-7urls).
//
// Spec ref: docs/components/internal/handler-pause-and-resume.md §4 event flow.
// Bead ref: hk-37zy8.
func TestDaemonStart_HandlerPausePolicySubscribedInProductionComposition(t *testing.T) {
	t.Parallel()

	var capturedCount int
	var observed bool

	cfg := daemon.Config{
		// Unit-test mode: no ProjectDir, no BrPath, no JSONL log.
		// daemon.Start skips pidfile, orphan sweep, socket, and work loop.
		// The bus + policy subscription path still runs in full.
	}

	if err := daemon.StartForTesting(context.Background(), cfg,
		daemon.WithBusObserver(func(bus eventbus.EventBus) {
			capturedCount = eventbus.BusSubscriptionCount(bus)
			observed = true
		}),
	); err != nil {
		t.Fatalf("daemon.StartForTesting: unexpected error: %v", err)
	}

	if !observed {
		t.Fatal("WithBusObserver was never called; daemon.startWithHooks must invoke the observer pre-Seal")
	}

	// The composition root registers exactly 5 consumers pre-Seal:
	//   1. agent_rate_limit_status — HandlerPausePolicyGoroutine rate-limit hysteresis (hk-37zy8)
	//   2. budget_exhausted        — HandlerPausePolicyGoroutine budget-exhausted logic (hk-37zy8)
	//   3. operator_pause_status   — QueueOperatorEventConsumer pause → paused-by-drain (hk-7urls)
	//   4. operator_resuming       — QueueOperatorEventConsumer resume → active (hk-7urls)
	//   5. * (wildcard)            — SubscribeHub fans events to socket 'subscribe' op (hk-6ynv4)
	//
	// Any deviation indicates a composition-root wiring regression.
	// Updated 4 → 5 when SubscribeHub.Subscribe was wired (hk-6ynv4).
	const wantSubscriptions = 5
	if capturedCount != wantSubscriptions {
		t.Errorf("bus subscription count before Seal = %d, want %d; "+
			"HandlerPausePolicyGoroutine.Subscribe must be called pre-Seal in daemon.Start (hk-37zy8)",
			capturedCount, wantSubscriptions)
	}
}
