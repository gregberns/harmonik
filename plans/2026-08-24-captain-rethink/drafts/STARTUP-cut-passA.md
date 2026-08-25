---
name: captain-startup
description: The captain's boot runbook, plus the ops-monitor map, the crew liveness sweep, the keeper WARN path, and park/wake.
---

<!-- Generated from cmd/harmonik/assets/skills/captain/STARTUP.md
     Edit there and mirror in the same commit; scripts/skill-mirror-check.sh fails on drift. -->

# Captain boot runbook

Every session. Live state beats the handoff. `SKILL.md` owns the loop after boot.

## 0 — Identity and CWD

```bash
echo "agent=$HARMONIK_AGENT  cwd=$(pwd)"   # expect: captain, $HARMONIK_PROJECT
```

Pass the resolved value literally on every comms call; `export` does not survive between tool
calls. `--from` on **send**; `--agent` on **recv / who / log**, where `--from` is a sender
filter returning only your own messages. Two live sessions claiming `captain` freeze the
fleet. Verb before flags, or exit 2.

## 0a/0b/0c — Tier files

```bash
cat .harmonik/context/project.yaml       # phase, forbidden_actions, locked_decisions
cat .harmonik/context/captain-lanes.md   # lane table, operator initiatives, parked
cat .harmonik/context/direction-log.md   # WHAT / WHY / RETURN-PATH / expires:
```

Before skills or the handoff. **RETURN-PATH is ground truth for sequencing.** Past `expires:`
lapses to the standing autonomous posture, never to a hold. No `project.yaml` ⇒ phase
`operational`, locked decisions from `STATUS.md`. No `captain-lanes.md` ⇒ derive lanes.

## 1 — Load your slice

This file, `SKILL.md`, **orchestrator-rules**. Everything else on demand.

## 2 — Ground-truth live state

```bash
scripts/captain-boot-digest.sh        # in-repo, portable; --project DIR optional
```

- **Exit 17 from any daemon RPC = daemon DOWN.** `queue status` printing "(no queue active)"
  is up-but-idle.
- **Sweep `harmonik queue list --json` and read `status`** — the digest under-reports.
  `paused-by-drain` → `queue resume --queue <name>`. `paused-by-failure` → only
  `queue recover --queue <name>`.

Daemon down: let the `hk-daemon-supervise` session win — backoff can delay the socket bind
past a minute, and a hand-launched daemon races the pidfile. No session, backoff elapsed,
still no socket ⇒ `harmonik supervise start` (`docs/daemon-redeploy.md` to swap a binary).

## 3 — Reconcile crews

| Signature | → |
|---|---|
| listed ∧ `who` ∧ tmux ∧ epic ∧ recently dispatched | HEALTHY — keep |
| listed ∧ tmux ∧ **not** `who` past the 120s TTL | ZOMBIE — `crew stop`, re-establish in 5 |
| listed ∧ **not** tmux ∧ **not** `who` | GHOST — `crew stop` |
| listed ∧ `who` ∧ epic ∧ **dispatched nothing** | IDLE — re-task over comms, do NOT `crew stop` |
| tmux window named `.../worktrees/<uuid>` | daemon bead worktree — leave it; not a crew |

`harmonik crew stop <name>` drops the registry record, the pane, and the keeper marker;
`--pause-queue` only on operator request. **A name or queue collision exits non-zero: report
it, never rename, never retry.** Find the holder — live ⇒ lane covered; dead ⇒ zombie,
re-establish under the **same** name.

## 4 — Plan before you dispatch

The feed is your direction's call: operator and admiral initiatives, the active plan's order,
dated directives in `captain-lanes.md`, the RETURN-PATH. **Mark keystone-gated beads
BLOCKED** — a bead depending on an open epic or an in-flight keystone insta-fails at dispatch
(`group_failure`, no `run_started`). Surface the plan; do not block on a reply.

## 5 — Establish and verify every lane

Per lane. `crew start` exiting 0 is not verification.

**5·0** No ready work under the lane's epic or `codename:` label, and no in-flight run ⇒
PARKED; skip 5a–5d.

**5a** Write and commit `.harmonik/crew/missions/<crew>.md` FIRST — schema `SKILL.md` §3.

**5b/5c**
```bash
br update <epic_id> --assignee <crew>   # metadata-only; NOT a terminal transition
harmonik crew start <crew> --queue <crew>-q --mission .harmonik/crew/missions/<crew>.md
# 0  → session_id printed (do NOT persist it)
# 17 → daemon down → Step 2
# other → collision (Step 3) or launch failure: post the exact error, diagnose, retry
```
A live crew that needs a new epic is a comms re-task, not a `crew start` — `SKILL.md` §4.

**5d** Both axes, or the lane has not passed:
```bash
# (a) online. Capture first: piping `comms who --json` to grep can lose it to SIGPIPE.
who="$(harmonik comms who --json)"; grep -q '"agent":"<crew>"' <<<"$who" || echo "NOT ONLINE"

# (b) pane-truth — expect comms join, "crew <crew> online owning <epic>", a queue submit.
tmux capture-pane -p -t harmonik-<hash>-crew-<crew>:hk-crew-<crew> | tail -25
```
(a) failing past ~120s ⇒ re-drive. (a) passing with (b) wedged at a prompt ⇒ clear-and-retype
(§sweep). Boot completes when every planned lane passes 5d or is explicitly parked.

## 6 — Arm the watchers, then monitor

```bash
harmonik comms recv --agent captain --follow --json   # operator direction + crew status
harmonik subscribe --types epic_completed --json      # structural completion trigger
```

Exactly these two, deduped on `event_id`. Re-arm the first on timeout, the second on daemon
restart. **No run-level subscribe** — heartbeats re-invoke you every minute; a short
diagnostic one during an incident is fine.

Poll for ready work at least every 5 minutes between events. That pull runs only while awake;
a dormant captain is woken by push only.

### Ops-monitor

`jq '.checks' .harmonik/ops-monitor/latest.json` — six checks, every 5 min. Act on flagged
items only. Missing file, or `ts` over 15 min stale, means the schedule is down.

- **paused-queues** and **crew-fresh** under-report; green is not evidence. `paused-queues`
  fires only when the owning crew is online and guesses the crew by stripping a trailing `-q`.
  Sweep both yourself.
- **review-gate** → a run completed with no reviewer verdict; surface `.review_bypass_run_ids`.

### Crew liveness sweep

Every 15–20 min while crews are staffed, `tmux capture-pane -p -t <session>:1` each crew.
Healthy is an advancing spinner or an empty `❯ ` box. Stable non-whitespace after `❯ ` with no
spinner, persisting across a re-sample ~15s later, is a wedge — nothing else types into a crew
pane, and the bus cannot see it (`SKILL.md` §6).

```bash
tmux send-keys -t <session>:1 C-u                     # a bare Enter on a stale buffer often
tmux send-keys -t <session>:1 -l "<fresh directive>"  # fails to register; clear and retype
tmux send-keys -t <session>:1 Enter
tmux capture-pane -p -t <session>:1 | tail -5         # confirm: spinner up, box EMPTY
```

For a dead wake-trigger, re-drive with the next directive or re-point it at its queue.

### Keeper

**On WARN:** ack in one line and keep working. At the next clean idle point (no `.dispatching`
in flight) write `HANDOFF-captain.md` including the `<!-- KEEPER:<nonce> -->` line, run
`harmonik keeper restart-now --agent captain`, keep the turn open, stop typing. **Never
`/quit` or self-terminate** — this overrides any injected `/quit` advisory. A crew's keeper
restart is a non-event; do not re-`crew start` one that cycled.

**Resume after a cycle** is lean. Re-drain FIRST with `harmonik comms recv --json`, which
drains and exits — not `--follow | head`, whose SIGPIPE reports as a failed drain. Re-read
0a/0b/0c, run the digest once, trust cached tier state, go to Steps 5–6. Re-arm both watchers;
keeper arming survives the cycle, the watchers do not.

## Park / wake

`specs/park-resume-protocol.md` §3.3, §4.1. A `park` message (topic `park`, from `daemon`) on
the comms watcher, which then exits 0, means the fleet is drained; a code-0 exit without that
line just means re-arm. **Park:** stop the subscribe, re-arm neither watcher, `crew stop` each
crew, idle until the daemon nudges you. The captain does not self-exit. **Wake:** Steps 1–6 fresh.
