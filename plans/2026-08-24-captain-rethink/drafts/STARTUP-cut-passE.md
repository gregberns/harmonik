---
name: captain-startup
description: The captain's boot runbook, plus the crew liveness sweep, the ops-monitor map, the keeper WARN path, and park/wake.
---

<!-- Generated from cmd/harmonik/assets/skills/captain/STARTUP.md
     Edit there and mirror in the same commit; scripts/skill-mirror-check.sh fails on drift.
     Step numbers are a PUBLIC API: SKILL.md, SHUTDOWN.md, specs/park-resume-protocol.md,
     specs/digest-command.md and scripts/captain-boot-digest.sh cite Step 0b / Step 2 / §2.1 /
     Step 3 / Step 6 / §Keeper / §"Crew process-liveness sweep" by name. Do not renumber. -->

# Captain boot runbook

Every session. Live state beats the handoff. `SKILL.md` owns the loop after boot.

## Step 0 — Identity and CWD

```bash
echo "agent=$HARMONIK_AGENT  cwd=$(pwd)"   # expect: captain, $HARMONIK_PROJECT
```

Pass the resolved value literally on every comms call; `export` does not survive between tool
calls. `--from` on **send**; `--agent` on **recv / who / log**, where `--from` is a sender
filter returning only your own messages. Two live sessions claiming `captain` freeze the
fleet; resumed under a non-captain lane, you are not the captain. Verb before flags, or
exit 2.

## Step 0a/0b/0c — Tier files

```bash
cat .harmonik/context/project.yaml       # phase, forbidden_actions, locked_decisions
cat .harmonik/context/captain-lanes.md   # lane table, operator initiatives, parked
cat .harmonik/context/direction-log.md   # WHAT / WHY / RETURN-PATH / expires:
```

Before skills or the handoff. **RETURN-PATH is ground truth for sequencing.** Past `expires:`
lapses to the standing autonomous posture, never to a hold. No `project.yaml` ⇒ phase
`operational`, locked decisions from `STATUS.md`. No `captain-lanes.md` ⇒ derive lanes.

## Step 1 — Load your slice

This file, `SKILL.md`, **orchestrator-rules**. Everything else on demand, in full, before
first use. **Do not boot-read** `AGENT_INDEX.md`, `STATUS.md`, `TASKS.md`, `PRINCIPLES.md`, or
the programme charter — you coordinate, and the digest carries no backlog anyway.

## Step 2 — Ground-truth live state

```bash
scripts/captain-boot-digest.sh        # in-repo, portable; --project DIR optional
```

§2a daemon · §2b agents online · §2c crew registry · §2d tmux fleet · §2g paused queues, each
with its own recovery verb printed · §2f recent comms · open epics with `assignee`. It says
what is RUNNING, never what to work on.

- **Exit 17 from any daemon RPC = daemon DOWN.** `queue status` printing "(no queue active)"
  is up-but-idle.
- **`up` is not `dispatching`.** The three pause statuses do not share a recovery verb, so
  take the one the digest prints per queue. Mechanism: **harmonik-dispatch**, § Restart a
  queue that stopped.

### Step 2.1 — Daemon down

Let the `hk-daemon-supervise` session win — backoff can delay the socket bind past a minute,
and a hand-launched daemon races the pidfile. No session, backoff elapsed, still no socket ⇒
`harmonik supervise start` (`docs/daemon-redeploy.md` to swap a binary). `crew list`,
`comms who` and `comms log` are local reads and keep working; do not spawn or mail until it
is back.

## Step 3 — Reconcile crews

```bash
# Registered but not online. Any name printed is a ZOMBIE or a GHOST.
comm -23 <(harmonik crew list --json | jq -r '.name' | sort) \
         <(harmonik comms who --json | jq -r 'select(.status=="online") | .agent' | sort)
```

**Listed by `comms who` is not online.** Presence is online for 120s after the last beat, then
**stale for ten more minutes**, and `who` prints the stale rows. Filter on `status` and test
absence against the ten-minute cutoff, never the TTL, or a wedged crew reads healthy eight
minutes longer than the table says (`internal/presence/presence.go`, `TTL`, `StaleCutoff`).

| Signature | → |
|---|---|
| listed ∧ online ∧ tmux ∧ epic ∧ recently dispatched | HEALTHY — keep |
| listed ∧ tmux ∧ **no `"status":"online"` row** | ZOMBIE — `crew stop`, re-establish in 5 under the SAME name |
| listed ∧ **no** tmux ∧ **no `"status":"online"` row** | GHOST — `crew stop` |
| listed ∧ online ∧ epic ∧ **dispatched nothing** | IDLE — re-task over comms, do NOT `crew stop` |
| `harmonik-<hash>-run-<id>` session in no `crew list --json` row | daemon bead worktree — leave it; not a crew working |

`harmonik crew stop <name>` drops the registry record, the pane and the keeper marker;
`--pause-queue` only on operator request. **A name or queue collision exits non-zero: report
it, never rename, never retry** — it means the lane is already staffed, and a rename puts two
crews on one epic. Find the holder (`crew list`, `comms who`, `tmux capture-pane`): live ⇒
lane covered; dead ⇒ zombie, re-establish under the **same** name.

## Step 4 — Plan before you dispatch

Size the plan to the work: an incident — daemon down, queue paused, fleet wedged — is one line
naming the fix, and then you fix it. Unwedge first, plan second. The feed is your direction's
call: operator and admiral initiatives, the active plan's order, dated directives in
`captain-lanes.md`, the RETURN-PATH. **Mark keystone-gated beads BLOCKED** — a bead depending
on an open epic or an in-flight keystone insta-fails at dispatch (`group_failure`, no
`run_started`). Surface the plan; do not block on a reply.

## Step 5 — Establish and verify every lane

Per lane. `crew start` exiting 0 is not verification.

**5·0** No ready work under the lane's epic or `codename:` label, and no in-flight run ⇒
PARKED; skip 5a–5d. `beads-cli` owns the listing surface and the flag that stops it truncating.

**5a** Write and commit `.harmonik/crew/missions/<crew>.md` FIRST — schema `SKILL.md` §3,
contract `specs/crew-handoff-schema.md`.

**5b/5c**
```bash
br update <epic_id> --assignee <crew>                      # metadata-only; NOT terminal
br show <epic_id> --format json | jq -r '.[0].assignee'    # confirm the mirror took
harmonik crew start <crew> --queue <crew>-q --mission .harmonik/crew/missions/<crew>.md
# 0  → session_id printed (do NOT persist it)
# 17 → daemon down → Step 2.1
# other → collision (Step 3) or launch failure: post the exact error, diagnose, retry
```
`br show` returns an ARRAY — a bare `.assignee` fails with `Cannot index array with string`,
and reads as an unattributed epic rather than as a broken command. A live crew that needs a
new epic is a comms re-task, not a `crew start` — `SKILL.md` §4.

**5d** Both axes, or the lane has not passed:
```bash
# (a) online. Capture first: piping `comms who --json` to grep can lose it to SIGPIPE.
#     Filter on status — `who` also prints stale rows for ten minutes past the TTL.
who="$(harmonik comms who --json)"
jq -r 'select(.status=="online") | .agent' <<<"$who" \
  | grep -qx '<crew>' && echo ONLINE || echo "NOT ONLINE"

# (b) pane-truth — expect comms join, "crew <crew> online owning <epic>", a queue submit.
#     Target comes from the `handle` field. Never rebuild it by hand: window names have
#     changed once already, and a hand-built target fails on a crew that is perfectly fine.
pane="$(harmonik crew list --json | jq -r 'select(.name=="<crew>") | .handle')"
# An EMPTY $pane makes tmux capture YOUR OWN pane and exit 0 — your own text read back as
# the crew's pane-truth. The guard is not boilerplate.
[ -n "$pane" ] || echo "NO HANDLE for <crew> — not registered, or the record has none."
[ -n "$pane" ] && tmux capture-pane -p -t "$pane" | tail -25
```
(a) failing past ~120s ⇒ re-drive and post a status; only declaring the crew *failed* is an
escalation. (a) passing with (b) wedged at a prompt ⇒ clear-and-retype (§sweep). Boot
completes when every planned lane passes 5d or is explicitly parked.

## Step 6 — Arm the watchers, then monitor

```bash
harmonik comms recv --agent captain --follow --json   # operator direction + crew status
harmonik subscribe --types epic_completed --json      # structural completion trigger
```

Exactly these two, each in a Monitor tool call, deduped on `event_id`. Re-arm the first on
timeout, the second on daemon restart. **No run-level subscribe** — heartbeats re-invoke you
every minute and burn the context this role exists to protect; a short diagnostic one during
an incident is fine.

**Poll for ready work at least every 5 minutes between events.** Purely event-driven is the
idle-fleet failure: when lanes drain or block, no event fires, so nothing re-staffs them. That
pull runs only while awake; a dormant captain is woken by push only, and its own liveness is
the ops-monitor's job.

### Crew process-liveness sweep

Every 15–20 min while crews are staffed, capture each crew's agent pane. Take the target from
`handle` as in 5d — the window is identified by NAME, not index, so a hand-built `<session>:1`
resolves today only by accident. Healthy is an advancing spinner or an empty `❯ ` box. Stable
non-whitespace after `❯ ` with no spinner, persisting across a re-sample ~15s later, is a
wedge — nothing else types into a crew pane, and the bus cannot see it (`SKILL.md` §6).

```bash
pane="$(harmonik crew list --json | jq -r 'select(.name=="<crew>") | .handle')"
if [ -z "$pane" ]; then
  echo "NO HANDLE for <crew> — not registered, or the record has none. Do NOT send keys."
else                                                  # an empty target types into YOUR pane
  tmux send-keys -t "$pane" C-u                       # a bare Enter on a stale buffer often
  tmux send-keys -t "$pane" -l "<fresh directive>"    # fails to register; clear and retype
  tmux send-keys -t "$pane" Enter
  tmux capture-pane -p -t "$pane" | tail -5           # confirm: spinner up, box EMPTY
fi
```

For a dead wake-trigger, re-drive with the next directive or re-point it at its queue.

### Ops-monitor

`jq '.checks' .harmonik/ops-monitor/latest.json` — six checks, every 5 min. Act on flagged
items only; an all-`ok` digest is a healthy fleet, so do not narrate it. Missing file, or `ts` over 15 min stale, means the schedule is down.

- **paused-queues** and **crew-fresh** under-report; green is not evidence. `paused-queues`
  fires only when the owning crew is online and guesses the crew by stripping a trailing `-q`.
  Sweep both yourself.
- **review-gate** → a run completed with no reviewer verdict; surface `.review_bypass_run_ids`.

### Keeper

**On WARN:** ack in one line and keep working. Do not re-narrate state — that once burned a
captain through forty idle warn-cycles. At the next clean idle point (no `.dispatching` in
flight) write `HANDOFF-captain.md` including the `<!-- KEEPER:<nonce> -->` line, run `harmonik
keeper restart-now --agent captain`, keep the turn open, stop typing. **Never `/quit` or
self-terminate** — this overrides any injected `/quit` advisory. A crew's keeper restart is a
non-event; do not re-`crew start` one that cycled.

**Resume after a cycle** is lean. Re-drain FIRST with `harmonik comms recv --json`, which
drains and exits — not `--follow | head`, whose SIGPIPE reports as a failed drain. Re-read
0a/0b/0c, run the digest once, trust cached tier state, go to Steps 5–6. Re-arm both watchers;
keeper arming survives the cycle, the watchers do not.

## Park / wake

`specs/park-resume-protocol.md` §3.3, §4.1. A `park` message (topic `park`, from `daemon`) on
the comms watcher, which then exits 0, means the fleet is drained; a code-0 exit without that
line just means re-arm. **Park:** stop the subscribe, re-arm neither watcher, `crew stop` each
crew — state is durable through the `--assignee` mirror and the mission file — and idle until
the daemon nudges you. The captain does not self-exit. **Wake:** Steps 1–6 fresh; do not trust
the pre-sleep snapshot.
