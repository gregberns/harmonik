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

	"github.com/gregberns/harmonik/internal/core"
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
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
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

	// Pre-Seal subscription inventory (composition root audit):
	//   1. agent_rate_limit_status — HandlerPausePolicyGoroutine rate-limit hysteresis (hk-37zy8)
	//   2. budget_exhausted        — HandlerPausePolicyGoroutine budget-exhausted logic (hk-37zy8)
	//   3. run_started             — DaemonSpendMeter run counter (hk-k3f8g)
	//   4. budget_accrual          — DaemonSpendMeter byte-proxy spend (hk-k3f8g)
	//   5. operator_pause_status   — QueueOperatorEventConsumer pause → paused-by-drain (hk-7urls)
	//   6. operator_resuming       — QueueOperatorEventConsumer resume → active (hk-7urls)
	//   7. * (wildcard)            — SubscribeHub fans events to socket subscribers (hk-6ynv4)
	//   8. * (wildcard)            — StaleWatcher per-run silence monitor (hk-wkzlc)
	//   9. agent_rate_limit_status  — bandwidthTunerBackstop emergency backstop
	//  10. bead_closed             — ReviewGateAnomalyWatcher consecutive-close alarm (hk-tnmjy)
	//  11. reviewer_verdict        — ReviewGateAnomalyWatcher verdict reset (hk-tnmjy)
	//
	// Any deviation indicates a composition-root wiring regression.
	// Update history: 9→11 (ReviewGateAnomalyWatcher hk-tnmjy registers two
	// subscriptions — bead_closed + reviewer_verdict).
	const wantSubscriptions = 11
	if capturedCount != wantSubscriptions {
		t.Errorf("bus subscription count before Seal = %d, want %d; "+
			"QueueOperatorEventConsumer.Subscribe must be called pre-Seal in daemon.Start (hk-7urls)",
			capturedCount, wantSubscriptions)
	}
}
