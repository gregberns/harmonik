package core

// eventreg_hqwn59.go — startup-time registration of §8.1.* through §8.10.*
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
// §8.1  Run lifecycle event registrations are in registerRunLifecycle().
// §8.1a Run lifecycle registrations also include bead_closed and working_tree_refresh_failed.
// §8.2  Control-point lifecycle event registrations are in registerControlPoints().
// §8.3  Agent/handler lifecycle event registrations are in registerAgentEvents().
// §8.4  Budget lifecycle event registrations are in registerBudgetEvents().
// §8.5  Workspace lifecycle event registrations are in registerWorkspaceEvents().
// §8.6  Reconciliation lifecycle event registrations are in registerReconciliationEvents().
// §8.7  Daemon/operator lifecycle event registrations are in registerDaemonLifecycleEvents().
// §8.8  Bus/observability event registrations are in registerBusEvents().
// §8.10 Queue lifecycle event registrations are in registerQueueEvents().
//
// Bead refs: hk-hqwn.59.1 through hk-hqwn.59.78, hk-yslws, hk-gjyks.

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
	registerQueueEvents()
	registerHandlerPauseEvents()
	registerGateDispatchEvents()
	registerWorkflowLoaderEvents()
	registerKeeperEvents()
	registerAlarmEvents()
	registerHITLDecisionEvents()
}

// registerRunLifecycle registers all §8.1 run-lifecycle event payload constructors,
// plus the two §4.12 merge-to-main adjacents (bead_closed and working_tree_refresh_failed)
// that share the run-lifecycle emission sequence per execution-model.md §4.12 EM-052/EM-054.
//
// Durability classes per §8.1 table:
//   - run_started (§8.1.1):                    F (fsync-boundary per beads-integration.md §4.3 BI-009)
//   - run_completed (§8.1.2):                  F (terminal-state fsync per BI-010)
//   - run_failed (§8.1.3):                     F (terminal-state fsync per BI-010)
//   - state_entered (§8.1.4):                  O (ordinary — observability stream)
//   - state_exited (§8.1.5):                   O (ordinary — observability stream)
//   - transition_event (§8.1.6):               F (fsync-boundary per checkpoint write)
//   - checkpoint_written (§8.1.7):             F (fsync-boundary per checkpoint write)
//   - outcome_emitted (§8.1.8):                O (ordinary — handler-to-daemon pipe)
//   - sub_workflow_entered (§8.1.9):           O (ordinary — observability stream)
//   - sub_workflow_exited (§8.1.10):           O (ordinary — observability stream)
//   - node_dispatch_requested (§8.1.11):       O (ordinary — observability stream)
//   - bead_closed (EM-052 §4.12.6):            F (fsync-boundary — bead closure terminal-state landmark)
//   - epic_completed (§8.13 hk-w6y70):         O (ordinary — observational; at-most-once per epic)
//   - working_tree_refresh_failed (EM-054):    O (ordinary — informational; merge already durable)
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
	// node_dispatch_decided: emitted by the DOT-mode cascade engine after EM-041
	// edge selection resolves the next node (or determines terminal state / failure).
	// Durability class: O. Bead ref: hk-bf85t (T-IMPL-008).
	mustRegister("node_dispatch_decided", func() EventPayload { return &NodeDispatchDecidedPayload{} })
	mustRegister("bead_closed", func() EventPayload { return &BeadClosedPayload{} })
	// epic_completed (hk-w6y70): emitted at most once per parent epic after the
	// last child closes. Durability class: O (ordinary — observational).
	mustRegister("epic_completed", func() EventPayload { return &EpicCompletedPayload{} })
	mustRegister("working_tree_refresh_failed", func() EventPayload { return &WorkingTreeRefreshFailedPayload{} })
	// implementer_escaped_worktree (hk-6zylj): emitted by the daemon workloop
	// when, after the implementer exits, the MAIN repo's working tree contains
	// dirty files outside the .harmonik/.claude/.beads churn allowlist —
	// indicating implementer cross-contamination. Durability class: F.
	mustRegister("implementer_escaped_worktree", func() EventPayload { return &ImplementerEscapedWorktreePayload{} })
	// implementer_phase_complete (hk-cd8yu): emitted immediately after the
	// implementer session ends (normal exit, noChange-timeout kill, or context
	// cancellation) and before any reviewer phase begins. Closes the diagnostic
	// gap between run_started and reviewer_launched. Durability class: F.
	mustRegister("implementer_phase_complete", func() EventPayload { return &ImplementerPhaseCompletePayload{} })
	// merge_build_failed (hk-o68j3): emitted when go build+vet fails on the
	// freshly fast-forwarded merged tree inside lockedMergeRunBranchToMain,
	// before the push. update-ref is rolled back; caller reopens the bead.
	// Durability class: F.
	mustRegister("merge_build_failed", func() EventPayload { return &MergeBuildFailedPayload{} })
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

// registerAgentEvents registers all §8.3 agent/handler-lifecycle event payload constructors,
// plus the agent-comms typed events (agent-comms spec §1, hk-djqc9).
//
// Durability classes per §8.3 table:
//   - agent_ready (§8.3.1):                 O (ordinary — handler lifecycle observability)
//   - agent_started (§8.3.2):               O (ordinary — handler lifecycle audit and observability)
//   - agent_output_chunk (§8.3.3):          L (lossy-tail-ok — per-chunk statistical aggregate)
//   - agent_completed (§8.3.4):             O (ordinary — handler lifecycle observability)
//   - agent_failed (§8.3.5):                O (ordinary — handler lifecycle observability)
//   - agent_rate_limit_status (§8.3.6):     O (ordinary — rate-limit lifecycle observability)
//   - session_log_location (§8.3.7):        O (ordinary — session-log-pipeline audit)
//   - skills_provisioned (§8.3.8):          O (ordinary — skill-injection audit and observability)
//   - handler_capabilities (§8.3.9):        O (ordinary — version-negotiation observability)
//   - agent_warning_silent_hang (§8.3.10):  O (ordinary — silent-hang detection)
//   - agent_resumed_after_warning (§8.3.11): O (ordinary — hang recovery observability)
//   - agent_soft_terminating (§8.3.12):     O (ordinary — termination lifecycle audit)
//   - agent_hard_terminating (§8.3.13):     O (ordinary — termination lifecycle audit)
//   - agent_heartbeat (HC-026a):            O (ordinary — silent-hang timer reset)
//   - launch_initiated:                     O (ordinary — pre-exec lifecycle observability)
//   - agent_message (agent-comms §1.1):     F (fsync-boundary — durable directed/broadcast messaging; no silent drops G2)
//   - agent_presence (agent-comms §1.2):    O (ordinary — presence beat; TTL projection handles crash gaps)
func registerAgentEvents() {
	mustRegister("agent_started", func() EventPayload { return &AgentStartedPayload{} })
	mustRegister("agent_ready", func() EventPayload { return &AgentReadyPayload{} })
	mustRegister("agent_output_chunk", func() EventPayload { return &AgentOutputChunkPayload{} })
	mustRegister("agent_completed", func() EventPayload { return &AgentCompletedPayload{} })
	mustRegister("agent_failed", func() EventPayload { return &AgentFailedPayload{} })
	mustRegister("agent_heartbeat", func() EventPayload { return &AgentHeartbeatPayload{} })
	mustRegister("agent_rate_limit_status", func() EventPayload { return &AgentRateLimitStatusPayload{} })
	mustRegister("session_log_location", func() EventPayload { return &SessionLogLocationPayload{} })
	mustRegister("skills_provisioned", func() EventPayload { return &SkillsProvisionedPayload{} })
	mustRegister("handler_capabilities", func() EventPayload { return &HandlerCapabilitiesPayload{} })
	mustRegister("agent_warning_silent_hang", func() EventPayload { return &AgentWarningSilentHangPayload{} })
	mustRegister("agent_resumed_after_warning", func() EventPayload { return &AgentResumedAfterWarningPayload{} })
	mustRegister("agent_soft_terminating", func() EventPayload { return &AgentSoftTerminatingPayload{} })
	mustRegister("agent_hard_terminating", func() EventPayload { return &AgentHardTerminatingPayload{} })
	mustRegister("launch_initiated", func() EventPayload { return &LaunchInitiatedPayload{} })
	// agent_ready_timeout (hk-5cox8): emitted by the daemon workloop when HC-056
	// fires — no agent_ready arrived within the timeout window. Durability class: O.
	mustRegister("agent_ready_timeout", func() EventPayload { return &AgentReadyTimeoutPayload{} })
	// post_agent_ready_hang (hk-a2okh): emitted when an implementer becomes ready
	// but makes no observable progress within the hang-detection timeout. Durability class: O.
	mustRegister("post_agent_ready_hang", func() EventPayload { return &PostAgentReadyHangPayload{} })
	// lifecycle_transition (§8.3.14, hk-xrygh): emitted by the watcher and
	// workloop on every LifecycleState machine transition per HC-064..HC-067.
	// Durability class: O.
	mustRegister("lifecycle_transition", func() EventPayload { return &LifecycleTransitionPayload{} })
	// pasteinject_failed (hk-fra5l): emitted by the daemon when paste-inject
	// cannot deliver the kick-off message to the tmux pane. Durability class: O.
	mustRegister("pasteinject_failed", func() EventPayload { return &PasteInjectFailedPayload{} })
	// launch_stall_detected (hk-fra5l): emitted by the stale watcher when
	// run_started fires but launch_initiated is absent for >30 s. Durability class: O.
	mustRegister("launch_stall_detected", func() EventPayload { return &LaunchStallDetectedPayload{} })
	// spawn_cap_blocked (hk-4l7zs): emitted by the daemon when SpawnWindow cannot
	// acquire a spawn-semaphore slot within the bounded acquire timeout — the
	// observable signature of a slot leak. Durability class: O.
	mustRegister("spawn_cap_blocked", func() EventPayload { return &SpawnCapBlockedPayload{} })
	// implementer_budget_exceeded (hk-9vp51): emitted by pasteInjectQuitOnCommit
	// when an implementer session is force-killed for exhausting its commit
	// budget (hard ceiling reached, or progress went stale). Durability class: O.
	mustRegister("implementer_budget_exceeded", func() EventPayload { return &ImplementerBudgetExceededPayload{} })
	// reviewer_budget_exceeded (hk-da3rr): emitted by the builtin review-loop
	// and the DOT reviewer-node path when pasteInjectQuitOnReviewFile
	// force-kills a reviewer session that exhausted its diff-scaled verdict
	// budget. Durability class: O.
	mustRegister("reviewer_budget_exceeded", func() EventPayload { return &ReviewerBudgetExceededPayload{} })
	// tmux_new_window_timeout (hk-r1rup): emitted by the daemon when
	// tmuxSubstrate.SpawnWindow's underlying `tmux new-window` shell call hangs
	// past the bounded new-window timeout — the observable signature of a hung
	// tmux invocation (the no-spawn wedge). Durability class: O.
	mustRegister("tmux_new_window_timeout", func() EventPayload { return &TmuxNewWindowTimeoutPayload{} })
	// codex_billing_guard (hk-tu48u, C3/T11): emitted by the codex launch path's
	// positive billing guard at each observable step (materialize
	// forced_login_method=chatgpt, pre-flight assert allowed, pre-flight assert
	// denied = fail-closed). Durability class: O.
	mustRegister("codex_billing_guard", func() EventPayload { return &CodexBillingGuardPayload{} })
	// agent_message (hk-djqc9, agent-comms spec §1.1): directed/broadcast message
	// between agents. Durability class: F (fsync-boundary — durable delivery G2).
	mustRegister("agent_message", func() EventPayload { return &AgentMessagePayload{} })
	// agent_presence (hk-djqc9, agent-comms spec §1.2): join/refresh/leave presence
	// beat. Durability class: O (ordinary — TTL projection reconciles crash gaps).
	mustRegister("agent_presence", func() EventPayload { return &AgentPresencePayload{} })
	// harness_selected (hk-lr5t): emitted by resolveHarness at dispatch time to
	// record which harness (agent_type) was chosen and which tier resolved it.
	// Closes the observability gap where silent claude-code fallback was invisible.
	// Durability class: O.
	mustRegister("harness_selected", func() EventPayload { return &HarnessSelectedPayload{} })
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
//   - reconciliation_completed (hk-mptxw)
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
	mustRegister("reconciliation_completed", func() EventPayload { return &ReconciliationCompletedPayload{} })
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
	mustRegister("reconciliation_mismatch_observed", func() EventPayload { return &ReconciliationMismatchObservedPayload{} })
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
//   - daemon_config (§8.7.18):                    O (ordinary, resolved-config audit)
//   - disk_low (§8.7.19, hk-sxlb):               O (ordinary, disk-watermark self-healing signal)
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
	mustRegister("daemon_config", func() EventPayload { return &DaemonConfigPayload{} })
	// disk_low (§8.7.19, hk-sxlb): emitted when available disk falls below the
	// configured watermark; daemon pauses dispatch and attempts go clean -cache.
	mustRegister("disk_low", func() EventPayload { return &DiskLowPayload{} })
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
	// bead_claim_skipped (BI-013c): emitted by the pre-claim status re-read guard
	// when the bead's status is not open between dispatcher selection and claim write.
	// Durability class: O.
	mustRegister("bead_claim_skipped", func() EventPayload { return &BeadClaimSkippedPayload{} })
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
//   - review_bypassed            (hk-81n9r): O (ordinary — audit event when workflow:single gates single mode)
func registerReviewLoopEvents() {
	mustRegister("implementer_resumed", func() EventPayload { return &ImplementerResumedPayload{} })
	mustRegister("reviewer_launched", func() EventPayload { return &ReviewerLaunchedPayload{} })
	mustRegister("reviewer_verdict", func() EventPayload { return &ReviewerVerdictPayload{} })
	mustRegister("iteration_cap_hit", func() EventPayload { return &IterationCapHitPayload{} })
	mustRegister("no_progress_detected", func() EventPayload { return &NoProgressDetectedPayload{} })
	mustRegister("review_loop_cycle_complete", func() EventPayload { return &ReviewLoopCycleCompletePayload{} })
	mustRegister("bead_label_conflict", func() EventPayload { return &BeadLabelConflictPayload{} })
	// review_bypassed (hk-81n9r): emitted when a bead's workflow:single label
	// resolves at tier-1, gating single-mode dispatch behind an observable audit event.
	// Durability class: O.
	mustRegister("review_bypassed", func() EventPayload { return &ReviewBypassedPayload{} })
	// review_fixup_stalled (hk-m1wqp): emitted when a REQUEST_CHANGES fix-up run
	// advances HEAD by zero commits; carries the reviewer flags from the prior
	// REQUEST_CHANGES verdict so triage sees the specific flag the implementer
	// failed to address. Durability class: O.
	mustRegister("review_fixup_stalled", func() EventPayload { return &ReviewFixupStalledPayload{} })
}

// registerQueueEvents registers all §8.10 queue lifecycle event payload
// constructors (extqueue v0.1, hk-yslws).
//
// Durability classes per §8.10 table:
//   - queue_submitted                    (§8.10.1): F (fsync-boundary — loss orphans the execution plan per EV-016)
//   - queue_group_started                (§8.10.2): O (ordinary — reconstructible from predecessor queue_group_completed + queue.json)
//   - queue_group_completed              (§8.10.3): F (fsync-boundary — group-boundary advance landmark per EV-016)
//   - queue_paused                       (§8.10.4): F (fsync-boundary — hard execution stop landmark per EV-016)
//   - queue_appended                     (§8.10.5): O (ordinary — reconstructible from queue.json mutation history)
//   - queue_item_deferred_for_ledger_dep (§8.10.6): O (ordinary — reconstructible from ledger state + queue.json)
//   - queue_item_reconciled              (§8.10.7): F (fsync-boundary — correction MUST be durable before re-dispatch per §8.10.7)
func registerQueueEvents() {
	mustRegister("queue_submitted", func() EventPayload { return &QueueSubmittedPayload{} })
	mustRegister("queue_group_started", func() EventPayload { return &QueueGroupStartedPayload{} })
	mustRegister("queue_group_completed", func() EventPayload { return &QueueGroupCompletedPayload{} })
	mustRegister("queue_paused", func() EventPayload { return &QueuePausedPayload{} })
	mustRegister("queue_appended", func() EventPayload { return &QueueAppendedPayload{} })
	mustRegister("queue_item_deferred_for_ledger_dep", func() EventPayload { return &QueueItemDeferredForLedgerDepPayload{} })
	mustRegister("queue_item_reconciled", func() EventPayload { return &QueueItemReconciledPayload{} })
}

// registerHandlerPauseEvents registers all §8.11 handler-pause lifecycle event
// payload constructors (handler-pause MVH, hk-ifqnj).
//
// Durability classes per §8.11 table:
//   - handler_paused                    (§8.11.1): F (fsync-boundary — pause-state landmark for restart recovery)
//   - handler_resumed                   (§8.11.2): F (fsync-boundary — resume action durable before dispatcher proceeds)
//   - queue_item_held_for_handler_pause (§8.11.3): O (ordinary — reconstructible from handler-state.json + queue.json)
func registerHandlerPauseEvents() {
	mustRegister("handler_paused", func() EventPayload { return &HandlerPausedPayload{} })
	mustRegister("handler_resumed", func() EventPayload { return &HandlerResumedPayload{} })
	mustRegister("queue_item_held_for_handler_pause", func() EventPayload { return &QueueItemHeldForHandlerPausePayload{} })
}

// registerGateDispatchEvents registers the §8.2a gate-node dispatch event
// payload constructors (hk-jtxnr, T-IMPL-010).
//
// Durability classes per §8.2a:
//   - gate_decision_recorded: O (ordinary — observability and audit)
func registerGateDispatchEvents() {
	mustRegister("gate_decision_recorded", func() EventPayload { return &GateDecisionRecordedPayload{} })
}

// registerWorkflowLoaderEvents registers the workflow-loader event payload
// constructors (hk-zqr6f, CP-057 skills_ref resolution).
func registerWorkflowLoaderEvents() {
	mustRegister("skills_resolved", func() EventPayload { return &SkillsResolvedPayload{} })
}

// registerKeeperEvents registers §8.13 session-keeper event payload constructors
// (codename:session-keeper, hk-ekap1; beads hk-8vzek, hk-22i70, hk-kct9t, hk-aalsm).
//
// Durability classes per §8.13:
//   - session_keeper_warn                (§8.13.1): O (ordinary — observability)
//   - session_keeper_no_gauge            (§8.13.2): O (ordinary — configuration-gap signal)
//   - session_keeper_handoff_started     (§8.13.3): O (ordinary — observability)
//   - session_keeper_cycle_complete      (§8.13.4): O (ordinary — observability)
//   - session_keeper_cycle_aborted       (§8.13.5): O (ordinary — operator attention)
//   - session_keeper_clear_unconfirmed   (§8.13.6): O (ordinary — observability)
//   - session_keeper_cycle_recovered     (§8.13.7): O (ordinary — observability)
//   - session_keeper_precompact_blocked  (§8.13.8): O (ordinary — observability)
func registerKeeperEvents() {
	mustRegister("session_keeper_warn", func() EventPayload { return &SessionKeeperWarnPayload{} })
	mustRegister("session_keeper_no_gauge", func() EventPayload { return &SessionKeeperNoGaugePayload{} })
	mustRegister("session_keeper_handoff_started", func() EventPayload { return &SessionKeeperHandoffStartedPayload{} })
	mustRegister("session_keeper_cycle_complete", func() EventPayload { return &SessionKeeperCycleCompletePayload{} })
	mustRegister("session_keeper_cycle_aborted", func() EventPayload { return &SessionKeeperCycleAbortedPayload{} })
	mustRegister("session_keeper_clear_unconfirmed", func() EventPayload { return &SessionKeeperClearUnconfirmedPayload{} })
	mustRegister("session_keeper_cycle_recovered", func() EventPayload { return &SessionKeeperCycleRecoveredPayload{} })
	// hk-aalsm: PreCompact backstop hook.
	mustRegister("session_keeper_precompact_blocked", func() EventPayload { return &SessionKeeperPrecompactBlockedPayload{} })
	// hk-3w2: supervised respawn path.
	mustRegister("session_keeper_respawn_attempted", func() EventPayload { return &SessionKeeperRespawnAttemptedPayload{} })
	// hk-6qf: operator-attached guard (warn-only suppression).
	mustRegister("session_keeper_operator_attached", func() EventPayload { return &SessionKeeperOperatorAttachedPayload{} })
	// hk-wjzf, ON-059: captain-initiated restart-now gate/freshness suppression.
	mustRegister("session_keeper_restart_now_blocked", func() EventPayload { return &SessionKeeperRestartNowBlockedPayload{} })
	// hk-34ac: blind-keeper alarm (continuous foreign_session > 5 min).
	mustRegister("session_keeper_blind", func() EventPayload { return &SessionKeeperBlindPayload{} })
	// hk-34ac: SID-independent hard-ceiling failsafe (tokens >= 280K).
	mustRegister("session_keeper_hard_ceiling", func() EventPayload { return &SessionKeeperHardCeilingPayload{} })
	// hk-ee81: idle crew below idle-restart floor (advisory to captain).
	mustRegister("session_keeper_idle_crew", func() EventPayload { return &SessionKeeperIdleCrewPayload{} })
}

// registerAlarmEvents registers §8.14 alarm / self-check event payload
// constructors (hk-tnmjy).
//
// Durability classes per §8.14:
//   - review_gate_anomaly (§8.14.1): O (ordinary — observability alarm; reconstructible
//     from bead_closed + reviewer_verdict sequence in the JSONL log)
func registerAlarmEvents() {
	mustRegister("review_gate_anomaly", func() EventPayload { return &ReviewGateAnomalyPayload{} })
}

// registerHITLDecisionEvents registers the §8.15 hitl-decisions event payload
// constructors (codename:hitl-decisions, hk-33p, component K1).
//
// These are the agent→human decision dual of agent-comms. All three are F-class
// (fsync-boundary — added to eventbus.fsyncBoundaryEventTypes per SPEC §6 N1):
// a lost decision_resolved would leave the blocked agent waiting forever
// (Risk R1, load-bearing).
//
// Durability classes per hitl-decisions SPEC §1 / §6 N1:
//   - decision_needed (§1.1):    F (fsync-boundary — durable decision-request landmark)
//   - decision_resolved (§1.2):  F (fsync-boundary — a lost answer never wakes the agent)
//   - decision_withdrawn (§1.3): F (fsync-boundary — a lost withdrawal leaves a stale open decision)
//
// Distinct from the §8.12 decision_required / decision_acknowledged
// daemon-escalation family.
func registerHITLDecisionEvents() {
	mustRegister("decision_needed", func() EventPayload { return &DecisionNeededPayload{} })
	mustRegister("decision_resolved", func() EventPayload { return &DecisionResolvedPayload{} })
	mustRegister("decision_withdrawn", func() EventPayload { return &DecisionWithdrawnPayload{} })
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
