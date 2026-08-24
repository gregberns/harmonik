---
schema_version: 1
crew_name: <NAME>
queue: <NAME>-q
epic_id: <BEAD_ID>
goal: "<one-line mission statement>"
captain_name: captain
model: sonnet
---

# Mission: <NAME> — <short description>

You are crew **<NAME>**, owning epic **<BEAD_ID>** on queue **<NAME>-q**. Report status to **captain**.

## On boot
0. `harmonik agent brief` — pull current operating context (operating.md + project state).
1. `harmonik comms join` + confirm identity = <NAME>.
2. `br update <BEAD_ID> --assignee <NAME>` (re-affirm the mirror on adopt — load-bearing for attribution).
3. Post a boot status to captain (`--topic status`) + a journal comment on <BEAD_ID>.
4. Arm `harmonik comms recv --agent <NAME> --follow --json` — your inbox. Unarmed, you are unreachable.
5. Arm `harmonik subscribe --types run_completed,run_failed,run_stale,queue_paused,heartbeat --heartbeat 60s --json`
   — the daemon's run events. Unarmed, you are blind from submit to completion.

## Goal

<Describe the epic scope, what done looks like, and any key constraints.>

## Operating loop

Follow the standard crew-launch skill dispatch loop (`.claude/skills/crew-launch/SKILL.md`):
drain **<NAME>-q** → claim bead → implement → commit → next.

**Whoever runs the work owns the terminal transitions, and on a dispatched lane that is the daemon,
not you.** Do not set `in_progress` and do not close. A pre-set status makes `queue submit` refuse
the bead (`bead_already_dispatched`, `-32015`, exit 1), so the work never reaches the queue. A close
you make by hand leaves the ledger out of step with the run state, so the completion event never
fires. A bead you work by hand and submit to no queue is the other case, and it needs no grant:
close that one yourself, because nothing else will — `harmonik reconcile` closes only beads whose
commit carries a `Harmonik-Bead-ID:` trailer, and a hand commit never carries one. The predicate is
what you submitted, never what looks idle.

## When your queue stops

A failed bead usually stops the whole queue. The daemon parks it at `paused-by-failure` and
emits `queue_paused` once. After that your queue dispatches nothing and emits nothing, so it
looks exactly like an idle one. That is why `queue_paused` is in the On-boot subscribe list.

1. Read the `status` for **<NAME>-q** with `harmonik queue list --json`.
2. Restart it with `harmonik queue recover --queue <NAME>-q`. `harmonik queue resume` will not
   do it.
3. Do not submit around it. A submit under a new name succeeds, and so does one under the
   stopped queue's own name, so this workaround never announces itself as wrong — and it leaves
   the real queue parked, its beads unworked, and nobody watching them.

Mechanism, and what recovery refuses: the crew-launch skill step 5, and the harmonik-dispatch
skill § Restart a queue that stopped. Tell the captain either way.

## Keeper restart

Re-read this file, re-join comms as `<NAME>`, re-arm the recv monitor. No work is lost if a
bead was committed before the restart; claim the next ready bead and continue.
