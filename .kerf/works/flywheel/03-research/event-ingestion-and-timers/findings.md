# Research/Design — Event ingestion into a live agent + timers/watchdog

> Component: `event-ingestion-and-timers`. Round-2. Source: sub-agent (sonnet), Pi `agent-harness.ts`/`agent-loop.ts` + reactive patterns, 2026-05-27. **[SOURCED]** = Pi API; **[DESIGN]** = proposal.

## TL;DR
- Pi's `AgentHarness` has the in-process write paths to push into a LIVE session without restart: `steer()`, `followUp()`, `nextTurn()`. None is exposed externally → the minimal **bridge** is a TS async loop (same process) that tails `harmonik subscribe` stdout and calls `harness.followUp()` (or `prompt()` if idle) at each debounce boundary.
- `steer` injects at the next turn boundary (not mid-LLM-stream); true mid-stream interruption needs `harness.abort()` then re-prompt. So "urgent" vs "normal" events share the mechanism but differ by whether they abort.
- Idle-wait is a real gap: Pi's loop exits to idle when both queues are empty (no built-in block). Flywheel wraps the harness in an **outer loop** that `await Promise.race([wakePromise, sleep(watchdog)])` → zero token burn while idle, wakes on event-or-timer.

## 1. Injecting events into a live session [SOURCED]
`AgentHarness` write paths (`agent-harness.ts`): `steer(text)`→`steerQueue` (drained after every tool-batch within a run, `agent-loop.ts:~152`); `followUp(text)`→`followUpQueue` (drained when the agent would stop, `:~214`); `nextTurn(text)`→`nextTurnQueue` (start of next explicit `prompt()`). `steer()`/`followUp()` throw if phase==="idle" (`:679,685`). **Neither reachable externally** → bridge = a long-running async fn in the flywheel process that: spawns `harmonik subscribe --types run_completed,run_failed,reviewer_verdict,merge_conflict,heartbeat --heartbeat 30s`, reads NDJSON line-by-line, debounces, and per window calls `harness.followUp(digest)` — guarded: **idle → `harness.prompt(digest)` (new turn); active → `followUp(digest)`.** Never `steer()` from the bridge (can't safely tell from outside if a tool call is in flight).

## 2. Interrupt vs queue policy
Steering is consumed at the turn boundary, not mid-stream [SOURCED `agent-loop.ts:~152`]. True interrupt = `harness.abort()` (`:1005`, sets runAbortController.abort → active stream resolves `stopReason:"aborted"` → `agent_end` → idle → deliver via fresh `prompt()`). **Policy [DESIGN]:** ALL events queue via followUp (correct, zero-cost for ~95%); **urgent class = only `merge_conflict`** → abort → wait idle → `prompt(urgentDigest)` (current work discarded; branch state is invalid anyway). Bridge tags `{event, urgent}`; after debounce, if any urgent → abort+reprompt, else followUp.

## 3. Idle behavior — sleep until event-or-timer [DESIGN]
Pi's loop exits to idle when both queues empty (`waitForIdle()` `:1034`); no built-in block. Outer flywheel loop:
```
while (true) {
  armWake();                                   // wakePromise = new Promise(r => wakeResolve = r)
  await Promise.race([wakePromise, sleep(WATCHDOG_MS)]);
  const digest = drainPendingEvents();         // debounced batch
  if (harness.isIdle()) await harness.prompt(digest ?? heartbeatCheckPrompt());
  else if (digest)      await harness.followUp(digest);
  // else: agent already running, let it finish
}
```
Bridge calls `triggerWake()` on event; timer fires the watchdog. No model calls unless there's something to act on. (`phase` is private `:182`; expose `isIdle()` or read `AgentState.isStreaming` `types.ts:~294`.)

## 4. Burst coalescing (debounce) [DESIGN]
10 concurrent completions → ONE turn with a refreshed digest, not 10. Bridge: `onEvent` pushes to `pending`, resets a ~400ms timer; `flush` splices `pending`, builds one digest ("3 completed hk-x/y/z, 1 failed hk-a: merge_conflict, 2 approved"), sets `pendingDigest`, `triggerWake()`. The subscribe heartbeat (default 60s) flows the same path but is filtered unless its `active_runs` carry interesting (stalled) ages.

## 5. Timers / watchdog [DESIGN]
Checked when the outer loop wakes via the `sleep(WATCHDOG_MS)` path:
- **A. Quiet:** `now - lastEvent > QUIET_THRESHOLD` (~5 min) AND active_runs exist → prompt "no events in 5m, active runs [...], check daemon alive + runs progressing (`harmonik queue`)."
- **B. Run stall:** each heartbeat carries `active_runs[].age_seconds`; bridge maps run ages; if any > STALL_THRESHOLD (~600s) → bypass debounce, `triggerWake()` with a stall event → agent investigates that run.
- **C. Daemon down:** track `lastHeartbeatAt`; if `now - lastHeartbeatAt > 3×heartbeat` (~180s for 60s) → wake "daemon heartbeat stopped, reconnect; if socket missing (exit 17) flag down + pause dispatch." Bridge handles reconnect: on `harmonik subscribe` exit 17, exp backoff (5/10/30s), push synthetic `daemon_down` after first failure.

## 6. Liveness of the flywheel itself [DESIGN — keep minimal]
Run flywheel in a named tmux pane (NTM-managed) by convention. (1) outer loop writes `Date.now()` to `.harmonik/flywheel.heartbeat` every wake; (2) daemon/cron checks its age (>2×WATCHDOG → log `flywheel_stale` to events.jsonl); (3) reuse the daemon's existing tmux pane-liveness check (`internal/daemon/pasteinject.go`) if flywheel runs in a named pane. Do NOT build a recursive who-watches-the-watcher; heartbeat-file + pane check suffices for a dev-stage orchestrator.

## Summary table
| Problem | Mechanism | Status |
|---|---|---|
| Push event into live session | bridge → `harness.followUp()` (idle → `prompt()`) | DESIGN over SOURCED Pi API |
| Mid-run steering | `harness.steer()` at next tool-batch boundary | SOURCED `agent-loop.ts:~152` |
| Urgent interrupt (merge_conflict) | `harness.abort()` → `prompt()` | SOURCED `agent-harness.ts:1005` |
| Idle wait, no token burn | outer `Promise.race([wake, sleep(N)])` | DESIGN |
| Burst coalescing | ~400ms debounce → one digest | DESIGN |
| Stall/quiet/daemon-down watchdog | timer arm + heartbeat-payload checks | DESIGN (subscribe exit-17 SOURCED) |
| Flywheel liveness | heartbeat file + tmux pane check | DESIGN |
