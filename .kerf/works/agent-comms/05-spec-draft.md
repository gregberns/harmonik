# agent-comms — Spec (Pass 5)

> **STATUS: FINALIZED 2026-06-01.** The named-queues peer (reviewer of record for the
> event-model half) signed off with Q1/Q2/Q3 APPROVED and three NORMATIVE conditions
> folded in. The authoritative resolution lives in the "## FINALIZED (peer sign-off
> 2026-06-01)" section at the foot of this file; where that section and §7 disagree,
> the FINALIZED section wins. §1–§6 are normative as written; §7 below is retained as
> the rationale trail behind the now-locked answers.

**Status: DRAFT for review.** The **named-queues** lane owns the event-model half
(event schemas, bus delivery, subscribe-side filtering, cursor mechanism) and is the
reviewer of record. The **flywheel** lane owns the `harmonik comms` CLI surface and
this draft. Cross-cutting items are flagged inline with **[CROSS-CUTTING]**.

Builds on `01-problem-space.md` (4 locked positions), `02-analysis.md` (file:line
extension points), `peer-event-model-strawman.md` (converging event model), and
`03-components.md` (the 6 components C1–C6). Citations are to the harmonik tree as of
2026-06-01.

---

## 1. Event schemas

Two new types in the existing `agent_*` family (register at
`internal/core/eventreg_hqwn59.go:140`). Both ride the standard EV-001 envelope
(`internal/core/event.go:27`): `schema_version`, `event_id` (UUIDv7 → chronological),
`timestamp_wall`, `type`, optional `run_id`, `source_subsystem`, `payload`. The
`payload` shapes below are the only new structures.

### 1.1 `agent_message`

```jsonc
{
  "type": "agent_message",
  "payload": {
    "from":        "<agent-name>",        // REQUIRED. Sender's presence id.
    "to":          "<agent-name>" | "*",  // REQUIRED. Directed name OR "*" broadcast.
    "topic":       "<string>",            // OPTIONAL. Free-text filter key.
    "body":        "<utf8>",              // REQUIRED. Compact message body.
    "in_reply_to": "<event_id>"           // OPTIONAL. Threading hint (no thread engine v1).
  }
}
```

- `from` is the sender's registered presence id (see §1.2 / §4). It is supplied by the
  CLI caller (`--from` or `$HARMONIK_AGENT` env); the daemon does NOT authenticate it
  (inter-agent auth is a non-goal, `01-problem-space.md` §non-goals).
- `to == "*"` is the broadcast sentinel. A literal agent named `*` is disallowed.
- `body` size: soft-cap (e.g. 8 KiB) enforced at the `comms-send` op; large payloads
  are a non-goal. Over-cap → op returns an error, no event emitted.
- **F-class durability:** add `agent_message` to `fsyncBoundaryEventTypes`
  (`internal/eventbus/busimpl.go:116`) so the event is fsync'd to `events.jsonl`
  before `comms-send` returns OK. This is what makes "no silent drops" (G2) hold even
  across an immediate daemon crash.

### 1.2 `agent_presence`

```jsonc
{
  "type": "agent_presence",
  "payload": {
    "agent":     "<agent-name>",                 // REQUIRED.
    "status":    "online" | "offline",           // REQUIRED.
    "last_seen": "<rfc3339>",                     // REQUIRED. Wall time of this beat.
    "reason":    "join" | "refresh" | "leave"     // OPTIONAL. Provenance of the beat.
  }
}
```

- The registry is the **projection** of these events, not stored state (§4).
- `agent_presence` is ordinary-durability (NOT fsync-boundary) — losing the last
  refresh beat on a crash is harmless; the TTL projection (Q2) reconciles it.

---

## 2. CLI surface — `harmonik comms`

New two-level verb in `cmd/harmonik/main.go` (mirrors the `queue` block at
`main.go:238-312`). Each subcommand dials `.harmonik/daemon.sock` like
`cmd/harmonik/subscribe.go:41`. Exit code 17 = daemon not running (same convention as
`queue`). The daemon name an agent uses is `--from`/`--name` or `$HARMONIK_AGENT`.

### 2.1 `harmonik comms send`

```
harmonik comms send (--to NAME | --broadcast) [--from NAME] [--topic T] [--reply-to ID] -- <body>
```

- `--to NAME` XOR `--broadcast` (sets `to:"*"`). Exactly one required.
- `--from NAME` defaults to `$HARMONIK_AGENT`; error if both absent.
- `--topic T`, `--reply-to ID` optional → payload `topic` / `in_reply_to`.
- `<body>` is the trailing arg (or `-` to read stdin).
- Sends one `{"op":"comms-send","payload":{…}}`; prints the minted `event_id`.

### 2.2 `harmonik comms recv`

```
harmonik comms recv [--from NAME] [--topic T] [--follow] [--json] [--name NAME]
```

- Durable read of `agent_message` from THIS agent's persisted cursor (§4) forward,
  delivering events where `to == me || to == "*"`. `--from`/`--topic` further filter.
- Advances the cursor past the delivered batch (see §5 / Q3 for ack semantics).
- `--follow`: replay backlog from cursor, THEN tail live via the subscribe transport
  (no gap — `subscribe.go:304` registers live before replay). Without `--follow`,
  `recv` drains the backlog once and exits.
- `--name` overrides the agent identity (defaults to `$HARMONIK_AGENT`).

### 2.3 `harmonik comms who`

```
harmonik comms who [--json]
```

- Prints the presence registry: agents `online` within the staleness window (§4),
  with `last_seen`. Read-only; emits nothing, advances no cursor.

### 2.4 `harmonik comms log`

```
harmonik comms log [--since <event_id|duration>] [--to NAME] [--from NAME] [--topic T] [--json]
```

- **Read-only operator projection** (position 4): ALL `agent_message` events,
  regardless of addressing, ordered by `event_id`. Does NOT advance ANY agent's cursor.
- `--since` accepts an `event_id` (scan after) or a duration (`30m`) → wall-time window.
- This is the human "read the conversation" view that replaces tailing the old
  `.harmonik/comms/*.md` files.

### 2.5 Presence registration (join/leave) — **[CROSS-CUTTING]**

Presence beats are emitted by the daemon on behalf of agents. v1 surface:
- `harmonik comms join [--name NAME]` → emit `agent_presence{online, reason:"join"}`.
- `harmonik comms leave [--name NAME]` → emit `agent_presence{offline, reason:"leave"}`.
- Refresh: emitted automatically by a long-lived `recv --follow` / `subscribe` session
  (the daemon beats on the connection's behalf at the heartbeat cadence), OR the agent
  calls `comms join` periodically. **See Q2** for the cadence/TTL decision.

---

## 3. Subscribe filter semantics (C4)

`SubscribeRequest` (`internal/daemon/subscribe.go:90-104`) gains optional `To`, `From`,
`Topic`. The per-connection predicate extends the existing type filter
(`subscriptionStream.typeFilter`/`wildcard`, `subscribe.go:440-441`; applied in `offer`
at `subscribe.go:449` and on the replay path at `subscribe.go:340`).

Predicate (applied AFTER the existing type filter, on BOTH replay and live paths):

```
deliver(evt) :=
  passesTypeFilter(evt)                          // existing
  AND ( evt.type != "agent_message"              // non-comms events bypass addressing
        OR ( (To    == "" OR evt.to ∈ {To, "*"}) // --to: directed-to-me or broadcast
         AND (From  == "" OR evt.from == From)    // --from
         AND (Topic == "" OR evt.topic == Topic)  // --topic
       ) )
```

- Empty addressing flags = wildcard (preserves today's behavior for run-event monitors).
- A single subscription can carry run events AND comms (`--types
  run_completed,agent_message --to me`): run events bypass the addressing clause.
- **Must apply on the replay path too** (`subscribe.go:340`), or a reconnecting agent
  would re-receive backlog that the live path filtered out.
- **[CROSS-CUTTING]** the named-queues lane owns this predicate; it must match the
  `recv` server-side predicate (§5) exactly so live and durable paths agree.

---

## 4. Presence registry semantics (C6)

The registry is a pure projection over `agent_presence` events (no stored membership):

```
online(agent) := ∃ latest agent_presence(agent) with
                 status=="online" AND now - last_seen < TTL
```

- `who` (and any addressing-validation) computes this by scanning `events.jsonl`
  (`ScanAfter` / bof scan, `jsonlwriter.go:311`) for `agent_presence`, keeping the
  latest beat per `agent`.
- Join → `online` beat; leave → `offline` beat; periodic refresh → `online` beat.
- A crashed agent (no leave) expires when its last beat ages past `TTL` — this is why
  TTL is needed even with explicit join/leave. **See Q2.**

---

## 5. Durable delivery end-to-end

The load-bearing flow (position 2 — no silent drops):

```
1. send:   `harmonik comms send --to B "..."`
           → comms-send op → bus.Emit("agent_message", …) (daemon.go:774)
           → fsync'd to events.jsonl (F-class, busimpl.go:116) → OK + event_id.
2. on bus: the event is now totally ordered (event_id UUIDv7) and durable.
3. recv:   B (whenever it next reads, even if offline at send-time) runs
           `harmonik comms recv` → daemon reads from B's persisted cursor (§Q1),
           ScanAfter(events.jsonl, cursorB) (jsonlwriter.go:311), applies the §3
           predicate with To=B, returns the batch.
4. ack:    B's cursor advances past the delivered batch (Q3: cursor-advance =
           implicit delivery ack for v1). On crash before ack, B re-reads the same
           batch on reconnect — at-least-once, never lost.
5. live:   `recv --follow` then tails via subscribe (--since-event-id=cursorB),
           so steps 3→5 have no gap (subscribe.go:304 registers live before replay).
```

- **Daemon restart:** durable because the messages ARE events in `events.jsonl`
  (already survives restart) and the cursor store is daemon-persisted (Q1). On
  restart, B's `recv` resumes from the persisted cursor — nothing re-delivered beyond
  the unacked tail, nothing lost.
- **Back-pressure:** the live path can still drop under the 256-slot drop-oldest
  discipline (`subscribe.go:460`), but `recv`/`--follow` always re-reads dropped
  events from the durable log via the cursor — drop affects only liveness, never
  durability.

---

## 6. Migration (position 4)

- The file outboxes `.harmonik/comms/*.md` remain ONLY as the bootstrap channel until
  this lands, then are retired (`01-problem-space.md` §non-goals).
- Cut-over: agents switch their Monitor from tailing `.harmonik/comms/<me>.md` to
  `harmonik comms recv --follow` (or `subscribe --types agent_message --to <me>`).
- Post-cut-over verification: a `comms send` leaves `git status` unchanged
  (success-criterion 3) — events live under gitignored `.harmonik/`, so the
  escape-detector (the v0 `hk-77q8e` false-positive source) never fires.

---

## 7. Open Questions — recommendations (ALL: NEEDS PEER/USER SIGN-OFF)

### Q1 — Cursor ownership: client-held vs daemon-persisted-per-agent?

**Recommendation: daemon-persisted-per-agent**, at `.harmonik/comms/cursors/<name>`
(daemon-owned, gitignored, single-writer). `recv` becomes a `comms-recv` socket op.

*Rationale.* Position 2 demands durable "no silent drops" across reconnects AND daemon
restarts; a client-held cursor pushes crash-safety onto every agent and loses the
cursor if an agent forgets to persist it. Daemon-side single-writer is crash-safe and
matches the peer's lean (strawman §Open-Qs: "Lean daemon for single-writer +
crash-safety"). Cost is one small file-per-agent + a `comms-recv` op — modest, and the
replay engine itself is reused unchanged. **[CROSS-CUTTING — named-queues owns the
cursor mechanism.] NEEDS PEER/USER SIGN-OFF.**

### Q2 — Presence window: heartbeat-TTL vs explicit join/leave?

**Recommendation: BOTH.** Explicit `comms join`/`leave` events for clean,
fast-converging membership, PLUS a refresh beat at cadence `C` with `online`
defined as `last_seen < 2×C` (peer's suggestion). Propose `C = 60s` (aligns with the
existing subscribe heartbeat default, `subscribe.go:110`) → TTL = 120s.

*Rationale.* Explicit join/leave gives immediate, intentional membership for the common
case; the TTL backstop expires crashed agents that never send `leave` — required for
the dynamic-membership goal (G1) to be robust to crashes. Reusing the 60s subscribe
cadence means a `recv --follow`/`subscribe` session can carry the refresh beat for
free (the daemon already beats per connection). **[CROSS-CUTTING — refresh cadence
couples to the subscribe heartbeat the named-queues lane touches.] NEEDS PEER/USER
SIGN-OFF.**

### Q3 — Acks: does a directed message need a delivery/read ack?

**Recommendation: v1 = cursor-advance IS the implicit delivery ack; no explicit
read-ack.** Delivery is at-least-once (cursor advances only after `recv` returns a
batch; a crash before advance re-delivers). Defer an explicit read-receipt
(`agent_ack` event referencing the message `event_id`) to v2 if a use case appears.

*Rationale.* The 4 locked positions do not require acks (strawman §Open-Qs: "not
required by the 4 decisions; defer to v2"). Cursor-advance gives the durability
guarantee position 2 actually asks for ("delivered on next read, no silent drops")
without a second event type or a write-amplification round-trip. If a sender later
needs "did B read it," that is cleanly an additive `agent_ack` event (referencing
`in_reply_to`/the message `event_id`) — no schema break. **NEEDS PEER/USER SIGN-OFF.**

---

## 8. Risks / flags

- **R1 — Predicate divergence [CROSS-CUTTING].** The live subscribe filter (§3) and the
  durable `recv` filter (§5) MUST share one predicate implementation, or live and
  replay paths disagree and an agent sees different messages depending on whether it
  was connected. Mitigation: factor a single `matchAgentMessage(payload, to, from,
  topic)` used by both `subscriptionStream.offer` and `comms-recv`.
- **R2 — At-least-once, not exactly-once.** Q3's cursor-advance-after-return means a
  crash mid-batch re-delivers. Acceptable for coordination messages; flagged so
  reviewers don't assume dedup. Recipients should treat re-delivery as benign.
- **R3 — Presence projection cost.** `who` scans `agent_presence` history each call.
  For long-lived projects the log grows; a bof scan is O(log). Mitigation if it
  bites: cap the scan to a recent window (TTL is 120s, so only recent beats matter) —
  scan backward from EOF until older than TTL.
- **R4 — `from` is unauthenticated.** Spoofable by design (auth is a non-goal). Flagged
  so it is a conscious decision, not an oversight.
- **R5 — fsync on every send.** Adding `agent_message` to `fsyncBoundaryEventTypes`
  adds a sync per send. Comms volume is low (coordination, not telemetry), so
  acceptable; flagged in case a chatty agent changes that assumption.

---

## FINALIZED (peer sign-off 2026-06-01)

The **named-queues** peer (reviewer of record for the event-model half) reviewed §1–§7
and the `peer-event-model-strawman.md` and gave **SIGN-OFF**. This section is the
authoritative resolution of the three open questions plus the three NORMATIVE
conditions the peer required. It supersedes §7's "NEEDS PEER/USER SIGN-OFF" framing.

### Open questions — all APPROVED

- **Q1 — Cursor ownership: APPROVED → daemon-persisted-per-agent.** The cursor is a
  daemon-owned store at `.harmonik/comms/cursors/<name>` (gitignored, single-writer,
  crash-safe). `recv` is the `comms-recv` socket op that reads from the stored cursor,
  scans `ScanAfter`, returns the batch, and advances the stored cursor. The
  client-held alternative is rejected (it pushes crash-safety onto every agent and
  loses durability if an agent forgets to persist). This is what makes "no silent
  drops" hold across BOTH reconnects and daemon restarts.

- **Q2 — Presence window: APPROVED → explicit join/leave + 60s refresh TTL.** Both
  mechanisms ship in v1: explicit `comms join` / `comms leave` events for fast,
  intentional membership, PLUS a refresh beat at cadence `C = 60s` (aligned with the
  subscribe heartbeat default, `subscribe.go:110`). **`online(agent)` is defined as a
  latest `online` presence beat within ~2× TTL** (i.e. `now − last_seen < 120s`). A
  crashed agent that never sent `leave` expires when its last beat ages past the TTL —
  required for dynamic-membership robustness (G1).

- **Q3 — Acks: APPROVED → implicit cursor-advance at-least-once for v1; `agent_ack`
  deferred to v2.** Delivery is **at-least-once**: the cursor advances only after
  `recv` returns a batch, so a crash before advance re-delivers that batch on
  reconnect. There is no explicit read-receipt in v1; a future `agent_ack` event
  referencing the message `event_id` is a clean additive v2 extension (no schema
  break).

### NORMATIVE conditions (folded in — these are requirements, not advice)

**N1 — Single shared `matchAgentMessage` predicate (build-TASK #1).**
> R1 is promoted from a mitigation to a **NORMATIVE REQUIREMENT.** The spec **MUST
> REQUIRE** exactly one exported predicate
>
> ```go
> func matchAgentMessage(payload AgentMessagePayload, to, from, topic string) bool
> ```
>
> called by BOTH:
> - the **replay** path (`internal/daemon/subscribe.go:340-344`, after the type
>   filter, inside the `ScanAfter` loop), and
> - the **live-offer** path (`internal/daemon/subscribe.go:450-453`, after the type
>   filter, inside `subscriptionStream.offer`),
>
> and ALSO by the durable `comms-recv` op (C5). **There MUST NOT be two copies of the
> addressing logic.** A shared **table-driven test** MUST assert that the live and
> replay paths return **identical verdicts** for the same `(payload, to, from, topic)`
> rows (directed-to-me, directed-to-other, broadcast `*`, from-filter, topic-filter,
> non-`agent_message` bypass). This is **build-TASK #1** (T1) and is a prerequisite for
> C4 and C5. The empty-flag = wildcard rule and the non-`agent_message` bypass (§3)
> are part of the predicate's contract and covered by the shared test.

**N2 — Build-order dependency: C4 (replay AND live) fully done before C3 `recv --follow`.**
> `harmonik comms recv --follow` (C3) replays the durable backlog through the C4 REPLAY
> predicate and then tails the live path through the C4 LIVE predicate. Therefore **C4
> MUST be FULLY done — both the replay-path predicate AND the live-offer-path predicate
> AND their shared table test (N1) — before C3 starts.** A partial C4 (live-only or
> replay-only) is insufficient: `recv --follow` would deliver a different message set
> across the catch-up→live boundary. This dependency is explicit in the §"Dependency
> graph" below and in `07-tasks.md`.

**N3 — Recipients MUST dedupe on `event_id` (at-least-once tolerance).**
> Because Q3 delivery is **at-least-once** (N-condition restatement of R2 — redelivery
> on crash before cursor-advance), **recipients MUST tolerate duplicate delivery.** The
> normative requirement: **dedupe on `event_id`.** Each `agent_message` carries a unique
> UUIDv7 `event_id`; a recipient that has already processed an `event_id` MUST treat a
> re-delivery of that same `event_id` as a no-op. This MUST be **flagged in the
> agent-comms skill / handler-contract** so that every agent's launch context tells it:
> "comms delivery is at-least-once — dedupe by `event_id`; never assume exactly-once."
> Implementations of `recv` SHOULD make redelivery rare (advance the cursor promptly
> after returning a batch), but correctness rests on recipient-side `event_id` dedupe,
> not on never-redelivering.

### Updated dependency graph (reflects N1/N2)

```
T1 matchAgentMessage predicate + shared table test  ──┐  (build-TASK #1, FIRST)
                                                       │
C1 (event types)  ──┬─→ C2 (send + comms-send op)      │
                    ├─→ C4 (subscribe filter) ─────────┘──→ C5 (cursor store / comms-recv)
                    │        ▲ uses T1 on BOTH replay+live          │
                    │        └── C4 FULLY done (N2) ──────────────┐ │
                    └─→ C6 (presence registry) ───────────────────┤ │
                                                                   ▼ ▼
                                                      C3 (recv/log/who/join/leave)
```

### Deploy gating (carried into 07-tasks.md)

All C1/C4/C5 (named-queues lane) and C3/C6 (flywheel lane) tasks dispatch **only after
the daemon-reliability deploy-restart** — the running daemon must be on the new binary
before any comms task lands, because every comms path rides the daemon's bus / socket /
subscribe surface. T1 (the predicate + test) is pure code with no runtime coupling and
is the one exception that may be authored ahead, but it still merges behind the
restart since it shares the `subscribe.go` files the live daemon serves.
