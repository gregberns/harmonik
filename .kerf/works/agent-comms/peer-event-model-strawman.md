# agent-comms — event-model strawman (named-queues lane)

Builds on the LOCKED problem-space (4 user-confirmed decisions). Goal: replace the
file-append v0 with a durable, ordered, typed bus over the EXISTING event log +
`harmonik subscribe`. Author: named-queues. Input to the `agent-comms` kerf work
(hk-uxm0j), analyze pass.

## Bus substrate
Messages are events in the existing per-project event log (`.harmonik/events/events.jsonl`):
already gitignored runtime (NO escape-detector trips — decision 4 "no working-tree files"),
already totally-ordered by ULID `event_id`, already streamed by `harmonik subscribe`.
Two new event types.

### `agent_message`
```
{ schema_version, event_id, timestamp_wall, type:"agent_message",
  payload:{
    from:        "<agent-name>",          // sender = its presence id
    to:          "<agent-name>" | "*",    // directed OR broadcast (decision 3)
    topic:       "<string>"?,             // optional, for filtering
    body:        "<utf8, compact>",
    in_reply_to: "<event_id>"?            // optional threading
  } }
```

### `agent_presence`
```
{ ..., type:"agent_presence",
  payload:{ agent, status:"online"|"offline", last_seen } }
```
Emitted on join/leave + a periodic refresh. The registry (decision 1, dynamic
join/leave) is just the PROJECTION of recent presence events (online = presence seen
within a staleness window). Joining = emit a presence event; no static config.

## Delivery — durable, per-agent cursor (decision 2, the load-bearing one)
- Each agent has a cursor = last `event_id` consumed, persisted daemon-side
  (`.harmonik/comms/cursors/<name>`, daemon-owned — NOT a working-tree file).
- `recv` reads `agent_message` events from `cursor+1` forward where `to == me || to == "*"`,
  then advances the cursor. An agent offline/busy at send-time gets the message on its
  next `recv` — replay-from-log, NOT live-stream-only. This is what the file-tail v0
  could not do (missed-while-busy + tail-replay).
- `--follow` first replays the backlog from `cursor`, THEN tails live via subscribe —
  no gap between catch-up and live.

## CLI surface (`harmonik comms`)
- `harmonik comms send --to NAME | --broadcast [--topic T] [--reply-to ID] "body"`  → emits one `agent_message`.
- `harmonik comms recv [--from N] [--topic T] [--follow]`  → durable read from this agent's cursor forward; advances cursor; `--follow` streams.
- `harmonik comms who`  → presence registry (online agents).
- `harmonik comms log [--since …]`  → READ-ONLY operator projection: ALL messages, does NOT advance any cursor (decision 4, operator view).

## subscribe integration
Add `agent_message`,`agent_presence` to `harmonik subscribe --types`, with subscribe-side
`--to/--from/--topic` filters (matches the strawman addressing). An agent's live monitor
becomes `harmonik subscribe --types agent_message --to <me>` — directly replaces the
file-tail Monitor.

## How it kills every v0 failure we actually hit this session
- escape-detector trips (AGENT_COMMS.md dirtying main) → GONE: events.jsonl is gitignored runtime, no working-tree file.
- append-race / tail-replay-on-rewrite → GONE: single appender (the event log), total order by `event_id`.
- missed-while-busy → GONE: durable per-agent cursor replays on next recv.
- unparseable free-text → GONE: typed payload with from/to/topic.

## Open Qs for analyze/design (flywheel to weigh)
- Cursor ownership: daemon-managed (recv = a daemon RPC like the queue verbs) vs agent-local file. Lean daemon for single-writer + crash-safety.
- Presence staleness window + heartbeat cadence (e.g. online = presence < 2×cadence).
- Read-receipts/acks: not required by the 4 decisions; defer to v2.
- Multi-project: events.jsonl is per-project, so the bus is per-project; cross-project comms out of scope v1.
