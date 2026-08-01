package keeper_test

// conformance_keeperx_test.go — acceptance corpus registration (black-box tier).
//
// Named conformance set for the keeper test-validation system.  This file
// registers the black-box (package keeper_test) tier of the acceptance corpus:
// the remaining floor scenarios plus corpus items #1 (resolve seam) and #5.
//
// Run the full non-integration conformance set:
//
//   go test -run 'TestKeeperConformance' ./internal/keeper/
//
// Each t.Run slot delegates to the owning test.  The subtest name is the
// canonical corpus slot, and the design doc names scenarios by those slots, so
// the names are the corpus's public identifiers.  No new harness.
//
// The tier split is real, not cosmetic.  Go allows one external test package
// per directory, so a white-box test in package keeper and a black-box test in
// package keeper_test cannot be registered from one file.  That is why there
// are two registration files and why this one carries the x suffix.
//
// Registered slots (black-box tier):
//   floor/force-act-bypasses-crisp-idle  → TestCycler_ForcedClear_BypassesCrispIdle
//   floor/hard-ceiling-sid-independent   → TestHardCeiling_FiresAbove280K_DespiteForeignSession
//   corpus/1/resolve-tmux-b4             → TestResolveTmuxTarget_CrewNaming_B4
//   corpus/5/hold-dies-on-restart        → TestHold_H2_AutoRevertAcrossRestart
//   corpus/5/hard-ceiling-overrides-hold → TestWatcher_HardCeilingOverridesHold
//   corpus/5/warn-fires-under-hold       → TestWatcher_WarnFiresUnderHold
//
// ─── GAPS.  Four slots in this tier have no test left to register. ─────────
//
//   floor/pct-inert-warn-1m
//       Owned by TestWatcher_LargeWindow_NoWarnBelowWarnPct.  Deleted by
//       ec66da798.  Unguarded: nothing proves the 200k absolute warn gate stays
//       inert on a 1M-context window until warn_pct is reached.
//
//   corpus/3/hkvpnp-no-truncate
//       Owned by TestActLoop_HKVPNP_DoesNotTruncateNonEmptyHandoffOnTimeout.
//       Deleted by ec66da798.  Unguarded: nothing proves a non-empty handoff
//       survives a failed nonce confirmation instead of being wiped to 0 lines.
//
//   corpus/3/hkvpnp-no-refire
//       Owned by TestActLoop_HKVPNP_DoesNotRefireSecondNonceAfterTimeout.
//       Deleted by ec66da798.  Unguarded: nothing proves the cycle stops after
//       a handoff timeout instead of firing a second nonce at the same session.
//
//   corpus/4/b3-restall-fake-tmux
//       Owned by TestWatcher_B3_ReStall_FiresViaReResolvedTarget.  Deleted by
//       ec66da798.  Corpus item #4 keeps its real-tmux leg in
//       conformance_keeper_integration_test.go, so the scenario is not fully
//       dark.  Its fake-tmux leg is, and that leg is the one that runs without
//       tmux on PATH.
//
// Corpus item #3 now has NO leg at any tier.  It is the largest hole in the
// corpus.
//
// None of the four is registered as a t.Skip.  A skip passes, and a passing
// slot that asserts nothing is the exact failure this file was restored to end.
// Read the gaps as real lost coverage.  Register each slot again when a test
// owns it.
//
// White-box floor items (package keeper): see conformance_keeper_test.go, which
// carries a fifth gap of its own and the history of why both files went
// missing.
// Integration items (real tmux):          see conformance_keeper_integration_test.go.
// Binary-upgrade migration (cmd-level):   see cmd/harmonik/conformance_keeper_migration_test.go.
//
// Refs: plans/2026-07-06-quality-system/11-keeper-test-design.md §3,
// .kerf/works/keeper-test-harden/05-specs/keeper-fixes-spec.md.

import "testing"

// TestKeeperConformanceCorpus covers the black-box acceptance corpus floor items
// plus corpus items #1 (resolve seam) and #5 (hold invariants).  Corpus item #3
// and the fake-tmux leg of item #4 have no test to register — see the gap list
// in the file header.
func TestKeeperConformanceCorpus(t *testing.T) {
	t.Log("keeper acceptance corpus, black-box tier: 6 slots registered, 4 GAPS.")
	t.Log("GAP floor/pct-inert-warn-1m — TestWatcher_LargeWindow_NoWarnBelowWarnPct, deleted by ec66da798.")
	t.Log("GAP corpus/3/hkvpnp-no-truncate — TestActLoop_HKVPNP_DoesNotTruncateNonEmptyHandoffOnTimeout, deleted by ec66da798.")
	t.Log("GAP corpus/3/hkvpnp-no-refire — TestActLoop_HKVPNP_DoesNotRefireSecondNonceAfterTimeout, deleted by ec66da798.")
	t.Log("GAP corpus/4/b3-restall-fake-tmux — TestWatcher_B3_ReStall_FiresViaReResolvedTarget, deleted by ec66da798. The real-tmux leg survives under -tags=integration.")

	// ── Supporting floor (keep green) ────────────────────────────────────────

	// force-act (240k abs / 95 pct) fires even when CrispIdle=false (the agent
	// is not between turns).  Below this threshold, CrispIdle still gates.
	t.Run("floor/force-act-bypasses-crisp-idle", TestCycler_ForcedClear_BypassesCrispIdle)

	// hard-ceiling (280k) fires independently of which session ID wrote the gauge
	// (SID-independent failsafe).
	t.Run("floor/hard-ceiling-sid-independent",
		TestHardCeiling_FiresAbove280K_DespiteForeignSession)

	// ── Corpus item #1 — restart-now does NOT abort no_tmux_target (B4) ─────
	// Resolution seam: ResolveTmuxTarget returns the correct pane for a
	// crew-named session ("harmonik-<hash>-crew-<name>:agent").
	t.Run("corpus/1/resolve-tmux-b4", TestResolveTmuxTarget_CrewNaming_B4)

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
