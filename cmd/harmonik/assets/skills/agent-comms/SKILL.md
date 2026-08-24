---
name: agent-comms
description: >
  The `harmonik comms` inter-agent message bus: the at-least-once delivery
  guarantee, the normative requirement to dedupe on `event_id`, and the CLI
  surface. Load-bearing: must not rot.
---

<!-- Generated from cmd/harmonik/assets/skills/agent-comms/SKILL.md
     Edit there and mirror in the same commit; scripts/skill-mirror-check.sh fails on drift. -->

# Agent-Comms Skill

How you send and receive messages from other agents, and the delivery guarantee
you must build on.

## Delivery guarantee — read this first (N3, NORMATIVE)

**Delivery is at-least-once, not exactly-once.**

Every `agent_message` carries a unique `event_id`. The cursor advances *after* a
batch is returned, so a crash between delivery and cursor-advance replays the
whole batch on the next `recv`.

> A recipient that has already processed an `event_id` MUST treat a re-delivery
> of that same `event_id` as a **no-op**.

Keep a `seen` set of processed `event_id` values and skip anything already in it.
`event_id` is the only safe dedupe key — never body content, never a timestamp.

## Identity

Every op needs an identity: an explicit flag, else `$HARMONIK_AGENT` (set at
launch). With neither, the command exits 1.

**The identity flag is spelled differently per subcommand**, and the wrong one
fails with `unknown flag`:

| Subcommand | Identity flag |
|---|---|
| `recv` | `--agent NAME` |
| `join`, `leave` | `--name NAME` |
| `send` | `--from NAME` (`--to` is the recipient, not your identity) |
| `who` | none — read-only |

Relying on `$HARMONIK_AGENT` avoids the spelling entirely.

## `harmonik comms send`

```
harmonik comms send (--to NAME | --broadcast) [--from NAME] [--topic T]
                    [--reply-to ID] [--wake | --no-wake] [--project DIR] [--] <body>
```

`--to` and `--broadcast` are exclusive and one is required. The body is the
trailing args, or `-` to read stdin. On success it prints the minted `event_id`.

**A directed send nudges the recipient's tmux pane by default**, so an idle
session wakes and processes the message. `--wake` is the explicit spelling of
that default and `--no-wake` opts out. You cannot wake a broadcast. **The nudge
is best-effort**: if no pane can be found or pasted into, it prints to stderr and
neither the exit code nor the delivery changes. An armed `comms recv --follow` on
the recipient is the half that does not fail silently — crews are expected to
keep one running for their whole life.

**Exit 1 means the recipient is a name this project does not know.** The message
is still recorded and still durable, and a peer that boots later reads it on its
first `recv` — but nobody has received it yet, and a caller that branches on the
exit code must not read that as delivered. A name counts as known when it is in
the crew registry, in `.harmonik/agents/`, or in the presence registry.
`operator` is always addressable, and a broadcast is never checked.

`harmonik wake --agent <name>` also exits 1 on a name that matches nothing, but
the two surfaces do not share a list: `wake` reaches a tmux pane and `send`
reaches a mailbox, so `operator` is addressable and not wakeable. They agree only
on a name neither knows.

```bash
harmonik comms send --to orchestrator -- Batch complete
harmonik comms send --broadcast --from myagent -- Status: ready
harmonik comms send --to crew-alpha --no-wake -- Non-urgent status
echo '{"result":"ok"}' | harmonik comms send --to orchestrator -
```

## `harmonik comms recv`

```
harmonik comms recv [--agent NAME] [--from NAME] [--topic T]
                    [--follow] [--json] [--project DIR]
```

Reads unread messages from this agent's durable cursor forward and advances it.
Delivers events where `to` is you or `*`.

- `--follow` replays the backlog then tails live with no gap. `--wait` blocks for
  exactly one message. Both use the LIVE cursor.
- `--json` emits one JSON object per message. Use it always.
- Exit 17 = daemon not running.

> **A one-shot `recv` and a `--follow` session own INDEPENDENT cursors.** Draining
> one never advances the other, so a polling `recv` and an armed `--follow` can
> both run without starving each other's view — at the cost of each seeing
> messages the other consumed. Dedupe on `event_id` already handles this.

`recv --json` is flat: `.from`, `.to`, `.topic`, `.body`, `.event_id`, `.ts`.

> **`recv --json` is FLAT; `log --json` is a NESTED envelope, and a jq filter
> written for one silently matches nothing on the other.** `log` marshals the
> whole event, so the same fields live under `.payload` and the timestamp key is
> `timestamp_wall`, not `ts`. The failure looks like health: an agent that arms
> its Monitor with `jq 'select(.payload.from == "operator")'` against a `recv`
> stream sees zero matches forever while `ps` shows a live follower. Match
> `.from` on a `recv` stream and `.payload.from` on a `log` scan.

## `harmonik comms log`

```
harmonik comms log [--since <event_id|duration>] [--to NAME] [--from NAME]
                   [--topic T] [--json] [--project DIR]
```

Read-only scan of every `agent_message` in `events.jsonl`. Needs no daemon and
advances no cursor. For debugging and human inspection — never as a substitute
for `recv`, which it cannot replace because it ignores per-agent addressing.

## `harmonik comms join` / `leave` — presence

```
harmonik comms join [--name NAME] [--reason join|refresh]
harmonik comms leave [--name NAME]
```

Call `join` at session start and `leave` at clean shutdown. An agent that crashes
without `leave` expires when its last beat ages past the TTL.

**An armed `comms recv --follow` self-refreshes presence.** It emits its own
`agent_presence{reason:"refresh"}` beat on its own timer and its own connection,
so a quiet subscriber stays Online in `comms who` with no traffic at all, and it
emits a `leave` beat immediately on a clean exit. Several role docs contradict
this and are wrong.

So **do not run a manual `join` timer alongside `--follow`** — double beats just
pollute `events.jsonl`. Without `--follow` armed, re-run `harmonik comms join
--reason=refresh` yourself on a timer comfortably inside the TTL. Use
`--reason=refresh`, not a bare `join`, so the heartbeat is not persisted.

The durations are compiled constants — cite the symbols, not the numbers:
`internal/presence` `TTL` and `StaleCutoff`, and `cmd/harmonik/comms.go`
`commsFollowPresenceBeatInterval`, which is half the TTL by design so one dropped
beat does not age you out.

Two cases where presence still ages out with `--follow` running, neither of which
contradicts the above: the **daemon is down**, so the beat cannot be delivered and
`--follow` is retrying with backoff; or the session was **parked**, where
`--follow` exits on the park message, stops beating, and deliberately sends no
`leave`. A parked agent reading Stale is intended — quiesced, not gone.

## `harmonik comms who`

Lists agents Online, plus agents that have gone Stale but not yet Offline.
Read-only. `--json` emits one
`{"agent","last_seen","status"}` per line.

**`status` is part of the contract, not optional.** An agent past the Online
window but inside the offline cutoff is reported as `stale` rather than dropped,
so a consumer that ignores the field treats a stale agent as a healthy one.
Fully-offline agents are omitted entirely.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Argument error, op rejected, or on `send` an unknown recipient (see above) |
| 2 | Unrecognised verb |
| 17 | Daemon not running (send / recv / join / leave) |

## What agents must and must not do

- **Dedupe on `event_id`.** Never assume exactly-once.
- Arm a `--follow` stream **through the Monitor tool**, not a background bash.
  Only a Monitor re-invocation delivers a line as a turn you act on; a
  backgrounded follow writing to a file is delivered and never read.
- Use `--json`. Never parse the human-readable output of any comms verb.
- `comms` carries agent messages. **Daemon run events are a different surface** —
  `harmonik subscribe`, owned by the **harmonik-dispatch** skill. Arm both: one
  tells you what peers are saying, the other what the daemon is doing with your
  beads.
