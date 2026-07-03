# agent-comms — Decomposition (Pass 3): Components

Decomposes the feature into implementable components. Each cites the existing
harmonik code it extends (file:line from `02-analysis.md`, verified against the
tree on 2026-06-01). The headline from analysis holds: **durable cursor *replay*
already exists** (`subscribe --since-event-id` + `ScanAfter`). The genuinely-new
mechanisms are (1) the two comms event types, (2) a presence registry, and (3)
addressing-aware filtering. Everything else is wiring that mirrors the existing
`queue` / `subscribe` surfaces.

Lane ownership (from `01-problem-space.md` §provenance): the **named-queues** lane
owns the event-model half (C1, C4, and the cursor-store mechanism in C5); the
**flywheel** lane owns the CLI surface (C2, C3) and this decomposition/spec. C6
(presence) is cross-cutting and flagged for joint sign-off.

---

## Component map (6 components)

| # | Component | New? | Lane | Extends |
|---|-----------|------|------|---------|
| C1 | `agent_message` + `agent_presence` event types | NEW types, existing registry | named-queues | `eventreg_hqwn59.go:140`, `busimpl.go:116` |
| C2 | `harmonik comms send` (CLI + `comms-send` socket op) | NEW op | flywheel | `socket.go:398`, `daemon.go:603-774`, `main.go:238` |
| C3 | `harmonik comms recv/log/who` (CLI read surface) | NEW verbs | flywheel | `jsonlwriter.go:311` (`ScanAfter`), `subscribe.go:41` (client) |
| C4 | subscribe-side `--to/--from/--topic` filtering | EXTEND existing filter | named-queues | `subscribe.go:90-104`, `subscribe.go:440-453` |
| C5 | per-agent cursor persistence | NEW store; replay exists | named-queues | `subscribe.go:99` (`SinceEventID`), `subscribe.go:404` (heartbeat cursor) |
| C6 | presence registry (projection of `agent_presence`) | NEW primitive | joint | C1 events + `ScanAfter` projection over `jsonlwriter.go:311` |

---

## C1 — Comms event types: `agent_message` + `agent_presence`

**Responsibility.** Define and register the two new typed payloads that carry all
comms traffic on the existing bus. These are the only new event types; everything
else reads/writes them.

**Extends.**
- Register via `mustRegister` alongside the existing `agent_*` family at
  `internal/core/eventreg_hqwn59.go:140` (`registerAgentEvents`). The family
  already holds `agent_started`/`ready`/`heartbeat`/… (`:141-152`); `agent_message`
  and `agent_presence` join it. (No runtime `RegisterEventType` needed — these are
  compile-time built-ins like their siblings.)
- Payload structs live in `internal/core` next to `AgentHeartbeatPayload` et al.
- Add `agent_message` (and `agent_presence`) to `fsyncBoundaryEventTypes`
  (`internal/eventbus/busimpl.go:116`) so a durable send is fsync'd before the
  socket op returns OK — durable delivery (G2) is the whole point.
- Envelope is unchanged (EV-001, `internal/core/event.go:27`); these ride the
  standard `event_id` UUIDv7 → chronological ordering that makes cursor replay work.

**Dependencies.** None inbound. C2/C3/C4/C5/C6 all depend on C1. **First to land.**

---

## C2 — `harmonik comms send` + `comms-send` socket op

**Responsibility.** A sender turns a CLI invocation into exactly one `agent_message`
event on the bus, with no working-tree artifact. The daemon is the single appender.

**Extends.**
- CLI: new `comms` verb block in `cmd/harmonik/main.go:189-386` chain, mirroring the
  two-level `queue` dispatch (`main.go:238-312`). `send` opens `.harmonik/daemon.sock`
  exactly like `cmd/harmonik/subscribe.go:41` (`runSubscribeSubcommand`) but sends a
  one-shot `{"op":"comms-send", ...}` and reads a single `SocketResponse`.
- Socket op: new `case "comms-send"` in the op-switch at
  `internal/daemon/socket.go:398` (next to `queue-submit`). Handler calls
  `bus.Emit(ctx, "agent_message", payload)` — the daemon already owns the bus
  reference (`internal/daemon/daemon.go:603` constructs the bus; emits at `:774`).
  Returns the minted `event_id` so the sender can correlate / set `in_reply_to`.
- Op dispatch follows the injected-handler pattern (`RequestHandler`/`QueueHandler`/
  `SubscribeHandler` at `subscribe.go:74` analog); add a `CommsHandler` interface.

**Dependencies.** C1 (event type must exist to emit). The presence `--from` self-id
comes from the agent name the caller passes (see C6 for how that name is registered).

---

## C3 — `harmonik comms recv / log / who` (CLI read surface)

**Responsibility.** The receive + operator-view surface. Three read verbs, no event
emission (except `recv`'s cursor advance, which is a store write, not a bus event):
- `recv` — durable read of `agent_message` from this agent's cursor forward, where
  `to == me || to == "*"`; advances the cursor. `--follow` replays backlog then tails.
- `log` — read-only operator projection of ALL messages; does NOT advance any cursor.
- `who` — render the presence registry (online agents).

**Extends.**
- `recv` backlog + `log`: scan `events.jsonl` via `eventbus.ScanAfter`
  (`internal/eventbus/jsonlwriter.go:311`), type-filtered to `agent_message`
  (same scan the daemon uses for subscribe replay, `subscribe.go:331-351`).
- `recv --follow`: after the backlog scan, hand off to the existing subscribe
  transport (`cmd/harmonik/subscribe.go:41`) with `--types agent_message
  --since-event-id <cursor>` so there is no gap between catch-up and live (the
  daemon already registers the live channel before replay, `subscribe.go:304`).
- `who`: consume C6's presence projection (a `comms-who` socket op, or a direct
  `ScanAfter` over `agent_presence` for a pure-client v1 — see C6 open decision).
- CLI verbs slot into the same `comms` block as C2 (`main.go:238` pattern).

**Dependencies.** C1 (types), C5 (cursor store — `recv` advances it), C6 (`who`
reads presence), C4 (server-side filter for `recv --follow` and `--from/--topic`).

---

## C4 — Subscribe-side `--to/--from/--topic` filtering

**Responsibility.** Extend server-side filtering from type-only to addressing-aware,
so a live monitor `harmonik subscribe --types agent_message --to <me>` delivers only
messages addressed to that agent (directed or broadcast). This is the live-path twin
of `recv`'s `to == me || to == "*"` predicate.

**Extends.**
- `SubscribeRequest` (`internal/daemon/subscribe.go:90-104`) gains optional `To`,
  `From`, `Topic` fields alongside `Types`/`SinceEventID`/`HeartbeatSeconds`.
- The per-connection filter at `subscriptionStream` (`subscribe.go:440-441`:
  `typeFilter`/`wildcard`) gains an addressing predicate. `offer` (`subscribe.go:449`)
  already filters by type before queuing; add: if the event is `agent_message`, also
  match `payload.to ∈ {me, "*"}` (for `--to`), `payload.from == X` (`--from`),
  `payload.topic == T` (`--topic`). Non-`agent_message` events bypass the addressing
  predicate (so a single subscription can still carry run events).
- The same predicate must apply on the **replay** path (`subscribe.go:340`), not just
  live, or a reconnect would re-deliver filtered-out backlog.
- Client `subscribe` flags: add `--to/--from/--topic` to `cmd/harmonik/subscribe.go`
  (`--since-event-id` is the existing template at `subscribe.go:74-78`).

**Dependencies.** C1 (the payload fields it filters on). Decision-coupled with C5: if
the cursor is daemon-persisted, `recv` is a daemon RPC that reuses this same predicate.

---

## C5 — Per-agent cursor persistence

**Responsibility.** Persist each agent's "last consumed `event_id`" durably so that
`recv` resumes exactly where it left off across reconnects and daemon restarts —
closing the residual gap analysis flagged (replay exists; the *cursor* is currently
client-held and live-stream drops under back-pressure, `subscribe.go:460`).

**Extends.**
- Replay engine is reused as-is: `SinceEventID` (`subscribe.go:99`) + `ScanAfter`
  (`jsonlwriter.go:311`); the heartbeat already surfaces `last_event_id`
  (`subscribe.go:404`, `makeHeartbeat`).
- NEW: a small cursor store keyed by agent name. **Open decision (Q1, see below)** —
  daemon-persisted under `.harmonik/comms/cursors/<name>` (daemon-owned, gitignored,
  single-writer, crash-safe) vs. client-held (cheap, exists, but agent must remember
  its cursor). Recommendation in §Open Questions: **daemon-persisted**.
- If daemon-persisted: `recv` becomes a `comms-recv` socket op that reads from the
  stored cursor, scans `ScanAfter`, returns the batch, and atomically advances the
  stored cursor (optionally only after ack — see Q3). This reuses C4's predicate.

**Dependencies.** C1 (events to track), C4 (shared addressing predicate on the recv
path). Consumed by C3 (`recv`).

---

## C6 — Presence registry

**Responsibility.** The one genuinely-new primitive (analysis P1 gap). Lets N agents
join/leave at runtime and discover/address each other with no static config. The
registry is a **projection**, not stored membership: "online" = an `agent_presence`
event for that agent seen within a staleness window.

**Extends.**
- Built entirely on C1's `agent_presence` events + a projection scan over
  `events.jsonl` (`ScanAfter` / beginning-of-file scan, `jsonlwriter.go:311`).
- Join = emit `agent_presence{status:"online"}` (a `comms-send`-class emit via the
  bus, `daemon.go:774`); leave = `status:"offline"`; liveness = periodic refresh
  emits (cadence in Q2).
- `who` (C3) renders the projection: agents with a recent-enough presence event.
- **Open decision (Q2)** — heartbeat-TTL vs explicit join/leave; recommendation:
  **both** (explicit join/leave events + a TTL refresh so a crashed agent expires).

**Dependencies.** C1 (`agent_presence` type). Consumed by C3 (`who`) and by C2/C4
(the `--from`/`--to` agent-name namespace is the set of registered presence ids).

---

## Dependency graph

```
C1 (event types)  ──┬─→ C2 (send + socket op)
                    ├─→ C4 (subscribe filter) ──→ C5 (cursor store) ──→ C3 (recv/log/who)
                    └─→ C6 (presence registry) ─────────────────────────↑ (who)
```

**Build order.** C1 first (everything depends on it). Then C2 (send is independently
demonstrable: a send appends one event, `git status` stays clean — success-criterion 3).
Then C4 + C5 together (they share the recv predicate). C6 in parallel with C4/C5
(only shares C1). C3 last (it composes C4/C5/C6 into the user-facing verbs).

## Open questions (resolved in 05-spec-draft.md §Open Questions)

- **Q1 Cursor ownership** — daemon-persisted-per-agent (recommended) vs client-held.
- **Q2 Presence window** — heartbeat-TTL vs explicit join/leave (recommended: both).
- **Q3 Acks** — does a directed message need a delivery/read ack (recommended: cursor
  advance = implicit delivery ack for v1; explicit read-ack deferred).

All three are **NEEDS PEER/USER SIGN-OFF** — the peer (named-queues) owns the
event-model half and reviews. Cross-cutting flags noted in the spec draft.
