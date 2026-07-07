//go:build integration

package keeper_test

// conformance_keeper_integration_test.go — acceptance corpus registration (L-twin tier).
//
// Named conformance set for the keeper test-validation system.  This file
// registers the integration-tagged (real tmux) tier of the acceptance corpus:
// corpus items #1 (full restart-now drive), #2 (sid-rebind twin), and #4
// (B3 watch re-stall twin loop).
//
// Run the full integration conformance set (requires real tmux on PATH):
//
//   go test -race -tags=integration -run 'TestKeeperConformanceCorpus_Integration' ./internal/keeper/
//
// Or the combined unit + integration sweep:
//
//   make test-keeper-conformance-full
//
// Each t.Run slot delegates to the owning L-twin test.  No new harness.
//
// Acceptance corpus (integration tier):
//   corpus/1/restartnow-b4-integration → TestSmoke_RestartNow_Integration (CrewNamingB4 subtest)
//   corpus/2/sid-rebind-anti-loop      → TestIntegration_TwinSidRebind_AntiLoopGateHolds
//   corpus/4/b3-restall-twin-loop      → TestIntegration_B3_ReStall_AutoHealsNoLoop
//
// Non-integration corpus items: see conformance_keeper_test.go and
// conformance_keeperx_test.go.  Binary-upgrade migration: see
// cmd/harmonik/conformance_keeper_migration_test.go.
//
// Refs: plans/2026-07-06-quality-system/11-keeper-test-design.md §3 (L-twin rows).

import "testing"

// TestKeeperConformanceCorpus_Integration covers the L-twin tier of the acceptance
// corpus: the three scenarios that require a real tmux session.
func TestKeeperConformanceCorpus_Integration(t *testing.T) {
	// Corpus item #1 (B4 / hk-pp1in): restart-now resolves the crew-named pane
	// and drives the full ACK→/clear→resume sequence without aborting.
	// The CrewNamingB4 subtest inside this function is the primary B4 assertion.
	t.Run("corpus/1/restartnow-b4-integration", TestSmoke_RestartNow_Integration)

	// Corpus item #2: session_id survives a /clear cycle; the resume rebinds
	// the SAME conversation; the anti-loop gate (lastFiredSID) does not
	// immediately re-fire.
	t.Run("corpus/2/sid-rebind-anti-loop",
		TestIntegration_TwinSidRebind_AntiLoopGateHolds)

	// Corpus item #4 (B3 / hk-9cqtm): the watch session self-heals after a
	// re-stall — ForceRestart fires exactly once, the pane comes back, no
	// escalating alert storm.
	t.Run("corpus/4/b3-restall-twin-loop",
		TestIntegration_B3_ReStall_AutoHealsNoLoop)
}
