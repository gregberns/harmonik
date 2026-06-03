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

	// EventTypeNodeDispatchDecided is the node_dispatch_decided event type.
	// Emitted by the DOT-mode cascade engine after EM-041 edge selection resolves
	// the next node (or determines terminal state / cascade failure).
	// Durability class: O.
	// Bead ref: hk-bf85t (T-IMPL-008).
	EventTypeNodeDispatchDecided EventType = "node_dispatch_decided"

	// EventTypeBeadClosed is the bead_closed event type (§4.12.EM-052).
	// Emitted after CloseBead succeeds on a success branch, before run_completed.
	// Durability class: F.
	// Refs: hk-ftyvo.
	EventTypeBeadClosed EventType = "bead_closed"

	// EventTypeWorkingTreeRefreshFailed is the working_tree_refresh_failed event
	// type (§4.12.EM-054). Emitted when git reset --hard HEAD fails after a
	// successful merge-to-main. The merge itself succeeded; this event is
	// informational — the daemon continues to CloseBead normally.
	// Durability class: O.
	// Refs: hk-4goy3.
	EventTypeWorkingTreeRefreshFailed EventType = "working_tree_refresh_failed"

	// EventTypeImplementerEscapedWorktree is emitted by the daemon when, after
	// the implementer process exits, the MAIN repo's working tree contains
	// dirty files outside the normal harmonik churn allowlist
	// (.harmonik/, .claude/, .beads/issues.jsonl). This indicates the
	// implementer wrote files into the main repo instead of its worktree
	// (cross-contamination — the run branch will have no commit but main
	// is now dirty). Durability class: F (terminal-state landmark; the
	// run is failed on this event).
	// Refs: hk-6zylj.
	EventTypeImplementerEscapedWorktree EventType = "implementer_escaped_worktree"

	// EventTypeImplementerPhaseComplete is emitted by the daemon immediately
	// after the implementer session ends (regardless of how: normal exit,
	// noChange-timeout kill, or context cancellation) and before any reviewer
	// phase begins. Closes the diagnostic gap between run_started and
	// reviewer_launched where silent implementer failures previously produced
	// no event. Durability class: F.
	// Refs: hk-cd8yu.
	EventTypeImplementerPhaseComplete EventType = "implementer_phase_complete"

	// EventTypeMergeBuildFailed is the merge_build_failed event type.
	// Emitted inside lockedMergeRunBranchToMain when go build+vet fails on
	// the freshly fast-forwarded merged tree, before the push. The update-ref
	// is rolled back and the push is skipped; the caller reopens the bead.
	// Durability class: F (terminal-state landmark; the bead is about to be
	// reopened so loss would leave it closed when it should be open).
	// Refs: hk-o68j3.
	EventTypeMergeBuildFailed EventType = "merge_build_failed"
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

	// EventTypeSkillsResolved is the skills_resolved event type.
	// Emitted at workflow-ingest time when a node's skills_ref attribute is
	// successfully resolved against the run's policy skill_sets[] block
	// per [control-points.md §4.13 CP-057].
	// Durability class: O.
	EventTypeSkillsResolved EventType = "skills_resolved"
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

	// EventTypeAgentReadyTimeout is the agent_ready_timeout event type.
	// Emitted by the daemon workloop when HC-056 fires: no agent_ready event
	// arrived within the configured timeout window (default 30s). Carries
	// run_id, claude_session_id, and timeout_ms so post-hoc analysis can
	// correlate which runs never became ready (hk-5cox8 observability).
	// Durability class: O.
	// Refs: hk-5cox8.
	EventTypeAgentReadyTimeout EventType = "agent_ready_timeout"

	// EventTypeLifecycleTransition is the lifecycle_transition event type (§8.3.14).
	// Emitted by the watcher goroutine on every LifecycleState machine transition
	// per handler-contract.md §4.13 HC-064..HC-067.
	// Payload: from_state, to_state, reason, transitioned_at.
	// Durability class: O (reconstructible from the in-memory transition-history ring).
	// Spec ref: event-model.md §8.3.14.
	// Bead ref: hk-xrygh.
	EventTypeLifecycleTransition EventType = "lifecycle_transition"

	// EventTypePasteInjectFailed is the pasteinject_failed event type.
	// Emitted by the daemon when the paste-inject step cannot deliver the
	// kick-off message to the tmux pane (file absent, WriteLastPane error, etc.).
	// Payload: run_id, phase, reason.
	// Durability class: O.
	// Refs: hk-fra5l.
	EventTypePasteInjectFailed EventType = "pasteinject_failed"

	// EventTypeLaunchStallDetected is the launch_stall_detected event type.
	// Emitted by the stale watcher when a run has emitted run_started but no
	// launch_initiated within launchStallThreshold (30 s). Indicates the
	// pre-exec sequence stalled — most likely a tmux window creation failure
	// or a pre-exec emission gap in the daemon.
	// Payload: run_id, bead_id, stall_seconds.
	// Durability class: O.
	// Refs: hk-fra5l.
	EventTypeLaunchStallDetected EventType = "launch_stall_detected"
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

	// EventTypeReconciliationMismatchObserved is the reconciliation_mismatch_observed
	// event type (§8.6.15 — added by hk-nvfvj full three-way reconciliation).
	// Durability class: O.
	//
	// Emitted during daemon startup three-way reconciliation (QM-002b) for every
	// mismatch class that does not produce a queue_item_reconciled correction:
	//   - bead_closed_queue_pending    — queue item pending but ledger shows closed
	//   - bead_closed_queue_dispatched — queue item dispatched but ledger shows closed (Class A')
	//   - bead_inprogress_queue_absent — ledger in_progress with no queue record
	//   - bead_closed_queue_inprogress — queue item completed/failed but ledger in_progress
	EventTypeReconciliationMismatchObserved EventType = "reconciliation_mismatch_observed"
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

	// EventTypeDaemonConfig is the daemon_config event type (§8.7.18).
	// Emitted at startup after validation passes, stating the resolved merge
	// target and active branch-protection policy.
	// Durability class: O.
	// Bead ref: hk-sul12.
	EventTypeDaemonConfig EventType = "daemon_config"
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

	// EventTypeReviewBypassed is the review_bypassed event type.
	// Emitted during workflow-mode resolution (EM-012a) when a bead carries an
	// explicit workflow:single label, gating the single mode behind an observable
	// audit event. Durability class: O (ordinary — informational; the resolution
	// outcome is recorded in the run record).
	// Bead ref: hk-81n9r.
	EventTypeReviewBypassed EventType = "review_bypassed"
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

// ---------------------------------------------------------------------------
// §8.11 Handler-pause lifecycle event types (handler-pause MVH, hk-ifqnj)
// ---------------------------------------------------------------------------

const (
	// EventTypeHandlerPaused is the handler_paused event type (§8.11.1).
	// Durability class: F (fsync-boundary — pause-state landmark for restart recovery).
	EventTypeHandlerPaused EventType = "handler_paused"

	// EventTypeHandlerResumed is the handler_resumed event type (§8.11.2).
	// Durability class: F (fsync-boundary — resume action must be durable before dispatcher proceeds).
	EventTypeHandlerResumed EventType = "handler_resumed"

	// EventTypeQueueItemHeldForHandlerPause is the queue_item_held_for_handler_pause event type (§8.11.3).
	// Durability class: O (ordinary — reconstructible from handler-state.json + queue.json).
	// Dedup: at-most-once per (bead_id, paused_epoch) per §8.11.3 dedup contract.
	EventTypeQueueItemHeldForHandlerPause EventType = "queue_item_held_for_handler_pause"
)

// ---------------------------------------------------------------------------
// §8.10 Queue lifecycle event types (extqueue v0.1)
// ---------------------------------------------------------------------------

const (
	// EventTypeQueueSubmitted is the queue_submitted event type (§8.10.1).
	// Durability class: F.
	EventTypeQueueSubmitted EventType = "queue_submitted"

	// EventTypeQueueGroupStarted is the queue_group_started event type (§8.10.2).
	// Durability class: O.
	EventTypeQueueGroupStarted EventType = "queue_group_started"

	// EventTypeQueueGroupCompleted is the queue_group_completed event type (§8.10.3).
	// Durability class: F.
	EventTypeQueueGroupCompleted EventType = "queue_group_completed"

	// EventTypeQueuePaused is the queue_paused event type (§8.10.4).
	// Durability class: F.
	EventTypeQueuePaused EventType = "queue_paused"

	// EventTypeQueueAppended is the queue_appended event type (§8.10.5).
	// Durability class: O.
	EventTypeQueueAppended EventType = "queue_appended"

	// EventTypeQueueItemDeferredForLedgerDep is the queue_item_deferred_for_ledger_dep
	// event type (§8.10.6). Durability class: O.
	EventTypeQueueItemDeferredForLedgerDep EventType = "queue_item_deferred_for_ledger_dep"

	// EventTypeQueueItemReconciled is the queue_item_reconciled event type (§8.10.7).
	// Durability class: F — loss could silently re-dispatch a reverted item per EV-016.
	// Added in QM-002a v0.1.1.
	EventTypeQueueItemReconciled EventType = "queue_item_reconciled"
)

// ---------------------------------------------------------------------------
// §8.12 Staleness-detection event types (hk-wkzlc)
// ---------------------------------------------------------------------------

const (
	// EventTypeRunStale is the run_stale event type (§8.12.1).
	// Emitted by the stale-watch goroutine when an active run has produced no
	// event of any kind for M minutes (configurable; default 10). Re-emitted
	// at 2M, 4M, … (exponential, capped) until the run terminates.
	// Durability class: O (ordinary — observational; orchestrator decides action).
	// Refs: hk-wkzlc.
	EventTypeRunStale EventType = "run_stale"
)

// ---------------------------------------------------------------------------
// §8.2a Gate-node dispatch event types (hk-jtxnr)
// ---------------------------------------------------------------------------

const (
	// EventTypeGateDecisionRecorded is the gate_decision_recorded event type.
	// Emitted by the gate-node dispatch module after a gate evaluator produces
	// a GateDecisionPayload outcome (CP §6.5). Captures the full decision
	// envelope for audit and replay.
	// Durability class: O (ordinary — observability and audit).
	// Refs: hk-jtxnr (T-IMPL-010).
	EventTypeGateDecisionRecorded EventType = "gate_decision_recorded"
)

// ---------------------------------------------------------------------------
// §8.9 Cognition loop event types (cognition-loop.md)
// ---------------------------------------------------------------------------

const (
	// EventTypeLoopObservedPhantomDone is the loop_observed_phantom_done event
	// type.  Emitted by the cognition loop harness when a bead's Refs: trailer
	// is present on origin/main (Condition 2 of CL-051 two-phase done) but no
	// run_completed{success} terminal event has been observed for that bead
	// (Condition 1 absent).  The harness MUST NOT act directly; it routes to
	// Tier-2 reconciliation.
	//
	// Payload: {"bead_id": "<bead-id>"}
	// Durability class: O (warning; reconstructible via CL-051 re-check).
	//
	// Spec ref: specs/cognition-loop.md §4.7 CL-051.
	// Refs: hk-iht2w.
	EventTypeLoopObservedPhantomDone EventType = "loop_observed_phantom_done"
)
