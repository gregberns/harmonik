# agent-comms — Tasks (Pass 7)

Discrete implementation units, ready to become beads. Derived from the FINALIZED spec
(`05-spec-draft.md`, peer sign-off 2026-06-01) and the 6 components (`03-components.md`).
File:line refs are from `02-analysis.md`, verified against the tree on 2026-06-01.

**Do NOT create beads from this file — the orchestrator owns bead creation.**

## Lane ownership

- **named-queues** owns the event-model half: **C1** (event types), **C4** (subscribe
  filter), **C5** (cursor persistence) — i.e. tasks T1, T2, T6, T7, T8.
- **flywheel** owns the CLI surface: **C3** (recv/log/who/join/leave), **C6** (presence
  registry) — i.e. tasks T3, T4, T5, T9, T10, T11. (C2 `comms send` is the CLI/send
  pair: CLI verb is flywheel, the `comms-send` socket op rides the named-queues bus
  surface — split as T3/T4 below.)

## Deploy gating (HARD)

Every task in this work dispatches **only AFTER the daemon-reliability deploy-restart** —
the running daemon must be on the new binary first, because every comms path rides the
daemon's bus / socket / subscribe surface. This applies to **both lanes**: named-queues'
C1/C4/C5 and flywheel's C3/C6 all wait for that restart. **T1** is pure code (no runtime
coupling) and MAY be authored ahead, but it still merges behind the restart because it
edits `subscribe.go`, which the live daemon serves.

---

## Task list

### T1 — `matchAgentMessage` predicate + shared table test  ⟵ BUILD-TASK #1, FIRST

- **Component:** C4 (the shared predicate the whole addressing layer rests on).
- **Lane:** named-queues.
- **What:** Add one exported predicate
  `matchAgentMessage(payload core.AgentMessagePayload, to, from, topic string) bool`
  implementing the §3 semantics: empty flag = wildcard; `to` matches `evt.to ∈ {to,"*"}`;
  `from` matches exactly; `topic` matches exactly; non-`agent_message` events bypass the
  addressing clause. Add a **table-driven test** asserting the LIVE path
  (`subscriptionStream.offer`) and the REPLAY path (`ScanAfter` loop) return **identical
  verdicts** for the same rows (directed-to-me, directed-to-other, broadcast `*`,
  from-filter, topic-filter, non-`agent_message` bypass, empty-flag wildcard).
- **Files:** `internal/daemon/subscribe.go` (new predicate near the `subscriptionStream`
  filter at `:440-453`); test in `internal/daemon/subscribe_test.go` (or a new
  `match_agent_message_test.go`). Payload struct dependency: T2.
- **Dependencies:** none on other comms tasks except the `AgentMessagePayload` struct
  (T2) for the signature; T1 and T2 may land together, but the **predicate-and-test is
  the literal first build artifact** (NORMATIVE condition N1). **Blocks T6, T7, T8** —
  no addressing logic may be written that does not call this predicate; there MUST NOT
  be two copies.

### T2 — Register `agent_message` + `agent_presence` event types

- **Component:** C1.
- **Lane:** named-queues.
- **What:** Define `AgentMessagePayload` (`from`,`to`,`topic`,`body`,`in_reply_to`) and
  `AgentPresencePayload` (`agent`,`status`,`last_seen`,`reason`) in `internal/core`;
  register both via `mustRegister` in the `agent_*` family. Add **`agent_message` only**
  to `fsyncBoundaryEventTypes` (F-class durability — durable send); `agent_presence`
  stays ordinary-durability. Enforce the 8 KiB body soft-cap contract at the struct/op
  boundary (op-side enforcement lands in T4).
- **Files:** `internal/core` (new payload structs next to `AgentHeartbeatPayload`);
  `internal/core/eventreg_hqwn59.go:140` (`registerAgentEvents`);
  `internal/eventbus/busimpl.go:116` (`fsyncBoundaryEventTypes`).
- **Dependencies:** none inbound. **Blocks T1 (signature), T3, T4, T6, T7, T8, T9.**
  First substantive landing alongside T1.

### T3 — `harmonik comms send` CLI verb

- **Component:** C2 (CLI half).
- **Lane:** flywheel.
- **What:** New two-level `comms` verb block in `main.go` mirroring `queue`; `send`
  subcommand: `--to NAME` XOR `--broadcast`, `--from NAME` (default `$HARMONIK_AGENT`),
  `--topic`, `--reply-to`, trailing `<body>` (or `-` for stdin). Dials
  `.harmonik/daemon.sock`, sends one `{"op":"comms-send","payload":{…}}`, prints the
  minted `event_id`. Exit 17 if daemon down.
- **Files:** `cmd/harmonik/main.go:189-386` (new `comms` block, `queue` block at
  `:238-312` is the template); socket-dial pattern from `cmd/harmonik/subscribe.go:41`.
- **Dependencies:** T2 (payload), T4 (the `comms-send` op it calls). Send is independently
  demonstrable (success-criterion 3: a send leaves `git status` clean).

### T4 — `comms-send` socket op + `CommsHandler` interface

- **Component:** C2 (daemon half).
- **Lane:** named-queues (bus/socket surface).
- **What:** New `case "comms-send"` in the op-switch; a `CommsHandler` injected
  interface (mirrors `QueueHandler`/`SubscribeHandler`). Handler validates the payload
  (REQUIRED `from`/`to`/`body`; reject literal `to == "*"`-only edge per §1.1; reject
  body over 8 KiB → error, no event), calls `bus.Emit(ctx, "agent_message", payload)`,
  returns the minted `event_id` in the `SocketResponse`.
- **Files:** `internal/daemon/socket.go:391-440` (op-switch); `internal/daemon/daemon.go:603`
  (bus ref), `:774` (emit pattern); handler-interface wiring near `subscribe.go:74`.
- **Dependencies:** T2 (event type must exist to emit). Consumed by T3.

### T5 — `harmonik comms log` (read-only operator projection)

- **Component:** C3 (operator-view slice; no cursor, no presence — lands early).
- **Lane:** flywheel.
- **What:** `comms log [--since <event_id|duration>] [--to] [--from] [--topic] [--json]`
  — scans `events.jsonl` for ALL `agent_message` events (regardless of addressing),
  ordered by `event_id`, **does NOT advance any cursor**. `--since` accepts an
  `event_id` (scan after) or a duration (wall-time window). This is the human "read the
  conversation" view that replaces tailing `.harmonik/comms/*.md`.
- **Files:** `cmd/harmonik/main.go` (`comms log` subcommand in the T3 `comms` block);
  read via `eventbus.ScanAfter` / bof scan `internal/eventbus/jsonlwriter.go:311`.
- **Dependencies:** T2 (type), T3 (the `comms` verb block it slots into). Independent of
  C4/C5/C6 — pure read, can land before the cursor/filter machinery.

### T6 — Subscribe `--to/--from/--topic` filter (LIVE + REPLAY paths)

- **Component:** C4.
- **Lane:** named-queues.
- **What:** Add optional `To`,`From`,`Topic` to `SubscribeRequest`. Apply the **T1
  predicate** (NOT a second copy) AFTER the existing type filter on **BOTH** paths:
  the live `subscriptionStream.offer` (`:450-453`) AND the replay `ScanAfter` loop
  (`:340-344`). Non-`agent_message` events bypass addressing (run-event monitors keep
  working). Add `--to/--from/--topic` client flags to `cmd/harmonik/subscribe.go`
  (`--since-event-id` at `:74-78` is the template).
- **Files:** `internal/daemon/subscribe.go:90-104` (request fields), `:340-344` (replay),
  `:450-453` (live `offer`); `cmd/harmonik/subscribe.go:74-78` (client flags).
- **Dependencies:** T1 (the predicate — MUST be reused, not reimplemented), T2 (payload
  fields). **C4 is NOT done until BOTH live and replay use T1 and the shared test
  (T1) passes** (NORMATIVE condition N2). **Blocks T9 (`recv`/`recv --follow`).**

### T7 — Per-agent cursor store

- **Component:** C5 (storage half).
- **Lane:** named-queues.
- **What:** A daemon-owned cursor store keyed by agent name at
  `.harmonik/comms/cursors/<name>` (gitignored, single-writer, crash-safe atomic
  write). Read "last consumed `event_id`"; advance it atomically. No bus events — a
  store, not a projection.
- **Files:** new `internal/daemon/commscursor.go` (or under `internal/comms/`);
  gitignore already covers `.harmonik/`.
- **Dependencies:** T2 (events to track). Consumed by T8.

### T8 — `comms-recv` socket op (durable read + cursor advance)

- **Component:** C5 (op half) — composes C4 predicate + C5 store.
- **Lane:** named-queues.
- **What:** `comms-recv` op: read agent's stored cursor (T7), `ScanAfter(events.jsonl,
  cursor)`, apply the **T1 predicate** with `to == <agent>` (directed + broadcast) plus
  `--from`/`--topic`, return the batch, **then atomically advance the stored cursor**
  past the batch (cursor-advance = implicit at-least-once delivery ack, Q3). Delivery is
  at-least-once: a crash before advance re-delivers on reconnect.
- **Files:** `internal/daemon/socket.go` (op-switch, `CommsHandler`); reuses T1 predicate;
  T7 store; `internal/eventbus/jsonlwriter.go:311` (`ScanAfter`).
- **Dependencies:** T1, T6 (shared predicate), T7 (store), T2 (type). Consumed by T9.

### T9 — `harmonik comms recv` CLI (+ `--follow`)

- **Component:** C3 (the durable-receive verb).
- **Lane:** flywheel.
- **What:** `comms recv [--from] [--topic] [--follow] [--json] [--name]`. Without
  `--follow`: one `comms-recv` op (T8), drain the backlog, exit. With `--follow`: replay
  the backlog from cursor via the C4 **REPLAY** predicate, THEN tail live via the
  subscribe transport (`subscribe --types agent_message --to <me> --since-event-id
  <cursor>`) using the C4 **LIVE** predicate — no gap (live registered before replay,
  `subscribe.go:304`). `--name` overrides identity (default `$HARMONIK_AGENT`).
- **Files:** `cmd/harmonik/main.go` (`comms recv` in the T3 block); `cmd/harmonik/subscribe.go:41`
  (follow transport).
- **Dependencies:** **C4 FULLY done first (T1+T6) — NORMATIVE condition N2:** `recv
  --follow` crosses the catch-up→live boundary and MUST see the same message set on
  both sides. Also T8 (`comms-recv` op), T2.

### T10 — Presence registry: join/leave/refresh + projection

- **Component:** C6.
- **Lane:** flywheel (joint sign-off component; CLI + projection here).
- **What:** `comms join [--name]` → emit `agent_presence{online, reason:"join"}`;
  `comms leave [--name]` → emit `agent_presence{offline, reason:"leave"}`; a refresh beat
  at **C = 60s** emitted automatically by a long-lived `recv --follow`/`subscribe`
  session (daemon beats per connection) or by periodic `comms join`. The registry is a
  **projection**: `online(agent) := latest online beat with now − last_seen < 120s`
  (~2× TTL). A crashed agent expires when its last beat ages past TTL.
- **Files:** `cmd/harmonik/main.go` (`comms join`/`leave`); presence-emit via the
  `comms-send`-class op path (T4 surface); projection scan over `events.jsonl`
  (`internal/eventbus/jsonlwriter.go:311`), scanning backward from EOF until older than
  TTL (R3 cost cap).
- **Dependencies:** T2 (`agent_presence` type), T4 (emit surface). Consumed by T11.

### T11 — `harmonik comms who`

- **Component:** C3 (presence view) / C6 (projection consumer).
- **Lane:** flywheel.
- **What:** `comms who [--json]` — render the presence projection (T10): agents `online`
  within the 120s staleness window, with `last_seen`. Read-only; emits nothing, advances
  no cursor.
- **Files:** `cmd/harmonik/main.go` (`comms who`); reads T10's projection.
- **Dependencies:** T10 (projection), T2.

### T12 — Agent-comms skill / handler-contract: at-least-once + dedupe-by-`event_id`

- **Component:** cross-cutting (NORMATIVE condition N3).
- **Lane:** flywheel (owns the skill/contract surface) — coordinate with named-queues on
  wording.
- **What:** Add the agent-comms skill / handler-contract entry that tells every agent's
  launch context: "comms delivery is **at-least-once** — **dedupe by `event_id`**; never
  assume exactly-once; a re-delivered `event_id` MUST be a no-op." Document `comms
  send`/`recv`/`who`/`log`/`join`/`leave` usage for agents.
- **Files:** the agent-comms skill/contract surface (skills registry + handler-contract
  injection path); cross-ref the FINALIZED N3 condition.
- **Dependencies:** T3, T9 (the verbs it documents must exist). Lands last with C3.

### T13 — Retire `.harmonik/comms/*.md` file outboxes (migration)

- **Component:** migration (problem-space position 4 / §6).
- **Lane:** flywheel.
- **What:** After C3 lands, cut agents' Monitor from tailing `.harmonik/comms/<me>.md`
  to `harmonik comms recv --follow`. Retire the file-outbox protocol. Verify a `comms
  send` leaves `git status` unchanged (success-criterion 3) — no escape-detector trip.
- **Files:** agent launch/Monitor config; remove file-outbox writers; update AGENTS.md
  comms guidance.
- **Dependencies:** T9 (`recv --follow`), T11 (`who`), T12 (skill). Final task.

---

## Build order (topological, respects N1/N2 + deploy gate)

```
[deploy-restart gate — ALL tasks below wait for the daemon-reliability restart]

T1 (predicate + shared test, FIRST)  ┐
T2 (event types)                     ┘ land together; T1 is the literal first artifact
   │
   ├─ T4 (comms-send op) ── T3 (comms send CLI)
   ├─ T5 (comms log)                       (independent read; early)
   ├─ T6 (subscribe filter, LIVE+REPLAY via T1)  ── C4 FULLY done ─┐
   ├─ T7 (cursor store) ── T8 (comms-recv op, uses T1+T6+T7) ──────┤
   └─ T10 (presence join/leave/refresh + projection) ── T11 (who)  │
                                                                   ▼
                                              T9 (comms recv + --follow)   ← N2: needs C4 full
                                                                   │
                                              T12 (skill: dedupe-by-event_id)  ← N3
                                                                   │
                                              T13 (retire file outboxes / migration)
```

## Summary table

| Task | Title | Comp | Lane | Key deps |
|------|-------|------|------|----------|
| T1 | matchAgentMessage predicate + shared table test | C4 | named-queues | **FIRST**; blocks T6/T7/T8 |
| T2 | Register agent_message + agent_presence types | C1 | named-queues | none; blocks most |
| T3 | comms send CLI | C2 | flywheel | T2, T4 |
| T4 | comms-send socket op + CommsHandler | C2 | named-queues | T2 |
| T5 | comms log (read-only operator view) | C3 | flywheel | T2, T3 |
| T6 | subscribe --to/--from/--topic (live+replay, uses T1) | C4 | named-queues | T1, T2 |
| T7 | per-agent cursor store | C5 | named-queues | T2 |
| T8 | comms-recv op (durable read + cursor advance) | C5 | named-queues | T1, T6, T7, T2 |
| T9 | comms recv CLI (+ --follow) | C3 | flywheel | **C4 full (T1+T6)**, T8 |
| T10 | presence join/leave/refresh + projection | C6 | flywheel | T2, T4 |
| T11 | comms who | C3/C6 | flywheel | T10 |
| T12 | agent-comms skill: at-least-once / dedupe-by-event_id | x-cut | flywheel | T3, T9 |
| T13 | retire .harmonik/comms/*.md outboxes (migration) | migration | flywheel | T9, T11, T12 |

**NORMATIVE conditions enforced by the ordering:**
- **N1** — T1 (one shared predicate + shared table test) is build-TASK #1; T6/T8 reuse it,
  never recopy it.
- **N2** — C4 (T1 + T6, live AND replay) is FULLY done before T9 (`recv --follow`) starts.
- **N3** — T12 carries the at-least-once + dedupe-by-`event_id` requirement into the
  agent-comms skill/contract.
