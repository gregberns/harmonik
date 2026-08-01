package keeper

// conformance_keeper_test.go — acceptance corpus registration (white-box tier).
//
// Named conformance set for the keeper test-validation system.  This file
// registers the white-box (package keeper) tier of the acceptance corpus so
// that one command runs every slot the tier owns:
//
//   go test -run 'TestKeeperConformance' ./internal/keeper/
//
// Each t.Run slot delegates to an existing test that owns the scenario.  The
// subtest name is the canonical corpus slot, and the design doc names
// scenarios by those slots, so the names are the corpus's public identifiers.
// No new harness — the referenced tests carry all the assertions.
//
// Registered slots (white-box tier):
//   floor/band-min-200k-1m           → TestMinAbsOrPctCeil
//   floor/live-watcher-lock-held     → TestLiveKeeperPresent_LockHeld
//   floor/live-watcher-no-lockfile   → TestLiveKeeperPresent_NoLockfile
//   corpus/1/restartnow-b4-fake-tmux → TestRestartNow_CrewAgent_AccCorpus1_B4
//
// ─── GAP.  One slot in this tier has no test left to register. ──────────────
//
//   floor/operator-attached-warn-only
//       Owned by TestSelectWarnText_OperatorAttached_SuppressesActionable.
//       Deleted by ec66da798.  The scenario is unguarded today: nothing proves
//       that an operator attached to the pane suppresses the actionable WARN
//       text.
//
// The slot is absent on purpose.  It is NOT registered as a t.Skip.  A skip
// passes, and a passing slot that asserts nothing is the exact failure this
// file was restored to end.  Read the gap as real lost coverage, not as a
// formatting choice.  Register the slot again when a test owns it.
//
// ─── History.  Why this file went missing. ─────────────────────────────────
//
// ec66da798 deleted 681 signature-pinning test files.  It took this file and
// conformance_keeperx_test.go with them.  Neither one pinned a signature.  Both
// were registration shells, so the delete removed the corpus's named entry
// point and left the tests themselves in place.  `go test` exits 0 when a -run
// filter matches nothing, and cmd/harmonik was also on the target's command
// line and still matched, so `make test-keeper-conformance` kept reporting
// green while running zero keeper tests.  scripts/go-test-must-match.sh now
// fails a target in that state.
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
	t.Log("keeper acceptance corpus, white-box tier: 4 slots registered, 1 GAP.")
	t.Log("GAP floor/operator-attached-warn-only — TestSelectWarnText_OperatorAttached_SuppressesActionable, deleted by ec66da798.")

	t.Run("floor/band-min-200k-1m", TestMinAbsOrPctCeil)
	t.Run("floor/live-watcher-lock-held", TestLiveKeeperPresent_LockHeld)
	t.Run("floor/live-watcher-no-lockfile", TestLiveKeeperPresent_NoLockfile)
	t.Run("corpus/1/restartnow-b4-fake-tmux", TestRestartNow_CrewAgent_AccCorpus1_B4)
}
