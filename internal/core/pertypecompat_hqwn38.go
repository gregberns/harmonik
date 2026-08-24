package core

import "fmt"

// PayloadCompatEntry declares the N-1 compatibility window for one registered
// event type. It is the per-type analogue of the cross-artifact compatibility
// matrix described in specs/control-points.md §6.3.
//
// Spec ref: event-model.md §4.8 EV-029.
type PayloadCompatEntry struct {
	// TypeName is the §8 event type name (e.g. "run_started").
	TypeName EventType

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

var allPayloadCompatEntries = []PayloadCompatEntry{
	{TypeName: EventTypeLivenessHalt, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeStaleOpenBeadDetected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// ── §8.1 Run lifecycle ──────────────────────────────────────────────────
	{TypeName: EventTypeRunStarted, CurrentVersion: 2, PreviousVersion: 1, CompatWindowHolds: true, AdditiveOnly: false},
	{TypeName: EventTypeRunCompleted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeRunFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeStateEntered, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeStateExited, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeTransitionEvent, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeCheckpointWritten, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeOutcomeEmitted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSubWorkflowEntered, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSubWorkflowExited, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeNodeDispatchRequested, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeNodeDispatchDecided, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeBeadClosed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-w6y70: epic_completed — emitted at most once per parent epic after last child closes (§8.13).
	{TypeName: EventTypeEpicCompleted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeWorkingTreeRefreshFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-7qmpp: working_tree_local_edits_overwritten — the EM-054 refresh named what it overwrote.
	{TypeName: EventTypeWorkingTreeLocalEditsOverwritten, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeImplementerPhaseComplete, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-o68j3: post-merge build gate event.
	{TypeName: EventTypeMergeBuildFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-nqvqr / hk-4q6ah: the pre-rebase cleanup names what it destroys.
	{TypeName: EventTypeRunWorktreeChurnEditsDiscarded, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeRunWorktreeUntrackedFilesRemoved, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeImplementerResumed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReviewerLaunched, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReviewerVerdict, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeIterationCapHit, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeNoProgressDetected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReviewLoopCycleComplete, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReviewBypassed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReviewFixupStalled, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeHookFired, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeHookFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeHookVerdictPersisted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeGateAllowed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeGateDenied, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeGateEscalated, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeGateDecisionRecorded, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSkillsResolved, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeGuardReordered, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeGuardFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeControlPointsRegistered, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeControlPointsRegistrationStarted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeVerdictEnvelopeMismatch, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypePolicyExpressionExceededCost, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeAgentStarted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentReady, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentOutputChunk, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentCompleted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentHeartbeat, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentRateLimitStatus, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSessionLogLocation, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSkillsProvisioned, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeHandlerCapabilities, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentWarningSilentHang, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentResumedAfterWarning, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentSoftTerminating, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentHardTerminating, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeLaunchInitiated, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentReadyTimeout, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-a2okh: post-agent_ready hang-detector (fail-fast when implementer hangs after becoming ready).
	{TypeName: EventTypePostAgentReadyHang, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeLifecycleTransition, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-fra5l: launch-diagnostic events (pasteinject_failed, launch_stall_detected).
	{TypeName: EventTypePasteInjectFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeLaunchStallDetected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-1s1or: agent_ready_stall_detected — launch_initiated → agent_ready blind-spot detector.
	{TypeName: EventTypeAgentReadyStallDetected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-4l7zs: spawn-cap-blocked diagnostic (slot-leak signature).
	{TypeName: EventTypeSpawnCapBlocked, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-9vp51: implementer-budget-exceeded diagnostic (commit-budget kill).
	{TypeName: EventTypeImplementerBudgetExceeded, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-368i4: implementer-no-work-suspected detector (no commit + clean worktree + sub-floor duration).
	{TypeName: EventTypeImplementerNoWorkSuspected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-da3rr: reviewer-budget-exceeded diagnostic (diff-scaled verdict-budget kill).
	{TypeName: EventTypeReviewerBudgetExceeded, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-r1rup: tmux-new-window-timeout diagnostic (hung `tmux new-window`).
	{TypeName: EventTypeTmuxNewWindowTimeout, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-tu48u: codex positive billing guard (C3/T11) — forced ChatGPT login.
	{TypeName: EventTypeCodexBillingGuard, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-l1bkp: Pi fail-closed billing guard (PI-040/042/043) — absent provider key → deny.
	{TypeName: EventTypePiBillingGuard, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-djqc9: agent-comms typed events (agent-comms spec §1).
	{TypeName: EventTypeAgentMessage, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeAgentPresence, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-lr5t: harness-selected observability event (dispatch-time harness selection audit).
	{TypeName: EventTypeHarnessSelected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-eval-prog-model-on-log-bh2o7: model-selected observability event (effective model keyed on run_id).
	{TypeName: EventTypeModelSelected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-8ziid.2: provider-selected observability event (resolved Pi provider keyed on run_id).
	{TypeName: EventTypeProviderSelected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-33p: hitl-decisions typed events (hitl-decisions SPEC §1, component K1).
	{TypeName: EventTypeDecisionNeeded, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeDecisionResolved, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeDecisionWithdrawn, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeBudgetAccrual, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeBudgetWarning, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeBudgetExhausted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeWorkspaceCreated, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeWorkspaceLeased, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeWorkspaceDiscarded, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeWorkspaceInterrupted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeWorkspaceMergeStatus, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeReconciliationStarted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReconciliationCompleted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReconciliationMismatchObserved, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReconciliationCategoryAssigned, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReconciliationVerdictEmitted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReconciliationVerdictExecuted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReconciliationVerdictMalformed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReconciliationVerdictStale, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReconciliationVerdictExecutionRetry, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReconciliationBudgetExhausted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReconciliationDispatchDeduplicated, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeReconciliationDetectorPanic, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeBeadTerminalTransitionRecovered, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeOperatorEscalationRequired, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeDivergenceInconclusive, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeStoreDivergenceDetected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeDaemonStarted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeDaemonReady, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeDaemonShutdown, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeDaemonDegraded, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeDaemonStartupFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeDaemonOrphanSweepCompleted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeOperatorUpgradeRejected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeOperatorUpgradeCompleted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeOperatorUpgrading, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeOperatorStopped, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeOperatorResuming, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeOperatorPauseStatus, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeOperatorCommandFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeOperatorCommandRejected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeOperatorEscalationCleared, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeInfrastructureUnavailable, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeDaemonConfig, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeMergeConflictEscalation, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeDispatchDeferred, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-rnkuy: supervisor_revival (§8.7.20) — emitted at daemon startup when the
	// prior session ended without daemon_shutdown (SIGKILL, OOM, panic).
	{TypeName: EventTypeSupervisorRevival, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeConsumerFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeDeadLetterEnqueued, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeBusOverflow, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeMetric, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeRedactionFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeBeadLabelConflict, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeBeadClaimSkipped, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeQueueSubmitted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeQueueGroupStarted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeQueueGroupCompleted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeQueuePaused, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeQueueAppended, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeQueueItemDeferredForLedgerDep, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeQueueItemReconciled, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeQueueItemHeldForHandlerPause, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeCrossQueueCollision, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeHandlerPaused, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeHandlerResumed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeRunStale, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeSessionKeeperWarn, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSessionKeeperNoGauge, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// Phase-2 cycle events (hk-22i70):
	{TypeName: EventTypeSessionKeeperHandoffStarted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSessionKeeperCycleComplete, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSessionKeeperCycleAborted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSessionKeeperCycleParked, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSessionKeeperClearUnconfirmed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// Phase-2 crash-recovery (hk-kct9t):
	{TypeName: EventTypeSessionKeeperCycleRecovered, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// Phase-2 PreCompact backstop (hk-aalsm):
	{TypeName: EventTypeSessionKeeperPrecompactBlocked, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-3w2: supervised respawn path.
	{TypeName: EventTypeSessionKeeperRespawnAttempted, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-6qf: operator-attached guard (warn-only suppression).
	{TypeName: EventTypeSessionKeeperOperatorAttached, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-wjzf: captain-initiated restart-now blocked diagnostic (ON-059).
	{TypeName: EventTypeSessionKeeperRestartNowBlocked, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// SK-030: successful agent-run restart-now, nonce carried for audit.
	{TypeName: EventTypeSessionKeeperRestartNow, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeSessionKeeperBlind, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-34ac: session_keeper_hard_ceiling — SID-independent restart at 280K tokens.
	{TypeName: EventTypeSessionKeeperHardCeiling, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-ee81: session_keeper_idle_crew — crew is idle with context below 150K idle-restart floor.
	{TypeName: EventTypeSessionKeeperIdleCrew, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-4pnv: session_keeper_config_rejected — keeper refused to start on bad threshold config / flags.
	{TypeName: "session_keeper_config_rejected", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-qgfme: session_keeper_watcher_dead — crew keeper watcher dead post-spawn (flock not acquired within flock_acquire_grace).
	{TypeName: EventTypeSessionKeeperWatcherDead, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-wqdc: session_keeper_live_pane_recover — live-pane recovery attempt after a cleared pane is detected.
	{TypeName: EventTypeSessionKeeperLivePaneRecover, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// hk-wqdc: session_keeper_ack_timeout — ack timeout when keeper sent a clear but received no confirmation.
	{TypeName: EventTypeSessionKeeperAckTimeout, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeSessionKeeperHandoffWritten, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSessionKeeperModelDone, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSessionKeeperClearSent, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: EventTypeSessionKeeperNewSessionUp, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: "agent_input_acked", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	{TypeName: "agent_input_stale", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeReviewGateAnomaly, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeDiskLow, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeGateDefinitionDrift, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// gate_redefined_under_cat_6 (§8.2.14, F): Cat 6 authorized Gate
	// re-evaluation under a drifted definition (CP-038a). Payload: run_id,
	// gate_name, prior_decision, new_decision, cat_6_verdict_id.
	{TypeName: EventTypeGateRedefinedUnderCat6, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeDecisionRequired, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// decision_acknowledged (§8.12.2, F): ACK for a decision_required; unblocks
	// dispatch atomically. Payload: ack_token, subject, ack_method, acked_at.
	{TypeName: EventTypeDecisionAcknowledged, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeStallDetected, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeBeadSyncFailed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// bead_ledger_recovered (§8.BL2, O): Cat-BL2 retry succeeded; ledger back
	// in sync after a bead_sync_failed event per reconciliation/spec.md §8.BL2.
	// Payload: run_id, timestamp.
	{TypeName: EventTypeBeadLedgerRecovered, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// bead_ledger_corrupt (§8.BL2, O): Cat-BL2 retry failed persistently;
	// triggers Cat 6b operator escalation per reconciliation/spec.md §8.BL2.
	// Payload: run_id, error, timestamp.
	{TypeName: EventTypeBeadLedgerCorrupt, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// bead_ledger_conflict_audit (§8.15.2, O): reconciliation-investigator
	// audit batch from .beads/merge-conflicts.log per BL-MRG-003. Payload:
	// run_id, bead_ids, conflicts, timestamp.
	{TypeName: EventTypeBeadLedgerConflictAudit, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// orphaned_child_bead (§8.15.3, O): Cat-BL1 startup sweep emits one event
	// per child bead whose parent:hk-* label has no parent-run merge commit on
	// main (reconciliation/spec.md §8.BL1). Payload: bead_id, parent_id.
	{TypeName: EventTypeOrphanedChildBead, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},

	{TypeName: EventTypeDashboardStale, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
	// dashboard_refreshed: emitted on the transition out of dashboard_stale
	// (captain refreshed dashboard.json, or operator applied the unlock
	// override). Payload: reason, updated_at, detected_at.
	{TypeName: EventTypeDashboardRefreshed, CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
}

// RegisterPayloadCompatEntry adds the compatibility contract for an event
// whose payload is owned by a leaf package. Callers use it from package init,
// next to RegisterEventType, so the payload owner declares both contracts.
func RegisterPayloadCompatEntry(entry PayloadCompatEntry) error {
	if !entry.TypeName.Valid() {
		return fmt.Errorf("core: payload compatibility type must not be empty")
	}
	if entry.CurrentVersion < 1 {
		return fmt.Errorf("core: payload compatibility current version must be >= 1 for %q", entry.TypeName)
	}
	for _, existing := range allPayloadCompatEntries {
		if existing.TypeName == entry.TypeName {
			return fmt.Errorf("core: payload compatibility already registered for %q", entry.TypeName)
		}
	}
	allPayloadCompatEntries = append(allPayloadCompatEntries, entry)
	return nil
}

// LookupPayloadCompatEntry returns the PayloadCompatEntry for the given
// event type name, or (PayloadCompatEntry{}, false) if not declared.
func LookupPayloadCompatEntry(typeName EventType) (PayloadCompatEntry, bool) {
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
