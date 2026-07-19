# 03-Research / runexec — C4 + C6 findings (beadRunOne structure map)

> Pass 3 (Research), components C4 (reactor) and C6 (terminal spine). Verified
> against the working tree 2026-07-14. `beadRunOne` = `internal/daemon/workloop.go:3072–5438`
> (next top-level func `isWatcherErrCanceled` :5448); file total 8184 lines.

## 1. Signature census — the 17 parameters

`func beadRunOne(...)` at `workloop.go:3072`:

1. `ctx context.Context` — per-run ctx; cancelled by shutdown or the never-spawned
   reaper; many terminal paths swap to `context.Background()` when `ctx.Err()!=nil`.
2. `deps workLoopDeps` — the 85-field god-struct, BY VALUE.
3. `runID core.RunID`.
4. `beadRecord core.BeadRecord`.
5. `queueName string` (NQ-B1; review-loop-failure budget resolution).
6. `queueID *string` (QM-011/012 stamping; nil for non-queue runs).
7. `queueGroupIndex *int`.
8. `queueItemIndex int`.
9. **`runSucceeded *bool` — OUT-PARAM**, written only by the `emitDone` closure
   (`:3120–:3122`); also read by the Pi-worktree-retention defers (`:3712`, `:4689`).
10. `extraContext string` (hk-boiwe per-item `--context`).
11. `itemWorkflowMode string` (tier-0 override, `:3169`).
12. `itemWorkflowRef string` (`:3178`).
13. `itemTemplateParams map[string]string` (DOT templates).
14. `itemLocalOnly bool` (skip remote fallback `:3398`).
15. `itemWorkerTarget string` (pin worker).
16. `preSelectedWorker *workers.Worker` (hk-hs7ex; non-nil ⇒ remote).
17. `localSlotHeld bool` (hk-hs7ex; drives `relLocalSlot` defer `:3081–:3086`).

**Goroutine wrapper (`:3023–:3049`):** `wg.Add(1)` `:3017`; `var runSucceeded bool`
`:3029` passed as `&runSucceeded` `:3030`; after return: EM-015f
`evaluateGroupAdvanceWithOutcome(…, runSucceeded)` `:3041` (skipped on ctx-cancel —
item left `dispatched` for QM-002a recovery `:3038`); hk-f722
`stagedBeadGeneratorEval` when `runSucceeded && ctx.Err()==nil` `:3047`. Deferred:
`wg.Done`, `runCancel`, `runRegistry.Unregister`.

## 2. Phase map

Every workflow-mode branch EARLY-RETURNS (review-loop `:4012`, dot `:4219`), so
single-mode (`:4226+`) is only the `default` case. The run body blocks on
`waitWithSocketGrace` plus the `pasteInjectQuitOnCommit` watchdog goroutine.

| Phase | Lines | State carried / side effects / failure exits |
|---|---|---|
| P0 prologue/slots | `:3073–:3110` | `relLocalSlot` defer; `runTipSHA *string` (set only by DOT orphan-salvage `:4206`); `owningEpicID/Assignee` (`resolveOwningEpicFromRecord` `:3098` → RunHandle `:3101–:3104`); session-data fields |
| P1 emitDone def | `:3119–:3158` | see §5 |
| P2 config resolve | `:3160–:3251` | mode/ref (`:3168/:3178`), harness (`:3194`, hk-pkugu), model (`:3200`), Pi profile (`:3216`; profErr → reopen+return `:3220–:3226`), provider_selected `:3242–:3251` |
| P3 repo/branching | `:3253–:3349` | cross-repo safelist → reopen+return `:3277–:3283`; `resolveParentCommit`→`headSHA` (err→reopen `:3310`); `resolveBranching` lands_on gate → reopen `:3337–:3348` |
| P4 remote setup | `:3351–:3552` | `remoteBeadCtx`; SelectWorker fallback `:3398–:3433` (corrects localInFlight); reverse tunnel alloc/spawn (`:3461/:3509`) + `waitWorkerSocketLive` 10s → `worker_tunnel_failed` reopen `:3541–:3551`; closures `notifyWorkerOffline` `:3557`, `preMergeSync` `:3574` |
| P5 worktree | `:3592–:3722` | wtFactory select (remote `:3598–:3618` w/ worktreeCreateMu; local `:3620`); under mergeMu `:3636–:3668` `ensureBaseOnWorker` + create; wtErr → **first emitDone(false)** `:3675–:3679`; defers `runpkg.Remove` `:3701`, `wtCleanup` w/ Pi retain gate `:3707–:3722` |
| P6 snapshot+started | `:3724–:3742` | `snapshotUntrackedFiles`→`preRunUntracked` `:3730`; **run_started** `:3742` |
| P7 DOT preload + spec builder | `:3763–:3811` | standard-bead.dot preload `:3772–:3788`; `routedLaunchSpecBuilder` stored `:3797–:3811` |
| P8 mode fork | `:3824–:4224` | **review-loop** `:3825–:4012` (`runReviewLoop` `:3846`; success: gate `:3853` → preMergeSync `:3864` → trailers `:3882` → merge-retry×2 `:3891–:3913` (merge `:3904`) → close `:3923`; failure: budget under `queueStore.LockForMutation` `:3948–:3978`, cap → needs-attention-close vs reopen `:3980–:4010`); **dot** `:4014–:4219` (`driveDotWorkflow` `:4092`; success: preMergeSync `:4106` → trailers `:4118` → merge `:4123` → `alreadyApprovedOnMain` carve-out `:4138–:4151` → close `:4153`; `subsumed` branch `:4165–:4182`; failure: orphan-salvage runTipSHA `:4205` + Reopen(bg) `:4214`) |
| P9 single spec build | `:4226–:4337` | `specBuilder` `:4287` (err→reopen+emitDone `:4288–:4296`); D2 API-key fail-closed `:4322–:4330` |
| P10 substrate/sandbox | `:4332–:4570` | `perRunSubstrate` `:4352` (hk-012af); indep-session `:4376–:4416`; `verifySandboxEngaged` → reopen `:4483–:4497`; `spec.Terminal=true` `:4544` |
| P11 hooks+launch | `:4590–:4669` | `RegisterHookSession` `:4592`; `emitPreExecBeforeLaunch` `:4603`; per-run tap `:4607`; **spawn-sem acquire** select `:4621–:4635` (remote only); **Launch** `:4638` (err → spawn_cap_blocked/tmux_new_window_timeout + reopen `:4639–:4661`); held-back **launch_initiated** `:4667–:4669` |
| P12 post-launch wiring | `:4671–:4796` | `SetMachine` `:4675`; `forceTeardownSession` defer `:4717–:4721`; presence online/offline `:4726–:4729`; `SetAgentReadyCallback`→tap `:4763–:4787`; heartbeat goroutine `:4793–:4796` (300s) |
| P13 waitAgentReady | `:4798–:4916` | readyCtx cancelled on watcher.Done `:4840–:4849`; `waitAgentReady` `:4854` (150s/210s); timeout → Kill + 10s drain + reopen(bg) + agent_ready_timeout + emitDone `:4857–:4909`; ProcessExit harnesses skip |
| P14 brief+watchdog | `:4924–:5000` | `pasteInjectOnLaunch`→`briefDelivered` `:4951`; `noChangeTimeoutCh` `:4996`; watchdog goroutine `pasteInjectQuitOnCommit` `:4998` |
| P15 wait completion | `:5002–:5064` | `waitWithSocketGrace` `:5004` → `socketOutcome, ei`; per-run abort → reopen + emitRunCompleted(false) direct `:5016–:5029`; `transitionToTerminated(bg,…)` `:5036`; substrate Kill `:5045–:5047`; implementer_phase_complete `:5062` |
| P16 ProcessExit commit fallback | `:5066–:5109` | `ensurePiRefsTrailer` `:5090` / `ensureCodexRefsTrailer` `:5099` |
| P17 guards | `:5111–:5228` | `MapWaitReturnToTerminalEvent`→`term` `:5112`; `watcherFailed` `:5136`; escape guard under mergeMu `:5169–:5186`; no-commit guard `:5206–:5228` |
| P18 terminal switch | `:5230–:5437` | see §6 |

## 3. Event census (run-lifecycle path)

provider_selected `:3250`; worker_tunnel_failed `:3494/:3545`; worker_offline
(`notifyWorkerOffline` `:3561`); **run_started** `:3742` (def `:5984`); pre-exec
chain via `emitPreExecBeforeLaunch` `:4603` (def `:5892`, per-msg `:5858`);
**launch_initiated** held-back `:4668`; **agent_ready** via ready-callback tap
`:4781/:4786`; agent_heartbeat (300s, `claudeheartbeat.go:76`); spawn_cap_blocked
`:4647` (def `:7888`); tmux_new_window_timeout `:4654` (def `:7921`);
lifecycle_transition via `transitionToTerminated` `:5036` (def `:7994`/`:8022`);
implementer_phase_complete `:5062` (def `:7856`); agent_presence `:4726/:4728` (def
`:6038`); implementer_escaped_worktree `:5177` (def `:8046`); agent_ready_timeout
`:4906` (def `:7961`); **outcome_emitted** ×16 sites (def `:7319`); bead_closed /
epic_completed via `emitBeadClosedAndMaybeEpic` (def `:7606`/`:7617`, at-most-once
`emittedEpics`); **run_completed/run_failed** via `emitRunCompleted` (def `:6003`,
success switch `:6027–:6030`) — called by emitDone `:3130`, direct on
shutdown-drain `:5380/:5382/:5386` and per-run abort `:5021`. `run_stale` is NOT
emitted here (StaleWatcher, `stalewatch.go:990`). Review-loop events live in
reviewloop.go (reviewer_launched/verdict, implementer_resumed, iteration_cap_hit,
no_progress_detected, review_loop_cycle_complete…); DOT peers in dot_cascade.go.

## 4. Blocking waits & timers (the future timer-events)

| Wait | Line | Bound |
|---|---|---|
| `waitWorkerSocketLive` | `:3541` | `workerSocketReadyTimeout` 10s (`reversetunnel.go:78`) |
| spawn-sem acquire select | `:4622` | unbounded (slot or ctx) |
| `waitAgentReady` | `:4854` | `effectiveAgentReadyTimeout` — local 150s (`agentready.go:64`), remote 210s (`:80`), select impl `agentready.go:191–:206` |
| post-kill watcher drain / `sess.Wait` | `:4869–:4874/:4883` | `agentReadyKillReapTimeout` 10s (`workloop.go:141`) |
| `waitWithSocketGrace` | `:5004` | watcher.Done/ctx.Done + `killWatcherReapGrace` 3s + `stopHookGrace` 3s (`waitsocketgrace.go:40/:60/:86–:136`) |
| `noChangeTimeoutCh` non-blocking read | `:5330–:5331` | closed by watchdog (`pasteinject.go:908/:1046`) |
| watchdog `pasteInjectQuitOnCommit` | `pasteinject.go:829` | `commitPollInterval` 500ms (`:603`), `commitPollTimeout` 30m progress-extended (`:623`), `commitHardCeiling` 90m absolute (`:639`), `heartbeatStalenessThreshold` 8m (`:658`), `launchHeartbeatTimeout` 180s (`:685`), `briefDeliveredTimeout` 2m (`:595`) |
| heartbeat loop | `claudehandler_chb006_024.go:662` | `HeartbeatInterval` 300s (`:49`) |
| never-spawned reaper (external) | `stalewatch.go:86` | 30m; cancels run ctx + sets `handle.aborted` (surfaced `:5016`) |

## 5. The `emitDone` closure (`:3119–:3158`)

Captures: `runSucceeded`, `ctx`, `deps.bus`, `runID`, `beadID`,
`owningEpicID/Assignee`, `queueID`, `queueGroupIndex`, **`runTipSHA` (by
reference)**, `sdStartedAt/sdModel/sdHarness`, `deps.projectDir`. Does: (1)
`*runSucceeded = success`; (2) emitCtx = ctx, swapped to Background if cancelled
(hk-e3fy `:3126–:3129`); (3) `emitRunCompleted(...)` `:3130`; (4) detached
`sessiondata.Collect` goroutine `:3143–:3157` (best-effort). ~40 call sites, all
terminal. Shutdown-drain and per-run-abort BYPASS emitDone (call `emitRunCompleted`
directly, no sessiondata.Collect).

## 6. Terminal spine — the four merge/close blocks + the 6× close ladder

| Block | Lines | Merge | Close |
|---|---|---|---|
| A review-loop success | `:3860–:3935` | `:3904` (retry×2) | `:3923` |
| B dot success | `:4103–:4164` | `:4123` | `:4153` |
| C agent_completed | `:5231–:5275` | `:5252` | `:5263` |
| D exit-0 auto-close | `:5277–:5324` | `:5302` | `:5313` |

**C vs D are near-byte-identical** — differences are ONLY log/summary string labels
("(agent_completed)" vs "(auto-close)"; final summary "agent_completed: stop-hook
outcome" vs "auto-close: exit=0"). Structurally the same block.
**A adds:** merge-retry loop + per-retry trailer re-amend `:3891–:3913`,
`appendReviewTrailersToHEAD` `:3882`, and the separate failure sub-block with the
review-loop-failure budget (`:3936–:4010`).
**B adds:** `alreadyApprovedOnMain` rebase-dropped carve-out `:4138–:4151`,
`subsumed` no-merge close `:4165–:4182`, orphan-salvage runTipSHA `:4205`.
**Shutdown-drain (`:5372–:5402`)** is the genuinely different one: `bgCtx` for
resolve/merge/close, `emitRunCompleted` direct (no emitDone/sessiondata), reached
only when `ctx.Err()!=nil && !useIndepSession` and worktree HEAD advanced `:5373`.

**The shared inner "close ladder"** — `emitOutcomeEmitted("approved")` →
`closeBeadWithHistoryTrim(…)` → on `BrUnavailable` success-transient, on other err
close-error(false), on success `emitBeadClosedAndMaybeEpic` + emitDone(true) — is
duplicated **6×** (A, B, B-subsumed, C, D, noChange-subsumed `:5333–:5344`) with
only string differences. Single largest extraction candidate.

**`transitionToTerminated` (`:7994–:8017`):** drives the hclifecycle Machine
`→ Terminating(ReasonTerminateRequested)` then `→ Terminated | Failed("exit_error")`
based on exitCode/waitErr; each step via `emitWorkloopLifecycleTransition` `:8022`
(silently no-ops invalid transitions). Machine created by the session, stored on
RunHandle `:4675` so the StaleWatcher can drive `Ready→Failed(silent_hang)`.

## 7. External helpers touching shared state

`resolveOwningEpicFromRecord` `:6058`; `resolveWorkflowMode/Ref`
(`moderesolve.go:53/:145`); `ResolveModelPreference` (`modelpreference.go:190`);
`resolvePiProfile` (`pi_profile_resolve.go:59`); `resolveParentCommit`/
`resolveBranching` (`branching.go:401/:128`); `routedLaunchSpecBuilder`
(`harnessregistry.go:134`); `runReviewLoop` (`reviewloop.go:198`);
`driveDotWorkflow` (`dot_cascade.go:181`); `pasteInjectOnLaunch`
(`pasteinject.go:1411`); `pasteInjectQuitOnCommit` (`pasteinject.go:829`);
`waitAgentReady` (`agentready.go:154`); `waitWithSocketGrace`
(`waitsocketgrace.go:86`); `transitionToTerminated` `:7994`;
`lockedMergeRunBranchToMain` `:6512`; `closeBeadWithHistoryTrim` `:962` (method on
`*workLoopDeps`); `emitBeadClosedAndMaybeEpic`/`maybeEmitEpicCompleted`
`:7606/:7617`; `evaluateGroupAdvanceWithOutcome` `:6264` (wrapper-side);
`checkMainWorkingTreeDirty`/`snapshotUntrackedFiles` `:7763/:7695`;
`noCommitGuardShouldReopen`/`beadAlreadySubsumedInMain` `:5472/:5491`;
`runScenarioGateIfNeededVia` (`scenariogate.go:107`); `resolveWorktreeHEADVia`
(`pasteinject.go:506`); reverse-tunnel + worker-git helpers (`reversetunnel.go`);
`ensurePiRefsTrailer`/`ensureCodexRefsTrailer`.

Shared-state handles the reactor design must account for: `mergeMu`,
`worktreeCreateMu`, `localInFlight`, `agentSpawnSem`, `workerRegistry`
(SelectWorker/ReleaseSlot/SetEnabled), `runRegistry` (Machine/aborted/Remote/
attribution), `queueStore` (LockForMutation budget), `hookStore`
(RegisterHookSession/SetAgentReadyCallback), per-run `tap` fan-out.

## 8. Structural conclusion for the reactor design

The function is ALREADY a sequence of guard-gated early returns, each performing one
of a small terminal-action set: `{reopen, emitDone(false/true), close-ladder,
merge-then-close-ladder}`. The three mode branches converge on the same 6×
close ladder and the same merge call. A `Step(state, event) → (state, []action)`
reactor maps cleanly if actions model: Reopen, EmitRunTerminal, EmitOutcome,
CloseBead, SubmitMerge, LaunchAgent, plus the emits — with the blocking waits
(waitAgentReady, waitWithSocketGrace, watchdog) as EVENT SOURCES and
`noChangeTimeoutCh`/`ctx.Done()`/`handle.aborted` as distinct events, mirroring
`internal/keeper/step.go` (pure Step, timers-as-events) + `shell.go` (effector +
drive loop).
