# 04-Design / session-keeper — the session-restart vertical behind the seam

> **Pass 4 (Change Design), session-keeper component.** Elaborates D4/D7/D10/D11/D12 (pinned in
> `00-decisions.md`) into the concrete design a spec-drafter and an implementer follow. Grounded in
> `03-research/session-keeper/findings.md` (the code map; cited as **sk §**, with `file:line`),
> `03-research/substrate/findings.md §4.2/§4.3` (**sub §**), `03-research/events/findings.md`
> (**ev §**), and the 02-components requirements SK-R1..R11. It MUST NOT contradict the pins;
> where a pin fixes a signature this doc restates it verbatim and designs around it.
>
> Scope note (Constraint 1 — no daemon): everything here is the **off-daemon standalone
> `harmonik keeper` process**. There is exactly one writer per agent through one `FileEmitter`
> mutex (`watcher.go:84-99`), so per-cycle intra-agent event ordering is sound (sk §4). The daemon
> side is untouched.

---

## 0. Package shape and the object graph

The rebuild keeps `internal/keeper` as the vertical package. It gains a pure sub-machine and five
consumer-owned ports; it imports `internal/substrate` for the seam (`Run`, `EventSource`,
`Effector`, fakes, `Twin`, `ReplayCodec`) and `ClockPort` (D4). No new top-level subsystem package
is created — the ports are declared where consumed (SB-R13 / `internal/queue` idiom, sub §5).

```
watcher poll loop ─┐                        (imperative shell)
                   ├─ sample ports ──► []keeper.Event ──► substrate.Run(ctx, src, Cycle.Step, eff)
ClockPort tickers ─┘                                             │
                                                    pure Step(State, Event) → (State, []Action)
                                                             │
                          eff.Execute(Action) ──► PanePort / GaugePort / HandoffPort /
                                                  EmitterPort / RespawnPort / ClockPort(timers)
```

`Cycle` (the pure reactor) is the analog of `codexreactor.Reactor` (`reactor.go:168-277`, sub §1.2):
it holds `State`, exposes `Step(ev Event) []Action` and `State()`, and is driven by the free
function `substrate.Run[keeper.Event, keeper.Action]` (D1 — `Run` is a free function, not a method;
the keeper wrapper is one line, mirroring the codex wrapper in D1). Everything with IO is the shell.

---

## 1. The five ports — final interfaces

D10 promotes the 22 `CyclerConfig` function-fields (`cycle.go:38-243`, defaults `cycle.go:251-377`,
sk §1) to five named ports plus a one-method `RespawnPort`. Policy scalars (thresholds, timeouts,
`PollInterval`, `HandoffTimeout`, `ClearSettle`, `ClearConfirmBackstop`, `MaxHandoffTimeouts`,
`HoldTTL`, boot-grace constants) stay `CyclerConfig` fields — they are inputs, not IO, and several
are operator-HARD-NO-pinned bands (`thresholds.go:10-14,23-52`). Every function-field lands on
exactly one port; none is lost.

### 1a. PanePort — the tmux write/read boundary

```go
// PanePort — the tmux boundary. Inject MUST follow PL-021d (load-buffer +
// paste-buffer write discipline); Capture is keeper-only (PL-021b §5 forbids
// the daemon this read). SK-R11.
type PanePort interface {
    Inject(ctx context.Context, target, text string) error
    SendEscape(ctx context.Context, target string) error
    SetEnv(ctx context.Context, target, key, value string) error
    Capture(ctx context.Context, target string) (string, error)
    OperatorAttached(target string) bool
}
```

| CyclerConfig field (`cycle.go:`) | → PanePort method | Prod default / mechanism |
|---|---|---|
| `InjectFn` :96 | `Inject` | `InjectText` (:331), `injector.go:131-168` load-buffer→paste-buffer→settle 750ms→Enter→2 retries |
| `SendEscapeFn` :175 | `SendEscape` | `SendEscapeKey` `injector.go:184-193`; pre-handoff preempt `cycle.go:986-988` |
| `SetTmuxEnvFn` :116 | `SetEnv` | `SetTmuxEnv` (:355), `injector.go:224-233`; HARMONIK_AGENT `cycle.go:1106` |
| `PaneCapturer` (`awaitack.go:63`) | `Capture` | `CaptureTmuxPane` `awaitack.go:211-225` (`capture-pane -p -S -200`) |
| `OperatorAttachedFn` :186 | `OperatorAttached` | `tmuxresolve.go:213` (`list-clients`); Gate 7 `cycle.go:860` |

`ForceRestartFn` (:169) is **not** a pane write — it is a process-lifecycle kill+respawn. Per D10 it
becomes its own one-method port, never bloating PanePort:

```go
// RespawnPort — kill+respawn escalation after MaxHandoffTimeouts (cycle.go:1063-1074).
type RespawnPort interface { ForceRestart(ctx context.Context, agent string) error }
```

### 1b. GaugePort — file-state read surface + the one managed-session write-back

Per D10, GaugePort keeps only the reads/writes the **shell** performs; the seven gate-predicate
reads are folded into a per-tick `GateSnapshot` (§3b) so the pure `Step` never touches a port. Final
port:

```go
// GaugePort — the keeper's file-state universe (.ctx/.sid/.managed/markers/
// transcript) and the one write-back that keeps the watcher bound.
type GaugePort interface {
    ReadGauge() (*CtxFile, time.Time, error)          // .ctx (+ .sid overlay when primary UUIDv4)
    SetManagedSession(sessionID string) error         // "" clears; rebind after cycle
    ClearPrecompactTrigger() error                    // remove .precompact marker
    // The gate-input reads below are sampled by the shell into a GateSnapshot,
    // never called from Step:
    Snapshot(sessionID string) GateSnapshot           // one read-burst per tick
}
```

`Snapshot` performs the read-burst that today is scattered across `CrispIdleFn`, `HoldingDispatchFn`,
`SleepingCheckFn`, `HeldCheckFn`, `IsManagedFn`, `RecentTranscriptTurnFn`, plus the operator-attached
probe, returning the `GateSnapshot` value that rides on `GaugeTick` (§3b). Its impl takes the Clock
so `gates.go:126,:185` marker stamps and `:256` hold-TTL math (sk §2c) route through ClockPort.

| CyclerConfig field (`cycle.go:`) | → surfaced as | Prod default / mechanism |
|---|---|---|
| `ReadGaugeFn` :97 | `ReadGauge` | `ReadCtxFile` `gauge.go:34-63`, `.sid` overlay `:59-61` |
| `SetManagedSessionFn` :110 | `SetManagedSession` | `WriteManagedSessionID` (:352); rebind `:1138`, abort-clear `:1053` |
| `ClearPrecompactTriggerFn` :102 | `ClearPrecompactTrigger` | `gates.go:44-52` |
| `IsManagedFn` :86 | `Snapshot.Managed` | `.managed` stat; Gate 1 `:658` |
| `CrispIdleFn` :98 | `Snapshot.CrispIdle` | `.idle`/`.ctx` mtime, 10s tol `gates.go:67-93` |
| `HoldingDispatchFn` :99 | `Snapshot.HoldingDispatch` | `.dispatching` fail-closed `gates.go:102-111` |
| `SleepingCheckFn` :204 | `Snapshot.Sleeping` | `.sleeping.<sid>` fail-closed `gates.go:282-289` |
| `HeldCheckFn` :214 | `Snapshot.Held` | `.hold.<sid>`+TTL `gates.go:232-260` |
| `RecentTranscriptTurnFn` :242 | `Snapshot.LastUserTurnAt/LastAssistantTurnAt` | transcript tail `tmuxresolve.go:266`; Gates 5d/5e |
| `OperatorAttachedFn` :186 (read side) | `Snapshot.OperatorAttached` | (also on PanePort; Snapshot mirrors it for the ladder) |

### 1c. HandoffPort — handoff file + cycle journal (journal placement settled here)

Per D10 the journal lives **in HandoffPort** (not a separate `JournalPort`): its observability role
is superseded by the four events (§4) but its crash-recovery role (`RecoverFromCrash`,
`cycle.go:1197-1244`) is retained, and phase vocabulary stays byte-identical
(`opened/handoff_injected/confirmed/cleared/resumed/complete/aborted`, `cycle.go:25-26`) — a parity
requirement (sk §1c).

```go
// HandoffPort — the handoff-file + cycle-journal filesystem surface.
type HandoffPort interface {
    HandoffPath() string
    ReadHandoff() (string, error)
    HandoffModTime() (time.Time, bool)
    TruncateHandoff() error
    WriteJournal(j *CycleJournal) error
    ReadJournal() (*CycleJournal, error)
}
```

| CyclerConfig field (`cycle.go:`) | → HandoffPort method | Prod default |
|---|---|---|
| `HandoffFilePath` :87 | `HandoffPath` | `defaultHandoffFilePath` :443 (`<dir>/HANDOFF-<agent>.md`) |
| `ReadHandoff` :88 | `ReadHandoff` | :447; nonce poll `:1523-1525`, stale-nonce `:517-529` |
| `HandoffModTimeFn` :94 | `HandoffModTime` | :458; freshness recovery `:918-928` |
| `TruncateHandoffFn` :95 | `TruncateHandoff` | :466; stale-nonce wipe `:971-973` |
| `WriteJournalFn` :100 | `WriteJournal` | `writeJournalFile` :471 |
| `ReadJournalFn` :101 | `ReadJournal` | :487; crash recovery only `:1204` |

### 1d. EmitterPort — durable bus (kept verbatim, SK-R1)

D10: **EmitterPort = `keeper.Emitter` unchanged.** It is already the exact `EmitWithRunID` subset of
`handlercontract.EventEmitter` (`handlercontract/watcher_hc011.go:41`), so every
`handlercontract.EventEmitter` and `eventbus.EventBus` already satisfies it (sk §1d) — zero
adaptation code, SK-R1 satisfied by subsetting:

```go
// keeper.Emitter — UNCHANGED (watcher.go:23-25). Prod impl FileEmitter
// (watcher.go:39-100); test impl RecordingEmitter (watcher.go:102-134).
type Emitter interface {
    EmitWithRunID(ctx context.Context, runID core.RunID, eventType core.EventType, payload []byte) error
}
```

One hardening (D9): `FileEmitter`'s `TimestampWall: time.Now().UTC()` stamp (`watcher.go:68`) routes
through ClockPort for deterministic replay envelopes; and the four new emits MUST NOT swallow failure
(today every keeper emit is `_ =`) — at minimum log-on-failure (§4).

### 1e. ClockPort — from substrate (D4), restated verbatim

The clock port is pinned in D4 and lives in `internal/substrate`; keeper requires it by reference:

```go
// substrate.ClockPort (D4 / SB-R9). Real impl delegates to package time; the
// fake advances virtual time.
type ClockPort interface {
    Now() time.Time
    Since(t time.Time) time.Duration
    NewTicker(d time.Duration) Ticker
    Sleep(ctx context.Context, d time.Duration) bool  // sleepCtx shape; reports full-d-elapsed
}
type Ticker interface { C() <-chan time.Time; Stop() }
```

Migration of the 34 cycle sites + injector + watcher + the two existing `cfg.Now` seams onto this
port is §2.

---

## 2. ClockPort migration

ClockPort absorbs **no existing `CyclerConfig` field** — that is the gap: the auto-cycle has zero
clock seam today while `RestartNowConfig.Now` (`restartnow.go:54`) and `AwaitAckConfig.Now`
(`awaitack.go:78`) already exist (sk §1e). D4/SK-R3 close it. Every clock read below is replaced by a
ClockPort call; the shell owns the Clock and never lets a clock call happen inside `Step` (D11 —
timestamps in state come from event `at` fields, Clock-stamped by the shell).

### 2a. The 34 `cycle.go` sites (sk §2b, grep-verified 2026-07-13)

- **19 `time.Now` → `Clock.Now()`**: `:435` (cycle-id prefix), `:707` (boot-grace arm), `:937/:945/
  :984` (journal/force/handoff anchors), `:999,:1020,:1031,:1082,:1114,:1150,:1155` (journal
  `UpdatedAt` per phase), `:1219,:1227,:1235` (`RecoverFromCrash`), `:1468` (idle-restart stamp),
  `:1494` (operator-attached throttle window), `:1541/:1546` (backstop deadline arm + check).
- **13 `time.Since` → `Clock.Since(t)`**: `:714,:730,:735,:739` (boot-grace math), `:794,:799`
  (Gate 5d lookback), `:811,:813` (Gate 5e grace), `:832,:844` (Gate 6 force-retry), `:1304,:1307`
  (`RunForPrecompact` boot-grace), `:1450` (idle cooldown).
- **2 `time.NewTicker` → dissolved into timer events** (below): `:1515` (`pollForNonce`),
  `:1562` (`waitForNewSessionID`).

In the reactor split, the `time.Now`/`Since` interval gates (Gates 5d/5e/6, boot-grace, cooldowns)
are **pure math in `Step`** over event-carried timestamps: the shell stamps each event's `at` with
`Clock.Now()`, and `Step` compares `event.at` against state timestamps (`Clock.Since` becomes
`event.at.Sub(stateTS)` arithmetic on values already in the event). No clock call survives inside the
pure core. `newCycleIDGen`'s hidden `time.Now()` (`:435`) either derives its prefix from `Clock.Now()`
in the shell or the generator stays injected as today (parity-safe either way, D4/sk §1e); the shell
mints the id and puts it on the entry event so `Step` never mints.

### 2b. injector.go, watcher.go, and the two existing `Now` seams

- **`injector.go` sleeps → `Clock.Sleep(ctx, d)`.** `sleepCtx` (`injector.go:197-206`, a
  `select ctx.Done() vs timer.C`) is the shape D4's `Sleep` was built to preserve — a bare
  `Sleep(d)` cannot honor the cancellation `InjectText` relies on at `injector.go:148,:161`. The two
  callers are `submitSettle` (750ms, `:148`) and `submitRetryDelay` (400ms ×2, `:161`) — the *only*
  real-timer sleeps in the package (no `time.Sleep` anywhere). Routing must not reorder the
  750ms/400ms×2 sequence (`TestInjectText_SettleConstants` guards it, parity risk #8).
- **`watcher.go` ticker → the Clock-driven tick generator.** The watcher's main `time.NewTicker`
  (`:1003`) becomes `Clock.NewTicker(cfg.PollInterval)`; the ~30 `time.Now`/`Since` sites in
  `watcher.go` (`:68,:1025..:1687`, sk §2c) route through ClockPort. The watcher loop is the reactor's
  **event source** (§3): each tick samples ports and yields events.
- **`restartnow.go` / `awaitack.go` `cfg.Now` → `Clock ClockPort`.** These two already have a
  `Now func() time.Time` field nil-defaulted to `time.Now` (`restartnow.go:69`, `awaitack.go:96`),
  driven by fake clocks in their tests (sk §2d). Migration: replace the `Now` field with
  `Clock ClockPort`; `awaitack.go:165` already calls `sleepCtx`, so it takes `Clock.Sleep` too;
  `awaitack.go:162`'s `time.Until(deadline)` derives from `Clock.Now()` (no extra method). Keep `Now`
  as a deprecated alias in the CLI constructor for one release if wiring churn is a concern.

### 2c. The two poll loops' `context.WithTimeout` → armed `TimerFired` events (D11)

Both poll loops carry a real-time deadline in the ctx, not just the ticker (`cycle.go:1512`, `:1559`,
sk §2b note). Deterministic replay cannot honor a wall-clock ctx deadline, so D11 dissolves both loops
into **armed timers the reactor owns as events**. The reactor emits `ArmTimer{kind,d}` /
`CancelTimer{kind}` actions; the shell holds a Clock timer per armed kind and emits `TimerFired{kind}`
when it elapses. The four timer kinds, their durations, and the state that arms them:

| Timer kind | Duration (default) | Armed on entering | Replaces (today) | Fired-in-state action |
|---|---|---|---|---|
| `handoff_timeout` | `HandoffTimeout` = **300s** | `AwaitingHandoff` | `pollForNonce` ctx-timeout `:1512` | fresh⇒`AwaitModelDone`(recovered); no-fresh⇒`Aborted` |
| `model_done_timeout` | **~60s** (D12, `< ClearConfirmBackstop`) | `AwaitModelDone` | none (new, SR4) | ⇒`Clearing`, degraded flag |
| `clear_settle` | `ClearSettle` = **10s** | `Clearing` (re-armed per retry) | `waitForNewSessionID` ctx-timeout `:1559` | retries left⇒re-`InjectClear`+re-arm; else fall to backstop |
| `clear_backstop` | `ClearConfirmBackstop` = **150s** | `Clearing` (once) | backstop deadline `:1541` | ⇒`Briefing` via `clear_unconfirmed` |

The 200ms **detection** cadence (nonce appearing in the handoff file, SID flipping in the gauge) is a
*shell* poll, not a reactor timer: while off-`Idle` the shell runs `Clock.NewTicker(PollInterval)`
and, per tick, reads the phase-appropriate source and emits `NonceObserved`/`HandoffFreshSeen`/
`ModelDone`/`SessionChanged`. This is distinct from the four reactor timers and must reproduce
**first-tick-after-interval** semantics (parity risk #4): the first detection read happens one full
`PollInterval` after state entry, exactly as `time.NewTicker` delivers today (`cycle.go:1515-1528`,
`:1562-1575`).

---

## 3. The `Step` reactor — full design

Mirrors `codexreactor` (`reactor.go:185-277`, sk §3a) — flat JSON-round-trippable `Event`/`Action`
structs, `State` inspectable between steps, invariants enforced *in* `Step`, driven by
`substrate.Run`. The one structural addition codex lacks: **timers as events** (D11 / §2c), which
makes every timeout race a replayable event interleaving.

### 3a. Event vocabulary (shell → reactor)

| Event | Produced by (shell) | Today's analog |
|---|---|---|
| `GaugeTick{cf CtxFile, at time.Time, gates GateSnapshot}` | watcher poll tick (`watcher.go:1003→1240`), **only when Idle** | `MaybeRun(ctx, cf)` `cycle.go:656` |
| `PrecompactTrigger{cf, at, gates}` | `.precompact` marker seen (`watcher.go:1251`) | `RunForPrecompact` `cycle.go:1276` |
| `IdleRestartTick{cf, at, gates}` | idle-branch of the tick | `RunForIdle` `cycle.go:1405` |
| `NonceObserved{cycleID, at}` | detection poll finds nonce | `pollForNonce` true `cycle.go:1003` |
| `HandoffFreshSeen{cycleID, mtime, at}` | mtime ≥ injectedAt after handoff_timeout | `handoffWrittenAndFresh` `cycle.go:918-928` |
| `ModelDone{cycleID, sessionID, at, source}` | **NEW** — §5 (`.idle`-mtime transition) | none |
| `SessionChanged{cycleID, prevSID, newSID, at}` | gauge SID ≠ prevSID (`cycle.go:1570-1573`) | `waitForNewSessionID` success |
| `TimerFired{cycleID, kind, at}` kinds: `handoff_timeout`, `model_done_timeout`, `clear_settle`, `clear_backstop` | Clock-armed timers | ctx-timeouts `:1512,:1559`, deadline `:1541` |
| `CrashJournal{j CycleJournal, at}` | boot, journal present | `RecoverFromCrash` `cycle.go:1197` |

`GateSnapshot` (sampled by the shell, one GaugePort read-burst per tick — sk §3b):

```go
type GateSnapshot struct {
    Managed             bool
    CrispIdle           bool
    HoldingDispatch     bool
    Sleeping            bool
    Held                bool
    OperatorAttached    bool
    LastUserTurnAt      time.Time // Gate 5d
    LastAssistantTurnAt time.Time // Gate 5e
}
```

Sampling in the shell keeps `Step` pure; the watcher already computes two of these per tick
(`watcher.go:1227-1228`).

### 3b. Action vocabulary (reactor → effector)

| Action | Port | Today's site (`cycle.go:`) |
|---|---|---|
| `WriteJournal{phase, reason}` | HandoffPort | :955,:998-1000,:1022,:1033,:1081-1083,:1113-1115,:1149-1151,:1154-1157,:1218-1237 |
| `TruncateHandoff` | HandoffPort | :971-973 |
| `SendEscape` | PanePort | :986-988 |
| `InjectHandoffCmd{cycleID}` (text from `nonceMarker` :502-504) | PanePort | :993 |
| `InjectClear` | PanePort | :1111; re-inject :1551 |
| `InjectBrief` (`briefRestartCmd` :20) | PanePort | :1147; crash path :1216 |
| `SetTmuxEnv{HARMONIK_AGENT}` | PanePort | :1106 |
| `SetManagedSession{sid}` (incl. `""`) | GaugePort | :1053,:1138 |
| `ClearPrecompactMarker` | GaugePort | 8 sites `:1285-1378` |
| `SetHold` (Gate 5d side effect) | GaugePort | :797 |
| `Emit{type, payload}` | EmitterPort | the emit* sites (§4 + :1421,:1484,:1586,:1598,:1610,:1621,:1632) |
| `ArmTimer{kind,d}` / `CancelTimer{kind}` | shell/Clock | replaces the two poll loops + backstop |
| `ForceRestart` | RespawnPort | :1069 |

### 3c. State machine — the complete transition table

State (per agent; one `Cycle` instance): a `Phase` enum + the anti-loop/hysteresis fields
(`cycle.go:559-626`) lifted verbatim into reactor `State`: `lastFiredSID`,
`seenLowPctAfterLastFire`, `lastFireWasAbort`, `lastForcedAttemptAt`, `consecutiveHandoffTimeouts`,
boot-grace (`currentSessionID`, `currentSessionIDSince`, `seenSessionIDs`, `bootGraceFirstArmAt`),
`lastIdleRestartAt`, `lastIdleCrewNotifiedSID`, `lastOperatorAttachedEmit`, plus the in-flight
`cycleID`, `injectedAt`, `clearAttempt`, `prevSID`, `handoffMtime`. Every timestamp comes from an
event `at` (Clock-stamped by the shell), never a clock call inside `Step` (D11).

The **11-gate ladder** (`MaybeRun`, `cycle.go:656-865`) is **not states** — it is a pure predicate +
an unconditional side-effect prelude, evaluated **only in `Idle`** on `GaugeTick`/`PrecompactTrigger`/
`IdleRestartTick`. That "ladder only in Idle" *is* SR7's structural guarantee (sk §3d). Gates in exact
order (all preserved, order included): 1 `.managed` (:658) · 2 empty-SID (:662) · *prelude
(unconditional): re-arm observation :669-671, same-SID escape hatch :686-691, boot-grace SID tracking
:701-724* · boot-grace gate :728-742 (force-exempt) · 3 below act threshold :747 · 4 CrispIdle-unless-
force :754-760 · 5 HoldingDispatch :762 · 5b Sleeping :769 · 5c Held :777 · 5d recent operator turn →
auto-hold (writes `SetHold`) :792-803 · 5e post-answer grace :809-817 · 6 anti-loop + force-retry
exceptions :825-852 · 7 operator-attached :860-863.

Full transition table (elaborating sk §3d). Each row: `state × event [guard] → state' + [actions]`.

**Idle**
| Event | Guard | → | Actions |
|---|---|---|---|
| `GaugeTick`/`PrecompactTrigger`/`IdleRestartTick` | prelude runs unconditionally; then ladder-pass (entry-specific gate subset) | `AwaitingHandoff` | `WriteJournal(opened)`, `Emit(handoff_started)`, `TruncateHandoff?`, `SendEscape`, `InjectHandoffCmd{cycleID}`, `WriteJournal(handoff_injected)`, `ArmTimer(handoff_timeout, 300s)` |
| same | ladder-fail | `Idle` | prelude side effects only (`SetHold` on 5d path; `ClearPrecompactMarker` + `Emit(precompact_blocked)` on the precompact entry's per-gate path, incl. the `hold_dispatch_skip` empty-SID quirk `:1292`) |
| `CrashJournal` | phase `cleared` | `Briefing` | fast-forward (below) |
| `CrashJournal` | phase `resumed`/terminal | `Complete`/`Aborted` | close-out per `RecoverFromCrash` matrix (`:1212-1241`) |
| any timer/detection event | — | `Idle` | ignored (no cycle in flight) |

**AwaitingHandoff**
| Event | Guard | → | Actions |
|---|---|---|---|
| `NonceObserved` | — | `AwaitModelDone` | `WriteJournal(confirmed)`, `Emit(handoff_written)`, `CancelTimer(handoff_timeout)`, `ArmTimer(model_done_timeout, 60s)` |
| `TimerFired(handoff_timeout)` | `HandoffFreshSeen` present (mtime ≥ injectedAt) | `AwaitModelDone` | `WriteJournal(confirmed, reason=handoff_timeout_recovered)`, `Emit(handoff_written{recovered:true})`, `ArmTimer(model_done_timeout)` |
| `TimerFired(handoff_timeout)` | no fresh handoff | `Aborted` (terminal) | `WriteJournal(aborted)`, `Emit(cycle_aborted, reason=handoff_timeout)`, anti-loop state update, `SetManagedSession("")?` (guarded `!currentSessionIDSince.IsZero()`, :1052-1057), `ForceRestart?` (after `MaxHandoffTimeouts`) |

**AwaitModelDone** (NEW state — SR4, §5)
| Event | Guard | → | Actions |
|---|---|---|---|
| `ModelDone` | mtime(.idle) ≥ t_nonce, or transcript backstop | `Clearing` | `Emit(model_done{source})`, `SetTmuxEnv`, `InjectClear`, `Emit(clear_sent{attempt:1})`, `WriteJournal(cleared)`, `CancelTimer(model_done_timeout)`, `ArmTimer(clear_settle,10s)`, `ArmTimer(clear_backstop,150s)` |
| `TimerFired(model_done_timeout)` | — | `Clearing` | same as above but `Emit(model_done{degraded:true})` (fail-open, §5) |

**Clearing**
| Event | Guard | → | Actions |
|---|---|---|---|
| `SessionChanged` | newSID ≠ prevSID | `Briefing` | `Emit(new_session_up{prev,new})`, `SetManagedSession(newSID)`, `CancelTimer(clear_settle)`, `CancelTimer(clear_backstop)` |
| `TimerFired(clear_settle)` | retries left | `Clearing` | `InjectClear` (defensive, :1550-1552), `Emit(clear_sent{attempt:n})`, re-`ArmTimer(clear_settle)` |
| `TimerFired(clear_backstop)` | — | `Briefing` | `Emit(clear_unconfirmed)`, `SetManagedSession("")`, `CancelTimer(clear_settle)` |

**Briefing** (immediate, no external event)
| Event | Guard | → | Actions |
|---|---|---|---|
| (entry) | — | `Complete` (terminal) | `InjectBrief`, `WriteJournal(resumed)`, `WriteJournal(complete)`, `Emit(cycle_complete)`, `Emit(cycle_recovered)?`, anti-loop state update, return to `Idle` |

Terminal mapping (sk §3d): **cycle_complete** = `Briefing→Complete`; **cycle_aborted** = the
`AwaitingHandoff` timeout-without-fresh path (the ONLY path that never sends `/clear`);
**clear_unconfirmed** = `Clearing` backstop exhaustion — **not a terminal by itself**: the brief still
fires and the cycle still records `cycle_complete` (sk §3d, dossier 02 §5). `RunForPrecompact`
(`:1276-1385`) and `RunForIdle` (`:1405-1474`) are *entry ladders* into the same `AwaitingHandoff`
chain — modeled as the `PrecompactTrigger`/`IdleRestartTick` events with their own gate subsets, not
separate machines. `RecoverFromCrash` is the boot-time `CrashJournal` event.

### 3d. Pure core vs imperative shell (sk §3e)

- **Pure (`Step`):** the ladders, threshold math (delegating to `thresholds.go:214` `minAbsOrPctCeil`,
  already pure), all anti-loop/hysteresis state updates, the phase machine, nonce-string construction
  (`nonceMarker` :502-504) and stale-nonce predicates (`handoffHasStaleNonce`/`isOnlyNonce`
  :517-553, given file content on the event), journal struct *contents*, event payload construction,
  timer arm/cancel *decisions*.
- **Shell (source + effector):** tmux exec (`injector.go`, `tmuxresolve.go`), all file IO (gauge,
  `.sid`, handoff, journal, markers, transcript tail), `events.jsonl` append (`FileEmitter`), the
  detection polls, timer arming via ClockPort, the watcher poll loop as tick source, `ForceRestart`
  respawn, and the `InCycle` suppression (§6).

### 3e. The `ReplayCodec[keeper.Event]` the vertical supplies

Per D2/SB-R4 (sub §4.2) **and the review resolution `00b-review-resolutions.md` R3 (AUTHORITATIVE)**:
the keeper corpus that drives the reactor is an **input-event corpus** — one serialized
`keeper.Event` *input* per line — produced at corpus-BUILD time by the measurement
`StimulusSynthesizer` from each cycle's `summary.json`. The **output→input synthesis lives in that
synthesizer, NOT in the codec** (the raw recorded OUTPUT log — `handoff_started`/`cycle_complete`/…
envelopes — is consumed only by the `internal/replay` invariant-checker, D6/events §4, which does
not drive the reactor). The keeper vertical therefore supplies one codec implementing
`substrate.ReplayCodec[keeper.Event]` that simply **deserializes already-synthesized input lines**:

```go
type keeperCodec struct{ /* stateless; input lines are already synthesized (see measurement §2) */ }

func (c *keeperCodec) DecodeLine(line []byte) (ev keeper.Event, emit bool, err error) {
    // 1. json.Unmarshal one synthesized keeper.Event INPUT line (GaugeTick/NonceObserved/
    //    ModelDone/SessionChanged/TimerFired/...). NO output→input mapping here — that already
    //    happened in the StimulusSynthesizer at corpus-build time.
    // 2. Blank / comment line → emit=false, err=nil  (D3/SB skip-vs-fatal split, R3).
    // 3. Malformed JSON / unknown input kind → err != nil  (FATAL: never a silent skip).
}
func (c *keeperCodec) ErrorEvent(msg string) keeper.Event      // keeper transport-error input stimulus (restart_failed-class)
func (c *keeperCodec) DisconnectEvent() keeper.Event           // keeper connection-lost input stimulus (restart_failed-class)
```
`substrate.Twin[keeper.Event]` then applies the four faults over the decoded input stream — exactly
the codex shape (corpus → codec → Twin → reactor). Old-corpus `ModelDone` synthesis (§5, D12) is
done by the `StimulusSynthesizer` (it inserts `ModelDone` immediately after `NonceObserved` for
pre-rebuild cycles), not by the codec. Cross-ref: measurement-design §2.

Per D3 the keeper — which lacks a natural "disconnect" — supplies its `restart_failed`-class terminal
event for BOTH `ErrorEvent` and `DisconnectEvent`. The scanner buffer defaults to **1 MB** (D3, not
codex's 64 KB) so an oversized keeper bus line does not truncate replay invisibly. Seq/dedup never
enters the substrate surface (D2/R2) — the codec owns any per-cycle counter internally.

### 3f. How the 11-gate ladder stays a pure predicate (parity risk #2)

The ladder is evaluated *only* in `Idle`, and the prelude side effects (re-arm observation :669-671,
same-SID escape hatch :686-691, boot-grace SID tracking :701-724, Gate-5d `SetHold` :797,
`RunForPrecompact`'s per-gate `ClearPrecompactMarker`+`precompact_blocked` emissions) run
**unconditionally before gating** — a "clean" short-circuit would change observable state, so `Step`
emits those prelude actions on the fail path too (§3c Idle rows). The predicate itself reads *only*
the `GateSnapshot` on the event (never a port), so it is a pure function of `(State, Event)`. This is
exactly what keeps the ~347-unconfirmed forensics and the anti-loop scars reproducible.

---

## 4. The four durable interior events — emission design

Registration mechanics (the §8.20 catalog, `EventType` consts, `mustRegister`, `PayloadCompatEntry`,
roundtrip tests, the EV-U5 §8.13 drift reconciliation, the cohort-guard carve-out) are **owned by
`events-design.md`** (D8) — this doc does NOT duplicate the §8.20 catalog work. Here: only the
keeper-side emission (transition + payload + ordering).

All four carry `agent_name`, `cycle_id` (REQUIRED, `Valid()`-checked — D7/EV-U2; the `runCycle` id
from `CycleIDGen` `cycle.go:936`, already present in recorded envelopes as
`payload.cycle_id: "cyc-…"`, ev §2.2), and `session_id`. All keep the envelope run_id absent (the 14
`core.RunID{}` args stay — D7). Emit failures for these four MUST NOT be `_ =` swallowed — log on
failure (D9).

```go
// keeperevents.go — §8.20 cohort (structs here; registration in events-design.md)
type SessionKeeperHandoffWrittenPayload struct {
    AgentName    string `json:"agent_name"`
    CycleID      string `json:"cycle_id"`               // REQUIRED, no omitempty
    SessionID    string `json:"session_id,omitempty"`
    Nonce        string `json:"nonce,omitempty"`         // confirmed nonce marker (audit)
    Recovered    bool   `json:"recovered,omitempty"`    // true via freshness-recovery edge
    HandoffMtime string `json:"handoff_mtime,omitempty"`// RFC3339, carried on recovery
}
type SessionKeeperModelDonePayload struct {
    AgentName string `json:"agent_name"`
    CycleID   string `json:"cycle_id"`
    SessionID string `json:"session_id,omitempty"`
    Source    string `json:"source"`                    // "idle_marker" | "transcript_turn" | "timeout"
    Degraded  bool   `json:"degraded,omitempty"`        // true when model_done_timeout fired
}
type SessionKeeperClearSentPayload struct {
    AgentName string `json:"agent_name"`
    CycleID   string `json:"cycle_id"`
    SessionID string `json:"session_id,omitempty"`
    Attempt   int    `json:"attempt"`                   // 1-based; defensive re-injects increment
}
type SessionKeeperNewSessionUpPayload struct {
    AgentName     string `json:"agent_name"`
    CycleID       string `json:"cycle_id"`
    PrevSessionID string `json:"prev_session_id"`
    NewSessionID  string `json:"new_session_id"`
}
// Valid() on all four asserts CycleID != "" (reconciliation_run_id precedent, ev §2.4).
```

| Event | Emitted at (transition) | Today's silent analog |
|---|---|---|
| `session_keeper_handoff_written` | `AwaitingHandoff → AwaitModelDone` on `NonceObserved` (`:1081`) OR the freshness-recovery edge (`:1016-1026`, `recovered:true`, carry mtime) | journal `Phase="confirmed"` only |
| `session_keeper_model_done` | `AwaitModelDone → Clearing`: `ModelDone` lands, or `model_done_timeout` fires (`degraded:true`) | nothing — no step exists today |
| `session_keeper_clear_sent` | first `InjectClear` (`:1110-1112`, `attempt:1`); each defensive re-inject (`:1550-1552`) re-emits with incremented `attempt` — makes the 347-unconfirmed forensics replayable | journal `Phase="cleared"` only |
| `session_keeper_new_session_up` | `Clearing → Briefing` on `SessionChanged` (`:1128`), immediately before the `.managed` rebind (`:1138`); `prev/new_session_id` | silent success (only the failure emits `clear_unconfirmed`) |

**Ordering invariants (per `cycle_id` c, single agent; SK-R5/R7/R6 → SR3/4/6/7/9):**

- **SR3** — `handoff_written(c)` before `clear_sent(c)`. Structural: `Clearing` is reachable only via
  `AwaitModelDone`, reachable only via the two `handoff_written` edges. (The abort path never clears.)
- **SR4** — `model_done(c)` before `clear_sent(c)`. NEW, via `AwaitModelDone` (§5).
- **SR6** — `new_session_up(c)` before the brief (thus before `cycle_complete(c)`); when absent,
  `clear_unconfirmed(c)` before `cycle_complete(c)` — **exactly one of the two per cycle**.
- **SR7** — no interleaving: between `handoff_started(c1)` and its terminal, no `handoff_started(c2)`
  for the same agent. Enforced structurally by "ladder only in `Idle`".
- **SR9** — bounded liveness: every `handoff_started(c)` reaches exactly one terminal within
  `HandoffTimeout(300s) + model_done_timeout(60s) + ClearConfirmBackstop(150s) + injection overhead`
  ≈ **~520s** (today ≈ ~460s) — or a `restart_failed`-class emission; never silence. Every
  `TimerFired` edge lands in a state with an outgoing action, so the machine cannot wedge without
  emitting.

Ordering substrate: `events.jsonl` is append-only, UUIDv7-`event_id`-ordered (Constraint 5), single
writer per agent (`FileEmitter` mutex `watcher.go:84-99`) — so per-cycle intra-agent `event_id`
assertions are sound. Cross-process global order is only approximate (per-process EventID generators,
ev §4.2), so the harness sorts by EventID after collection (D9) before cross-agent assertions.

---

## 5. The model-done signal (D12) — full design

SR4 has **no code today** (sk §4): after `pollForNonce` succeeds, `runCycle` marks `confirmed`
(`:1081`) and `completeCycleTail` injects `/clear` **immediately** (`:1109-1111`). The nonce proves
the handoff *file content* landed, not that the model's *turn* ended — the model may still be
streaming text/tool calls, and `/clear` races the tail of the turn. D12 introduces a real durable
source plus a fail-open bound.

- **Primary source — the Stop-hook `.idle` marker transition.** `keeper-stop-hook.sh` touches
  `.harmonik/keeper/<agent>.idle` and "fires only at await-input boundaries (verified by Anthropic)"
  (`scripts/keeper-stop-hook.sh:5-7`). Shell rule: after `handoff_written` at `t_nonce`, watch `.idle`
  mtime; the first `mtime(.idle) ≥ t_nonce` is `ModelDone{source:"idle_marker"}` — the model reached
  an await-input boundary AFTER the turn that wrote the handoff. **Strict** compare against the
  nonce-observation instant — **no `crispIdleTolerance` fudge**: that 10s tolerance (`gates.go:67`)
  exists to discount passive `.ctx` repaints, irrelevant here because we compare `.idle` against
  `t_nonce`, not against `.ctx`.
- **Backstop source — `recentTranscriptTurn(dir, sid, "assistant")`** (`tmuxresolve.go:266`, seam
  `RecentTranscriptTurnFn` `cycle.go:242`, now `GaugePort.Snapshot.LastAssistantTurnAt`) with turn-ts
  ≥ `t_nonce` → `ModelDone{source:"transcript_turn"}`. Heavier (JSONL tail scan); for agents whose
  Stop hook isn't wired. The shell prefers `.idle`, falls back to the transcript turn.
- **Liveness bound (SR9) — `model_done_timeout`.** A new `CyclerConfig` scalar, **default ~60s**,
  **strictly less than `ClearConfirmBackstop` = 150s**. On fire the reactor proceeds to `Clearing`
  anyway, emitting `model_done{degraded:true, source:"timeout"}`. *Why < 150s:* the added SR4 wait
  sits *inside* the SR9 budget ahead of the clear phase; if it approached or exceeded the backstop it
  would (a) dominate the liveness window and push SR9's total well past today's ~460s, and (b) let a
  lost Stop-hook write consume most of the budget before `/clear` is even sent — the exact
  resume-hang class SR9 exists to kill. 60s keeps the degraded path clearing comfortably within the
  existing envelope; the timeout path **preserves today's clear-immediately behavior as the degraded
  mode**, so the tightening is fail-open — a lost `.idle` write can never wedge the cycle.
- **Old-corpus replay parity (Constraint 4 / SK-R9 carve-out).** Pre-rebuild recordings contain no
  `model_done` event. The `keeperCodec` (§3e) **synthesizes `ModelDone` immediately after
  `handoff_written`** when replaying old corpora, so old-corpus action goldens stay byte-identical
  (clear fires right after confirm, as today). Only NEW recordings assert the real `ModelDone`
  ordering. This is the sole permitted-divergence item for the SR4 axis in the old-vs-new
  differential (D13).

---

## 6. Behavior-parity plan

Bands/thresholds are pinned (`thresholds.go:23-52`, operator HARD-NO on band changes `:10-14`); all
threshold math keeps routing through `minAbsOrPctCeil` (`thresholds.go:214`). Beyond that, the eight
concrete hazards (sk §5) and their design mitigations:

1. **Blocking → event-driven (BIGGEST RISK).** Today `MaybeRun` runs synchronously inside the watcher
   tick (`watcher.go:1240`); while `pollForNonce`/`waitForNewSessionIDWithBackstop` block (up to
   ~7.5 min), the watcher processes **no ticks** — no warns, no precompact detection, no heartbeat,
   reaper, or hard-ceiling. An unfrozen reactor would emit warn/precompact events that never happened
   in the corpus. **Mitigation — reproduce-the-freeze via `InCycle` suppression (D11):** the shell's
   tick handler checks `Cycle.State().Phase != Idle` (`InCycle`) and, while true, **parks all
   non-cycle processing** (warn state machine `watcher.go:1275-1331`, precompact detection, heartbeat,
   reaper, hard-ceiling) — running *only* the cycle-detection poll + timer-fire that drives the
   reactor forward. When the machine returns to `Idle`, full tick processing resumes. This reproduces
   the synchronous-block freeze exactly, keeping the baseline comparison apples-to-apples. Relaxing
   `InCycle` is a *later, separately-measured* change (D11, deferred), NOT part of this proof.
2. **Gate-prelude side effects run even when gates fail.** Mitigated by §3f: `Step` emits the prelude
   actions (re-arm, same-SID escape, boot-grace tracking, Gate-5d `SetHold`, precompact per-gate
   `ClearPrecompactMarker`+`precompact_blocked` incl. the `hold_dispatch_skip` empty-SID quirk :1292)
   on the ladder-**fail** path too. No short-circuit before the prelude.
3. **Model-done wait shifts `/clear` timing.** Bounded and defaulted small (60s, §5); its effect is
   isolated in the D13 old-vs-new differential's permitted-divergence allowlist so "SR4 wait" is
   distinguished from regression. Favorable case (clearing at a calmer instant reduces the 347
   `clear_unconfirmed`) is measured, not assumed.
4. **Poll quantization.** `time.NewTicker` delivers its first tick only after one full interval; the
   shell's detection poll (§2c) uses `Clock.NewTicker(PollInterval)` and reproduces
   first-tick-after-interval, so nonce/SID are never observed in the first 200ms — matching
   `cycle.go:1515-1528`/`:1562-1575`. `ClearSettle=10s / 200ms = 50 polls` exactly is preserved.
5. **Journal phase vocabulary + crash-recovery matrix.** `RecoverFromCrash` switches on exact phase
   strings + reasons (`crash_before_clear`, `recovered_from_crash`, `:1212-1241`); HandoffPort's
   `WriteJournal`/`ReadJournal` keep the seven phase strings byte-identical (`:25-26`) and the
   `CrashJournal` transitions (§3c Idle rows) copy the matrix exactly.
6. **Anti-loop hysteresis fine structure.** Each scar — `lastFireWasAbort` gating of the escape hatch
   (:686-691 vs :1324-1329), abort-path `.managed` clear gated on `!currentSessionIDSince.IsZero()`
   (:1052-1057), idle-cooldown stamp-then-unwind (:1468-1472), force-retry stamping at cycle START
   (:944-946) — has a regression test (hk-vpnp, hk-ibb, hk-4i0s, hk-qoz). The reactor `State` update
   rules copy them exactly; they surface as action-log diffs under replay if broken.
7. **Emission throttles.** `maybeEmitOperatorAttached` one-per-minute sampling (:1493-1501) becomes a
   `State`-tracked `lastOperatorAttachedEmit` gate in `Step`; `emitOperatorAttached` stays a
   deliberate **NO-OP** (:1507, logmine TA3/F55) — do NOT resurrect it. `idle_crew` once-per-SID
   (:1414-1424) → `lastIdleCrewNotifiedSID` in `State`.
8. **Injector timing constants.** `Clock.Sleep` routing must not alter the 750ms/400ms×2 sequence
   order (`TestInjectText_SettleConstants`, `injector.go:92`). The two `submitSettle`/`submitRetryDelay`
   calls keep their order and durations; only the timer source changes.

**Tests that MUST stay green (Constraint 2):** `TestKeeperConformance`
(`conformance_keeper_test.go:32-38`), `conformance_keeperx_test.go`,
`conformance_keeper_integration_test.go:37`, and the full **~55-file keeper suite** (sk §6). The
existing causal harness `cycle_reactive_harness_test.go` (the `reactiveSession` fake,
`:46-181,:251-263`) and the scenario suites (`cycle_scenario_reactive_test.go:46,170,260`;
`_wave2_test.go:60,151,305,382,512`) become the **golden Step-sequence corpus**: same fakes, re-wired
as `SyntheticSource[keeper.Event]` + `FakeEffector[keeper.Action]`, the injected-command-order
assertions mapping 1:1 onto action-log assertions (sk §3e). The frozen baseline
(`.harmonik/events/baseline-2026-07-13/events.jsonl`, 507 cycles) is the permanent L1 net: action-log
matches golden, terminal matches recorded terminal, aggregate counters match under parity mode
(427 complete / 79 aborted / 347 clear_unconfirmed).

---

## 7. Migration / sequencing

Single-writer on `cycle.go` (off-daemon, one keeper process per agent), so this is a linear refactor
with the keeper suite green at every step. Land in this order:

1. **ClockPort first.** Add `substrate.ClockPort` (D4; substrate-design owns the type). Route the 34
   `cycle.go` sites, `injector.go` sleeps, `watcher.go` ticker/clock sites, and the `restartnow.go`/
   `awaitack.go` `cfg.Now` seams onto it (§2). Pure mechanical substitution; the two poll loops keep
   their `context.WithTimeout` for now (converted in step 3). Keeper suite stays green — the existing
   `restartnow_test.go`/`awaitack_test.go` fake clocks already prove the pattern.
2. **Ports.** Extract PanePort/GaugePort/HandoffPort + RespawnPort from `CyclerConfig` function-fields
   (§1); EmitterPort is already `keeper.Emitter` (no work). `CyclerConfig` retains the policy scalars.
   Wire the production defaults into the real port impls; `RecordingEmitter` and the existing fakes
   become the port fakes. Green.
3. **Step reactor.** Introduce `Cycle.Step` + `State` + the Event/Action vocab (§3), driven by
   `substrate.Run`. This is where the two poll loops + backstop deadline dissolve into `ArmTimer`/
   `TimerFired` (§2c) and the `InCycle` suppression lands in the shell (§6.1). Convert the scenario
   harness to the golden Step-sequence corpus in the same step so parity is proven as the machine is
   built. Green against the ~55-file suite + baseline.
4. **The four events.** Add the emit actions at their transitions (§4); registration lands via
   `events-design.md`'s §8.20 work (D8), which must precede this so the types exist. Add `ModelDone`
   detection + `model_done_timeout` (§5). Green + the new events appear in fresh recordings.
5. **Property tests + Twin.** Supply `keeperCodec` (§3e), build the trace-driven `Twin[keeper.Event]`
   corpus (D13, `testdata/keeper-cycles/baseline-2026-07-13/`), and add the L0–L2 tiers + SR3/4/6/7/9
   property checks + the old-vs-new differential (D13). This is the acceptance net; it does not gate
   the earlier steps' green-ness.

Steps 1–2 are pure parity refactors (zero behavior change); step 3 is the structural rebuild held to
parity by `InCycle` + the golden corpus; step 4 is the only intentional behavior change (SR4), bounded
and measured; step 5 is verification. `old runCycle` is deleted only after step 5's differential is
green (D13); the L1 golden-vs-baseline corpus test is the permanent net that outlives the differential.
