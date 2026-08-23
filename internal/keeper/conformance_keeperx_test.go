package keeper_test

import "testing"

// TestKeeperConformanceCorpus covers the black-box acceptance corpus floor items
// plus corpus items #1, #3, #4, and #5.
func TestKeeperConformanceCorpus(t *testing.T) {
	t.Log("keeper acceptance corpus, black-box tier: 10 slots registered.")

	t.Run("floor/force-act-bypasses-crisp-idle", TestCycler_ForcedClear_BypassesCrispIdle)

	t.Run("floor/hard-ceiling-sid-independent",
		TestHardCeiling_FiresAbove280K_DespiteForeignSession)
	t.Run("floor/pct-inert-warn-1m", TestWatcher_LargeWindow_NoWarnBelowWarnPct)

	t.Run("corpus/1/resolve-tmux-b4", TestResolveTmuxTarget_CrewNaming_B4)
	t.Run("corpus/3/hkvpnp-no-truncate", TestActLoop_HKVPNP_DoesNotTruncateNonEmptyHandoffOnTimeout)
	t.Run("corpus/3/hkvpnp-no-refire", TestActLoop_HKVPNP_DoesNotRefireSecondNonceAfterTimeout)
	t.Run("corpus/4/b3-restall-fake-tmux", TestWatcher_B3_ReStall_FiresViaReResolvedTarget)

	t.Run("corpus/5/hold-dies-on-restart", TestHold_H2_AutoRevertAcrossRestart)
	t.Run("corpus/5/hard-ceiling-overrides-hold", TestWatcher_HardCeilingOverridesHold)
	t.Run("corpus/5/warn-fires-under-hold", TestWatcher_WarnFiresUnderHold)
}
