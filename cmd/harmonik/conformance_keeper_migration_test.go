package main

import "testing"

// TestKeeperConformanceMigration covers acceptance corpus item #6:
// binary-upgrade required-keys landmine — a new binary must refuse to start
// with a complete aggregated missing-key list, and `keeper config --example`
// must restore a clean start.
func TestKeeperConformanceMigration(t *testing.T) {
	t.Run("corpus/6/binary-upgrade-refuse-to-start",
		TestKeeperBinaryUpgradeMigration_CorpusItem6)
}
