# 03-Research / C5 — Resume-hang bounded-liveness invariant + property test

> Pass 3 (Research), component C5. All `file:line` verified against the current
> working tree (branch `phase1-session-restart-substrate`, 2026-07-14). Delegated
> to a fresh-context sub-agent; parent owns this write. Central obligation:
> ground C5 in the real resume-path mechanics and explain why today's staleness
> detectors miss the hang.

## Research questions

1. Resume-path mechanics end to end (reviewloop.go relaunch branch).
2. Why every existing staleness detector misses the resumed-then-silent run.
3. The keeper SR9 template (SK-INV-005 / SK-015) and how it is tested.
4. Derivation of the daemon's bounded window from real constants.
5. Durable events at the resume boundary; whether a new event is needed.
6. Fault-injection machinery (substrate Twin, corpus).
7. Fail-open vs fail-closed precedent.

## Findings

### Q1 — Resume-path mechanics, step by step (reviewloop.go)

Iteration loop starts **:250**; iteration >=2 branch **:252-256**.
1. **`implementer_resumed` emitted BEFORE dispatch** (:252-256, EM-015d): records *intent to resume*, not confirmed resumption - nothing later confirms the resume took.
2. **Launch spec** phase=`implementer-resume`, `priorClaudeSessID` set (:270-279); `buildClaudeLaunchSpec` builds `--resume <uuid>` when `mintRes.ResumeMode` (claudelaunchspec.go:409-416). The agent is resumed by **spawning a fresh tmux window running `claude --resume <uuid>`** via per-run substrate (`newPerRunSubstrate` :350).
3. **Launch** :552; launch-phase wedges caulked: `ErrSpawnCapTimeout`->`spawn_cap_blocked` (:560-563), `ErrTmuxNewWindowTimeout`->`tmux_new_window_timeout` (:567-569). `launch_initiated` held back, emitted after Launch (:574-580).
4. **Daemon-side heartbeat goroutine** :588-592: `go handler.RunHeartbeatLoop(...)` - first beat immediately, then every `handler.HeartbeatInterval`=300s **unconditionally** until iteration end (claudehandler_chb006_024.go:662-683). Its comment (:581-587) says it exists so the stale watcher does not false-positive `run_stale`. **This goroutine is the villain of Q2.**
5. **Readiness signal**: under tmux the daemon's sole real ready signal is the relay-synthesized `agent_ready` on SessionStart hook receipt (:623-631, hk-wths). But `claude --resume` **reattaches without reliably re-firing SessionStart** (:89-99 comment), so the relay never sends it.
6. **`resumeReadyFallbackGrace` (:85-110, `const = 2*time.Second`) is the caulk**: on iteration >=2 with `implWatcher==nil`, a goroutine (:655-664) waits a fixed 2s then **self-emits `agent_ready` into the tap** (`capturedImplTap.Emit(...)` :660). So `waitAgentReady` (:722-723, 150s local/210s remote) is always satisfied at ~2s on resume - the configured ready timeout is **bypassed, not enforced**, every iteration >=2. Note the fallback uses `Emit` (not `EmitWithRunID`), so its envelope carries **no run_id** (workloopeventsource.go:145-147) - this accident matters in Q2 #9.
7. **Post-ready hang detector structurally excluded here** (:765-767): "tmux path (implWatcher==nil) is intentionally excluded: the only post-ready signal there would be unconditional daemon heartbeats which cannot distinguish a hung agent from a working one" (also postreadyhang.go:16-18).
8. **Paste-inject** resume brief (:806-807), then `pasteInjectQuitOnCommit` watchdog (:836-837); review-loop passes `noChangeTimeoutCh=nil` (:828).
9. **THE WEDGE POINT - `waitWithSocketGrace` (:843-844)**. On the substrate path (`watcher==nil`) it skips to `sess.Wait(ctx)` (waitsocketgrace.go:95-124), polling the tmux pane PID. A resumed claude sitting idle (paste unsubmitted, splash/session-picker, or model silent) keeps the pane PID alive forever: no stop hook, no watcher, no exit. **The loop is parked here indefinitely unless an external killer fires.** Secondary wedge: a wrong-pane/stale-handle Kill can miss and `sess.Wait` stays parked (pasteinject.go:755-769, hk-5s7tg).

### Q2 — Why today's staleness detectors miss it (the heart)

**Architectural root cause: `agent_heartbeat` is not agent-derived.** `RunHeartbeatLoop` is a daemon-side wall-clock timer (300s) that beats as long as the per-run goroutine lives, regardless of agent activity. Every recency/heartbeat detector measures *daemon-goroutine liveness*, not *agent liveness*.

| # | Detector | Location | Why it misses |
|---|---|---|---|
| 1 | `run_stale` (10-min quiet, 30s scan) | stalewatch.go:56-64; observe :555-605 | `observe()` refreshes `lastEventAt` on EVERY run_id event incl. `agent_heartbeat`. Daemon beats every 5min < 10min -> age never crosses -> **never fires**. Explains census "no run_stale"; suppression is by design (hk-nvjk, reviewloop.go:581-587). |
| 2 | Kill-consumer backstop | stalewatch.go:904-916,1015-1032 | Gated on the first `run_stale` emission -> never reached (#1). |
| 3 | `heartbeatStalenessThreshold` (8min) | pasteinject.go:641-658; :1055-1075 | `lastHeartbeat` refreshed by every daemon beat (5min<8min) -> never trips. |
| 4 | `commitPollTimeout` progress budget (30min) | pasteinject.go:605-623; :1065-1074 | Every `agent_heartbeat` treated as progress, extends `totalDeadline` by 30min -> **never expires**. The idle-pane check that WOULD catch idle claude (hk-ukx :1136-1193) is only reachable after totalDeadline expiry -> unreachable. |
| 5 | `launchHeartbeatTimeout` (180s)/`launchSuppressionCeiling` (12min) | pasteinject.go:660-719 | Daemon emits first beat immediately -> `firstHeartbeatSeen` instantly true -> passes vacuously. |
| 6 | `post_agent_ready_hang` (7min) | postreadyhang.go:37; reviewloop.go:768-781 | (a) structurally excluded on substrate path (all prod review-loop runs are tmux); (b) even on exec path a daemon beat arrives within 300s<7min. |
| 7 | `agentReadyTimeout` (150s, HC-056) | agentready.go:64; reviewloop.go:722-755 | Bypassed on iteration >=2: 2s fallback self-satisfies waitAgentReady (Q1 #6). |
| 8 | `agent_ready_stall_detected` (3min) + classic never-spawned reaper (30min) | stalewatch.go:100-120,:86,:798-833 | Keyed on `agent_ready` absence; `agentReadySeen` sticky true after iter 1 (:230-234) -> both suppressed for iteration >=2. |
| 9 | Per-dispatch never-spawned reaper (hk-sj6a) | stalewatch.go:835-867 | The ONE detector that can fire - **by accident**. Iter-2 `launch_initiated` resets `agentReadySeenSinceLastLaunch` (:593-594). Fallback `agent_ready` tap-`Emit`ed with no run_id (:660) -> `observe()` drops it (:556-558) -> reaper fires + Cancels at 30min. BUT if the relay DOES fire on resume (newer claude), callback emits WITH run_id (:626-630) -> reaper suppressed -> only #11 left. The bound flickers on which racing emission is bus-visible. |
| 10 | `noChangeTimeoutCh` | workloop.go:4932-4998,:5331 | Single-mode only; review-loop passes nil (:828,:837). |
| 11 | `commitHardCeiling` (90min, never extended) | pasteinject.go:625-639; :1114-1119 | Only designed bound that survives: /quit->30s->Kill->sess.Wait unblocks->no-progress fail->reopen. Conditional on watchdog running AND Kill hitting the right pane (hk-5s7tg); a missed Kill records nothing, so force-reap never starts from here. |
| 12 | Fast dead-process reap (5min) + force-reap (90s) | stalewatch.go:705-743,:86-168 | Needs pane DEAD AND event-silent >=5min: resumed-idle pane is alive and beats flow -> both gates fail. |

**Net current-tree behavior**: the resumed-then-silent run terminates at ~30min (reaper #9, when fallback stays run_id-invisible) or ~91min (#11), as an *emergent* property of 12 stacked caulks - three actively defeated by the daemon's own heartbeat, one enabled by an emit-API accident - **with no stated invariant and no test asserting any bound**. The census incident ("in_progress never resolves") is the case where the run escaped all of them (hk-mdus1 comment, stalewatch.go:128-136: goroutine "parked on a wait that does NOT observe runCtx ... leaks forever ... until a daemon restart"); hk-mdus1/hk-sj6a post-date the census and narrow but do not close the class.

### Q3 — The keeper SR9 template (specs/session-keeper.md)

- **SK-INV-005 / SR9 (:264-266)**: "For every cycle c, handoff_started(c) MUST reach exactly one terminal outcome within the SK-015 bounded window, or emit a `restart_failed`-class event. A cycle that produces neither ... is a conformance failure. **Every TimerFired edge lands in a state with an outgoing action, so the machine cannot wedge silently.**"
- **SK-015 (:191-193)**: bounded window "approximately HandoffTimeout(300s) + model_done_timeout(60s) + ClearConfirmBackstop(150s) + injection overhead ~= 520s - or emit a restart_failed-class event. **Silence is FORBIDDEN.**"
- **Timers-as-events (:142)**: Step emits ArmTimer/CancelTimer, consumes TimerFired; "No context.WithTimeout wall-clock deadline may survive inside the cycle."
- Escape hatch (:470); baseline anchor (:223): "unterminated cycles 1 -> MUST be 0 (SR9)".

**How it's tested:**
- `internal/replay/checkers.go:149-161` - `SR9Checker` is a **finalizing** checker: after the whole corpus replays, any cycle with handoff_started and no terminal is a violation; terminal exclusivity is the companion.
- `internal/keepertest/l2_fault_matrix_test.go` (T12): 4 fault modes x 4 corpus strata x every EventN = **44 cells, 100% required** ("invariants, not statistics; one silence=fail"). `sr9VirtualBound(cfg) = HandoffTimeout + ModelDoneTimeout + ClearConfirmBackstop + 1min` (~520s **virtual** time). Never-silence enforced by the harness: `runDiscrete` converts "still in-cycle with nothing pending" into failure; `drainTwin` wall-clock idle timer catches a hung stream; 100k-step guard catches livelock.
- T13 (af4171ae): no-regress metrics (`scripts/keeper-metrics.sh`), coverage floor, **N=10 out-of-band oracle** `scripts/keeper-oracle-n10.sh` - spec :504 "the keeper does not grade itself" (jq/grep over the log + stat reads).

### Q4 — Deriving the daemon's bounded window

| Phase | Constant | Value | File:line |
|---|---|---|---|
| resume ready (caulk) | resumeReadyFallbackGrace | 2s | reviewloop.go:110 |
| ready (bypassed on resume) | agentReadyTimeout local/remote | 150s/210s | agentready.go:64,:80 |
| first-progress (inert on tmux) | postAgentReadyHangTimeout | 7min | postreadyhang.go:37 |
| brief delivery | briefDeliveredTimeout | 2min | pasteinject.go:595 |
| launch verify | launchHeartbeatTimeout/launchSuppressionCeiling | 180s/12min | pasteinject.go:685,:719 |
| progress budget | commitPollTimeout | 30min (heartbeat-extended) | pasteinject.go:623 |
| absolute | commitHardCeiling | 90min | pasteinject.go:639 |
| teardown | noChangeKillDelay + postQuitKillGrace + stopHookGrace | 30s+60s+3s | pasteinject.go:730,:769; waitsocketgrace.go:40 |

**Proposed C5 bound, two tiers:**
- **Silent-resume bound** (zero agent-derived signal after implementer_resumed): agentReadyTimeout(150s) + postAgentReadyHangTimeout(420s) + teardown(~93s) ~= **663s ~= 11min** - the direct peer of the keeper's 520s (readiness + first-progress + teardown). Precondition: both timers become **real reactor states** whose inputs are agent-derived (relay agent_ready, pane-output/worktree fingerprint, tool events) - never the daemon's heartbeat - and the 2s fallback dissolves into the ready-timer edge ("arm a resume_ready timer; TimerFired -> degraded-confirm or fail", not "assume ready").
- **Slow-progress bound**: commitHardCeiling(90min) + teardown ~= 91.5min absolute, with the 30-min per-progress budget meaningful again once "progress" excludes the unconditional heartbeat.

### Q5 — Durable events at the resume boundary

Existing: `implementer_resumed` (class O, event-model.md:107,:1253-1264; emitted pre-dispatch = intent only), launch_initiated, agent_ready, agent_heartbeat, agent_ready_timeout (eventtype.go:275-282), post_agent_ready_hang (:284-291), implementer_budget_exceeded (:345), spawn_cap_blocked, tmux_new_window_timeout, launch_stall_detected, agent_ready_stall_detected, run_stale (eventreg_wkzlc.go).

- **Spec drift found**: `run_stale` cites "event-model.md §8.12.1" (eventreg_wkzlc.go:3; eventtype.go:872-878) but `grep -c run_stale specs/event-model.md` = **0** - §8.12 is the `decision_required` family. The daemon staleness event is spec-orphaned; C5's spec statement should home it.
- **No `run_resumed`-confirmation and no `run_liveness_timeout` exist**. Gaps: (i) nothing confirms a resume succeeded (fallback agent_ready isn't even run_id-attributed :660 + workloopeventsource.go:145-147); (ii) no failure-class event fires for the silent-resume timeout on the tmux path.
- **Keeper precedent**: §8.20 (event-model.md:484-493) added exactly 4 interior milestones because "a machine-checkable replay-invariant harness needs" per-boundary access. Daemon analog: minimally one **confirmed-resume milestone** (run_id-attributed agent_ready with iteration_count, or a new `run_resume_confirmed`) plus one **`run_liveness_timeout` failure-class event** (the restart_failed analog). implementer_resumed + terminal events cover the outer edges.

### Q6 — Fault-injection machinery

- `internal/substrate/replay.go:17-42` - generic Twin with 5 fault modes: FaultNone, FaultDropAfter, **FaultStall** ("delivers events 1..N-1, then blocks before event N until ctx cancelled"), FaultTruncate, FaultDup; parameterised by `FaultConfig{Mode, EventN}`; vertical supplies a `ReplayCodec`. FakeClock (fakeclock.go) provides virtual time; tested substrate_test.go:185-206.
- Keeper vertical: `internal/keepertwin/` (codec.go, synthesizer.go) + frozen corpus `testdata/keeper-cycles/baseline-2026-07-13` (manifest + cycles, drift-canary keepertest/canary_test.go) extracted by `scripts/extract-keeper-corpus.py` = D13.
- **Daemon: no recorded-run corpus exists** (`testdata/` holds only codex-app-server, depguard, keeper-cycles). Options: build one (D13 analog - `events.jsonl` has raw material, `harmonik subscribe --json` is the sanctioned reader), or note the keeper fault matrix largely runs on **discrete synthetic stimuli** (l2_fault_matrix_test.go strips pre-scheduled TimerFired lines and regenerates them from the reactor's own ArmTimer actions) - that corpus-free pattern transfers directly to C5's "stalled agent on relaunch over FakeClock" test; a recorded corpus adds the no-regress/parity layer, not the invariant layer.

### Q7 — Fail-open vs fail-closed precedent

Every existing daemon timeout path is **fail-closed -> reopen with a bounded retry budget**: agent_ready_timeout (reviewloop.go:730-755, Kill+reap+emit+rlErrorResult needsAttention=true :1784-1791); post_agent_ready_hang (:874-886); budget/hard-ceiling kill (pasteinject.go:1014-1048,:1114-1119 -> Kill -> sess.Wait unblocks -> no-commit/no-progress guard -> error); all converge workloop.go:3936-4010: `ReviewLoopFailures++`; below `MaxReviewLoopFailures` -> **ReopenBead for retry** (:4004-4009); at exhaustion -> **close with needs-attention** (:3980-4002). Reaper paths: handle.Cancel -> bead reopened + run_failed (stalewatch.go:1108-1113).

The keeper's model-done bound is fail-open (proceeding is safe there). For the daemon, proceeding past an unconfirmed resume would dispatch a reviewer against an unverified worktree (violating EM-015d's HEAD-advance requirement) - no precedent supports it. **Recommendation: fail-closed** - `run_liveness_timeout` -> kill -> reopen, riding the existing `ReviewLoopFailures` budget for anti-thrash (matches 02-components.md:192-193).

## Patterns to follow (SR9 template distilled)

1. **Normative wording** (adapt SK-INV-005): "For every run r that emits implementer_resumed(r,i), the run MUST reach exactly one terminal outcome, or emit a run_liveness_timeout/failure-class event, within the bounded window. Silence is FORBIDDEN."
2. **Bound = sum of named phase timers with constants stated** (daemon: 150s + 420s + ~93s ~= 663s silent-resume tier; 90min absolute tier).
3. **Structural rule**: timers-as-events; every TimerFired edge lands in a state with an outgoing action; no context.WithTimeout/side-goroutine deadline survives inside the run cycle (this is what kills the 12-caulk stack: watchdogs become reactor edges).
4. **Escape-hatch event**: a durable failure-class emission (run_liveness_timeout) when even the degraded path can't complete.
5. **Checker**: finalizing replay checker keyed per run_id (SR9Checker pattern, checkers.go:149-161) - "unterminated N -> MUST be 0" anchor.
6. **Fault matrix**: modes x strata x EventN, 100% pass; never-silence enforced by the harness; FaultStall on the resume-launch event is C5's headline cell.
7. **Out-of-band oracle**: N=10 clean relaunch cycles graded by jq/grep over events.jsonl - the machine does not grade itself (spec :504).

## Risks / conflicts

- **The unconditional daemon heartbeat is load-bearing in two opposite directions**: it suppresses false run_stale (hk-nvjk) AND defeats four true-silence detectors (#1,#3,#4,#6). C5 cannot just delete it; the design must **split "daemon goroutine alive" from "agent making progress"** (distinct event type, payload tag, or agent-derived progress inputs). This is the central design tension.
- **The only <=30-min bound today is an accident**: the fallback agent_ready's missing run_id (:660) is what lets the per-dispatch reaper fire. "Fixing" that emit to EmitWithRunID would silently remove the bound (-> 90min). The property test must pin whichever semantics C5 chooses before anyone touches that line.
- **Kill-miss leak survives**: a hard-ceiling Kill hitting a wrong/stale pane leaves sess.Wait parked, and this path never records cancelledAt, so force-reap doesn't start. Argues for the timeout living as a reactor terminal edge, not a side goroutine that merely hopes to unblock the wait.
- **M2 overlap**: paste-injection and the tmux pane wait are M2's replacement target. C5 should state the invariant over reactor states/timers, not tmux mechanics, or it rots when M2 lands.
- **Spec debts to settle in C5's statement**: run_stale is spec-orphaned; agentReadyTimeout comment drift (30s/90s/150s across workloop.go:500, agentready.go:144, agentready.go:64 - the const is 150s).
- **Corpus**: no daemon recorded-run corpus exists; the invariant/fault-matrix layer doesn't need one, but the parity/no-regress layer (keeper T13 analog) would - a D13-style extraction from events.jsonl is the likely follow-up.

## PLANNER-RECONCILE

- **C5 requires splitting the daemon heartbeat semantics, which touches a hk-nvjk-motivated design and interacts with an accidental liveness bound.** The bounded-liveness fix cannot land without (a) making agent_heartbeat distinguishable from agent-progress (or introducing agent-derived progress inputs), and (b) deciding whether to fix the run_id-less fallback agent_ready emit (:660) - which currently is the *only* thing giving a <=30-min bound. Both are behavior changes beyond a pure extraction. Design proceeds fail-closed with a new run_liveness_timeout event + the two-tier bound; flagged because it changes an observable (previously the run hung; now it terminates) which is the ONE sanctioned divergence per the parity constraint - the planner should confirm this is the intended M3 scope for the absorbed STEP-0a fix (ROADMAP says yes, but it crosses into heartbeat-semantics territory the decompose didn't fully name).
