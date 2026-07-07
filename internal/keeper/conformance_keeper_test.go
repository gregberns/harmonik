package keeper

// conformance_keeper_test.go — acceptance corpus registration (white-box tier).
//
// Named conformance set for the keeper test-validation system.  This file
// registers the white-box (package keeper) tier of the acceptance corpus so
// that CI can run all floor scenarios and corpus item #1 with one command:
//
//   go test -run 'TestKeeperConformance' ./internal/keeper/
//
// Each TestKeeperConformance* function delegates to an existing test that owns
// the scenario.  The subtest name is the canonical corpus slot.  No new
// harness — the referenced tests carry all assertions.
//
// Acceptance corpus (full map):
//   floor/band-min-200k-1m            → TestMinAbsOrPctCeil
//   floor/live-watcher-flock-vs-corpse → TestLiveKeeperPresent_LockHeld / _NoLockfile
//   floor/operator-attached-warn-only  → TestSelectWarnText_OperatorAttached_SuppressesActionable
//   corpus/1/restartnow-b4-fake-tmux   → TestRestartNow_CrewAgent_AccCorpus1_B4
//
// Corpus items in package keeper_test:  see conformance_keeperx_test.go.
// Integration items (real tmux):        see conformance_keeper_integration_test.go.
// Binary-upgrade migration (cmd-level): see cmd/harmonik/conformance_keeper_migration_test.go.
//
// Refs: plans/2026-07-06-quality-system/11-keeper-test-design.md §3,
// .kerf/works/keeper-test-harden/05-specs/keeper-fixes-spec.md.

import "testing"

// TestKeeperConformance covers the white-box acceptance corpus floor items and
// the fake-tmux layer of corpus item #1 (restart-now no_tmux_target fix, B4).
func TestKeeperConformance(t *testing.T) {
	t.Run("floor/band-min-200k-1m", TestMinAbsOrPctCeil)
	t.Run("floor/live-watcher-lock-held", TestLiveKeeperPresent_LockHeld)
	t.Run("floor/live-watcher-no-lockfile", TestLiveKeeperPresent_NoLockfile)
	t.Run("floor/operator-attached-warn-only", TestSelectWarnText_OperatorAttached_SuppressesActionable)
	t.Run("corpus/1/restartnow-b4-fake-tmux", TestRestartNow_CrewAgent_AccCorpus1_B4)
}
