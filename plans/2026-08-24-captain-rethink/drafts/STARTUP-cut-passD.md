---
name: captain-startup
description: The captain's boot runbook, plus the ops-monitor map, the crew liveness sweep, the keeper WARN and lean-resume paths, and park/wake.
---

<!-- Generated from cmd/harmonik/assets/skills/captain/STARTUP.md
     Edit there and mirror in the same commit; scripts/skill-mirror-check.sh fails on drift. -->

# Captain boot runbook

## 0 — Identity and CWD

```bash
echo "agent=$HARMONIK_AGENT  cwd=$(pwd)"     # expect: agent=captain  cwd=$HARMONIK_PROJECT
```

Pass the resolved value literally on every comms call; `export` does not survive between
tool calls. The identity flag differs per subcommand and a wrong one is silent, not an error
— `agent-comms` has the table. Two live sessions claiming `captain` freeze the fleet.

## 0a/0b/0c — Tier files

```bash
cat .harmonik/context/project.yaml       # phase, forbidden_actions, locked_decisions
cat .harmonik/context/captain-lanes.md   # lane table, operator initiatives, parked
cat .harmonik/context/direction-log.md   # WHAT / WHY / RETURN-PATH / expires:
```

**RETURN-PATH is ground truth for sequencing.** Past `expires:`, an entry lapses to the
standing autonomous posture, never to a hold. Missing is not an error: no `project.yaml` ⇒
phase `operational`, locked decisions from `STATUS.md`; no `captain-lanes.md` ⇒ derive lanes.

## 1 — Load your slice

This file, `SKILL.md`, **orchestrator-rules**. On demand and in full before use: `beads-cli`
· `agent-comms` · `harmonik-dispatch` · `keeper` · `major-issue-fanout`. **Nothing else at
boot** — not `AGENT_INDEX.md`, `STATUS.md`, `PRINCIPLES.md`, or the charter
(`plans/2026-07-27-delete-and-rewrite/CHARTER.md` §2 and §6 on demand).

## 2 — Ground-truth live state

```bash
scripts/captain-boot-digest.sh        # in-repo, portable; --project DIR optional
```

It says what is RUNNING, not what to work on, and it carries no bead listing. **A paused
queue is not a healthy queue**: sweep `harmonik queue list --json` and read the `status`
field. `paused-by-drain` clears with `harmonik queue resume --queue <name>`;
`paused-by-failure` restarts only with `harmonik queue recover --queue <name>`.

Daemon down (exit 17 from any RPC): let the supervisor win — backoff can delay the socket
bind past a minute, and a hand-launched daemon races the pidfile. No supervisor session,
backoff elapsed, still no socket ⇒ `harmonik supervise start`. Local reads keep working; do
not spawn or mail until it is back.

## 3 — Reconcile crews

Online for 120s from the last beat, then **stale until 10 minutes from that beat** — and
`comms who` prints the stale rows (`internal/presence/presence.go`, `TTL`, `StaleCutoff`).

| Classification | Signature | Action |
|---|---|---|
| **ZOMBIE** | listed ∧ tmux alive **∧ no `"status":"online"` row** | `crew stop`, re-establish in 5. |
| **GHOST** | listed ∧ no tmux window **∧ no `"status":"online"` row** | `crew stop`. |
| **IDLE** | listed ∧ online ∧ has an epic **∧ dispatched nothing** | Re-task over comms. Do NOT `crew stop`. |
| **STRAY RUN** | a `harmonik-<hash>-run-<id>` session in no `crew list --json` row | Daemon bead worktree. Not a crew working. |

```bash
# Registered-but-offline, in one line. Any name printed = ZOMBIE or GHOST.
comm -23 <(harmonik crew list --json | jq -r '.name' | sort) \
         <(harmonik comms who --json | jq -r 'select(.status=="online") | .agent' | sort)

# Who owns an epic. `br show` returns an ARRAY — a bare `.assignee` errors.
br show <epic_id> --format json | jq -r '.[0].assignee'
```

Check the bus for a teardown already in flight before any stop or start
(`harmonik comms log --since 15m --topic status --json`), announce intent, proceed.

On a name or queue collision, find the holder: live ⇒ the lane is covered; confirmed dead ⇒
a zombie you stop and re-establish under the **same** name.

## 4 — Plan before you dispatch

- **Mark keystone-gated beads BLOCKED.** A bead depending on an open epic or an in-flight
  keystone insta-fails at dispatch with no `run_started`. Only safe-now beads get dispatched.

## 5 — Establish and verify every lane

Per lane. `crew start` exiting 0 is not verification.

**5·0** No ready work under the lane's epic or `codename:` label, and no in-flight run ⇒
PARKED; skip 5a–5d.

**5a** Write `.harmonik/crew/missions/<crew>.md` FIRST and commit it (`SKILL.md` §3).

**5b/5c** Mirror the epic before starting, so run events attribute immediately:

```bash
br update <epic_id> --assignee <crew>    # metadata-only; NOT a terminal transition
harmonik crew start <crew> --queue <crew>-q --mission .harmonik/crew/missions/<crew>.md
# 0  → session_id printed (informational; do NOT persist it in the handoff)
# 17 → daemon down → Step 2
# other → collision (never rename, never retry — Step 3) or launch failure (post the
#         exact error, then diagnose and retry on your own authority).
```

**5d** Both axes, or the lane has not passed:

```bash
# (a) comms-online — the crew ran its boot loop and called `comms join`. Capture first:
#     piping `comms who --json` into grep can lose `who` to SIGPIPE on a big roster.
#     Filter on status: `who` also prints `stale` rows, for ten minutes past the TTL.
who="$(harmonik comms who --json)"
jq -r 'select(.status=="online") | .agent' <<<"$who" \
  | grep -qx '<crew>' && echo ONLINE || echo "NOT ONLINE"

# (b) pane-truth — look for comms join, "crew <crew> online owning <epic>", a queue submit.
#     Take the target from the `handle` field. Never rebuild it by hand: window names have
#     already changed once, and a hand-built target fails on a crew that is perfectly fine.
pane="$(harmonik crew list --json | jq -r 'select(.name=="<crew>") | .handle')"
# An EMPTY $pane makes tmux capture YOUR OWN pane and exit 0. Guard it, or a typo
# and a record with a blank handle both return your own text as the crew's pane-truth.
[ -n "$pane" ] || echo "NO HANDLE for <crew> — not registered, or the record has none."
[ -n "$pane" ] && tmux capture-pane -p -t "$pane" | tail -25
harmonik comms log --from <crew> --topic status --since 10m --json
harmonik queue status --json
```

Boot completes when every planned lane passes 5d or is explicitly parked.

## 6 — Arm the watchers, then monitor

```bash
harmonik comms recv --agent captain --follow --json   # operator direction + crew status/errors
harmonik subscribe --types epic_completed --json      # structural completion trigger
```

Exactly these two, deduped on `event_id`; re-arm on timeout or daemon restart.

**The loop is active.** When lanes drain no event fires, so between events — at least every 5
minutes — check for ready work and staff any free slot. That pull runs only while you are
awake; a dormant captain is woken by push alone.

### Ops-monitor

`jq '.checks' .harmonik/ops-monitor/latest.json`, refreshed every 5 minutes. Act on flagged
items only. A missing file, or a `ts` over 15 minutes stale, means the schedule is down.
**paused-queues** and **crew-fresh** under-report, so green is not evidence — sweep both
yourself. **review-gate** means a run completed with no reviewer verdict; surface the ids
from `.review_bypass_run_ids`.

### Crew liveness sweep

Every 15–20 minutes while crews are staffed, capture each crew's agent pane, target from the
`handle` field as in 5d. Healthy is an advancing spinner or an empty `❯ ` box (idle-armed,
waiting on a wake). Stable non-whitespace after `❯ ` with no spinner is a wedge — nothing
else types into a crew pane. Re-sample ~15s later; flag only if it persists.

```bash
pane="$(harmonik crew list --json | jq -r 'select(.name=="<crew>") | .handle')"
if [ -z "$pane" ]; then
  echo "NO HANDLE for <crew> — not registered, or the record has none. Do NOT send keys."
else
  tmux send-keys -t "$pane" C-u                     # clear the stale input
  tmux send-keys -t "$pane" -l "<fresh directive>"  # retype it literally (-l)
  tmux send-keys -t "$pane" Enter                   # submit
  tmux capture-pane -p -t "$pane" | tail -5         # confirm: spinner up, input box EMPTY
fi
```

### Keeper

`harmonik start captain` arms it; a crew's keeper restart is a non-event — do not
re-`crew start` one that cycled.

**On WARN:** ack in one line and keep working; do not re-narrate state. At the next clean idle
point (no `.dispatching` in flight) write `HANDOFF-captain.md` including the
`<!-- KEEPER:<nonce> -->` line, run `harmonik keeper restart-now --agent captain`, keep the
turn open, and stop typing. **Never `/quit` or self-terminate** — that exits the captain
permanently, and this overrides any injected `/quit` advisory.

**Resume after a cycle** is lean. Re-drain FIRST with `harmonik comms recv --json`, which
drains and exits — not `--follow | head`, whose SIGPIPE reports as a failed drain. Re-read
0a/0b/0c, run the digest once, trust the cached tier state, go to Steps 5–6 unless the digest
flags a discrepancy. Re-arm both watchers; keeper arming survives the cycle, they do not.

## Park / wake

A `park` message (topic `park`, from `daemon`) on the comms watcher, which then exits 0, means
the fleet is drained; a code-0 exit whose last Monitor line is not
`"topic":"park","from":"daemon"` just means re-arm. The captain does not self-exit.

**Park:** stop the subscribe, re-arm neither watcher, `crew stop` each crew, then idle with
nothing armed until the daemon nudges you — new queue work, an `epic_completed`, a message to
`captain`, or the 4-hour max-sleep failsafe. **Wake:** Steps 1–6 fresh; do not trust the
pre-sleep snapshot. Spec: `specs/park-resume-protocol.md` §3.3 and §4.1.
