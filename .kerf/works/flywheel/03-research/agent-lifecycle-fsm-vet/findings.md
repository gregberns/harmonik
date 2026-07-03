# Vet — Agent lifecycle state machine port (gateway leverage item #1)

> Component: `agent-lifecycle-fsm-vet`. Round-4 vet. Source: sub-agent (opus), 2026-05-30. **Verdict: YES-WITH-CHANGES. Fixes two real observed failures (`run_stale` ambiguity + hk-za5mz iter-2 stuck-in-READY).**

## TL;DR
- 8-state model maps cleanly onto harmonik's existing implicit lifecycle; addresses **two real observed failure modes** (`run_stale` ambiguity in events.jsonl + the iter-2 review-loop bug **hk-za5mz**, P1 OPEN); the spec corpus already gestures at a similar machine without naming it.
- BUT: PAUSED belongs to `specs/handler-pause.md` at a *different layer* (handler-type, not per-session) and must not be conflated → rename PAUSED → **Suspended**; `correlationId` collapses into existing `run_id`/`session_id`; the TS event-listener mechanism collapses into the existing event bus.
- **3 beads** proposed (spec amendment, Go package, watcher integration + event emission). All `codename:flywheel`.

## §1 — TS shape (verbatim from `Dicklesworthstone/flywheel_gateway`)
**`apps/gateway/src/models/agent-state.ts`:** `enum LifecycleState {SPAWNING, INITIALIZING, READY, EXECUTING, PAUSED, TERMINATING, TERMINATED, FAILED}`; `TERMINAL_STATES={TERMINATED, FAILED}`. `VALID_TRANSITIONS` dense map: every non-terminal can reach `TERMINATING`+`FAILED`; `READY↔EXECUTING` bidirectional; `PAUSED↔READY` (no direct PAUSED→EXECUTING). `TransitionReason` = 14-member string-union (`spawn_started, init_complete, user_action, command_started, command_complete, pause_requested, resume_requested, terminate_requested, terminate_complete, error, timeout, health_check_failed, driver_error, resource_limit`). `StateTransition = {previousState, newState, timestamp, reason, correlationId, error?{code,message}, metadata?}`. `InvalidStateTransitionError extends Error` carries `fromState, toState, agentId`.
**`apps/gateway/src/services/agent-state-machine.ts`:** module of exported functions over a `Map<agentId, AgentStateRecord>`. `AgentStateRecord={agentId, currentState, stateEnteredAt, createdAt, history: StateTransition[]}`. Ring: `MAX_HISTORY_SIZE=50`; tail-keep, drop-oldest. Core API: `transitionState(agentId, newState, reason, error?, metadata?)`; getters; helpers `markAgentReady/Executing/Idle/Paused/Terminating/Terminated/Failed`. Event: `onStateChange(listener) → unsubscribe`; emits `StateChangeEvent {type:"agent.state.changed", agentId, previousState, currentState, timestamp, reason, correlationId, error?}`. **Activity-state (thinking/working/tool_calling) is NOT in these two files** — lives elsewhere; SDK-driver-specific.

## §2 — Harmonik current state (implicit-via-events CONFIRMED)
No per-session lifecycle FSM type. Today: `internal/handler/session.go:42` declares `Session` (SendInput/Kill/Wait/Outcome/Stdout/Stderr/CloseStdin — no `State()`). Implicit states encoded in atomic flags (sync/atomic), `lifecycle.WaitOwner`, and watcher-published events. `specs/handler-contract.md` §4.5/4.6/4.9 declares `agent_ready`, heartbeat+silent-hang detection, typed-error taxonomy. The events `agent_started/ready/output_chunk/completed/failed/rate_limited/rate_limit_cleared/heartbeat` together imply a state machine but no spec consolidates them.
**`handler-pause.md`** has `HandlerStatus {live, paused}` at the **handler-type tier** (per `agent_type` like `claude-code`), persisted in `.harmonik/handler-state.json`. Different layer; TS `PAUSED` is per-session — must not collide.
**`process-lifecycle.md` §7.1** has daemon-status FSM `starting → reconciling → ready → degraded/paused/draining/stopped`. Also different layer.
**Evidence in `.harmonik/events/events.jsonl`:** repeated `run_stale` events (e.g. run `019e7305-7241` at age 627s, then again 1224s) where the last lifecycle event was just `run_started` or `implementer_resumed`. Orchestrator cannot tell whether run is "subprocess never reached READY (spawn-stuck)" vs "READY but no command" vs "EXECUTING but silent" vs "TERMINATING but reap hung."
**Iter-2 review-loop bug `hk-za5mz` (P1, OPEN):** literally "claude --resume on tmux substrate does not process pasted input as a new implementation turn." Per-session FSM would let daemon assert `transition(READY → EXECUTING, reason:command_started)` on paste-injection and detect EXECUTING never validated — surfacing as `InvalidStateTransitionError` or stuck-READY instead of a 10-minute `run_stale` timeout.
**Real failure mode, not preemptive cleanup.**

## §3 — Design
**Home:** `internal/handlercontract/lifecycle/` (new sub-package, ~250 LOC). Justification: per-session FSM owner is `internal/handlercontract/` where Session/Handler/watcher live; `internal/lifecycle/` is already the daemon-process lifecycle; `internal/supervise/` is flywheel-only; under handlercontract twin-handlers see it automatically (HC-INV twin parity).
**Go enum:**
```go
type LifecycleState uint8
const (
    StateSpawning     LifecycleState = iota // proc started, not yet handshaken
    StateInitializing                       // handshake done, skills provisioning
    StateReady                              // agent_ready fired; idle, accepting input
    StateExecuting                          // command in flight (between input-send and outcome)
    StateSuspended                          // per-session pause (operator-paused-this-run)
    StateTerminating                        // SIGTERM sent; Wait not yet returned
    StateTerminated                         // Wait returned, exit==0 or expected
    StateFailed                             // Wait returned with classified error
)
```
Renamed PAUSED → **Suspended** to disambiguate from `handler-pause.md` HandlerStatus.paused (handler-type tier).
**Transitions:** identical TS shape + two harmonik edges: `Executing→Executing` legal as `RecordActivity()` call (not a self-loop transition); `Ready→Failed{reason=silent_hang}` direct edge for HC-026 silent-hang routing.
**Activity-state separation:** SKIP for v1 (TS doesn't actually integrate it; harmonik twin-binary+tmux model produces opaque bytes; defer).
**Concrete Go shape:**
```go
type Machine struct {
    mu       sync.Mutex
    sessID   string                       // harmonik session_id (UUIDv7), replaces correlationId
    runID    string                       // for event correlation
    current  LifecycleState
    enteredAt time.Time
    history  [50]Transition                // fixed-size ring; head index in headIdx
    headIdx  uint8
    len      uint8
    bus      events.Publisher             // existing event bus, NOT separate listener mechanism
}
type Transition struct {
    From, To LifecycleState
    At       time.Time
    Reason   TransitionReason
    ErrCode, ErrMsg string                 // populated on To==StateFailed
}
func (m *Machine) Transition(to LifecycleState, reason TransitionReason, err error) error
func (m *Machine) Current() LifecycleState
func (m *Machine) History() []Transition  // copy
```
`InvalidStateTransitionError` sentinel+struct in `errors.go`, classified `ErrDeterministic` per HC §4.5 (program bug, not retryable).
**Integration points (who calls `Transition`):** (1) `internal/handler/session.go NewSession` → `Spawning,spawn_started`; (2) `internal/handlercontract/adapter.go` (watcher) on `agent_ready`→`Ready,init_complete`, on command emission→`Executing,command_started`, on `agent_completed`→`Terminating,terminate_complete`, on `agent_failed`→`Failed,error,err`; (3) `internal/daemon/workloop.go` on SIGTERM→`Terminating,terminate_requested`, on `Wait()` return→`Terminated|Failed`; (4) `internal/handlerpause/` on per-session freeze-list snapshot→`Suspended,pause_requested`, on resume→`Ready,resume_requested`.
`correlationId` → harmonik's `session_id` (UUIDv7). No new ID type.
**Event emission:** NOT a parallel listener bus. Each `Transition()` publishes `lifecycle_transition` (new event-model entry, class O, additive, schema-version bump) to existing event bus.

## §4 — Vet (real failure modes it fixes)
1. `run_stale` ambiguity (cited): runs go quiet 10+ min with no signal which phase is stuck. `Current()` replaces forensic event-archaeology.
2. **hk-za5mz (P1 OPEN, blocking phase-2 dogfood):** can't tell if claude --resume entered EXECUTING or sat at READY ignoring the paste. FSM + 60s `Ready→Executing` timeout surfaces this explicitly instead of waiting for 10-min `run_stale`.
3. HC-004 idempotency-key consistency: state guard ("if current state not in {terminated,failed}, second Launch returns existing Session") replaces atomic-flag enforcement; makes the rule legible.
4. Twin parity (HC §4.8): FSM becomes a conformance contract twins must drive — strengthens twin-vs-real drift detection.
**Cost:** ~250 LOC + ~150 LOC tests + ~80 LOC integration + spec amendment (~3 pages). One-day port estimate from eval roughly right. Risk: schema-version bump on event-model §8.3 for new event; additive per §6.4, no breaking change.
**Don't import:** TS listener-callback pattern (use existing event bus), helper-functions module-shape (use receivers on `Machine`), `MAX_HISTORY_SIZE=50` magic number (size from observed `run_stale` recurrence; 50 fine for v1, make it a const).

## §5 — Proposed beads (3, all `codename:flywheel`)

**Bead 1 — spec amendment:**
```
br create --title="HC spec: per-session lifecycle FSM (§4.x new requirement HC-064..HC-067)" \
  --type=task --priority=2
# Description:
# Amend specs/handler-contract.md with new §4.x "Per-session lifecycle state":
# - HC-064: LifecycleState enum {Spawning, Initializing, Ready, Executing, Suspended, Terminating, Terminated, Failed}; TERMINAL_STATES={Terminated, Failed}.
# - HC-065: VALID_TRANSITIONS table (mirror TS; add Ready→Failed{silent_hang}).
# - HC-066: InvalidStateTransitionError sentinel; classify ErrDeterministic per §4.5.
# - HC-067: transition-history ring (size 50, drop-oldest); Session.History() returns copy.
# Disambiguate Suspended (per-session) from handler-pause.md HandlerStatus.paused (per-handler-type) in §3.
# Add event-model.md §8.3.x entry for lifecycle_transition (class O, additive, schema-version bump).
# Acceptance: agent-reviewer APPROVE; cross-refs added to handler-pause.md §1 and process-lifecycle.md §7.1 disambiguating layers.
# Refs: hk-za5mz, run_stale events in events.jsonl.
# Labels: codename:flywheel, handler-contract, spec-amendment
```

**Bead 2 — Go package:**
```
br create --title="Port flywheel_gateway agent lifecycle FSM → internal/handlercontract/lifecycle/" \
  --type=feature --priority=2
# Description:
# Implement HC-064..HC-067 in new internal/handlercontract/lifecycle/ subpackage (~250 LOC).
# - types.go: LifecycleState enum (uint8); TransitionReason enum; Transition struct.
# - machine.go: Machine struct {mu, sessID, runID, current, enteredAt, history[50], headIdx, len, bus}; methods Transition/Current/History/EnteredAt.
# - errors.go: InvalidStateTransitionError; wraps to ErrDeterministic per HC §4.5.
# - table.go: validTransitions[from][to] bool; package init builds dense table.
# - machine_test.go: table-driven test (every legal+illegal edge), ring-eviction test (51 transitions), concurrent-Transition smoke.
# Acceptance: go build/test green; depguard/component-matrix entry added (leaf package); no event emission yet.
# Refs: HC-064 (bead 1).
# Labels: codename:flywheel, handler-contract
```

**Bead 3 — watcher integration + event emission:**
```
br create --title="Wire LifecycleState into Session/watcher; emit lifecycle_transition events" \
  --type=feature --priority=2
# Description:
# Integrate FSM from bead 2 into live handler/Session/watcher; emit new lifecycle_transition event.
# - internal/handler/session.go: NewSession constructs Machine, Transitions to StateSpawning on cmd.Start.
# - internal/handlercontract/adapter.go: on agent_ready/started/completed/failed/rate_limited, call Machine.Transition with appropriate reason. Heartbeat does NOT transition (call RecordActivity instead).
# - internal/daemon/workloop.go: SIGTERM path Transitions to StateTerminating; Wait return → Terminated|Failed.
# - internal/eventmodel/types.go: register lifecycle_transition (schema_version bump, additive).
# - Twin parity: claude-twin + pi-twin drive same Transition calls (HC §4.8).
# Acceptance: scenario test of happy-path emits Spawning→Initializing→Ready→Executing→Terminating→Terminated in order; silent-hang scenario transitions to StateFailed{silent_hang} BEFORE run_stale; hk-za5mz repro surfaces as deterministic transition-timeout (not 10-min run_stale).
# Refs: hk-za5mz, beads 1+2.
# Labels: codename:flywheel, handler-contract, event-model
```
