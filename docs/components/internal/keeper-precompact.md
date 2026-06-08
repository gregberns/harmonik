# Session-Keeper: PreCompact Backstop Hook

Bead: hk-aalsm  
Epic: hk-ekap1 (codename:session-keeper)  
Depends on: hk-22i70 (cycle core), hk-djdng (stop hook), hk-kct9t (crash recovery)

---

## Purpose

Claude Code's `PreCompact` hook fires synchronously **before** native auto-compaction
and can block it by returning `decision:block` (exit code 2). This is the **backstop**
for when the watcher's gauge-threshold cycle did not fire in time — for example, when
a single large turn jumps the context window from below `act_pct` straight into
auto-compaction territory.

For a **managed** session the keeper's intent-preserving cycle
(handoff → clear → resume) is strictly better than native (lossy) compaction. The
PreCompact hook ensures we can intercept that path.

---

## Components

### `scripts/keeper-precompact-hook.sh`

The Claude Code hook script. Add it to `~/.claude/settings.json`:

```json
"PreCompact": [
  {
    "hooks": [
      {
        "type": "command",
        "command": "HARMONIK_PROJECT=/path/to/project HARMONIK_KEEPER_AGENT=orchestrator /path/to/scripts/keeper-precompact-hook.sh"
      }
    ]
  }
]
```

Environment:

| Variable              | Description                                     |
|-----------------------|-------------------------------------------------|
| `HARMONIK_PROJECT`    | Absolute path to the project root (default: `$PWD`) |
| `HARMONIK_KEEPER_AGENT` | Agent name (default: `"default"`)             |

Exit codes:

| Code | Meaning                             |
|------|-------------------------------------|
| 0    | Fail-open: allow native compaction  |
| 2    | `decision:block`: suppress compaction; keeper will cycle |

### `internal/keeper/gates.go` — marker helpers

- `HasPrecompactTrigger(projectDir, agent)` — true when `.harmonik/keeper/<agent>.precompact` exists.
- `ClearPrecompactTrigger(projectDir, agent)` — removes the marker. Idempotent.

### `internal/keeper/cycle.go` — `Cycler.RunForPrecompact`

Called by the keeper watcher when it detects the `.precompact` marker. Runs the
cycle with a **relaxed gate set** (CrispIdle and act_pct are NOT required — the
PreCompact hook implies the context window is full and the agent may be mid-turn).

Gate order:

1. `.managed` opt-in guard (same as MaybeRun; defensive since the shell script also checks).
2. Non-empty `session_id` (required for anti-loop identity).
3. NOT `HoldingDispatch` (fail-closed; skip if orchestrator has in-flight queue work).
4. Anti-loop suppression (same policy as `MaybeRun` — suppress re-fire on the same session
   until a new session_id is observed AND pct has been seen below `warn_pct` on it).

Always emits `session_keeper_precompact_blocked` with an `action` field, then
**always clears the marker** regardless of which gate fired (see below).

### Watcher integration

`Watcher.Run` polls for the `.precompact` marker on every tick (after the normal
`MaybeRun` call). If the marker is present and a `Cycler` is configured, it calls
`RunForPrecompact`. The watcher only calls `RunForPrecompact` when it has a valid
gauge file (a `nil` ctxFile would prevent session_id identification).

---

## Can't-Cycle Fallback Policy

**Problem:** The `PreCompact` hook runs synchronously inside the claude process.
The keeper is an async poller. If the hook blocks compaction (exit 2) but the
keeper cannot cycle (e.g., `HoldingDispatch`, watcher not running, or anti-loop
suppression), the session would be stuck: compaction blocked AND no cycle.

**Resolution: block at most once per compaction wave.**

1. Hook writes `.precompact` marker → exits 2 (blocks compaction).
2. Keeper detects marker on next poll tick:
   - If cycle can run → emits `"cycle_triggered"` action, clears marker, runs cycle.
   - If can't cycle (dispatch, anti-loop, etc.) → emits appropriate action, clears marker.
3. If Claude Code fires `PreCompact` again while the marker still exists (i.e., the
   keeper has NOT yet had a tick to detect it), the hook exits 0 (fail-open).
4. After the keeper clears the marker (step 2), each new `PreCompact` fire gets a
   fresh chance to block again.

**Result:** The session can never be permanently wedged by the hook. In the worst
case — keeper is down or perpetually blocked — the second `PreCompact` fire falls
through to native compaction. One complete cycle (or one bounded fail-open) per
compaction wave.

### Event: `session_keeper_precompact_blocked`

Emitted by `RunForPrecompact` once per marker detection, before clearing. The
`action` field records the decision:

| `action`               | Meaning                                                  |
|------------------------|----------------------------------------------------------|
| `cycle_triggered`      | All gates passed; cycle was started.                     |
| `hold_dispatch_skip`   | `HoldingDispatch` was true or `session_id` was empty.    |
| `anti_loop_suppressed` | Anti-loop gate suppressed re-fire on this session.       |
| `not_managed`          | `.managed` marker was absent (defensive; shell script already checks). |

Durability class: O (ordinary — observability).

---

## Wire-Up Checklist

To opt a managed agent into the PreCompact backstop:

1. Ensure `.harmonik/keeper/<agent>.managed` exists (same opt-in as Phase-1/2 watcher).
2. Add `keeper-precompact-hook.sh` to `~/.claude/settings.json` under `PreCompact` hooks
   (see usage example above).
3. Start `harmonik keeper --agent <agent> ...` as usual — the watcher picks up
   `.precompact` markers automatically when a `Cycler` is configured.

No additional flags are required. The precompact path is automatically active
whenever the keeper watcher has a Cycler and the agent is managed.
