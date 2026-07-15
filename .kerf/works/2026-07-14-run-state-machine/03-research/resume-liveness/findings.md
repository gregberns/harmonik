# 03-Research / liveness — C5 findings (resume-hang + SR9 template)

> Pass 3 (Research), component C5. Verified against the working tree 2026-07-14.
> Subject: the resume/relaunch branch (`internal/daemon/reviewloop.go`), today's
> caulks, why the staleness detectors miss the hang, and the SK-INV-005/SR9
> template (`specs/session-keeper.md`, `internal/replay/checkers.go`).

## 1. Review-loop mechanics

`reviewLoopState` (`reviewloop.go:135–:179`): `iterationCount` (1-based, init
`:248`), `claudeSessionID` (`:141`, captured iter-1 via SessionIDInterceptor),
`lastVerdictNotes` (`:145`), `lastDiffHash`/`lastIterHeadSHA` (`:157/:166`,
no-progress), `priorVerdicts`/`lastVerdictFlags` (`:171/:178`).
Iteration loop `for {}` at `:250`; terminates by return (APPROVE/BLOCK/cap/error)
or re-enters after REQUEST_CHANGES.

**Resume branch (`:252–:279`):** `iterationCount >= 2` → emit
`implementer_resumed` BEFORE dispatch (`:255`; emitter `:1995–:2018`,
`EmitWithRunID` `:2017` — durable, run-scoped), then
`ReviewLoopPhaseImplementerResume` + `implPrior=&state.claudeSessionID`
(`:276–:278`). Iter-1 differences: `claude --session-id <uuid>` (fresh) vs
`--resume <uuid>` (reattach, `:635–:641`, `:90–:99`); SessionID
interception/CHB-023 persist only on iter 1 (`:425–:522`); the resume-ready
fallback armed only when `iterationCount>=2 && implWatcher==nil` (`:655–:664`).

**The wait after resume:** Launch `:552` (tmux substrate → `implWatcher==nil`) →
`waitAgentReady(implReadyCtx, runID, implEventSrc, implAdapter, implReadyTimeout)`
`:723` over the per-run tap (`newChanAgentEventSource(implTapCh)` `:720`). With
`implWatcher==nil` the watcher-done cancel goroutine `:710–:718` is SKIPPED — the
only releases are (a) a relay `agent_ready` on the tap, (b) the fallback emit,
(c) `implReadyTimeout` (150s/210s) → run fails `"implementer agent_ready_timeout
at iteration 2"` (`:730–:754`, emits `emitReviewLoopCycleComplete` `:753`),
(d) ctx cancel.

## 2. The 2s caulk (`resumeReadyFallbackGrace`)

`const resumeReadyFallbackGrace = 2*time.Second` (`:110`). Armed `:633–:664`:
`time.After(2s)` → `capturedImplTap.Emit(bg, EventTypeAgentReady, nil)`
(`:659–:660`); cancelled eagerly right after waitAgentReady returns (`:728`,
`:785`, defer `:654`). Rationale (`:100–:107`): `--resume` renders the welcome
splash before the REPL accepts input (hk-kunm4) — a fixed wall-clock guess, not an
observed ready signal. **Load-bearing subtlety:** the fallback uses plain
`implTap.Emit` → `underlying.Emit` with **NO run_id**
(`workloopeventsource.go:145–:161`): tap subscribers see a run-stamped synthetic
envelope, but the events.jsonl copy is NOT run-correlated — a replay blind spot
(fix candidate: emit with run_id).

## 3. Why today's detectors miss the hang (all verified)

The StaleWatcher (`stalewatch.go`) is a wildcard bus observer (`:540–:552`)
tracking `lastEventType/lastEventAt` per run (`:574–:575`), scanning every 30s
(`:64`):
1. **`agentReadySeen` is latched by iteration 1** (`:598–:599`) — permanently
   suppressing the never-spawned reaper (guard `!agentReadySeen` `:816`) and
   `agent_ready_stall_detected` (guard `:781`).
2. **The 300s heartbeat masks `run_stale`:** `observe` refreshes `lastEventAt` on
   EVERY event incl. `agent_heartbeat` (`:574–:575`); heartbeats every 300s vs a
   600s stale window (`:59`) ⇒ `run_stale` NEVER fires while the agent process
   idles alive. The heartbeat is a false liveness pulse.
3. The per-dispatch reaper (hk-sj6a `:835–:867`, `agentReadySeenSinceLastLaunch`
   reset on re-`launch_initiated` `:594`) WOULD fire — but only after
   `neverSpawnedTimeout` ~30m (`:86`), and only because the fallback's
   `agent_ready` carries no run_id (so `observe` early-returns `:556–:558` and the
   flag stays false).

**Census-claim correction (carry into design):** "stays in_progress forever" is NOT
supported by the current tree — three uncoordinated bounds exist (2s fallback;
150s/210s agent_ready timeout → cycle-complete failure; ~30m reaper). What is
actually wrong: these are wall-clock caulks, not a structural invariant, and the
window between `implementer_resumed` and whichever bound fires is run-correlated-
event-silent. The C5 deliverable is the structural bound, exactly as SK-INV-005
replaced the keeper's caulks.

**DOT path lacks the caulk entirely:** `dot_cascade.go` reaches
`ReviewLoopPhaseImplementerResume` (`:1285`) and waitAgentReady (`:1705`) with NO
`resumeReadyFallbackGrace` reference (grep-verified) — exposed to the same hk-isq02
silence with only the timeout/reaper bounds. The invariant must live at the SHARED
spine, covering reviewloop.go AND dot_cascade.go uniformly.

## 4. Ready-detection asymmetry (root cause)

First launch: fresh `--session-id` fires SessionStart → hook relay synthesizes
`agent_ready` → daemon relay callback emits via EmitWithRunID (`reviewloop.go:
623–:631`; single-mode `workloop.go:4784–:4786`); exec path also has watcher-done
cancel (`workloop.go:4841–:4849`). Resume: `--resume` reattach does NOT reliably
re-fire SessionStart; relay never sends agent_ready; tmux substrate nils the
watcher ⇒ no ready signal at all. That asymmetry IS the hang; the 2s emit papers
over it on the builtin loop only.

## 5. The SR9 template to mirror

**SK-INV-005 (`specs/session-keeper.md:264–:266`):** every cycle MUST reach exactly
one terminal within the SK-015 bounded window or emit a `restart_failed`-class
event; neither = conformance failure; structurally every `TimerFired` edge lands in
a state with an outgoing action.
**SK-015 window (`:191–:193`):** `HandoffTimeout(300s) + model_done_timeout(60s) +
ClearConfirmBackstop(150s) + overhead ≈ 520s`.
**Checker shape (`internal/replay/checkers.go:149–:194`):** `SR9Checker.Types()` =
lifecycle markers; `Check` = no-op; `Finalize(states)` flags (a) started-but-no-
terminal, (b) terminal-exclusivity breach. Registered via `DefaultCheckers`
(`:20–:28`); `Replay` finalizes end-of-corpus (`replay.go:243`).
**Daemon peer:** a per-run_id finalizing checker flagging any run whose
`implementer_resumed` is not followed within the derived window by a
run-correlated terminal (`review_loop_cycle_complete`/`run_completed`/`run_failed`)
or failure-class signal.

## 6. Timeout constants for the daemon window derivation

`resumeReadyFallbackGrace` 2s (`reviewloop.go:110`); `defaultAgentReadyTimeout`
150s (`agentready.go:64`); `defaultRemoteAgentReadyTimeout` 210s (`:80`);
`effectiveAgentReadyTimeout` (`:94–:105`); `defaultPostAgentReadyHangTimeout` 7m
(`postreadyhang.go:37`, exec path ONLY — excluded on tmux/resume per
`reviewloop.go:759–:781`); `agentReadyKillReapTimeout` 10s (`workloop.go:141`);
`staleWatchDefaultAfter` 10m / scan 30s / reviewer-launch 30m
(`stalewatch.go:59/:64/:72`); `neverSpawnedReaperDefaultTimeout` 30m (`:86`);
`launchStallThreshold` 30s (`:98`); `agentReadyStallThreshold` 3m (`:120`);
`HeartbeatInterval` 300s (`claudehandler_chb006_024.go:49`); watchdog
`commitPollTimeout` 30m / `commitHardCeiling` 90m (`pasteinject.go:623/:639`);
config-threaded timeout fields `workloop.go:499–:523`, populated `:1143–:1144`.

## 7. Durable-event picture at the resume boundary

Present in a replay of a hung run: `implementer_resumed` (run-scoped, `:2017`),
`launch_initiated` (re-emitted per iteration `:578–:580`, hk-4l7zs),
`agent_heartbeat` (300s — the only traffic during the hang). Absent:
run-correlated `agent_ready` (fallback emits without run_id), `reviewer_launched`,
any terminal. So a hung run IS replay-detectable as a per-run_id
"resumed-but-no-terminal" gap — exactly the SR9-shaped Finalize check. Fix
candidate folded into C5: stamp the synthetic ready emit with the run_id so the
resume ready signal is replay-correlatable.

## 8. dot_cascade relationship

`dot_cascade.go:8–:16`: generalization of the review-loop driver; shares the phase
enum + iterationCount semantics (`:252–:254`, `:761`, `:1285`), launch_initiated
hold (`:1554–:1560`), ready gating (`:1656`), reviewer_verdict emission (`:1857`),
implementer_resumed on back-edges (`:2598–:2624`). Divergence: no resume caulk
(§3). Conclusion: C5's bound must attach to the shared monitoring spine (the C4
reactor's Monitoring/Resuming states), not be patched per-driver.
