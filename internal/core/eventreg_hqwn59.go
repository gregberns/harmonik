package core

// eventreg_hqwn59.go — startup-time registration of §8.1.* through §8.8.*
// payload types into the global event registry per EV-032 / EV-034.
//
// Spec ref: specs/event-model.md §6.3 EV-032, §4.9 EV-034.
//
// Each helper function called from init() registers one section's worth of §8
// event types so the global registry is populated before any event is emitted
// (EV-034). The registry is sealed at bus-seal time per EV-009.
//
// Tags: mechanism
// Durability classes per §8 table: F = fsync-boundary, O = ordinary, L = lossy.
//
// §8.1 Run lifecycle event registrations are in registerRunLifecycle().
// §8.2 Control-point lifecycle event registrations are in registerControlPoints().
// §8.3 Agent/handler lifecycle event registrations are in registerAgentEvents().
// §8.4 Budget lifecycle event registrations are in registerBudgetEvents().
// §8.5 Workspace lifecycle event registrations are in registerWorkspaceEvents().
// §8.6 Reconciliation lifecycle event registrations are in registerReconciliationEvents().
// §8.7 Daemon/operator lifecycle event registrations are in registerDaemonLifecycleEvents().
// §8.8 Bus/observability event registrations are in registerBusEvents().
//
// Bead refs: hk-hqwn.59.1 through hk-hqwn.59.78.

func init() {
	registerRunLifecycle()
	registerControlPoints()
	registerAgentEvents()
	registerBudgetEvents()
	registerWorkspaceEvents()
	registerReconciliationEvents()
	registerDaemonLifecycleEvents()
	registerBusEvents()
	registerReviewLoopEvents()
}

// registerRunLifecycle registers all §8.1 run-lifecycle event payload constructors.
//
// Durability classes per §8.1 table:
//   - run_started (§8.1.1):             F (fsync-boundary per beads-integration.md §4.3 BI-009)
//   - run_completed (§8.1.2):           F (terminal-state fsync per BI-010)
//   - run_failed (§8.1.3):              F (terminal-state fsync per BI-010)
//   - state_entered (§8.1.4):           O (ordinary — observability stream)
//   - state_exited (§8.1.5):            O (ordinary — observability stream)
//   - transition_event (§8.1.6):        F (fsync-boundary per checkpoint write)
//   - checkpoint_written (§8.1.7):      F (fsync-boundary per checkpoint write)
//   - outcome_emitted (§8.1.8):         O (ordinary — handler-to-daemon pipe)
//   - sub_workflow_entered (§8.1.9):    O (ordinary — observability stream)
//   - sub_workflow_exited (§8.1.10):    O (ordinary — observability stream)
//   - node_dispatch_requested (§8.1.11): O (ordinary — observability stream)
func registerRunLifecycle() {
	mustRegister("run_started", func() EventPayload { return &RunStartedPayload{} })
	mustRegister("run_completed", func() EventPayload { return &RunCompletedPayload{} })
	mustRegister("run_failed", func() EventPayload { return &RunFailedPayload{} })
	mustRegister("state_entered", func() EventPayload { return &StateEnteredPayload{} })
	mustRegister("state_exited", func() EventPayload { return &StateExitedPayload{} })
	mustRegister("transition_event", func() EventPayload { return &TransitionEventPayload{} })
	mustRegister("checkpoint_written", func() EventPayload { return &CheckpointWrittenPayload{} })
	mustRegister("outcome_emitted", func() EventPayload { return &OutcomeEmittedPayload{} })
	mustRegister("sub_workflow_entered", func() EventPayload { return &SubWorkflowEnteredPayload{} })
	mustRegister("sub_workflow_exited", func() EventPayload { return &SubWorkflowExitedPayload{} })
	mustRegister("node_dispatch_requested", func() EventPayload { return &NodeDispatchRequestedPayload{} })
}

// registerControlPoints registers all §8.2 control-point-lifecycle event payload constructors.
//
// Durability classes per §8.2 table:
//   - hook_fired (§8.2.1):                          O (ordinary — observability)
//   - hook_failed (§8.2.2):                         O (ordinary — observability)
//   - hook_verdict_persisted (§8.2.3):              O (ordinary — observability)
//   - gate_allowed (§8.2.4):                        O (ordinary — observability)
//   - gate_denied (§8.2.5):                         O (ordinary — observability)
//   - gate_escalated (§8.2.6):                      O (ordinary — observability)
//   - guard_reordered (§8.2.7):                     O (ordinary — observability)
//   - guard_failed (§8.2.8):                        O (ordinary — observability)
//   - control_points_registered (§8.2.9):           O (ordinary — observability)
//   - control_points_registration_started (§8.2.10): O (ordinary — observability)
//   - verdict_envelope_mismatch (§8.2.11):          O (ordinary — reconciliation input)
//   - policy_expression_exceeded_cost (§8.2.12):    F (fsync-boundary per CP-034b durability-pair)
func registerControlPoints() {
	mustRegister("hook_fired", func() EventPayload { return &HookFiredPayload{} })
	mustRegister("hook_failed", func() EventPayload { return &HookFailedPayload{} })
	mustRegister("hook_verdict_persisted", func() EventPayload { return &HookVerdictPersistedPayload{} })
	mustRegister("gate_allowed", func() EventPayload { return &GateAllowedPayload{} })
	mustRegister("gate_denied", func() EventPayload { return &GateDeniedPayload{} })
	mustRegister("gate_escalated", func() EventPayload { return &GateEscalatedPayload{} })
	mustRegister("guard_reordered", func() EventPayload { return &GuardReorderedPayload{} })
	mustRegister("guard_failed", func() EventPayload { return &GuardFailedPayload{} })
	mustRegister("control_points_registered", func() EventPayload { return &ControlPointsRegisteredPayload{} })
	mustRegister("control_points_registration_started", func() EventPayload { return &ControlPointsRegistrationStartedPayload{} })
	mustRegister("verdict_envelope_mismatch", func() EventPayload { return &VerdictEnvelopeMismatchPayload{} })
	mustRegister("policy_expression_exceeded_cost", func() EventPayload { return &PolicyExpressionExceededCostPayload{} })
}

// registerAgentEvents registers all §8.3 agent/handler-lifecycle event payload constructors.
//
// Durability classes per §8.3 table:
//   - agent_started (§8.3.2):            O (ordinary — handler lifecycle audit and observability)
//   - agent_ready (§8.3.1):              O (ordinary — handler lifecycle observability)
//   - agent_output_chunk (§8.3.3):       L (lossy-tail-ok — per-chunk statistical aggregate)
//   - agent_failed (§8.3.5):             O (ordinary — handler lifecycle observability)
//   - agent_rate_limit_status (§8.3.6):  O (ordinary — rate-limit lifecycle observability)
//   - session_log_location (§8.3.7):     O (ordinary — session-log-pipeline audit)
//   - skills_provisioned (§8.3.8):       O (ordinary — skill-injection audit and observability)
//   - handler_capabilities (§8.3.9):     O (ordinary — version-negotiation observability)
//
// Note: §8.3.4 (agent_completed), §8.3.10 (agent_warning_silent_hang),
// §8.3.11 (agent_resumed_after_warning), §8.3.12 (agent_soft_terminating), and
// §8.3.13 (agent_hard_terminating) are registered by other implementer waves (future waves).
func registerAgentEvents() {
	mustRegister("agent_started", func() EventPayload { return &AgentStartedPayload{} })
	mustRegister("agent_ready", func() EventPayload { return &AgentReadyPayload{} })
	mustRegister("agent_output_chunk", func() EventPayload { return &AgentOutputChunkPayload{} })
	mustRegister("agent_failed", func() EventPayload { return &AgentFailedPayload{} })
	mustRegister("agent_rate_limit_status", func() EventPayload { return &AgentRateLimitStatusPayload{} })
	mustRegister("session_log_location", func() EventPayload { return &SessionLogLocationPayload{} })
	mustRegister("skills_provisioned", func() EventPayload { return &SkillsProvisionedPayload{} })
	mustRegister("handler_capabilities", func() EventPayload { return &HandlerCapabilitiesPayload{} })
	mustRegister("launch_initiated", func() EventPayload { return &LaunchInitiatedPayload{} })
}

// registerBudgetEvents registers all §8.4 budget-lifecycle event payload constructors.
//
// Durability classes per §8.4 table:
//   - budget_warning (§8.4.1):    O (ordinary — budget observability)
//   - budget_accrual (§8.4.2):    L (lossy-tail-ok — per-chunk accrual)
//   - budget_exhausted (§8.4.3):  O (ordinary — budget lifecycle)
func registerBudgetEvents() {
	mustRegister("budget_warning", func() EventPayload { return &BudgetWarningPayload{} })
	mustRegister("budget_accrual", func() EventPayload { return &BudgetAccrualPayload{} })
	mustRegister("budget_exhausted", func() EventPayload { return &BudgetExhaustedEventPayload{} })
}

// registerWorkspaceEvents registers all §8.5 workspace-lifecycle event payload constructors.
//
// Durability classes per §8.5 table:
//   - workspace_created (§8.5.1):         O (ordinary — workspace lifecycle observability)
//   - workspace_leased (§8.5.2):          O (ordinary — workspace lifecycle observability)
//   - workspace_merge_status (§8.5.3):    F (fsync-boundary — merge authority per workspace-model.md §4.5)
//   - workspace_discarded (§8.5.4):       O (ordinary — workspace lifecycle observability)
//   - workspace_interrupted (§8.5.5):     O (ordinary — reconciliation and audit input)
//   - merge_conflict_escalation (§8.5.6): O (ordinary — operator-observability and audit)
func registerWorkspaceEvents() {
	mustRegister("workspace_created", func() EventPayload { return &WorkspaceCreatedPayload{} })
	mustRegister("workspace_leased", func() EventPayload { return &WorkspaceLeasedPayload{} })
	mustRegister("workspace_merge_status", func() EventPayload { return &WorkspaceMergeStatusPayload{} })
	mustRegister("workspace_discarded", func() EventPayload { return &WorkspaceDiscardedPayload{} })
	mustRegister("workspace_interrupted", func() EventPayload { return &WorkspaceInterruptedPayload{} })
	mustRegister("merge_conflict_escalation", func() EventPayload { return &MergeConflictEscalationPayload{} })
}

// registerReconciliationEvents registers all §8.6 reconciliation-lifecycle event payload constructors.
//
// Durability classes per §8.6 table (all class O — ordinary):
//   - reconciliation_started (§8.6.1)
//   - reconciliation_category_assigned (§8.6.2)
//   - reconciliation_verdict_emitted (§8.6.3)
//   - reconciliation_verdict_executed (§8.6.4)     — uses VerdictExecutedPayload
//   - reconciliation_verdict_malformed (§8.6.5)    — uses MalformedVerdictPayload
//   - reconciliation_budget_exhausted (§8.6.6)     — uses BudgetExhaustedPayload
//   - reconciliation_verdict_stale (§8.6.7)        — uses StaleVerdictPayload
//   - store_divergence_detected (§8.6.8)
//   - operator_escalation_required (§8.6.9)
//   - divergence_inconclusive (§8.6.10)
//   - reconciliation_dispatch_deduplicated (§8.6.11)
//   - reconciliation_detector_panic (§8.6.12)
//   - reconciliation_verdict_execution_retry (§8.6.13)
//   - bead_terminal_transition_recovered (§8.6.14) — post-MVH per OQ-BI-008; type reserved
func registerReconciliationEvents() {
	mustRegister("reconciliation_started", func() EventPayload { return &ReconciliationStartedPayload{} })
	mustRegister("reconciliation_category_assigned", func() EventPayload { return &ReconciliationCategoryAssignedPayload{} })
	mustRegister("reconciliation_verdict_emitted", func() EventPayload { return &ReconciliationVerdictEmittedPayload{} })
	mustRegister("reconciliation_verdict_executed", func() EventPayload { return &VerdictExecutedPayload{} })
	mustRegister("reconciliation_verdict_malformed", func() EventPayload { return &MalformedVerdictPayload{} })
	mustRegister("reconciliation_budget_exhausted", func() EventPayload { return &BudgetExhaustedPayload{} })
	mustRegister("reconciliation_verdict_stale", func() EventPayload { return &StaleVerdictPayload{} })
	mustRegister("store_divergence_detected", func() EventPayload { return &StoreDivergenceDetectedPayload{} })
	mustRegister("operator_escalation_required", func() EventPayload { return &OperatorEscalationRequiredPayload{} })
	mustRegister("divergence_inconclusive", func() EventPayload { return &DivergenceInconclusivePayload{} })
	mustRegister("reconciliation_dispatch_deduplicated", func() EventPayload { return &ReconciliationDispatchDeduplicatedPayload{} })
	mustRegister("reconciliation_detector_panic", func() EventPayload { return &ReconciliationDetectorPanicPayload{} })
	mustRegister("reconciliation_verdict_execution_retry", func() EventPayload { return &ReconciliationVerdictExecutionRetryPayload{} })
	mustRegister("bead_terminal_transition_recovered", func() EventPayload { return &BeadTerminalTransitionRecoveredPayload{} })
}

// registerDaemonLifecycleEvents registers all §8.7 operator-control and daemon
// lifecycle event payload constructors.
//
// Durability classes per §8.7 table:
//   - daemon_started (§8.7.1):                    F (fsync-boundary, startup landmark)
//   - daemon_ready (§8.7.2):                      F (fsync-boundary, RTO measurement endpoint)
//   - daemon_shutdown (§8.7.3):                   F (fsync-boundary, SIGTERM landmark for ON-033)
//   - daemon_startup_failed (§8.7.4):             F (fsync-boundary, operator-observability)
//   - daemon_degraded (§8.7.5):                   O (ordinary, operator-observability)
//   - operator_pause_status (§8.7.6):             O (ordinary, paired-phase lifecycle)
//   - operator_resuming (§8.7.7):                 O (ordinary)
//   - operator_stopped (§8.7.8):                  O (ordinary)
//   - operator_upgrading (§8.7.9):                O (ordinary)
//   - operator_upgrade_completed (§8.7.10):       F (fsync-boundary, version-boundary landmark)
//   - operator_upgrade_rejected (§8.7.11):        O (ordinary, operator-observability)
//   - operator_command_rejected (§8.7.12):        O (ordinary, operator-observability)
//   - dispatch_deferred (§8.7.13):                O (ordinary)
//   - daemon_orphan_sweep_completed (§8.7.14):    O (ordinary)
//   - infrastructure_unavailable (§8.7.15):       O (ordinary, operator-observability)
//   - operator_command_failed (§8.7.16):          O (ordinary, ON-013a panic-barrier emission)
//   - operator_escalation_cleared (§8.7.17):      O (ordinary, ON-emission-owned companion to §8.6.9)
func registerDaemonLifecycleEvents() {
	mustRegister("daemon_started", func() EventPayload { return &DaemonStartedPayload{} })
	mustRegister("daemon_ready", func() EventPayload { return &DaemonReadyPayload{} })
	mustRegister("daemon_shutdown", func() EventPayload { return &DaemonShutdownPayload{} })
	mustRegister("daemon_startup_failed", func() EventPayload { return &DaemonStartupFailedPayload{} })
	mustRegister("daemon_degraded", func() EventPayload { return &DaemonDegradedPayload{} })
	mustRegister("operator_pause_status", func() EventPayload { return &OperatorPauseStatusPayload{} })
	mustRegister("operator_resuming", func() EventPayload { return &OperatorResumingPayload{} })
	mustRegister("operator_stopped", func() EventPayload { return &OperatorStoppedPayload{} })
	mustRegister("operator_upgrading", func() EventPayload { return &OperatorUpgradingPayload{} })
	mustRegister("operator_upgrade_completed", func() EventPayload { return &OperatorUpgradeCompletedPayload{} })
	mustRegister("operator_upgrade_rejected", func() EventPayload { return &OperatorUpgradeRejectedPayload{} })
	mustRegister("operator_command_rejected", func() EventPayload { return &OperatorCommandRejectedPayload{} })
	mustRegister("dispatch_deferred", func() EventPayload { return &DispatchDeferredPayload{} })
	mustRegister("daemon_orphan_sweep_completed", func() EventPayload { return &DaemonOrphanSweepCompletedPayload{} })
	mustRegister("infrastructure_unavailable", func() EventPayload { return &InfrastructureUnavailablePayload{} })
	mustRegister("operator_command_failed", func() EventPayload { return &OperatorCommandFailedPayload{} })
	mustRegister("operator_escalation_cleared", func() EventPayload { return &OperatorEscalationClearedPayload{} })
}

// registerBusEvents registers all §8.8 observability and bus-internal event
// payload constructors.
//
// Durability classes per §8.8 table:
//   - metric (§8.8.1):               L (lossy-tail-ok, §8.9(g) escape-hatch exception)
//   - consumer_failed (§8.8.2):      O (ordinary, bus-internal; hk-hqwn.59.75)
//   - dead_letter_enqueued (§8.8.3): O (ordinary, bus-internal)
//   - bus_overflow (§8.8.4):         O (ordinary; promoted to F via direct-JSONL-append
//     fallback when reservation slot is exhausted per EV-011a)
//   - redaction_failed (§8.8.5):     O (ordinary, bus-internal; ON-022 fail-closed redactor)
func registerBusEvents() {
	mustRegister("metric", func() EventPayload { return &MetricPayload{} })
	mustRegister("consumer_failed", func() EventPayload { return &ConsumerFailedPayload{} })
	mustRegister("dead_letter_enqueued", func() EventPayload { return &DeadLetterEnqueuedPayload{} })
	mustRegister("bus_overflow", func() EventPayload { return &BusOverflowPayload{} })
	mustRegister("redaction_failed", func() EventPayload { return &RedactionFailedPayload{} })
}

// registerReviewLoopEvents registers all §8.1a review-loop cycle and §8.8.6
// event payload constructors (hk-7om2q.4).
//
// Durability classes per §8.1a and §8.8.6 tables:
//   - implementer_resumed        (§8.1a.1): O (ordinary — orchestrator-core lifecycle)
//   - reviewer_launched          (§8.1a.2): O (ordinary — orchestrator-core lifecycle)
//   - reviewer_verdict           (§8.1a.3): F (fsync-boundary — verdict gates terminal routing)
//   - iteration_cap_hit          (§8.1a.4): O (ordinary — deliberately downgraded; see §8.1a Note)
//   - no_progress_detected       (§8.1a.5): O (ordinary — improvement-loop early-exit signal)
//   - review_loop_cycle_complete (§8.1a.6): F (fsync-boundary — terminal routing landmark)
//   - bead_label_conflict        (§8.8.6):  O (ordinary — claim-path observational evidence)
func registerReviewLoopEvents() {
	mustRegister("implementer_resumed", func() EventPayload { return &ImplementerResumedPayload{} })
	mustRegister("reviewer_launched", func() EventPayload { return &ReviewerLaunchedPayload{} })
	mustRegister("reviewer_verdict", func() EventPayload { return &ReviewerVerdictPayload{} })
	mustRegister("iteration_cap_hit", func() EventPayload { return &IterationCapHitPayload{} })
	mustRegister("no_progress_detected", func() EventPayload { return &NoProgressDetectedPayload{} })
	mustRegister("review_loop_cycle_complete", func() EventPayload { return &ReviewLoopCycleCompletePayload{} })
	mustRegister("bead_label_conflict", func() EventPayload { return &BeadLabelConflictPayload{} })
}

// mustRegister calls RegisterEventType and panics on error.
//
// init() functions that call mustRegister run before the first event is emitted
// (EV-034); a registration error is a programming error (duplicate type name or
// nil constructor) that MUST be caught at startup, not silently ignored. Panic
// is acceptable here because init() runs before any request is served.
//
// This helper is intentionally unexported and limited to init() callers; it
// MUST NOT be called after startup completes.
func mustRegister(typeName string, ctor func() EventPayload) {
	if err := RegisterEventType(typeName, ctor); err != nil {
		panic("core: mustRegister: " + typeName + ": " + err.Error())
	}
}
