# 04-Design / C4 — the runexec reactor (two pure machines + the daemon shell)

> Pass 4 design for C4 (reactor) + the terminal-spine tail it shares with C6. Elaborates
> pins D1 (package/depguard), D5 (reactor shape), D6 (timers-as-events), D8 (terminal spine),
> D9 (parity), D10 (M2 consume). Research: `03-research/c4-runexec-reactor/findings.md` (RF),
> `03-research/c6-terminal-spine/findings.md` (TF). Template mirrored verbatim:
> `internal/keeper/step.go` (pure total Step, flat Event/Action, timers-as-events,
> event-`At`-sourced timestamps) + `internal/keeper/shell.go` (effector switch, `drive` loop
> with detection ticker + nearest-deadline wake, `fireOnCancel`).
> Target spec: NEW `specs/run-state-machine.md` (prefix RSM).

## Current state
`beadRunOne` (workloop.go:3072-5447, ~2,376 lines, 17 params incl. `runSucceeded *bool`
out-param) is one god-function with the run lifecycle spread across four implicit
representations (hclifecycle.Machine, the pre-exec event chain, the Step-9 terminal switch,
`reviewLoopState`) and the launch→gate→merge→close spine open-coded 4× (RF §6 / TF). No
`specs/` file owns it. No pure state machine; timers are wall-clock blocking waits.

## Target state

### RSM-RX-1 — package shape (D1)
```
internal/daemon/runexec/     (pure; depguard: deny internal/daemon)
  vocab.go      Event, Action, EventKind, ActionKind, TimerKind, SessionRef, InputID/MsgID
  dispatch.go   Dispatch machine: DispatchState, stepDispatch (pure, total)
  run.go        Run machine: RunState, stepRun (pure, total)
  doc.go        3-way disambiguation (runexec vs hclifecycle vs queue states)
internal/daemon/
  runshell.go   the effector (execute(Action)) + per-run drive loop
  runports.go   RunPorts/RunEnv/SharedHandles (see c3-ports-design.md)
  workloop.go   beadRunOne → thin driver: prepareRun (guards) + drive(Run)
```
Both machines expose the keeper reactor surface verbatim (step.go:234-253): `New*(cfg)`,
`Step(ev Event) []Action`, `State()`, `InFlight() bool`, and a one-line `Run(ctx, src, eff)`
over `substrate.Run[Event, Action]`. `Step` is TOTAL and pure: no IO, no clock reads, no id
minting — every timestamp from the event's shell-stamped `At`; `RunID`/`MsgID` minted
shell-side.

### RSM-RX-2 — Event vocabulary (shell → reactor)
Flat, JSON-round-trippable (keeper step.go:106 shape). One vocabulary for both machines.

| Kind | Fields (beyond Kind, At) | Source (today's mechanism) |
|---|---|---|
| `EvProvisioned` | WtPath, HeadSHA, BaseBranch | worktree create return (RF P5) |
| `EvProvisionFailed` | Reason | wtErr (RF :3675) |
| `EvLaunched` | SessionRef | Launch return (RF :4638) |
| `EvLaunchFailed` | Reason (spawn_cap_blocked / tmux_new_window_timeout) | launchErr (RF :4639-4661) |
| `EvAgentReady` | SessionRef, RunID | relay callback tap (RF :4763-4787); **run_id-stamped** (D7 attribution fix) |
| `EvInputAcked` / `EvInputStale` | SessionRef, MsgID, LatencyMs / BoundMs | **M2 seam** (D10): agent_input_acked / agent_input_stale, run-scoped, msg_id-keyed |
| `EvInputUnsupported` | MsgID | M2 `ErrInputUnsupported` from a tmux/IN-007 session → observation-only fallback |
| `EvHeartbeat` | SessionRef | heartbeat loop (RF §4) — daemon-goroutine liveness, NOT agent progress (D7) |
| `EvCommitObserved` | SHA | watchdog worktree-HEAD advance (agent-derived progress, D7) |
| `EvNoChangeTimeout` | — | watchdog `noChangeTimeoutCh` close (RF :5330) |
| `EvHeartbeatStale` | — | watchdog staleness kill (pasteinject.go:658) |
| `EvAgentExited` | SessionRef, ExitCode, WaitErr | waitWithSocketGrace `ei` (RF :5004) |
| `EvOutcomeReceived` | SessionRef, Outcome | socketOutcome (RF :5004) |
| `EvAborted` | Reason | `handle.aborted` / reaper (RF :5016) |
| `EvModeOutcome` | Mode, Success, Detail (verdict / budget / subsumed / salvage SHA) | runReviewLoop / driveDotWorkflow returns (RF P8) |
| `EvGatePassed` / `EvGateFailed` | Reason | scenario gate (RF :3853 / :5235 / :5285) |
| `EvGuardsPassed` / `EvEscapeDetected` / `EvNoCommitGuardReopen` | Reason | escape check inside the mergequeue exclusion (RF :5169) + no-commit guard (RF :5206) |
| `EvMergeResult` | Outcome ∈ {success, noChange, retryable(reason), fatal(reason)} | mergequeue Submit return (c2-merge-queue-design) |
| `EvCloseResult` | Outcome ∈ {closed, br_unavailable, error} | LedgerPort close return |
| `EvShutdownDrain` | WorktreeAheadSHA | ctx-cancel + HEAD probe (RF :5372-5373) |
| `EvTimerFired` | Timer | shell deadlines (D6 kinds) |

### RSM-RX-3 — Action vocabulary (reactor → effector)

| Kind | Fields | Effector mapping (per-action failure policy = today's, keeper shell.go:35 table) |
|---|---|---|
| `ActCreateWorktree` | — | WorktreePort (base-sync + create, inside mergequeue exclusion — c2 design) |
| `ActEmit` | Type, Payload | EmitterPort; best-effort `_ =` except noted |
| `ActLaunchAgent` | SessionRef, SpecRef | LaunchPort; spawn-sem acquire is EFFECTOR policy for remote (RF §6 settled: shell concern) |
| `ActSubmitSeed` / `ActSubmitBrief` | SessionRef, MsgID, PayloadRef | **M2 seam** (D10): shell calls `Submit(ctx, InputMsg{MsgID,Kind,Body})`; tmux/IN-007 path uses paste-inject transitionally |
| `ActKillAgent` | SessionRef | sess.Kill (idempotent) |
| `ActRunGate` | — | scenario gate helper (nil gateRunner ⇒ skip) |
| `ActPrepareMerge` | — | mergequeue speculative phase (rebase/build/fmt, OUTSIDE the queue — c2 design) |
| `ActSubmitMerge` | Label | mergequeue Submit (the serialized critical section) |
| `ActCloseBead` | SummaryRef, NeedsAttention | LedgerPort closeBeadWithHistoryTrim + emitBeadClosedAndMaybeEpic |
| `ActReopenBead` | Reason | LedgerPort ReopenBead (Background-ctx policy, hk-e3fy — D8) |
| `ActEmitRunTerminal` | Success, SummaryRef | emitRunCompleted incl. ctx-swap; sessiondata.Collect side-launch EXCEPT the drain batch (D8) |
| `ActDriveLifecycleTerminated` | ExitCode, WaitErr | transitionToTerminated wrapper (HC-065 unmoved — D5 projection) |
| `ActArmTimer` / `ActCancelTimer` | Timer, D | shell deadline map (keeper shell.go:103-125 verbatim) |

### RSM-RX-4 — the `Dispatch` machine (per agent session — the keeper-Cycle analog)
State: `Phase`, `SessionRef`, `Attempt`, `IsResume bool`, `SeedMsgID`, `ReadyAt`,
`LastAckedInput`, `ModelHints…` — all event-`At`-sourced. Every row total; unlisted
(state,event) pairs are explicit no-ops (keeper-style).

| State | Event | Next | Actions |
|---|---|---|---|
| Idle | (shell entry: StartDispatch) | Launching | ActLaunchAgent, ActArmTimer(agent_ready) |
| Launching | EvLaunched | AwaitingReady | ActEmit(launch_initiated — held-back semantics preserved, RF :4667) |
| Launching | EvLaunchFailed | Failed(reason) | ActEmit(spawn_cap_blocked / tmux…), ActCancelTimer(agent_ready) |
| AwaitingReady | EvAgentReady | Briefing | ActCancelTimer(agent_ready), ActSubmitSeed / ActSubmitBrief |
| AwaitingReady | EvTimerFired(agent_ready) | ReadyTimeout | ActKillAgent, ActArmTimer(kill_reap), ActEmit(agent_ready_timeout) — **the SR9 edge: outgoing action, never silence (D7)** |
| AwaitingReady | EvAgentExited | Exited | ActCancelTimer(agent_ready) |
| ReadyTimeout | EvAgentExited / EvTimerFired(kill_reap) | Failed(agent_ready_timeout) | — (terminal; Run machine reopens) |
| Briefing | EvInputAcked | Working | — (M2 owns the 30s bound; D10) |
| Briefing | EvInputStale / EvInputUnsupported | per policy: retry (Attempt++<max → ActSubmit* ) else Failed(input_undeliverable) | **consumes M2 IN-INV-001 as the resume-seed liveness edge (D10)** |
| Working | EvHeartbeat | Working | — (daemon-goroutine liveness only, NOT progress — D7) |
| Working | EvCommitObserved | Working | — (agent-derived progress noted) |
| Working | EvOutcomeReceived | Completed(outcome) | ActCancelTimer(*) |
| Working | EvAgentExited | Exited(exitCode) | ActDriveLifecycleTerminated |
| Working | EvNoChangeTimeout / EvHeartbeatStale | Stalled(reason) | ActKillAgent |
| any non-terminal | EvAborted | Aborted | ActKillAgent |
| any non-terminal | ctx-cancel (shell fireOnCancel → phase timeout edge) | per-phase timeout row | keeper shell.go:318 |

ProcessExit harnesses (codex/pi): the shell starts the dispatch with `SkipReadyHandshake`
config → `Launching --EvLaunched--> Working` directly (parity with RF P13 "ProcessExit
harnesses skip"). **Terminals:** `Completed | Exited | Stalled | ReadyTimeout→Failed |
Failed | Aborted` — exactly one per dispatch (structural: terminals have no outgoing edges).

### RSM-RX-5 — the `Run` machine (the spine; C6 lives in its tail)
State: `Phase`, `Mode`, `Iteration`, `HeadSHA`, `RunTipSHA`, `Success`, `SummaryLabel`,
`MergeAttempt`, `GateDone bool`, terminal `Done{closed|reopened}`.

| State | Event | Next | Actions |
|---|---|---|---|
| Resolving | (entry — shell runs prepareRun guards sequentially, RF P2-P4; failures feed EvProvisionFailed-class) | Provisioning | ActCreateWorktree |
| Provisioning | EvProvisioned | Dispatching | ActEmit(run_started) (RF :3742 position preserved) |
| Provisioning | EvProvisionFailed | Finalizing(reopen) | ActReopenBead, ActEmitRunTerminal(false) — first-emitDone parity point (RF :3675) |
| Dispatching | EvModeOutcome{success} | Gating | ActRunGate (modes that gate; TF) |
| Dispatching | EvModeOutcome{failure/budget…} | Finalizing(reopen or needs-attention-close per detail) | budget policy carried as event detail (shell-computed under BudgetPort) |
| Dispatching | EvAgentCompleted / EvCleanExit (single-mode terminals) | Guarding | ActCheckEscape (mergequeue exclusion, hk-zguy6), then shell feeds guard events |
| Guarding | EvEscapeDetected | Finalizing(reopen) | ActEmit(implementer_escaped_worktree), ActReopenBead, ActEmitRunTerminal(false) (RF :5169-5186) |
| Guarding | EvNoCommitGuardReopen | Finalizing(reopen) | (RF :5206-5228) |
| Guarding | EvGuardsPassed | Gating | ActRunGate |
| Dispatching | EvModeOutcome{subsumed} / EvNoChangeSubsumed | Finalizing(close, no merge) | — (the 2 no-merge close-ladder entries, TF) |
| Dispatching | EvShutdownDrain | Merging(drain) | ActSubmitMerge — drain batch: effector bgCtx policy (D8) |
| Gating | EvGatePassed | Merging | ActPrepareMerge |
| Gating | EvGateFailed | Finalizing(reopen) | ActReopenBead, ActEmitRunTerminal(false) |
| Merging | EvMergeResult{success\|noChange} | Finalizing(close) | the ONE close ladder: ActEmit(outcome_emitted approved) → ActCloseBead → ActEmitRunTerminal(true) |
| Merging | EvMergeResult{retryable} ∧ MergeAttempt<max(mode) | Merging | ActPrepareMerge (+ trailer re-amend for review-loop, RF :3899) |
| Merging | EvMergeResult{retryable ∧ exhausted, fatal} | Finalizing(reopen) | ActEmit(outcome_emitted rejected), ActReopenBead, ActEmitRunTerminal(false); DOT alreadyApprovedOnMain carve-out = distinct row → Finalizing(close) (RF :4138-4151) |
| Finalizing(close) | EvCloseResult{closed} | Done{closed, success} | ActEmitRunTerminal(true) (once) |
| Finalizing(close) | EvCloseResult{br_unavailable} | Done{closed-transient} | success-transient string preserved (TF ladder) |
| Finalizing(reopen) | (immediate) | Done{reopened} | — |

`Run` phases before Gating are deliberately thin in M3 (D4 scope cut): the prepareRun guards
(config/branching/remote/worktree, RF P2-P5) stay sequential shell code with early returns;
each failure path routes through `Finalizing(reopen)` so the terminal spine — not scattered
returns — owns every reopen/emit pairing. That alone removes ~20 hand-rolled emitDone/reopen
sites. **C6 divergence params** (label, successSummary, gateRunner-nil, doPreMergeSync,
trailerVerdict+re-amend, mergeRetries, allowRebaseDroppedFallthrough, ctx, emitOutcome,
needsAttention — TF divergence table) ride as RunState fields / event detail; exit-0
auto-close and shutdown-drain stay distinct ENTRY events, same tail (D8).

### RSM-RX-6 — the shell (internal/daemon/runshell.go)
Mirrors keeper shell.go exactly: `execute(Action)` effector switch (per-action failure policy
byte-compatible with today; only worktree-create and Launch are fatal-to-drive, and they feed
failure EVENTS rather than erroring the loop); `drive(ctx)` per-run loop selecting over
{per-run tap channel, watcher.Done, watchdog signals, nearest-deadline `Clock.NewTicker` wake
(shell.go:190-242), ctx.Done → fireOnCancel onto phase timeout edges}. Sub-drivers
`runReviewLoop`/`driveDotWorkflow` keep their control flow but call `shell.RunDispatch(cfg)`
(drive a Dispatch instance to terminal) instead of open-coded Launch+waitAgentReady+caulk;
`waitAgentReady` (agentready.go:154) and the 2s caulk (reviewloop.go:110) are DELETED once all
callers migrate.

### RSM-RX-7 — the M2 seam consumption (D10)
The Dispatch rows ARE the state-machine expression of consuming M2's ratified contract:
`ActSubmitSeed`/`ActSubmitBrief` → shell `Submit(ctx, InputMsg)` → `EvInputAcked`/`EvInputStale`
(M2 IN-001/IN-003/IN-INV-001), correlated by `MsgID` (IN-004). M3 does NOT define the ack or
its 30s bound — those are M2's. `EvAgentReady` is required on start AND resume, run_id-stamped,
with the `agent_ready` timer as the failure backstop. The transitional tmux path satisfies the
same Dispatch rows via paste-inject + the run_id-stamped ready probe (an `ActSubmitSeed` whose
effector is paste-inject and whose ack is the resume-ready fallback, replaced by M2's driver at
the composition root, IN-005/IN-008). So M3 lands green on tmux today and M2's driver swaps in.

## Rationale
Two machines (Dispatch + Run) not one: review-loop/DOT run MULTIPLE dispatches per run and the
sub-drivers stay shell-side in M3, so they need a standalone-drivable Dispatch (RF §4). The
pure-Step/effector split is the keeper-proven shape (RF §1). Guarding is a distinct state so
the single-mode P17 guards (escape hk-zguy6, no-commit) have a home between Dispatching and
Gating (they run inside the mergequeue exclusion; review-loop/DOT do not pass Guarding —
parity: only single-mode runs them today). prepareRun stays sequential (D4 thin cut) — the
machine's value is in the waiting/terminal regions; M5 revisits.

## Requirements traceability
02-components C4 → RSM-RX-1..7; C6 → RSM-RX-5 tail + D8. Goals "explicit named-state machine",
"beadRunOne a thin driver" (01 §2) → RSM-RX-4/5. Success-criteria 1,2,5 → RSM-RX-1/5.

## PLANNER-RECONCILE
- **[D10]** M2-1 OWNS the input/ack seam; M3 CONSUMES it (planner correction 2026-07-14).
  Reactor uses M2's `Submit(ctx,InputMsg)(InputAck,error)` + `InputAcked`/`InputStale` +
  `MsgID`; no competing type; no veto. M2's 30s stale becomes the primary resume-seed detector,
  M3's per-run liveness the backstop — confirm the bounds stack (00-decisions D10).
- **Report upstream, don't fork:** the daemon needs multi-source event mux + action-result-as-
  event that `substrate.Run` (single channel, error-only Execute) lacks (RF §2); the shell
  drives `feed()` directly (keeper precedent) and the gaps are reported to the substrate seam,
  not copied (02-components §C4(e)).
