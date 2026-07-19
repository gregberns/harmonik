# 03-Research / C4 — the runexec reactor (the Step state machine)

> Pass 3 (Research), component C4. All `file:line` verified against the current
> working tree (branch `phase1-session-restart-substrate`, 2026-07-14). `beadRunOne`
> = workloop.go:3072-~5447 (next func isWatcherErrCanceled :5448), ~2,376 lines.
> Delegated to a fresh-context sub-agent; parent owns this write. Contains the
> CRITICAL M3->M2 contract-surface research (the M3-4 -> M2-1 edge).

## Research questions

1. Keeper template (state/event/action types, Step, shell drive loop, timers, error handling).
2. Codex reactor + substrate seam signatures/semantics; genericization gaps.
3. beadRunOne real flow -> candidate states, current line numbers.
4. Review-loop + DOT nesting.
5. hclifecycle.Machine - subsume or project.
6. Concurrency gates - shell vs reactor.
7. M3->M2 contract surface (the M3-4 -> M2-1 edge).
8. Parity strategy (D11/D13) and reusable corpus machinery.

## Findings

### 1. The keeper template (internal/keeper/step.go + shell.go)

**State.** `type Phase string` named states (step.go:47-60): PhaseIdle, PhaseAwaitingHandoff, PhaseAwaitModelDone, PhaseClearing, PhaseBriefing (Briefing = **immediate pass-through**: the transition into it emits the full brief batch and lands back at Idle in the same Step; terminals appear as `Phase==Idle` with `LastTerminal` set, `"complete"|"aborted"`, step.go:213). Full state = `CycleState` (step.go:161-214): phase + in-flight cycle fields + **every anti-loop/hysteresis field lifted verbatim from the old Cycler**. All timestamps event-`At`-sourced - never a clock read inside Step.

**Events.** ONE flat JSON-round-trippable struct discriminated by kind (step.go:106-121): `EventKind` = gauge_tick, precompact_trigger, idle_restart_tick, nonce_observed, handoff_fresh_seen, model_done, session_changed, timer_fired, crash_journal. **Shell-sampled data rides ON the event** (handoff content, gauge snapshot, shell-minted CycleID) so the transition is total+pure - no IO, no id-minting, no clock (step.go:13-23).

**Actions.** Same flat pattern (step.go:123-156): ActWriteJournal/ActTruncateHandoff/ActSendEscape/ActInjectHandoffCmd/ActInjectClear/ActInjectBrief/ActSetTmuxEnv/ActSetManagedSession/ActClearPrecompact/ActSetHold/ActEmit/ActArmTimer/ActCancelTimer/ActForceRestart. ActEmit carries a pre-marshaled `Payload []byte` (payload construction is pure, in Step). Timer actions carry `Timer TimerKind`+`D time.Duration`.

**Step signatures** (step.go:234-242,278-280):
```go
func (m *Cycle) Step(ev Event) []Action          // mutates m.state via stepCycle
func (m *Cycle) State() CycleState               // copy, inspectable between steps
func stepCycle(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action)  // total pure fn
func (m *Cycle) Run(ctx, src substrate.EventSource[Event], eff substrate.Effector[Action]) error {
    return substrate.Run(ctx, src, m.Step, eff)  // step.go:251-253
}
```
Two support methods matter: `peekFires(ev) bool` (step.go:260) - runs the transition on a state copy so the shell mints ids/samples files **only for entries that actually fire** - and `failOpen()` (step.go:271) - rollback to Idle after the one fatal effector failure.

**Shell drive** (shell.go). The shell does **not** use `substrate.Run` in production:
- `execute(ctx, Action) error` (:35-128) - effector switch. Failure policy: **everything best-effort (`_=`) EXCEPT the "opened" journal write**, which returns error. `ActArmTimer` writes `c.timers[kind]=Clock.Now().Add(d)`+`c.timersArmed=true` (deadline anchored at execution time, after preceding actions, :103-123); `ActCancelTimer` deletes.
- `feed(ctx, ev) error` (:134-142) - `for _,a := range c.machine.Step(ev) { execute }`; on error `machine.failOpen()` + propagate.
- `runEntry(ctx, ev)` (:149-160) - peek -> mint CycleID + sample handoff -> feed -> drive.
- `drive(ctx)` (:190-221) - `for machine.InCycle()`: fresh detection ticker per armed-timer generation (`Clock.NewTicker(PollInterval)` 200ms) **plus one-shot punctual wake at the nearest armed deadline** (nearestDeadline :227-242), select over ctx.Done/ticker/deadline, each wake -> pollOnce.
- `pollOnce(ctx)` (:246-312) - per-phase detection: check elapsed timer deadlines first (**timeout-before-read**), else read the phase-appropriate port and feed the detection event.
- `fireOnCancel` (:318-336) - ctx cancellation maps onto the phase's timeout edge, never silent.

**Ack/completion semantics:** none explicit - synchronous loop; an event's actions fully execute before the next event is fed. The drive loop runs **synchronously inside the watcher tick** ("reproduce-the-freeze", SK-017/D11, shell.go:20-29): while off-Idle the watcher processes nothing else.

**Structural-invariant idiom worth copying:** SR4 is enforced by making `injectClearAction` the ONLY constructor of ActInjectClear, refusing while `ModelDoneSource==""` (step.go:870-875,832-858). Invalid orderings are unrepresentable, not merely untested.

### 2. Codex reactor + substrate seam

`internal/substrate/seam.go` (37 lines):
```go
type EventSource[E any] interface { Events(ctx context.Context) <-chan E }   // :7-9
type Effector[A any]  interface { Execute(ctx context.Context, a A) error }  // :13-15
func Run[E, A any](ctx, src EventSource[E], step func(E) []A, eff Effector[A]) error  // :27
```
Semantics (:19-36): Run ranges over `src.Events(ctx)`, applies step, executes each action **in order**; returns nil on channel close, or the **first effector error without executing further actions**. Run never closes the channel (source owns closure); cancellation flows through ctx. **Strictly sequential** - next event not consumed until all actions from the previous execute (synchronous Execute IS the implicit ack). Buffering source-owned (Twin.Events chan cap 16, replay.go:114-121).

`internal/codexreactor/reactor.go`: flat Event (:66-77, `Seq uint64` monotonic dedup - Step drops `ev.Seq<=LastSeq`, resets on Connected, :193-200), flat Action (:106-117), `State{ThreadID,TurnID string; InFlight bool; LastSeq uint64}` (:151-165), `func (r *Reactor) Step(ev Event) []Action` (:193). `Effector`/`EventSource` are **type aliases** (:130,:145). `Reactor.Run` (:298) same one-liner.

**Genericization gaps the daemon hits that codex/keeper don't:**
1. **Run accepts exactly one EventSource channel.** Daemon run has many concurrent origins: bus-tap relay (agent_ready callback -> tapCh, :4731-4755), process-exit watcher (:5002), noChangeTimeoutCh closed by pasteInjectQuitOnCommit (:4934-4998,:5330), heartbeat, per-run ctx cancel, daemon shutdown, timers. Keeper solved this by **not using Run in production** - the shell multiplexes via its own select + pollOnce. Daemon shell needs the same (a mux funneling N sources into feed) or a small `Merge[E]` helper. **Report upstream, don't fork** (02-components.md:157-159).
2. **Action results must come back as events.** `Effector.Execute` returns only error. Daemon actions produce values the machine needs (Launch -> session handle/id; merge -> `mergeRes{success,noChange,reason}`; commit-fallback -> outcome). Shell must convert effect results into follow-up events (e.g. ActLaunch -> EvLaunched{err}/EvLaunchFailed) - which Run's one-way loop can't express -> another reason the shell drives feed directly.
3. **Dynamic timers** exist only shell-side (keeper's `timers map` + fresh-ticker-per-generation); generalizes fine but lives outside Run.
4. **Per-run vs per-process lifetime:** codex/keeper are one long-lived instance; the daemon needs one machine per bead-run goroutine (constructed at dispatch, dropped at Unregister).
5. **Error policy:** Run aborts on first effector error; the daemon (like keeper) needs mostly-best-effort with a small fatal set. Keeper's feed + failOpen is the template.

### 3. beadRunOne — real flow and candidate states

Dispatch-site prologue (OUTER loop, before the goroutine): ClaimBead :2853, runRegistry.Register :2991, localInFlight.Add(1) :3009-3010, per-run runCtx/runCancel :2965, goroutine spawn calling beadRunOne :3025-3030 with defer runRegistry.Unregister :3026.

| Phase | Lines | What |
|---|---|---|
| Run resolution | ~:3098-:3400 | owning epic :3098; resolveWorkflowMode (immutable for run life :3163-3168); harness agent type + ResolveModelPreference :3194-3207; Pi profile refuse-before-launch reopen :3215-3236; ~early-return ReopenBead guards :3223,:3280,:3313,:3343,:3497,:3548 |
| Worktree setup | :3592-:3682 | wtFactory/CreateWorktree under worktreeCreateMu :3592-3603; remote base-SHA fetch :3639; wtErr->reopen :3670-3677 |
| Launch-spec pre-build | :3790-:3810 | routedLaunchSpecBuilder built once for ALL modes |
| **Workflow-mode fork** | **switch workflowMode :3824** | ReviewLoop :3825 -> runReviewLoop :3846 then merge :3860-3924, budget/reopen :3938-4007; Dot :4014 -> driveDotWorkflow then merge/close/reopen :4103-4215; default(single) :4221, dispatch :4226 |
| Single: build+spawn | :4228-:4589 | launch spec :4228; per-run substrate + independent-session :4337-4381; sandbox gate+canary :4443-4540 |
| Single: launch | :4590-:4730 | hook-session reg :4590; pre-exec emits :4595; tapping emitter :4605; **agentSpawnSem acquire :4620-4634** (cap 3, remote cold-start); Launch + ErrSpawnCapTimeout :4645-4657; handle.SetMachine :4674-4675 |
| Agent-ready wait | :4731-:4914 | ready-callback into tapCh :4731-4755; kickoff paste-inject :4789; heartbeat goroutine :4791; completionMode :4810-4816; **waitAgentReady skipped for CompletionProcessExit** :4826-4831; the wait :4854; ProcessExit falls through :4914 |
| Monitoring | :4927-:5036 | noChangeTimeoutCh :4932-4934 (nil for ProcessExit); pasteInjectOnLaunch :4936; pasteInjectQuitOnCommit goroutine closing noChangeTimeoutCh :4978-4998; Step 7 wait for watcher/handler exit or ctx cancel :5002-5028; transitionToTerminated :5036 |
| Post-exit gating | :5049-:5115 | implementer_phase_complete :5049-5062; **ProcessExit daemon-side commit fallback :5066-:5110** (ensureCodexRefsTrailer/ensurePiRefsTrailer); Step 8 map Wait->terminal :5111-5113 |
| Terminal switch | :5116-:5445 | agent_completed -> sync+merge+close :5248-5273; exit-0 auto-close :5277-5325; default `select { case <-noChangeTimeoutCh: :5330-:5331 }` -> subsumed close or reopen; shutdown-drain merge under bgCtx :5367-5398; failure classification -> reopen :5410+ |

Signature (:3072) note: `runSucceeded *bool` out-param C6 eliminates.

**Proposed state list (grounded):** `Resolving -> WorktreeReady -> {ModeFork} -> BuildingSpec -> Launching -> AwaitingReady -> Monitoring -> Exited -> Gating -> Merging -> {Closed | Reopened}`, with:
- **Claiming stays a shell concern**: ClaimBead/Register/localInFlight all in the OUTER dispatch loop before the run goroutine exists (:2853/:2991/:3010) - the reactor is born already-claimed. If a `Claiming` state is kept, it is entered pre-armed.
- AwaitingReady skipped (or immediate) for CompletionProcessExit (:4831) - a per-mode edge, not a separate machine.
- Monitoring's exits = the event set: EvHandlerExited{exitCode,waitErr}, EvNoChangeTimeout, EvCtxCancelled{shutdown|per-run-abort} (:5011-5028), plus TimerFired(agent_ready|post_ready_hang|heartbeat).
- The ~20 early-return ReopenBead guards before launch are all the same edge: `* -> Reopened` with a reason.

### 4. Review-loop + DOT nesting

**Review loop** (reviewloop.go). `reviewLoopState` :135-178: iterationCount (1-based), claudeSessionID, lastVerdictNotes, lastDiffHash, lastIterHeadSHA (progress = HEAD advancement, hk-togxq), priorVerdicts, lastVerdictFlags. `runReviewLoop` (:198-238, returns reviewLoopResult{success,completionReason,summary,needsAttention,approveVerdict} :115-132) is **already a for-loop state machine**: init `state:=reviewLoopState{iterationCount:1}` :248; iter>=2 -> implementer_resumed + --resume relaunch :252-255; per-iteration build spec -> launch implementer -> **its own agent-ready wait with fixed 2s resumeReadyFallbackGrace** (:110; raw time.After :655-659, the caulk C5 replaces) -> wait exit -> launch reviewer -> read verdict -> route. It re-enters the Launching->Monitoring shape per iteration.

**DOT cascade** (dot_cascade.go). `driveDotWorkflow` (:181) walks a graph from entry to terminal with `currentNodeID` cursor; dispatch `switch node.Type` :422: NonAgentic :423 (shell/tool), Agentic :486 (dispatchDotAgenticNode - full launch/monitor cycle, no-progress guard :491-720, implementer_resumed mirroring reviewloop :873-886), Gate :1001 (PolicyExprEvaluator). Data-driven - "state" = (currentNodeID, iteration bookkeeping), transition table = graph edges.

**Fold vs nest:** both are naturally **nested sub-machines** sharing the run reactor's inner Launch->AwaitReady->Monitor->Exited spine: review loop = fixed 2-node cascade (implementer->reviewer, verdict-routed); DOT generalizes it; single-shot = degenerate 1-node cascade. Cleanest shape: run-level states own claim-through-worktree and merge-through-terminal; a **per-node/per-iteration sub-machine** (iterationCount/currentNodeID in reactor state) owns the repeated launch cycle. Folding all three flat would triple the launch/monitor states. Code already wants this: dot's agentic dispatch "mirrors reviewloop.go" (dot_cascade.go:786,:873).

### 5. hclifecycle.Machine

`internal/handlercontract/lifecycle/types.go:16-43` - an **8-state** machine (Spawning->Initializing->Ready->Executing->Suspended->Terminating->{Terminated|Failed}, IsTerminal() :41-43), legal-transition table (table.go), history, `Transition(to,reason,errCode,errMsg) error` that **rejects invalid transitions** (machine.go:45). `transitionToTerminated` (workloop.go:7994-8018) drives current->Terminating->Terminated/Failed after waitWithSocketGrace, silently ignoring invalid transitions; every successful transition emits durable `lifecycle_transition` (HC-065, emitWorkloopLifecycleTransition :8022). Called from beadRunOne :5036 with context.Background().

**Readers:** RunHandle.GetMachine (runregistry.go:139); stale watcher (stalewatch.go:931); state-gather/dashboard (stategather.go:153); set onto handle :4675.

**Subsume or project:** it is per-*session* (handler-contract surface, HC-065), not per-*run*; review-loop and DOT spawn multiple sessions per run, each with its own Machine. It has external concurrent readers (stale watcher) needing a live snapshot mid-run. **Recommendation: keep it as a downstream projection** - the reactor's session-scoped transitions emit `ActLifecycleTransition{to,reason}` that the effector applies to the session's Machine, preserving HC-065 emission and the external-reader surface. Subsuming would force per-session sub-state into run state and break the stale watcher's read path.

### 6. Concurrency gates

- **ClaimBead / runRegistry.Register / localInFlight** - all OUTER-loop, pre-goroutine (:2853,:2991,:3010); Unregister + runCancel are goroutine defers (:3025-3027). **Shell concerns acquired before the reactor exists**; the reactor never sees them. Wrinkle: the fallback local re-check inside beadRunOne can release a held local slot early (relLocalSlot :3076-3084,:3419-3424) - a shell-side adjustment, still not reactor state.
- **agentSpawnSem** (:409-430 decl, cap 3 :1163, acquire :4620-4634) - acquired **mid-flow** (remote cold-start only, rbc!=nil), blocking select with reopen-on-ctx-cancel, released after agent_ready with a sync.Once leak backstop. This sits inside reactor territory. Two clean options: (a) effector action ActAcquireSpawnSlot whose result returns as EvSpawnSlotAcquired/EvSpawnSlotAborted (fits gap #2), or (b) fold acquisition into the effector's ActLaunch execution (keeper precedent: blocking port calls live in execute). Either way it is an **effector/shell concern**; must NOT be a busy-wait state in the pure machine.

### 7. CRITICAL — the M3->M2 contract surface

**The edge, verbatim.** decompose-review.md:43-45: "M3-4 -> M2-1 is the single M3->M2 edge (ROADMAP §phase-map note); **the C4 design MUST pin the reactor Step input/ack contract explicitly for M2 to implement against.**" ROADMAP.md:84: "M3-4 (reactor Step) -> M2-1 (seam input/ack contract); everything else runs concurrently." TASKS.md:85 lists M3-4 deps as "M3-3, M3-1, M2-1". **Direction stated both ways**: TASKS has M3-4 depending on M2-1 while decompose-review makes C4's design the producer M2 consumes.

M2's own review resolves the deadlock. `.kerf/works/2026-07-14-agent-input-substrate/decompose-review.md:41-44` (F4): "The M2-1 <-> M3-4 cross-work edge is live: M3 is itself only at decompose, so no reactor-Step input/ack contract exists yet to consume. **Design will pin M2-1 self-contained and flag divergence risk as a PLANNER-RECONCILE item rather than guess M3's contract.**" (M2 spec.yaml status now `change-design`; its 03-research/{seam-contract,driver,capture-tee,harness} dirs currently **empty**.)

**What M2 expects to deliver/consume** (M2 02-components.md C1 :29-55): a `SendInput`/`Submit(ctx, payload) (Ack, error)`-shaped method on `handler.Substrate`/`SubstrateSession` "whose success means 'the agent accepted this input,' not 'tmux queued a buffer'", retiring the no-ops at internal/handler/substrate.go:140 (SendInput returns nil) + :173 (CloseStdin) and the type-asserted side-interfaces in pasteinject.go. Its open questions (:46-55): "Is the ack a **synchronous return** or an **awaited event** on the reactor stream? ... Bounded-liveness contract: what is the M2 analog of P1 SR9 / STEP-0a - 'every input reaches ack-or-stale within a bounded window, never silence'? Where does the timeout live (ClockPort)?" M2-2 (:57-81) additionally builds an input **driver**: "a codexwire-shaped wire codec + an Event/Action vocabulary + a Step state machine, instantiated over the generic Run[E,A] loop" - i.e. M2's driver is itself a reactor whose events (frame-decoded agent output, acks) must reach M3's run reactor.

**Today's ingress contract:** events enter substrate.Run via `EventSource[E].Events(ctx) <-chan E`: single channel, **strictly ordered, synchronous** - Run does not pull event N+1 until every action from event N has Execute'd (seam.go:27-36). **No ack primitive**: the only completion signal is Execute's error return, and Run aborts on first non-nil. Backpressure = the channel (source-owned buffering; Twin cap 16). Keeper's production shell bypasses Run and feeds synchronously (shell.go:134).

**What C4's explicit input contract must specify** (the pin M2 implements against):
1. **Input type**: the daemon-run Event vocabulary agent-input signals map into - at minimum EvAgentReady, EvAgentOutput/Heartbeat, EvInputAcked{seq}, EvInputStale{seq}, EvHandlerExited{code,err} - flat JSON-round-trippable structs per the keeper/codex idiom, each carrying a Clock-stamped At.
2. **Ack semantics**: recommend the **awaited-event form** (M2's C1 open question resolved toward D11): the reactor emits ActSendInput{seq,payload}; the M2 driver's synchronous (Ack,error) return is converted by the shell into EvInputAcked{seq}/EvInputFailed{seq} - so the pure machine sees ack-or-timeout as event interleavings and the SR9-analog ("ack-or-stale within a bounded window, never silence") is expressible as ArmTimer(input_ack)/TimerFired exactly like keeper SK-014.
3. **Ordering**: per-run events totally ordered as fed (one feed goroutine per run); inputs carry a per-run monotonic seq (codex Seq dedup, reactor.go:193-200) so late/duplicate acks are droppable.
4. **Backpressure/buffering**: shell-owned; the machine never blocks - a full input pipe is a TimerFired(input_ack) -> stale edge, never a wedge.

**This contract is written as a standalone quotable section in 04-design (the C4 design doc); see the "M3->M2 reactor Step input/ack contract" section there.**

### 8. Parity strategy (D11/D13) and reusable machinery

**D11** (session-restart-substrate 00-decisions.md:290-314): "reproduce-the-freeze first" - the rebuild reproduces old blocking behavior so the baseline comparison is apples-to-apples; relaxing it is a later, separately-measured change. Daemon analog: the extraction must not change observable event streams except the sanctioned SR9-class fix.

**D13** (00-decisions.md:337-366): build a corpus (507 per-cycle streams -> testdata/keeper-cycles/baseline-2026-07-13/), a **trace-driven Twin** (baseline records outputs; a synthesizer derives the input stimulus schedule from each cycle's outcome), an **old-vs-new differential** (old Cycler + new Step over the SAME schedules; diff terminal type/reason/interior order/bounded-termination; allowlist = only sanctioned changes; scaffold deleted when old code is deleted), frozen anchors, an out-of-band oracle.

**In-tree to reuse:**
- `testdata/keeper-cycles/baseline-2026-07-13/` - 507 cycles (1,014 files) + manifest.json/EXTRACT-LOG.md/ACCEPTANCE-T14.md; built by scripts/extract-keeper-corpus.py.
- `internal/keepertwin` (T9) - SynthesizeStimulus (summary.json -> []keeper.Event) + keeperCodec (ReplayCodec[keeper.Event]) routed through the **generic** substrate.Twin[E] + FaultConfig (replay.go:82,:104,:114) so the four fault modes apply with zero vertical-specific fault code.
- `internal/keepertest` - L0-L3 tier taxonomy (l0_step_test.go, l1_contract_test.go, l2_fault_matrix_test.go, l2_integration_test.go, l3_live_test.go, canary_test.go frozen anchors, metrics_export_test.go).
- `internal/replay` (T4) - `Replay(path, since, strict, checkers) (Report, error)` (replay.go:157) over the durable event log (EventID-sorted UUIDv7), Checker/Finalizer interfaces (:43,:51), the SR3/4/6/7/9 checker set (checkers.go:20-28). SR9 is a finalizing bounded-liveness checker - the shape C5's daemon invariant needs.

**The daemon's advantage over the keeper:** the keeper had to *build* its corpus (outputs weren't durable); the daemon already emits a rich durable stream (run_*, implementer_phase_complete, lifecycle_transition, reviewer_verdict, merge outcomes) - so an extract-run-corpus script (per-run_id streams + a summary golden) + a run-outcome synthesizer over substrate.Twin is a straight copy of T9/T11. The old-vs-new differential runs old beadRunOne + the new runexec reactor over the same schedules. M2's C4 capture tee later provides true input-side recordings, but the output-derived synthesizer works before M2 lands - **decoupling the parity harness from the M2-1 wait.**

## Patterns to follow (keeper template distilled)

1. **One flat Event struct + one flat Action struct**, kind-discriminated, JSON-round-trippable; per-kind field-population documented on the struct.
2. **Total pure transition as a free function** `step(cfg,state,ev)->(state',[]Action)` with a thin mutating wrapper `Step(ev) []Action` + `State() copy` + a `substrate.Run` one-liner for replay.
3. **Timers as events**: ArmTimer{kind,d}/CancelTimer{kind} actions, TimerFired{kind} events; shell owns `map[TimerKind]time.Time` over ClockPort, checks **timeout-before-read** on each poll, punctual one-shot wake at nearest deadline.
4. **Shell samples, reactor decides**: everything the machine needs from IO rides ON the event (peek-then-sample keeps expensive/id-minting sampling fire-aligned).
5. **Effector failure policy**: best-effort by default; a named tiny fatal set with failOpen() rollback.
6. **Structural invariants**: make illegal actions unconstructible (injectClearAction idiom) - the daemon peer: "close/merge unconstructible before a gate verdict is recorded in state".
7. **Immediate/pass-through states** documented but never rested in (keeper's Briefing) - useful for the daemon's Gating->Merging chain.
8. **Reproduce-the-blocking-behavior first**; relax later as a measured change.
9. **Parity harness lineage**: corpus extract -> stimulus synthesizer -> generic Twin -> old-vs-new differential (deleted with old code) -> permanent L0-L3 tiers + frozen anchors + out-of-band oracle.

## M2 contract inputs (dedicated summary)

- **Obligation**: C4's design doc must contain an explicit "reactor Step input/ack contract" section - the single cross-work artifact (decompose-review.md:43; ROADMAP.md:84).
- **Counterparty state**: M2 is at change-design, pinning M2-1 **self-contained** and flagging divergence as PLANNER-RECONCILE (its F4) - so C4's pin should be written to *converge* with M2-1's Submit(ctx,payload)(Ack,error) shape, not assume it.
- **Concrete convergence proposal**: M2's driver returns synchronous (Ack,error); M3's shell converts that into EvInputAcked/EvInputFailed{seq} events; the reactor pairs every ActSendInput{seq} with ArmTimer(input_ack), and the SR9-analog invariant becomes a replay-checkable Finalizer in internal/replay. Ordering = per-run total order + monotonic seq dedup. Backpressure = shell-owned; machine never blocks.
- **What C4 must NOT do**: bake tmux/paste-inject semantics (screen-scrape verify, blind Enter x3 - pasteinject.go:1708/:1795 per M2's 01-problem-space) into the event vocabulary; events must be substrate-agnostic so the tmux impl and the structured driver feed the same reactor.

## Risks / conflicts

1. **Circular dependency on paper**: TASKS.md:85 says M3-4 depends on M2-1, while both decompose reviews make M3's C4 design the contract *producer*. Both works design concurrently and self-contained; the parent design states the convergence contract and marks it PLANNER-RECONCILE (matching M2's F4) rather than block.
2. **substrate.Run is not the daemon's production drive loop** - keeper already proves the shell drives feed() directly. Do NOT force the daemon through Run; keep Run for replay verticals and report the multi-source / action-result-as-event gaps upstream (02-components.md:157-159 mandates report-don't-fork).
3. **The decompose's state list starts at Claiming, but claim/register/localInFlight happen in the outer loop before the run goroutine** (:2853/:2991/:3010) - the design must either move the reactor boundary out to the dispatch loop or (simpler, recommended) start the machine post-claim.
4. **hclifecycle.Machine is per-session with external concurrent readers** (stale watcher stalewatch.go:931) - subsuming it into per-run reactor state breaks that surface; project it via actions.
5. **ReviewLoop/DOT each contain full inner launch/monitor cycles** with their own raw timers (resumeReadyFallbackGrace via time.After reviewloop.go:659) - the state count explodes if flattened; nest a per-node sub-machine sharing one launch spine.
6. **workLoopDeps is 85 fields** - C4 is unbuildable before C3's port cut; the ~20 pre-launch ReopenBead guard sites all collapse onto one Reopened terminal edge.
7. **beadRunOne's keeper-relative scale**: ~2,376 lines and 3 workflow modes vs keeper's ~1,100-line reactor with 4 phases - "small state-transition-at-a-time PRs" (02-components.md:141) is load-bearing; the differential harness must exist *before* the first transition lands, unlike keeper where T9-T14 followed T7.

## PLANNER-RECONCILE

- **M3-4 <-> M2-1 direction is circular on paper.** TASKS.md:85 lists M3-4 depending on M2-1's seam input/ack contract; the decompose-review + ROADMAP make C4's design the producer. M2 is designing NOW (change-design) and has committed to pinning M2-1 self-contained + flagging divergence (its F4). The C4 design writes the convergence contract (awaited-event ack form, ActSendInput{seq}+ArmTimer(input_ack)+EvInputAcked/Failed, per-run monotonic seq, shell-owned backpressure) as a standalone quotable section for M2 to implement against. Flagged for the planner to arbitrate the two works' contract convergence, since neither can unilaterally close it.
