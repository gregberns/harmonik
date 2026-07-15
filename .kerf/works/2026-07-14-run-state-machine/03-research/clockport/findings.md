# 03-Research / C1 — ClockPort in the daemon

> Pass 3 (Research), component C1. All `file:line` verified against the current
> working tree (branch `phase1-session-restart-substrate`, 2026-07-14).
> Delegated to a fresh-context sub-agent; parent (design agent) owns this write.

## Research questions

1. Census of direct `time.*` sites in `internal/daemon/workloop.go` / `reviewloop.go`, split run-lifecycle vs maintenance.
2. Definitions/values/usages of the named run-lifecycle timeouts.
3. Exact `substrate.ClockPort` API + SystemClock + FakeClock capabilities.
4. The keeper's ClockPort threading (D4) and timers-as-events (D11) template.
5. Existing test-clock seams in `internal/daemon`.
6. Blocking-wait vs interval-gate classification per timeout.

## Findings

### Q1 — time.Now() census (current working tree)

**`internal/daemon/workloop.go`: 38 `time.Now|Since|NewTicker|Sleep|After|NewTimer(` sites** (excluding comments). `runWorkLoop` = 1508-3071, `beadRunOne` = 3072-5447.

**Run-lifecycle path (beadRunOne + emit helpers + queue-advance-on-outcome) - 21 sites:**

| Line | Site | What |
|---|---|---|
| 2988 | `StartedAt: time.Now()` | RunHandle registration at dispatch (runWorkLoop, just before spawning beadRunOne) |
| 3109, 3134 | `sdStartedAt / sdEndedAt := time.Now()` | sessiondata.Collect timestamps inside beadRunOne |
| 4408 | `StartedAt: time.Now()` | second RunHandle registration path inside beadRunOne |
| 4637 | `implementerLaunchedAt := time.Now()` | launch anchor |
| 4647, 4654 | `time.Since(implementerLaunchedAt)` | emitSpawnCapBlocked / emitTmuxNewWindowTimeout |
| 4871 | `case <-time.After(agentReadyKillReapTimeout)` | agent_ready-timeout kill/reap bound |
| 4883 | `context.WithTimeout(..., agentReadyKillReapTimeout)` | session-reap bound (wall-clock via ctx) |
| 5063 | `time.Since(implementerLaunchedAt)` | emitImplementerPhaseComplete duration |
| 5989, 6016, 6042 | `time.Now().UTC().Format(RFC3339)` | emitRunStarted / emitRunCompleted / emitImplPresence event timestamps |
| 6142, 6220, 6304, 6326 | `queue.AdvanceGroup(ctx, ..., time.Now())` | group advance on outcome - **queue API already takes `now` as a parameter** |
| 7404, 7671, 8032 | RFC3339 stamps | emitBeadSyncFailed / epic ClosedAt / lifecycle TransitionedAt |

**Maintenance / non-run - 17 sites:** 1153, 1628, 1717/1719, 1777-1779, 1842-1844, 1893-1895, 2242/2342/2398, 5658 (`workloopSleep`), 5703 (`scheduleAwareIdleWait`), 8080 (`time.NewTicker(2s)` in `adoptLiveRunSession`).

**`internal/daemon/reviewloop.go`: 8 sites, ALL run-lifecycle** (`runReviewLoop` = 198-1783): 551, 562/568, 659 (`time.After(resumeReadyFallbackGrace)`), 738, 862, 1478, 1907 (`rlSynthesiseClaudeSessionID`). Plus `context.WithTimeout(...)` at 744 and 1484.

**Adjacent run-path files the census MUST include (Q2 mechanisms live here, not in workloop.go):**
- `internal/daemon/agentready.go:194` - `case <-time.After(timeout)` is THE agent_ready wait.
- `internal/daemon/postreadyhang.go:59` - `time.NewTimer(timeout)` is the post-ready hang wait.
- `internal/daemon/pasteinject.go` - ~25 time sites; watchdog core: 932/939/947/1002 (`NewTicker(pollInterval)`)/1021/1036/1066/1078, splash/paste/resume `time.After`s at 1531/1762/1803, reviewer twin loop 2365-2577.

### Q2 — Named timeouts

| Name | Definition | Value | Usage |
|---|---|---|---|
| `agentReadyTimeout` | `workLoopDeps` field workloop.go:505; wired from `Config.AgentReadyTimeout` (daemon.go:161-177) at workloop.go:1143 | 0 -> `defaultAgentReadyTimeout=150s` (agentready.go:64); remote `defaultRemoteAgentReadyTimeout=210s` (:80) | `effectiveAgentReadyTimeout(...)` at workloop.go:4853, reviewloop.go:722+1460, dot_gate.go:436, dot_cascade.go:1705; consumed by `waitAgentReady` (agentready.go:154-200) blocking on `time.After` at :194 |
| `postAgentReadyHangTimeout` | `workLoopDeps` field workloop.go:517-523 | 0 -> `defaultPostAgentReadyHangTimeout=7min` (postreadyhang.go:37) | reviewloop.go:776 -> `waitPostAgentReadyProgress` (postreadyhang.go:55-74), blocking select on `time.NewTimer(timeout)` (:59) |
| `noChangeTimeoutCh` | declared workloop.go:4934, created :4996, passed to `pasteInjectQuitOnCommit` :4998 | closed by pasteinject.go:908 and :1046 on watchdog kill | consumed non-blockingly at workloop.go:5331 (`select {case <-...: default:}`). Watchdog internals: `commitPollInterval=500ms`(:603), `commitPollTimeout=30min` progress-extended(:623), `commitHardCeiling=90min` backstop(:639), `heartbeatStalenessThreshold=8min` primary kill(:658), `postQuitKillGrace=60s`(:769) |
| `resumeReadyFallbackGrace` | `const = 2*time.Second` reviewloop.go:110 (rationale :85-109, hk-isq02) | 2s | reviewloop.go:659 - goroutine `case <-time.After(resumeReadyFallbackGrace)` synthesizing agent_ready on `--resume` iterations >=2 |
| `agentReadyKillReapTimeout` | `var = 10*time.Second` workloop.go:141 (**var so tests override**, export_test.go:648) | 10s | workloop.go:4871+4883, reviewloop.go:738/744/1478/1484, dot_gate.go:447, dot_cascade.go:1716 |

**StaleWatcher** (stalewatch.go) owns heartbeat/stale detection with its own config: `StaleAfter`10min, `ReviewerLaunchStaleAfter`30min, `ScanInterval`30s, `NeverSpawnedReaperTimeout`30min, `AgentReadyStallThreshold`3min, `ForceReapGrace`90s, `DeadProcessStaleAfter`2min - and **already has a `Now func() time.Time` seam** (stalewatch.go:336). Non-run: `workloopPollInterval=2s`(:79), `shutdownDrainTimeout=10s`(:97), `periodicCoordinatorReapInterval=5min`(:147), `dashboardGateEvalInterval=30s`(:154).

### Q3 — substrate.ClockPort API (`internal/substrate/clock.go`)

```go
type ClockPort interface {
    Now() time.Time
    Since(t time.Time) time.Duration
    NewTicker(d time.Duration) Ticker
    // Sleep waits for d or until ctx is cancelled; reports whether the full d elapsed.
    Sleep(ctx context.Context, d time.Duration) bool
}
type Ticker interface { C() <-chan time.Time; Stop() }
```
(clock.go:10-24). `SystemClock` (clock.go:30-56) delegates to package `time`; `Sleep` is `time.NewTimer` + select on `ctx.Done()`.

**FakeClock** (`internal/substrate/fakeclock.go`): virtual mutex-guarded `now`; only `Advance(d)` (:114-128) moves time, walking the timeline in deadline order, firing tickers at each interval boundary and waking sleepers -> deterministic interleavings. Ticker semantics match `time.Ticker` (first tick after full interval :71/:75-89; buffer cap 1 non-blocking send so ticks coalesce :156-169; `Stop` doesn't drain :93-107). `Sleep` registers a deadline, blocks on done vs ctx.Done (:48-63). `BlockUntil(n)` (:188-198) spins until `len(sleepers)+len(tickers)>=n` - the anti-race primitive for "wait for the reactor goroutine to arm before advancing". **`Advance` is a `*FakeClock` method, NOT part of `ClockPort`.** No one-shot `time.After` analog on the port - express a one-shot as `Sleep(ctx,d)` or a `NewTicker(remaining)` used once (keeper does the latter, shell.go:201).

### Q4 — Keeper D4/D11 template

**D4** (`.kerf/works/session-restart-substrate/04-design/00-decisions.md:127-147`): ClockPort lives in `internal/substrate` (SB-R9); keeper takes it by reference; "available to the daemon later". `Since` kept as sugar for interval-gate sites.

**D11** (00-decisions.md:290-312): pure `Step(state, Event) -> (state, []Action)` reactor "with ONE structural addition codex lacks: **timers as events**. The reactor emits `ArmTimer{kind,d}` / `CancelTimer{kind}` actions and consumes `TimerFired{kind}` events; the shell owns ClockPort and converts." Every timestamp in state comes from an event's Clock-stamped `At`, never a clock call inside Step.

Concrete types (`internal/keeper/`):
- **Reactor vocabulary** (step.go): `EvTimerFired EventKind="timer_fired"`(:74); `TimerKind` names each timer(:79-90); `ActArmTimer`/`ActCancelTimer`(:139-140); flat `Action` carries `Timer TimerKind`+`D time.Duration`(:154-155); flat `Event` carries `Timer TimerKind`+`At time.Time`(:107-114). `stepCycle` total+pure, no IO/clock reads(:13-34).
- **Shell owns deadlines** (cycle.go:639-645 + shell.go): `Cycler` holds `timers map[TimerKind]time.Time`+`timersArmed`. `execute(ActArmTimer)` stamps `c.timers[a.Timer]=c.cfg.Clock.Now().Add(a.D)` - anchored at execution time (shell.go:103-123); `ActCancelTimer`->`delete`(:124-125).
- **Drive loop** (shell.go:190-221): one detection ticker `Clock.NewTicker(PollInterval)` per armed-timer generation, plus a punctual deadline wake `Clock.NewTicker(remaining)` at `nearestDeadline()`(:198-203,:227-242). Select on ctx.Done/ticker.C/deadlineC.
- **Timer->event** (`pollOnce`, shell.go:246-312): each tick `at:=Clock.Now()`; if armed deadline elapsed (`!at.Before(dl)`) delete + `feed(Event{Kind:EvTimerFired,Timer:...,At:at})` - timeout-checked-before-read; else read detection source.
- **Cancellation** (`fireOnCancel`, shell.go:318-336): parent-ctx cancel maps onto the phase's timeout edge as a synthesized `EvTimerFired`.
- **Config seam** (cycle.go:92-97,:358-360): `CyclerConfig.Clock substrate.ClockPort`; nil->`SystemClock{}`; tests pass `FakeClock`.
- Generic driver: `substrate.Run[E,A]` (seam.go:27).

### Q5 — Existing daemon test-clock seams (reconcile to ONE)

No unified seam today; **four disjoint mechanisms**:
1. `daemonTestHooks` (daemon.go:571-620) has **NO time hook** (busObserver, brAdapterFactory, spendMeterObserver, worktreeFactory, mergeMu only).
2. **Package-var mutation**: `agentReadyKillReapTimeout`(workloop.go:141, setter export_test.go:648); pasteinject `commitPollInterval/commitPollTimeout/commitHardCeiling/heartbeatStalenessThreshold` mutated directly by tests. Racy under parallel tests, invisible in design.
3. **`Now func() time.Time` config fields** in three subsystems: `StaleWatchConfig.Now`(stalewatch.go:336), crewidlereap.go:100, subscribe.go:205 - each "nil->time.Now".
4. **Duration-config injection**: `Config.AgentReadyTimeout/RemoteAgentReadyTimeout/PostAgentReadyHangTimeout` shrink the wait but still burn real time; env-tuned `splashDismissDelayDur()` etc. (pasteinject.go:76/122/171).

`queue.AdvanceGroup` already takes `now time.Time` (workloop.go:6142 etc.) - queue package is already clock-pure at its boundary.

## Patterns to follow (keeper template -> M3)

- Add `Clock substrate.ClockPort` to run-lifecycle deps (next to `agentReadyTimeout`, workloop.go:499-523), default `SystemClock{}` when nil - exactly `CyclerConfig.Clock` (cycle.go:92-97,:358-360).
- Reactor: timers-as-events per D11 - `TimerKind` per named timeout (agent_ready, post_ready_hang, kill_reap, resume_ready_grace, commit_budget/heartbeat_stale/hard_ceiling); `ArmTimer{kind,d}`/`CancelTimer{kind}` actions; `TimerFired{kind,at}` events; all state timestamps event-`At`-sourced, zero clock reads in Step.
- Shell: `timers map[TimerKind]time.Time`+`timersArmed` regeneration; deadline stamped at execution time; detection ticker + punctual `nearestDeadline` wake; timeout-before-read at boundaries; ctx-cancel mapped to phase timeout edge.
- Property test: `FakeClock.Advance` + `BlockUntil` to serialize advance-after-arm - makes bounded-liveness deterministic (mirrors keeper SR9/SK-014 fail-open TimerModelDone: a timer firing must always move the machine, never wedge - the shape the daemon's `commitHardCeiling`/never-spawned-reaper liveness property wants).

## Risks / conflicts

1. **The run-lifecycle timeout surface spans FIVE files**, not two: workloop.go, reviewloop.go, agentready.go(:194), postreadyhang.go(:59), pasteinject.go (the whole quit-on-commit watchdog). C1 scoped to only workloop/reviewloop leaves the three deepest wall-clock waits untouched. **Design must expand C1's scope statement to name these five files.**
2. **`context.WithTimeout` sites** (workloop.go:4883, reviewloop.go:744/1484, dot_gate/dot_cascade peers) are wall-clock timeouts no ClockPort call replaces - need conversion to `Clock.Sleep`/timer-event or explicit exemption, else the liveness test still burns real time.
3. **Seam proliferation** (Q5): four existing mechanisms. Design must pick ONE (`substrate.ClockPort` in deps) and state the retirement story for the mutable vars and the `Now func()` fields, or the codebase ends with five.
4. **FakeClock has no `time.After` analog**; every `select{case <-time.After(d)}` becomes `Clock.Sleep(ctx,d)` in a goroutine, a one-shot `NewTicker(remaining)`, or a reactor timer event - straight mechanical substitution is unavailable.
5. **`noChangeTimeoutCh` is a cross-goroutine signal**, not a timer: the watchdog goroutine (pasteinject.go) closes it after its own clock decisions. Determinism requires either threading Clock into `pasteInjectQuitOnCommit` too, or reifying the watchdog kill as a reactor event (the cleaner D11 move).
6. **StaleWatcher already has `Now func()`** (stalewatch.go:336) but uses raw tickers for `ScanInterval`; if the liveness property covers never-spawned-reaper / force-reap, its ticker needs ClockPort and its `Now` field should reconcile to the same port rather than persist as a second seam.

## PLANNER-RECONCILE

- **C1 scope is broader than 02-components.md states.** The decompose named "26 time.Now() sites in workloop.go"; the verified surface is 38 in workloop.go + 8 in reviewloop.go + the three deepest waits in agentready.go / postreadyhang.go / pasteinject.go. The bounded-liveness property test (C5) is impossible-to-make-deterministic unless C1 also covers agentready/postreadyhang/pasteinject. Design proceeds with the expanded five-file scope; flagged for planner because it enlarges C1's blast radius beyond the decompose estimate.
