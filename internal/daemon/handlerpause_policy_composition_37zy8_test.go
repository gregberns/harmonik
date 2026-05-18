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
// The test uses Config.TestOnlyBusObserver to capture the bus subscription
// count immediately before Seal, without modifying the EventBus interface.
// The expected count is 2: one consumer per event type subscribed by the policy
// goroutine (agent_rate_limit_status + budget_exhausted) per hk-37zy8.
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
		TestOnlyBusObserver: func(bus eventbus.EventBus) {
			capturedCount = eventbus.BusSubscriptionCount(bus)
			observed = true
		},
	}

	if err := daemon.Start(context.Background(), cfg); err != nil {
		t.Fatalf("daemon.Start: unexpected error: %v", err)
	}

	if !observed {
		t.Fatal("TestOnlyBusObserver was never called; daemon.Start must invoke the observer pre-Seal")
	}

	// The policy goroutine registers exactly 2 asynchronous consumers:
	//   1. agent_rate_limit_status — rate-limit hysteresis logic
	//   2. budget_exhausted        — single-hit budget-exhaustion logic
	//
	// Any deviation (0 = not subscribed at all, 1 = partial, >2 = unexpected)
	// indicates a composition-root wiring regression.
	const wantSubscriptions = 2
	if capturedCount != wantSubscriptions {
		t.Errorf("bus subscription count before Seal = %d, want %d; "+
			"HandlerPausePolicyGoroutine.Subscribe must be called pre-Seal in daemon.Start (hk-37zy8)",
			capturedCount, wantSubscriptions)
	}
}
