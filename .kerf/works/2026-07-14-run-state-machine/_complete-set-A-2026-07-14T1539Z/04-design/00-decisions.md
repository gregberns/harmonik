# 04-Design / 00 — Cross-cutting decisions (the pinned contracts)

> **Pass 4 (Change Design), `run-state-machine` (M3).** Synthesis of the four
> research findings (`03-research/{merge-queue,runexec,workloop-ports,liveness}/`)
> against `02-components.md`, mirroring the P1 template
> (`.kerf/works/session-restart-substrate/04-design/00-decisions.md`). This doc
> SETTLES every cross-component decision; the component design docs elaborate
> within these pins and MUST NOT contradict them. Autonomous mode (signoffs
> waived, 2026-07-14). The design template is the LANDED keeper reactor:
> `internal/keeper/step.go` (pure Step) + `internal/keeper/shell.go` (effector +
> drive loop) + `internal/substrate` (seam + ClockPort) — cited per decision.

---

## M3-D1 — Package placement: pure core in top-level `internal/runexec`; shell stays in `internal/daemon`

**Decision:** The pure state machines + Event/Action vocabulary live in a NEW
top-level package **`internal/runexec`** (NOT `internal/daemon/runexec`). The
imperative shell — the effector, the drive loop, and every git/tmux/br/SSH side
effect — stays in `internal/daemon` (where `beadRunOne` shrinks to the thin
driver). The merge queue is a second new leaf package **`internal/mergeq`**
(serialization mechanism only; the git work it serialises is daemon-supplied
closures).

**Why:** Track C pre-authorized exactly this shape — the depguard rule for
`**/internal/runexec/**` with `deny: internal/daemon` is written and waiting
(`plans/2026-07-13-code-revamp/track-c-enforcement.md` §2.2 / TASKS.md TC-6):
"runexec is functional core; the daemon shell drives it — logic must not leak
back." A sub-package under `internal/daemon/` would defeat the direction lock.
This mirrors keeper exactly: `internal/keeper` (core+shell, deny daemon) driven
from outside; here the shell must live daemon-side because the effects it
executes (merge helpers, reviewloop/dot sub-drivers, worker SSH) remain daemon
code during M3. `internal/mergeq` gets the same inverse-edge deny (a new depguard
rule following the TC-6 pattern — "applied per carve" per track-c §2.2).
Allow-set for runexec starts minimal: `$gostd` + `internal/core` +
`internal/substrate` (+ self), trimmed from TC-6's provisional list.

**Consequence:** runexec declares NO ports — a pure `Step` needs none (all IO is
shell-side, keeper step.go precedent: "no IO, no clock reads, no id minting").
The C3 ports are a **daemon-internal** reorganization of what the shell consumes
(M3-D6).

## M3-D2 — Two pure machines, one vocabulary: `Dispatch` (per agent session) + the `Run` spine

**Decision:** `internal/runexec` defines TWO pure machines sharing ONE flat
`Event`/`Action` vocabulary (both drivable by `substrate.Run[runexec.Event,
runexec.Action]`):

1. **`Dispatch`** — one agent session: `Launching → AwaitingReady → Briefing →
   Working → {Completed | Exited | Stalled | ReadyTimeout | Aborted}`. This is the
   direct daemon analog of the keeper `Cycle` (step.go): timers-as-events, every
   TimerFired edge lands in a state with an outgoing action. It is instantiated
   per agent dispatch: once for single-mode, per implementer/reviewer iteration in
   the review loop (including every RESUME — where SR9's daemon peer lives), and
   per agentic DOT node.
2. **`Run`** — the run-level spine: `Resolving → Provisioning → Dispatching →
   Gating → Merging → Finalizing → Done{closed|reopened}` with the workflow-mode
   fork (`review_loop`/`dot`/`single`) recorded as state, the iteration counter as
   state, and the FOUR open-coded terminal blocks collapsed into its
   Gating→Merging→Finalizing tail (M3-D9).

**Why two, not one:** the daemon run genuinely contains N sequential agent
dispatches (research runexec §2 P8; review-loop iterations; DOT nodes) — folding
them into one flat machine triplicates the launch/ready/work segment per mode,
which is exactly the 4×-duplication disease M3 exists to cure. The keeper is one
machine because a keeper cycle has one "session interaction"; the honest daemon
shape is a reusable dispatch segment. Both machines mirror keeper mechanics
verbatim: pure total `Step(state, Event) → (state, []Action)`, `State()`,
`InFlight()`, timestamps only from event `At` fields, ids minted by the shell.

**Scope cut (green-incremental, census "small state-transition-at-a-time PRs"):**
- Single-mode: fully reactor-driven (both machines).
- Review-loop: the per-iteration implementer/reviewer **launch→ready→brief→work
  segments** are re-driven over `Dispatch` (this is what structurally kills the
  resume caulk); the iteration CONTROL logic (verdict parsing, no-progress
  hashes, failure budget) remains sequential shell code in M3, frozen
  byte-compatible, feeding `Run` events (`EvModeOutcome`).
- DOT: the cascade remains the shell sub-driver it is; its agentic-node
  dispatches go through `Dispatch`, and its terminal feeds the same `Run` tail.
  Full cascade reactorization is OUT (M5-adjacent follow-on).

## M3-D3 — Timers are events (keeper D11, verbatim mechanics)

**Decision:** The reactor emits `ActArmTimer{kind,d}` / `ActCancelTimer{kind}`
and consumes `EvTimerFired{kind}`; the shell owns `ClockPort` deadlines plus the
per-run drive loop (select over tap events / watcher-done / nearest-deadline wake
/ ctx.Done), converting each arrival into a reactor event — the exact
`internal/keeper/shell.go` `drive`/`pollOnce`/`nearestDeadline` shape, cited as
the template.

**Timer kinds (values = today's constants, unchanged):**
`worker_socket_ready` (10s, reversetunnel.go:78) · `agent_ready`
(effectiveAgentReadyTimeout 150s/210s, agentready.go:64/:80) · `ready_kill_reap`
(10s, workloop.go:141) · `input_ack` (NEW — M3-D11; default provisional 30s,
tmux-transitional impl acks synthetically) · `post_ready_hang` (7m, exec path
only, postreadyhang.go:37) · `stop_hook_grace` (3s, waitsocketgrace.go:40).
`ctx.Done()` maps onto phase-appropriate timeout edges exactly like keeper
`fireOnCancel`.

**Deliberate phasing (the daemon analog of D11's reproduce-the-freeze
pragmatism):** the commit watchdog (`pasteInjectQuitOnCommit`, pasteinject.go:829
— 500ms poll, 30m progress budget, 90m hard ceiling, 8m heartbeat staleness) is
NOT dissolved into reactor timers in M3. It remains a shell event source whose
outputs become first-class events (`EvCommitObserved`, `EvNoChangeTimeout`,
`EvHeartbeatStale`); its internal logic is behavior-frozen. Dissolving it is a
later, separately-measured change. Rationale: it is 600+ lines of
hysteresis-laden policy with its own bead history (hk-37giq, hk-jgxqc); freezing
it keeps the parity envelope small, exactly as the keeper froze InCycle.

## M3-D4 — ClockPort: reuse `substrate.ClockPort`; run-path sites only

**Decision:** Add `clock substrate.ClockPort` to the daemon (a `workLoopDeps`
field, wired `substrate.SystemClock{}` in `newWorkLoopDeps`, `FakeClock` in
tests) and migrate ONLY the run-path wall-clock sites (the census in
workloop-ports findings §4: 8 workloop.go + 8 reviewloop.go + 7 dot_cascade.go
sites; the five `time.After` selects become shell timer deadlines). The ~23
OUTER-loop sites stay on `time.*` — they are M5's.

**Why:** identical to P1 D4 — zero new abstraction, the port is landed and green
(`internal/substrate/clock.go:10`), and C5's deterministic fault tests are
impossible against the wall clock. Research confirmed there is NO existing daemon
clock seam to reconcile (PLANNING-LOG Fable-verified: zero daemon ClockPort).

## M3-D5 — The merge queue: strict-FIFO serial executor; build/rebase/fmt OUT, ref-commit IN

**Decision:** `internal/mergeq` provides a strict-FIFO serial executor
(goroutine + submission channel — not a bare mutex) with
`Submit(ctx, label, critical func(context.Context) error) error`. ONE instance
per daemon serialises THREE kinds of critical sections (preserving today's single
exclusion domain):
1. **Merge-commit sections** — the re-validate → FF-check → update-ref → push →
   rollback → restore/reset-hard → br-sync tail of the merge (research
   merge-queue §7).
2. **The escape-worktree check** (hk-zguy6) — stays in the SAME domain so no
   sibling can be mid-ref-window during the read (research §4b).
3. **The remote base-sync + worktree-add section** (hk-lt091/hk-h8u7p) — kept in
   the domain unchanged for M3; M4 re-homes it (research §4a; explicitly NOT
   silently dropped).

`mergeRunBranchToMain` splits into **`prepareMerge`** (worktree-local, runs
OUTSIDE the queue, speculative against an observed mainTip: churn-discard,
residual-commit, clean, `git rebase`, strip-run-context, `go build`, `go vet`,
gofumpt/gci incl. auto-commit — everything Dir=wtPath/buildDir) and
**`commitMerge`** (inside the queue: re-resolve mainTip; if it moved since
prepare → return `ErrStale` and the caller re-prepares; FF-check; update-ref;
push; rollback pairing; reset-hard; br sync). The existing 3-attempt loop becomes
the prepare↔commit retry cycle (same max attempts, same retryable-reason
strings).

**Deliberate deviation from the problem-space goal's literal wording (record
prominently):** problem-space §2/§5.4 said `git push` and `br sync` move "outside
any global daemon lock". The research (merge-queue §2, §7) proves update-ref ↔
push ↔ rollback form a **rollback-paired atomic unit** (`:6821`/`:6895`/`:6906`)
and reset-hard/br-sync mutate the main checkout inside the escape-check window
(hk-zguy6) — splitting them creates a rollback-over-advanced-base hazard and
regresses the escape invariant. The REAL goal — no build/vet/rebase/fmt under a
daemon-wide lock (the minutes-long pole) — is fully achieved; push/reset/br-sync
(seconds) stay serialised because correctness requires it. Decompose open
question (b) asked exactly this; this settles it. The DoD-2 mechanical check
becomes: **a test fails if `go build`/`go vet`/`gofumpt`/`gci`/`git rebase`
execute inside the queue's critical section** (asserted via a recording
CommandRunner seam in mergeq tests), plus an enumerated-IO allowlist for the
critical section. Flagged to the planner as an FYI reconcile item (not a fork —
it is the design-pass answer to the question the decompose deferred).

**Also pinned:** submissions accept a caller-supplied ctx (the shutdown-drain
site uses `bgCtx`, research §3); FIFO across ALL named queues preserves hk-yyso7
single-writer; queue observability is slog-only in M3 (NO new durable event —
keeps the event-model untouched, M3-D10).

## M3-D6 — Ports: daemon-internal `RunPorts` + `RunEnv` + `SharedHandles`; runexec stays port-free

**Decision:** C3 is a daemon-internal reorganization: `beadRunOne`'s 17-param /
85-field surface becomes three explicit bundles defined in
`internal/daemon/runports.go`:
- **`RunPorts`** (narrow interfaces, keeper ports.go idiom): `LedgerPort`
  (brAdapter ops + closeBeadWithHistoryTrim + brTimeoutCfg/intentLogDir/tidGen
  co-travel), `EmitterPort` (= `handlercontract.EventEmitter` subset via type
  alias — keeper ports.go:107 precedent), `WorktreePort` (worktreeFactory +
  create-mutex + base-sync), `MergePort` (the `mergeq` handle + prepare/commit
  fns), `LaunchPort` (launchSpecBuilder/substrate/harnessRegistry/
  adapterRegistry/hookStore/binaries/env/timeouts/sandboxCfg), `GatePort`
  (cpRegistry — dot_gate only), `Clock substrate.ClockPort`.
- **`RunEnv`** (immutable per-run values): projectDir, targetBranch,
  protectBranches, allowedRepos, workflowModeDefault, defaultHarness, projectCfg,
  brPath, runID, beadRecord, queue identifiers, item overrides.
- **`SharedHandles`** (shared-by-reference concurrency state): runRegistry,
  localInFlight, agentSpawnSem, workerRegistry, and a one-method **`BudgetPort`**
  wrapping the queueStore's review-loop-failure budget mutation (the ONLY run-path
  queueStore use, workloop.go:3947–:3978).

**Pinned exclusions (research workloop-ports):** the 2 DEAD fields (`h`,
`beadAuditLogger`) are DELETED, not ported; `staleBlockerCloser` stays outer-loop;
the 4 loop-owned value fields (lastCoordinatorReap/lastDiskCheck/lastGoCacheClean/
diskLow) are lifted out of the by-value bundle into loop-owned state. The
remaining ~47 OUTER fields stay on `workLoopDeps` untouched — that struct
survives M3 for the outer loop; its full decomposition is M5. Nil-means-disabled
fallbacks keep their exact semantics (ports may be nil where deps were nil;
adapters preserve the defaulting).

## M3-D7 — SR9's daemon peer: the resume bound is STRUCTURAL, on `Dispatch`

**Decision:** The 2s `resumeReadyFallbackGrace` caulk (reviewloop.go:110) is
DELETED and replaced by the `Dispatch` machine's structure: every dispatch —
fresh or resume, builtin review-loop or DOT — arms `TimerAgentReady` on
`Launching→AwaitingReady`; the shell feeds `EvAgentReady` from BOTH the relay
callback AND (transitional, until M2) a readiness probe; the timeout edge is
`AwaitingReady --TimerFired(agent_ready)--> ReadyTimeout` which EMITS
`agent_ready_timeout` (the EXISTING event, workloop.go:7961) + kills + reopens —
an outgoing action, never silence. Because DOT dispatches ride the same machine,
**DOT gains the bound the caulk never gave it** (research liveness §3 — the DOT
resume path has NO caulk today); this is allowlisted as part of the SR9 fix
class.

**Two defect fixes folded in (both replay-joinability, research liveness §2/§7):**
(1) the transitional synthetic ready emit is stamped **with the run_id** (today's
fallback emits without it, workloopeventsource.go:145–:161 — a replay blind
spot); (2) therefore `agentReadySeenSinceLastLaunch` tracking becomes accurate.

**The invariant (normative, spec §5):** for every run, `implementer_resumed(run)`
MUST be followed within the bounded window by exactly one run-correlated terminal
(`review_loop_cycle_complete` / `run_completed` / `run_failed`) or a
failure-class event (`agent_ready_timeout` / `run_stale`). Window derivation
(from the real constants, liveness findings §6): ready segment ≤
`effectiveAgentReadyTimeout` (150s/210s); working segment bounded by the frozen
watchdog's `commitHardCeiling` (90m absolute). The structural rule (SK-INV-005
verbatim shape): every `TimerFired` edge lands in a state with an outgoing
action. Checker: a run_id-keyed `Finalize`-style checker mirroring `SR9Checker`
(`internal/replay/checkers.go:149–:194`).

**Honesty note carried from research:** the census's "in_progress forever" is
not literally true on the current tree (three uncoordinated bounds exist); the
C5 deliverable is replacing caulks with structure + closing the DOT gap + making
the boundary replay-visible. Stated as-is in the spec.

## M3-D8 — `emitDone` and `runSucceeded *bool` dissolve into terminal state

**Decision:** Success becomes `Run` terminal state (`Done{success, summary,
runTipSHA}`), read by the goroutine wrapper after the drive loop returns —
eliminating the out-param and the 40-call-site closure. The wrapper's post-run
duties (EM-015f group-advance with the outcome, hk-f722 flywheel eval) read the
machine's terminal. `emitRunCompleted` becomes `ActEmitRunTerminal{success,
summary}` executed by the effector with the hk-e3fy ctx-swap policy
(Background when cancelled) preserved in the effector — failure policy lives
shell-side exactly like keeper shell.go's per-action `_ =` table.
`sessiondata.Collect` stays a shell effect (detached goroutine, best-effort,
skipped on the shutdown-drain path — byte-compatible with today's bypass).

## M3-D9 — Terminal spine: ONE close ladder, four entry conditions preserved

**Decision:** The 6×-duplicated close ladder (research runexec §6) becomes ONE
`Run` tail: `Gating → Merging → Finalizing`, entered by four distinct events
(`EvModeOutcome{review_loop}`, `EvModeOutcome{dot}`, `EvAgentCompleted`,
`EvCleanExit`) plus the two no-merge variants (`subsumed`, `noChange`). The
ENTRY CONDITIONS stay distinct (they are real, different triggers); the BLOCK
unifies. Exit-0 auto-close (decompose C6 Q(a)): research proves C vs D are
near-byte-identical with only summary-string labels — both become the same spine
with the label carried as event data (exact strings preserved for parity).
Shutdown-drain (Q(b)) stays a DISTINCT terminal edge (`EvShutdownDrain`): bgCtx
execution policy + no sessiondata + direct emitRunCompleted are effector policy
for that action batch. The review-loop merge-retry (×2, retryable reasons) and
the DOT `alreadyApprovedOnMain` carve-out become explicit transitions, not
copies. The `hclifecycle.Machine` (decompose C4 Q(c)) is KEPT as a downstream
projection: `ActDriveLifecycleTerminated` wraps today's `transitionToTerminated`
(workloop.go:7994) — HC-065 ownership does not move in M3.

## M3-D10 — Event-model touch: ZERO new event types

**Decision:** M3 registers NO new durable event types. The C5 bound reuses
`agent_ready_timeout` (registered, emitted today at workloop.go:4906) and the
existing terminal family; the queue emits no durable event (slog only); parity
demands the existing per-run event vocabulary unchanged. The only event-stream
divergences are the allowlist (M3-D12). This keeps `specs/event-model.md`
untouched by M3 — one spec lands (M3-D13), no EV amendment.

## M3-D11 — The M3-4 → M2-1 reactor-Step input/ack contract (the owned edge)

**Decision (the contract M2-1 implements against; stated normatively in the RX
spec §6):** the `Dispatch` machine's input surface toward the agent channel is:

```
Actions (reactor → driver, via the shell effector):
  ActLaunchAgent   {SessionRef, SpecRef}          -- start or resume a session
  ActDeliverInput  {SessionRef, InputID, Kind, Payload}
                                                  -- Kind ∈ {brief, resume_prompt, quit}
  ActKillAgent     {SessionRef}
  ActArmTimer/ActCancelTimer {input_ack | agent_ready | ...}

Events (driver → reactor, via the shell event loop):
  EvAgentReady     {SessionRef, At}               -- REQUIRED on fresh start AND reattach/resume
  EvInputAck       {SessionRef, InputID, At}      -- "agent ACCEPTED this input for processing"
  EvInputRejected  {SessionRef, InputID, Reason, At}
  EvHeartbeat      {SessionRef, At}
  EvAgentExited    {SessionRef, ExitCode, At}
  EvOutcomeReceived{SessionRef, Outcome, At}      -- stop-hook socket outcome
  EvTimerFired     {Kind, At}
```

**Normative clauses M2's driver must satisfy:**
1. **Ack-or-fail, never silence (M2's SR9 analog):** every `ActDeliverInput`
   MUST resolve to exactly one of `EvInputAck` / `EvInputRejected` /
   `EvTimerFired(input_ack)`; the reactor arms `TimerInputAck` with every
   delivery, and that timeout edge has an outgoing action (retry policy or
   dispatch failure) — the machine cannot wedge on delivery.
2. **Ack semantics:** acceptance means "the agent harness accepted the input for
   processing" (protocol-level), NOT "bytes queued in a pane buffer". The
   tmux-transitional shell may approximate (synthetic ack after paste-verify);
   M2's structured driver provides real acks. The reactor is agnostic — it sees
   only the event.
3. **Ready on resume:** `EvAgentReady` MUST be delivered on every session start
   AND every reattach/resume. If the channel cannot observe readiness, the
   DRIVER owns a bounded fallback and must still deliver `EvAgentReady` or
   `EvAgentExited` before `TimerAgentReady` fires; the reactor's timeout edge is
   the backstop failure, never a hang.
4. **Sync-vs-async resolution (M2 C1's open question, settled here):** the
   reactor-facing ack is ALWAYS an awaited event (D11 timers-as-events shape).
   M2 may implement its port as a synchronous `Submit(ctx, payload) (Ack, error)`
   OR a stream — the daemon shell adapts either into `EvInputAck`/`EvInputRejected`
   (a sync return becomes an immediate event). M2 is free on the transport shape;
   the EVENT contract is fixed.
5. **Ordering:** per-session events are delivered in observation order; acks may
   interleave with heartbeats; `InputID` is reactor-minted (shell-supplied,
   keeper cycle-id precedent) and opaque to the driver.

**Why this direction:** ROADMAP pins M3-4 → M2-1 as the single M3→M2 edge and
B2 of REVIEW-FINDINGS asked M3's design to confirm it. The contract above is
what the reactor NEEDS; it deliberately over-constrains nothing about M2's wire
protocol. If M2's design finds a genuine impossibility (e.g. a harness that
cannot distinguish accept from reject), that is the PLANNER-RECONCILE trigger —
nothing else here should need renegotiation.

## M3-D12 — Parity: regression net + event-stream goldens + explicit allowlist (D13-adapted)

**Decision:** The keeper's old-vs-new differential (D13) ran BOTH
implementations purely; `beadRunOne` cannot run without IO, so the daemon parity
plan substitutes: (1) the ~466 hk- regression net green per commit (the primary
envelope, census); (2) **L1 event-stream goldens** — per-run event sequences
extracted from the existing `events.jsonl` baseline (run_id-keyed, EventID-
sorted) replayed as reactor stimulus schedules with golden action sequences;
(3) the M1-5 coverage audit floor on the extracted files. **Permitted-divergence
allowlist (everything else is a defect):** (a) the SR9 resume fix incl. the DOT
bound gain; (b) run_id on the synthetic ready emit; (c) the escape-check
transient window SHRINKS (build no longer inside the update-ref→reset window);
(d) merge build failures no longer transiently advance the target ref (same
events emitted, different transient ref state). Exact summary/reason strings,
event names, and bead transitions are otherwise byte-compatible.

## M3-D13 — Spec: `specs/run-state-machine.md`, prefix **RX**

**Decision:** ONE new spec, spec-id `run-state-machine`, requirement prefix
**RX** (verified free in `specs/_registry.yaml` — no RX/RM entry). It owns: the
two machines + vocabulary (RX-001..), timers-as-events, the merge-queue contract
incl. the critical-section enumeration + the no-build-under-lock invariant, the
port bundles (informative — daemon-internal), and the invariants RX-INV-001..005
(ack liveness, ready-or-fail, resume bounded liveness = the SR9 peer citing
SK-INV-005, terminal exclusivity + single spine, merge critical-section purity).
It depends-on `replay-substrate.md` (seam + ClockPort, required by reference —
never redefined) and cites `session-keeper.md` SK-INV-005 as the template
lineage. `event-model.md` is NOT amended (M3-D10).

## M3-D14 — Measurement: run corpus + fault matrix + N=10 relaunch oracle

**Decision:** (1) Build a **run replay corpus** from the frozen events.jsonl
baseline: per-run_id event streams + a `summary.json` golden (terminal type,
outcome, mode, iteration count), extracted EventID-sorted — the daemon peer of
P1's 507-cycle corpus (D13 shape). (2) Extend `internal/replay` with a
**run_id-keyed state track** (additive; today it is (agent_name, cycle_id)-keyed
— an upstream extension, NOT a daemon-local fork, per the reuse constraint) and
an `RX9Checker` mirroring `SR9Checker.Finalize`. (3) **Fault matrix** over
`substrate.Twin` fault modes against synthesized dispatch schedules —
FaultStall after `implementer_resumed` is the headline cell (stalled agent on
relaunch): assert a terminal-or-failure event within the virtual-time window,
never silence. (4) The census M3 DoD oracle: ONE deterministic fault-injection
test + **N=10 clean relaunch cycles** green, all out-of-band (`go test`/jq —
no daemon pipeline). Coverage floor: the M1-5 audit value, measured & recorded.

---

## Decision → requirement traceability

| Decision | Satisfies (02-components / problem-space §5) | Component doc |
|---|---|---|
| M3-D1 packages+depguard | §5.5 (depguard), C4 | runexec, merge-queue |
| M3-D2 two machines | §5.1 named states, §5.2 thin driver | runexec |
| M3-D3 timers-as-events | C4 open-q (c), C1 (c) | runexec, ports |
| M3-D4 ClockPort | C1 (a)(b)(c) | ports |
| M3-D5 merge queue | §5.4, C2 (a)–(e), M4 prereq | merge-queue |
| M3-D6 ports | §5.2, C3 (a)–(d) | ports |
| M3-D7 resume bound | §5.3, C5 (a)–(d) | liveness-parity |
| M3-D8 emitDone dissolve | §5.2, C6 (c) | runexec |
| M3-D9 terminal spine | §5.1, C6 (a)(b) | runexec |
| M3-D10 zero new events | problem-space §6 "minimal event touch" | liveness-parity |
| M3-D11 M2 contract | ROADMAP M3-4→M2-1 edge | runexec §6 |
| M3-D12 parity plan | Constraint "observable behavior unchanged" | liveness-parity |
| M3-D13 RX spec | problem-space §6 specs | (spec draft) |
| M3-D14 measurement | §5.3, §5.6, census DoD | liveness-parity |

Constraints held: regression net green per increment; single-writer daemon core;
out-of-pipeline (no daemon); daemon owns terminal bead transitions (LedgerPort
factors, ownership unmoved); seam reused not forked (M3-D4, M3-D14's replay
extension is upstream-additive); M1→M3 coverage-audit gate consumed (M3-D14).

## Open items explicitly deferred (NOT blocking this design)
- Full watchdog (`pasteInjectQuitOnCommit`) dissolution into reactor timers — M3-D3.
- Full DOT-cascade + review-loop-control reactorization — M3-D2 (M5-adjacent).
- Region A (remote fetch+create) re-homing out of the merge exclusion domain — M4 (M3-D5).
- `workLoopDeps` outer-loop remainder (~47 fields) — M5 (M3-D6).
- A durable merge-queue observability event — deferred (M3-D5/M3-D10).
- `cacheReapMu` rework — out of scope (problem-space §3).
