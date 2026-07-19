# M1-5 — daemon workloop coverage baseline

**Purpose:** durable pre-M3 coverage baseline of `internal/daemon` (esp. `beadRunOne`
and `runWorkLoop`) that the **M3 phase-2 parity gate** consumes — after the
run-state-machine (`runexec` reactor) refactor, per-func coverage must not regress
below these numbers.

## How it was produced

```bash
go test ./internal/daemon/ -coverprofile=cov.out \
  -coverpkg=./internal/daemon/... -count=1 -timeout=600s
go tool cover -func=cov.out | grep workloop
```

- Captured **2026-07-14** on branch `phase1-session-restart-substrate` at commit `32791808`.
- Wall time ≈ 587s. Package reported `FAIL` on **3 pre-existing environmental E2E flakes**
  (StopHookE2E ×2, SSHLocalhost) — unrelated to any code-revamp change; coverage still
  computes. Per the code-revamp handoff these are ignored for the baseline.
- **Overall `internal/daemon/...` coverage: 73.5%** (statements). Package `-func` total: 73.6%.

## Key parity-gate anchors

| Function | File:line | Coverage |
|---|---|---|
| `beadRunOne` | workloop.go:3072 | **60.3%** |
| `runWorkLoop` | workloop.go:1508 | **71.8%** |

These two are the load-bearing run-execution functions the M3 `runexec` refactor
touches; they are the primary no-regression floor.

## Full `workloop*.go` per-func table

```
github.com/gregberns/harmonik/internal/daemon/workloop.go:962:   closeBeadWithHistoryTrim            100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:1033:  newLocalRunRegistry                100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:1043:  newWorkLoopDeps                     81.2%
github.com/gregberns/harmonik/internal/daemon/workloop.go:1207:  buildWorkerRegistry                100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:1219:  buildWorkerRegistryWithRunner      100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:1264:  bootHealthRunner                    85.7%
github.com/gregberns/harmonik/internal/daemon/workloop.go:1280:  resolveTargetBranch                100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:1364:  effectiveQueueWorkers              100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:1398:  selectNextQueue                     81.1%
github.com/gregberns/harmonik/internal/daemon/workloop.go:1508:  runWorkLoop                         71.8%
github.com/gregberns/harmonik/internal/daemon/workloop.go:3072:  beadRunOne                          60.3%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5448:  isWatcherErrCanceled               100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5472:  noCommitGuardShouldReopen          100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5491:  beadAlreadySubsumedInMain           90.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5526:  beadExplicitlyReopened             100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5565:  autoCloseStaleBlockersOnClaimFailure 80.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5627:  drainCancelledQueue                 88.9%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5654:  workloopSleep                      100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5678:  workloopIdleWait                   100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5696:  scheduleAwareIdleWait               28.6%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5714:  hasEnabledScheduledJob               0.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5738:  productionWorktreeFactory          100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5763:  resolveHEAD                          0.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5811:  forceTeardownSession               100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5829:  removeWorktree                     100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5858:  emitPreExecMessage                 100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5871:  preExecMsgType                      75.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5892:  emitPreExecBeforeLaunch            100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5969:  hasAPIKeyInEnv                     100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:5984:  emitRunStarted                      80.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:6003:  emitRunCompleted                    92.3%
github.com/gregberns/harmonik/internal/daemon/workloop.go:6038:  emitImplPresence                    80.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:6058:  resolveOwningEpicFromRecord        100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:6106:  activateFirstPendingGroup            0.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:6200:  activateFirstPendingGroupLocked     66.7%
github.com/gregberns/harmonik/internal/daemon/workloop.go:6264:  evaluateGroupAdvanceWithOutcome     78.5%
github.com/gregberns/harmonik/internal/daemon/workloop.go:6445:  isRetryableMergeReason             100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:6512:  lockedMergeRunBranchToMain         100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:6544:  mergeRunBranchToMain                80.7%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7109:  discardDirtyChurn                   88.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7204:  commitResidualDelta                 82.8%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7306:  cleanUntrackedFiles                 75.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7319:  emitOutcomeEmitted                  80.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7339:  emitWorkingTreeRefreshFailed         0.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7361:  isMergeBuildColdCacheError         100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7372:  emitMergeBuildFailed                87.5%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7396:  emitBeadSyncFailed                  87.5%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7429:  runMergeFmtCheck                    21.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7573:  readGoModule                        75.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7591:  emitBeadClosed                      80.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7606:  emitBeadClosedAndMaybeEpic         100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7617:  maybeEmitEpicCompleted              25.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7695:  snapshotUntrackedFiles              90.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7714:  parsePorcelainPaths                 83.3%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7763:  checkMainWorkingTreeDirty           93.3%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7795:  filterIgnoredPaths                  45.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7830:  isHarmonikChurn                    100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7856:  emitImplementerPhaseComplete        91.7%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7888:  emitSpawnCapBlocked                  0.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7921:  emitTmuxNewWindowTimeout            70.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7950:  artifactAgentType                  100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7961:  emitAgentReadyTimeout               71.4%
github.com/gregberns/harmonik/internal/daemon/workloop.go:7994:  transitionToTerminated              90.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:8022:  emitWorkloopLifecycleTransition     87.5%
github.com/gregberns/harmonik/internal/daemon/workloop.go:8046:  emitImplementerEscapedWorktree       0.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:8064:  extractTmuxAdapterFromSubstrate    100.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:8079:  adoptLiveRunSession                 80.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:8151:  sandboxOSTmpDirs                    80.0%
github.com/gregberns/harmonik/internal/daemon/workloop.go:8170:  strandedBeadHasOnDiskRun            88.9%
github.com/gregberns/harmonik/internal/daemon/workloop_handlerpause_kac8g.go:25:  heldDedupKey             100.0%
github.com/gregberns/harmonik/internal/daemon/workloop_handlerpause_kac8g.go:46:  emitHeldEvent            63.2%
github.com/gregberns/harmonik/internal/daemon/workloop_handlerpause_kac8g.go:94:  pruneHeldDedupOnEpochChange 100.0%
github.com/gregberns/harmonik/internal/daemon/workloopeventsource.go:94:   newPerRunEventTap          100.0%
github.com/gregberns/harmonik/internal/daemon/workloopeventsource.go:111:  Subscribe                  100.0%
github.com/gregberns/harmonik/internal/daemon/workloopeventsource.go:122:  fanOut                     100.0%
github.com/gregberns/harmonik/internal/daemon/workloopeventsource.go:145:  Emit                       100.0%
github.com/gregberns/harmonik/internal/daemon/workloopeventsource.go:169:  EmitWithRunID              100.0%
github.com/gregberns/harmonik/internal/daemon/workloopeventsource.go:195:  newChanAgentEventSource    100.0%
github.com/gregberns/harmonik/internal/daemon/workloopeventsource.go:209:  Events                      90.9%
```
