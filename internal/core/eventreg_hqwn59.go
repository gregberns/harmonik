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
	mustRegister(EventTypeLivenessHalt, func() EventPayload { return &LivenessHaltPayload{} })
	mustRegister(EventTypeStaleOpenBeadDetected, func() EventPayload { return &StaleOpenBeadDetectedPayload{} })
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
	registerKeeperInteriorEvents()
	registerAgentInputEvents()
	registerAlarmEvents()
	registerHITLDecisionEvents()
	registerDecisionRequiredEvents()
	registerBeadLedgerEvents()
}

func registerRunLifecycle() {
	mustRegisterAtVersion(EventTypeRunStarted, func() EventPayload { return &RunStartedPayload{} }, 2)
	mustRegister(EventTypeRunCompleted, func() EventPayload { return &RunCompletedPayload{} })
	mustRegister(EventTypeRunFailed, func() EventPayload { return &RunFailedPayload{} })
	mustRegister(EventTypeStateEntered, func() EventPayload { return &StateEnteredPayload{} })
	mustRegister(EventTypeStateExited, func() EventPayload { return &StateExitedPayload{} })
	mustRegister(EventTypeTransitionEvent, func() EventPayload { return &TransitionEventPayload{} })
	mustRegister(EventTypeCheckpointWritten, func() EventPayload { return &CheckpointWrittenPayload{} })
	mustRegister(EventTypeOutcomeEmitted, func() EventPayload { return &OutcomeEmittedPayload{} })
	mustRegister(EventTypeSubWorkflowEntered, func() EventPayload { return &SubWorkflowEnteredPayload{} })
	mustRegister(EventTypeSubWorkflowExited, func() EventPayload { return &SubWorkflowExitedPayload{} })
	mustRegister(EventTypeNodeDispatchRequested, func() EventPayload { return &NodeDispatchRequestedPayload{} })
	mustRegister(EventTypeNodeDispatchDecided, func() EventPayload { return &NodeDispatchDecidedPayload{} })
	mustRegister(EventTypeBeadClosed, func() EventPayload { return &BeadClosedPayload{} })
	mustRegister(EventTypeEpicCompleted, func() EventPayload { return &EpicCompletedPayload{} })
	mustRegister(EventTypeWorkingTreeRefreshFailed, func() EventPayload { return &WorkingTreeRefreshFailedPayload{} })
	mustRegister(EventTypeWorkingTreeLocalEditsOverwritten, func() EventPayload {
		return &WorkingTreeLocalEditsOverwrittenPayload{}
	})
	mustRegister(EventTypeImplementerPhaseComplete, func() EventPayload { return &ImplementerPhaseCompletePayload{} })
	mustRegister(EventTypeMergeBuildFailed, func() EventPayload { return &MergeBuildFailedPayload{} })
}

func registerControlPoints() {
	mustRegister(EventTypeHookFired, func() EventPayload { return &HookFiredPayload{} })
	mustRegister(EventTypeHookFailed, func() EventPayload { return &HookFailedPayload{} })
	mustRegister(EventTypeHookVerdictPersisted, func() EventPayload { return &HookVerdictPersistedPayload{} })
	mustRegister(EventTypeGateAllowed, func() EventPayload { return &GateAllowedPayload{} })
	mustRegister(EventTypeGateDenied, func() EventPayload { return &GateDeniedPayload{} })
	mustRegister(EventTypeGateEscalated, func() EventPayload { return &GateEscalatedPayload{} })
	mustRegister(EventTypeGuardReordered, func() EventPayload { return &GuardReorderedPayload{} })
	mustRegister(EventTypeGuardFailed, func() EventPayload { return &GuardFailedPayload{} })
	mustRegister(EventTypeControlPointsRegistered, func() EventPayload { return &ControlPointsRegisteredPayload{} })
	mustRegister(EventTypeControlPointsRegistrationStarted, func() EventPayload { return &ControlPointsRegistrationStartedPayload{} })
	mustRegister(EventTypeVerdictEnvelopeMismatch, func() EventPayload { return &VerdictEnvelopeMismatchPayload{} })
	mustRegister(EventTypePolicyExpressionExceededCost, func() EventPayload { return &PolicyExpressionExceededCostPayload{} })
	mustRegister(EventTypeGateDefinitionDrift, func() EventPayload { return &GateDefinitionDriftPayload{} })
	mustRegister(EventTypeGateRedefinedUnderCat6, func() EventPayload { return &GateRedefinedUnderCat6Payload{} })
}

func registerAgentEvents() {
	mustRegister(EventTypeAgentStarted, func() EventPayload { return &AgentStartedPayload{} })
	mustRegister(EventTypeAgentReady, func() EventPayload { return &AgentReadyPayload{} })
	mustRegister(EventTypeAgentOutputChunk, func() EventPayload { return &AgentOutputChunkPayload{} })
	mustRegister(EventTypeAgentCompleted, func() EventPayload { return &AgentCompletedPayload{} })
	mustRegister(EventTypeAgentFailed, func() EventPayload { return &AgentFailedPayload{} })
	mustRegister(EventTypeAgentHeartbeat, func() EventPayload { return &AgentHeartbeatPayload{} })
	mustRegister(EventTypeAgentRateLimitStatus, func() EventPayload { return &AgentRateLimitStatusPayload{} })
	mustRegister(EventTypeSessionLogLocation, func() EventPayload { return &SessionLogLocationPayload{} })
	mustRegister(EventTypeSkillsProvisioned, func() EventPayload { return &SkillsProvisionedPayload{} })
	mustRegister(EventTypeHandlerCapabilities, func() EventPayload { return &HandlerCapabilitiesPayload{} })
	mustRegister(EventTypeAgentWarningSilentHang, func() EventPayload { return &AgentWarningSilentHangPayload{} })
	mustRegister(EventTypeAgentResumedAfterWarning, func() EventPayload { return &AgentResumedAfterWarningPayload{} })
	mustRegister(EventTypeAgentSoftTerminating, func() EventPayload { return &AgentSoftTerminatingPayload{} })
	mustRegister(EventTypeAgentHardTerminating, func() EventPayload { return &AgentHardTerminatingPayload{} })
	mustRegister(EventTypeLaunchInitiated, func() EventPayload { return &LaunchInitiatedPayload{} })
	mustRegister(EventTypeAgentReadyTimeout, func() EventPayload { return &AgentReadyTimeoutPayload{} })
	mustRegister(EventTypePostAgentReadyHang, func() EventPayload { return &PostAgentReadyHangPayload{} })
	mustRegister(EventTypeLifecycleTransition, func() EventPayload { return &LifecycleTransitionPayload{} })
	mustRegister(EventTypePasteInjectFailed, func() EventPayload { return &PasteInjectFailedPayload{} })
	mustRegister(EventTypeLaunchStallDetected, func() EventPayload { return &LaunchStallDetectedPayload{} })
	mustRegister(EventTypeAgentReadyStallDetected, func() EventPayload { return &AgentReadyStallDetectedPayload{} })
	mustRegister(EventTypeSpawnCapBlocked, func() EventPayload { return &SpawnCapBlockedPayload{} })
	mustRegister(EventTypeImplementerBudgetExceeded, func() EventPayload { return &ImplementerBudgetExceededPayload{} })
	mustRegister(EventTypeImplementerNoWorkSuspected, func() EventPayload { return &ImplementerNoWorkSuspectedPayload{} })
	mustRegister(EventTypeReviewerBudgetExceeded, func() EventPayload { return &ReviewerBudgetExceededPayload{} })
	mustRegister(EventTypeTmuxNewWindowTimeout, func() EventPayload { return &TmuxNewWindowTimeoutPayload{} })
	mustRegister(EventTypeCodexBillingGuard, func() EventPayload { return &CodexBillingGuardPayload{} })
	mustRegister(EventTypePiBillingGuard, func() EventPayload { return &PiBillingGuardPayload{} })
	mustRegister(EventTypeAgentMessage, func() EventPayload { return &AgentMessagePayload{} })
	mustRegister(EventTypeAgentPresence, func() EventPayload { return &AgentPresencePayload{} })
	mustRegister(EventTypeHarnessSelected, func() EventPayload { return &HarnessSelectedPayload{} })
	mustRegister(EventTypeModelSelected, func() EventPayload { return &ModelSelectedPayload{} })
	mustRegister(EventTypeProviderSelected, func() EventPayload { return &ProviderSelectedPayload{} })
}

func registerBudgetEvents() {
	mustRegister(EventTypeBudgetWarning, func() EventPayload { return &BudgetWarningPayload{} })
	mustRegister(EventTypeBudgetAccrual, func() EventPayload { return &BudgetAccrualPayload{} })
	mustRegister(EventTypeBudgetExhausted, func() EventPayload { return &BudgetExhaustedEventPayload{} })
}

func registerWorkspaceEvents() {
	mustRegister(EventTypeWorkspaceCreated, func() EventPayload { return &WorkspaceCreatedPayload{} })
	mustRegister(EventTypeWorkspaceLeased, func() EventPayload { return &WorkspaceLeasedPayload{} })
	mustRegister(EventTypeWorkspaceMergeStatus, func() EventPayload { return &WorkspaceMergeStatusPayload{} })
	mustRegister(EventTypeWorkspaceDiscarded, func() EventPayload { return &WorkspaceDiscardedPayload{} })
	mustRegister(EventTypeWorkspaceInterrupted, func() EventPayload { return &WorkspaceInterruptedPayload{} })
	mustRegister(EventTypeMergeConflictEscalation, func() EventPayload { return &MergeConflictEscalationPayload{} })
}

func registerReconciliationEvents() {
	mustRegister(EventTypeReconciliationStarted, func() EventPayload { return &ReconciliationStartedPayload{} })
	mustRegister(EventTypeReconciliationCompleted, func() EventPayload { return &ReconciliationCompletedPayload{} })
	mustRegister(EventTypeReconciliationCategoryAssigned, func() EventPayload { return &ReconciliationCategoryAssignedPayload{} })
	mustRegister(EventTypeReconciliationVerdictEmitted, func() EventPayload { return &ReconciliationVerdictEmittedPayload{} })
	mustRegister(EventTypeReconciliationVerdictExecuted, func() EventPayload { return &VerdictExecutedPayload{} })
	mustRegister(EventTypeReconciliationVerdictMalformed, func() EventPayload { return &MalformedVerdictPayload{} })
	mustRegister(EventTypeReconciliationBudgetExhausted, func() EventPayload { return &BudgetExhaustedPayload{} })
	mustRegister(EventTypeReconciliationVerdictStale, func() EventPayload { return &StaleVerdictPayload{} })
	mustRegister(EventTypeStoreDivergenceDetected, func() EventPayload { return &StoreDivergenceDetectedPayload{} })
	mustRegister(EventTypeOperatorEscalationRequired, func() EventPayload { return &OperatorEscalationRequiredPayload{} })
	mustRegister(EventTypeDivergenceInconclusive, func() EventPayload { return &DivergenceInconclusivePayload{} })
	mustRegister(EventTypeReconciliationDispatchDeduplicated, func() EventPayload { return &ReconciliationDispatchDeduplicatedPayload{} })
	mustRegister(EventTypeReconciliationDetectorPanic, func() EventPayload { return &ReconciliationDetectorPanicPayload{} })
	mustRegister(EventTypeReconciliationVerdictExecutionRetry, func() EventPayload { return &ReconciliationVerdictExecutionRetryPayload{} })
	mustRegister(EventTypeBeadTerminalTransitionRecovered, func() EventPayload { return &BeadTerminalTransitionRecoveredPayload{} })
	mustRegister(EventTypeReconciliationMismatchObserved, func() EventPayload { return &ReconciliationMismatchObservedPayload{} })
}

func registerDaemonLifecycleEvents() {
	mustRegister(EventTypeDaemonStarted, func() EventPayload { return &DaemonStartedPayload{} })
	mustRegister(EventTypeDaemonReady, func() EventPayload { return &DaemonReadyPayload{} })
	mustRegister(EventTypeDaemonShutdown, func() EventPayload { return &DaemonShutdownPayload{} })
	mustRegister(EventTypeDaemonStartupFailed, func() EventPayload { return &DaemonStartupFailedPayload{} })
	mustRegister(EventTypeDaemonDegraded, func() EventPayload { return &DaemonDegradedPayload{} })
	mustRegister(EventTypeOperatorPauseStatus, func() EventPayload { return &OperatorPauseStatusPayload{} })
	mustRegister(EventTypeOperatorResuming, func() EventPayload { return &OperatorResumingPayload{} })
	mustRegister(EventTypeOperatorStopped, func() EventPayload { return &OperatorStoppedPayload{} })
	mustRegister(EventTypeOperatorUpgrading, func() EventPayload { return &OperatorUpgradingPayload{} })
	mustRegister(EventTypeOperatorUpgradeCompleted, func() EventPayload { return &OperatorUpgradeCompletedPayload{} })
	mustRegister(EventTypeOperatorUpgradeRejected, func() EventPayload { return &OperatorUpgradeRejectedPayload{} })
	mustRegister(EventTypeOperatorCommandRejected, func() EventPayload { return &OperatorCommandRejectedPayload{} })
	mustRegister(EventTypeDispatchDeferred, func() EventPayload { return &DispatchDeferredPayload{} })
	mustRegister(EventTypeDaemonOrphanSweepCompleted, func() EventPayload { return &DaemonOrphanSweepCompletedPayload{} })
	mustRegister(EventTypeInfrastructureUnavailable, func() EventPayload { return &InfrastructureUnavailablePayload{} })
	mustRegister(EventTypeOperatorCommandFailed, func() EventPayload { return &OperatorCommandFailedPayload{} })
	mustRegister(EventTypeOperatorEscalationCleared, func() EventPayload { return &OperatorEscalationClearedPayload{} })
	mustRegister(EventTypeDaemonConfig, func() EventPayload { return &DaemonConfigPayload{} })
	mustRegister(EventTypeDiskLow, func() EventPayload { return &DiskLowPayload{} })
	mustRegister(EventTypeSupervisorRevival, func() EventPayload { return &SupervisorRevivalPayload{} })
	mustRegister(EventTypeDashboardStale, func() EventPayload { return &DashboardStalePayload{} })
	mustRegister(EventTypeDashboardRefreshed, func() EventPayload { return &DashboardRefreshedPayload{} })
}

func registerBusEvents() {
	mustRegister(EventTypeMetric, func() EventPayload { return &MetricPayload{} })
	mustRegister(EventTypeConsumerFailed, func() EventPayload { return &ConsumerFailedPayload{} })
	mustRegister(EventTypeDeadLetterEnqueued, func() EventPayload { return &DeadLetterEnqueuedPayload{} })
	mustRegister(EventTypeBusOverflow, func() EventPayload { return &BusOverflowPayload{} })
	mustRegister(EventTypeRedactionFailed, func() EventPayload { return &RedactionFailedPayload{} })
	mustRegister(EventTypeBeadClaimSkipped, func() EventPayload { return &BeadClaimSkippedPayload{} })
}

func registerReviewLoopEvents() {
	mustRegister(EventTypeImplementerResumed, func() EventPayload { return &ImplementerResumedPayload{} })
	mustRegister(EventTypeReviewerLaunched, func() EventPayload { return &ReviewerLaunchedPayload{} })
	mustRegister(EventTypeReviewerVerdict, func() EventPayload { return &ReviewerVerdictPayload{} })
	mustRegister(EventTypeIterationCapHit, func() EventPayload { return &IterationCapHitPayload{} })
	mustRegister(EventTypeNoProgressDetected, func() EventPayload { return &NoProgressDetectedPayload{} })
	mustRegister(EventTypeReviewLoopCycleComplete, func() EventPayload { return &ReviewLoopCycleCompletePayload{} })
	mustRegister(EventTypeBeadLabelConflict, func() EventPayload { return &BeadLabelConflictPayload{} })
	mustRegister(EventTypeReviewBypassed, func() EventPayload { return &ReviewBypassedPayload{} })
	mustRegister(EventTypeReviewFixupStalled, func() EventPayload { return &ReviewFixupStalledPayload{} })
}

func registerQueueEvents() {
	mustRegister(EventTypeQueueSubmitted, func() EventPayload { return &QueueSubmittedPayload{} })
	mustRegister(EventTypeQueueGroupStarted, func() EventPayload { return &QueueGroupStartedPayload{} })
	mustRegister(EventTypeQueueGroupCompleted, func() EventPayload { return &QueueGroupCompletedPayload{} })
	mustRegister(EventTypeQueuePaused, func() EventPayload { return &QueuePausedPayload{} })
	mustRegister(EventTypeQueueAppended, func() EventPayload { return &QueueAppendedPayload{} })
	mustRegister(EventTypeQueueItemDeferredForLedgerDep, func() EventPayload { return &QueueItemDeferredForLedgerDepPayload{} })
	mustRegister(EventTypeQueueItemReconciled, func() EventPayload { return &QueueItemReconciledPayload{} })
	mustRegister(EventTypeCrossQueueCollision, func() EventPayload { return &CrossQueueCollisionPayload{} })
}

func registerHandlerPauseEvents() {
	mustRegister(EventTypeHandlerPaused, func() EventPayload { return &HandlerPausedPayload{} })
	mustRegister(EventTypeHandlerResumed, func() EventPayload { return &HandlerResumedPayload{} })
	mustRegister(EventTypeQueueItemHeldForHandlerPause, func() EventPayload { return &QueueItemHeldForHandlerPausePayload{} })
}

func registerGateDispatchEvents() {
	mustRegister(EventTypeGateDecisionRecorded, func() EventPayload { return &GateDecisionRecordedPayload{} })
}

func registerWorkflowLoaderEvents() {
	mustRegister(EventTypeSkillsResolved, func() EventPayload { return &SkillsResolvedPayload{} })
}

func registerKeeperEvents() {
	mustRegister(EventTypeSessionKeeperWarn, func() EventPayload { return &SessionKeeperWarnPayload{} })
	mustRegister(EventTypeSessionKeeperNoGauge, func() EventPayload { return &SessionKeeperNoGaugePayload{} })
	mustRegister(EventTypeSessionKeeperHandoffStarted, func() EventPayload { return &SessionKeeperHandoffStartedPayload{} })
	mustRegister(EventTypeSessionKeeperCycleComplete, func() EventPayload { return &SessionKeeperCycleCompletePayload{} })
	mustRegister(EventTypeSessionKeeperCycleAborted, func() EventPayload { return &SessionKeeperCycleAbortedPayload{} })
	mustRegister(EventTypeSessionKeeperCycleParked, func() EventPayload { return &SessionKeeperCycleParkedPayload{} })
	mustRegister(EventTypeSessionKeeperClearUnconfirmed, func() EventPayload { return &SessionKeeperClearUnconfirmedPayload{} })
	mustRegister(EventTypeSessionKeeperCycleRecovered, func() EventPayload { return &SessionKeeperCycleRecoveredPayload{} })
	mustRegister(EventTypeSessionKeeperPrecompactBlocked, func() EventPayload { return &SessionKeeperPrecompactBlockedPayload{} })
	mustRegister(EventTypeSessionKeeperRespawnAttempted, func() EventPayload { return &SessionKeeperRespawnAttemptedPayload{} })
	mustRegister(EventTypeSessionKeeperOperatorAttached, func() EventPayload { return &SessionKeeperOperatorAttachedPayload{} })
	mustRegister(EventTypeSessionKeeperRestartNowBlocked, func() EventPayload { return &SessionKeeperRestartNowBlockedPayload{} })
	mustRegister(EventTypeSessionKeeperRestartNow, func() EventPayload { return &SessionKeeperRestartNowPayload{} })
	mustRegister(EventTypeSessionKeeperBlind, func() EventPayload { return &SessionKeeperBlindPayload{} })
	mustRegister(EventTypeSessionKeeperHardCeiling, func() EventPayload { return &SessionKeeperHardCeilingPayload{} })
	mustRegister(EventTypeSessionKeeperIdleCrew, func() EventPayload { return &SessionKeeperIdleCrewPayload{} })
	mustRegister("session_keeper_config_rejected", func() EventPayload { return &SessionKeeperConfigRejectedPayload{} })
	mustRegister(EventTypeSessionKeeperWatcherDead, func() EventPayload { return &SessionKeeperWatcherDeadPayload{} })
	mustRegister(EventTypeSessionKeeperLivePaneRecover, func() EventPayload { return &SessionKeeperLivePaneRecoverPayload{} })
	mustRegister(EventTypeSessionKeeperAckTimeout, func() EventPayload { return &SessionKeeperAckTimeoutPayload{} })
}

func registerKeeperInteriorEvents() {
	mustRegister(EventTypeSessionKeeperHandoffWritten, func() EventPayload { return &SessionKeeperHandoffWrittenPayload{} })
	mustRegister(EventTypeSessionKeeperModelDone, func() EventPayload { return &SessionKeeperModelDonePayload{} })
	mustRegister(EventTypeSessionKeeperClearSent, func() EventPayload { return &SessionKeeperClearSentPayload{} })
	mustRegister(EventTypeSessionKeeperNewSessionUp, func() EventPayload { return &SessionKeeperNewSessionUpPayload{} })
}

func registerAgentInputEvents() {
	mustRegister("agent_input_acked", func() EventPayload { return &AgentInputAckedPayload{} })
	mustRegister("agent_input_stale", func() EventPayload { return &AgentInputStalePayload{} })
}

func registerAlarmEvents() {
	mustRegister(EventTypeReviewGateAnomaly, func() EventPayload { return &ReviewGateAnomalyPayload{} })

	mustRegister(EventTypeStallDetected, func() EventPayload { return &StallDetectedPayload{} })
}

func registerHITLDecisionEvents() {
	mustRegister(EventTypeDecisionNeeded, func() EventPayload { return &DecisionNeededPayload{} })
	mustRegister(EventTypeDecisionResolved, func() EventPayload { return &DecisionResolvedPayload{} })
	mustRegister(EventTypeDecisionWithdrawn, func() EventPayload { return &DecisionWithdrawnPayload{} })
}

func registerDecisionRequiredEvents() {
	mustRegister(EventTypeDecisionRequired, func() EventPayload { return &DecisionRequiredPayload{} })
	mustRegister(EventTypeDecisionAcknowledged, func() EventPayload { return &DecisionAcknowledgedPayload{} })
}

func registerBeadLedgerEvents() {
	mustRegister(EventTypeBeadSyncFailed, func() EventPayload { return &BeadSyncFailedPayload{} })
	mustRegister(EventTypeBeadLedgerRecovered, func() EventPayload { return &BeadLedgerRecoveredPayload{} })
	mustRegister(EventTypeBeadLedgerCorrupt, func() EventPayload { return &BeadLedgerCorruptPayload{} })
	mustRegister(EventTypeBeadLedgerConflictAudit, func() EventPayload { return &BeadLedgerConflictAuditPayload{} })
	mustRegister(EventTypeOrphanedChildBead, func() EventPayload { return &OrphanedChildBeadPayload{} })
}

func mustRegister(typeName EventType, ctor func() EventPayload) {
	if err := RegisterEventType(typeName, ctor); err != nil {
		panic("core: mustRegister: " + string(typeName) + ": " + err.Error())
	}
}

func mustRegisterAtVersion(typeName EventType, ctor func() EventPayload, version int) {
	if err := RegisterEventTypeAtVersion(typeName, ctor, version); err != nil {
		panic("core: mustRegisterAtVersion: " + string(typeName) + ": " + err.Error())
	}
}
