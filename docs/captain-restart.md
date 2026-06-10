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

## Mechanism — the SAME session-keeper used for crews, pointed at the captain

The captain already runs in a **named tmux session `captain`** (the hard prereq for
`/session-resume` to work non-interactively). So the keeper applies directly:

1. **Gauge:** the keeper statusline writes `.harmonik/keeper/captain.ctx`
   (pct/tokens/session_id) each turn.
2. **Watcher:** polls ~5s; at the act threshold it runs the cycle.
3. **Cycle (nonce-safe):** truncates the handoff → injects `/session-handoff` →
   **polls for the handoff nonce** → ONLY THEN `/clear` → `/session-resume captain`.
   The invariant the crew test validates — *never `/clear` without a confirmed
   handoff* — is exactly what protects the captain too.

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

## Enablement steps (do AFTER the crew keeper test passes)

```
harmonik keeper enable captain --tmux captain --yes-destructive   # creates .managed, wires hooks
harmonik keeper doctor captain                                     # expect all green
harmonik keeper --agent captain --tmux captain                    # start watcher (warn-only Phase-1)
```

Then verify a forged-gauge dry run aborts safely (no /clear without nonce) on the
captain exactly as the crew test did, before trusting Phase-2.

## Open question for the operator

Who restarts the captain if the keeper *itself* fails (the keeper is un-dogfooded
on the captain)? Interim backstop: the hourly heartbeat cron + a fresh `HANDOFF.md`
kept current — a human (or a cron) can always `/session-resume captain` manually.
That backstop is live TODAY regardless of keeper enablement.
