package daemon_test

// queue_operatoreventconsumer_composition_7urls_test.go — composition test
// confirming that QueueOperatorEventConsumer.Subscribe is called in the
// production composition root (daemon.Start), not only in isolated unit tests.
//
// Spec ref: specs/queue-model.md §8.5 QM-054.
// Bead ref: hk-7urls.

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// TestDaemonStart_QueueOperatorEventConsumerSubscribedInProductionComposition
// verifies that daemon.Start wires QueueOperatorEventConsumer.Subscribe(bus)
// before bus.Seal(), so the operator_pause_status and operator_resuming
// consumers are registered in the production event bus.
//
// The expected subscription count is 4:
//   - 2 from HandlerPausePolicyGoroutine (agent_rate_limit_status + budget_exhausted)
//   - 2 from QueueOperatorEventConsumer  (operator_pause_status + operator_resuming)
//
// Any deviation indicates a composition-root wiring regression.
//
// Spec ref: specs/queue-model.md §8.5 QM-054.
// Bead ref: hk-7urls.
func TestDaemonStart_QueueOperatorEventConsumerSubscribedInProductionComposition(t *testing.T) {
	t.Parallel()

	var capturedCount int
	var observed bool

	cfg := daemon.Config{
		// Unit-test mode: no ProjectDir, no BrPath, no JSONL log.
		// daemon.Start skips pidfile, orphan sweep, socket, and work loop.
		// The bus + subscription path still runs in full.
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

	// HandlerPausePolicyGoroutine registers 2 subscribers; QueueOperatorEventConsumer
	// registers 2 more for a total of 4.
	//
	//   1. agent_rate_limit_status — HandlerPausePolicyGoroutine rate-limit hysteresis
	//   2. budget_exhausted        — HandlerPausePolicyGoroutine budget-exhausted logic
	//   3. operator_pause_status   — QueueOperatorEventConsumer pause → paused-by-drain
	//   4. operator_resuming       — QueueOperatorEventConsumer resume → active
	const wantSubscriptions = 4
	if capturedCount != wantSubscriptions {
		t.Errorf("bus subscription count before Seal = %d, want %d; "+
			"QueueOperatorEventConsumer.Subscribe must be called pre-Seal in daemon.Start (hk-7urls)",
			capturedCount, wantSubscriptions)
	}
}
