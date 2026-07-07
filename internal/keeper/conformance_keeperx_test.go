package keeper_test

// conformance_keeperx_test.go — acceptance corpus registration (black-box tier).
//
// Named conformance set for the keeper test-validation system.  This file
// registers the black-box (package keeper_test) tier of the acceptance corpus:
// the remaining floor scenarios plus corpus items #1 (resolve seam), #3, #4
// (fake-tmux), and #5.
//
// Run the full non-integration conformance set:
//
//   go test -run 'TestKeeperConformance' ./internal/keeper/
//
// Each t.Run slot delegates to the owning test.  No new harness.
//
// Acceptance corpus (full map):
//   floor/force-act-bypasses-crisp-idle → TestCycler_ForcedClear_BypassesCrispIdle
//   floor/hard-ceiling-sid-independent  → TestHardCeiling_FiresAbove280K_DespiteForeignSession
//   floor/pct-inert-warn-1m             → TestWatcher_LargeWindow_NoWarnBelowWarnPct
//   corpus/1/resolve-tmux-b4            → TestResolveTmuxTarget_CrewNaming_B4
//   corpus/3/hkvpnp-no-truncate         → TestActLoop_HKVPNP_DoesNotTruncateNonEmptyHandoffOnTimeout
//   corpus/3/hkvpnp-no-refire           → TestActLoop_HKVPNP_DoesNotRefireSecondNonceAfterTimeout
//   corpus/4/b3-restall-fake-tmux       → TestWatcher_B3_ReStall_FiresViaReResolvedTarget
//   corpus/5/hold-dies-on-restart       → TestHold_H2_AutoRevertAcrossRestart
//   corpus/5/hard-ceiling-overrides-hold→ TestWatcher_HardCeilingOverridesHold
//   corpus/5/warn-fires-under-hold      → TestWatcher_WarnFiresUnderHold
//
// White-box floor items (package keeper): see conformance_keeper_test.go.
// Integration items (real tmux):          see conformance_keeper_integration_test.go.
// Binary-upgrade migration (cmd-level):   see cmd/harmonik/conformance_keeper_migration_test.go.
//
// Refs: plans/2026-07-06-quality-system/11-keeper-test-design.md §3,
// .kerf/works/keeper-test-harden/05-specs/keeper-fixes-spec.md.

import "testing"

// TestKeeperConformanceCorpus covers the black-box acceptance corpus floor items
// plus corpus items #1 (resolve seam), #3 (hk-vpnp), #4 (fake-tmux gate), and
// #5 (hold invariants).
func TestKeeperConformanceCorpus(t *testing.T) {
	// ── Supporting floor (keep green) ────────────────────────────────────────

	// force-act (240k abs / 95 pct) fires even when CrispIdle=false (the agent
	// is not between turns).  Below this threshold, CrispIdle still gates.
	t.Run("floor/force-act-bypasses-crisp-idle", TestCycler_ForcedClear_BypassesCrispIdle)

	// hard-ceiling (280k) fires independently of which session ID wrote the gauge
	// (SID-independent failsafe).
	t.Run("floor/hard-ceiling-sid-independent",
		TestHardCeiling_FiresAbove280K_DespiteForeignSession)

	// on a 1M-context window (e.g. [1m] models) the absolute-token warn gate
	// (200k) must not fire below warn_pct=80%; the pct gate wins.
	t.Run("floor/pct-inert-warn-1m", TestWatcher_LargeWindow_NoWarnBelowWarnPct)

	// ── Corpus item #1 — restart-now does NOT abort no_tmux_target (B4) ─────
	// Resolution seam: ResolveTmuxTarget returns the correct pane for a
	// crew-named session ("harmonik-<hash>-crew-<name>:agent").
	t.Run("corpus/1/resolve-tmux-b4", TestResolveTmuxTarget_CrewNaming_B4)

	// ── Corpus item #3 — hk-vpnp no-truncate, no-loop ────────────────────────
	// A non-empty handoff that fails nonce-confirmation is NOT wiped to 0 lines.
	t.Run("corpus/3/hkvpnp-no-truncate",
		TestActLoop_HKVPNP_DoesNotTruncateNonEmptyHandoffOnTimeout)
	// After a handoff timeout the cycle does NOT re-fire a second nonce on the
	// same un-cleared session.
	t.Run("corpus/3/hkvpnp-no-refire",
		TestActLoop_HKVPNP_DoesNotRefireSecondNonceAfterTimeout)

	// ── Corpus item #4 — watch re-stall auto-heals (B3, fake-tmux gate) ─────
	// Stale gauge over an alive pane with a mangled target → ForceRestart fires
	// once via the re-resolved target; cooldown prevents a loop.
	t.Run("corpus/4/b3-restall-fake-tmux", TestWatcher_B3_ReStall_FiresViaReResolvedTarget)

	// ── Corpus item #5 — hold invariants ─────────────────────────────────────
	// A .hold.<sessionID> marker from session A is unreachable as soon as the
	// .sid flips to session B (which /clear causes).
	t.Run("corpus/5/hold-dies-on-restart", TestHold_H2_AutoRevertAcrossRestart)
	// A held session at ≥280k is force-restarted anyway; the hold cannot block
	// the hard-ceiling failsafe.
	t.Run("corpus/5/hard-ceiling-overrides-hold", TestWatcher_HardCeilingOverridesHold)
	// WARN still fires even while a hold is active (the hold suppresses ACT, not
	// the warning injection).
	t.Run("corpus/5/warn-fires-under-hold", TestWatcher_WarnFiresUnderHold)
}
