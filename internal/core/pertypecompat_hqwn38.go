package core

// pertypecompat_hqwn38.go — Per-type N-1 compatibility window declarations
// per EV-029 (event-model.md §4.8 EV-029).
//
// EV-029 states: "Readers of events MUST accept the immediately prior schema
// version (N-1) for every event type AND for the envelope. Per-type independence
// means harmonik maintains up to 71+ independent compatibility contracts."
//
// This file provides:
//
//  1. PayloadCompatEntry — the struct declaring the N-1 compatibility contract
//     for one event type.
//  2. allPayloadCompatEntries — the exhaustive per-type compat table. Tests in
//     pertypecompat_hqwn38_test.go assert this table covers every registered type
//     and that every entry with a prior version (PreviousVersion != 0) declares
//     CompatWindowHolds = true.
//  3. LookupPayloadCompatEntry — lookup helper.
//  4. AllPayloadCompatEntries — slice accessor used by tests.
//
// ## How to evolve a type's schema version
//
// When a payload type advances from version N to N+1:
//   1. Update the payload struct (additive-only changes require no migration
//      release per §6.4; breaking changes require a migration release).
//   2. Add a call to SetPayloadSchemaVersion(typeName, N+1) in the appropriate
//      eventreg_*.go init path.
//   3. Update or add the PayloadCompatEntry in allPayloadCompatEntries:
//      - Set CurrentVersion = N+1, PreviousVersion = N.
//      - Set CompatWindowHolds = true (additive-only changes) or false (breaking
//        change — which requires a migration release per operator-nfr.md §4.3
//        ON-018/ON-019).
//      - Set AdditiveOnly = true when the delta is additive-only (non-breaking
//        per §6.4).
//   4. Run tests — pertypecompat_hqwn38_test.go will catch any omission.
//
// ## Initial state (all types at v1)
//
// All event types start at schema version 1. At v1 there is no prior version
// (PreviousVersion = 0), so the N-1 compat window is vacuously satisfied. The
// CompatWindowHolds field is true for all v1 entries.
//
// Spec ref: event-model.md §4.8 EV-028, EV-029; §6.4 breaking-change table;
// operator-nfr.md §4.5 ON-018, ON-019.
// Bead ref: hk-hqwn.38.

// PayloadCompatEntry declares the N-1 compatibility window for one registered
// event type. It is the per-type analogue of the cross-artifact compatibility
// matrix in internal/operatornfr/schemacompatwindow_test.go.
//
// Spec ref: event-model.md §4.8 EV-029.
type PayloadCompatEntry struct {
	// TypeName is the §8 event type name (e.g. "run_started").
	TypeName string

	// CurrentVersion is the current schema version of this type's payload (≥ 1).
	CurrentVersion int

	// PreviousVersion is the immediately prior schema version. Zero means this
	// type has no prior version (it is at its initial schema). When non-zero,
	// CompatWindowHolds MUST be true unless this is a declared migration release.
	PreviousVersion int

	// CompatWindowHolds asserts that a reader at PreviousVersion can successfully
	// parse payload bytes written by a writer at CurrentVersion. MUST be true for
	// additive-only changes. May be false only for a declared migration release
	// (operator-nfr.md §4.5 ON-018/ON-019).
	CompatWindowHolds bool

	// AdditiveOnly is true when the CurrentVersion → PreviousVersion delta
	// consists solely of additive (non-breaking) changes per §6.4. When true,
	// CompatWindowHolds MUST also be true.
	AdditiveOnly bool
}

// allPayloadCompatEntries is the authoritative per-type N-1 compatibility table.
// It must cover every registered event type (tests enforce this).
//
// At initial state all types are at version 1 with PreviousVersion = 0.
// Entries are grouped by §8 section for readability and exactly match the
// registered type names from eventreg_hqwn59.go and its companion files.
//
// Amendment rule: any addition or modification to this table requires the
// reviewer to verify §6.4 classification (additive vs. breaking) and, for
// breaking changes, a migration release per ON-018/ON-019.
var allPayloadCompatEntries = []PayloadCompatEntry{
	// ── §8.1 Run lifecycle ──────────────────────────────────────────────────
	{TypeName: "run_started", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "run_completed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "run_failed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "state_entered", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "state_exited", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "transition_event", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "checkpoint_written", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "outcome_emitted", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "sub_workflow_entered", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "sub_workflow_exited", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "node_dispatch_requested", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "node_dispatch_decided", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "bead_closed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "working_tree_refresh_failed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "implementer_escaped_worktree", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "implementer_phase_complete", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	// ── §8.1a Review-loop cycle ─────────────────────────────────────────────
	{TypeName: "implementer_resumed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reviewer_launched", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reviewer_verdict", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "iteration_cap_hit", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "no_progress_detected", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "review_loop_cycle_complete", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	// ── §8.2 Control-point lifecycle ───────────────────────────────────────
	{TypeName: "hook_fired", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "hook_failed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "hook_verdict_persisted", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "gate_allowed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "gate_denied", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "gate_escalated", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "gate_decision_recorded", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "skills_resolved", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "guard_reordered", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "guard_failed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "control_points_registered", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "control_points_registration_started", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "verdict_envelope_mismatch", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "policy_expression_exceeded_cost", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	// ── §8.3 Agent/handler lifecycle ───────────────────────────────────────
	{TypeName: "agent_started", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_ready", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_output_chunk", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_completed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_failed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_heartbeat", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_rate_limit_status", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "session_log_location", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "skills_provisioned", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "handler_capabilities", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_warning_silent_hang", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_resumed_after_warning", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_soft_terminating", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_hard_terminating", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "launch_initiated", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_ready_timeout", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	// ── §8.4 Budget lifecycle ───────────────────────────────────────────────
	{TypeName: "budget_accrual", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "budget_warning", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "budget_exhausted", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	// ── §8.5 Workspace lifecycle ────────────────────────────────────────────
	{TypeName: "workspace_created", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "workspace_leased", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "workspace_discarded", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "workspace_interrupted", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "workspace_merge_status", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	// ── §8.6 Reconciliation lifecycle ──────────────────────────────────────
	{TypeName: "reconciliation_started", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reconciliation_mismatch_observed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reconciliation_category_assigned", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reconciliation_verdict_emitted", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reconciliation_verdict_executed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reconciliation_verdict_malformed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reconciliation_verdict_stale", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reconciliation_verdict_execution_retry", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reconciliation_budget_exhausted", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reconciliation_dispatch_deduplicated", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "reconciliation_detector_panic", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "bead_terminal_transition_recovered", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "operator_escalation_required", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "divergence_inconclusive", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "store_divergence_detected", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	// ── §8.7 Operator-control and daemon lifecycle ──────────────────────────
	{TypeName: "daemon_started", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "daemon_ready", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "daemon_shutdown", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "daemon_degraded", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "daemon_startup_failed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "daemon_orphan_sweep_completed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "operator_upgrade_rejected", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "operator_upgrade_completed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "operator_upgrading", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "operator_stopped", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "operator_resuming", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "operator_pause_status", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "operator_command_failed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "operator_command_rejected", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "operator_escalation_cleared", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "infrastructure_unavailable", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "merge_conflict_escalation", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "dispatch_deferred", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	// ── §8.8 Observability and bus-internal ────────────────────────────────
	{TypeName: "consumer_failed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "dead_letter_enqueued", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "bus_overflow", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "metric", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "redaction_failed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "bead_label_conflict", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	// ── §8.10 Queue lifecycle ───────────────────────────────────────────────
	{TypeName: "queue_submitted", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "queue_group_started", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "queue_group_completed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "queue_paused", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "queue_appended", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "queue_item_deferred_for_ledger_dep", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "queue_item_reconciled", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "queue_item_held_for_handler_pause", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	// ── §8.11 Handler-pause lifecycle ──────────────────────────────────────
	{TypeName: "handler_paused", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "handler_resumed", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	// ── §8.12 Staleness-detection ───────────────────────────────────────────
	{TypeName: "run_stale", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
}

// LookupPayloadCompatEntry returns the PayloadCompatEntry for the given
// event type name, or (PayloadCompatEntry{}, false) if not declared.
func LookupPayloadCompatEntry(typeName string) (PayloadCompatEntry, bool) {
	for _, e := range allPayloadCompatEntries {
		if e.TypeName == typeName {
			return e, true
		}
	}
	return PayloadCompatEntry{}, false
}

// AllPayloadCompatEntries returns a copy of the full per-type compat table.
// Used by tests and diagnostic tooling.
func AllPayloadCompatEntries() []PayloadCompatEntry {
	out := make([]PayloadCompatEntry, len(allPayloadCompatEntries))
	copy(out, allPayloadCompatEntries)
	return out
}
