package daemon_test

// spawn_path_sc6_hknx5wu_test.go — SC-6: daemon.Start composition-root wiring scan.
//
// # What this test covers
//
// SC-6 verifies that daemon.Start registers ALL expected pre-Seal subscriptions
// before bus.Seal() is called (EV-009). The WithBusObserver hook fires immediately
// after all pre-Seal Subscribe calls and before Seal, so the BusSubscriptionCount
// captured there is the authoritative pre-Seal count.
//
// Three sub-tests:
//
//  1. pre-seal-without-notify-stream: count = 4
//     - 2 from HandlerPausePolicyGoroutine (agent_rate_limit_status, budget_exhausted)
//     - 2 from QueueOperatorEventConsumer  (operator_pause_status, operator_resuming)
//
//  2. pre-seal-with-notify-stream: count = 8
//     - 4 base subscriptions (same as above)
//     - 4 from NotifyStreamConsumer (run_started, workspace_merge_status,
//       run_completed, run_failed)
//
//  3. wiring-table-pre-seal-entries: structural scan
//     - The exported compositionRootWirings table MUST contain at least two
//       ".Subscribe" symbols before the "bus.Seal" symbol.
//     - This guards against silent drops from the canonical wiring map
//       (docs/audits/2026-05-20/composition-root-wiring-map.md).
//
// Spec refs:
//   - specs/event-model.md §4 EV-009 (subscribers must register before Seal)
//   - specs/process-lifecycle.md §4.2 PL-005 (Start step ordering)
//
// Bead: hk-nx5wu.

import (
	"bytes"
	"context"
	"strings"
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

		// 2 from HandlerPausePolicyGoroutine + 2 from QueueOperatorEventConsumer = 4.
		const wantBase = 4
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

		// 4 base + 4 from NotifyStreamConsumer (run_started, workspace_merge_status,
		// run_completed, run_failed) = 8.
		const wantWithNotify = 8
		if capturedCount != wantWithNotify {
			t.Errorf("SC6/with-notify: pre-Seal subscription count = %d, want %d; "+
				"NotifyStreamConsumer.Subscribe must add 4 subscriptions when NotifyStream is set (hk-nx5wu)",
				capturedCount, wantWithNotify)
		} else {
			t.Logf("SC6/with-notify PASS: pre-Seal subscription count = %d", capturedCount)
		}
	})

	// ── Sub-test 3: wiring-table structural scan ──────────────────────────────
	t.Run("wiring-table-pre-seal-entries", func(t *testing.T) {
		t.Parallel()

		entries := daemon.ExportedCompositionRootWirings()
		if len(entries) == 0 {
			t.Fatal("SC6/wiring-table: compositionRootWirings is empty; expected ≥29 entries")
		}

		// Find the index of the bus.Seal entry.
		sealIdx := -1
		for i, e := range entries {
			if e.Symbol == "bus.Seal" {
				sealIdx = i
				break
			}
		}
		if sealIdx < 0 {
			t.Fatal("SC6/wiring-table: 'bus.Seal' entry not found in compositionRootWirings; " +
				"the wiring map must contain the Seal call site (hk-nx5wu)")
		}

		// Count and name all pre-Seal .Subscribe entries.
		var preSealSubscribes []string
		for i := 0; i < sealIdx; i++ {
			if strings.HasSuffix(entries[i].Symbol, ".Subscribe") {
				preSealSubscribes = append(preSealSubscribes, entries[i].Symbol)
			}
		}

		// Mandatory pre-Seal subscriptions: pausePolicy.Subscribe + queueOpConsumer.Subscribe.
		const wantMinPreSealSubscribes = 2
		if len(preSealSubscribes) < wantMinPreSealSubscribes {
			t.Errorf("SC6/wiring-table: found %d pre-Seal .Subscribe entries before bus.Seal (idx=%d), want ≥%d; "+
				"entries found: %v (hk-nx5wu / EV-009)",
				len(preSealSubscribes), sealIdx, wantMinPreSealSubscribes, preSealSubscribes)
		} else {
			t.Logf("SC6/wiring-table PASS: %d pre-Seal .Subscribe entries: %v", len(preSealSubscribes), preSealSubscribes)
		}

		// Verify specific mandatory entries are present.
		sc6FixtureAssertPreSealEntry(t, preSealSubscribes, "pausePolicy.Subscribe")
		sc6FixtureAssertPreSealEntry(t, preSealSubscribes, "queueOpConsumer.Subscribe")

		t.Logf("SC6/wiring-table PASS: wiring table has %d entries; bus.Seal at index %d; "+
			"%d pre-Seal .Subscribe entries confirmed", len(entries), sealIdx, len(preSealSubscribes))
	})
}

// sc6FixtureAssertPreSealEntry checks that want appears in the pre-Seal subscribe list.
func sc6FixtureAssertPreSealEntry(t *testing.T, preSealSubscribes []string, want string) {
	t.Helper()
	for _, s := range preSealSubscribes {
		if s == want {
			return
		}
	}
	t.Errorf("SC6/wiring-table: mandatory pre-Seal entry %q not found in compositionRootWirings before bus.Seal; "+
		"found: %v (hk-nx5wu / EV-009)", want, preSealSubscribes)
}
