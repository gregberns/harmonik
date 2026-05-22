package daemon_test

// spawn_path_sc6_hknx5wu_test.go — SC-6: daemon.Start composition-root wiring scan.
//
// # What this test covers
//
// SC-6 verifies that daemon.Start registers ALL expected pre-Seal subscriptions
// before bus.Seal() is called (EV-009). The WithBusObserver hook fires immediately
// after all pre-Seal Subscribe calls and before Seal, so the captured bus state
// is the authoritative pre-Seal snapshot.
//
// Four sub-tests:
//
//  1. pre-seal-without-notify-stream: count = 5
//     - 2 from HandlerPausePolicyGoroutine (agent_rate_limit_status, budget_exhausted)
//     - 2 from QueueOperatorEventConsumer  (operator_pause_status, operator_resuming)
//     - 1 from SubscribeHub (wildcard observer)
//
//  2. pre-seal-with-notify-stream: count = 9
//     - 5 base subscriptions (same as above)
//     - 4 from NotifyStreamConsumer (run_started, workspace_merge_status,
//       run_completed, run_failed)
//
//  3. registry-all-pre-seal-entries: typed-registry check (hk-ndysh)
//     - Enumerates RequiredPreSealSubscribers; verifies each ConsumerID is present
//       in the bus before Seal. Fails with the subsystem name, not a count.
//
//  4. registry-notify-stream-entries: typed-registry check for NotifyStream path
//     - Enumerates RequiredPreSealSubscribers + NotifyStreamSubscribers; same
//       named-failure semantics.
//
// Spec refs:
//   - specs/event-model.md §4 EV-009 (subscribers must register before Seal)
//   - specs/process-lifecycle.md §4.2 PL-005 (Start step ordering)
//
// Bead: hk-nx5wu, hk-ndysh.

import (
	"bytes"
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ─────────────────────────────────────────────────────────────────────────────
// TestSC6_CompositionRootWiringScan_AllPreSealSubscriptionsPresent
// ─────────────────────────────────────────────────────────────────────────────

// TestSC6_CompositionRootWiringScan_AllPreSealSubscriptionsPresent is SC-6 from
// the spawn-path scenario suite (hk-p3diy).
//
// It verifies that daemon.Start wires all expected event-bus subscribers before
// bus.Seal() is called, covering both the base case (no NotifyStream) and the
// extended case (NotifyStream enabled).
//
// Bead: hk-nx5wu.
func TestSC6_CompositionRootWiringScan_AllPreSealSubscriptionsPresent(t *testing.T) {
	t.Parallel()

	// ── Sub-test 1: base subscriptions without NotifyStream ──────────────────
	t.Run("pre-seal-without-notify-stream", func(t *testing.T) {
		t.Parallel()

		var capturedCount int
		var observed bool

		cfg := daemon.Config{
			// Unit-test mode: no ProjectDir, no BrPath, no JSONL log.
			// daemon.Start skips pidfile, orphan sweep, socket, and work loop.
			// The bus + subscription wiring path runs in full.
		}

		if err := daemon.StartForTesting(context.Background(), cfg,
			daemon.WithBusObserver(func(bus eventbus.EventBus) {
				capturedCount = eventbus.BusSubscriptionCount(bus)
				observed = true
			}),
		); err != nil {
			t.Fatalf("SC6/without-notify: daemon.StartForTesting: %v", err)
		}

		if !observed {
			t.Fatal("SC6/without-notify: WithBusObserver was never called; " +
				"startWithHooks must invoke the observer after pre-Seal subscriptions")
		}

		// 2 from HandlerPausePolicyGoroutine + 2 from QueueOperatorEventConsumer
		// + 1 from SubscribeHub (hk-6ynv4) = 5.
		const wantBase = 5
		if capturedCount != wantBase {
			t.Errorf("SC6/without-notify: pre-Seal subscription count = %d, want %d; "+
				"a subscribe call is missing or spurious in daemon.Start composition root (hk-nx5wu / EV-009)",
				capturedCount, wantBase)
		} else {
			t.Logf("SC6/without-notify PASS: pre-Seal subscription count = %d", capturedCount)
		}
	})

	// ── Sub-test 2: extended subscriptions with NotifyStream ─────────────────
	t.Run("pre-seal-with-notify-stream", func(t *testing.T) {
		t.Parallel()

		var capturedCount int
		var observed bool

		var notifyBuf bytes.Buffer
		cfg := daemon.Config{
			NotifyStream: &notifyBuf,
		}

		if err := daemon.StartForTesting(context.Background(), cfg,
			daemon.WithBusObserver(func(bus eventbus.EventBus) {
				capturedCount = eventbus.BusSubscriptionCount(bus)
				observed = true
			}),
		); err != nil {
			t.Fatalf("SC6/with-notify: daemon.StartForTesting: %v", err)
		}

		if !observed {
			t.Fatal("SC6/with-notify: WithBusObserver was never called; " +
				"startWithHooks must invoke the observer after pre-Seal subscriptions")
		}

		// 5 base + 4 from NotifyStreamConsumer (run_started, workspace_merge_status,
		// run_completed, run_failed) = 9. Base includes 1 SubscribeHub (hk-6ynv4).
		const wantWithNotify = 9
		if capturedCount != wantWithNotify {
			t.Errorf("SC6/with-notify: pre-Seal subscription count = %d, want %d; "+
				"NotifyStreamConsumer.Subscribe must add 4 subscriptions when NotifyStream is set (hk-nx5wu)",
				capturedCount, wantWithNotify)
		} else {
			t.Logf("SC6/with-notify PASS: pre-Seal subscription count = %d", capturedCount)
		}
	})

	// ── Sub-test 3: typed registry — base (no NotifyStream) ─────────────────
	t.Run("registry-all-pre-seal-entries", func(t *testing.T) {
		t.Parallel()

		var capturedIDs []string

		if err := daemon.StartForTesting(context.Background(), daemon.Config{},
			daemon.WithBusObserver(func(bus eventbus.EventBus) {
				capturedIDs = eventbus.BusSubscribedConsumerIDs(bus)
			}),
		); err != nil {
			t.Fatalf("SC6/registry: daemon.StartForTesting: %v", err)
		}

		sc6FixtureCheckRegistry(t, "SC6/registry", capturedIDs,
			daemon.RequiredPreSealSubscribers)
	})

	// ── Sub-test 4: typed registry — with NotifyStream ────────────────────────
	t.Run("registry-notify-stream-entries", func(t *testing.T) {
		t.Parallel()

		var capturedIDs []string
		var notifyBuf bytes.Buffer

		if err := daemon.StartForTesting(context.Background(), daemon.Config{NotifyStream: &notifyBuf},
			daemon.WithBusObserver(func(bus eventbus.EventBus) {
				capturedIDs = eventbus.BusSubscribedConsumerIDs(bus)
			}),
		); err != nil {
			t.Fatalf("SC6/registry-notify: daemon.StartForTesting: %v", err)
		}

		sc6FixtureCheckRegistry(t, "SC6/registry-notify", capturedIDs,
			daemon.RequiredPreSealSubscribers)
		sc6FixtureCheckRegistry(t, "SC6/registry-notify", capturedIDs,
			daemon.NotifyStreamSubscribers)
	})
}

// sc6FixtureCheckRegistry verifies that every ConsumerID declared in registry
// appears in capturedIDs. Failures name the subsystem, not just the count.
func sc6FixtureCheckRegistry(
	t *testing.T,
	label string,
	capturedIDs []string,
	registry map[daemon.PreSealSubsystem]daemon.SubscribeContract,
) {
	t.Helper()

	idSet := make(map[string]bool, len(capturedIDs))
	for _, id := range capturedIDs {
		idSet[id] = true
	}

	for subsystem, contract := range registry {
		for _, cid := range contract.ConsumerIDs {
			if !idSet[cid] {
				t.Errorf("%s: subsystem %q: ConsumerID %q not subscribed before Seal "+
					"(hk-ndysh / EV-009)", label, subsystem, cid)
			}
		}
	}
	t.Logf("%s PASS: %d pre-Seal subscriptions verified against registry (%d subsystems)",
		label, len(capturedIDs), len(registry))
}
