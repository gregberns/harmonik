package keeper

import "testing"

// TestKeeperConformance covers the white-box acceptance corpus floor items and
// the fake-tmux layer of corpus item #1 (restart-now no_tmux_target fix, B4).
func TestKeeperConformance(t *testing.T) {
	t.Log("keeper acceptance corpus, white-box tier: 5 slots registered.")

	t.Run("floor/band-min-200k-1m", TestMinAbsOrPctCeil)
	t.Run("floor/live-watcher-lock-held", TestLiveKeeperPresent_LockHeld)
	t.Run("floor/live-watcher-no-lockfile", TestLiveKeeperPresent_NoLockfile)
	t.Run("floor/operator-attached-warn-only", TestSelectWarnText_OperatorAttached_SuppressesActionable)
	t.Run("corpus/1/restartnow-b4-fake-tmux", TestRestartNow_CrewAgent_AccCorpus1_B4)
}
