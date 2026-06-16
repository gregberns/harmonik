# Keeper injects `/session-handoff` into a brand-new session after a cross-generation restart

**Status:** open
**Severity:** high (corrupts a freshly-launched orchestrator session before it does any work)
**Component:** `internal/keeper` (cycle lifecycle / crash-recovery)
**Filed:** 2026-06-16

## Summary

When a fresh `harmonik keeper` process is armed against a brand-new Claude session
**while stale keeper state files from a prior generation are still on disk**, the new
keeper opens a handoff cycle and injects `/session-handoff` into the pane within ~10
seconds of launch — even though the session is nearly empty (well below `--act-pct`).
The result is a brand-new orchestrator session that is immediately told to write a
handoff and tear itself down, instead of booting normally.

This is a **cross-generation restart** bug: it surfaces whenever the keeper is killed
and relaunched (e.g. a fleet/daemon-upgrade restart) without deleting
`.harmonik/keeper/<agent>.{cycle,managed,lock}` first.

## Observed behavior (real incident, agent `captain`)

A fleet-restart script killed the old keeper via
`pkill -f "harmonik keeper --agent captain"` + `tmux kill-session`, installed a new
daemon binary, then relaunched the captain and armed a fresh keeper. It did **not**
delete the keeper state files. Within seconds, the freshly-launched captain session
received a `/session-handoff` slash command.

State on disk at the time:

```
# .harmonik/keeper/captain.cycle
{"cycle_id":"cyc-20260616T203628-000001","phase":"aborted",
 "opened_at":"2026-06-16T20:36:38Z","updated_at":"2026-06-16T20:39:40Z",
 "reason":"handoff_timeout"}

# .harmonik/keeper/captain.managed
4f75b3ae-ce9b-4000-a0c1-0a42183689ba        # the NEW session id

# .harmonik/keeper/captain.ctx  (the live gauge)
{"pct":5,"tokens":52645,"window_size":1000000,
 "session_id":"4f75b3ae-...","ts":"2026-06-16T20:43:14Z"}
```

- Keeper armed at `20:36:28Z`; the cycle `opened_at` is `20:36:38Z` — **~10s after arm**.
- Gauge shows **5% / 52k tokens**; the keeper ran with `--warn-pct 30 --act-pct 35`.
  A handoff at 5% is far below any threshold — this was not a legitimate context-fill
  handoff.
- The cycle then **aborted** with `reason: handoff_timeout`: the injected handoff never
  completed (there was nothing to hand off on a fresh boot), so the keeper gave up — but
  it had already corrupted the session's first turn.

## Root cause

Two interacting defects in the cycle lifecycle let a fresh keeper open a handoff on a
low-context session it inherited from a prior generation:

### 1. An `aborted` cycle is treated as terminal by crash recovery, so suppression is never restored

`RecoverFromCrash` (`internal/keeper/cycle.go:915-916`):

```go
case "complete", "aborted":
    // Terminal state — nothing to recover.
```

The anti-re-fire suppression that prevents a repeat handoff
(`lastFiredSID` / `seenLowPctAfterLastFire`, set on the abort path at
`cycle.go:741-742`) lives **in process memory only**. When the keeper process is
killed and relaunched, that suppression is gone, and `RecoverFromCrash` does **not**
re-establish it from the `aborted` journal — it treats `aborted` as "nothing to
recover." The new keeper therefore boots with zero-valued suppression.

### 2. The abort path leaves `.managed` opted-in on a first-monitored-session keeper

`cycle.go:744-759`:

```go
// Re-arm: clear .managed ... but ONLY when a real session-id change was
// previously observed (currentSessionIDSince non-zero). ...
if !c.currentSessionIDSince.IsZero() {
    if setErr := c.cfg.SetManagedSessionFn(c.cfg.ProjectDir, c.cfg.AgentName, ""); setErr != nil { ... }
}
```

A freshly-armed keeper on a relaunched session has `currentSessionIDSince == 0` (it has
not yet observed a session-id *change*), so `.managed` is **not** cleared. Combined
with defect 1, the new keeper sees: `.managed` opted-in + no persisted suppression +
a stale `aborted` `.cycle` journal it considers terminal.

### Net effect

On its first watcher tick the new keeper finds the agent opted-in (`.managed` set) with
no suppression record, and opens/injects a handoff against the new session — bypassing
the intent of `--act-pct` because the post-`handoff_timeout` force-retry path
(referenced at `cycle.go:752`, "Gate-6 same-SID force-retry") is meant to recover an
*unresponsive* pane and does not re-check the gauge. Across a restart, that force-retry
mis-fires on a brand-new, healthy, near-empty session.

Crucially, none of the existing guards catch this because **none of them validate the
`.managed` session-id against the current session, or the `.cycle` state, at keeper
boot** (`cmd/.../keeper` arms the watcher and calls `RecoverFromCrash` but does not
purge or reconcile stale cross-generation state first).

## Trigger / reproduction

1. Run a keeper for agent `X`; let a handoff cycle reach `phase: aborted`
   (`reason: handoff_timeout`) — or just have any non-`complete` `.cycle` + a set
   `.managed` on disk.
2. Kill the keeper process and its tmux **without** deleting
   `.harmonik/keeper/X.{cycle,managed,lock}`.
3. Launch a new Claude session for `X` and arm a new keeper against it.
4. Observe: within seconds the new keeper injects `/session-handoff` into the fresh,
   near-empty session.

This is exactly what a fleet/daemon-upgrade restart does today: it tears down the
keeper *process* but never purges keeper *state*.

## Proposed fix (minimal)

1. **Validate identity at keeper boot.** Before arming the watcher, if `.managed`
   holds a session-id that does not match the keeper's current session (or `.cycle` is
   in a non-`complete` state from a prior generation), purge the stale
   `.cycle`/`.managed`/`.lock` and start clean. A keeper should never inherit another
   generation's in-flight cycle.
2. **Restore suppression from an `aborted` journal in `RecoverFromCrash`.** For
   `aborted` (at least `handoff_timeout`), re-establish `lastFiredSID` /
   `seenLowPctAfterLastFire` from the journal instead of treating it as
   "nothing to recover" — so a relaunched keeper does not immediately re-fire.
3. **Never open a handoff unless the current gauge ≥ `act-pct`** (or an explicit
   PreCompact/force trigger). The post-`handoff_timeout` force-retry should require the
   *same* session-id it originally fired against; on a new session-id it must reset, not
   force.

Any one of (1)–(3) breaks the incident; (1) is the smallest and most direct.

## Companion launcher gap (not a harmonik-core bug, but the trigger)

The operator-side teardown (a fleet-restart script and `captain-launch.sh`) kills the
keeper process/tmux but does not `rm` `.harmonik/keeper/<agent>.{cycle,managed,lock}`.
Until the keeper self-heals per the fix above, teardown should purge keeper state for
the agent before relaunch. Noting here for cross-reference; the durable fix belongs in
the keeper.

## References

- `internal/keeper/cycle.go:741-742` — in-memory suppression set on abort
- `internal/keeper/cycle.go:744-759` — abort-path `.managed` clear gated on
  `!currentSessionIDSince.IsZero()`
- `internal/keeper/cycle.go:915-916` — `RecoverFromCrash` treats `aborted` as terminal
- Related guard prior art: hk-4f8 (no-re-arm fix), hk-ibb (gate abort-clear),
  hk-qoz (force-restart escalation), DEFECT-4 (re-fire suppression)
