# 03-Research / C6 — Terminal-spine factoring (the 4x merge/close block)

> Pass 3 (Research), component C6. All `file:line` verified against the current
> working tree (branch `phase1-session-restart-substrate`, 2026-07-14),
> `internal/daemon/workloop.go` (8184 lines). Delegated to a fresh-context
> sub-agent; parent (design agent) owns this write.

## Research questions

1. Locate + diff the four launch->gate->merge->close blocks; enumerate every divergence.
2. Is exit-0 auto-close redundant with agent_completed, or a distinct pre-bridge path?
3. What is distinct about the shutdown-drain merge (bgCtx, skipped gates)?
4. `emitDone` closure: definition, captured state, invocation census, reactor-vs-shell split.
5. ReopenBead / closeBeadWithHistoryTrim call-site census in the run-lifecycle region.
6. What `preMergeSync` + the scenario gates do; gate conditions per block.
7. How `transitionToTerminated` (HC-065) interleaves with the four blocks.

## Findings

### Q1 — The four blocks (+ two merge-less close blocks the spine must cover)

| # | Block | Current location | Selected when |
|---|---|---|---|
| A | review-loop success | :3848-3935 (merge **:3904**, in retry loop :3893-3913) | `workflowMode==ReviewLoop && rlResult.success` |
| A' | review-loop failure (budget) | :3936-4011 (close-with-attention :3989) | `ReviewLoop && !success`; close only if `budgetExhausted` |
| B | DOT success | :4097-4164 (merge **:4123**) | `workflowMode==Dot && dotResult.success` |
| B' | DOT noChange-subsumed | :4165-4182 (close :4171, **no merge**) | `Dot && dotResult.subsumed` |
| C | agent_completed (single) | :5231-5275 (merge **:5252**) | `term.Type==ProgressMsgTypeAgentCompleted` (stop-hook WORK_COMPLETE/REVIEWER_VERDICT) |
| D | exit-0 auto-close (single) | :5277-5324 (merge **:5302**) | `socketOutcome==nil && ei.exitCode==0 && !watcherFailed` |
| D' | noChange-subsumed (single) | :5330-5354 (close :5334, **no merge**) | default + noChangeTimeoutCh fired + beadAlreadySubsumedInMain |
| E | shutdown-drain | :5355-5402 (merge **:5374**) | default + `ctx.Err()!=nil` + `!useIndepSession` + HEAD advanced |

**Genuinely identical across A,B,C,D:** the merge call
`lockedMergeRunBranchToMain(ctx, deps.mergeMu, activeRepo, runID, deps.bus, beadID, headSHA, deps.targetBranch, effectiveMergeProtectBranches, deps.brPath)` (E differs only in ctx); the merge-fail sequence (`outcome_emitted(rejected)` -> fresh reopenTID -> `ReopenBead(..., "merge-to-main failed: <reason>")` -> `emitDone(false, "merge-failed (<label>): <reason>")`); the preMergeSync-fail, scenario-gate-fail, and close sequences (all textually identical modulo `<label>`).

### Divergence table (the deliverable - each row must survive as a spine parameter)

| Divergence | A review-loop | B dot | C agent_completed | D auto-close | E shutdown-drain |
|---|---|---|---|---|---|
| ctx for merge/close | per-run ctx | per-run ctx | per-run ctx | per-run ctx | **bgCtx=context.Background()** |
| Scenario gate | yes (rlRunner :3853) | **no** (in-graph commit_gate) | yes (runRunner :5235) | yes (runRunner :5285) | **no** |
| preMergeSync | yes (:3864) | yes (:4106) | yes (:5244) | yes (:5294) | **no** |
| Trailer amend | yes + re-amend per retry, LOCAL only (:3881,:3894) | yes once, LOCAL only (:4118) | no | no | no |
| Merge retries | **loop <=2 on retryable** (:3891) | 1 | 1 | 1 | 1 |
| rebase_dropped_commits fall-through | no | **yes** (alreadyApprovedOnMain :4138) | no | no | n/a |
| outcome_emitted | rejected/approved | rejected/approved | rejected/approved | rejected/approved | **none** |
| Merge-fail control flow | `return` | `return` | falls off switch (no return) | falls off switch | falls to context_cancelled reopen; **merge reason not in reopen reason** (log only :5391) |
| Reopen reason (merge fail) | `merge-to-main failed: <r>` | same | same | same | `context_cancelled: daemon shutdown, requeue pending` |
| emitDone labels | (review-loop)/(review-loop APPROVE) | (dot)/(dot success) | (agent_completed) | (auto-close) | n/a - **direct emitRunCompleted, so *runSucceeded NOT set, NO sessiondata.Collect** |
| Success summary | rlResult.summary | dotResult.summary | "agent_completed: stop-hook outcome" | "auto-close: exit=0" | "shutdown-drain: committed work merged" |
| needsAttention on close | false (**A' budget close=true** :3989) | false | false | false | false |
| Failure-side close | A': budget-exhausted -> close(needsAttention=true); non-BrUnavailable close error -> **fallback Reopen** (:3995-3998) | B' subsumed close (no merge) | - | - | - |
| runTipSHA salvage | no | **yes on failure** (:4201-4207, bg-ctx HEAD resolve) | no | no | no |

### Q2 — Exit-0 auto-close: NOT redundant

C and D are cases of one `switch` (:5230) - **mutually exclusive; both can never execute for one run**. D requires `socketOutcome==nil` (:5277): `waitWithSocketGrace` (waitsocketgrace.go:86) returned no outcome within the 3s stopHookGrace. C requires a non-nil WORK_COMPLETE/REVIEWER_VERDICT outcome (`MapWaitReturnToTerminalEvent` branch 1, claudehandler_chb006_024.go:599-608). **D carries a distinct load-bearing semantic**: the terminal path for a run where agent-completed never arrives but the process exits 0 - kept for MVH twin-blind runs + shell-script fixtures (comment :5118-5122), and in practice **THE path for CompletionProcessExit harnesses (codex, pi)** which have no claude stop-hook (their daemon-side commit fallback :5085-5109 immediately precedes it). Without D, branch-3 mapping (`agent_failed{structural, claude_exit_without_outcome}`) would fail every clean codex/pi run. Dossier "byte-for-byte duplicate" is true of the **body** but false of the **guard** - the spine absorbs D as C-with-different-(condition,label,summary), not delete it.

### Q3 — Shutdown-drain

:5372-5374 `bgCtx:=context.Background()`, merge under bgCtx - confirmed (hk-dnrg: "shutdown does not abort the merge"; per-run ctx already cancelled, guard :5357). Also distinct: entered only from `default` when noChangeTimeoutCh didn't fire; independent-session runs (`useIndepSession`) return earlier (:5364-5366) leaving bead in_progress for boot-time adoption (hk-o85ye). Precondition `resolveWorktreeHEAD(bgCtx, wtPath)` is **box-A-local**, so a remote run's drain degrades to reopen. No timeouts, no gate, no preMergeSync, no outcome_emitted; direct `emitRunCompleted` (skips emitDone). **Stays a distinct terminal transition** (or spine invocation with gate/sync/outcome/emitDone disabled + bgCtx) - its reopen-reason-substitution is intentional QM-002a recovery.

### Q4 — emitDone closure

Definition **:3119-3158** (comment :3112-3118; EM-015b, QM-011/QM-012, hk-45ude). Behavior: (1) writes `*runSucceeded` (out-param from wrapper :3029-3030); (2) picks `emitCtx=ctx` or Background if cancelled (hk-e3fy); (3) `emitRunCompleted(...)` - emits **run_completed on success, run_failed otherwise** (:6003-6031), stamping owningEpicID/Assignee, queueID/queueGroupIndex, runTipSHA; (4) fires detached `sessiondata.Collect` goroutine (:3143-3157) with sdStartedAt/sdModel/sdHarness. Captured: `runSucceeded *bool`, per-run ctx, deps.bus, runID, beadID, owningEpicID/Assignee (:3098), queueID, queueGroupIndex, `runTipSHA *bool` late-write (declared :3092, mutated :4206), sdStartedAt/sdModel/sdHarness, deps.projectDir.

**Reactor state vs shell effect:** `*runSucceeded`, `runTipSHA`, queue coords, owning-epic attribution = per-run reactor state (consumed post-return at :3041 `evaluateGroupAdvanceWithOutcome`, :3046 `stagedBeadGeneratorEval`, and by the deferred Pi worktree-retention/stderr-capture guards :3712,:4689). Bus emission + sessiondata goroutine = shell effects.

Invocation census: **45 call sites**, all inside beadRunOne (:3675-5435). ~13 pre-launch/launch failures, ~4 guards (escape :5184, no-commit :5226), rest are terminal blocks. **Three bypass it**: never-spawned abort (:5020-5023, direct emitRunCompleted + manual `*runSucceeded=false`), shutdown-drain (:5380/5382/5386, runSucceeded NOT set), context_cancelled reopen (:5397, no event at all). The eliminate-`runSucceeded` refactor must preserve all three consumers of the out-param above.

### Q5 — Close/reopen call-site census (region :3072-5438)

- `ReopenBead`: **36 call sites** (3223,3280,3313,3343,3497,3548,3660,3677,3856,3867,3917,3997,4006,4046,4109,4146,4214,4292,4327,4492,4514,4627,4657,4890,5020,5180,5222,5238,5247,5257,5287,5297,5307,5349,5397,5431). All on distinct paths; only "defensive" one is budget-path fallback reopen after failed needs-attention close (:3997). Distinct annotations that must survive: reason strings + **ctx choice**: `context.Background()` at :4214(hk-e3fy),:4890(hk-4hso5),:5020,:5349; `bgCtx` at :5397; per-run ctx elsewhere - including terminal-block reopens (:3856/:3867/:3917/:5238/...), which **silently no-op if ctx cancelled mid-block** (pre-existing inconsistency; a spine that normalizes to Background changes behavior - flag, don't silently do).
- `closeBeadWithHistoryTrim` (def :962, only wrapper over `brAdapter.CloseBead`): **8 call sites** - :3923, :3989 (sole needsAttention=true), :4153, :4171, :5263, :5313, :5334, :5377 (bgCtx). All reachable. Outside region: only `SweepCloseBead` (:5596, out of scope).

### Q6 — preMergeSync + scenario gates

`preMergeSync` closure **:3574-3589** (comment :3567): no-op returning "" for local runs (`rbc==nil`); for remote runs fetches `refs/heads/run/<id>` from worker over SSH to box A (`fetchRunBranchBoxA`, hk-7bwx), emitting `worker_offline` + disabling worker on SSH failure (B11). Captures **per-run ctx** - a spine moving it under a different ctx changes remote behavior.

Scenario gate `runScenarioGateIfNeededVia` (scenariogate.go:107): if committed changes since headSHA touch scenario-tagged files, runs `go test -tags=scenario <affected pkgs>` (10-min timeout, fail-open on machinery failure hk-ur428, retry-once-on-genuine-FAIL hk-5em); `blocked=true` only on deterministic RED. Per block: A via rlRunner, C/D via runRunner (worker-routed when remote), B none (in-graph commit_gate), E none. Known TODO scenariogate.go:83-89: gate runs pre-rebase outside mergeMu; moving it inside the merge critical section is the flagged "correct fix" but deferred.

### Q7 — transitionToTerminated / hclifecycle

Def **:7994-8017** (HC-065, hk-xrygh); drives Machine -> StateTerminating -> StateTerminated (exit 0, no waitErr) or StateFailed, emitting `lifecycle_transition` per transition via `emitWorkloopLifecycleTransition` (:8022). **Single production call site: :5036**, right after `waitWithSocketGrace` returns (:5004) and **before** the escape guard (:5176), no-commit guard (:5206), and the C/D/D'/E switch (:5230). So the formal machine reaches terminal **strictly before merge/close** - it models the session/process lifecycle, not the bead/run terminal transition; the spine can treat it as a fixed pre-spine step of the single-mode path. **The review-loop (A) and DOT (B) paths never call it** (grep confirms :5036 is the only caller) - their sessions are managed inside runReviewLoop/driveDotWorkflow with no Machine terminal drive. Any spine design assuming "machine terminal -> then spine" holds only for single mode; the spine itself must NOT require a Machine.

## Patterns to follow

- Parameterize the spine on: `label`, `successSummary`, `gateRunner` (nil=skip), `doPreMergeSync bool`, `trailerVerdict *ReviewVerdict` + re-amend-on-retry flag, `mergeRetries int`, `allowRebaseDroppedFallthrough bool`, `ctx` (per-run vs background), `emitOutcome bool` (false only for drain), `needsAttention bool`, close/reopen reason templates. Every divergence row maps to exactly one.
- Keep shared merge-fail/sync-fail/gate-fail/close sequences as the spine body - already textually identical modulo `label`.
- `hk-hypbi` BrUnavailable-after-merge -> emit success + retain intent file: uniform across all close sites; preserve verbatim.
- `emitDone` -> a method on a per-run terminal-emitter struct returning the success bool (or recording it in a run-state struct read by the wrapper) instead of the `*bool` out-param; the three consumers are `evaluateGroupAdvanceWithOutcome` (:3041), `stagedBeadGeneratorEval` (:3046), and the two Pi-retention defers (:3712,:4689).

## Risks / conflicts

- **Silently dropping a divergence** - highest risk rows: B's rebase_dropped_commits fall-through (:4138), A's retry loop + per-retry re-amend, A''s needsAttention=true close with non-BrUnavailable fallback reopen, E's bypass of emitDone/runSucceeded/sessiondata + reopen-reason substitution, C/D's missing `return` after merge-fail (they fall off the switch; a spine appending post-switch steps would change behavior).
- **ctx normalization**: terminal-block reopens on per-run ctx currently no-op silently if the run was cancelled mid-merge; unifying to Background is a behavior change (probably an improvement per hk-e3fy, but must be an explicit decision).
- `preMergeSync` and scenario gates capture per-run state (rbc, rlRunner/runRunner, ctx); hoisting into a spine struct must thread the correct runner per mode (rlRunner != runRunner in origin).
- runSucceeded-elimination interacts with never-spawned abort (:5016-5026, sets it manually) - that path must route through the new terminal emitter too.
- scenariogate.go TODO (gate outside mergeMu) will tempt a "fix while we're here"; keep out of C6 scope unless the spec says otherwise.
