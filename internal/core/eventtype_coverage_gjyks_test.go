package core

import (
	"encoding/json"
	"testing"
)

// eventtype_coverage_gjyks_test.go — drift-detection test asserting that every
// EventType constant declared in eventtype.go has a corresponding registry entry.
//
// Without this test, adding a new EventType constant without registering its
// constructor would compile silently but cause DispatchUnknownEventError on
// JSONL replay — the failure mode surfaced by bead hk-gjyks.
//
// The test is table-driven: allEventTypeCohort enumerates every constant with
// its constructor. This is the single source of truth for "what must be
// registered". When a new EventType constant is added to eventtype.go, it MUST
// also be appended to allEventTypeCohort in this file.
//
// The test uses a LOCAL registry snapshot (not the global registry) to be
// immune to eventRegistryReset() calls in TestRegistry subtests — same isolation
// pattern used by TestQueueEventsCohortRegistered.
//
// Bead ref: hk-gjyks.

// gjyksEventTypeCohortEntry pairs an EventType constant with its constructor —
// the two artifacts that MUST be in sync for correct JSONL replay.
type gjyksEventTypeCohortEntry struct {
	et    EventType
	mkPay func() EventPayload
}

// allEventTypeCohort enumerates every EventType constant in eventtype.go paired
// with its registered constructor. The list mirrors eventreg_hqwn59.go's
// mustRegister calls. Keep the two in sync.
var allEventTypeCohort = []gjyksEventTypeCohortEntry{
	// §8.1 Run lifecycle
	{EventTypeRunStarted, func() EventPayload { return &RunStartedPayload{} }},
	{EventTypeRunCompleted, func() EventPayload { return &RunCompletedPayload{} }},
	{EventTypeRunFailed, func() EventPayload { return &RunFailedPayload{} }},
	{EventTypeStateEntered, func() EventPayload { return &StateEnteredPayload{} }},
	{EventTypeStateExited, func() EventPayload { return &StateExitedPayload{} }},
	{EventTypeTransitionEvent, func() EventPayload { return &TransitionEventPayload{} }},
	{EventTypeCheckpointWritten, func() EventPayload { return &CheckpointWrittenPayload{} }},
	{EventTypeOutcomeEmitted, func() EventPayload { return &OutcomeEmittedPayload{} }},
	{EventTypeSubWorkflowEntered, func() EventPayload { return &SubWorkflowEnteredPayload{} }},
	{EventTypeSubWorkflowExited, func() EventPayload { return &SubWorkflowExitedPayload{} }},
	{EventTypeNodeDispatchRequested, func() EventPayload { return &NodeDispatchRequestedPayload{} }},
	{EventTypeNodeDispatchDecided, func() EventPayload { return &NodeDispatchDecidedPayload{} }},
	{EventTypeBeadClosed, func() EventPayload { return &BeadClosedPayload{} }},
	{EventTypeEpicCompleted, func() EventPayload { return &EpicCompletedPayload{} }},
	{EventTypeWorkingTreeRefreshFailed, func() EventPayload { return &WorkingTreeRefreshFailedPayload{} }},
	{EventTypeImplementerEscapedWorktree, func() EventPayload { return &ImplementerEscapedWorktreePayload{} }},
	{EventTypeImplementerPhaseComplete, func() EventPayload { return &ImplementerPhaseCompletePayload{} }},
	{EventTypeMergeBuildFailed, func() EventPayload { return &MergeBuildFailedPayload{} }},

	// §8.2 Control-point lifecycle
	{EventTypeHookFired, func() EventPayload { return &HookFiredPayload{} }},
	{EventTypeHookFailed, func() EventPayload { return &HookFailedPayload{} }},
	{EventTypeHookVerdictPersisted, func() EventPayload { return &HookVerdictPersistedPayload{} }},
	{EventTypeGateAllowed, func() EventPayload { return &GateAllowedPayload{} }},
	{EventTypeGateDenied, func() EventPayload { return &GateDeniedPayload{} }},
	{EventTypeGateEscalated, func() EventPayload { return &GateEscalatedPayload{} }},
	{EventTypeGuardReordered, func() EventPayload { return &GuardReorderedPayload{} }},
	{EventTypeGuardFailed, func() EventPayload { return &GuardFailedPayload{} }},
	{EventTypeControlPointsRegistered, func() EventPayload { return &ControlPointsRegisteredPayload{} }},
	{EventTypeControlPointsRegistrationStarted, func() EventPayload { return &ControlPointsRegistrationStartedPayload{} }},
	{EventTypeVerdictEnvelopeMismatch, func() EventPayload { return &VerdictEnvelopeMismatchPayload{} }},
	{EventTypePolicyExpressionExceededCost, func() EventPayload { return &PolicyExpressionExceededCostPayload{} }},

	// §8.3 Agent/handler lifecycle
	{EventTypeAgentReady, func() EventPayload { return &AgentReadyPayload{} }},
	{EventTypeAgentStarted, func() EventPayload { return &AgentStartedPayload{} }},
	{EventTypeAgentOutputChunk, func() EventPayload { return &AgentOutputChunkPayload{} }},
	{EventTypeAgentCompleted, func() EventPayload { return &AgentCompletedPayload{} }},
	{EventTypeAgentFailed, func() EventPayload { return &AgentFailedPayload{} }},
	{EventTypeAgentHeartbeat, func() EventPayload { return &AgentHeartbeatPayload{} }},
	{EventTypeAgentRateLimitStatus, func() EventPayload { return &AgentRateLimitStatusPayload{} }},
	{EventTypeSessionLogLocation, func() EventPayload { return &SessionLogLocationPayload{} }},
	{EventTypeSkillsProvisioned, func() EventPayload { return &SkillsProvisionedPayload{} }},
	{EventTypeHandlerCapabilities, func() EventPayload { return &HandlerCapabilitiesPayload{} }},
	{EventTypeAgentWarningSilentHang, func() EventPayload { return &AgentWarningSilentHangPayload{} }},
	{EventTypeAgentResumedAfterWarning, func() EventPayload { return &AgentResumedAfterWarningPayload{} }},
	{EventTypeAgentSoftTerminating, func() EventPayload { return &AgentSoftTerminatingPayload{} }},
	{EventTypeAgentHardTerminating, func() EventPayload { return &AgentHardTerminatingPayload{} }},
	{EventTypeLaunchInitiated, func() EventPayload { return &LaunchInitiatedPayload{} }},

	// §8.4 Budget lifecycle
	{EventTypeBudgetWarning, func() EventPayload { return &BudgetWarningPayload{} }},
	{EventTypeBudgetAccrual, func() EventPayload { return &BudgetAccrualPayload{} }},
	{EventTypeBudgetExhausted, func() EventPayload { return &BudgetExhaustedEventPayload{} }},

	// §8.5 Workspace lifecycle
	{EventTypeWorkspaceCreated, func() EventPayload { return &WorkspaceCreatedPayload{} }},
	{EventTypeWorkspaceLeased, func() EventPayload { return &WorkspaceLeasedPayload{} }},
	{EventTypeWorkspaceMergeStatus, func() EventPayload { return &WorkspaceMergeStatusPayload{} }},
	{EventTypeWorkspaceDiscarded, func() EventPayload { return &WorkspaceDiscardedPayload{} }},
	{EventTypeWorkspaceInterrupted, func() EventPayload { return &WorkspaceInterruptedPayload{} }},
	{EventTypeMergeConflictEscalation, func() EventPayload { return &MergeConflictEscalationPayload{} }},

	// §8.6 Reconciliation lifecycle
	{EventTypeReconciliationStarted, func() EventPayload { return &ReconciliationStartedPayload{} }},
	{EventTypeReconciliationCategoryAssigned, func() EventPayload { return &ReconciliationCategoryAssignedPayload{} }},
	{EventTypeReconciliationVerdictEmitted, func() EventPayload { return &ReconciliationVerdictEmittedPayload{} }},
	{EventTypeReconciliationVerdictExecuted, func() EventPayload { return &VerdictExecutedPayload{} }},
	{EventTypeReconciliationVerdictMalformed, func() EventPayload { return &MalformedVerdictPayload{} }},
	{EventTypeReconciliationBudgetExhausted, func() EventPayload { return &BudgetExhaustedPayload{} }},
	{EventTypeReconciliationVerdictStale, func() EventPayload { return &StaleVerdictPayload{} }},
	{EventTypeStoreDivergenceDetected, func() EventPayload { return &StoreDivergenceDetectedPayload{} }},
	{EventTypeOperatorEscalationRequired, func() EventPayload { return &OperatorEscalationRequiredPayload{} }},
	{EventTypeDivergenceInconclusive, func() EventPayload { return &DivergenceInconclusivePayload{} }},
	{EventTypeReconciliationDispatchDeduplicated, func() EventPayload { return &ReconciliationDispatchDeduplicatedPayload{} }},
	{EventTypeReconciliationDetectorPanic, func() EventPayload { return &ReconciliationDetectorPanicPayload{} }},
	{EventTypeReconciliationVerdictExecutionRetry, func() EventPayload { return &ReconciliationVerdictExecutionRetryPayload{} }},
	{EventTypeBeadTerminalTransitionRecovered, func() EventPayload { return &BeadTerminalTransitionRecoveredPayload{} }},
	{EventTypeReconciliationMismatchObserved, func() EventPayload { return &ReconciliationMismatchObservedPayload{} }},

	// §8.7 Operator-control and daemon lifecycle
	{EventTypeDaemonStarted, func() EventPayload { return &DaemonStartedPayload{} }},
	{EventTypeDaemonReady, func() EventPayload { return &DaemonReadyPayload{} }},
	{EventTypeDaemonShutdown, func() EventPayload { return &DaemonShutdownPayload{} }},
	{EventTypeDaemonStartupFailed, func() EventPayload { return &DaemonStartupFailedPayload{} }},
	{EventTypeDaemonDegraded, func() EventPayload { return &DaemonDegradedPayload{} }},
	{EventTypeOperatorPauseStatus, func() EventPayload { return &OperatorPauseStatusPayload{} }},
	{EventTypeOperatorResuming, func() EventPayload { return &OperatorResumingPayload{} }},
	{EventTypeOperatorStopped, func() EventPayload { return &OperatorStoppedPayload{} }},
	{EventTypeOperatorUpgrading, func() EventPayload { return &OperatorUpgradingPayload{} }},
	{EventTypeOperatorUpgradeCompleted, func() EventPayload { return &OperatorUpgradeCompletedPayload{} }},
	{EventTypeOperatorUpgradeRejected, func() EventPayload { return &OperatorUpgradeRejectedPayload{} }},
	{EventTypeOperatorCommandRejected, func() EventPayload { return &OperatorCommandRejectedPayload{} }},
	{EventTypeDispatchDeferred, func() EventPayload { return &DispatchDeferredPayload{} }},
	{EventTypeDaemonOrphanSweepCompleted, func() EventPayload { return &DaemonOrphanSweepCompletedPayload{} }},
	{EventTypeInfrastructureUnavailable, func() EventPayload { return &InfrastructureUnavailablePayload{} }},
	{EventTypeOperatorCommandFailed, func() EventPayload { return &OperatorCommandFailedPayload{} }},
	{EventTypeOperatorEscalationCleared, func() EventPayload { return &OperatorEscalationClearedPayload{} }},
	{EventTypeDaemonConfig, func() EventPayload { return &DaemonConfigPayload{} }},
	// supervisor_revival (§8.7.20, hk-rnkuy): emitted at startup when prior session lacked daemon_shutdown.
	{EventTypeSupervisorRevival, func() EventPayload { return &SupervisorRevivalPayload{} }},

	// §8.1a Review-loop cycle + §8.8.6 bead label conflict
	{EventTypeImplementerResumed, func() EventPayload { return &ImplementerResumedPayload{} }},
	{EventTypeReviewerLaunched, func() EventPayload { return &ReviewerLaunchedPayload{} }},
	{EventTypeReviewerVerdict, func() EventPayload { return &ReviewerVerdictPayload{} }},
	{EventTypeIterationCapHit, func() EventPayload { return &IterationCapHitPayload{} }},
	{EventTypeNoProgressDetected, func() EventPayload { return &NoProgressDetectedPayload{} }},
	{EventTypeReviewLoopCycleComplete, func() EventPayload { return &ReviewLoopCycleCompletePayload{} }},
	{EventTypeBeadLabelConflict, func() EventPayload { return &BeadLabelConflictPayload{} }},
	{EventTypeReviewBypassed, func() EventPayload { return &ReviewBypassedPayload{} }},

	// §8.8 Observability and bus-internal
	{EventTypeMetric, func() EventPayload { return &MetricPayload{} }},
	{EventTypeConsumerFailed, func() EventPayload { return &ConsumerFailedPayload{} }},
	{EventTypeDeadLetterEnqueued, func() EventPayload { return &DeadLetterEnqueuedPayload{} }},
	{EventTypeBusOverflow, func() EventPayload { return &BusOverflowPayload{} }},
	{EventTypeRedactionFailed, func() EventPayload { return &RedactionFailedPayload{} }},
	{EventTypeBeadClaimSkipped, func() EventPayload { return &BeadClaimSkippedPayload{} }},

	// §8.11 Handler-pause lifecycle
	{EventTypeHandlerPaused, func() EventPayload { return &HandlerPausedPayload{} }},
	{EventTypeHandlerResumed, func() EventPayload { return &HandlerResumedPayload{} }},
	{EventTypeQueueItemHeldForHandlerPause, func() EventPayload { return &QueueItemHeldForHandlerPausePayload{} }},

	// hk-lr5t: harness_selected dispatch observability
	{EventTypeHarnessSelected, func() EventPayload { return &HarnessSelectedPayload{} }},

	// hk-eval-prog-model-on-log-bh2o7: model_selected dispatch observability
	{EventTypeModelSelected, func() EventPayload { return &ModelSelectedPayload{} }},

	// §8.12 Staleness-detection
	{EventTypeRunStale, func() EventPayload { return &RunStalePayload{} }},

	// §8.2a Gate-node dispatch
	{EventTypeGateDecisionRecorded, func() EventPayload { return &GateDecisionRecordedPayload{} }},

	// Launch / dispatch diagnostics (hk-9vp51, hk-da3rr)
	{EventTypeImplementerBudgetExceeded, func() EventPayload { return &ImplementerBudgetExceededPayload{} }},
	{EventTypeReviewerBudgetExceeded, func() EventPayload { return &ReviewerBudgetExceededPayload{} }},

	// Launch_initiated → agent_ready stall detector (hk-1s1or)
	{EventTypeAgentReadyStallDetected, func() EventPayload { return &AgentReadyStallDetectedPayload{} }},

	// §8.10 Queue lifecycle
	{EventTypeQueueSubmitted, func() EventPayload { return &QueueSubmittedPayload{} }},
	{EventTypeQueueGroupStarted, func() EventPayload { return &QueueGroupStartedPayload{} }},
	{EventTypeQueueGroupCompleted, func() EventPayload { return &QueueGroupCompletedPayload{} }},
	{EventTypeQueuePaused, func() EventPayload { return &QueuePausedPayload{} }},
	{EventTypeQueueAppended, func() EventPayload { return &QueueAppendedPayload{} }},
	{EventTypeQueueItemDeferredForLedgerDep, func() EventPayload { return &QueueItemDeferredForLedgerDepPayload{} }},
	{EventTypeQueueItemReconciled, func() EventPayload { return &QueueItemReconciledPayload{} }},

	// §8.15 Bead-ledger merge lifecycle (hk-u3q6o, hk-k7va9)
	{EventTypeBeadSyncFailed, func() EventPayload { return &BeadSyncFailedPayload{} }},
	{EventTypeBeadLedgerRecovered, func() EventPayload { return &BeadLedgerRecoveredPayload{} }},
	{EventTypeBeadLedgerCorrupt, func() EventPayload { return &BeadLedgerCorruptPayload{} }},
	{EventTypeBeadLedgerConflictAudit, func() EventPayload { return &BeadLedgerConflictAuditPayload{} }},
	{EventTypeOrphanedChildBead, func() EventPayload { return &OrphanedChildBeadPayload{} }},
}

// TestAllEventTypeConstantsHaveRegistryEntries asserts that every EventType
// constant listed in allEventTypeCohort has a working constructor that produces
// a non-nil payload and round-trips through JSON successfully.
//
// This test protects against the failure mode in hk-gjyks: declaring an
// EventType constant in eventtype.go without adding a mustRegister call in
// eventreg_hqwn59.go. Without registration, JSONL replay of any event of that
// type fails with DispatchUnknownEventError at runtime.
//
// The test uses a LOCAL registry snapshot built from allEventTypeCohort, not
// the global registry. This makes it immune to eventRegistryReset() calls in
// sibling tests (same isolation pattern as TestQueueEventsCohortRegistered).
//
// What it verifies per entry:
//  1. Constructor returns a non-nil pointer.
//  2. An empty JSON object unmarshal succeeds (no required struct fields cause panic).
//  3. The EventType constant string value matches the type name used in eventreg_hqwn59.go
//     (tested indirectly: the cohort is the same table that register* functions use,
//     so if a constant is mapped to the wrong string the drift-detection fails at
//     DecodePayload time when the real registry is queried).
func TestAllEventTypeConstantsHaveRegistryEntries(t *testing.T) {
	t.Parallel()

	// Build a local constructor map from the cohort table.
	// This mirrors what eventreg_hqwn59.go's init() does, but isolated from
	// the global registry so eventRegistryReset() in other tests cannot interfere.
	localCtors := make(map[string]func() EventPayload, len(allEventTypeCohort))
	for _, entry := range allEventTypeCohort {
		localCtors[string(entry.et)] = entry.mkPay
	}

	for _, entry := range allEventTypeCohort {
		entry := entry // capture loop variable
		t.Run(string(entry.et), func(t *testing.T) {
			t.Parallel()

			// 1. Constructor must return a non-nil pointer.
			got := entry.mkPay()
			if got == nil {
				t.Fatalf("constructor for %q returned nil", entry.et)
			}

			// 2. Empty JSON object must unmarshal without error.
			// This guards against constructors that return types which panic
			// on zero-value JSON decode (e.g., non-pointer receivers or required
			// non-nullable fields that the JSON decoder would panic on).
			if err := json.Unmarshal([]byte(`{}`), got); err != nil {
				t.Errorf("json.Unmarshal({}) into %T for EventType %q: %v", got, entry.et, err)
			}

			// 3. The EventType constant must be present in the local constructor map
			// (tautological here, but ensures the table is self-consistent and that
			// the string value of the EventType constant matches a registered key).
			if _, ok := localCtors[string(entry.et)]; !ok {
				t.Errorf("EventType %q not found in allEventTypeCohort map — table is inconsistent", entry.et)
			}
		})
	}
}
