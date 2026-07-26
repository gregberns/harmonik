# 03-Research — session-keeper component findings

> Pass 3 (Research) for `specs/session-keeper.md` (SK). Read against the REAL code at
> commit-state 2026-07-13 (`internal/keeper/*.go`, non-test unless noted). Every claim carries
> `file:line`. Grounds Change-Design for SK-R1..R11. Companion: dossier
> `plans/2026-07-13-code-revamp/research/02-session-restart.md` (verified; corrections noted
> where its counts were approximate).

---

## 1. CyclerConfig → 5 ports mapping

`CyclerConfig` (`cycle.go:38-243`) carries **22 injectable function-fields**; defaults bound in
`applyDefaults` (`cycle.go:251-377`). They partition onto the 5 ports as follows. Fields that
are *policy scalars* (thresholds, timeouts) stay config — they are inputs, not IO.

### 1a. PanePort — tmux inject / capture / session-env / attach probe

| CyclerConfig field | Line | Production default | Mechanism |
|---|---|---|---|
| `InjectFn(ctx, target, text)` | `cycle.go:96` | `InjectText` (bound `:331-332`) | `injector.go:131-168`: tmux `load-buffer`→`paste-buffer`→settle 750ms→`send-keys Enter`→2 retries |
| `SendEscapeFn(ctx, target)` | `cycle.go:175` | nil (prod wires `SendEscapeKey`, `injector.go:184-193`) | `tmux send-keys Escape`, pre-handoff preempt (`cycle.go:986-988`) |
| `SetTmuxEnvFn(ctx, target, k, v)` | `cycle.go:116` | `SetTmuxEnv` (bound `:355-357`) | `tmux setenv -t` (`injector.go:224-233`); HARMONIK_AGENT at `cycle.go:1106` |
| `OperatorAttachedFn(target)` | `cycle.go:186` | `OperatorAttached` (bound `:358-360`) | `tmux list-clients` (`tmuxresolve.go:213`); Gate 7 (`cycle.go:860`) |
| `ForceRestartFn(ctx, agent)` | `cycle.go:169` | nil (prod wires respawn path) | kill+respawn escalation after `MaxHandoffTimeouts` (`cycle.go:1063-1074`) |

Plus the pane **read** side used only by the ack handshake today: `PaneCapturer`
(`awaitack.go:63`, default `CaptureTmuxPane` `awaitack.go:211-225`, `tmux capture-pane -p -S -200`).

Proposed minimal interface:

```go
// PanePort — the tmux boundary. Inject MUST follow PL-021d
// (load-buffer + paste-buffer write discipline); Capture is keeper-owned
// (PL-021b §5 forbids the daemon this read).
type PanePort interface {
    Inject(ctx context.Context, target, text string) error
    SendEscape(ctx context.Context, target string) error
    SetEnv(ctx context.Context, target, key, value string) error
    Capture(ctx context.Context, target string) (string, error)
    OperatorAttached(target string) bool
}
```

`ForceRestartFn` is a *process-lifecycle* effect (kills/respawns the agent), not a pane write —
recommend it stays a config callback (or a one-method `RespawnPort`) rather than bloating
PanePort; Change-Design call.

### 1b. GaugePort — token / session_id / keeper file-state reads

| CyclerConfig field | Line | Production default | Mechanism |
|---|---|---|---|
| `ReadGaugeFn(dir, agent)` | `cycle.go:97` | `ReadCtxFile` (bound `:334-335`) | reads `.harmonik/keeper/<agent>.ctx` (`gauge.go:34-63`), overlays `.sid` when primary UUIDv4 (`gauge.go:59-61`) |
| `IsManagedFn(dir, agent)` | `cycle.go:86` | `IsManaged` (bound `:316-318`) | `.managed` opt-in stat; Gate 1 (`cycle.go:658`) |
| `SetManagedSessionFn(dir, agent, sid)` | `cycle.go:110` | `WriteManagedSessionID` (bound `:352-354`) | rebind after cycle (`cycle.go:1138`) / abort clear (`cycle.go:1053`) |
| `CrispIdleFn(dir, agent)` | `cycle.go:98` | `CrispIdle` (bound `:337-339`) | `.idle` vs `.ctx` mtime compare, 10s tolerance (`gates.go:67-93`) |
| `HoldingDispatchFn(dir, agent)` | `cycle.go:99` | `HoldingDispatch` (bound `:340-342`) | `.dispatching` stat, fail-closed (`gates.go:102-111`) |
| `SleepingCheckFn(dir, sid)` | `cycle.go:204` | `IsSleeping` (bound `:361-363`) | `.sleeping.<sid>` stat, fail-closed on empty sid (`gates.go:282-289`) |
| `HeldCheckFn(dir, agent)` | `cycle.go:214` | closure over `IsHeld(.,.,HoldTTL)` (bound `:367-370`) | `.hold.<sid>` keyed by live `.sid` + TTL (`gates.go:232-260`) |
| `ClearPrecompactTriggerFn(dir, agent)` | `cycle.go:102` | `ClearPrecompactTrigger` (bound `:349-351`) | remove `.precompact` marker (`gates.go:44-52`) |
| `RecentTranscriptTurnFn(dir, sid, role)` | `cycle.go:242` | `recentTranscriptTurn` (via `recentTurnFn` `cycle.go:891-896`) | transcript JSONL tail scan (`tmuxresolve.go:266`); Gates 5d/5e (`cycle.go:792-817`) |

Proposed shape (dir/agent bound at construction, matching how `Cycler` already closes over
`cfg.ProjectDir`/`cfg.AgentName`):

```go
// GaugePort — reads the keeper's file-state universe (.ctx/.sid/.managed/
// markers/transcript) and owns the one write-back that keeps the watcher
// bound (managed session id).
type GaugePort interface {
    ReadGauge() (*CtxFile, time.Time, error)
    IsManaged() bool
    SetManagedSession(sessionID string) error
    CrispIdle() bool
    HoldingDispatch() bool
    Sleeping(sessionID string) bool
    Held() bool
    ClearPrecompactTrigger() error
    RecentTurn(sessionID, role string) (time.Time, bool)
}
```

This is deliberately wide (9 methods): it is the *gate-input* surface. Alternative for
Change-Design: keep `ReadGauge`/`SetManagedSession` as GaugePort proper and fold the seven
predicate reads into a per-tick `GateSnapshot` the shell samples (see §3) — the reactor then
never touches the port at all. Either way no function-field is lost.

### 1c. HandoffPort — nonce poll + mtime freshness + journal

| CyclerConfig field | Line | Production default | Mechanism |
|---|---|---|---|
| `HandoffFilePath(dir, agent)` | `cycle.go:87` | `defaultHandoffFilePath` (`cycle.go:443-445`) | `<dir>/HANDOFF-<agent>.md` |
| `ReadHandoff(path)` | `cycle.go:88` | `defaultReadHandoff` (`cycle.go:447-454`) | `os.ReadFile`; nonce poll `cycle.go:1523-1525`, stale-nonce check `cycle.go:517-529` |
| `HandoffModTimeFn(path)` | `cycle.go:94` | `defaultHandoffModTime` (`cycle.go:458-464`) | `os.Stat` mtime; freshness recovery `cycle.go:918-928` |
| `TruncateHandoffFn(path)` | `cycle.go:95` | `defaultTruncateHandoff` (`cycle.go:466-469`) | write-empty; stale-nonce wipe `cycle.go:971-973` |
| `WriteJournalFn(path, j)` | `cycle.go:100` | `writeJournalFile` (`cycle.go:471-485`) | `.harmonik/keeper/<agent>.cycle`, overwritten per phase |
| `ReadJournalFn(path)` | `cycle.go:101` | `defaultReadJournal` (`cycle.go:487-498`) | crash recovery only (`cycle.go:1204`) |

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

Journal placement note: the journal is cycle-state persistence, not handoff IO; once the 4
durable events land (§4) the journal's *observability* role is superseded but its
**crash-recovery** role (`RecoverFromCrash`, `cycle.go:1197-1244`) is not — keep it, either
here or as a 2-method `JournalPort`. Change-Design call; behavior parity requires the phase
vocabulary stay byte-identical (`"opened"/"handoff_injected"/"confirmed"/"cleared"/"resumed"/
"complete"/"aborted"`, `cycle.go:25-26`).

### 1d. EmitterPort — durable bus

Already an interface: `keeper.Emitter` (`watcher.go:23-25`):

```go
type Emitter interface {
    EmitWithRunID(ctx context.Context, runID core.RunID, eventType core.EventType, payload []byte) error
}
```

**Confirmed structurally compatible with `handlercontract.EventEmitter`**
(`internal/handlercontract/watcher_hc011.go:41`): that interface declares `Emit(ctx, eventType,
payload)` **and** `EmitWithRunID(ctx, runID, eventType, payload)`; `keeper.Emitter` is exactly
the `EmitWithRunID` subset, so every `handlercontract.EventEmitter` (and `eventbus.EventBus`)
already satisfies it. EmitterPort = keep `keeper.Emitter` verbatim (SK-R1 "no divergent bus
port" is satisfied by subsetting, zero adaptation code). Production impl in the standalone
keeper process: `FileEmitter` (`watcher.go:39-100`) appending EV-001 envelopes to
`.harmonik/events/events.jsonl`; test impl `RecordingEmitter` (`watcher.go:102-134`).
Note `FileEmitter` itself stamps `TimestampWall: time.Now().UTC()` (`watcher.go:68`) — that
stamp should route through ClockPort too for deterministic replay envelopes.

### 1e. ClockPort — time

Absorbs **no existing CyclerConfig field** (that is the gap): the auto-cycle has zero clock
seam while `RestartNowConfig.Now` (`restartnow.go:54`) and `AwaitAckConfig.Now`
(`awaitack.go:78`) already exist. `CycleIDGen` (`cycle.go:85`, default `newCycleIDGen`
`cycle.go:434-441`) hides a `time.Now()` at `:435` — its prefix should be derived via ClockPort
(or the generator injected as today; parity-safe either way). Full treatment in §2.

---

## 2. ClockPort — concrete

### 2a. Interface

What the real sites need: `Now` (stamps, deadlines), `Since` (interval gates), `NewTicker`
(poll loops), and a context-aware `Sleep` (injector settle/retry — `sleepCtx`,
`injector.go:197-206`, is `select ctx.Done() vs timer.C`, so plain `Sleep(d)` is insufficient).
`awaitack.go:162` also computes `time.Until(deadline)` — derivable from `Now`, no extra method.

```go
// ClockPort — the deterministic-time seam (SB-R9). Consumer-owned; the real
// impl delegates to package time.
type ClockPort interface {
    Now() time.Time
    Since(t time.Time) time.Duration
    NewTicker(d time.Duration) Ticker
    // Sleep waits d or until ctx cancels; reports whether the full d elapsed.
    // (This is injector.go's sleepCtx shape — a bare Sleep(d) cannot honor
    // context cancellation, which InjectText relies on at injector.go:148,161.)
    Sleep(ctx context.Context, d time.Duration) bool
}

type Ticker interface {
    C() <-chan time.Time
    Stop()
}
```

`Since` is sugar over `Now` but is kept: 13 call sites in `cycle.go` read as interval gates and
a fake clock wants one advance-point, not arithmetic at every site.

### 2b. Enumeration of the cycle.go clock sites (grep-verified 2026-07-13)

Exact count in `cycle.go` non-test: **19 `time.Now` + 13 `time.Since` + 2 `time.NewTicker` = 34
sites** (dossier 02 §3's "32" headline undercounts by 2; its own line list matches this one).

`time.Now` → `Clock.Now()`:

| :line | Site | Purpose |
|---|---|---|
| 435 | `newCycleIDGen` | cycle-id timestamp prefix |
| 707 | `MaybeRun` boot-grace | arm grace on novel SID |
| 937 | `runCycle` | journal `OpenedAt`/`UpdatedAt` |
| 945 | `runCycle` | `lastForcedAttemptAt` stamp |
| 984 | `runCycle` | `handoffInjectedAt` anchor (freshness recovery) |
| 999, 1020, 1031, 1082, 1114, 1150, 1155 | `runCycle`/`completeCycleTail` | journal `UpdatedAt` per phase |
| 1219, 1227, 1235 | `RecoverFromCrash` | journal `UpdatedAt` |
| 1468 | `RunForIdle` | `lastIdleRestartAt` stamp |
| 1494 | `maybeEmitOperatorAttached` | sample-throttle window |
| 1541 | `waitForNewSessionIDWithBackstop` | backstop deadline (`Now().Add`) |
| 1546 | same | deadline check (`Now().Before`) |

`time.Since` → `Clock.Since(t)`:

| :line | Site | Purpose |
|---|---|---|
| 714, 730, 735, 739 | `MaybeRun` | boot-grace / MaxBootGraceTotal math |
| 794, 799 | Gate 5d | operator-turn lookback |
| 811, 813 | Gate 5e | post-answer grace |
| 832, 844 | Gate 6 | force-retry interval |
| 1304, 1307 | `RunForPrecompact` | boot-grace mirror |
| 1450 | `RunForIdle` | idle-restart cooldown |

`time.NewTicker` → `Clock.NewTicker(cfg.PollInterval)`:

| :line | Site |
|---|---|
| 1515 | `pollForNonce` (nonce poll, 200ms cadence, 300s ctx-timeout at `:1512`) |
| 1562 | `waitForNewSessionID` (SID poll, 200ms cadence, 10s ctx-timeout at `:1559`) |

Note the two poll loops ALSO use `context.WithTimeout` (`cycle.go:1512`, `:1559`) — real-time
deadlines living in the ctx, not just the ticker. Deterministic replay needs those expressed
via Clock too (deadline check against `Clock.Now()` rather than ctx-timeout), or a fake-clock
context. This is a Change-Design item: the reactor version (§3) dissolves both loops into
armed-timer events, which sidesteps it.

### 2c. Other keeper clock sites in scope

- `injector.go:198` — `time.NewTimer` inside `sleepCtx`; called at `:148` (`submitSettle`
  750ms) and `:161` (`submitRetryDelay` 400ms ×2). Route: `Clock.Sleep(ctx, d)`. These are the
  *only* real-timer sleeps in the package (no `time.Sleep` anywhere, confirmed).
- `gates.go:126, :185` — `time.Now().UTC()` marker content stamps; `:256` `time.Since(parsed)`
  hold-TTL; `:88` pure mtime compare (no clock). These live behind GaugePort in the rebuild, so
  the port impl takes the Clock.
- `watcher.go` — the main `time.NewTicker` at `:1003` and `time.Now`/`Since` at
  `:68, :1025, :1036, :1048, :1064, :1067, :1073, :1134, :1136-1137, :1176, :1181, :1190,
  :1195, :1221, :1282, :1315, :1399, :1402, :1433, :1547, :1550, :1581, :1637, :1640, :1679,
  :1687`. The watcher loop is the event-*source* in the rebuild; its ticker becomes the
  Clock-driven tick generator.
- `heartbeat.go:287`, `tmuxresolve.go:225` — port-impl-internal.

### 2d. The existing seam to generalize

`restartnow.go:67-79` and `awaitack.go:94-114` are the template: a `Now func() time.Time`
config field, nil-defaulted to `time.Now` (`restartnow.go:69-71`, `awaitack.go:96-98`), driven
by fake clocks in `restartnow_test.go` / `awaitack_test.go`. ClockPort is that seam promoted to
a named interface + extended with the ticker/sleep surface those two paths don't need but the
auto-cycle and injector do. Migration for those two files: `Now` field → `Clock ClockPort`
(keep `Now` as a deprecated alias or adapt in the CLI constructor; both files also want
`Clock.Sleep` — `awaitack.go:165` already calls `sleepCtx`).

---

## 3. The Step(event) → []action reactor

### 3a. What the codex template gives us

`internal/codexreactor/reactor.go`: pure `Reactor.Step(Event) []Action` (`reactor.go:185-277`)
over flat JSON-round-trippable Event/Action structs (`:62-73`, `:102-112`), state inspectable
between steps (`:143-157`), `Run(ctx, src, eff)` driver (`:282-292`), `Effector`/`EventSource`
one-method interfaces (`:120-137`), fakes in `fake.go`. Two invariants (I1 one-turn-in-flight,
I2 dedup-by-seq) enforced *in* Step. The keeper reactor mirrors this exactly, with one
structural addition codex doesn't need: **timers**. Codex events all originate in the server
stream; keeper timeouts (nonce 300s, settle 10s, backstop 150s) originate in *time itself*, so
the reactor must emit `ArmTimer` actions and consume `TimerFired` events — the shell owns the
ClockPort and converts one to the other. That keeps Step pure AND makes every timeout race
replayable as an explicit event interleaving.

### 3b. Event vocabulary (shell → reactor)

| Event | Produced by (shell) | Today's code analog |
|---|---|---|
| `GaugeTick{cf CtxFile, at, gates GateSnapshot}` | watcher poll tick (`watcher.go:1003→1240`) | `MaybeRun(ctx, cf)` entry `cycle.go:656` |
| `PrecompactTrigger{cf}` | `.precompact` marker seen (`watcher.go:1251`) | `RunForPrecompact` `cycle.go:1276` |
| `NonceObserved{cycleID, at}` | handoff poll finds nonce (`cycle.go:1523-1525`) | `pollForNonce` → true `cycle.go:1003` |
| `HandoffFreshSeen{cycleID, mtime}` | mtime ≥ injectedAt after nonce timeout | `handoffWrittenAndFresh` `cycle.go:918-928, :1016` |
| `ModelDone{cycleID, sessionID, at}` | **NEW** — see §4 (stop-hook `.idle` transition) | none (dossier 02 §1 step 4) |
| `SessionChanged{cycleID, newSID}` | gauge SID ≠ prevSID (`cycle.go:1570-1573`) | `waitForNewSessionID` success |
| `TimerFired{cycleID, kind}` kinds: `handoff_timeout`, `model_done_timeout`, `clear_settle`, `clear_backstop` | Clock-armed timers | ctx-timeouts at `cycle.go:1512, :1559`, deadline `:1541` |
| `CrashJournal{j CycleJournal}` | boot, journal present | `RecoverFromCrash` `cycle.go:1197` |

`GateSnapshot` carries the sampled boolean/timestamp inputs of the ladder: `managed`,
`crispIdle`, `holdingDispatch`, `sleeping`, `held`, `operatorAttached`, `lastUserTurnAt`,
`lastAssistantTurnAt`. Sampling in the shell (one GaugePort read-burst per tick) keeps Step
pure; the watcher already computes two of these per tick (`watcher.go:1227-1228`).

### 3c. Action vocabulary (reactor → effector)

| Action | Executed via port | Today's code site |
|---|---|---|
| `WriteJournal{phase, reason}` | HandoffPort | `cycle.go:955, :998-1000, :1022, :1033, :1081-1083, :1113-1115, :1149-1151, :1154-1157, :1218-1237` |
| `TruncateHandoff` | HandoffPort | `cycle.go:971-973` |
| `SendEscape` | PanePort | `cycle.go:986-988` |
| `InjectHandoffCmd{cycleID}` (text built from `nonceMarker`, `cycle.go:989-992`) | PanePort | `cycle.go:993` |
| `InjectClear` | PanePort | `cycle.go:1111`, re-inject `:1551` |
| `InjectBrief` (`briefRestartCmd`, `cycle.go:20`) | PanePort | `cycle.go:1147`, crash path `:1216` |
| `SetTmuxEnv{HARMONIK_AGENT}` | PanePort | `cycle.go:1106` |
| `SetManagedSession{sid}` (incl. `""` clear) | GaugePort | `cycle.go:1053, :1138` |
| `ClearPrecompactMarker` | GaugePort | 8 sites in `RunForPrecompact` (`cycle.go:1285-1378`) |
| `SetHold` (Gate 5d side effect) | GaugePort | `cycle.go:797` |
| `Emit{type, payload}` | EmitterPort | the 8 emit* sites (§4 + `cycle.go:1421, :1484, :1586, :1598, :1610, :1621, :1632`) |
| `ArmTimer{kind, d}` / `CancelTimer{kind}` | shell/Clock | replaces the two poll loops + backstop deadline |
| `ForceRestart` | lifecycle callback | `cycle.go:1069` |

### 3d. States and the 11-gate ladder

The ladder (`MaybeRun`, `cycle.go:656-865`) is **not** states — it is a pure predicate + a small
side-effect prelude evaluated per `GaugeTick` while the machine is in `Idle`. Gates, in exact
order (all must be preserved, order included):

1. `.managed` (`:658`) · 2. empty SID (`:662`) · *(prelude: re-arm observation `:669-671`,
same-SID escape hatch `:686-691`, boot-grace SID tracking `:701-724`)* · boot-grace gate
(`:728-742`, force-exempt) · 3. below act threshold (`:747`) · 4. CrispIdle unless force
(`:754-760`) · 5. HoldingDispatch (`:762`) · 5b. Sleeping (`:769`) · 5c. Held (`:777`) ·
5d. recent operator turn → auto-hold (`:792-803`) · 5e. post-answer grace (`:809-817`) ·
6. anti-loop suppression + force-retry exceptions (`:825-852`) · 7. operator-attached
(`:860-863`).

Reactor state machine (per agent; one instance):

```
Idle
 └─ GaugeTick + ladder-pass ──────────────► AwaitingHandoff   [WriteJournal(opened),
                                             Emit(handoff_started), TruncateHandoff?,
                                             SendEscape, InjectHandoffCmd,
                                             WriteJournal(handoff_injected),
                                             ArmTimer(handoff_timeout=300s)]
AwaitingHandoff
 ├─ NonceObserved ────────────────────────► AwaitModelDone    [WriteJournal(confirmed),
 │                                           Emit(handoff_written),
 │                                           ArmTimer(model_done_timeout)]
 ├─ TimerFired(handoff_timeout) + fresh ──► AwaitModelDone    [journal(confirmed,
 │      (HandoffFreshSeen)                   reason=handoff_timeout_recovered),
 │                                           Emit(handoff_written{recovered})]
 └─ TimerFired(handoff_timeout) no fresh ─► TERMINAL Aborted  [journal(aborted),
                                             Emit(cycle_aborted), anti-loop state,
                                             SetManagedSession("")?, ForceRestart?]
AwaitModelDone                               ← NEW state; SR4 (see §4)
 ├─ ModelDone ────────────────────────────► Clearing          [SetTmuxEnv, InjectClear,
 │                                           WriteJournal(cleared), Emit(model_done),
 │                                           Emit(clear_sent),
 │                                           ArmTimer(clear_settle=10s) + backstop]
 └─ TimerFired(model_done_timeout) ───────► Clearing          [same, degraded flag]
Clearing
 ├─ SessionChanged ───────────────────────► Briefing          [Emit(new_session_up),
 │                                           SetManagedSession(newSID)]
 ├─ TimerFired(clear_settle), retries left► Clearing          [InjectClear (defensive,
 │                                           cycle.go:1550-1552), re-ArmTimer]
 └─ TimerFired(clear_backstop) ───────────► Briefing          [Emit(clear_unconfirmed),
                                             SetManagedSession("")]
Briefing ─(immediate)─────────────────────► TERMINAL Complete [InjectBrief,
                                             WriteJournal(resumed), journal(complete),
                                             Emit(cycle_complete),
                                             Emit(cycle_recovered)? , anti-loop state]
```

Terminal mapping: **cycle_complete** = `Briefing→Complete` (`cycle.go:1153-1163`);
**cycle_aborted** = `AwaitingHandoff` timeout-without-fresh-handoff (`cycle.go:1029-1078`) —
the only path that never sends `/clear`; **clear_unconfirmed** = the `Clearing` backstop
exhaustion (`cycle.go:1128-1131`) — NOT a terminal by itself: the brief still fires and the
cycle still records `cycle_complete` (dossier 02 §5). SR7 (no overlapping restarts) is the
structural rule "ladder only evaluated in `Idle`" — which is exactly what today's *blocking*
`runCycle` provides implicitly (see §5 risk 1).

Anti-loop fields become reactor `State`: `lastFiredSID`, `seenLowPctAfterLastFire`,
`lastFireWasAbort`, `lastForcedAttemptAt`, `consecutiveHandoffTimeouts`, boot-grace tracking
(`currentSessionID`, `currentSessionIDSince`, `seenSessionIDs`, `bootGraceFirstArmAt`),
`lastIdleRestartAt`, `lastIdleCrewNotifiedSID`, `lastOperatorAttachedEmit` — all at
`cycle.go:559-626`. Timestamps in state come from event `at` fields (Clock-stamped by the
shell), never from a clock call inside Step.

`RunForPrecompact` (`cycle.go:1276-1385`) and `RunForIdle` (`cycle.go:1405-1474`) are two more
*entry ladders* into the same `AwaitingHandoff` chain — modeled as distinct events
(`PrecompactTrigger`, plus the idle branch of `GaugeTick`) with their own gate subsets, not
separate machines. `RecoverFromCrash` (`cycle.go:1197-1244`) is a boot-time event that either
fast-forwards to `Briefing` (phase `cleared`), closes out (`resumed`), or marks `aborted`.

### 3e. Pure core vs imperative shell

**Pure (Step):** the ladders, threshold math (delegating to `thresholds.go:214`
`minAbsOrPctCeil` — pure already), anti-loop/hysteresis state, phase machine, nonce string
construction (`nonceMarker` `cycle.go:502-504`, `handoffHasStaleNonce`/`isOnlyNonce`
`cycle.go:517-553` given file content in the event), journal struct contents, event payload
construction.

**Shell (effector/source):** tmux exec (`injector.go`, `tmuxresolve.go`), all file IO (gauge,
`.sid`, handoff, journal, markers, transcript tail), `events.jsonl` append (`FileEmitter`),
timer arming via ClockPort, the watcher poll loop as tick source, `ForceRestartFn` respawn.

**Existing harness this must preserve:** `cycle_reactive_harness_test.go` already drives the
cycle causally — a `reactiveSession` fake whose `InjectFn` pattern-matches injected text and
mutates gauge+handoff state (`cycle_reactive_harness_test.go:46-181`), asserting the SID flip
is *caused by* `/clear` (`:84-94, :251-263`). The scenario suite
(`cycle_scenario_reactive_test.go:46,170,260`; `_wave2_test.go:60,151,305,382,512`) covers:
full cycle, nonce-timeout abort, fresh-handoff recovery, clear-settle-unconfirmed, forced
clear, anti-loop re-arm, precompact backstop, slow-clear hard gate. These become the golden
Step-sequence corpus: same fakes, but wired as SyntheticSource events + FakeEffector action
assertions — the harness's injected-command-order assertions map 1:1 onto action-log
assertions.

---

## 4. The 4 durable interior events + ordering

All four carry `agent_name`, `cycle_id` (the `runCycle` id from `CycleIDGen`, `cycle.go:936`),
`session_id`; registered per EV-U1/U1a as `session_keeper_*` (catalog precedent:
`internal/core/eventreg_hqwn59.go:478-518` — noting the §8.13 collision EV-U5 must fix first).
Every current keeper emit passes `core.RunID{}` (zero) — `cycle.go:1421, :1484, :1586, :1598,
:1610, :1621, :1632`; the payload `cycle_id` is the real join key (EV-U2), confirmed present in
the recorded envelopes (baseline sample: `payload.cycle_id: "cyc-20260608T101057-000001"`).

| Event | Emitted at (transition) | Today's silent analog |
|---|---|---|
| `session_keeper_handoff_written` | `AwaitingHandoff → AwaitModelDone`: nonce observed (`pollForNonce` true, `cycle.go:1003→1081`) OR freshness recovery accepted (`cycle.go:1016-1026`; payload `recovered: true`, carry handoff mtime) | journal `Phase="confirmed"` only (`:1081-1083`, `:1019-1022`) |
| `session_keeper_model_done` | `AwaitModelDone → Clearing`: the model-done signal lands (below), or `model_done_timeout` fires (payload `degraded: true`) | **nothing — no step exists** (dossier 02 §1 step 4) |
| `session_keeper_clear_sent` | first `InjectClear` of the cycle (`completeCycleTail`, `cycle.go:1110-1112`); defensive re-injects (`cycle.go:1550-1552`) either re-emit with `attempt: n` or are folded into the eventual `new_session_up`/`clear_unconfirmed` — recommend re-emit with attempt, it makes the 347-unconfirmed forensics replayable | journal `Phase="cleared"` only (`:1113-1115`) |
| `session_keeper_new_session_up` | `waitForNewSessionIDWithBackstop` returns non-empty (`cycle.go:1128`), immediately before the `.managed` rebind (`:1138`); payload `prev_session_id`, `new_session_id` | silent success (only the *failure* emits `clear_unconfirmed`, `:1129-1131`) |

### The model-done signal — concrete proposal (SR4 has NO code today)

Verified: after `pollForNonce` succeeds, `runCycle` marks `confirmed` (`cycle.go:1081`) and
`completeCycleTail` injects `/clear` **immediately** (`cycle.go:1109-1111`) — there is no
wait-for-model-done anywhere; the only approximations are the *pre-cycle* `CrispIdle` gate and
the nonce itself (dossier 02 §1). The nonce proves the handoff *file content* landed, not that
the model's **turn** ended — the model may still be streaming text/tool calls after writing the
file, and `/clear` then races the tail of the turn.

**Proposed durable source: the Stop-hook `.idle` marker transition.** `keeper-stop-hook.sh`
touches `.harmonik/keeper/<agent>.idle` and "fires only at await-input boundaries (verified by
Anthropic)" (`scripts/keeper-stop-hook.sh:5-7`). Concretely:

- Shell rule: after `handoff_written` at time `t_nonce`, watch `.idle` mtime; the first
  `mtime(.idle) ≥ t_nonce` is `ModelDone` — the model reached an await-input boundary AFTER
  the turn that wrote the handoff. Strict ordering, no `crispIdleTolerance` fudge (that 10s
  tolerance, `gates.go:67`, exists to discount passive `.ctx` repaints — irrelevant here
  because we compare `.idle` against the nonce-observation instant, not against `.ctx`).
- Corroborating/backstop source (already injectable): `recentTranscriptTurn(dir, sid,
  "assistant")` (`tmuxresolve.go:266`, seam `CyclerConfig.RecentTranscriptTurnFn`
  `cycle.go:242`) with turn-ts ≥ `t_nonce` — heavier (JSONL tail scan) but useful when the
  Stop hook is not wired for an agent.
- Liveness bound (SR9): `model_done_timeout` (new config, suggest default well under
  `ClearConfirmBackstop` — e.g. 60s) after which the reactor proceeds to `Clearing` anyway,
  emitting `model_done{degraded:true}`. Without the bound, a lost Stop-hook write would wedge
  the cycle — the resume-hang class SR9 exists to kill. The timeout path preserves today's
  behavior (clear-immediately) as the degraded mode, so the tightening is fail-open.

This is the deliberate SR4 tightening Constraint 4 / SK-R9 carve out: replay of the OLD corpus
cannot contain `model_done`, so the replay harness synthesizes `ModelDone` immediately after
`handoff_written` when replaying pre-rebuild recordings (keeping old-corpus action goldens
identical), while NEW recordings assert the real ordering.

### Ordering invariants over the four events (per cycle_id c, single agent)

- **SR3** — `handoff_written(c)` **before** `clear_sent(c)`. Structural: `Clearing` reachable
  only via `AwaitModelDone` which is reachable only via the two `handoff_written` edges.
  (Today's analog: `/clear` only after nonce-confirm or freshness recovery,
  `cycle.go:1003-1085`; the abort path `:1029-1078` never clears.)
- **SR4** — `model_done(c)` **before** `clear_sent(c)`. NEW (the `AwaitModelDone` state).
- **SR6** — `new_session_up(c)` before the brief inject (and thus before `cycle_complete(c)`);
  when absent, `clear_unconfirmed(c)` before `cycle_complete(c)` — exactly one of the two per
  cycle. (Today: `waitForNewSessionIDWithBackstop` `:1128` strictly precedes `InjectBrief`
  `:1147`.)
- **SR7** — no interleaving: between `handoff_started(c1)` and its terminal
  (`cycle_complete(c1)` | `cycle_aborted(c1)`), no `handoff_started(c2)` for the same agent.
  (Today enforced by the blocking `runCycle` + Gate 6; reactor: ladder only in `Idle`.)
- **SR9** — bounded liveness: every `handoff_started(c)` is followed by exactly one terminal
  within `HandoffTimeout(300s) + model_done_timeout + ClearConfirmBackstop(150s) + injection
  overhead` (≈ ~520s with proposed defaults; today ≈ ~460s) — or a `restart_failed`-class
  emission; never silence. Every `TimerFired` edge lands in a state with an outgoing action, so
  the machine cannot wedge without emitting.

Ordering substrate: events.jsonl is append-only, UUIDv7-`event_id`-ordered (Constraint 5), and
the standalone keeper is a single writer per agent through one `FileEmitter` mutex
(`watcher.go:84-99`) — so per-cycle intra-agent ordering assertions over `event_id` are sound.

---

## 5. Behavior-parity risks

The band/threshold values are pinned (`thresholds.go:23-52`, defaults-PIN in
`thresholds_test.go`; operator HARD-NO on band changes, `thresholds.go:10-14`) and all math
must keep routing through `minAbsOrPctCeil` (`thresholds.go:214`). Beyond that, the rebuild has
these concrete parity hazards:

1. **Blocking → event-driven is a real semantic change (BIGGEST RISK).** Today
   `Cycler.MaybeRun` runs **synchronously inside the watcher tick** (`watcher.go:1240`): while
   `pollForNonce`/`waitForNewSessionIDWithBackstop` block (up to ~7.5 min worst case), the
   watcher processes **no ticks at all** — no warn emissions, no precompact detection, no
   heartbeat, no reaper, no hard-ceiling checks. The reactor unfreezes the loop; gauge ticks
   arriving mid-cycle would hit the warn state machine (`watcher.go:1275-1331`) and gate
   ladder in ways that literally never happened before. Mitigation: an explicit `InCycle`
   suppression in the shell (drop/park non-cycle processing while the machine is off-`Idle`) to
   reproduce the freeze, OR consciously spec the unfreeze as a change — but then warn/event
   counts diverge from the corpus. Recommend reproduce-the-freeze first, relax later.
2. **Gate-prelude side effects run even when gates fail.** The re-arm observation
   (`cycle.go:669-671`), same-SID escape hatch (`:686-691`), and boot-grace SID tracking
   (`:701-724`) mutate state on EVERY tick before gating; Gate 5d *writes a hold marker*
   (`SetHold`, `:797`) on its deferral path; `RunForPrecompact` clears the marker on every
   gate (8 sites) and emits `precompact_blocked` with a per-gate action string
   (`:1284-1378` — incl. the quirk that empty-SID emits reason `"hold_dispatch_skip"`,
   `:1292`). A "clean" predicate refactor that short-circuits before these side effects
   changes observable state. The Step design must keep prelude-mutations unconditional.
3. **The model-done wait shifts `/clear` timing.** Any added delay between nonce-confirm and
   `/clear` can push slow panes past thresholds that today just barely pass (or, favorably,
   reduce the 347 `clear_unconfirmed` by clearing at a calmer instant). Deliberate (SR4), but
   must be bounded, defaulted small, and its effect isolated in measurement so baseline
   comparison distinguishes "SR4 wait" from regression.
4. **Poll quantization.** `time.NewTicker` delivers its FIRST tick only after one full
   interval — `pollForNonce` (`cycle.go:1515-1528`) therefore never sees a nonce in the first
   200ms, and `waitForNewSessionID` (`:1562-1575`) likewise. An event-driven shell that checks
   immediately confirms one poll earlier; conversely a coarser tick misses the
   `ClearSettle=10s` window edge (10s / 200ms = 50 polls exactly). Timer semantics in the
   shell must reproduce first-tick-after-interval.
5. **Journal phase vocabulary + crash-recovery matrix.** `RecoverFromCrash`
   (`cycle.go:1212-1241`) switches on exact phase strings and reasons
   (`"crash_before_clear"`, `"recovered_from_crash"`); keep byte-identical or boot behavior
   changes across binary upgrade.
6. **Anti-loop hysteresis fine structure.** `lastFireWasAbort` gating of the escape hatch
   (`:686-691` vs `:1324-1329`), abort-path `.managed` clear gated on
   `!currentSessionIDSince.IsZero()` (`:1052-1057`), idle-cooldown stamp-then-unwind
   (`:1468-1472`), force-retry stamping at cycle START (`:944-946`). Each is a bug-fix scar
   (hk-vpnp, hk-ibb, hk-4i0s, hk-qoz) with a regression test; the reactor state update rules
   must copy them exactly.
7. **Emission throttles.** `maybeEmitOperatorAttached` one-per-minute sampling
   (`cycle.go:1493-1501`) — and note `emitOperatorAttached` is a deliberate NO-OP
   (`:1507`, logmine TA3/F55): do not "helpfully" resurrect it. `idle_crew` once-per-SID
   (`:1414-1424`).
8. **Injector timing constants** are regression-guarded (`TestInjectText_SettleConstants`,
   per `injector.go:92`); ClockPort routing must not alter the 750ms/400ms×2 sequence order.

**How the recorded corpus guards this:** the frozen baseline
(`.harmonik/events/baseline-2026-07-13/events.jsonl`) + replay. For each of the 507 recorded
cycles, feed the reconstructed event sequence to Step and assert (a) the action log matches the
golden derived from today's implementation (L1 golden-action replay per SB-R6), (b) terminal
classification matches the recorded terminal event, (c) aggregate counters match the frozen
baseline exactly under parity mode (427 complete / 79 aborted / 347 clear_unconfirmed). Risks
1, 2, 6, 7 all surface as action-log diffs; risks 3–4 surface as timer-event reorderings, which
is why timeouts must be explicit events, not wall-clock.

---

## 6. The recorded cycles — what exists, granularity, what the 4 events add

**How recorded today:** the standalone `harmonik keeper` process's `FileEmitter`
(`watcher.go:39-100`) appends EV-001 envelopes to `.harmonik/events/events.jsonl`; a frozen
copy lives at `.harmonik/events/baseline-2026-07-13/events.jsonl`. Grep-counted (2026-07-13):

| Event | baseline | live log |
|---|---|---|
| `session_keeper_handoff_started` | **507** | 508 |
| `session_keeper_cycle_complete` | **427** (84.2%) | 428 |
| `session_keeper_cycle_aborted` | **79** | 79 |
| `session_keeper_clear_unconfirmed` | **347** | 347 |
| `session_keeper_cycle_recovered` | 0 | 0 |

(The plan's "~476" is an earlier approximate of this population; the normative anchor is the
frozen baseline's 507/427/347 — matching SK-R10's 84% = 427/507.) Envelopes carry
`event_id` (UUIDv7), `timestamp_wall`, `source_subsystem: "internal/keeper"`, **no run_id**,
and payloads with real `cycle_id`/`session_id` — verified by sample.

**Replayability today: boundary-only.** A cycle appears as `handoff_started` → terminal
(`cycle_complete` | `cycle_aborted`), optionally with `clear_unconfirmed` between. The interior
(nonce confirmed, model done, `/clear` sent, new SID up) exists only as `.cycle` journal phase
overwrites (`cycle.go:471-485`) — destroyed each cycle, absent from the corpus (dossier 02 §2
table). Consequence: replay can classify outcomes but cannot localize WHERE the 79 aborts
stalled or reconstruct the interleaving that produced 347 unconfirmed-but-complete cycles, and
SR3/SR4/SR6 are **unstatable** over the recorded stream.

**What the 4 events add:** interior-step granularity — each cycle becomes a 6–7 event chain
(`handoff_started → handoff_written → model_done → clear_sent[×n attempts] →
new_session_up | clear_unconfirmed → cycle_complete`), UUIDv7-ordered, joined by payload
`cycle_id`. That makes (a) SR3/SR4/SR6/SR7/SR9 directly assertable as properties over the
event log (L1, zero-token); (b) the replay `Twin` able to re-drive Step at every interior
transition and fault-inject between them (drop the `SessionChanged`, stall the nonce, dup the
`/clear`) per the four SB fault modes; (c) the 347-unconfirmed forensics quantifiable (which
attempt count, how long after `clear_sent` the SID finally moved). The existing scenario
harness (`cycle_reactive_harness_test.go`) remains the causal L0 fake; the corpus becomes the
L1/L2 ground truth.

**Conformance anchors to keep green:** `TestKeeperConformance`
(`conformance_keeper_test.go:32-38`), `conformance_keeperx_test.go`,
`conformance_keeper_integration_test.go:37`, plus the ~55-file keeper suite (Constraint 2).
