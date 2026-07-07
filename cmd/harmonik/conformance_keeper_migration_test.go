package main

// conformance_keeper_migration_test.go — acceptance corpus registration (cmd-level).
//
// Named conformance set for the keeper test-validation system.  This file
// registers corpus item #6 (binary-upgrade migration) so that CI can run it
// under the shared TestKeeperConformance* prefix:
//
//   go test -run 'TestKeeperConformance' ./cmd/harmonik/
//
// Or as part of the combined sweep:
//
//   make test-keeper-conformance
//
// The owning test (TestKeeperBinaryUpgradeMigration_CorpusItem6) lives in
// resolve_keeper_required_test.go and carries the full assertions.  This
// wrapper adds no new logic; it exists solely to include corpus item #6 in
// the -run 'TestKeeperConformance' filter alongside the keeper package tests.
//
// Refs: plans/2026-07-06-quality-system/11-keeper-test-design.md §3 item #6,
// .kerf/works/keeper-test-harden/05-specs/keeper-fixes-spec.md §"Acceptance-corpus lock".

import "testing"

// TestKeeperConformanceMigration covers acceptance corpus item #6:
// binary-upgrade required-keys landmine — a new binary must refuse to start
// with a complete aggregated missing-key list, and `keeper config --example`
// must restore a clean start.
func TestKeeperConformanceMigration(t *testing.T) {
	t.Run("corpus/6/binary-upgrade-refuse-to-start",
		TestKeeperBinaryUpgradeMigration_CorpusItem6)
}
