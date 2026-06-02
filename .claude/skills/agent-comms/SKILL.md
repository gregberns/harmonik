---
name: agent-comms
description: >
  Agent-facing contract for `harmonik comms` — the inter-agent messaging
  surface. Declares the at-least-once delivery guarantee (N3), the NORMATIVE
  requirement to dedupe on `event_id`, and the CLI surface for
  send/recv/log/join/leave/who. Required in every agent's launch context that
  participates in agent-to-agent coordination.

  Load-bearing: must not rot. Kept current with agent-comms spec
  (FINALIZED 2026-06-01, peer sign-off).

sources:
  - ~/.kerf/projects/gregberns-harmonik/agent-comms/05-spec-draft.md §N3 (FINALIZED)
  - ~/.kerf/projects/gregberns-harmonik/agent-comms/07-tasks.md T12
  - specs/handler-contract.md §4.11 (HC-046–HC-049)
---

# Agent-Comms Skill

You are operating inside a harmonik run. This skill defines how you send and
receive messages from other agents via the `harmonik comms` surface, and
explains the delivery guarantee you MUST rely on.

---

## Delivery guarantee — READ THIS FIRST (N3, NORMATIVE)

**Comms delivery is at-least-once, NOT exactly-once.**

Every `agent_message` carries a unique UUIDv7 `event_id` in its envelope. A
crash or restart before the daemon advances the cursor causes the same batch to
be re-delivered on the next `recv` call.

**NORMATIVE requirement (N3):**

> A recipient that has already processed an `event_id` MUST treat a
> re-delivery of that same `event_id` as a **no-op**. Never assume
> exactly-once. Dedupe by `event_id`.

Practical dedupe pattern:

```python
# pseudo-code — adapt to your language/context
seen = set()          # or a persistent store keyed on event_id

for msg in recv_batch:
    if msg["event_id"] in seen:
        continue      # re-delivery — skip
    seen.add(msg["event_id"])
    handle(msg)
```

Why: the cursor advances **after** the batch is returned, so a crash between
delivery and cursor-advance replays the same batch. The `event_id` is the only
safe dedup key — do not rely on body content or wall-clock time.

---

## Identity

Every comms op requires an agent identity. Resolution order:

1. Explicit flag (`--from`, `--name`, `--agent`).
2. `$HARMONIK_AGENT` environment variable (set by the daemon at launch).

If both are absent, the command exits with code 1.

---

## CLI surface

### `harmonik comms send` — send a message (requires daemon)

```
harmonik comms send (--to NAME | --broadcast) [--from NAME] [--topic T]
                    [--reply-to ID] [--project DIR] [--] <body>
```

- `--to NAME` XOR `--broadcast` (sets `to:"*"`). Exactly one required.
- `--from NAME` — sender identity (default: `$HARMONIK_AGENT`).
- `--topic T` — optional free-text filter key.
- `--reply-to ID` — optional `event_id` of the message being replied to.
- `<body>` — trailing args joined by space, or `-` to read stdin.
- Prints the minted `event_id` on success.
- Exit 17 = daemon not running.

```bash
# Direct message
harmonik comms send --to orchestrator -- Batch complete

# Broadcast
harmonik comms send --broadcast --from myagent -- Status: ready

# With topic
harmonik comms send --to alice --from bob --topic status -- ready

# Stdin body
echo '{"result": "ok"}' | harmonik comms send --to orchestrator --from myagent -
```

---

### `harmonik comms recv` — receive messages from durable cursor (requires daemon)

```
harmonik comms recv [--agent NAME] [--from NAME] [--topic T]
                    [--follow] [--json] [--project DIR]
```

Reads unread `agent_message` events from this agent's persisted cursor
forward, advancing the cursor after delivery (at-least-once, N3). Delivers
events where `to == me || to == "*"`.

- `--agent NAME` — agent identity (default: `$HARMONIK_AGENT`).
- `--from NAME` — filter: only messages from NAME.
- `--topic T` — filter: only messages with topic T.
- `--follow` — replay backlog, then tail live events until signal (no gap).
- `--json` — emit one JSON object per message (NDJSON).
- Exit 17 = daemon not running.

```bash
# Drain backlog once (one-shot)
harmonik comms recv --agent myagent

# Drain then stream live
harmonik comms recv --agent myagent --follow

# Filter and stream JSON
harmonik comms recv --agent myagent --from orchestrator --topic status --json

# Uses $HARMONIK_AGENT
harmonik comms recv --follow
```

**JSON output shape (per message):**

```json
{
  "event_id": "<UUIDv7>",
  "from": "sender-name",
  "to": "myagent",
  "topic": "status",
  "body": "...",
  "in_reply_to": "<UUIDv7 or omitted>",
  "ts": "2026-06-01T12:00:00Z"
}
```

`event_id` is the dedup key. See "Delivery guarantee" above.

---

### `harmonik comms log` — operator view (no daemon needed)

```
harmonik comms log [--since <event_id|duration>] [--to NAME] [--from NAME]
                   [--topic T] [--json] [--project DIR]
```

Read-only scan of ALL `agent_message` events in `events.jsonl`. Does NOT
advance any agent cursor. Use for debugging / human inspection.

- `--since EVENT_ID` — scan after that event.
- `--since DURATION` — events within the last duration (e.g. `30m`, `1h`).
- `--to NAME` — filter: only to NAME or broadcast.
- `--from NAME` — filter: only from NAME.
- `--topic T` — filter: only with topic T.
- `--json` — NDJSON output (full event envelope).

```bash
harmonik comms log --since 30m
harmonik comms log --from orchestrator --json
harmonik comms log --since 1h --to myagent
```

---

### `harmonik comms join` / `leave` — presence beats (requires daemon)

```
harmonik comms join [--name NAME] [--project DIR]
harmonik comms leave [--name NAME] [--project DIR]
```

- `join` → emits `agent_presence{status:"online", reason:"join"}`.
- `leave` → emits `agent_presence{status:"offline", reason:"leave"}`.
- Prints the minted `event_id` on success.
- Exit 17 = daemon not running.

```bash
harmonik comms join --name myagent
harmonik comms leave --name myagent
harmonik comms join    # uses $HARMONIK_AGENT
```

Call `join` at session start; call `leave` at clean shutdown. An agent that
crashes without calling `leave` expires naturally when its last presence beat
ages past the TTL (~120s).

---

### `harmonik comms who` — presence registry (no daemon needed)

```
harmonik comms who [--json] [--project DIR]
```

Lists agents online within the ~120s staleness window. Read-only; emits
nothing, advances no cursor.

- `--json` — NDJSON, one `{"agent": "name", "last_seen": "RFC3339"}` per line.

```bash
harmonik comms who
harmonik comms who --json
```

---

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Argument error or op rejected |
| 2 | Unrecognised verb |
| 17 | Daemon not running (send/recv/join/leave) |

---

## What agents MUST do

- **Dedupe on `event_id`** — never assume exactly-once (N3).
- Use `$HARMONIK_AGENT` as identity (already set in the launch environment).
- Use `--follow` for persistent message loops; without it `recv` is one-shot.
- Call `comms join` at startup and `comms leave` at clean shutdown.

## What agents MUST NOT do

- Do NOT assume a message will be delivered exactly once.
- Do NOT use `comms log` as a substitute for `recv` — `log` does not advance
  any cursor and ignores per-agent addressing.
- Do NOT parse the human-readable output of `comms recv` — use `--json`.

---

## Spec references

- **N3** (normative, FINALIZED 2026-06-01):
  `~/.kerf/projects/gregberns-harmonik/agent-comms/05-spec-draft.md §FINALIZED`
- **Q3** (acks, at-least-once): same file, §Q3 / §5 step 4.
- `specs/handler-contract.md §4.11` — HC-046–HC-049 (skill provisioning).
