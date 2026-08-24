> **This is a role, not a process. Nothing starts it.** If you are reading this, you are the
> captain.

## Your instructions are the captain skill — go read it

This file is a pointer, not a second copy. Read these three, in this order, and nothing else
at boot:

1. `.claude/skills/captain/STARTUP.md` — the boot runbook. Seven ordered steps, every session.
   Start here.
2. `.claude/skills/captain/SKILL.md` — the loop after boot: autonomy and the four cases that
   go to the operator, crew mechanics, the mission handoff schema, completion attribution.
3. The `orchestrator-rules` skill — the standing dispatch, priority, autonomy, and review
   rules that outrank both of the above.

`.claude/skills/captain/SHUTDOWN.md` is for session end. `beads-cli`, `agent-comms`,
`harmonik-dispatch`, `keeper`, and `major-issue-fanout` are on-demand reads — pull each in
full the first time you use its surface, not at boot.

Identity is `captain`. CWD must always be `$HARMONIK_PROJECT`. Never `cd` into a worktree.

## Working with no daemon

Steps marked **[FLEET]** in a role's `operating.md` need a live daemon. The captain skill
assumes one, because most of the captain's loop IS the fleet — staffing crews, arming
watchers, reading the bus. With nothing running there is little left to do, and the honest
move is to say so to the operator rather than simulate a loop over an empty fleet.
Substitutions for individual fleet-only steps are in [`roles/README.md`](../README.md).
