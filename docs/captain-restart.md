# Captain self-restart (context management for the orchestrator)

> How the **captain** LLM session is saved-and-restarted when its own context
> window fills — so the fleet keeps running indefinitely without a human in the
> loop. Written 2026-06-10 per operator directive ("figure out how you the captain
> are going to be restarted"). Gated on the session-keeper crew test passing.

## Problem

The captain is a long-running Claude Code session that orchestrates crews. Over a
multi-hour session its context fills (this is the same ~200k-token wall crews hit).
Unlike a crew, the captain can't be trivially replaced — it holds orchestration
state (which crews are live, their epics/queues, armed watchers, the task list,
pending operator directives). It must be saved-and-restarted **without losing that
state and without stopping mid-critical-op**.

## Two restart paths (ON-059, hk-wjzf, hk-xjlq)

The keeper supports two cycle-trigger paths for the captain:

### Watcher path (passive — keeper-initiated)

The keeper polls the gauge every 5s and at the **act threshold** (≥215k tokens /
`--act-pct`, CrispIdle, no HoldingDispatch) it runs the automatic cycle:
handoff-inject → nonce-poll → `/clear` → `/session-resume captain`.

On this path the keeper **injects `/session-handoff`** itself (the captain does not
need to act). The nonce-confirmed invariant still applies.

### Request path (active — captain-initiated, ON-059)

The captain can **trigger the cycle early** at a clean idle point rather than
waiting for the act threshold. This is the preferred mechanism because the captain
can choose a moment that is not mid-dispatch or mid-crew-spawn.

**Procedure:**

1. At a clean idle point (no `.dispatching` in flight, not mid crew-spawn/merge/submit):
2. Write `HANDOFF-captain.md` (the captain mints the nonce via `/session-handoff`
   or writes it manually as `<!-- KEEPER:<uuid> -->`).
3. Run: `harmonik keeper restart-now --agent captain [--project DIR]`
4. Keep the turn OPEN and stop typing. The keeper fires `RunOnDemand` on its next
   tick (≤5 s), which: consumes the `.restart-now` marker (once), verifies the
   nonce in `HANDOFF-captain.md`, then runs the same `/clear` → `/session-resume`
   cycle as the watcher path.

**The keeper band is UNCHANGED.** `restart-now` bypasses ONLY the act-pct idle
gate (CrispIdle check). All other safety gates intact:
- `.managed` must exist (destructive consent required)
- Nonce in `HANDOFF-captain.md` must match the marker (no `/clear` without confirmed handoff)
- `HoldingDispatch` check still defers the cycle if `keeper set-dispatching` is held

The captain **mints the nonce** on the request path by writing the handoff first.
The marker JSON (`{nonce, requested_at, session_id}`) is consumed once — a second
call with the same nonce is a no-op.

### Per-agent WARN text

When the keeper watcher crosses the WARN threshold for the captain agent
(`WatcherConfig.OnDemandRestart=true`, auto-set when `AgentName=="captain"`), it
injects the captain-specific text:

> *"[KEEPER WARNING — automated] Proactive context checkpoint — you have ample buffer remaining. Keep working. At a clean checkpoint only: write HANDOFF-captain.md (include the KEEPER nonce), then run: harmonik keeper restart-now --agent captain, keep the turn open, and stop typing. The keeper drives the clear→resume cycle."*

Crews receive the default advisory. The band is not widened for either.

## Mechanism — the SAME session-keeper used for crews, pointed at the captain

The captain already runs in a **named tmux session `captain`** (the hard prereq for
`/session-resume` to work non-interactively). So the keeper applies directly:

1. **Gauge:** the keeper statusline writes `.harmonik/keeper/captain.ctx`
   (pct/tokens/session_id) each turn.
2. **Watcher:** polls ~5s; at the act threshold (watcher path) or on the next tick
   after `restart-now` (request path) it runs the cycle.
3. **Cycle (nonce-safe):** truncates the handoff → nonce-poll → ONLY THEN
   `/clear` → `/session-resume captain`. The invariant — *never `/clear` without a
   confirmed handoff nonce* — applies on BOTH paths.

## Re-hydration — captain resumes from BOTH the handoff AND live ground-truth

This is the key difference from a crew. A crew re-hydrates from its mission +
`--assignee` mirror. The captain re-hydrates from:

- **`HANDOFF.md`** (the rich orchestration snapshot — see below), AND
- **`.claude/skills/captain/STARTUP.md`** boot runbook, which *re-measures* live
  state (`comms who`, `crew list`, `queue status`, `tmux list-windows`), **re-arms
  the watchers**, and **re-verifies every crew on both axes**.

So the captain never trusts the handoff's live claims — it re-grounds. The handoff
carries intent/plan; STARTUP re-derives reality. (This session is the proof: it
booted via `/session-resume captain` → STARTUP and re-grounded everything.)

### What the captain HANDOFF.md must capture (the orchestration state)

- Live crews: name · epic · queue · what they're churning (e.g. gurney → hk-9gkwa).
- Armed watchers (so resume re-arms them): comms-recv + subscribe(problems+epic).
- The task list state + the next autonomous actions.
- Pending operator directives (e.g. "asleep, keep working, test keeper, design
  captain restart") and anything tabled (GitHub perm, codex OAuth).
- Daemon state: version tag, concurrency, any open incident.

## Safety — never /clear the captain mid-critical-op

A mistimed `/clear` during a deploy / crew-spawn / merge would strand state. Guard:

- Before a critical op, the captain calls `harmonik keeper set-dispatching captain`
  (writes `.dispatching` → the idle-gate defers the cycle); `clear-dispatching`
  after. (Same gate the crew test exercises.)
- **Phase-1 = warn-only first:** enable the gauge + an ~80% statusline warning with
  NO auto-clear, to dogfood the signal on the captain for a few cycles. Only after
  that, and after the crew abort-case + idle-gate tests PASS, arm Phase-2 full-cycle.

## Current deployment state (2026-06-20+)

**Captain and crew keepers are LIVE.** The gauge, watcher, and auto-clear cycle are
active for both:

- `statusLine.command` wired globally in `~/.claude/settings.json` — all sessions
  write `.ctx` gauge files.
- Stop and PreCompact hook stanzas wired.
- Captain keeper started manually (or by the captain's own boot sequence).
- Crew keepers auto-armed by the daemon: `HandleCrewStart → SpawnCrewSession` adds a
  sibling `keeper` window running full force-cut mode (hk-rmy1, hk-lcga, hk-tt9q).

Verify any agent with: `harmonik keeper doctor --agent <name>`. The `live-watcher`
check uses `LiveKeeperPresent` (flock probe) — distinguishes a running keeper from a
stale corpse lockfile.

**`keeper enable --yes-destructive` rewrites the GLOBAL `~/.claude/settings.json`
statusLine** — do this deliberately (not mid-churn, with operator awareness).
