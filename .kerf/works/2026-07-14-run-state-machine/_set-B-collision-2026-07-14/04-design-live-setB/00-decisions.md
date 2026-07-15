# 04-Design / 00 — Cross-cutting decisions (the pinned contracts)

> **Pass 4 (Change Design).** Fable synthesis of the six Research findings
> (`03-research/c{1..6}-*/findings.md`) against the Decompose requirements
> (`02-components.md`). This doc SETTLES every cross-component decision and pins the
> shared contracts; the six per-component design docs elaborate within these pins and
> MUST NOT contradict them. Autonomous mode, signoffs waived (2026-07-14). The design
> mirrors the LANDED keeper reactor (P1 through T7:
> `internal/keeper/{step,shell,cycle}.go`, `internal/substrate`) — M3 is the second
> production instantiation of that seam, not a parallel invention.
>
> Target spec: a NEW normative `specs/run-state-machine.md`, prefix **RSM** (reserve in
> `specs/_registry.yaml` at landing). Requirement IDs below use RSM-NNN. Additive touch:
> `specs/event-model.md` (two new durable events). Enforcement: `.golangci.yml` depguard.

---

## D1 — Package layout + depguard `[grounds C4/C3 success-criterion 5]`

**Decision.** Two new sub-packages under `internal/daemon/`:
- `internal/daemon/runexec` — the pure reactor: `Step`, `State`, `Event`, `Action`,
  `TimerKind`, and the run-lifecycle sub-machines. **No IO, no clock reads, no
  `internal/daemon` imports** (depends only on `core`, `substrate`, and small
  value/enum types).
- `internal/daemon/mergequeue` — the explicit merge queue (C2). Owns the serialising
  goroutine + the critical-section executor.

The `beadRunOne` shell stays in `package daemon` and becomes a thin driver: it
constructs the reactor, samples IO into events, and executes actions via an `Effector`.

**Depguard edges** (`.golangci.yml`, new `depguard` list entries mirroring the keeper's
`internal/keeper` isolation): `internal/daemon/runexec` may NOT import `internal/daemon`
(prevents run-lifecycle logic leaking back into the flat package); `internal/daemon` MAY
import both sub-packages. `runexec` may import `substrate` and `core` only.

**Rationale.** RSM success-criterion 5 (01-problem-space §5.5). The keeper proves the
shape: `internal/keeper` is depguard-isolated from `daemon`. C4 findings §1 confirm the
pure-Step / shell-effector split is what makes the machine testable.

## D2 — ClockPort seam and its scope `[C1]`

**Decision.** Add `Clock substrate.ClockPort` as a run-lifecycle dependency (a field on
the C3 launch/wait ports, and a `CyclerConfig`-style field on the reactor shell),
defaulting to `substrate.SystemClock{}` when nil — exactly the keeper's
`CyclerConfig.Clock` (cycle.go:92-97). Tests pass `substrate.FakeClock`.

**Scope is the FIVE run-path files, not two.** C1 findings Risk 1 + PLANNER-RECONCILE:
the deterministic-timeout surface the liveness test (C5) needs spans
`workloop.go`, `reviewloop.go`, `agentready.go` (`:194` `time.After`), `postreadyhang.go`
(`:59` `time.NewTimer`), and `pasteinject.go` (the quit-on-commit watchdog). The design
adopts the broader scope; the 26-site figure in 02-components §C1 is superseded by the
verified 38 (workloop) + 8 (reviewloop) + the three deepest waits.

**The ONE seam.** All four existing daemon time mechanisms (the hook-struct with no clock,
the mutable package vars like `agentReadyKillReapTimeout`, the three `Now func()` config
fields, the duration configs) reconcile onto `substrate.ClockPort`. The `StaleWatcher.Now`
field (stalewatch.go:336) and its raw `ScanInterval` ticker migrate to the same port.
`context.WithTimeout` run-path sites (workloop.go:4883, reviewloop.go:744/1484) become
`Clock.Sleep`/timer-event form (C1 Risk 2). `queue.AdvanceGroup(..., now)` already takes
`now` — pass `Clock.Now()` (zero-risk win).

**Timer vs interval classification.** `time.After`/`NewTimer` select-deadlines become
**reactor timer events** (D6). `Now`/`Since` reads become `Clock.Now()`/`Clock.Since()`
in the shell (stamped onto events). No blocking wait reads the wall clock.

## D3 — Merge-queue shape + the true critical section `[C2]`

**Decision — queue shape.** A **channel-fed single serialising goroutine per target
branch** in `internal/daemon/mergequeue` (NOT a bare mutex; NOT `internal/queue`, which
is the bead-queue data model, not an execution serialiser — C2 findings Q5). Submit is
`Submit(ctx, MergeJob) MergeOutcome` (synchronous to the caller, serialised at the
goroutine). The queue REPLACES `mergeMu`; the daemon holds no global merge mutex.

**The true critical section (C2 findings conclusion).** Only this contiguous window runs
inside the serialised goroutine, per target branch:

> **re-validate (FF-check with freshly-read mainTip) → `git update-ref` land → [fmt-gate
> ref-advance] → `git push origin` (+ blind rollback on failure) → `git restore --staged`
> → `git reset --hard HEAD`**  — i.e. workloop.go `:6755` → `:7043` minus the
> build/vet/fmt *executions*.

**Runs OUTSIDE the lock, speculatively, re-validated inside** (the big win): tip
resolution, worktree cleanup, **the rebase**, `stripRunContextFromMerge`, **`go build ./...`
+ `go vet`**, the **gofumpt/gci** run. Re-rebase-⇒-re-build invariant is preserved: the
existing inner retry loop (`:6750`, `maxPushAttempts=3`) already re-executes per attempt;
the queue design keeps build/fmt inside the per-attempt re-run, NOT a build-once-before-submit.

**`git push` STAYS inside the window** (coupled to its blind rollback). Moving push out —
so the window ends at local `update-ref` — requires the local-land-is-commit-point +
EM-INV-005 reconciliation redesign that ROADMAP assigns to **M4**. C2 ships the
"build/rebase-out, ref-window-in" split; push-async is explicitly out of scope
(PLANNER-RECONCILE, C2 findings §PLANNER-RECONCILE).

**Two coupled invariants the queue MUST preserve:**
- **hk-zguy6 escape check** (workloop.go:5169): today mutually exclusive with the merge
  under `mergeMu`. Preserve by routing the escape read through the SAME queue as a
  read-only "tree-quiescent" slot (fenced against the update-ref→reset-hard window), else
  false `implementer_escaped_worktree` reopens return. Regression-pinned by
  `escapedetect_hkooexj_test.go`.
- **hk-lt091 / hk-h8u7p remote base-sync + worktree-add** (workloop.go:3636 region):
  keep an equivalent exclusion NOW (remote runs get empty-HEAD worktrees + index.lock
  races otherwise). M4 re-plumbs it; C2 keeps it inside the same exclusion domain via the
  WorktreePort's create serialisation (`worktreeCreateMu` survives, its nesting under the
  merge lock dissolves).

**Ordering.** The FIFO goroutine STRENGTHENS ordering (bounds a loser's re-rebase count to
queue-depth-ahead) and breaks nothing — no current merge ordering guarantee exists (C2 Q7).
The queue accepts `context.Background()` submissions (shutdown-drain, hk-dnrg).

**DoD lint (success-criterion 4).** A test/vet check fails if a lock (or the queue
goroutine) is held across `go build`/`git push origin`/`br sync`. Realised as a test that
asserts the critical-section executor performs no build/network IO (the build/fmt/rebase
run on the speculative path, observable via injected effect spies).

## D4 — The workLoopDeps port cut (eight groupings) `[C3]`

**Decision.** M3 promotes ONLY the run-lifecycle fields into consumer-owned narrow ports
(keeper D10 idiom: interfaces declared in `runexec`/`daemon` beside the consumer, the
daemon shell satisfies them structurally; existing-interface reuse by alias where possible,
e.g. `EmitterPort = handlercontract.EventEmitter`). The other ~50 fields stay on
`workLoopDeps` for **M5**. The cut line is the C3 census (RUN vs SG/MAINT/OTHER).

**The eight ports** (revising the dossier's six — C3 PLANNER-RECONCILE):
1. **LedgerPort** — brAdapter, intentLogDir, brTimeoutCfg, skipBrHistoryRotation, tidGen,
   + `CloseBead`/`ReopenBead`/close-with-history-trim methods. (Drop `staleBlockerCloser` —
   it is dispatch/claim-side, NOT run.)
2. **EmitterPort** — bus (alias); fold emittedEpics(+Mu) behind a `CompletionEmitter` method.
3. **WorktreePort** — worktreeFactory, worktreeCreateMu (owns BOTH local + remote create).
4. **MergePort** — the D3 merge-queue submit surface (absorbs mergeMu, targetBranch,
   protectBranches, brPath).
5. **LaunchPort** — launchSpecBuilder, harnessRegistry, substrate, defaultHarness,
   handler{Binary,Args,Env}, daemonBinaryPath, sandboxCfg, agentSpawnSem. (An
   **AgentWaitPort** split — hookStore, adapterRegistry, the three agent-ready timeouts —
   is offered as a design option; the design keeps ONE LaunchPort unless the planner
   directs otherwise — C3 PLANNER-RECONCILE.)
6. **ClockPort** — C1/D2.
7. **WorkerPort** — workerRegistry, runner, remote code-sync helpers (dossier missed it).
8. **GatePort** — cpRegistry (DOT gate-node eval; fits none of the other seven).

**Shared-by-reference, NOT ports** (SG): runRegistry, localInFlight, tidGen (one shared
generator, EM-018a), agentSpawnSem (rides LaunchPort as a handle), cacheReapMu
(dispatch-side, not a run concern). **queueStore** does NOT become a run port: its ONE run
write (review-loop-failure budget, workloop.go:3947) is surfaced as a terminal-event field
the dispatch side applies (keeps the queue out of the reactor). **MAINT value fields**
(lastCoordinatorReap/lastDiskCheck/lastGoCacheClean/diskLow) lift OUT of the by-value
bundle into loop-owned state. **Delete two DEAD fields** (`h`, `beadAuditLogger`) — census
shrinks 85→83.

**Rationale.** The by-value copy at workloop.go:3072 already implies every RUN field is
immutable config or a shared handle; the port decomposition preserves this exactly (values
→ RunConfig value-bag; shared state → reference-typed ports). No mutex-by-value bug exists
today (all mutexes are pointers) — extraction removes the latent frozen-copy trap.

## D5 — The reactor: State / Event / Action shape `[C4]`

**Decision.** Mirror the keeper reactor exactly (C4 findings §1):
- **State** = a `RunState` struct: a `Phase string` named-state enum + in-flight run
  fields + iteration/cascade bookkeeping. All timestamps event-`At`-sourced; no clock read
  inside Step.
- **Named phases:** `Resolving → WorktreeReady → {mode fork} → BuildingSpec → Launching →
  AwaitingReady → Monitoring → Exited → Gating → Merging → {Closed | Reopened}`.
  - **Claiming is a SHELL concern** (ClaimBead/Register/localInFlight all happen in the
    outer dispatch loop BEFORE the run goroutine — workloop.go:2853/2991/3010). The reactor
    is born post-claim (C4 Risk 3 / PLANNER-RECONCILE resolved: start the machine post-claim).
  - The ~20 pre-launch `ReopenBead` guards collapse onto ONE `* → Reopened{reason}` edge.
  - `AwaitingReady` is skipped/immediate for `CompletionProcessExit` harnesses (codex/pi) —
    a per-mode edge, not a separate machine.
- **Event** = ONE flat JSON-round-trippable struct, kind-discriminated (`EventKind`).
  Shell-sampled data (spec content, worktree HEAD, mint ids) rides ON the event so Step is
  total + pure. Monotonic per-run `Seq` for dedup (codex idiom). Kinds include:
  EvClaimed, EvWorktreeReady/Failed, EvLaunched/EvLaunchFailed, EvAgentReady, EvAgentOutput,
  EvHandlerExited{exitCode,waitErr}, EvNoChangeTimeout, EvCtxCancelled{shutdown|abort},
  EvMergeResult{success,noChange,reason}, EvTimerFired{kind}, plus the M2 input events (D10).
- **Action** = ONE flat struct: ActReopen, ActEmit{payload []byte}, ActEmitOutcome,
  ActCloseBead, ActSubmitMerge, ActLaunch, ActAcquireSpawnSlot, ActSendInput,
  ActLifecycleTransition, ActArmTimer{kind,d}, ActCancelTimer{kind}.
- **Signatures:** `func stepRun(cfg, s RunState, ev Event) (RunState, []Action)` (total,
  pure) + `func (m *Run) Step(ev Event) []Action` + `State() RunState` + a `substrate.Run`
  one-liner for the replay path.

**Sub-machine nesting (C4 §4).** ReviewLoop and DOT are **nested sub-machines** sharing the
inner `Launching→AwaitingReady→Monitoring→Exited` spine; single-shot is the degenerate
1-node cascade. iterationCount / currentNodeID live in RunState. Flattening all three
triples the launch/monitor state count — the code already wants nesting (dot mirrors
reviewloop).

**hclifecycle.Machine is PROJECTED, not subsumed (C4 §5).** It is per-*session* (HC-065)
with an external concurrent reader (the stale watcher). The reactor emits
`ActLifecycleTransition{to,reason}` that the effector applies to the session Machine,
preserving HC-065 emission + the reader surface. (Only single-mode calls it today, at
workloop.go:5036; the reactor must not require a Machine.)

**Concurrency gates are effector/shell concerns (C4 §6).** `agentSpawnSem` acquire becomes
`ActAcquireSpawnSlot` whose result returns as EvSpawnSlotAcquired/EvSpawnSlotAborted (or
folds into ActLaunch execution) — NEVER a busy-wait state in the pure machine.

## D6 — Timers as events `[C1+C4, keeper D11]`

**Decision.** Adopt the keeper D11 timers-as-events mechanism verbatim. The reactor emits
`ActArmTimer{kind,d}` / `ActCancelTimer{kind}`; consumes `EvTimerFired{kind,at}`. The shell
owns `timers map[TimerKind]time.Time` over `ClockPort`, stamps deadlines at execution time,
runs a detection ticker + a punctual `nearestDeadline` one-shot wake, and checks
**timeout-before-read** each poll. Parent-ctx cancel maps onto the phase's timeout edge
(`fireOnCancel`) — never silent. `TimerKind`s: agent_ready, post_ready_hang, kill_reap,
resume_ready, commit_budget, heartbeat_stale, hard_ceiling. (The input-ack 30s bound is NOT
an M3 timer — it lives inside M2's `Submit` per D10; the reactor consumes `EvInputStale`
rather than arming its own `input_ack` timer.)

**Structural invariant (from keeper SR4 idiom).** Every `EvTimerFired` edge MUST land in a
state with an outgoing action (D7). Make illegal actions unconstructible — e.g. a
merge/close action cannot be built before a gate verdict is recorded in RunState (the
`injectClearAction` idiom, step.go:870).

## D7 — Bounded-liveness invariant + event reuse (attribution fix) `[C5]`

**Decision — the invariant (RSM-LIVENESS, the daemon SR9).** Normative wording (adapts
SK-INV-005): *"For every run r that emits `implementer_resumed(r,i)`, r MUST reach exactly
one terminal outcome, or emit a `run_liveness_timeout` failure-class event, within the
bounded window. A run that produces neither is a conformance failure. Silence is
FORBIDDEN."* Structurally: every `EvTimerFired` edge lands in a state with an outgoing
action, so the machine cannot wedge (D6).

**Two-tier bound (C5 Q4), constants stated:**
- **Silent-resume tier ≈ 11 min** = agentReadyTimeout(150s) + postAgentReadyHangTimeout
  (420s) + teardown(~93s). Precondition: both become REAL reactor timers whose inputs are
  **agent-derived** (relay agent_ready, worktree-HEAD/pane-output fingerprint, tool events)
  — never the daemon heartbeat — and the fixed 2s `resumeReadyFallbackGrace` caulk
  DISSOLVES into the resume_ready timer edge (arm → TimerFired → degraded-confirm-or-fail,
  not "assume ready").
- **Slow-progress tier ≈ 91.5 min** = commitHardCeiling(90min) + teardown, with the 30-min
  per-progress budget meaningful again once "progress" EXCLUDES the unconditional heartbeat.

**Root cause the fix removes (C5 Q2).** `agent_heartbeat` is a daemon-side wall-clock timer
(RunHeartbeatLoop, 300s), NOT agent-derived; it refreshes every recency detector's clock, so
run_stale (10min), heartbeat-staleness (8min), and the 30-min progress budget all never
fire on a resumed-then-silent run. **The design SPLITS "daemon goroutine alive" from "agent
making progress"** (distinct event semantics / agent-derived progress inputs). This is the
central design move and the ONE sanctioned observable divergence (a run that used to hang
now terminates or emits failure — 01-problem-space §4 constraint).

**No new M3 durable events — reuse the existing family (revised after the M2 correction).**
The resume boundary is made replay-visible by TWO fixes to what already exists, NOT by
minting new event types:
- **Fix run_id attribution on the synthetic ready.** Today the 2s-fallback `agent_ready` is
  `Emit`ed WITHOUT a run_id (reviewloop.go:660 + workloopeventsource.go:145), so the boundary
  isn't joinable and the per-dispatch reaper only fires by accident (C5 Q1#6/Q2#9). The fix
  stamps run_id (and iteration) so `agent_ready` / `agent_ready_timeout` + the terminal family
  (`run_completed`/`run_failed`/`review_loop_cycle_complete`) fully bracket the resume.
- **Consume M2's run-scoped input events.** M2 (now OWNING the input seam, D10) emits
  `agent_input_submitted` / `agent_input_acked{latency_ms}` / `agent_input_stale{bound_ms}`,
  run-scoped and `msg_id`-keyed (IN-INV-001..003). The resume SEED is submitted via
  `ActSubmitSeed` → so a stalled resume surfaces as `agent_input_stale` at M2's 30s bound —
  making the resume boundary replay-visible with ZERO new M3 events.
- Also HOME the spec-orphaned `run_stale` (cited to a non-existent event-model §8.12.1 — C5
  Q5) in the new RSM spec / event-model, and reconcile the `agentReadyTimeout` comment drift.

Rationale for reuse-over-mint: the prior design iteration and the C5 research both showed the
existing `agent_ready_timeout` + terminal family suffice once attribution is fixed; the keeper
minted interior events because its outputs weren't durable, but the daemon's already are. The
M2 correction makes this decisive — M2's input events cover the confirm/stale milestones a new
`run_resume_confirmed`/`run_liveness_timeout` would have added. (If a future gap needs a
dedicated daemon liveness event, it is an additive follow-on, not an M3 blocker.)

**Fail-closed (C5 Q7).** On liveness-timeout: kill → reopen, riding the existing
`ReviewLoopFailures` budget for anti-thrash (every daemon timeout path is fail-closed today;
proceeding past an unconfirmed resume would dispatch a reviewer against an unverified
worktree, violating EM-015d). NOT fail-open (unlike the keeper's model-done bound).

**Test (RSM DoD 3).** A finalizing replay checker keyed per run_id (the `SR9Checker`
pattern, `internal/replay/checkers.go:149`: unterminated-N MUST be 0) + a
FaultStall-on-resume-launch fault-injection test over FakeClock + an N=10 clean-relaunch
out-of-band oracle (jq/grep over events.jsonl — the machine does not grade itself).

## D8 — Terminal-spine parameterization `[C6]`

**Decision.** Collapse the four merge/close blocks (+ two merge-less close blocks + the 6×
close ladder) into ONE factored terminal spine, realised as the reactor's
`Gating→Merging→{Closed|Reopened}` edges + a single shell close-ladder effector. Every real
divergence from the C6 divergence table survives as an explicit parameter:
`label`, `successSummary`, `gateRunner` (nil=skip), `doPreMergeSync bool`, `trailerVerdict`
+ re-amend-on-retry flag, `mergeRetries int`, `allowRebaseDroppedFallthrough bool`,
`ctx` (per-run vs background), `emitOutcome bool`, `needsAttention bool`, close/reopen reason
templates.

**Non-collapsible cases the spine keeps distinct:**
- **exit-0 auto-close is NOT redundant** with agent_completed (C6 Q2): it is THE terminal
  path for `CompletionProcessExit` harnesses (codex/pi have no claude stop hook). Same
  spine body, different (guard, label, summary).
- **shutdown-drain stays a distinct terminal edge** (C6 Q3): `context.Background()`, no
  gate, no preMergeSync, no outcome_emitted, direct emitRunCompleted (bypasses emitDone),
  reopen-reason substitution (QM-002a recovery).

**Eliminate `runSucceeded *bool`.** Success becomes a terminal RunState the shell reads
after the reactor returns (the three current out-param consumers —
`evaluateGroupAdvanceWithOutcome`, `stagedBeadGeneratorEval`, the two Pi-retention defers —
read it from RunState). `emitDone` becomes a per-run terminal-emitter method.

**ctx-normalization is an explicit decision, not silent (C6 Risk).** Terminal-block reopens
currently no-op silently if the per-run ctx was cancelled mid-merge; unifying to Background
(per hk-e3fy) is a behavior change — the design chooses Background for terminal reopens and
records it as a deliberate, tested change.

## D9 — Parity harness (reproduce-behavior-first) `[C4 §8]`

**Decision.** Follow the keeper D13 lineage: (1) extract a per-run_id recorded-run corpus
from the durable event log (`harmonik subscribe --json` is the sanctioned reader — NOT
hand-grep); (2) a run-outcome **stimulus synthesizer** over the generic `substrate.Twin[E]`
+ `FaultConfig` (the daemon already emits a rich durable stream, so the synthesizer works
BEFORE M2 lands — decoupling parity from the M2-1 wait); (3) an **old-vs-new differential**
(old `beadRunOne` + new `runexec` reactor over the same schedules; diff terminal
type/reason/interior order/bounded-termination; allowlist = only the sanctioned SR9-class
change; scaffold deleted when old code is deleted); (4) permanent L0-L3 tiers + frozen
anchors + the N=10 oracle. The extraction lands "small state-transition-at-a-time" (RSM
constraint) with the differential harness in place BEFORE the first transition (unlike
keeper, where T9-T14 followed T7 — C4 Risk 7).

Reuse in-tree: `internal/replay` (T4 checkers incl. SR9 shape), `internal/substrate`
Twin + FaultStall, the keeper's `keepertest` L0-L3 taxonomy as the template. Report any
`substrate.EventSource`/`Effector` genericization gap UPSTREAM (multi-source mux;
action-result-as-event) — do NOT fork the seam (C4 §2 gaps, 02-components §C4(e)).

---

## D10 — The M2→M3 input/ack seam: the reactor CONSUMES M2-1 `[C4 §7 — STANDALONE, QUOTABLE]`

> **Ownership (corrected by the planner, 2026-07-14).** M2-1 (`agent-input-substrate`,
> landed at `ready`) **OWNS** the seam input/ack contract; M3-4 (this reactor) **CONSUMES**
> it (ROADMAP line 84: "M3-4 reactor `Step` → M2-1 seam input/ack contract"; M4 dep list
> line 78). M3 does NOT define a competing input/ack type. This section pins how the runexec
> reactor consumes M2's **already-ratified** surface (M2 `05-spec-drafts/agent-input.md`,
> IN-001..IN-013 + IN-INV-001..003). M2 named this M3-side vocabulary explicitly and left
> the surface "for M3 to veto" (agent-input.md line 140-142); anything the reactor genuinely
> cannot consume is flagged `PLANNER-RECONCILE:` below, not silently diverged.

**The fixed seam M3 consumes (M2 owns these, verbatim):**
- `handler.SubstrateSession.Submit(ctx, InputMsg) (InputAck, error)` (IN-001). Returns nil
  error ONLY with a real ack (IN-003); no input channel ⇒ `ErrInputUnsupported`.
- Types `InputMsg{MsgID, Kind, Body}` and `InputAck`; sentinels `ErrInputUnsupported`,
  `ErrInputStale` (M2 §1, IN-001/IN-003). `MsgID` is the idempotency key (IN-004).
- **Ack** = the first turn-opening output frame after write-flush (IN-003) — NOT process
  spawn, NOT tmux exit-0. Bound = `input_ack_timeout` (default 30s, ClockPort-timed):
  `Submit` resolves ack / `ErrInputStale` / transport-error — **ack-or-stale, never silence**
  (IN-INV-001, the RS-INV-003 instantiation).
- Serialization: at most ONE uncorrelated input in flight per session; concurrent `Submit`
  queues (IN-004). tmux is observation-only on the structured path (IN-006).
- Durable events M2 emits, which the reactor observes on the bus: `agent_input_submitted`,
  `agent_input_acked{latency_ms}`, `agent_input_stale{bound_ms}` — run-scoped, `msg_id`-keyed
  (IN-INV-001/002/003).

**How the runexec reactor consumes it (M3 owns this side):**

1. **Actions** — the reactor emits `ActSubmitSeed` / `ActSubmitBrief` (M2's named vocabulary,
   agent-input.md:141), each carrying an `InputMsg{MsgID, Kind, Body}`. The **shell**
   effector calls `Submit(ctx, InputMsg)`; M3 does NOT reimplement Submit or its bound.
2. **Events** — the shell converts the `Submit` return (and/or the run-scoped
   `agent_input_acked`/`agent_input_stale` bus events) into reactor events `EvInputAcked{msgID,
   latencyMs}` / `EvInputStale{msgID, boundMs}` (M2 names them `InputAcked`/`InputStale`,
   agent-input.md:142). `ErrInputUnsupported` from a tmux/IN-007 session maps to a distinct
   `EvInputUnsupported{msgID}` edge (the reactor falls back to the observation-only path).
   The pure `Step` never blocks on `Submit`.
3. **Correlation is by `MsgID`, NOT an M3 seq.** The reactor keys in-flight inputs on the
   `MsgID` it minted for each `ActSubmit*` (IN-004 idempotency); duplicate/late
   `EvInputAcked` for an already-correlated `MsgID` is dropped by `Step` (IN-INV-002). (The
   reactor's own event stream may still carry a monotonic `Seq` for general dedup — see D5 —
   but input correlation uses `MsgID`, per M2.)
4. **Bounded liveness is M2's IN-INV-001, consumed as an event by M3.** The reactor does NOT
   arm its own `input_ack` timer — the 30s bound lives inside `Submit`/the driver (M2, IN-003,
   ClockPort-timed). The reactor simply requires that an `ActSubmit*` is eventually followed
   by exactly one of `EvInputAcked` / `EvInputStale` / a driver-terminal event, and that each
   such event lands in a state with an outgoing action (this composes M2's per-input
   IN-INV-001 into M3's per-run RSM-LIVENESS / D7: an `EvInputStale` on the resume seed is a
   first-class input to the run's bounded-liveness edge — a stale seed feeds the resume-hang
   fail-closed path, never silence).

**Seam boundary (unchanged principle).** M2 owns the wire + the ack-producing `Submit` + the
per-input 30s bound (retiring the no-op `SendInput` at `internal/handler/substrate.go:140`
and `CloseStdin` at `:173`, and building the structured driver — M2 D1/D2). M3 owns the
reactor that (a) mints `InputMsg`s and emits `ActSubmit*`, (b) consumes `InputAcked/Stale`
events, and (c) routes a stale seed into the run-level liveness edge. The `Submit` and its
bound are a **fixed dependency**, not an M3 deliverable.

**Veto check (M3 → M2, evidence-based).** Reviewing M2's surface against the reactor's needs:
the `Submit(ctx, InputMsg) (InputAck, error)` synchronous shape composes cleanly with the
shell-converts-to-event pattern (the shell already runs `Submit` inside an effector and feeds
the result — exactly the keeper pattern). `MsgID` idempotency subsumes the reactor's
correlation need. `ErrInputStale` at 30s is the resume-seed liveness signal C5 wants. **No
field or signature is un-consumable; no veto is raised.** One coordination note is carried
below.

**PLANNER-RECONCILE (D10, one coordination note — not a veto).** M2's per-input bound is a
**fixed 30s** `input_ack_timeout` (IN-003, OQ-IN-002 "confirm against spike latencies"). M3's
resume-hang liveness (D7) is a two-tier **per-run** bound (~11 min silent-resume / ~91.5 min
absolute). These COMPOSE (a 30s stale-seed event is one input to the 11-min run edge), but
the resume seed is submitted via `ActSubmitSeed` → so a stalled resume now surfaces as an
`EvInputStale` at 30s, MUCH earlier than the 11-min tier. That is STRICTLY BETTER (faster
fail-closed), but it means the dominant resume-hang detector post-M2 is M2's IN-INV-001 at
30s, not M3's 11-min timer — the 11-min tier becomes the backstop for the non-seed silent
paths (agent goes dark AFTER a successful ack). The planner should confirm the two bounds are
intended to stack this way (M2-30s-primary, M3-11min-backstop) and that the C5 property test
asserts BOTH layers.

---

## PLANNER-RECONCILE — consolidated list (also in each design doc inline)

1. **[D2 / C1]** C1's ClockPort scope is FIVE run-path files (workloop, reviewloop,
   agentready, postreadyhang, pasteinject), broader than 02-components' 26-site
   workloop-only estimate. Enlarges C1 blast radius; required for C5 determinism.
2. **[D3 / C2]** The merge critical section keeps `git push` INSIDE the window; making push
   async (window ends at local update-ref) is an M4-class semantic change, not a C2 win.
   A reviewer expecting push fully removed should note this boundary.
3. **[D4 / C3]** Eight ports, not the dossier's six (adds WorkerPort + GatePort; corrects
   two mis-assigned fields; deletes two dead fields). The LaunchPort-vs-AgentWaitPort split
   is left as ONE LaunchPort unless the planner directs otherwise.
4. **[D7 / C5]** The liveness fix SPLITS the daemon heartbeat semantics (agent_heartbeat ≠
   agent progress) and changes an observable (hang → terminate) — the ONE sanctioned
   divergence. It also touches the hk-nvjk-motivated false-stale suppression and the
   accidental <30-min reaper bound (the run_id-less fallback agent_ready emit). Confirm this
   is the intended M3 scope for the absorbed STEP-0a fix (ROADMAP says yes).
5. **[D10 / C4]** OWNERSHIP CORRECTED (planner, 2026-07-14): M2-1 OWNS the input/ack seam;
   M3-4 CONSUMES it. D10 rewritten to consume M2's ratified `Submit(ctx, InputMsg)
   (InputAck, error)` surface (IN-001..IN-INV-003) — no competing M3 type. No veto raised.
   One coordination note: M2's fixed 30s `input_ack_timeout` becomes the PRIMARY resume-hang
   detector (stale seed at 30s), with M3's two-tier per-run liveness (D7, ~11min/~91.5min) as
   the backstop for post-ack silent paths. Confirm the two bounds are intended to stack, and
   that the C5 property test asserts both layers.
