package daemon_test

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
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
		WorkflowModeDefault: core.WorkflowModeDot,
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

	const wantSubscriptions = 14
	if capturedCount != wantSubscriptions {
		t.Errorf("bus subscription count before Seal = %d, want %d; "+
			"HandlerPausePolicyGoroutine.Subscribe must be called pre-Seal in daemon.Start (hk-37zy8)",
			capturedCount, wantSubscriptions)
	}
}
