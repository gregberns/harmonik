# Proposed package map for `internal/core`

Assessment for [`tasks/core-cluster-map.md`](tasks/core-cluster-map.md) (`hk-core-cluster-map-l6rq9`). This is a map only. No Go file moves in this change.

Measured 2026-08-24 against the current worktree: **239 production Go files**, **210 test Go files**, and **571 distinct exported symbols selected through an import of `internal/core` outside that package**. The inventory below assigns each production file exactly once. Counts after public symbols are selector occurrences, not declaration counts.

## Method and limits

The file graph comes from Go syntax, not text search. A file-level edge exists when one production file uses a package identifier declared by another production file. Package edges are those file edges collapsed through the proposed ownership table. The external surface comes from selector expressions whose import resolves to `github.com/gregberns/harmonik/internal/core`; import aliases are included. This is a source dependency map. Reflection and JSON field-name coupling are not visible to it.

The proposal keeps the event wire catalog together for the first move. Splitting event payloads by business domain immediately would make the registry import every domain and multiply the cycles already present. A later split may still be right, but it needs a registry design decision.

## Proposed boundaries

### `core/event` — 34 files

Defines the event envelope, type registry, subscriptions, and the complete wire-payload catalog.

`agentevents_hqwn59.go`, `agentinputevents.go`, `budgetevents_hqwn59.go`, `busevents_hqwn59.go`, `cpevents_hqwn59.go`, `daemonevents_hqwn59.go`, `event.go`, `event_payload.go`, `eventdispatch.go`, `eventenvelope.go`, `eventid.go`, `eventidgen.go`, `eventidghwm.go`, `eventpattern.go`, `eventpayloads_u3q6o.go`, `eventreg_hqwn59.go`, `eventreg_wkzlc.go`, `eventregistry.go`, `eventtype.go`, `gateevents_hqwn59.go`, `guardevents_hqwn59.go`, `handlerpauseevents_ifqnj.go`, `hookevents_hqwn59.go`, `jsonlformat_hqwn58.go`, `keeperevents.go`, `pertypecompat_hqwn38.go`, `queueevents_extqueue.go`, `reconciliationevents_hqwn59.go`, `reviewloopevents_hk7om2q4.go`, `schemachangekind_hqwn39.go`, `subscription.go`, `transitioneventpayload.go`, `wfevents_hk_zqr6f.go`, `workspaceevents_hqwn59.go`.

### `core/reconcile` — 21 files

Models reconciliation evidence, verdict discovery, staleness, execution, override, and release claims.

`checkpoint.go`, `checkpointwrittenpayload.go`, `detectorclass.go`, `divergencecorroboration.go`, `gateverdictrecord.go`, `hookverdictrecord.go`, `malformedverdictpayload.go`, `reconciliationcategory.go`, `releaseclaim.go`, `releaseclaimcheckpoint.go`, `staledivergencereason.go`, `staleverdictpayload.go`, `verdict.go`, `verdictdiscovery_rc026.go`, `verdictevent.go`, `verdictexecutedpayload.go`, `verdictexecution_rc025.go`, `verdictoverride_rc027.go`, `verdictretrycap_rc026a.go`, `verdictstalenesscheck_rc024.go`, `wipcapture_rc019.go`.

### `core/policy` — 29 files

Evaluates budgets, permissions, freedom profiles, and policy limits.

`budgetcategorydefaults_on047.go`, `budgetcounterstate_hka8bg25.go`, `budgetdispatchcheck_hka8bg22.go`, `budgetexhaustedpayload.go`, `budgetexhaustion_on048.go`, `budgetexhaustion_rc018.go`, `budgetouterbound_hka8bg27.go`, `budgetpayload.go`, `budgetref.go`, `budgetresource.go`, `budgetscope.go`, `budgettightest_hka8bg21.go`, `budgetwarning_hka8bg24.go`, `clearancereason.go`, `configprecedence_hka8bg38.go`, `costbasis.go`, `defaultroles_hka8bg29.go`, `freedomprofile.go`, `freedomprofileref.go`, `freedomprofiletightest_hka8bg33.go`, `permissionschema.go`, `policydocument.go`, `policyengine.go`, `policyexpression.go`, `policyexprevaluator.go`, `policyref.go`, `policyversion.go`, `ratelimitsource.go`, `shedpolicy.go`.

### `core/control` — 23 files

Models control points, gates, hooks, and declared side effects.

`controlpoint.go`, `cp011_gate_cognition_s01.go`, `cp017_hook_cognition_s05.go`, `cpregistry_hka8bg2.go`, `dashboardgatepayload_hkxg6rw.go`, `gateaction.go`, `gatedecisionpayload_2fknq.go`, `gatedecisionrecorded_jtxnr.go`, `gatepayload.go`, `gatependingrecord.go`, `gateref.go`, `gateresolutionsignal.go`, `gatesubtype.go`, `hookname.go`, `hookpayload.go`, `hooktrigger_hka8bg12.go`, `idempotencyclass.go`, `idempotencykey.go`, `reviewgateanomaly_hktnmjy.go`, `s02_hka8bg45.go`, `s02registrar_hka8bg45.go`, `sideeffect.go`, `sideeffectkind.go`.

### `core/workflow` — 38 files

Defines workflow graphs, nodes, state, transitions, and subworkflow expansion.

`actiondescriptor.go`, `attachpoint.go`, `dependencyedge.go`, `edge.go`, `edgecascade.go`, `edgecascade_em042.go`, `edgekind.go`, `evaluator.go`, `lifecycletransitionpayload_hxrygh.go`, `node.go`, `nodedispatchdecidedpayload.go`, `nodedispatchpayload.go`, `nodeid.go`, `noderole.go`, `nodetype.go`, `state.go`, `stateid.go`, `statelifecyclepayload.go`, `subworkflowacyclic.go`, `subworkflowenteredpayload.go`, `subworkflowexitedpayload.go`, `subworkflowexpansion.go`, `subworkflowexpansionpin.go`, `subworkflownamespaceid.go`, `subworkflowref.go`, `transition.go`, `transitionaudit.go`, `transitionid.go`, `transitionidgen.go`, `transitionkind.go`, `transitionpath.go`, `transitionrecord.go`, `trigger.go`, `workflow.go`, `workflowclass.go`, `workflowid.go`, `workflowmode.go`, `workflowversion.go`.

### `core/runtime` — 19 files

Models runs, agents, sessions, launch and lifecycle state, and billing guards.

`agentcommspayloads_djqc9.go`, `agentlifecyclepayloads_gjyks.go`, `agentreadystall_hk1s1or.go`, `agenttype.go`, `codexbillingguard_hktu48u.go`, `cyclecounter.go`, `implementernowork_hk368i4.go`, `implementerphasecomplete_hkcd8yu.go`, `interruptstate.go`, `launchdiag_hkfra5l.go`, `pibillingguard.go`, `run.go`, `runcontextkeys.go`, `runid.go`, `runstartedpayload.go`, `runstartedread.go`, `runterminalpayload.go`, `sessionid.go`, `stalldetected_hkl087e.go`.

### `core/workspace` — 13 files

Names workspaces and describes repository, worktree, and local file state.

`commitrange.go`, `gitinprogressop.go`, `harmonikdirmode.go`, `harmonikwritestatus.go`, `leaselockfile.go`, `pathglob.go`, `projecthash.go`, `snapshottoken.go`, `workspaceid.go`, `workspaceobservation.go`, `workspaceref.go`, `workspacestate.go`, `worktreelocaledits_hk7qmpp.go`.

### `core/ledger` — 3 files

Models beads, dependency edges, coarse status, and queue event data.

`beadid.go`, `beadrecord.go`, `coarsestatus.go`.

### `core/observe` — 10 files

Carries metrics, traces, dashboard health, disk pressure, evidence, and redaction rules.

`disklowpayload_hksxlb.go`, `evidence.go`, `metriclabels.go`, `metricname.go`, `metricunit.go`, `redaction.go`, `redactionregistry.go`, `trace.go`, `tracecontext.go`, `verifiermetrics.go`.

### `core/operation` — 18 files

Classifies outcomes, failures, terminal operations, and infrastructure or operator conditions.

`deadlettersink.go`, `durability.go`, `errorcategory.go`, `failureclass.go`, `failureclass_107gz.go`, `failuremode.go`, `infrastructureprerequisite.go`, `onpanic.go`, `operatorcommand.go`, `operatorupgraderejectedreason.go`, `outcome.go`, `outcomeaction.go`, `outcomeemittedpayload.go`, `outcomekind.go`, `outcomestatus.go`, `pgid.go`, `terminalop.go`, `terminalstate.go`.

### `core/unassigned` — 31 files

Cross-cutting vocabulary that has no clear owner until consumers are separated.

These files are explicitly unassigned instead of forced into a false package. The role and name types are shared vocabulary. `cognitionmeta` joins policy, workflow, and agent concepts. `kind` and `kindpayload` are generic tagged-value machinery. `trailers` parses a commit convention. The split task must place this residue after consumer moves reveal a direction, or keep a small compatibility root.

`actorrole.go`, `axistags.go`, `cognitionmeta.go`, `consumerclass.go`, `contextrestoreenforce_em046.go`, `daemondegradedreason.go`, `daemonstatus.go`, `decisionpayloads_33p.go`, `delegationpath.go`, `dispatchdeferredreason.go`, `guardpayload.go`, `handlerref.go`, `inputenvelope_cp040a.go`, `intentlogentry.go`, `investigatorinput.go`, `kind.go`, `kindpayload.go`, `malformationreason.go`, `modetag.go`, `retrycounter.go`, `role.go`, `rolename.go`, `scopetarget.go`, `skillname.go`, `skillunion_cp050.go`, `skillversion.go`, `subsystemregistry_hqwn44.go`, `suiteid.go`, `templateparams.go`, `toolname.go`, `trailers.go`.

## Dependency graph

Arrow means “uses after the proposed move.” The graph is the direct collapse of 602 file-level identifier edges.

```text
event -> control, ledger, observe, operation, policy, reconcile, runtime, unassigned, workflow, workspace
reconcile -> control, ledger, operation, runtime, unassigned, workflow, workspace
policy -> control, event, operation, reconcile, runtime, unassigned, workflow, workspace
control -> event, ledger, operation, policy, reconcile, runtime, unassigned, workflow, workspace
workflow -> control, event, ledger, observe, operation, policy, reconcile, runtime, unassigned, workspace
runtime -> event, ledger, operation, workflow, workspace
workspace -> ledger, runtime
ledger -> operation, workflow
observe -> operation, policy, reconcile, unassigned, workflow
operation -> control, event, reconcile, runtime, unassigned, workflow
unassigned -> control, event, ledger, observe, operation, policy, reconcile, runtime, workflow, workspace
```

### Cycles

1. **control ↔ event ↔ ledger ↔ observe ↔ operation ↔ policy ↔ reconcile ↔ runtime ↔ unassigned ↔ workflow ↔ workspace.** These packages form one strongly connected component in the current code. The direct graph above names its edges. The split task must break or stage them; this map does not invent an interface or merge packages to conceal the cycle.

The graph has 74 direct proposed-package edges and 1 non-trivial strongly connected component. “Unassigned” participates because those files are real dependencies even though they are not a good final package.

## External public surface by proposed owner

Every selector observed outside `internal/core` is listed once under its declaring file's proposed owner. No observed selector lacked a declaration in the 239 production files.

### `core/event` — 293 symbols

AgentInputAckedPayload (2), AgentRateLimitStatus (3), AgentRateLimitStatusActive (13), AgentRateLimitStatusCleared (4), AgentRateLimitStatusPayload (10), AgentReadyPayload (4), AgentReadyTimeoutPayload (3), AllPayloadSchemaVersions (5), BeadLabelConflictPayload (4), BeadLedgerConflict (5), BeadLedgerConflictAuditPayload (1), BeadLedgerCorruptPayload (1), BeadLedgerRecoveredPayload (1), BeadSyncFailedPayload (2), BudgetAccrualPayload (5), BudgetExhaustedEventPayload (5), CrossQueueCollisionCompleted (3), CrossQueueCollisionDisposition (1), CrossQueueCollisionFailed (1), CrossQueueCollisionPayload (6), CrossQueueCollisionRefused (5), DaemonConfigPayload (1), DaemonDegradedPayload (5), DaemonOrphanSweepCompletedPayload (2), DaemonReadyPayload (3), DaemonShutdownPayload (4), DaemonStartedPayload (2), DaemonStartupFailedPayload (4), DispatchObservational (1), DispatchUnknownEventError (2), DivergenceInconclusivePayload (1), DivergenceInconclusiveReasonAuthorityUnavailable (4), ErrSchemaVersionMismatch (1), ErrSkipUnknown (1), ErrUnknownEventType (7), Event (287), EventEnvelope (64), EventID (160), EventIDGenerator (4), EventPattern (64), EventPayload (41), EventType (476), EventTypeAgentCompleted (2), EventTypeAgentFailed (4), EventTypeAgentHeartbeat (27), EventTypeAgentMessage (3), EventTypeAgentOutputChunk (1), EventTypeAgentPresence (1), EventTypeAgentRateLimitStatus (5), EventTypeAgentReady (38), EventTypeAgentReadyStallDetected (2), EventTypeAgentReadyTimeout (14), EventTypeAgentResumedAfterWarning (2), EventTypeAgentStarted (1), EventTypeAgentWarningSilentHang (2), EventTypeBeadClaimSkipped (2), EventTypeBeadClosed (27), EventTypeBeadLabelConflict (22), EventTypeBeadLedgerConflictAudit (1), EventTypeBeadLedgerCorrupt (1), EventTypeBeadLedgerRecovered (1), EventTypeBeadSyncFailed (5), EventTypeBudgetAccrual (12), EventTypeBudgetExhausted (5), EventTypeCheckpointWritten (1), EventTypeCodexBillingGuard (3), EventTypeCrossQueueCollision (3), EventTypeDaemonConfig (1), EventTypeDaemonDegraded (6), EventTypeDaemonOrphanSweepCompleted (5), EventTypeDaemonReady (1), EventTypeDaemonShutdown (3), EventTypeDaemonStarted (12), EventTypeDaemonStartupFailed (3), EventTypeDashboardRefreshed (2), EventTypeDashboardStale (3), EventTypeDecisionAcknowledged (1), EventTypeDecisionNeeded (13), EventTypeDecisionRequired (2), EventTypeDecisionResolved (13), EventTypeDecisionWithdrawn (15), EventTypeDiskLow (1), EventTypeDivergenceInconclusive (2), EventTypeEpicCompleted (13), EventTypeGateDecisionRecorded (3), EventTypeGateDefinitionDrift (1), EventTypeGateRedefinedUnderCat6 (1), EventTypeGovernorSignal (9), EventTypeHandlerCapabilities (8), EventTypeHandlerPaused (4), EventTypeHandlerResumed (8), EventTypeHarnessSelected (3), EventTypeHookFired (4), EventTypeHookVerdictPersisted (3), EventTypeImplementerBudgetExceeded (4), EventTypeImplementerNoWorkSuspected (6), EventTypeImplementerPhaseComplete (7), EventTypeImplementerResumed (9), EventTypeInfrastructureUnavailable (7), EventTypeLaunchInitiated (26), EventTypeLaunchStallDetected (3), EventTypeLifecycleTransition (9), EventTypeLivenessHalt (2), EventTypeMergeBuildFailed (5), EventTypeMetric (1), EventTypeModelSelected (2), EventTypeNoProgressDetected (4), EventTypeNodeDispatchDecided (2), EventTypeNodeDispatchRequested (4), EventTypeOperatorEscalationRequired (5), EventTypeOperatorPauseStatus (10), EventTypeOperatorResuming (9), EventTypeOperatorUpgradeCompleted (1), EventTypeOrphanedChildBead (1), EventTypeOutcomeEmitted (20), EventTypePasteInjectFailed (1), EventTypePiBillingGuard (3), EventTypePolicyExpressionExceededCost (1), EventTypePostAgentReadyHang (2), EventTypeProviderSelected (7), EventTypeQueueAppended (7), EventTypeQueueGroupCompleted (11), EventTypeQueueGroupStarted (5), EventTypeQueueItemDeferredForLedgerDep (4), EventTypeQueueItemHeldForHandlerPause (4), EventTypeQueueItemReconciled (3), EventTypeQueuePaused (10), EventTypeQueueSubmitted (5), EventTypeReconciliationCompleted (5), EventTypeReconciliationDetectorPanic (4), EventTypeReconciliationMismatchObserved (7), EventTypeReconciliationStarted (8), EventTypeReconciliationVerdictEmitted (3), EventTypeReconciliationVerdictExecuted (3), EventTypeReconciliationVerdictMalformed (2), EventTypeReconciliationVerdictStale (2), EventTypeResourceBreach (10), EventTypeReviewBypassed (7), EventTypeReviewFixupStalled (1), EventTypeReviewGateAnomaly (2), EventTypeReviewLoopCycleComplete (5), EventTypeReviewerBudgetExceeded (2), EventTypeReviewerLaunched (7), EventTypeReviewerVerdict (20), EventTypeRunCompleted (76), EventTypeRunFailed (60), EventTypeRunStale (11), EventTypeRunStarted (77), EventTypeSessionKeeperAckTimeout (9), EventTypeSessionKeeperBlind (8), EventTypeSessionKeeperClearSent (23), EventTypeSessionKeeperClearUnconfirmed (13), EventTypeSessionKeeperCycleAborted (24), EventTypeSessionKeeperCycleComplete (64), EventTypeSessionKeeperCycleParked (24), EventTypeSessionKeeperCycleRecovered (6), EventTypeSessionKeeperHandoffStarted (81), EventTypeSessionKeeperHandoffWritten (25), EventTypeSessionKeeperHardCeiling (12), EventTypeSessionKeeperIdleCrew (7), EventTypeSessionKeeperLivePaneRecover (7), EventTypeSessionKeeperModelDone (24), EventTypeSessionKeeperNewSessionUp (15), EventTypeSessionKeeperNoGauge (12), EventTypeSessionKeeperOperatorAttached (10), EventTypeSessionKeeperPrecompactBlocked (11), EventTypeSessionKeeperRespawnAttempted (7), EventTypeSessionKeeperRestartNow (2), EventTypeSessionKeeperWarn (24), EventTypeSessionKeeperWatcherDead (1), EventTypeSessionLogLocation (4), EventTypeSkillsProvisioned (1), EventTypeSpawnCapBlocked (5), EventTypeStaleOpenBeadDetected (1), EventTypeStallDetected (4), EventTypeStateEntered (1), EventTypeStateExited (1), EventTypeSubWorkflowEntered (12), EventTypeSubWorkflowExited (10), EventTypeSupervisorRevival (1), EventTypeTmuxNewWindowTimeout (6), EventTypeTransitionEvent (2), EventTypeVerdictEnvelopeMismatch (2), EventTypeWorkerOffline (4), EventTypeWorkerReport (8), EventTypeWorkerTunnelFailed (11), EventTypeWorkerUnhealthy (9), EventTypeWorkingTreeLocalEditsOverwritten (3), EventTypeWorkingTreeRefreshFailed (5), EventTypeWorkspaceMergeStatus (12), EventsJSONLPath (3), HandlerCapabilitiesPayload (2), HandlerPauseCause (25), HandlerPausedPayload (5), HandlerResumedBy (2), HandlerResumedByAutoBackoff (2), HandlerResumedByOperator (14), HandlerResumedBySignal (4), HandlerResumedPayload (8), HarnessSelectedPayload (3), HookFailedPayload (3), HookFiredPayload (5), HookVerdictPersistedPayload (2), ImplementerResumedPayload (3), InfrastructureUnavailablePayload (4), IsHWMClockRegression (1), LaunchInitiatedPayload (2), LivenessHaltPayload (1), LookupPayloadCompatEntry (3), LookupTypeSchemaVersion (7), MergeConflictEscalationPayload (7), ModelSelectedPayload (4), NewEventIDGenerator (9), NewEventIDGeneratorWithHWM (1), NoProgressDetectedPayload (1), OperatorEscalationReasonCat6bAutoEscalated (1), OperatorEscalationReasonMergeConflict (1), OperatorEscalationReasonOtherVerdictDriven (2), OperatorEscalationRequiredPayload (4), OperatorPauseStatusPayload (5), OperatorPauseStatusValue (3), OperatorPauseStatusValuePaused (7), OperatorPauseStatusValuePausing (13), OperatorResumingPayload (6), OrphanedChildBeadPayload (1), PayloadCompatEntry (7), PayloadHasBeadID (1), ProviderSelectedPayload (2), QueueAppendedPayload (4), QueueGroupCompletedPayload (12), QueueGroupStartedPayload (4), QueueItemDeferredForLedgerDepPayload (4), QueueItemHeldForHandlerPausePayload (2), QueueItemReconciledPayload (1), QueuePausedPayload (11), QueueSubmittedPayload (5), ReadEventIDHWM (1), ReconciliationCompletedPayload (2), ReconciliationDetectorPanicPayload (2), ReconciliationMismatchObservedPayload (6), ReconciliationStartedPayload (4), ReconciliationTriggerScheduled (4), ReconciliationTriggerStartup (3), ReconciliationVerdictEmittedPayload (1), RegisterEventType (8), RegisterPayloadCompatEntry (7), ReviewBypassedPayload (3), ReviewFixupStalledPayload (1), ReviewLoopCompletionReasonApproved (1), ReviewLoopCycleCompletePayload (2), ReviewerLaunchedPayload (1), ReviewerVerdict (2), ReviewerVerdictApprove (1), ReviewerVerdictPayload (1), RunStalePayload (11), RunStaleSnapshot (1), ScanRegisteredPayloadsForSecretFields (1), SealEventRegistry (1), SessionKeeperAckTimeoutPayload (3), SessionKeeperBlindPayload (2), SessionKeeperClearSentPayload (8), SessionKeeperClearUnconfirmedPayload (4), SessionKeeperConfigRejectedPayload (1), SessionKeeperCycleAbortedPayload (7), SessionKeeperCycleCompletePayload (15), SessionKeeperCycleParkedPayload (7), SessionKeeperCycleRecoveredPayload (4), SessionKeeperHandoffStartedPayload (4), SessionKeeperHandoffWrittenPayload (7), SessionKeeperHardCeilingPayload (4), SessionKeeperLivePaneRecoverPayload (3), SessionKeeperModelDonePayload (9), SessionKeeperNewSessionUpPayload (5), SessionKeeperNoGaugePayload (9), SessionKeeperOperatorAttachedPayload (2), SessionKeeperPrecompactBlockedPayload (3), SessionKeeperRespawnAttemptedPayload (2), SessionKeeperRestartNowPayload (2), SessionKeeperWarnPayload (3), SessionKeeperWatcherDeadPayload (1), SessionLogLocationPayload (4), ShutdownModeGraceful (1), ShutdownModeImmediate (1), SkillsResolvedPayload (3), StaleOpenBeadDetectedPayload (1), Subscription (99), SupervisorRevivalCauseUnexpectedExit (1), SupervisorRevivalPayload (1), ValidateEnvelopeSchemaVersion (1), VerdictEnvelopeMismatchPayload (1), WorkspaceMergeStatusMerged (3), WorkspaceMergeStatusPayload (3), WriteEventIDHWMAtomicNoSync (1).

### `core/reconcile` — 37 symbols

CheckVerdictStaleness (1), Checkpoint (2), DetectorClass (10), HookVerdictRecord (28), MalformedVerdictPayload (1), OperatorVerdictOverrideRequest (8), PlanForVerdict (1), ReconciliationCategory (14), ReconciliationCategoryCat5 (3), ReconciliationCategoryCat6b (4), Verdict (6), VerdictActionKindAcceptCloseWithNote (1), VerdictActionKindDispatchCurrentNode (1), VerdictActionKindEscalateToHuman (1), VerdictActionKindNoOp (1), VerdictActionKindReopenBead (1), VerdictActionKindResetToCheckpoint (1), VerdictEscalateToHuman (2), VerdictEvent (8), VerdictExecutedPayload (1), VerdictExecutionAttemptRecord (4), VerdictExecutionPlan (1), VerdictNoOpAccept (6), VerdictOverrideDecision (1), VerdictOverrideDecisionConfirm (2), VerdictOverrideDecisionVeto (2), VerdictReopenBead (2), VerdictResetToCheckpoint (7), VerdictResumeHere (5), VerdictResumeWithContext (6), VetoPromotion (1), VetoPromotionEscalateToHuman (1), VetoPromotionNone (1), WIPCapture (8), WIPCaptureDiffFile (4), WIPCaptureStatusFile (4), WIPCaptureUntrackedFile (4).

### `core/policy` — 12 symbols

BudgetRef (9), BudgetScopeHandlerAccount (1), CostBasisOutputBytes (6), DefaultPolicyExprEvaluatorConfig (2), NewPolicyExprEvaluator (2), NoOpPolicyEngine (1), PolicyDocument (5), PolicyEngine (1), PolicyExprEvaluator (1), PolicyExpression (8), PolicyGate (2), PolicySkillSet (2).

### `core/control` — 22 symbols

ControlPoint (38), DashboardRefreshedPayload (1), DashboardStalePayload (1), GateAction (7), GateActionAllow (13), GateActionDeny (9), GateActionEscalateToHuman (6), GateDecisionPayload (25), GateDecisionRecordedPayload (3), GateRef (25), HookName (8), HookPayload (9), IdempotencyClass (2), IdempotencyClassIdempotent (5), IdempotencyClassNonIdempotent (18), IdempotencyKey (5), Registry (5), ResetBeadIdempotencyKey (2), ReviewGateAnomalyPayload (1), SideEffect (22), SideEffectKind (1), SideEffectKindEmitEvent (15).

### `core/workflow` — 49 symbols

DependencyEdge (30), Edge (6), EdgeKind (3), EdgeKindBlocks (6), EdgeKindParentChild (14), Evaluator (7), HookVerdictFilePath (5), LifecycleTransitionPayload (3), NamespaceNodeID (8), NewSubWorkflowRefGraph (4), NewTransitionIDGenerator (6), NewWorkflowID (19), Node (2), NodeDispatchDecidedPayload (15), NodeDispatchOriginWorkflow (1), NodeDispatchRequestedPayload (1), NodeID (73), NodeType (13), NodeTypeAgentic (14), NodeTypeGate (6), NodeTypeNonAgentic (18), NodeTypeSubWorkflow (13), ReviewPolicy (3), ReviewPolicyNoReview (5), ReviewPolicyReviewed (16), SelectNextEdge (2), StateID (29), SubWorkflowEnteredPayload (4), SubWorkflowExitedPayload (5), SubWorkflowExpansion (5), SubWorkflowExpansionPin (5), SubWorkflowRef (4), SubWorkflowRefGraph (1), TransitionID (167), TransitionRecordPath (1), Trigger (6), WorkflowDescriptor (15), WorkflowID (27), WorkflowMode (68), WorkflowModeDot (214), WorkflowModeRetiredReviewLoop (8), WorkflowModeSingle (84), WorkflowSelectionEmbeddedDefault (13), WorkflowSelectionExplicitRef (5), WorkflowSelectionLegacySingleLabel (3), WorkflowSelectionProjectDefault (3), WorkflowSelectionQueueItemSingleMode (2), WorkflowSelectionSource (6), WorkflowVersion (38).

### `core/runtime` — 61 symbols

AgentMessagePayload (17), AgentPresencePayload (16), AgentPresenceReason (5), AgentPresenceReasonJoin (4), AgentPresenceReasonLeave (5), AgentPresenceReasonRefresh (9), AgentPresenceStatus (6), AgentPresenceStatusOffline (5), AgentPresenceStatusOnline (13), AgentReadyStallDetectedPayload (1), AgentType (341), AgentTypeClaudeCode (157), AgentTypeClaudeTwin (1), AgentTypeCodex (68), AgentTypePi (108), AgentTypeRegexPattern (4), BeadClaimSkippedPayload (2), BeadClosedPayload (1), CodexBillingGuardAllowed (3), CodexBillingGuardDenied (4), CodexBillingGuardMaterialized (3), CodexBillingGuardOutcome (4), CodexBillingGuardPayload (3), CycleCounter (20), DecodeRunStartedForRead (2), EpicCompletedPayload (7), ImplementerBudgetExceededPayload (2), ImplementerNoWorkSuspectedPayload (1), ImplementerPhaseCompletePayload (1), InterruptState (11), InterruptStateDaemonCrashSuspected (11), InterruptStateNone (39), InterruptStateOperatorPaused (12), InterruptStateOperatorStoppedGraceful (9), InterruptStateOperatorStoppedImmediate (7), LaunchStallDetectedPayload (1), MergeBuildFailedPayload (1), NewCycleCounter (143), PasteInjectFailedPayload (1), PiBillingGuardAllowed (6), PiBillingGuardDenied (9), PiBillingGuardOutcome (5), PiBillingGuardPayload (9), ReservedAgentTypes (2), ReviewerBudgetExceededPayload (1), Run (94), RunCompletedPayload (2), RunFailedPayload (2), RunID (760), RunStartedPayload (14), RunStartedReadPayload (1), SessionID (40), SpawnCapBlockedPayload (1), StallDetectedPayload (4), StallSignature (5), StallSignatureHeartbeatGap (3), StallSignatureReviewStall (4), StallSignatureRunAge (3), TmuxNewWindowTimeoutPayload (1), ValidPolicyBinding (1), WorkingTreeRefreshFailedPayload (1).

### `core/workspace` — 15 symbols

HarmonikDirMode (84), LeaseLockFile (20), ProjectHash (68), SnapshotToken (9), WorkingTreeLocalEditsOverwrittenPayload (1), WorkspaceID (3), WorkspaceRef (27), WorkspaceState (28), WorkspaceStateConflictResolving (25), WorkspaceStateCreated (26), WorkspaceStateDiscarded (46), WorkspaceStateLeased (43), WorkspaceStateMergePending (25), WorkspaceStateMerged (20), WorkspaceStateReady (22).

### `core/ledger` — 12 symbols

BeadID (1486), BeadRecord (428), CoarseStatus (41), CoarseStatusBlocked (20), CoarseStatusClosed (54), CoarseStatusDeferred (7), CoarseStatusDraft (8), CoarseStatusInProgress (59), CoarseStatusOpen (132), CoarseStatusPinned (4), CoarseStatusTombstone (13), TerminalCoarseStatuses (1).

### `core/observe` — 5 symbols

DiskLowPayload (1), NewRedactionRegistry (24), RedactByFieldName (1), RedactedSentinel (17), RedactionRegistry (6).

### `core/operation` — 32 symbols

DeadLetterSink (3), ErrorCategory (10), ErrorCategoryDeterministic (12), ErrorCategoryPanic (3), ErrorCategoryTransient (10), FailureClass (19), FailureClassBudgetExhausted (11), FailureClassCanceled (18), FailureClassCompilationLoop (33), FailureClassDeterministic (33), FailureClassStructural (40), FailureClassTransient (25), InfrastructurePrerequisiteQueueWriteError (7), NoopDeadLetterSink (6), OnPanicRecoverAndLog (63), OpenDeadLetterSink (1), Outcome (149), OutcomeActionSideEffect (6), OutcomeEmittedPayload (1), OutcomeKindDefault (46), OutcomeKindGateDecision (6), OutcomeStatus (15), OutcomeStatusFail (66), OutcomeStatusPartialSuccess (2), OutcomeStatusRetry (5), OutcomeStatusSuccess (357), PGID (10), TerminalOp (13), TerminalOpClaim (20), TerminalOpClose (15), TerminalOpReopen (10), TerminalOpReset (10).

### `core/unassigned` — 33 symbols

BaselineAxisTags (6), ConsumerClassAsynchronous (21), ConsumerClassObserver (24), ConsumerClassSynchronous (35), DaemonDegradedReasonCat0PostReady (4), DaemonDegradedReasonClockRegression (1), DaemonDegradedReasonInfrastructureUnavailable (5), DecisionNeededPayload (10), DecisionResolvedPayload (10), DecisionTopicOperatorMailbox (8), DecisionUrgency (4), DecisionUrgencyBlocker (10), DecisionUrgencyFYI (4), DecisionUrgencyQuestion (5), DecisionWithdrawnPayload (12), DecisionWithdrawnReason (2), DecisionWithdrawnReasonOrphaned (5), DecisionWithdrawnReasonSelfObsoleted (4), DelegationPath (9), ErrDuplicateSourceSubsystem (1), HandlerRef (37), IntentLogEntry (23), KindGate (2), KindHook (8), KindPayload (6), LookupTrailer (1), MalformationReasonUnknownVerdictValue (1), ModeTagCognition (8), ModeTagMechanism (10), ReadIntentLogEntry (3), RegisterSourceSubsystem (3), SuiteID (7), ValidateTemplateParams (2).

## Move-order consequence

The cycles make a one-commit physical split unsafe. Start with leaf vocabulary whose outgoing edges are small. Preserve old import paths with aliases only if the split task accepts that temporary cost. Move the event registry after payload ownership is decided. This document records current coupling; it does not claim that the proposed move already compiles.
