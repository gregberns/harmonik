<!-- RECONCILE 2026-07-15: id vocabulary. This design doc predates the RX->RSM rename and
     uses RX-* ids from the superseded Set-A draft lineage. RX numbering is NOT 1:1 with the
     normative RSM ids (verified: RX-INV-003 == RSM-INV-001; RX-020 != RSM-020). The normative
     spec specs/run-state-machine.md (RSM) is AUTHORITATIVE; treat RX-* here as historical only. -->

# 04-Design / runexec — the two pure machines + the daemon shell

> Component design for C4 (reactor) + C6 (terminal spine), within the M3-D pins.
> Code template mirrored throughout: `internal/keeper/step.go` (pure total Step,
> flat Event/Action, timers-as-events, event-`At`-sourced timestamps) and
> `internal/keeper/shell.go` (effector switch, `drive` loop with detection ticker
> + nearest-deadline wake, `fireOnCancel`). Facts cite
> `03-research/runexec/findings.md` (RF) and `03-research/liveness/findings.md` (LF).

## 0. Package shape

```
internal/runexec/            (pure; depguard: deny internal/daemon, deny workloop — TC-6)
  vocab.go      Event, Action, EventKind, ActionKind, TimerKind, SessionRef, InputID
  dispatch.go   Dispatch machine: DispatchState, stepDispatch (pure, total)
  run.go        Run machine: RunState, stepRun (pure, total)
  doc.go        3-way disambiguation (runexec vs hclifecycle vs queue states)
internal/daemon/
  runshell.go   the effector (execute(Action)) + per-run drive loop
  runports.go   RunPorts/RunEnv/SharedHandles (c3-workloopdeps-ports-design.md)
  workloop.go   beadRunOne → thin driver: prepareRun (guards) + drive(Run)
```

Both machines expose the keeper reactor surface verbatim (step.go:223–:253):
`New*(cfg)`, `Step(ev Event) []Action`, `State()`, `InFlight() bool`, and a
one-line `Run(ctx, src, eff)` wrapper over `substrate.Run[Event, Action]`.
`Step` is TOTAL and pure: no IO, no clock reads, no id minting — every timestamp
from the event's shell-stamped `At`; `RunID`/`InputID` minted shell-side
(keeper cycle-id precedent, step.go:14–:22).

## 1. Event vocabulary (shell → reactor)

Flat, JSON-round-trippable (keeper step.go:106 shape). One vocabulary for both
machines; kinds are namespaced by consumer.

| Kind | Fields (beyond Kind, At) | Source (today's mechanism) |
|---|---|---|
| `EvProvisioned` | WtPath, HeadSHA, BaseBranch | worktree create return (RF P5) |
| `EvProvisionFailed` | Reason | wtErr (RF `:3675`) |
| `EvLaunched` | SessionRef | Launch return (RF `:4638`) |
| `EvLaunchFailed` | Reason (`spawn_cap_blocked`/`tmux_new_window_timeout`) | launchErr (RF `:4639–:4661`) |
| `EvAgentReady` | SessionRef | relay callback tap (RF `:4763–:4787`); transitional probe (M3-D7) |
| `EvInputAck` / `EvInputRejected` | SessionRef, InputID, Reason | M2 driver; transitional synthetic ack (M3-D11) |
| `EvHeartbeat` | SessionRef | heartbeat loop (RF §4) |
| `EvCommitObserved` | SHA | frozen watchdog (M3-D3) |
| `EvNoChangeTimeout` | — | watchdog `noChangeTimeoutCh` close (RF `:5330`) |
| `EvHeartbeatStale` | — | watchdog staleness kill (pasteinject.go:658) |
| `EvAgentExited` | SessionRef, ExitCode, WaitErr | waitWithSocketGrace `ei` (RF `:5004`) |
| `EvOutcomeReceived` | SessionRef, Outcome | socketOutcome (RF `:5004`) |
| `EvAborted` | Reason | `handle.aborted` / reaper (RF `:5016`) |
| `EvModeOutcome` | Mode, Success, Detail (verdict/budget/subsumed/salvage SHA) | runReviewLoop / driveDotWorkflow returns (RF P8) |
| `EvGatePassed` / `EvGateFailed` | Reason | scenario gate (RF `:3853/:5235/:5285`) |
| `EvGuardsPassed` / `EvEscapeDetected` / `EvNoCommitGuardReopen` | Reason | escape check under the mergeq domain (RF `:5169`) + no-commit guard (RF `:5206`) — single-mode P17 guards |
| `EvMergeResult` | Outcome ∈ {success, noChange, retryable(reason), fatal(reason)} | mergeq Submit return (c2-merge-queue-design §3) |
| `EvCloseResult` | Outcome ∈ {closed, br_unavailable, error} | LedgerPort close return |
| `EvShutdownDrain` | WorktreeAheadSHA | ctx-cancel + HEAD probe (RF `:5372–:5373`) |
| `EvTimerFired` | Timer | shell deadlines (M3-D3 kinds) |

## 2. Action vocabulary (reactor → effector)

| Kind | Fields | Effector mapping (failure policy = today's, per-action, keeper shell.go:35 table) |
|---|---|---|
| `ActCreateWorktree` | — | WorktreePort (base-sync + create, under mergeq domain — c2-merge-queue-design §4) |
| `ActEmit` | Type, Payload | EmitterPort; best-effort `_ =` except noted |
| `ActLaunchAgent` | SessionRef, SpecRef | LaunchPort; spawn-sem acquire is EFFECTOR policy for remote (decompose C4 Q(d) settled: shell concern) |
| `ActDeliverInput` | SessionRef, InputID, Kind∈{brief,resume_prompt,quit}, PayloadRef | LaunchPort paste-inject today; M2 driver later (M3-D11) |
| `ActKillAgent` | SessionRef | sess.Kill (idempotent) |
| `ActRunGate` | — | scenario gate helper |
| `ActPrepareMerge` | — | mergeq prepare phase (outside queue) |
| `ActSubmitMerge` | Label | mergeq Submit (critical section) |
| `ActCloseBead` | SummaryRef | LedgerPort closeBeadWithHistoryTrim + emitBeadClosedAndMaybeEpic |
| `ActReopenBead` | Reason | LedgerPort ReopenBead (Background-ctx policy preserved, hk-e3fy) |
| `ActEmitRunTerminal` | Success, SummaryRef | emitRunCompleted incl. ctx-swap; sessiondata.Collect side-launch EXCEPT on drain batch (M3-D8) |
| `ActDriveLifecycleTerminated` | ExitCode, WaitErr | transitionToTerminated wrapper (M3-D9; HC-065 unmoved) |
| `ActArmTimer` / `ActCancelTimer` | Timer, D | shell deadline map (keeper shell.go:103–:125 verbatim mechanics) |

## 3. The `Dispatch` machine (per agent session — the keeper-Cycle analog)

State: `Phase`, `SessionRef`, `Attempt`, `IsResume bool`, `BriefInputID`,
`ReadyAt`, `LastAckedInput`, `ModelHints…` — all event-`At`-sourced.

Transition table (every row total; unlisted (state,event) pairs are explicit
no-ops, keeper-style):

| State | Event | Next | Actions |
|---|---|---|---|
| Idle | (entry by shell: StartDispatch) | Launching | ActLaunchAgent, ActArmTimer(agent_ready) |
| Launching | EvLaunched | AwaitingReady | ActEmit(launch_initiated — held-back semantics preserved: emitted only now, RF `:4667`) |
| Launching | EvLaunchFailed | Failed(reason) | ActEmit(spawn_cap_blocked/tmux…), ActCancelTimer(agent_ready) |
| AwaitingReady | EvAgentReady | Briefing | ActCancelTimer(agent_ready), ActDeliverInput(brief|resume_prompt), ActArmTimer(input_ack) |
| AwaitingReady | EvTimerFired(agent_ready) | ReadyTimeout | ActKillAgent, ActArmTimer(ready_kill_reap), ActEmit(agent_ready_timeout) — **the SR9 edge: outgoing action, never silence (M3-D7)** |
| AwaitingReady | EvAgentExited | Exited | ActCancelTimer(agent_ready) |
| ReadyTimeout | EvAgentExited / EvTimerFired(ready_kill_reap) | Failed(agent_ready_timeout) | — (terminal; Run machine reopens) |
| Briefing | EvInputAck | Working | ActCancelTimer(input_ack) |
| Briefing | EvInputRejected / EvTimerFired(input_ack) | per policy: retry (Attempt++<max → ActDeliverInput+re-arm) else Failed(input_undeliverable) | **RX-INV-001 edge** |
| Working | EvHeartbeat / EvCommitObserved | Working | — (state notes progress) |
| Working | EvOutcomeReceived | Completed(outcome) | ActCancelTimer(*) |
| Working | EvAgentExited | Exited(exitCode) | ActDriveLifecycleTerminated |
| Working | EvNoChangeTimeout / EvHeartbeatStale | Stalled(reason) | ActKillAgent |
| any non-terminal | EvAborted | Aborted | ActKillAgent |
| any non-terminal | ctx-cancel (shell fireOnCancel → phase timeout edge) | per-phase timeout row | keeper shell.go:318 mechanics |

ProcessExit harnesses (codex/pi): the shell starts the dispatch with
`SkipReadyHandshake` config → `Launching --EvLaunched--> Working` directly
(parity with RF P13 "ProcessExit harnesses skip").

**Terminals:** `Completed | Exited | Stalled | ReadyTimeout→Failed | Failed |
Aborted`. Exactly one per dispatch (structural: terminals have no outgoing
edges). The Run machine (or the mode sub-driver) consumes the terminal.

## 4. The `Run` machine (the spine; C6 lives in its tail)

State: `Phase`, `Mode`, `Iteration`, `HeadSHA`, `RunTipSHA`, `Success`,
`SummaryLabel`, `MergeAttempt`, `GateDone bool`, terminal `Done{closed|reopened}`.

| State | Event | Next | Actions |
|---|---|---|---|
| Resolving | (entry) — shell runs prepareRun guards sequentially (RF P2–P4; guard failures feed EvProvisionFailed-class events) | Provisioning | ActCreateWorktree |
| Provisioning | EvProvisioned | Dispatching | ActEmit(run_started) (RF `:3742` position preserved) |
| Provisioning | EvProvisionFailed | Finalizing(reopen) | ActReopenBead, ActEmitRunTerminal(false) — the first-emitDone parity point (RF `:3675`) |
| Dispatching | EvModeOutcome{success} | Gating | ActRunGate (modes that gate; RF §3 table) |
| Dispatching | EvModeOutcome{failure/budget…} | Finalizing(reopen or needs-attention-close per detail) | budget policy carried as event detail (shell-computed under BudgetPort) |
| Dispatching | EvAgentCompleted / EvCleanExit (single-mode dispatch terminals) | Guarding | ActCheckEscape (Merge-port domain submit, hk-zguy6), then shell feeds guard events |
| Guarding | EvEscapeDetected | Finalizing(reopen) | ActEmit(implementer_escaped_worktree), ActReopenBead, ActEmitRunTerminal(false) (RF `:5169–:5186`) |
| Guarding | EvNoCommitGuardReopen | Finalizing(reopen) | (RF `:5206–:5228`) |
| Guarding | EvGuardsPassed | Gating | ActRunGate |
| Dispatching | EvModeOutcome{subsumed} / EvNoChangeSubsumed | Finalizing(close, no merge) | — (the 2 no-merge close-ladder entries, RF §6) |
| Dispatching | EvShutdownDrain | Merging(drain) | ActSubmitMerge — drain batch: effector bgCtx policy (M3-D9) |
| Gating | EvGatePassed | Merging | ActPrepareMerge |
| Gating | EvGateFailed | Finalizing(reopen) | ActReopenBead, ActEmitRunTerminal(false) |
| Merging | EvMergeResult{success|noChange} | Finalizing(close) | the ONE close ladder: ActEmit(outcome_emitted approved) → ActCloseBead → ActEmitRunTerminal(true) — BrUnavailable/close-error variants as EvCloseResult rows below |
| Merging | EvMergeResult{retryable} ∧ MergeAttempt<max(mode) | Merging | ActPrepareMerge (+ trailer re-amend action for review-loop, RF `:3899`) |
| Merging | EvMergeResult{retryable ∧ exhausted, fatal} | Finalizing(reopen) | ActEmit(outcome_emitted rejected), ActReopenBead, ActEmitRunTerminal(false); DOT alreadyApprovedOnMain carve-out = a distinct row → Finalizing(close) (RF `:4138–:4151`) |
| Finalizing(close) | EvCloseResult{closed} | Done{closed, success} | ActEmitRunTerminal(true) (once) |
| Finalizing(close) | EvCloseResult{br_unavailable} | Done{closed-transient} | success-transient string preserved (RF §6 ladder) |
| Finalizing(reopen) | (immediate) | Done{reopened} | — |

`Run` phases before Gating are deliberately thin in M3 (M3-D2 scope cut): the
prepareRun guards (config/branching/remote/worktree, RF P2–P5) stay sequential
shell code with early returns; each failure path routes through
`Finalizing(reopen)` so the terminal spine — not scattered returns — owns every
reopen/emit pairing. That alone removes ~20 hand-rolled emitDone/reopen sites.

## 5. The shell (internal/daemon/runshell.go)

Mirrors keeper shell.go structure exactly:
- **execute(Action)** — the effector switch; per-action failure policy table
  byte-compatible with today's call sites (best-effort `_ =` vs fatal; the only
  fatal-to-drive failures are worktree create and Launch, which feed failure
  events rather than erroring the loop).
- **drive(ctx)** — per-run loop: select over {per-run tap channel (events),
  watcher.Done, frozen-watchdog signals, nearest-deadline wake via
  `Clock.NewTicker` (keeper shell.go:190–:242 verbatim pattern), ctx.Done →
  fireOnCancel mapping onto phase timeout edges}.
- Sub-drivers: `runReviewLoop`/`driveDotWorkflow` keep their control flow but
  their launch/wait segments call `shell.RunDispatch(cfg) DispatchTerminal`
  (drive a Dispatch instance to terminal) instead of open-coded
  Launch+waitAgentReady+caulk. `waitAgentReady` (agentready.go:154) and the 2s
  caulk (reviewloop.go:110) are DELETED once all callers migrate.

## 6. The M3-4 → M2-1 contract

Restated normatively in M3-D11 (00-decisions) and RX spec §6; the Dispatch rows
above are its state-machine expression: `ActDeliverInput`+`TimerInputAck` ↔
`EvInputAck|EvInputRejected|EvTimerFired(input_ack)` (RX-INV-001), and
`EvAgentReady` required on start AND resume with `TimerAgentReady` as the
failure backstop (RX-INV-002). The transitional tmux shell satisfies the same
contract with synthetic acks (paste-verify) and the run_id-stamped ready probe —
so M3 lands green without M2, and M2's driver swaps in at the composition root.

## 7. Self-review notes (for an independent reviewer to challenge)

1. **Two machines vs one:** justified in M3-D2; the cost is two transition
   tables. Challenge point: could Dispatch fold into Run with a sub-phase field?
   Rejected because review-loop/DOT run MULTIPLE dispatches per run and the
   sub-drivers stay shell-side in M3 — they need a machine they can drive
   standalone.
2. **prepareRun stays sequential:** a purist would put Resolving/Provisioning
   fully in the machine. Kept thin deliberately (M3-D2) — the guards are
   straight-line validation with zero waiting; machine value is in the
   waiting/terminal regions. Revisit at M5.
3. **The frozen watchdog** (M3-D3) leaves `commitHardCeiling` etc. outside the
   timer-event system — C5's window derivation therefore CITES those constants
   rather than owning them. Acceptable for M3; flagged as deferred.
4. **EvModeOutcome carries verdict/budget detail** computed shell-side — a pure
   purist would want budget math in the machine. It requires queueStore
   mutation mid-computation (LockForMutation read-modify-write, RF P8a), which
   cannot be pure; the CHOICE (close-needs-attention vs reopen) is in the
   machine, the COUNTER mutation is an action.
