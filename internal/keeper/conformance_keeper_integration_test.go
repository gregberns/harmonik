//go:build integration

package keeper_test

import "testing"

// TestKeeperConformanceCorpus_Integration covers the L-twin tier of the acceptance
// corpus: the scenarios that require a real tmux session.
func TestKeeperConformanceCorpus_Integration(t *testing.T) {
	t.Run("corpus/2/sid-rebind-anti-loop",
		TestIntegration_TwinSidRebind_AntiLoopGateHolds)

	t.Run("corpus/4/b3-restall-twin-loop",
		TestIntegration_B3_ReStall_AutoHealsNoLoop)
}
