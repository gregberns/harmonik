package core

// EventType is the typed string identifier for an event type in the §8 taxonomy
// (event-model.md §8).
//
// The ~79 constants below cover all active rows across §8.1–§8.8. The
// Event.Type field uses this type per EV-001; the registry (eventregistry.go)
// enforces that only registered types are dispatched.
//
// Spec ref: specs/event-model.md §6.1 (Event.type), §8 (taxonomy table).
// Bead ref: hk-hqwn.59.82.
type EventType string

// Valid reports whether e is a non-empty EventType string.
// Registry-level validation (known vs unknown type) is enforced by EventRegistry.
func (e EventType) Valid() bool {
	return e != ""
}

// ---------------------------------------------------------------------------
// §8.1 Run lifecycle event types
// ---------------------------------------------------------------------------

const (
	// EventTypeRunStarted is the run_started event type (§8.1.1).
	// Durability class: F.
	EventTypeRunStarted EventType = "run_started"

	// EventTypeRunCompleted is the run_completed event type (§8.1.2).
	// Durability class: F.
	EventTypeRunCompleted EventType = "run_completed"

	// EventTypeRunFailed is the run_failed event type (§8.1.3).
	// Durability class: F.
	EventTypeRunFailed EventType = "run_failed"

	// EventTypeStateEntered is the state_entered event type (§8.1.4).
	// Durability class: O.
	EventTypeStateEntered EventType = "state_entered"

	// EventTypeStateExited is the state_exited event type (§8.1.5).
	// Durability class: O.
	EventTypeStateExited EventType = "state_exited"

	// EventTypeTransitionEvent is the transition_event event type (§8.1.6).
	// Durability class: F.
	EventTypeTransitionEvent EventType = "transition_event"

	// EventTypeCheckpointWritten is the checkpoint_written event type (§8.1.7).
	// Durability class: F.
	EventTypeCheckpointWritten EventType = "checkpoint_written"

	// EventTypeOutcomeEmitted is the outcome_emitted event type (§8.1.8).
	// Durability class: O.
	EventTypeOutcomeEmitted EventType = "outcome_emitted"

	// EventTypeSubWorkflowEntered is the sub_workflow_entered event type (§8.1.9).
	// Durability class: O.
	EventTypeSubWorkflowEntered EventType = "sub_workflow_entered"

	// EventTypeSubWorkflowExited is the sub_workflow_exited event type (§8.1.10).
	// Durability class: O.
	EventTypeSubWorkflowExited EventType = "sub_workflow_exited"

	// EventTypeNodeDispatchRequested is the node_dispatch_requested event type (§8.1.11).
	// Durability class: O.
	EventTypeNodeDispatchRequested EventType = "node_dispatch_requested"
)

// ---------------------------------------------------------------------------
// §8.2 Control-point lifecycle event types
// ---------------------------------------------------------------------------

const (
	// EventTypeHookFired is the hook_fired event type (§8.2.1).
	// Durability class: O.
	EventTypeHookFired EventType = "hook_fired"

	// EventTypeHookFailed is the hook_failed event type (§8.2.2).
	// Durability class: O.
	EventTypeHookFailed EventType = "hook_failed"

	// EventTypeHookVerdictPersisted is the hook_verdict_persisted event type (§8.2.3).
	// Durability class: O.
	EventTypeHookVerdictPersisted EventType = "hook_verdict_persisted"

	// EventTypeGateAllowed is the gate_allowed event type (§8.2.4).
	// Durability class: O.
	EventTypeGateAllowed EventType = "gate_allowed"

	// EventTypeGateDenied is the gate_denied event type (§8.2.5).
	// Durability class: O.
	EventTypeGateDenied EventType = "gate_denied"

	// EventTypeGateEscalated is the gate_escalated event type (§8.2.6).
	// Durability class: O.
	EventTypeGateEscalated EventType = "gate_escalated"

	// EventTypeGuardReordered is the guard_reordered event type (§8.2.7).
	// Durability class: O.
	EventTypeGuardReordered EventType = "guard_reordered"

	// EventTypeGuardFailed is the guard_failed event type (§8.2.8).
	// Durability class: O.
	EventTypeGuardFailed EventType = "guard_failed"

	// EventTypeControlPointsRegistered is the control_points_registered event type (§8.2.9).
	// Durability class: O.
	EventTypeControlPointsRegistered EventType = "control_points_registered"

	// EventTypeControlPointsRegistrationStarted is the control_points_registration_started
	// event type (§8.2.10). Durability class: O.
	EventTypeControlPointsRegistrationStarted EventType = "control_points_registration_started"

	// EventTypeVerdictEnvelopeMismatch is the verdict_envelope_mismatch event type (§8.2.11).
	// Durability class: O.
	EventTypeVerdictEnvelopeMismatch EventType = "verdict_envelope_mismatch"

	// EventTypePolicyExpressionExceededCost is the policy_expression_exceeded_cost
	// event type (§8.2.12). Durability class: F.
	EventTypePolicyExpressionExceededCost EventType = "policy_expression_exceeded_cost"
)

// ---------------------------------------------------------------------------
// §8.3 Agent/handler event types
// ---------------------------------------------------------------------------

const (
	// EventTypeAgentReady is the agent_ready event type (§8.3.1).
	// Durability class: F.
	EventTypeAgentReady EventType = "agent_ready"

	// EventTypeAgentStarted is the agent_started event type (§8.3.2).
	// Durability class: F.
	EventTypeAgentStarted EventType = "agent_started"

	// EventTypeAgentOutputChunk is the agent_output_chunk event type (§8.3.3).
	// Durability class: O.
	EventTypeAgentOutputChunk EventType = "agent_output_chunk"

	// EventTypeAgentCompleted is the agent_completed event type (§8.3.4).
	// Durability class: F.
	EventTypeAgentCompleted EventType = "agent_completed"

	// EventTypeAgentFailed is the agent_failed event type (§8.3.5).
	// Durability class: F.
	EventTypeAgentFailed EventType = "agent_failed"

	// EventTypeAgentHeartbeat is the agent_heartbeat event type (§8.3 HC-026a).
	// Durability class: O.
	EventTypeAgentHeartbeat EventType = "agent_heartbeat"

	// EventTypeAgentRateLimitStatus is the agent_rate_limit_status event type (§8.3.6).
	// Durability class: O.
	EventTypeAgentRateLimitStatus EventType = "agent_rate_limit_status"

	// EventTypeSessionLogLocation is the session_log_location event type (§8.3.7).
	// Durability class: O.
	EventTypeSessionLogLocation EventType = "session_log_location"

	// EventTypeSkillsProvisioned is the skills_provisioned event type (§8.3.8).
	// Durability class: O.
	EventTypeSkillsProvisioned EventType = "skills_provisioned"

	// EventTypeHandlerCapabilities is the handler_capabilities event type (§8.3.9).
	// Durability class: O.
	EventTypeHandlerCapabilities EventType = "handler_capabilities"

	// EventTypeAgentWarningSilentHang is the agent_warning_silent_hang event type (§8.3.10).
	// Durability class: O.
	EventTypeAgentWarningSilentHang EventType = "agent_warning_silent_hang"

	// EventTypeAgentResumedAfterWarning is the agent_resumed_after_warning event type (§8.3.11).
	// Durability class: O.
	EventTypeAgentResumedAfterWarning EventType = "agent_resumed_after_warning"

	// EventTypeAgentSoftTerminating is the agent_soft_terminating event type (§8.3.12).
	// Durability class: O.
	EventTypeAgentSoftTerminating EventType = "agent_soft_terminating"

	// EventTypeAgentHardTerminating is the agent_hard_terminating event type (§8.3.13).
	// Durability class: O.
	EventTypeAgentHardTerminating EventType = "agent_hard_terminating"

	// EventTypeLaunchInitiated is the launch_initiated event type.
	// Emitted by the handler-process pre-exec (CHB-018 step 4) under the
	// interactive (tmux) substrate.  Signals that the handler is about to exec
	// Claude but does NOT indicate ready-state — that is the relay-synthesized
	// agent_ready on first SessionStart receipt (CHB-013 / HC-039).
	// Durability class: O.
	EventTypeLaunchInitiated EventType = "launch_initiated"
)

// ---------------------------------------------------------------------------
// §8.4 Budget event types
// ---------------------------------------------------------------------------

const (
	// EventTypeBudgetWarning is the budget_warning event type (§8.4.1).
	// Durability class: O.
	EventTypeBudgetWarning EventType = "budget_warning"

	// EventTypeBudgetAccrual is the budget_accrual event type (§8.4.2).
	// Durability class: O.
	EventTypeBudgetAccrual EventType = "budget_accrual"

	// EventTypeBudgetExhausted is the budget_exhausted event type (§8.4.3).
	// Durability class: F.
	EventTypeBudgetExhausted EventType = "budget_exhausted"
)

// ---------------------------------------------------------------------------
// §8.5 Workspace event types
// ---------------------------------------------------------------------------

const (
	// EventTypeWorkspaceCreated is the workspace_created event type (§8.5.1).
	// Durability class: F.
	EventTypeWorkspaceCreated EventType = "workspace_created"

	// EventTypeWorkspaceLeased is the workspace_leased event type (§8.5.2).
	// Durability class: F.
	EventTypeWorkspaceLeased EventType = "workspace_leased"

	// EventTypeWorkspaceMergeStatus is the workspace_merge_status event type (§8.5.3).
	// Durability class: F.
	EventTypeWorkspaceMergeStatus EventType = "workspace_merge_status"

	// EventTypeWorkspaceDiscarded is the workspace_discarded event type (§8.5.4).
	// Durability class: F.
	EventTypeWorkspaceDiscarded EventType = "workspace_discarded"

	// EventTypeWorkspaceInterrupted is the workspace_interrupted event type (§8.5.5).
	// Durability class: F.
	EventTypeWorkspaceInterrupted EventType = "workspace_interrupted"

	// EventTypeMergeConflictEscalation is the merge_conflict_escalation event type (§8.5.6).
	// Durability class: F.
	EventTypeMergeConflictEscalation EventType = "merge_conflict_escalation"
)

// ---------------------------------------------------------------------------
// §8.6 Reconciliation event types
// ---------------------------------------------------------------------------

const (
	// EventTypeReconciliationStarted is the reconciliation_started event type (§8.6.1).
	// Durability class: F.
	EventTypeReconciliationStarted EventType = "reconciliation_started"

	// EventTypeReconciliationCategoryAssigned is the reconciliation_category_assigned
	// event type (§8.6.2). Durability class: F.
	EventTypeReconciliationCategoryAssigned EventType = "reconciliation_category_assigned"

	// EventTypeReconciliationVerdictEmitted is the reconciliation_verdict_emitted
	// event type (§8.6.3). Durability class: F.
	EventTypeReconciliationVerdictEmitted EventType = "reconciliation_verdict_emitted"

	// EventTypeReconciliationVerdictExecuted is the reconciliation_verdict_executed
	// event type (§8.6.4). Durability class: F.
	EventTypeReconciliationVerdictExecuted EventType = "reconciliation_verdict_executed"

	// EventTypeReconciliationVerdictMalformed is the reconciliation_verdict_malformed
	// event type (§8.6.5). Durability class: O.
	EventTypeReconciliationVerdictMalformed EventType = "reconciliation_verdict_malformed"

	// EventTypeReconciliationBudgetExhausted is the reconciliation_budget_exhausted
	// event type (§8.6.6). Durability class: F.
	EventTypeReconciliationBudgetExhausted EventType = "reconciliation_budget_exhausted"

	// EventTypeReconciliationVerdictStale is the reconciliation_verdict_stale
	// event type (§8.6.7). Durability class: O.
	EventTypeReconciliationVerdictStale EventType = "reconciliation_verdict_stale"

	// EventTypeStoreDivergenceDetected is the store_divergence_detected event type (§8.6.8).
	// Durability class: F.
	EventTypeStoreDivergenceDetected EventType = "store_divergence_detected"

	// EventTypeOperatorEscalationRequired is the operator_escalation_required
	// event type (§8.6.9). Durability class: F.
	EventTypeOperatorEscalationRequired EventType = "operator_escalation_required"

	// EventTypeDivergenceInconclusive is the divergence_inconclusive event type (§8.6.10).
	// Durability class: O.
	EventTypeDivergenceInconclusive EventType = "divergence_inconclusive"

	// EventTypeReconciliationDispatchDeduplicated is the reconciliation_dispatch_deduplicated
	// event type (§8.6.11). Durability class: O.
	EventTypeReconciliationDispatchDeduplicated EventType = "reconciliation_dispatch_deduplicated"

	// EventTypeReconciliationDetectorPanic is the reconciliation_detector_panic
	// event type (§8.6.12). Durability class: O.
	EventTypeReconciliationDetectorPanic EventType = "reconciliation_detector_panic"

	// EventTypeReconciliationVerdictExecutionRetry is the reconciliation_verdict_execution_retry
	// event type (§8.6.13). Durability class: O.
	EventTypeReconciliationVerdictExecutionRetry EventType = "reconciliation_verdict_execution_retry"

	// EventTypeBeadTerminalTransitionRecovered is the bead_terminal_transition_recovered
	// event type (§8.6.14). Durability class: F.
	//
	// (post-MVH) Reserved per OQ-BI-008. No MVH emitter exists; structured-log
	// via ON-035 at MVH per event-model.md §8.6.14.
	EventTypeBeadTerminalTransitionRecovered EventType = "bead_terminal_transition_recovered"
)

// ---------------------------------------------------------------------------
// §8.7 Operator-control and daemon lifecycle event types
// ---------------------------------------------------------------------------

const (
	// EventTypeDaemonStarted is the daemon_started event type (§8.7.1).
	// Durability class: F.
	EventTypeDaemonStarted EventType = "daemon_started"

	// EventTypeDaemonReady is the daemon_ready event type (§8.7.2).
	// Durability class: F.
	EventTypeDaemonReady EventType = "daemon_ready"

	// EventTypeDaemonShutdown is the daemon_shutdown event type (§8.7.3).
	// Durability class: F.
	EventTypeDaemonShutdown EventType = "daemon_shutdown"

	// EventTypeDaemonStartupFailed is the daemon_startup_failed event type (§8.7.4).
	// Durability class: F.
	EventTypeDaemonStartupFailed EventType = "daemon_startup_failed"

	// EventTypeDaemonDegraded is the daemon_degraded event type (§8.7.5).
	// Durability class: O.
	EventTypeDaemonDegraded EventType = "daemon_degraded"

	// EventTypeOperatorPauseStatus is the operator_pause_status event type (§8.7.6).
	// Durability class: O.
	EventTypeOperatorPauseStatus EventType = "operator_pause_status"

	// EventTypeOperatorResuming is the operator_resuming event type (§8.7.7).
	// Durability class: O.
	EventTypeOperatorResuming EventType = "operator_resuming"

	// EventTypeOperatorStopped is the operator_stopped event type (§8.7.8).
	// Durability class: O.
	EventTypeOperatorStopped EventType = "operator_stopped"

	// EventTypeOperatorUpgrading is the operator_upgrading event type (§8.7.9).
	// Durability class: O.
	EventTypeOperatorUpgrading EventType = "operator_upgrading"

	// EventTypeOperatorUpgradeCompleted is the operator_upgrade_completed event type (§8.7.10).
	// Durability class: F.
	EventTypeOperatorUpgradeCompleted EventType = "operator_upgrade_completed"

	// EventTypeOperatorUpgradeRejected is the operator_upgrade_rejected event type (§8.7.11).
	// Durability class: O.
	EventTypeOperatorUpgradeRejected EventType = "operator_upgrade_rejected"

	// EventTypeOperatorCommandRejected is the operator_command_rejected event type (§8.7.12).
	// Durability class: O.
	EventTypeOperatorCommandRejected EventType = "operator_command_rejected"

	// EventTypeDispatchDeferred is the dispatch_deferred event type (§8.7.13).
	// Durability class: O.
	EventTypeDispatchDeferred EventType = "dispatch_deferred"

	// EventTypeDaemonOrphanSweepCompleted is the daemon_orphan_sweep_completed
	// event type (§8.7.14). Durability class: O.
	EventTypeDaemonOrphanSweepCompleted EventType = "daemon_orphan_sweep_completed"

	// EventTypeInfrastructureUnavailable is the infrastructure_unavailable event type (§8.7.15).
	// Durability class: O.
	EventTypeInfrastructureUnavailable EventType = "infrastructure_unavailable"

	// EventTypeOperatorCommandFailed is the operator_command_failed event type (§8.7.16).
	// Durability class: O.
	EventTypeOperatorCommandFailed EventType = "operator_command_failed"

	// EventTypeOperatorEscalationCleared is the operator_escalation_cleared
	// event type (§8.7.17). Durability class: O.
	EventTypeOperatorEscalationCleared EventType = "operator_escalation_cleared"
)

// ---------------------------------------------------------------------------
// §8.1a Review-loop cycle event types (only when workflow_mode = review-loop)
// ---------------------------------------------------------------------------

const (
	// EventTypeImplementerResumed is the implementer_resumed event type (§8.1a.1).
	// Durability class: O. Emitted before each implementer dispatch from iteration 2+.
	EventTypeImplementerResumed EventType = "implementer_resumed"

	// EventTypeReviewerLaunched is the reviewer_launched event type (§8.1a.2).
	// Durability class: O. Emitted before each reviewer dispatch.
	EventTypeReviewerLaunched EventType = "reviewer_launched"

	// EventTypeReviewerVerdict is the reviewer_verdict event type (§8.1a.3).
	// Durability class: F. Emitted after reading and validating .harmonik/review.json.
	EventTypeReviewerVerdict EventType = "reviewer_verdict"

	// EventTypeIterationCapHit is the iteration_cap_hit event type (§8.1a.4).
	// Durability class: O. Emitted when iteration cap is reached.
	EventTypeIterationCapHit EventType = "iteration_cap_hit"

	// EventTypeNoProgressDetected is the no_progress_detected event type (§8.1a.5).
	// Durability class: O. Emitted when diff hash matches prior iteration.
	EventTypeNoProgressDetected EventType = "no_progress_detected"

	// EventTypeReviewLoopCycleComplete is the review_loop_cycle_complete event type (§8.1a.6).
	// Durability class: F. Emitted exactly once per cycle before run_completed/run_failed.
	EventTypeReviewLoopCycleComplete EventType = "review_loop_cycle_complete"
)

// ---------------------------------------------------------------------------
// §8.8 Observability and bus-internal event types
// ---------------------------------------------------------------------------

const (
	// EventTypeMetric is the metric event type (§8.8.1).
	// Durability class: L (lossy-tail-ok; §8.9(g) escape-hatch exception).
	EventTypeMetric EventType = "metric"

	// EventTypeConsumerFailed is the consumer_failed event type (§8.8.2).
	// Durability class: O.
	EventTypeConsumerFailed EventType = "consumer_failed"

	// EventTypeDeadLetterEnqueued is the dead_letter_enqueued event type (§8.8.3).
	// Durability class: O.
	EventTypeDeadLetterEnqueued EventType = "dead_letter_enqueued"

	// EventTypeBusOverflow is the bus_overflow event type (§8.8.4).
	// Durability class: O (promoted to F via direct-JSONL-append fallback per EV-011a).
	EventTypeBusOverflow EventType = "bus_overflow"

	// EventTypeRedactionFailed is the redaction_failed event type (§8.8.5).
	// Durability class: O.
	EventTypeRedactionFailed EventType = "redaction_failed"

	// EventTypeBeadLabelConflict is the bead_label_conflict event type (§8.8.6).
	// Durability class: O (ordinary — claim-path observational evidence; the
	// resolution path falls through to a defined tier-2/3/4 result per §4.3.EM-012a).
	//
	// Emitted by the daemon's claim path when (a) a bead carries more than one
	// workflow:<mode> label, or (b) a bead carries a workflow:<mode> label whose
	// <mode> value is not in {single, review-loop, dot}. In either case, tier-1
	// is treated as absent and the precedence walk continues.
	EventTypeBeadLabelConflict EventType = "bead_label_conflict"
)
