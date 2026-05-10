package core

// eventreg_hqwn59.go — startup-time registration of §8.1.* through §8.6.*
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
//
// Bead refs: hk-hqwn.59.1 through hk-hqwn.59.56.

func init() {
	registerRunLifecycle()
	registerControlPoints()
	registerAgentEvents()
	registerBudgetEvents()
	registerWorkspaceEvents()
	registerReconciliationEvents()
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
//   - agent_ready (§8.3.1):             O (ordinary — handler lifecycle observability)
//   - agent_output_chunk (§8.3.3):      L (lossy-tail-ok — per-chunk statistical aggregate)
//   - agent_failed (§8.3.5):            O (ordinary — handler lifecycle observability)
//   - handler_capabilities (§8.3.9):    O (ordinary — version-negotiation observability)
//
// Note: §8.3.2 (agent_started), §8.3.4 (agent_completed), §8.3.6 (agent_rate_limit_status),
// §8.3.7 (session_log_location), §8.3.8 (skills_provisioned), §8.3.10 (agent_warning_silent_hang),
// §8.3.11 (agent_resumed_after_warning), §8.3.12 (agent_soft_terminating), and §8.3.13
// (agent_hard_terminating) are registered by other implementer waves (hqwn59a and future waves).
func registerAgentEvents() {
	mustRegister("agent_ready", func() EventPayload { return &AgentReadyPayload{} })
	mustRegister("agent_output_chunk", func() EventPayload { return &AgentOutputChunkPayload{} })
	mustRegister("agent_failed", func() EventPayload { return &AgentFailedPayload{} })
	mustRegister("handler_capabilities", func() EventPayload { return &HandlerCapabilitiesPayload{} })
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
