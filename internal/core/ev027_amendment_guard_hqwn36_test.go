package core

import "testing"

// ev027_amendment_guard_hqwn36_test.go — foundation-amendment gate for EV-027.
//
// EV-027 (event-model.md §4.6 EV-027) states: "A subsystem that wants to emit
// a new cross-bus event type MUST add the type to §8 via the foundation
// amendment protocol ([architecture.md §4.6])." Symmetrically, removing a
// cross-bus event type also requires an amendment.
//
// This test pins the number of EventType constants in the allEventTypeCohort
// table. Any addition or removal of a cross-bus EventType constant causes this
// test to fail, which forces the developer to:
//   1. Update the wantCount constant below to the new count.
//   2. Acknowledge in the commit message that the change constitutes a
//      foundation amendment per EV-027 and [architecture.md §4.6].
//
// Spec ref: event-model.md §4.6 EV-027.
// Bead ref: hk-hqwn.36.

// TestEV027_CrossBusEventTypeTaxonomyCount guards the size of the §8 EventType
// taxonomy per EV-027. Changing wantCount requires a foundation amendment per
// event-model.md §4.6 EV-027 and architecture.md §4.6.
//
// Current taxonomy breakdown (116 types total):
//
//	§8.1  Run lifecycle (17 types including bead_closed, working_tree_refresh_failed,
//	      implementer_escaped_worktree, implementer_phase_complete,
//	      node_dispatch_decided [hk-bf85t T-IMPL-008], merge_build_failed [hk-o68j3])
//	§8.1a Review-loop cycle (7 types including review_bypassed [hk-81n9r])
//	§8.2  Control-point lifecycle (12 types)
//	§8.2a Gate-node dispatch (1 type: gate_decision_recorded [hk-jtxnr T-IMPL-010])
//	§8.3  Agent/handler lifecycle (15 types including launch_initiated)
//	§8.4  Budget lifecycle (3 types)
//	§8.5  Workspace lifecycle (6 types)
//	§8.6  Reconciliation lifecycle (15 types including reconciliation_mismatch_observed)
//	§8.7  Operator-control and daemon lifecycle (18 types including daemon_config [hk-sul12])
//	§8.8  Observability and bus-internal (6 types)
//	§8.10 Queue lifecycle (7 types)
//	§8.11 Handler-pause lifecycle (3 types)
//	§8.12 Staleness-detection (1 type: run_stale)
//	§8.15 Bead-ledger merge lifecycle (5 types: bead_sync_failed [hk-u3q6o],
//	      bead_ledger_recovered, bead_ledger_corrupt [hk-k7va9],
//	      bead_ledger_conflict_audit [hk-u3q6o], orphaned_child_bead [hk-27ghc])
//
// Total: 121 EventType constants registered in allEventTypeCohort.
// Amendment: merge_build_failed added for post-merge build gate (hk-o68j3;
// EV-027 foundation amendment — new F-class event emitted when go build+vet
// fails on the freshly fast-forwarded merged tree before push).
// Amendment: review_bypassed added (hk-81n9r; O-class audit event emitted when
// workflow:single label gates single mode, bypassing review-loop).
// Amendment: daemon_config added (hk-sul12; O-class startup event stating the
// resolved merge target and active branch-protection policy).
// Amendment: implementer_budget_exceeded added (hk-9vp51; O-class diagnostic
// emitted when pasteInjectQuitOnCommit force-kills an implementer session for
// exhausting its commit budget — makes a previously-silent no_commit explain
// elapsed time and last-progress time).
// Amendment: §8.15 bead-ledger merge lifecycle added (hk-u3q6o, hk-27ghc,
// hk-k7va9; 5 types): bead_sync_failed (F-class, fsync-boundary — loss silences
// Cat-BL2 routing), bead_ledger_recovered (O-class, Cat-BL2 retry success),
// bead_ledger_corrupt (O-class, Cat-BL2 persistent failure + Cat 6b escalation),
// bead_ledger_conflict_audit (O-class, Cat-BL3 merge-conflict audit batch),
// orphaned_child_bead (O-class, Cat-BL1 child-bead orphan detection).
//
// To add an EventType: update allEventTypeCohort in eventtype_coverage_gjyks_test.go,
// add the constant to eventtype.go, register the constructor in eventreg_hqwn59.go
// (or the appropriate eventreg_*.go file), and increment wantCount here. Include a
// commit message citing the EV-027 foundation amendment per architecture.md §4.6.
//
// To remove an EventType: decrement wantCount, remove from allEventTypeCohort,
// remove the constant (or mark it retired per EV-027 burn rule), and include the
// required deletion amendment fields in the commit: retiring name, emitter-spec
// edit, migration guidance for consumers, and confirmation the identifier is burned.
func TestEV027_CrossBusEventTypeTaxonomyCount(t *testing.T) {
	t.Parallel()

	// wantCount is the number of entries in allEventTypeCohort (event-model.md §8
	// cross-bus taxonomy). Changing this value requires a foundation amendment per
	// EV-027 and architecture.md §4.6.
	const wantCount = 122 // +5 for §8.15 bead-ledger types (hk-u3q6o/hk-27ghc/hk-k7va9 EV-027 amendment); +1 agent_ready_stall_detected (hk-1s1or launch_initiated→agent_ready stall detector)

	got := len(allEventTypeCohort)
	if got != wantCount {
		t.Errorf(
			"EV-027 amendment gate: allEventTypeCohort has %d entries, want %d.\n"+
				"Adding or removing a cross-bus EventType requires a foundation amendment per\n"+
				"event-model.md §4.6 EV-027 and architecture.md §4.6. Update wantCount here\n"+
				"and include the amendment details in your commit message.",
			got, wantCount,
		)
	}
}
