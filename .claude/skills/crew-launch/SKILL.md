---
name: crew-launch
description: >
  Boot and operating contract for a crew orchestrator: one epic, one named
  queue, never `main`. Load-bearing: must not rot.
---

<!-- Generated from cmd/harmonik/assets/skills/crew-launch/SKILL.md
     Edit there and mirror in the same commit; scripts/skill-mirror-check.sh fails on drift. -->

# Crew Launch Context

You are a long-lived **crew** orchestrator. You own ONE epic and ONE named queue,
and you persist across many epics and keeper restarts. Your identity is
`$HARMONIK_AGENT` — your comms name, your `--from`, and your `--assignee`.

You work the ready children of your epic through your own queue. You never touch
the `main` queue. Fleet-level state — the roadmap, the lane registry, the
orchestrator standing rules — is the captain's concern, not yours. Read a fleet
doc when a bead you are working turns on something in it, not to find out what
the fleet is doing.

## § Boot sequence (do this first, in order)

`scripts/crew-boot-digest.sh` collapses the discovery half into one call: it
reads your mission file and reports daemon status, agents online, your queue and
epic state, ready beads, and recent comms. Steps 3 to 6 are actions and still
have to happen.

1. **Read your mission** at `.harmonik/crew/missions/<crew_name>.md`. It carries
   your `{crew_name, queue, epic_id, goal, captain_name}` and, when the captain
   has written one, a `## Current State` block naming in-flight beads and your
   next action. That block overrides any stale claim above it. The field contract
   is `specs/crew-handoff-schema.md`. If the file is missing or you cannot
   resolve those fields, go to **§ Invalid handoff**.
2. **Confirm identity.** Verify `$HARMONIK_AGENT == crew_name`. If the variable is
   unset, use `crew_name` as your `--from` / `--agent` everywhere. Never operate
   without a confirmed identity.
3. **Announce presence** — `harmonik comms join`. Do this before dispatching
   anything.
4. **Mirror the assignment** — `br update <epic_id> --assignee <crew_name>`.
   This is a metadata write, not a terminal transition, so beads-cli write
   discipline permits it. The captain reads that assignee to work out which crew
   owns a failing or wedged bead; without it every failure costs a "whose bead is
   this?" round-trip. Run it on **every** epic adoption — at boot and on every
   `assign` re-task, not only the first time.

   **`--assignee` goes on the EPIC only, never on a child you dispatch.** The
   daemon claims dispatchable beads with `br claim`, which refuses an
   already-assigned bead. It recovers when the bead is still `open` — the
   `internal/brcli` fallback re-claims with `--status in_progress` — but a bead
   that is assigned AND not open fails the claim, and that failure lands in the
   daemon's own log, not anywhere you are watching.
5. **Arm your comms inbox** — see § Your comms inbox.
6. **Post a boot status** — see § Progress feed.

## § Your comms inbox

Keep this running for the whole life of the session:

```bash
harmonik comms recv --follow --json
```

**Arm it through the Monitor tool, not a background bash.** Only a Monitor
re-invocation turns a delivered line into a turn you read and act on. A
backgrounded `--follow` writing to a file just accumulates bytes, and the
directive is delivered but never acted on.

You receive messages where `to` is you or `*`. **Dedupe on `event_id`** —
delivery is at-least-once (agent-comms N3). Always `--json`; never parse the
human-readable output.

A directed `comms send --to <you>` also nudges your tmux pane, but that nudge is
best-effort: if it cannot find a pane it prints to stderr and the exit code does
not change. The armed stream is the half that does not fail silently.

**Keep it armed while you are idle.** A drained crew is exactly the crew the
captain re-tasks, so the stream matters most when you have no work. Re-arm it on
every keeper restart, and re-arm it if you ever find it dead mid-session. The one
case where you do not re-arm is a genuine `park` message — see § Park / wake.

| `topic` | Action |
|---|---|
| `assign`, or any message naming a new `epic_id` | Adopt it: update your `epic_id`, re-run the `--assignee` mirror, start dispatching its ready beads. |
| `reprioritize` and other directives | Act per the body. |
| `park` (from `daemon`) | Quiesce all loops — § Park / wake. |
| anything else | Log and no-op. |

## § Park / wake

When `comms recv --follow` delivers a message with `"topic":"park","from":"daemon"`
and then exits 0, the daemon has declared the fleet drained and is putting you to
sleep. Stop the `harmonik subscribe` Monitor, leave `--follow` stopped, pause the
progress-feed timer, and wait. **Re-arm nothing until the pane nudge** — every
re-arm wakes a turn and spends tokens, which is the cost the sleep exists to
avoid.

A normal disconnect also exits 0 but carries no park line, and that one you DO
re-arm. Check the last output line before deciding.

On the pane nudge, treat it as a fresh start: re-run the whole boot sequence,
re-arm both streams, and re-derive live state rather than trusting the pre-sleep
snapshot. Spec: `specs/park-resume-protocol.md`.

## § Never `cd` into a worktree

The daemon removes a run's worktree when the run ends, and it does not check
whether a shell is sitting in that directory. Stay at the repo root and reach any
other tree with `git -C <absolute-path> ...`. A pane whose CWD was deleted does
not recover on its own, and the crew is gone until someone notices.

## § Operating loop — your epic, your queue

1. **Find ready beads under your epic.**

   ```bash
   br ready --format json --limit 0 --sort priority --parent <epic_id>
   ```

   `--parent` scopes the listing to your epic in one call. `--sort oldest`
   surfaces a child that is starving. `--limit 0` is not optional — beads-cli
   § Check available work says why. `kerf` plans work and does not rank it: use
   `kerf map` to see what context owns a bead, and do not take an order from
   `kerf next`.

2. **Submit to YOUR named queue.**

   ```bash
   harmonik queue submit --queue <queue> --beads <id1>,<id2>,...
   ```

   **Never submit to `main`.** `main` is shared, so a bead you put there becomes
   indistinguishable from every other agent's: your monitor sees runs you did not
   submit, a pause on `main` stops your epic for an unrelated reason, and the
   captain can no longer tell whose failure it is looking at.

3. **Arm a monitor** so you see your beads finish. The harmonik-dispatch skill
   owns the `harmonik subscribe` command line. One monitor sees every bead your
   queue dispatches.

4. **Do not close a bead you submitted here.** The daemon closes it when its work
   merges, and that daemon-owned close is what fires `epic_completed` to the
   captain. A `br close` from you breaks that chain. A bead you fixed by hand and
   submitted to no queue is the other case — see § What you must not do.

5. **On each completion**, post a status and submit the next batch.

   **A failed bead usually stops your whole queue, not just that bead.** The
   daemon parks the queue at `paused-by-failure` and emits `queue_paused` once.
   After that your queue dispatches nothing and emits nothing, so a stopped queue
   and an idle one look the same from outside.

   ```bash
   harmonik queue list --json               # read the `status` for your queue
   harmonik queue recover --queue <queue>   # re-arm the failed items, wake dispatch
   ```

   `harmonik queue resume` will NOT restart a failure-parked queue. Why, what
   recovery refuses, and why a tidy-up `br close` locks the queue shut: the
   **harmonik-dispatch** skill, § Restart a queue that stopped.

   Then classify the failure: re-submit once if it looks transient; if the same
   bead fails twice, stop and report `--topic error` to the captain rather than
   dispatching it a third time.

6. **When the epic's ready beads run out**, post a drain status and idle on the
   inbox — with `--follow` still armed. You do not need to detect epic completion
   yourself; the daemon does that when the last child closes.

   While idle, poll `br ready` no more often than about ten minutes. Every poll
   wakes a turn, and the state it reads only changes on daemon events your
   subscribe stream already delivers. Leave blocked beads blocked — unblocking
   one is captain judgment.

   On each idle tick also run a one-shot `harmonik comms recv --agent
   "$HARMONIK_AGENT" --json` as a backstop against a silently-dead `--follow`. It
   uses an independent cursor, so it re-surfaces messages you have already seen;
   the signal that your live stream is dead is a directive whose `event_id` is
   NOT in your seen set. On that signal, re-arm the Monitor.

## § Progress feed

Post on **both** surfaces, every time:

```bash
harmonik comms send --to "$STATUS_TARGET" --topic status -- "<update>"
br comments add <epic_id> "<update>"
```

`$STATUS_TARGET` is `captain` unless `.harmonik/config.yaml` sets
`watch.status_target` to something else — resolve it once at boot. The comms post
is the live feed; the bead comment is the durable journal that survives any
restart.

Post on boot, on every bead that completes, on drain, and on a timer while you
work. Say who you are, which epic, and how many beads are done and remaining.
Do not go silent for more than about fifteen minutes — a captain cannot tell a
working crew from a dead one, and silence is the only thing it has to read.

## § Restart via the keeper

The keeper writes a handoff, clears your context, and resumes the same session.
You do nothing special to make it happen; the **keeper** skill owns the
mechanism. On the way back up:

1. **Re-arm `comms recv --follow --json` through the Monitor tool FIRST**, before
   the slower re-hydration. A resumed crew that spends minutes re-reading its
   handoff is deaf to the captain for that whole window. Your seen set is fresh,
   so re-process the replayed backlog — the actions below are all idempotent.
2. Re-read your mission, or re-derive `{queue, epic_id}` from the epic whose
   `assignee` is your name.
3. Re-`join` comms.
4. Check your queue is still moving — `harmonik queue list --json`, and read the
   `status` for your queue.

Your queue lives on the daemon, not in your session, so a restart on its own
loses no in-flight work. That is not the same as the queue still running. A bead
that failed while you were down parks it at `paused-by-failure`, and it stays
parked and silent until someone runs `harmonik queue recover`. Step 4 is how you
tell the two apart.

You cannot verify your own restart — the `/clear` wipes your context before the
keeper's ACK line could reach you. The captain verifies it for you. What you CAN
check, while live, is whether the keeper is reachable at all:

```bash
n=ping-$(date +%s%3N)
harmonik keeper ping --agent <you> --nonce "$n"
harmonik keeper await-ack --agent <you> --nonce "$n" --kind ping --timeout 15s
```

Exit 0 means the keeper is alive and watching your pane. Exit 3 means it timed
out — tell the captain over comms rather than assuming the keeper will save you.

## § What you must not do

- Submit to the `main` queue.
- Put `--assignee` on a dispatchable bead. It goes on the epic only.
- `br close`, `br claim`, or `br reopen` a bead you submitted to your queue —
  those transitions are the daemon's. A pre-set `in_progress` makes `queue
  submit` refuse the bead (`bead_already_dispatched`, `-32015`, exit 1), and a
  hand close leaks to the parent repo before any code lands. A bead you worked
  by hand and never submitted is the other case: close it, because nothing else
  will — `harmonik reconcile` only closes beads whose commit carries a
  `Harmonik-Bead-ID:` trailer, and a hand commit never carries one.
- Submit around your own stopped queue. A submit under a new name always
  succeeds, and so does one under the stopped queue's own name, so this
  workaround never announces itself as wrong. It leaves your real queue parked,
  its failed beads unworked, and nobody watching them. Restart the queue you own
  with `harmonik queue recover --queue <queue>` and tell the captain.
- Spawn Agent-tool sub-agents for your epic's work. Use the queue.
- Parse non-JSON `comms` or `br` output.
- Re-dispatch the same bead a third time without reporting to the captain.
- Quit, `/clear`, or exit yourself on a keeper warn. A warn is informational.
  Only the keeper's act path runs the reset cycle, and a self-quit is permanent.

## § Clean shutdown

`harmonik comms leave`, after a final status on both surfaces. Presence ages out
on its own if you crash without it.

## § Invalid handoff

If the mission file is missing or unreadable, or you cannot resolve
`{crew_name, queue, epic_id}`:

1. **Dispatch nothing.**
2. Try to re-derive from `$HARMONIK_AGENT` and `br show` for any epic assigned to
   you.
3. Still indeterminate — post `--topic error` to the captain (broadcast if you do
   not know the captain's name) saying your handoff is invalid and you are
   awaiting a re-seed, then idle. Do not guess an epic.
