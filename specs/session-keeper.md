# Session Keeper

```yaml
---
title: Session Keeper
spec-id: session-keeper
requirement-prefix: SK   # reserve in specs/_registry.yaml at landing (same commit as this spec)
status: draft
spec-shape: requirements-first
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-07-13
depends-on:
  - replay-substrate
  - event-model
  - process-lifecycle
  - operator-nfr
---
```

## 1. Purpose

This spec defines the normative contract for the harmonik **session-keeper restart cycle** — the per-agent, off-daemon `harmonik keeper` process that drives an intent-preserving handoff → `/clear` → resume cycle before a long-lived Claude session's context pane overflows. It is normative for the five consumer-owned ports the cycle runs over, the pure `Step(state, event) → (state, [action])` reactor that expresses the cycle, the four durable interior events the cycle emits, and the ordering and liveness invariants (SR3, SR4, SR6, SR7, SR9) that constitute the cycle's correctness. It is the second instantiation of the replay-substrate seam ([replay-substrate.md §1]) and the load-bearing home for restart-cycle correctness, which has no normative home in `specs/` today.

## 2. Scope

### 2.1 In scope

- The five keeper ports (`PanePort`, `GaugePort`, `HandoffPort`, `EmitterPort`, `ClockPort`) plus the one-method `RespawnPort`, as consumer-owned narrow interfaces.
- The pure `Step` reactor: states `Idle → AwaitingHandoff → AwaitModelDone → Clearing → Briefing → {Complete | Aborted}`, timers-as-events, and the 11-gate ladder as a pure predicate evaluated only in `Idle`.
- The `ClockPort` requirement: all direct wall-clock reads in the cycle route through the port so the cycle is fake-clock-drivable under replay.
- The four durable interior events (`session_keeper_handoff_written`, `session_keeper_model_done`, `session_keeper_clear_sent`, `session_keeper_new_session_up`) and the ORDERING at which they are emitted, carrying `cycle_id`.
- SR3, SR4, SR6, SR7, SR9 as normative, per-`cycle_id` testable ordering/liveness invariants.
- The behavior-parity constraint (gate ladder, thresholds, bands preserved; reproduce-the-freeze via `InCycle` suppression) and the frozen baseline anchor (427/507 = 84% restart-completion; 347 `clear_unconfirmed`).

### 2.2 Out of scope

- Rewriting the keeper's decision logic (thresholds, gate order, band values) — this spec re-expresses the *existing* logic behind ports and a state machine; the bands are operator-pinned and owned by [operator-nfr.md §4.13 ON-059].
- The daemon and its dispatch loop — the keeper is a standalone per-agent process (Constraint 1: no daemon); daemon lifecycle is owned by [process-lifecycle.md §4.1].
- Remote-control and agent-input transports — not part of the restart cycle.
- The event REGISTRATION mechanics for the four interior events (the `EventType` consts, `mustRegister`, `PayloadCompatEntry`, the §8.13 drift reconciliation, cohort-guard carve-out) — owned by [event-model.md §8.20]; this spec references them and owns only the *when* and the *ordering*.
- The generic seam, the replay `Twin`, the fault model, and the `ClockPort` type definition — owned by [replay-substrate.md §4]; this spec instantiates them.

## 3. Glossary

- **cycle** — one keeper restart attempt: the traversal from `Idle` (gate ladder passes) through handoff, model-done, clear, and brief to a terminal outcome, identified by a `cycle_id`. (see §7)
- **gate ladder** — the ordered 11-gate predicate (`.managed`, empty-SID, boot-grace, threshold, CrispIdle, HoldingDispatch, Sleeping, Held, recent-turn auto-hold, post-answer grace, anti-loop, operator-attached) that decides whether a cycle may start, evaluated only in `Idle`. (see §4.3)
- **cycle_id** — the per-cycle correlation string `cyc-<ts>-<seq>` minted by the keeper at cycle entry; a required payload field on the four interior events. Globally unique only as the composite `(agent_name, cycle_id)`. (see [event-model.md §8.20], D7)
- **PanePort** — the tmux write/read boundary (`Inject`, `SendEscape`, `SetEnv`, `Capture`, `OperatorAttached`). (see §4.1)
- **GaugePort** — the keeper's file-state read surface (`.ctx`/`.sid`/markers/transcript) plus the one managed-session write-back, sampled into a `GateSnapshot` per tick. (see §4.1)
- **HandoffPort** — the handoff-file plus cycle-journal filesystem surface. (see §4.1)
- **EmitterPort** — the durable event bus, structurally the `EmitWithRunID` subset of the handler-contract `EventEmitter`. (see §4.1)
- **ClockPort** — the shared determinism port (`Now`, `Since`, `NewTicker`, `Sleep`) defined in [replay-substrate.md §6]. (see §4.1)
- **Step reactor** — the pure function `Step(state, event) → (state, [action])` mirroring the codex reactor, into which the cycle is refactored; all IO is the imperative shell's. (see §7)
- **model-done** — the signal that the model's *turn* reached an await-input boundary after the handoff was written (not merely that the handoff file content landed); the precondition for `/clear`. (see §4.5)
- **InCycle suppression** — the shell's parking of all non-cycle tick processing while the reactor is off-`Idle`, reproducing the synchronous-block freeze of the pre-rebuild watcher. (see §4.7)
- **terminal outcome** — one of `cycle_complete`, `cycle_aborted`, or the degraded-completion `clear_unconfirmed` (which is not itself a terminal — the brief still fires). (see §8)

## 4. Normative requirements

### 4.1 The five ports (SK-R1, SK-R11)

#### SK-001 — Keeper composes over five consumer-owned ports plus RespawnPort

The keeper cycle MUST be driven through exactly five consumer-owned ports — `PanePort`, `GaugePort`, `HandoffPort`, `EmitterPort`, and `ClockPort` — plus the one-method `RespawnPort`. These ports replace the 22 `CyclerConfig` function-fields of the pre-rebuild cycle. Every function-field MUST land on exactly one port; none is dropped. Policy scalars (thresholds, timeouts, poll interval, `HoldTTL`, boot-grace constants) are inputs, not IO, and MUST remain plain config fields, not ports.

Tags: mechanism

#### SK-002 — PanePort follows PL-021d; Capture is keeper-only

`PanePort` is the tmux write/read boundary. Its `Inject` method MUST follow the `tmux load-buffer` + `paste-buffer` write discipline of [process-lifecycle.md §4.7 PL-021d]; the bare `send-keys` form is FORBIDDEN for injected payloads. `PanePort.Capture` (pane read via `capture-pane`) is keeper-scoped: it MUST NOT be extended into the daemon's process-spawn path. [process-lifecycle.md §4.7 PL-021b] §5 forbids the daemon the `pipe-pane` bridge side-channel specifically (the daemon's own `logs` uses `capture-pane`, `process-lifecycle.md:958`); `PanePort` MUST remain consistent with the PL-021b process-spawn seam without rebuilding the daemon side.

```
INTERFACE PanePort:
    Inject(ctx, target, text) -> error        -- MUST follow PL-021d load-buffer + paste-buffer
    SendEscape(ctx, target) -> error          -- pre-handoff preempt
    SetEnv(ctx, target, key, value) -> error  -- HARMONIK_AGENT env stamp
    Capture(ctx, target) -> (String, error)   -- keeper-scoped; not extended into the daemon path (PL-021b)
    OperatorAttached(target) -> Bool           -- tmux list-clients probe
```

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### SK-003 — GaugePort reads are sampled into a per-tick GateSnapshot

`GaugePort` owns the keeper's file-state read surface and the single managed-session write-back. The seven gate-predicate reads (CrispIdle, HoldingDispatch, Sleeping, Held, Managed, operator-attached, recent-turn) MUST be sampled by the shell into a per-tick `GateSnapshot` (§6) that rides on the `GaugeTick` event, so the pure `Step` never calls a port. `GaugePort` MUST expose `ReadGauge`, `SetManagedSession`, `ClearPrecompactTrigger`, `SetHold(sessionID) → (sessionID, error)` (the Clock-stamped Gate-5d auto-hold action, per §6.3 and SK-011), and `Snapshot(sessionID) → GateSnapshot`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### SK-004 — HandoffPort holds the handoff file and the cycle journal

`HandoffPort` MUST expose both the handoff-file surface (`HandoffPath`, `ReadHandoff`, `HandoffModTime`, `TruncateHandoff`) and the cycle journal (`WriteJournal`, `ReadJournal`). The journal MUST be retained: its observability role is superseded by the four interior events (§4.4), but its crash-recovery role is not. The journal phase vocabulary MUST stay byte-identical to the pre-rebuild set — `opened`, `handoff_injected`, `confirmed`, `cleared`, `resumed`, `complete`, `aborted` — as a parity requirement.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### SK-005 — EmitterPort is the existing keeper.Emitter subset

`EmitterPort` MUST remain structurally identical to the existing `keeper.Emitter` interface, which is the exact `EmitWithRunID` subset of the handler-contract `EventEmitter`. No divergent bus port is introduced; every `handlercontract.EventEmitter` and `eventbus.EventBus` already satisfies it. SK-R1 is satisfied by subsetting, with zero adaptation code.

```
INTERFACE EmitterPort:   -- == keeper.Emitter (UNCHANGED)
    EmitWithRunID(ctx, runID, eventType, payload) -> error
```

Tags: mechanism

#### SK-006 — ClockPort is required by reference from replay-substrate

`ClockPort` MUST be the `ClockPort` type defined in [replay-substrate.md §6] (`Now`, `Since`, `NewTicker`, `Sleep`). The keeper MUST NOT define its own clock port. The real implementation delegates to package `time`; the fake advances virtual time.

Tags: mechanism

#### SK-007 — RespawnPort is a one-method process-lifecycle port

The kill-and-respawn escalation (invoked after `MaxHandoffTimeouts`) is a process-lifecycle effect, not a pane write. It MUST be a one-method `RespawnPort` (`ForceRestart(ctx, agent) -> error`) and MUST NOT be folded into `PanePort`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 4.2 ClockPort migration (SK-R3)

#### SK-008 — ClockPort replaces the direct wall-clock sites

Every direct wall-clock read in the cycle (`time.Now`, `time.Since`, `time.NewTicker`, `time.Sleep`/`sleepCtx`) — the 34 sites in the cycle core, the injector sleeps, the watcher ticker and clock reads, and the two pre-existing `cfg.Now` seams — MUST route through `ClockPort`. The shell MUST own the `ClockPort`; no clock call may occur inside the pure `Step`. Every timestamp used inside `Step` MUST come from an event's Clock-stamped `at` field, so that interval gates become pure arithmetic (`event.at.Sub(stateTS)`) over event-carried values.

Tags: mechanism

### 4.3 The pure Step reactor (SK-R2)

#### SK-009 — The cycle is a pure Step(state, event) → (state, [action]) machine

The cycle MUST be expressed as a pure reactor `Step(state, event) → (state, [action])`, mirroring the codex reactor and driven by the `Run` driver of [replay-substrate.md §4]. `Step` MUST be a pure function of `(State, Event)`: it performs no IO, mints no ids, and reads no clock. The terminal outcomes (`cycle_complete`, `cycle_aborted`, `clear_unconfirmed`) become explicit terminal transitions of this machine (§7, §8). All anti-loop and hysteresis fields become reactor `State`.

Tags: mechanism

#### SK-010 — Timers are modeled as events

Timeout races MUST be modeled as timers-as-events: `Step` emits `ArmTimer{kind, d}` and `CancelTimer{kind}` actions, and consumes `TimerFired{kind}` events. The shell owns one `ClockPort` timer per armed kind and emits `TimerFired` when it elapses. The four timer kinds are `handoff_timeout`, `model_done_timeout`, `clear_settle`, and `clear_backstop` (§7.1). This dissolves the two blocking poll loops and the backstop deadline into replayable event interleavings. No `context.WithTimeout` wall-clock deadline may survive inside the cycle.

Tags: mechanism

#### SK-011 — The 11-gate ladder is preserved as a pure predicate with an unconditional prelude

The 11-gate ladder MUST be preserved in exact order and evaluated as a pure predicate over the event's `GateSnapshot`, only in the `Idle` state (which is the structural guarantee for SR7). Its unconditional prelude side effects — re-arm observation, the same-SID escape hatch, boot-grace SID tracking, the Gate-5d auto-hold write, and the precompact per-gate `ClearPrecompactMarker` + `precompact_blocked` emissions — MUST run before gating and MUST be emitted as actions on the ladder-*fail* path too. A short-circuit that skips the prelude on failure is FORBIDDEN, because it would change observable state.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.4 The four durable interior events (SK-R4)

#### SK-012 — The four interior events are emitted at their named transitions carrying cycle_id

The keeper MUST emit the four durable interior events at these transitions, each carrying `agent_name`, a REQUIRED `Valid()`-checked `cycle_id` (json `cycle_id`, no omitempty), and `session_id`; the envelope `run_id` MUST be left absent (D7):

- `session_keeper_handoff_written` — on `AwaitingHandoff → AwaitModelDone` (nonce observed, or the freshness-recovery edge with `recovered:true` + `handoff_mtime`).
- `session_keeper_model_done` — on `AwaitModelDone → Clearing` (model-done observed, or `model_done_timeout` fired with `degraded:true`).
- `session_keeper_clear_sent` — on the first `InjectClear` (`attempt:1`) and on each defensive re-inject (`attempt` incremented).
- `session_keeper_new_session_up` — on `Clearing → Briefing` (new session id observed), immediately before the managed-session rebind.

The payload STRUCTURES and their registration are owned by [event-model.md §8.20]; this requirement owns the *when*. The four events are classified O-class (observational) for this phase (D9).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### SK-013 — Emit failures for the four interior events MUST NOT be silently swallowed

Emit failures for the four interior events MUST NOT be discarded (the pre-rebuild code discards every keeper emit as `_ =`). At minimum the keeper MUST log on emit failure. "A durable event that silently fails to write" is a spec lie; because the events are O-class (not F-class) the keeper does not retry or block on the failure, but it MUST make the failure observable.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 4.5 The model-done signal (SK-R5)

#### SK-014 — /clear MUST NOT be injected before model-done

`/clear` (`InjectClear`) MUST NOT be injected before a `model_done` signal for the in-flight `cycle_id`. The keeper MUST derive `model_done` from a real durable source with a fail-open bound:

- **Primary source** — the Stop-hook `.idle` marker: after `handoff_written` at `t_nonce`, the first `mtime(.idle) ≥ t_nonce` yields `ModelDone{source:"idle_marker"}`. The compare MUST be strict against the nonce-observation instant, with no CrispIdle tolerance applied.
- **Backstop source** — a recent assistant transcript turn with turn-timestamp `≥ t_nonce` yields `ModelDone{source:"transcript_turn"}`, for agents whose Stop hook is not wired.
- **Liveness bound** — a `model_done_timeout` (default ~60s, strictly less than `ClearConfirmBackstop` = 150s) after which the reactor MUST proceed to `Clearing` anyway, emitting `model_done{source:"timeout", degraded:true}`. The timeout path preserves the pre-rebuild clear-immediately behavior as the degraded mode, so the SR4 tightening is fail-open: a lost `.idle` write can never wedge the cycle.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 4.6 Bounded liveness (SK-R6)

#### SK-015 — Every cycle reaches a terminal or restart_failed within a bounded window

Every `handoff_started(c)` MUST reach exactly one terminal outcome within a bounded window — approximately `HandoffTimeout (300s) + model_done_timeout (60s) + ClearConfirmBackstop (150s) + injection overhead` ≈ 520s — or emit a `restart_failed`-class event. Silence is FORBIDDEN. Structurally, every `TimerFired` edge MUST land in a state that has an outgoing action, so the machine cannot wedge without emitting.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 4.7 Behavior parity (SK-R9)

#### SK-016 — Gate ladder, thresholds, and bands are preserved

The gate ladder (order and predicates), the threshold math (routed through the existing `minAbsOrPctCeil` ceiling function), and the operator-pinned warn/act bands MUST be preserved unchanged. This spec re-expresses the existing logic behind ports and a state machine; it MUST NOT alter band values or gate order. The bands are owned by [operator-nfr.md §4.13 ON-059] and are HARD-NO on change without operator direction.

Tags: mechanism

#### SK-017 — Reproduce-the-freeze via InCycle suppression

The shell MUST reproduce the pre-rebuild synchronous-block freeze via `InCycle` suppression: while the reactor is off-`Idle`, the shell MUST park all non-cycle tick processing (the warn state machine, precompact detection, heartbeat, reaper, hard-ceiling) and run only the cycle-detection poll and timer-fire that drive the reactor forward. When the machine returns to `Idle`, full tick processing resumes. This keeps the baseline comparison apples-to-apples; relaxing `InCycle` is deferred (§11).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### SK-018 — Old-corpus ModelDone synthesis preserves pre-rebuild goldens

When replaying pre-rebuild recordings (which contain no `model_done` event), the measurement `StimulusSynthesizer` MUST synthesize `ModelDone` immediately after `handoff_written`, so old-corpus action goldens stay byte-identical (clear fires right after confirm, as before). Only NEW recordings assert the real `model_done` ordering. This is the sole permitted-divergence item for the SR4 axis in the old-vs-new differential.

Tags: mechanism

### 4.8 Baseline anchor (SK-R10)

#### SK-019 — The frozen baseline MUST be characterized and not regressed

The rebuilt reactor, replayed over the recorded restart cycles, MUST characterize and MUST NOT regress the frozen baseline (`baseline-2026-07-13`): restart-completion 427/507 = 84% (MUST NOT decrease), degraded-completion `clear_unconfirmed`/complete 347/427 = 81% (target: decrease via SR6, MUST NOT increase), unterminated cycles 1 → MUST be 0 (SR9), and terminal exclusivity (zero overlapping terminals per cycle). Divergence from the baseline MUST be confined to the permitted-divergence allowlist: the SR4 tightening (SK-018) plus the four new interior events.

Tags: mechanism

### 4.9 Verification obligation (SK-R8)

#### SK-020 — The invariants are verified by property tests over the corpus and fault modes

The §5 invariants MUST be verified by property tests over the recorded corpus and the four replay-substrate fault modes (drop-after, stall, truncate, dup), using the replay-substrate L0–L2 zero-token tiers ([replay-substrate.md §4]). The keeper does not grade itself: an out-of-band oracle (`jq`/`grep` over the emitted log plus direct filesystem/`stat` reads) MUST confirm terminal-never-silence under the full fault matrix.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

## 5. Invariants

The invariants below are stated per `cycle_id` `c` for a single agent. Their ordering substrate is the append-only, UUIDv7-`event_id`-ordered `events.jsonl` with a single `FileEmitter` writer per agent, so per-cycle intra-agent `event_id` assertions are sound. This is the load-bearing section: each invariant is a testable property the property tests of SK-020 prove.

#### SK-INV-001 — SR3: handoff-write-done precedes /clear

For every cycle `c`, `session_keeper_handoff_written(c)` MUST be emitted before `session_keeper_clear_sent(c)`. This holds structurally: `Clearing` is reachable only via `AwaitModelDone`, reachable only via the two `handoff_written` edges; the abort path never clears.

Tags: mechanism

#### SK-INV-002 — SR4: model-done precedes /clear

For every cycle `c`, `session_keeper_model_done(c)` MUST be emitted before `session_keeper_clear_sent(c)`. This is the headline tightening: it holds via the new `AwaitModelDone` state (§4.5), which no pre-rebuild code enforced.

Tags: mechanism

#### SK-INV-003 — SR6: brief only after new-session confirmed, else clear_unconfirmed

For every cycle `c` that reaches `Briefing`, exactly one of the following MUST hold, and it MUST precede `cycle_complete(c)`: either `session_keeper_new_session_up(c)` was emitted (the confirmed path), or `clear_unconfirmed(c)` was emitted (the backstop-exhaustion degraded path). Both MUST NOT be emitted for the same cycle, and neither MUST be absent.

Tags: mechanism

#### SK-INV-004 — SR7: no overlapping restarts for one agent

For a single agent, between `handoff_started(c1)` and the terminal of `c1`, no `handoff_started(c2)` MUST be emitted for any `c2 ≠ c1`. This holds structurally because the gate ladder is evaluated only in `Idle`, and the machine is off-`Idle` for the entire duration of a cycle.

Tags: mechanism

#### SK-INV-005 — SR9: bounded liveness, never silence

For every cycle `c`, `handoff_started(c)` MUST reach exactly one terminal outcome within the SK-015 bounded window, or emit a `restart_failed`-class event. A cycle that produces neither a terminal nor a `restart_failed` event within the window is a conformance failure. Every `TimerFired` edge lands in a state with an outgoing action, so the machine cannot wedge silently.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

## 6. Schemas and data shapes

### 6.1 The five port interfaces

The port interfaces are given in §4.1 (SK-002 `PanePort`, SK-005 `EmitterPort`) and restated below for `GaugePort`, `HandoffPort`, `ClockPort`, and `RespawnPort`.

```
INTERFACE GaugePort:
    ReadGauge() -> (CtxFile, Timestamp, error)   -- .ctx (+ .sid overlay when primary UUIDv4)
    SetManagedSession(sessionID) -> error        -- "" clears; rebind after cycle
    ClearPrecompactTrigger() -> error            -- remove .precompact marker
    SetHold(sessionID) -> (String, error)        -- Gate-5d auto-hold; Clock-stamped hold marker (see §6.3, SK-011)
    Snapshot(sessionID) -> GateSnapshot          -- one read-burst per tick (Step never calls this)

INTERFACE HandoffPort:
    HandoffPath() -> String
    ReadHandoff() -> (String, error)
    HandoffModTime() -> (Timestamp, Bool)
    TruncateHandoff() -> error
    WriteJournal(j) -> error                      -- phase vocab byte-identical (SK-004)
    ReadJournal() -> (CycleJournal, error)        -- crash recovery only

INTERFACE RespawnPort:
    ForceRestart(ctx, agent) -> error             -- kill+respawn after MaxHandoffTimeouts

INTERFACE ClockPort:   -- defined in [replay-substrate.md §6]; required by reference (SK-006)
    Now() -> Timestamp
    Since(t) -> Duration
    NewTicker(d) -> Ticker
    Sleep(ctx, d) -> Bool                          -- honors ctx cancel; reports full-d-elapsed
```

### 6.2 GateSnapshot

The `GateSnapshot` is sampled by the shell (one `GaugePort` read-burst per tick) and rides on the `GaugeTick`/`PrecompactTrigger`/`IdleRestartTick` events, so `Step` reads gate inputs from the event value, never from a port.

```
RECORD GateSnapshot:
    Managed             : Bool
    CrispIdle           : Bool
    HoldingDispatch     : Bool
    Sleeping            : Bool
    Held                : Bool
    OperatorAttached    : Bool
    LastUserTurnAt      : Timestamp   -- Gate 5d
    LastAssistantTurnAt : Timestamp   -- Gate 5e
```

### 6.3 Event and Action vocabularies

The reactor consumes `Event` values (shell → reactor) and produces `Action` values (reactor → effector). All are flat, JSON-round-trippable structs.

```
Events:  GaugeTick{cf, at, gates}  PrecompactTrigger{cf, at, gates}  IdleRestartTick{cf, at, gates}
         NonceObserved{cycleID, at}  HandoffFreshSeen{cycleID, mtime, at}
         ModelDone{cycleID, sessionID, at, source}  SessionChanged{cycleID, prevSID, newSID, at}
         TimerFired{cycleID, kind, at}  CrashJournal{j, at}
         -- TimerFired.kind in {handoff_timeout, model_done_timeout, clear_settle, clear_backstop}

Actions: WriteJournal{phase, reason}  TruncateHandoff  SendEscape  InjectHandoffCmd{cycleID}
         InjectClear  InjectBrief  SetTmuxEnv{key, value}  SetManagedSession{sid}
         ClearPrecompactMarker  SetHold  Emit{type, payload}
         ArmTimer{kind, d}  CancelTimer{kind}  ForceRestart
```

### 6.4 The four interior payload structs (CANONICAL)

Registered in [event-model.md §8.20]; restated here verbatim from the authoritative resolution (00b R1+R2). Every struct carries `AgentName` and a required `CycleID` (json `cycle_id`, no omitempty); `Valid()` asserts `cycle_id != ""`.

```go
// §8.20.1
type SessionKeeperHandoffWrittenPayload struct {
    AgentName    string `json:"agent_name"`
    CycleID      string `json:"cycle_id"`                // REQUIRED
    SessionID    string `json:"session_id,omitempty"`
    Nonce        string `json:"nonce,omitempty"`         // confirmed nonce marker (audit)
    Recovered    bool   `json:"recovered,omitempty"`     // true iff accepted via freshness recovery (not nonce)
    HandoffMtime string `json:"handoff_mtime,omitempty"` // RFC3339; carried on the recovery edge
}
// §8.20.2
type SessionKeeperModelDonePayload struct {
    AgentName string `json:"agent_name"`
    CycleID   string `json:"cycle_id"`                   // REQUIRED
    SessionID string `json:"session_id,omitempty"`
    Source    string `json:"source"`                     // REQUIRED: "idle_marker" | "transcript_turn" | "timeout"
    Degraded  bool   `json:"degraded,omitempty"`         // true iff reached via model_done_timeout
}
// §8.20.3
type SessionKeeperClearSentPayload struct {
    AgentName string `json:"agent_name"`
    CycleID   string `json:"cycle_id"`                   // REQUIRED
    SessionID string `json:"session_id,omitempty"`
    Attempt   int    `json:"attempt"`                    // 1-based; increments on defensive re-injects
}
// §8.20.4
type SessionKeeperNewSessionUpPayload struct {
    AgentName     string `json:"agent_name"`
    CycleID       string `json:"cycle_id"`               // REQUIRED
    PrevSessionID string `json:"prev_session_id"`        // REQUIRED (needed for the != check)
    NewSessionID  string `json:"new_session_id"`         // REQUIRED; Valid(): non-empty AND != PrevSessionID
}
```

### 6.5 Co-owned event payloads

Per [replay-substrate.md §6]-style co-ownership, the emitting spec is normative for the *when*; event-model is normative for the *shape*:

- `session_keeper_handoff_written` — emitted per SK-012; payload schema in [event-model.md §8.20].
- `session_keeper_model_done` — emitted per SK-012 / SK-014; payload schema in [event-model.md §8.20].
- `session_keeper_clear_sent` — emitted per SK-012; payload schema in [event-model.md §8.20].
- `session_keeper_new_session_up` — emitted per SK-012; payload schema in [event-model.md §8.20].

### 6.6 Schema evolution

The four payloads are versioned schema v1 with a `PayloadCompatEntry` (`CurrentVersion:1, PreviousVersion:0, CompatWindowHolds:true, AdditiveOnly:true`) owned by [event-model.md §8.20]; the N-1 readable contract of [operator-nfr.md §4.5] applies.

## 7. Protocols and state machines

### 7.1 The Step transition table

States: `Idle`, `AwaitingHandoff`, `AwaitModelDone`, `Clearing`, `Briefing`, and terminals `Complete` / `Aborted`. `State` also carries the in-flight `cycleID`, `injectedAt`, `clearAttempt`, `prevSID`, `handoffMtime`, and the anti-loop/hysteresis fields (all timestamps sourced from event `at`). The timer table:

| Timer kind | Duration (default) | Armed on entering | Fired-in-state action |
|---|---|---|---|
| `handoff_timeout` | `HandoffTimeout` = 300s | `AwaitingHandoff` | fresh ⇒ `AwaitModelDone` (recovered); no-fresh ⇒ `Aborted` |
| `model_done_timeout` | ~60s (< `ClearConfirmBackstop`) | `AwaitModelDone` | ⇒ `Clearing`, `degraded:true` |
| `clear_settle` | `ClearSettle` = 10s | `Clearing` (re-armed per retry) | retries left ⇒ re-`InjectClear` + re-arm; else fall to backstop |
| `clear_backstop` | `ClearConfirmBackstop` = 150s | `Clearing` (once) | ⇒ `Briefing` via `clear_unconfirmed` |

**Idle**

| Event | Guard | To | Actions |
|---|---|---|---|
| `GaugeTick` / `PrecompactTrigger` / `IdleRestartTick` | prelude runs unconditionally; then ladder-pass | `AwaitingHandoff` | `WriteJournal(opened)`, `Emit(handoff_started)`, `TruncateHandoff?`, `SendEscape`, `InjectHandoffCmd{cycleID}`, `WriteJournal(handoff_injected)`, `ArmTimer(handoff_timeout, 300s)` |
| same | ladder-fail | `Idle` | prelude side effects only (`SetHold` on 5d path; `ClearPrecompactMarker` + `Emit(precompact_blocked)` on the precompact per-gate path) |
| `CrashJournal` | phase `cleared` | `Briefing` | fast-forward per crash-recovery matrix |
| `CrashJournal` | phase `resumed`/terminal | `Complete` / `Aborted` | close-out per crash-recovery matrix |
| any timer/detection event | — | `Idle` | ignored (no cycle in flight) |

**AwaitingHandoff**

| Event | Guard | To | Actions |
|---|---|---|---|
| `NonceObserved` | — | `AwaitModelDone` | `WriteJournal(confirmed)`, `Emit(handoff_written)`, `CancelTimer(handoff_timeout)`, `ArmTimer(model_done_timeout, 60s)` |
| `TimerFired(handoff_timeout)` | `HandoffFreshSeen` present (mtime ≥ injectedAt) | `AwaitModelDone` | `WriteJournal(confirmed, reason=handoff_timeout_recovered)`, `Emit(handoff_written{recovered:true})`, `ArmTimer(model_done_timeout)` |
| `TimerFired(handoff_timeout)` | no fresh handoff | `Aborted` (terminal) | `WriteJournal(aborted)`, `Emit(cycle_aborted, reason=handoff_timeout)`, anti-loop state update, `SetManagedSession("")?` (guarded), `ForceRestart?` (after `MaxHandoffTimeouts`) |

**AwaitModelDone** (the new SR4 state)

| Event | Guard | To | Actions |
|---|---|---|---|
| `ModelDone` | mtime(.idle) ≥ t_nonce, or transcript backstop | `Clearing` | `Emit(model_done{source})`, `SetTmuxEnv`, `InjectClear`, `Emit(clear_sent{attempt:1})`, `WriteJournal(cleared)`, `CancelTimer(model_done_timeout)`, `ArmTimer(clear_settle, 10s)`, `ArmTimer(clear_backstop, 150s)` |
| `TimerFired(model_done_timeout)` | — | `Clearing` | as above, but `Emit(model_done{source:"timeout", degraded:true})` (fail-open) |

**Clearing**

| Event | Guard | To | Actions |
|---|---|---|---|
| `SessionChanged` | newSID ≠ prevSID | `Briefing` | `Emit(new_session_up{prev, new})`, `SetManagedSession(newSID)`, `CancelTimer(clear_settle)`, `CancelTimer(clear_backstop)` |
| `TimerFired(clear_settle)` | retries left | `Clearing` | `InjectClear` (defensive), `Emit(clear_sent{attempt:n})`, re-`ArmTimer(clear_settle)` |
| `TimerFired(clear_backstop)` | — | `Briefing` | `Emit(clear_unconfirmed)`, `SetManagedSession("")`, `CancelTimer(clear_settle)` |

**Briefing** (immediate, no external event)

| Event | Guard | To | Actions |
|---|---|---|---|
| (entry) | — | `Complete` (terminal) | `InjectBrief`, `WriteJournal(resumed)`, `WriteJournal(complete)`, `Emit(cycle_complete)`, `Emit(cycle_recovered)?`, anti-loop state update, return to `Idle` |

### 7.2 Model-done detection protocol (D12)

```
FUNCTION detect_model_done(cycleID, t_nonce):
    -- shell-side detection poll, running while in AwaitModelDone (SK-014)
    ON each PollInterval tick:
        IF mtime(.idle) >= t_nonce:                       -- strict; no CrispIdle tolerance
            EMIT ModelDone{cycleID, source:"idle_marker"}
        ELSE IF recentAssistantTurn.ts >= t_nonce:        -- backstop for un-wired Stop hooks
            EMIT ModelDone{cycleID, source:"transcript_turn"}
    -- fail-open bound (SK-014, SK-INV-005):
    ON TimerFired(model_done_timeout):
        reactor proceeds to Clearing; Emit(model_done{source:"timeout", degraded:true})
```

The 200ms detection cadence is a shell poll, distinct from the four reactor timers; SK-016/SK-017 require it to reproduce first-tick-after-interval semantics (the first detection read happens one full `PollInterval` after state entry, as `time.NewTicker` delivers).

## 8. Error and failure taxonomy

The cycle's terminal/abort taxonomy has three named outcomes:

### 8.1 cycle_complete

The success terminal — reached at `Briefing → Complete`. The brief is injected and the journal records `complete`. Emitted as `cycle_complete(c)`.

### 8.2 cycle_aborted

The only path that never sends `/clear` — the `AwaitingHandoff` `handoff_timeout` edge with no fresh handoff. Emitted as `cycle_aborted(c, reason=handoff_timeout)`. Every abort carries an explicit reason (the baseline shows 79/79 `handoff_timeout`).

### 8.3 clear_unconfirmed (degraded-completion)

`clear_unconfirmed(c)` is the `Clearing` backstop-exhaustion outcome. It is NOT a terminal by itself: the brief still fires and the cycle still records `cycle_complete(c)`. It is the degraded-completion mode SR6 (SK-INV-003) tracks and aims to reduce; the baseline is 347/427 = 81% degraded-completion, which SK-019 forbids from increasing. A `restart_failed`-class emission is the SR9 escape hatch when even the degraded path cannot complete within the bounded window.

## 9. Cross-references

### 9.1 Depends on

- **[replay-substrate.md §4]** — the `EventSource` / `Effector` / `Run` seam (second instantiation), the replay `Twin`, the four fault modes, and the L0–L2 test tiers this spec's reactor and property tests instantiate.
- **[replay-substrate.md §6]** — the `ClockPort` / `Ticker` type, required by reference in SK-006.
- **[event-model.md §8.20]** — the registration, payload struct catalog, `PayloadCompatEntry`, and cohort-guard carve-out for the four interior events; the payload `cycle_id` joinability contract (D7) this spec's ordering invariants join on.
- **[process-lifecycle.md §4.7 PL-021d]** — the `tmux load-buffer` + `paste-buffer` write discipline that `PanePort.Inject` follows per SK-002.
- **[process-lifecycle.md §4.7 PL-021b]** — the process-spawn seam and its §5 `pipe-pane` bridge prohibition; `PanePort.Capture` stays keeper-scoped and is not extended into the daemon path (SK-002).
- **[operator-nfr.md §4.13 ON-059]** — the restart-now gate ladder and the operator-pinned warn/act bands and thresholds preserved by SK-016.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand by walking every spec's `depends-on` list; they are not stored here.

### 9.3 Co-references (read-only consumption)

- **[operator-nfr.md §4.5]** — the N-1 readable schema-compatibility window applied to the four payloads (§6.6); no reverse dependency.

## 10. Conformance

### 10.1 Conformance profiles

- **Core keeper (this phase)** — all of SK-001…SK-020 and SK-INV-001…SK-INV-005 are in force; an implementation conforms when each passes.
- **Extension (deferred)** — F-class durability for the four interior events and `InCycle` relaxation are explicitly deferred (§11) and are not part of the core conformance claim.

### 10.2 Test-surface obligations

- The full ~55-file keeper suite (including `TestKeeperConformance`, `conformance_keeperx_test.go`, `conformance_keeper_integration_test.go`) stays green at every migration step (SK-016, SK-017 prove parity). Cite [scenario-harness.md] cadence tiers once available.
- The existing causal harness and scenario suites, re-wired as `SyntheticSource[keeper.Event]` + `FakeEffector[keeper.Action]`, become the golden Step-sequence corpus: injected-command-order assertions map 1:1 onto action-log assertions (SK-009).
- Property tests over the frozen corpus (`baseline-2026-07-13`, 507 cycles) plus the four fault modes prove SK-INV-001…SK-INV-005 (SK-020); the permanent L1 net is the golden-vs-`summary.json` corpus test.
- The old-vs-new differential (transition scaffold) proves SK-018 and SK-019 with the permitted-divergence allowlist; it is deleted only after the `StimulusSynthesizer` decision table is frozen and reviewed against a green differential.
- The out-of-band oracle (`jq`/`grep` over the log + `stat` reads) proves terminal-never-silence under the fault matrix (SK-INV-005, SK-020) — the keeper does not grade itself.

### 10.3 Excluded conformance claims

- F-class durability / fsync for the four interior events (O-class this phase, D9).
- Relaxing `InCycle` suppression (a later, separately-measured change).
- The numeric coverage-floor value on the new `Step`/reactor files (measured and stated at implementation time).

## 11. Open questions

None blocking.

> INFORMATIVE: Two items are deferred by design, not open decisions. (1) F-class durability for the four interior events — revisited only if a later phase routes keeper emits through the daemon; O-class is correct for this phase (D9). (2) Relaxing the `InCycle` suppression to let warns/precompact/heartbeat fire during a cycle — a separately-measured change, out of scope for the parity proof (D11). Neither blocks finalizing this spec.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-07-13 | 0.1.0 | foundation-author | Initial draft — session-restart vertical: five ports (SK-001…SK-007), ClockPort migration (SK-008), pure Step reactor + timers-as-events + gate ladder (SK-009…SK-011), four durable interior events (SK-012…SK-013), model-done signal (SK-014), bounded liveness (SK-015), behavior parity (SK-016…SK-018), baseline anchor (SK-019), verification obligation (SK-020); SR3/SR4/SR6/SR7/SR9 as SK-INV-001…SK-INV-005; Step transition table and model-done detection protocol; terminal/abort taxonomy. |
```
