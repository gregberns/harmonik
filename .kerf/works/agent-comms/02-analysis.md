# agent-comms — Analysis (Pass 2): Current State of Subsystems Being Extended

Read-only analysis of what EXISTS today. Every claim cites `file:line`. The headline:
**durable per-agent cursor replay already exists** — `harmonik subscribe --since-event-id`
(hk-a5sil) replays from `events.jsonl` then attaches live. Locked-position-2, the
load-bearing decision, is mostly an existing capability, not new infrastructure.

## 1. Event bus

- In-process bus `busImpl` lives in `internal/eventbus/busimpl.go:87`. `Emit`
  (`busimpl.go:281`) and `EmitWithRunID` (`busimpl.go:406`) build the EV-001 envelope
  (`event_id` UUIDv7, `schema_version`, `type`, `timestamp_wall`, optional `run_id`,
  `source_subsystem`, `payload`), run the redaction pipeline, append to JSONL, then
  fan out to subscribers by consumer class (sync / async / observer).
- Envelope struct: `internal/core/event.go:27` (`Event`). `event_id` is a UUIDv7 →
  lexicographic byte order == chronological order (EV-002), which is what makes
  cursor replay work. `payload` is `json.RawMessage` decoded by a per-type registry.
- JSONL durability writer: `internal/eventbus/jsonlwriter.go:75` (`JSONLWriter`,
  `O_CREATE|O_WRONLY|O_APPEND`, append-only, batching drainer, caller-driven fsync).
  Writes go to `cfg.JSONLLogPath` (`.harmonik/events/events.jsonl`).
- F-class (fsync-boundary) types are an explicit set: `busimpl.go:116`
  (`fsyncBoundaryEventTypes`). `agent_message` would be added here if it needs durable
  fsync (it should — durable delivery is the whole point).
- **Event-type registry** (~79 types): registered via `mustRegister` at
  `internal/core/eventreg_hqwn59.go:63-152`; runtime add via `RegisterEventType` /
  `RegisterEventTypeAtVersion` (`internal/core/eventregistry.go:100,114`); version
  lookup `LookupTypeSchemaVersion` (`eventregistry.go:143`, called from
  `busimpl.go:310`). Existing families include run lifecycle, `agent_*` (started,
  ready, completed, failed, heartbeat — `eventreg_hqwn59.go:141-152`), `queue_*`,
  `daemon_*`, `reviewer_*`, `reconciliation_*`, `operator_*`, hook/gate. **No
  `agent_message` type exists** — this is the one new type to register.

## 2. Subscribe — DOES support durable cursor replay

- Daemon side: `internal/daemon/subscribe.go`. `SubscribeRequest` carries
  `Types`, `SinceEventID`, `HeartbeatSeconds` (`subscribe.go:90-104`).
- **Replay-from-cursor is implemented (hk-a5sil):** `HandleSubscribe`
  (`subscribe.go:255`) registers the live channel BEFORE replay (`subscribe.go:304`),
  then for a non-empty `since_event_id` iterates `eventbus.ScanAfter(path, sinceID)`
  (`subscribe.go:331-351`) — events strictly after the cursor, type-filtered — encodes
  them, records a high-water mark, then dedups overlapping live events
  (`subscribe.go:362-370`). `ScanAfter` is at `jsonlwriter.go:311`.
- `--types` filtering: server-side per-connection `typeFilter`
  (`subscribe.go:275-281`, applied at `subscribe.go:340` for replay and
  `subscriptionStream.offer` `subscribe.go:449` for live). Empty = wildcard.
- Heartbeat: timer clamped [10s,600s] (`subscribe.go:107-110`, `283-294`); emits a
  connection-only `heartbeat` line carrying `last_event_id` (`makeHeartbeat`
  `subscribe.go:404`). Crucially the heartbeat surfaces the cursor a client should
  resume from.
- Back-pressure: 256-slot per-conn channel, drop-oldest + `subscription_gap` notice
  (`subscribe.go:449-469`). **Relevant risk:** a slow/heads-down agent CAN have live
  events dropped — but `since_event_id` replay on reconnect re-reads them from JSONL,
  so durability holds as long as the recipient reconnects with its last cursor.

## 3. CLI structure

- Hand-rolled `os.Args` dispatch (NOT cobra) in `cmd/harmonik/main.go`: a chain of
  `if os.Args[1] == "<verb>"` blocks (`main.go:189-386`). `queue` is a two-level verb
  with its own sub-switch (`main.go:238-312`: submit/append/status/list/pause/
  resume/dry-run/cancel).
- Client `subscribe`: `cmd/harmonik/subscribe.go:41` (`runSubscribeSubcommand`) —
  dials `.harmonik/daemon.sock`, sends one `{"op":"subscribe",...}` JSON, `io.Copy`s
  the NDJSON stream to stdout. `--since-event-id` flag already plumbed
  (`subscribe.go:74-78,137-139`).
- A new `comms` verb slots in exactly like `queue`: a `main.go` block dispatching a
  two-level sub-switch (`send` / `log`), each opening the socket like `subscribe.go`.

## 4. Daemon / broker

- Single daemon per project (pidfile lock). Holds the bus (`daemon.go:603` constructs
  `NewBusImplWithWriter`) and emits via `bus.Emit` (e.g. `daemon.go:774`).
- Unix-socket op dispatch: `internal/daemon/socket.go:312` (`handleSocketConn`),
  op-switch at `socket.go:391-440` (`queue-submit`, …, `subscribe`). Handlers are
  injected interfaces (`RequestHandler`, `QueueHandler`, `SubscribeHandler`).
- `SubscribeHub` wired at `daemon.go:708` as a long-lived wildcard observer, dormant
  until a `subscribe` op connects; given `EventsJSONLPath` for replay (`daemon.go:711`).

## 5. Agent consumption (transport)

- Agents run `harmonik subscribe` (client → socket → SubscribeHub stream). Same
  transport carries run events today; comms receipt composes onto it by adding
  `agent_message` to `--types`. No new transport needed.

## Extension points

1. **New event type** `agent_message`: `mustRegister`/`RegisterEventType`
   (`eventreg_hqwn59.go`, `eventregistry.go:100`) + payload struct (to/from/topic/body)
   in `internal/core`. Add to `fsyncBoundaryEventTypes` (`busimpl.go:116`) for durability.
2. **New socket op** `comms-send`: add a `case "comms-send"` in `socket.go:391`-switch
   with a handler that calls `bus.Emit(ctx, "agent_message", payload)` — the daemon
   already owns the bus reference (`daemon.go:603-774`).
3. **New CLI verb** `comms` (send/log): a `main.go` block mirroring `queue`
   (`main.go:238`), opening the socket like `subscribe.go`.
4. **`comms log`**: read-only — point `eventbus.ScanAfter` / a beginning-of-file scan
   at `events.jsonl`, type-filter `agent_message`. Pure read side (`jsonlwriter.go:311`).
5. **Receipt**: reuse `harmonik subscribe --types agent_message --since-event-id <cursor>`.

## Gaps vs the 4 locked positions

- **P1 — N agents / presence registry.** GAP. No presence/registry primitive exists;
  addressing today is hardcoded peers in file comms (`.harmonik/comms/*.md`). Must
  build: a lightweight registry (could itself be `agent_*` lifecycle events on the bus).
- **P2 — Durable per-agent cursor delivery.** LARGELY EXISTS. `subscribe
  --since-event-id` + `ScanAfter` replay + heartbeat `last_event_id` give cursor
  catch-up out of the box (`subscribe.go:331-351`, `404-435`). **Gap:** the *cursor is
  client-held*, not daemon-persisted per agent — the agent must remember its last
  `event_id` across reconnects. "No silent drops" needs the agent to resume from its
  stored cursor; the live-stream alone drops under back-pressure (`subscribe.go:460`).
  Decision for design: client-persisted cursor (cheap, exists) vs. a new
  daemon-side per-agent cursor store (durable membership, more work).
- **P3 — Directed + broadcast addressing, server-side filter.** PARTIAL. `--types`
  filtering exists (`subscribe.go:275`); but filtering is by event *type* only — there
  is no `to:/from:/topic` field filter. Need: addressing fields in the `agent_message`
  payload + extend `subscriptionStream`/`SubscribeRequest` to filter on them (or
  client-side filter as a v1 shortcut).
- **P4 — Comms in harmonik, not the working tree; read-only `comms log`.** Mostly new
  surface but cheap: `comms send` (new op) and `comms log` (read `events.jsonl`) leave
  `git status` clean by construction (events live under `.harmonik/`, already
  gitignored). Retiring `.harmonik/comms/*.md` is a migration step, not infra.

**Biggest risk:** addressing-aware server-side filtering (P3) and per-agent durable
cursor *persistence* (P2's residual) are the only real new mechanisms. The replay
engine itself is done. If the design accepts a client-held cursor + type-level filter
for v1, agent-comms is mostly registration + one socket op + one CLI verb.
