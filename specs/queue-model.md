# Queue Model

```yaml
---
title: Queue Model
spec-id: queue-model
requirement-prefix: QM
status: draft
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.1.4
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-05-31
depends-on:
  - architecture
  - execution-model
  - event-model
  - beads-integration
  - process-lifecycle
  - operator-nfr
  - workspace-model
  - handler-pause
---
```

## 1. Purpose and scope

This spec defines the daemon-owned execution-plan data model that an external orchestrator submits, appends to, and queries. It owns the queue envelope, the two group primitives (wave and stream), the per-item record, the on-disk persistence file `.harmonik/queue.json`, the validation contract applied at every mutation, the `queue_id` identity discipline, the group-level state machine, and the queue-level lifecycle states (`active`, `paused-by-failure`, `paused-by-drain`, `completed`).

The spec does NOT own: the CLI surface and JSON-RPC transport (owned by [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4]), event-payload field schemas for queue-lifecycle events (owned by [/Users/gb/github/harmonik/specs/event-model.md §8.10]), the dispatch loop and per-run state machine (owned by [/Users/gb/github/harmonik/specs/execution-model.md §4.3, §7.1]), bead-status semantics or the `blocks`-edge contract (owned by [/Users/gb/github/harmonik/specs/beads-integration.md §4.3, §4.5]), pause/drain pseudocode (owned by [/Users/gb/github/harmonik/specs/operator-nfr.md §4.7 ON-027]), and operator-control state transitions (owned by [/Users/gb/github/harmonik/specs/operator-nfr.md §4.3]).

### 1.1 In scope

- The `Queue` envelope record (`schema_version`, `queue_id`, `status`, `groups`).
- The `Group` record and `GroupKind ∈ {wave, stream}` / `GroupStatus` enums.
- The `Item` record and `ItemStatus` enum.
- `.harmonik/queue.json` persistence: write discipline, startup read, removal on completion.
- `queue_id` minting and propagation onto `run_*` event payloads as OPTIONAL fields.
- The validation contract (QM-020..QM-026) applied by `queue-submit`, `queue-append`, `queue-dry-run`.
- The group state machine (per-group) and queue-level lifecycle (per-queue).
- Append semantics on stream groups; rejection rules on wave groups and completed groups.
- Concurrency composition with `--max-concurrent` (the queue narrows but never widens parallelism).

### 1.2 Out of scope

- CLI surface (`hk queue submit | append | status | dry-run`) and operator-command names — owned by [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.3, §4.4].
- JSON-RPC transport, framing, error-code numbering — owned by [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4].
- Event-payload field schemas for the six queue-lifecycle events — owned by [/Users/gb/github/harmonik/specs/event-model.md §8.10].
- Per-run state machine, dispatch eligibility, capacity gate — owned by [/Users/gb/github/harmonik/specs/execution-model.md §4.3, §7.1].
- `br` adapter, `blocks`-edge resolution, bead-status enum — owned by [/Users/gb/github/harmonik/specs/beads-integration.md §4.3, §4.5].
- Drain pseudocode and pause-class transitions — owned by [/Users/gb/github/harmonik/specs/operator-nfr.md §4.7 ON-027].
- `queue-resume` / `queue-remove` / `queue-clear` semantics — deferred to v0.2 (see §A.3).

## 2. Data Model

The queue is a daemon-singleton: at most one queue object exists per daemon instance, identified by a daemon-minted `queue_id`. The queue envelope contains an ordered list of `Group` records; each group contains an ordered list of `Item` records.

### 2.1 RECORD Queue

```
RECORD Queue:
  schema_version    : Integer     -- MUST equal 1; N-1 readable per [operator-nfr.md §4.5 ON-018]
  queue_id          : UUID        -- daemon-minted UUIDv7 at queue-submit accept; never client-supplied
  submitted_at      : Timestamp   -- ISO 8601 with ms, UTC; set at queue-submit accept
  groups            : List<Group> -- ordered; at least one entry; group_index is dense 0..N-1
  status            : QueueStatus -- queue-level lifecycle state (see §2.2)
```

### 2.2 ENUM QueueStatus

```
ENUM QueueStatus:
  active              -- groups are advancing per §5
  paused-by-failure   -- entered per §8.3 when a group reaches complete-with-failures
  paused-by-drain     -- entered per §8.5 when the daemon enters operator-pause / shutdown drain
  completed           -- all groups complete-success; durable landmark is the final queue_group_completed event
  cancelled           -- operator cancelled the run (SIGINT/SIGTERM or global timeout) before all groups reached
                         a terminal state; queue.json is left on disk with this status so the next harmonik run
                         can detect and overwrite it cleanly without the QM-027 "already active" guard triggering;
                         exit code 1 is returned to the operator (hk-ppt32)
```

### 2.3 RECORD Group

```
RECORD Group:
  group_index       : Integer            -- 0-based dense index; immutable after submit
  kind              : GroupKind          -- wave | stream (see §2.4)
  status            : GroupStatus        -- per-group state machine state (see §2.5)
  items             : List<Item>         -- waves: immutable after submit; streams: append-only
  created_at        : Timestamp          -- ISO 8601 with ms, UTC; set at submit accept
  started_at        : Timestamp | None   -- set when group transitions pending → active
  completed_at      : Timestamp | None   -- set when group transitions to a terminal status
```

### 2.4 ENUM GroupKind

```
ENUM GroupKind:
  wave    -- fixed, closed set; dispatched concurrently up to --max-concurrent; not appendable post-submit
  stream  -- ordered, open-ended sequence; dispatched as slots open; appendable while pending or active
```

> INFORMATIVE: **Pi-driven curated dispatch uses a `stream` group.** The cognition loop's eager refill (per [/Users/gb/github/harmonik/specs/cognition-loop.md §4.9 CL-071]) and the daemon's eager refill (per [/Users/gb/github/harmonik/specs/execution-model.md §4.13 EM-062]) both dispatch via `queue-append`, which only a `stream` group accepts (§7.1 QM-040, §6 QM-024). `harmonik run --beads` defaults to a `wave` group — correct for a closed, one-shot batch submitted and run to completion, but the wrong primitive for incremental curation, which a wave group cannot accept appends into. The two entry points coexist: `wave` for closed batches, `stream` for incremental curation. A future change MUST NOT alter the `harmonik run --beads` wave default to obtain appendability; the curation path obtains it by submitting a `stream` group. A `stream` group at `--max-concurrent > 1` dispatches its items concurrently (per [/Users/gb/github/harmonik/specs/execution-model.md §4.11 EM-NOTE-STREAM-CONCURRENCY]; `--wave` is for append-closed semantics, not for concurrency), and an appended item wakes the workloop at sub-poll-interval latency (per [/Users/gb/github/harmonik/specs/execution-model.md §4.11 EM-NOTE-WAKE]).

### 2.5 ENUM GroupStatus

```
ENUM GroupStatus:
  pending                  -- predecessor not yet complete-success; no items dispatched
  active                   -- predecessor is complete-success (or this is group 0); items eligible for dispatch
  complete-success         -- terminal; every item terminal AND zero failures
  complete-with-failures   -- terminal; every item terminal AND at least one failure
```

### 2.6 RECORD Item

```
RECORD Item:
  bead_id           : BeadID             -- Beads ledger reference; immutable
  status            : ItemStatus         -- per-item state (see §2.7)
  run_id            : UUID | None        -- daemon-minted on transition to dispatched per [execution-model.md §4.3]
  appended_at       : Timestamp | None   -- set when appended post-submit (streams only); None for submit-time items
```

### 2.7 ENUM ItemStatus

```
ENUM ItemStatus:
  pending                       -- eligible for dispatch once group is active and capacity allows
  dispatched                    -- daemon has handed the bead to the execution-model dispatcher; run_id populated
  completed                     -- run reached run_completed terminal per [execution-model.md §7.1]
  failed                        -- run reached run_failed terminal per [execution-model.md §7.1]
  deferred-for-ledger-dep       -- transient; a Beads `blocks` edge is open against this bead per QM-025
```

> INFORMATIVE: The §7.1 per-run state machine in [/Users/gb/github/harmonik/specs/execution-model.md] is layered underneath the per-item state. An item's `status: dispatched` corresponds to its run reaching `running` per EM §7.1; an item's terminal (`completed` / `failed`) corresponds to the run's terminal event.

### 2.8 Item transient deferral

When an item is at the head of dispatch eligibility but its bead has an open `blocks` edge in the Beads ledger (per [/Users/gb/github/harmonik/specs/beads-integration.md §4.3 BI-006]), the daemon MUST set the item's `status` to `deferred-for-ledger-dep` and emit `queue_item_deferred_for_ledger_dep` per [/Users/gb/github/harmonik/specs/event-model.md §8.10]. When the blocking bead closes, the dispatcher MUST re-evaluate and transition the item back to `pending`. No event is emitted on the deferred → pending transition; the next dispatch attempt is the observable signal.

The dispatcher MUST re-evaluate `deferred-for-ledger-dep` items on every dispatch-loop tick per [/Users/gb/github/harmonik/specs/execution-model.md §7.4]. No separate wakeup mechanism is required in v0.1; v0.2 may add `bead_closed`-event-driven wakeup as an optimization.

The `deferred-for-ledger-dep` state is NOT terminal for the purposes of §5 group-advance computation: a group containing an item in `deferred-for-ledger-dep` is NOT all-terminal, and the group MUST NOT advance until that item resolves and reaches `completed` or `failed`. If a blocker never closes (operator inaction, blocker bead tombstoned without resolution), the group remains `active` indefinitely; the orchestrator observes via `queue-status` and may decide to address the blocker via Beads or accept indefinite hold. v0.1 ships no timeout on deferred items.

### 2.9 On-disk JSON representation

The `.harmonik/queue.json` file is the JSON serialization of the `RECORD Queue` envelope. Field names are snake_case; timestamps are ISO 8601 strings with millisecond precision and UTC `Z` suffix; UUIDs are lowercase canonical-form strings; enums are their declared lowercase identifier strings.

Example envelope (informative; non-normative):

```json
{
  "schema_version": 1,
  "queue_id": "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0001",
  "submitted_at": "2026-05-14T18:22:11.482Z",
  "status": "active",
  "groups": [
    {
      "group_index": 0,
      "kind": "wave",
      "status": "complete-success",
      "items": [
        { "bead_id": "hk-09tne", "status": "completed",
          "run_id": "0190b3c4-9001-7000-8000-000000000001",
          "appended_at": null }
      ],
      "created_at": "2026-05-14T18:22:11.482Z",
      "started_at": "2026-05-14T18:22:11.483Z",
      "completed_at": "2026-05-14T18:25:02.117Z"
    },
    {
      "group_index": 1,
      "kind": "stream",
      "status": "active",
      "items": [
        { "bead_id": "hk-1n0cw", "status": "dispatched",
          "run_id": "0190b3c4-9001-7000-8000-000000000002",
          "appended_at": null },
        { "bead_id": "hk-u5c5i", "status": "pending",
          "run_id": null, "appended_at": null }
      ],
      "created_at": "2026-05-14T18:22:11.482Z",
      "started_at": "2026-05-14T18:25:02.117Z",
      "completed_at": null
    }
  ]
}
```

### 2.10 JSON-RPC request/response payload schemas

The four queue methods (`queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`) are carried over the daemon's Unix socket per [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4 PL-003a]. This section defines the normative request and response payload shapes. The transport framing (NDJSON, 1 MiB cap, error-code block) is owned by PL-003a; this section owns the field-level wire contract.

#### RECORD QueueSubmitRequest

```
RECORD QueueSubmitRequest:
  groups          : List<Group>   -- one or more groups; field schemas per §2.3
  schema_version  : Integer = 1   -- MUST equal 1; forward-incompatible value refuses per QM-002
```

Clients MUST NOT supply `queue_id`, `submitted_at`, `status`, or any item's `run_id` or `status` field. Those fields are daemon-minted at accept time. Any client-supplied value for those fields is silently ignored by the daemon.

#### RECORD QueueSubmitResponse

```
RECORD QueueSubmitResponse:
  queue_id        : UUID          -- daemon-minted UUIDv7 per QM-010
  status          : QueueStatus   -- always "active" on a successful submit
  group_count     : Integer       -- count of groups accepted (equals len(QueueSubmitRequest.groups))
```

#### RECORD QueueAppendRequest

```
RECORD QueueAppendRequest:
  queue_id        : UUID          -- identity guard; rejected if it does not match the active queue_id
  group_index     : Integer       -- 0-based index of the target stream group
  bead_ids        : List<BeadID>  -- beads to append; validated per QM-020..QM-024
```

#### RECORD QueueAppendResponse

```
RECORD QueueAppendResponse:
  appended_count      : Integer         -- number of items accepted and appended
  new_tail_indices    : List<Integer>   -- 0-based item indices of the newly appended items within the target group
```

#### RECORD QueueStatusResponse

```
RECORD QueueStatusResponse:
  queue           : Queue | null        -- the Queue envelope per §2.1, or null when no queue is loaded
```

`queue` is null when the daemon has no active queue (file absent, queue-completed and unlinked per QM-003). `queue-status` MUST NOT mutate state or emit events per QM-057.

#### RECORD QueueDryRunRequest

Same shape as `QueueSubmitRequest` (identical field set; the method name differs). The daemon routes the request through the full validation pipeline (§6) without persisting state or emitting events.

#### RECORD QueueDryRunResponse

```
RECORD QueueDryRunResponse:
  resolved_queue          : Queue                             -- the would-be Queue envelope as it would exist post-submit
  ledger_dep_notices      : List<{bead_id, blocker_bead_id}> -- items that would start in deferred-for-ledger-dep per QM-025
  parallelism_narrowed    : Bool                              -- true when ledger_dep_notices is non-empty
```

`QueueDryRunResponse` is returned on validation success. On validation failure the dry-run returns the same typed JSON-RPC error as `queue-submit` would, with the same error code per §6.11a.

## 3. Persistence

The in-memory queue object is the runtime authority. The on-disk file `.harmonik/queue.json` is the crash-recovery sync; it MUST always reflect the in-memory state at every mutation boundary.

### 3.1 QM-001 — Atomic write discipline

Every queue mutation MUST persist via the WM-026 atomic-write discipline per [/Users/gb/github/harmonik/specs/workspace-model.md §4.7 WM-026]: (i) marshal the queue envelope to JSON; (ii) write to sibling `.harmonik/queue.json.tmp-<pid>`; (iii) `fsync(temp_fd)`; (iv) `rename(2)` the temp file to `.harmonik/queue.json`; (v) `fsync(parent_directory_fd)`. Steps (iii) and (v) are both REQUIRED. Write coalescing is explicitly deferred to v0.2 — every mutation MUST flush, with no batching.

The mutations subject to QM-001 are: queue-submit accept, queue-append accept, group-status transition (per §5), item-status transition (per §2.7), queue-status transition (per §8).

On any I/O error during the atomic-write sequence (write, fsync, rename, fsync of parent directory), the daemon MUST treat the queue as inconsistent: refuse further queue mutations, emit `infrastructure_unavailable{failed_prerequisite: queue_write_error}` per [/Users/gb/github/harmonik/specs/event-model.md §8.7.15], and transition to `degraded` state per [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.8 PL-010]. Operator recovery is `harmonik stop` followed by restart; the queue state on disk is indeterminate after a failed atomic write and MUST NOT be auto-recovered.

### 3.2 QM-002 — Read on startup

The daemon MUST read `.harmonik/queue.json` at [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.1 PL-005 step 8a], alongside `daemon.state` and `daemon.upgrading`. v0.1 supports reading `schema_version ∈ {1}`. Any other value is forward-incompatible per [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.1 PL-005 step 8a] and refuses startup with exit code 2. Three outcomes:

- **File exists and parses**: the queue is loaded with its persisted `status` and all groups/items as written. The daemon emits no synthetic event for the load itself; existing event history in `events.jsonl` is the audit record.
- **File absent**: the daemon starts with no queue object. `queue-status` calls return `queue_not_active` until the next `queue-submit` accept.
- **File present but unparseable**: the file is treated as absent (no queue loaded), with a structured-log warning matching the [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.1 PL-005] precedent for corrupt markers. The file is NOT auto-deleted; operator inspection comes first.

If the loaded queue's `status` is `paused-by-failure` or `paused-by-drain`, the queue MUST remain in that status; no auto-resume across daemon restart in v0.1.

### 3.2a QM-002a — Startup cross-check against Beads ledger

After loading `queue.json` successfully per QM-002, the daemon MUST cross-check item statuses against the live Beads ledger. For every item whose `status` is `dispatched` in `queue.json`, the daemon MUST query `br show <bead_id>` per [/Users/gb/github/harmonik/specs/beads-integration.md §4.5]. If the Beads ledger shows the bead as `open` (i.e., a prior `br update` claim-write succeeded for the queue but the corresponding Beads status write failed and the item was left `dispatched` in queue.json without Beads reflecting it), the daemon MUST:

1. Revert the item's status to `pending` in the in-memory queue envelope.
2. Persist the corrected queue envelope via QM-001 atomic write before proceeding.
3. Emit a `queue_item_reconciled` event per [/Users/gb/github/harmonik/specs/event-model.md §8.10] with `reason: claim_write_lost`.

This check MUST run before the daemon reaches `ready` state and before any dispatch-loop tick that could re-dispatch an item. The reconciliation action maps to Cat 3 per [/Users/gb/github/harmonik/specs/reconciliation/spec.md §8] (claim-write-lost store disagreement). In v0.1 the daemon executes the revert directly rather than routing through the reconciliation investigator, because the correction is fully deterministic (ledger says `open` → item reverts to `pending`).

### 3.2b QM-002b — Three-way reconciliation on startup

After QM-002a completes, the daemon MUST run a full three-way reconciliation pass that covers mismatch classes not reachable by the `dispatched`-items-only scan. Three mismatch classes are defined:

**Class A — `bead_closed_queue_pending`:** A queue item has `status=pending` (or `status=deferred-for-ledger-dep`) but the Beads ledger shows the bead as `closed` or `tombstone`. The item is waiting for a bead that has already finished. The daemon MUST:

1. Advance the item's status to `completed` in the in-memory queue envelope.
2. Persist the corrected queue envelope via QM-001 atomic write (per QM-063 — persist BEFORE emit).
3. Emit `reconciliation_mismatch_observed` per [/Users/gb/github/harmonik/specs/event-model.md §8.6.15] with `mismatch_class: "bead_closed_queue_pending"`.

**Class B — `bead_inprogress_queue_absent`:** The Beads ledger reports a bead as `in_progress` but no queue item references that bead. The daemon MUST emit `reconciliation_mismatch_observed` with `mismatch_class: "bead_inprogress_queue_absent"` and log a structured warning for operator visibility. No queue mutation is applied — the orphan-sweep (hk-2ty0g) handles queue-owned remediation.

**Class C — `bead_closed_queue_inprogress`:** A queue item has `status=completed` or `status=failed` but the Beads ledger still shows the bead as `in_progress`. The queue-side terminal is already set; no queue mutation is applied. The daemon MUST emit `reconciliation_mismatch_observed` with `mismatch_class: "bead_closed_queue_inprogress"` and log a structured warning for operator visibility.

**Execution ordering (per QM-063):**

1. Scan all queue items; collect Class A mutations and all pending event payloads for Classes A and C.
2. If any Class A mutations were collected: persist the corrected queue envelope via QM-001 before proceeding.
3. Enumerate in-progress Beads ledger entries (via `br list --status in_progress`); collect Class B payloads for any bead not referenced by a queue item.
4. Emit all collected events in the order: Class A, then Class C, then Class B.

This pass MUST complete before the daemon reaches `ready` state and before any dispatch-loop tick. In v0.1 corrections are applied directly (no reconciliation-investigator routing) because all three classes are fully deterministic given the observed store state.

### 3.3 QM-003 — Removal on completion

When the queue transitions to `status=completed` per §8.4, the daemon MUST unlink `.harmonik/queue.json` and `fsync(parent_directory_fd)`. The in-memory queue object is then cleared. Subsequent `queue-status` calls return `queue_not_active`. The next `queue-submit` re-creates the file.

### 3.4 QM-004 — Persistence size bound

The persisted `queue.json` envelope MUST NOT exceed 1 MiB (1048576 bytes) after any proposed mutation. This bounds atomic-write cost on every mutation per QM-001 and bounds memory. Violations are rejected at the validation layer per QM-026; no truncation, no auto-split.

## 4. Identity

### 4.a Subsystem envelope

#### QM-ENV-001 — Envelope declaration

Envelope for the queue-model subsystem per [/Users/gb/github/harmonik/specs/architecture.md §4.0 AR-053]. The queue-model is a daemon-owned, singleton, orchestrator-side subsystem; it owns the queue envelope, group/item records, the `.harmonik/queue.json` persistence file, validation, the group state machine, and the queue lifecycle states.

(a) Events produced:
  - `queue_submitted` — emission rule §8.1; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.10.1]. Class F.
  - `queue_group_started` — emission rule §8.1, §8.2 step 3; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.10.2]. Class O.
  - `queue_group_completed` — emission rule §8.2, §5.9 QM-033 (durable landmark); payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.10.3]. Class F.
  - `queue_paused` — emission rule §8.3 (`reason: group_failure`), §8.5 (`reason: operator_drain`); payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.10.4]. Class F.
  - `queue_appended` — emission rule §7.3; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.10.5]. Class O.
  - `queue_item_deferred_for_ledger_dep` — emission rule §2.8, §6.5 QM-025; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.10.6]. Class O.
  - `queue_item_reconciled` — emission rule §3.2a QM-002a; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.10.7]. Class F.
  - `reconciliation_mismatch_observed` — emission rule §3.2b QM-002b; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.6.15]. Class O.
  - `infrastructure_unavailable{failed_prerequisite: queue_write_error}` — emission rule §3.1 QM-001 (I/O error path); payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.7.15] (the event type itself is event-model-owned; queue is one of several emitters).

(b) Events consumed:
  - `run_started`, `run_completed`, `run_failed` — the dispatcher's per-run terminal events drive per-item status transitions (`dispatched → completed | failed`) per §2.7 and §5; payload schemas in [/Users/gb/github/harmonik/specs/event-model.md §8.1]. Queue-model populates the OPTIONAL `queue_id` / `queue_group_index` fields on these payloads per QM-011 / QM-012 (co-ownership per [/Users/gb/github/harmonik/specs/event-model.md §6.5]).
  - `operator_pause_status{status: pausing|paused}`, `operator_resuming` — drive queue-level `active ↔ paused-by-drain` transitions per §8.5 and [/Users/gb/github/harmonik/specs/operator-nfr.md §4.3, §4.7 ON-027]; payload schemas in [/Users/gb/github/harmonik/specs/event-model.md §8.7]. (`operator_pausing`, `operator_paused`, `operator_stopping` do not exist as Go EventTypes; the consolidated `operator_pause_status` with a `status` enum covers pausing and paused phases.)
  - `bead_closed` (informative, v0.1 polling-only) — the dispatcher polls Beads ledger state for `deferred-for-ledger-dep` items per §2.8; v0.2 may consume an explicit event.

(c) Types introduced (cross-subsystem):
  | Type | `Tags:` | `Axes:` (if non-baseline) |
  |---|---|---|
  | `Queue` (§2.1) | mechanism | baseline |
  | `QueueStatus` (§2.2 ENUM) | mechanism | baseline |
  | `Group` (§2.3) | mechanism | baseline |
  | `GroupKind` (§2.4 ENUM) | mechanism | baseline |
  | `GroupStatus` (§2.5 ENUM) | mechanism | baseline |
  | `Item` (§2.6) | mechanism | baseline |
  | `ItemStatus` (§2.7 ENUM) | mechanism | baseline |
  | `QueueSubmitRequest` / `QueueSubmitResponse` (§2.10) | mechanism | baseline |
  | `QueueAppendRequest` / `QueueAppendResponse` (§2.10) | mechanism | baseline |
  | `QueueStatusResponse` (§2.10) | mechanism | baseline |
  | `QueueDryRunRequest` / `QueueDryRunResponse` (§2.10) | mechanism | baseline |
  | `queue_id` (UUIDv7) field on `run_started` / `run_completed` / `run_failed` (co-owned with [event-model.md §8.10]) | mechanism | baseline |
  | `BeadID` (consumed from [/Users/gb/github/harmonik/specs/beads-integration.md §4.6]) | mechanism | baseline |

(d) Handlers implemented: none. The queue-model is a daemon-singleton subsystem; it does not expose a handler. The JSON-RPC surface (`queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`) is carried over the daemon's Unix socket per [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4 PL-003a] — that is a transport surface, not a handler-contract handler.

(e) State owned:
  - `Queue` record (§2.1) — daemon-singleton; in-memory authority.
  - `.harmonik/queue.json` (§2.9, §3) — on-disk crash-recovery sync; atomic-write per §3.1 QM-001.
  - `Group` and `Item` records (§2.3, §2.6) — wholly contained in the `Queue` envelope; lifecycle ownership per §5 (group state machine), §2.7 (item status), §8 (queue lifecycle).
  - `Run` records are consumed but NOT owned here ([/Users/gb/github/harmonik/specs/execution-model.md §6.1]).

(f) Control points provided: none. The queue-model is a mechanism-tagged subsystem; its operations are not gate/hook/guard/budget points per [/Users/gb/github/harmonik/specs/control-points.md §4.1]. Operator-control state transitions that affect the queue (pause/drain/resume) are inherited from [/Users/gb/github/harmonik/specs/operator-nfr.md §4.3].

(g) NFRs inherited / overridden:
  - Inherited: `ON-018` N-1 schema compatibility (§3.2 applies it to `queue.json`'s `schema_version`).
  - Inherited: `ON-027` graceful-shutdown ordering (§8.5 transitions the queue to `paused-by-drain` on operator pause/stop; the dispatcher drains in-flight items per [/Users/gb/github/harmonik/specs/operator-nfr.md §4.7 ON-027]).
  - Overridden: none.

(h) Boundary classification per operation:
  | Operation | `Tags:` | Axes |
  |---|---|---|
  | `queue_submit_accept` (§6, §8.1) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `queue_append_accept` (§6.5 QM-024, §7.3) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `queue_status_read` (§2.10) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `queue_dry_run` (§6.11a, §2.10) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `persist_queue_json` (§3.1 QM-001) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `unlink_queue_json` (§3.3 QM-003) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `startup_load_queue` (§3.2 QM-002) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `startup_cross_check` (§3.2a QM-002a) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `three_way_reconcile` (§3.2b QM-002b) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `group_advance` (§5, §8.2) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `item_defer_for_ledger_dep` (§2.8) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `queue_pause` / `queue_resume_on_drain` (§8.3, §8.5) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |

Tags: mechanism

### 4.1 QM-010 — `queue_id` minting

`queue_id` is a UUIDv7 minted by the daemon at the moment a `queue-submit` request passes validation (§6) and is accepted. The `queue_id` MUST NOT be client-supplied; any client-supplied value in the request is ignored. The minted `queue_id` is returned in the JSON-RPC response and carried on every queue-lifecycle event per [/Users/gb/github/harmonik/specs/event-model.md §8.10].

UUIDv7 monotonicity within a daemon process follows the EV-002a discipline per [/Users/gb/github/harmonik/specs/event-model.md §6.2 EV-002a].

### 4.2 QM-011 — `queue_id` on run events

When a run is dispatched as an item of a queue, the daemon MUST populate an OPTIONAL `queue_id` field on the `run_started`, `run_completed`, and `run_failed` event payloads per [/Users/gb/github/harmonik/specs/event-model.md §8.10]. The field is absent for runs dispatched outside the queue surface (direct dispatch, reconciliation-issued runs). This is an additive-optional, non-breaking schema change per [/Users/gb/github/harmonik/specs/event-model.md §6.4 row 1].

### 4.3 QM-012 — `queue_group_index` on run events

Alongside QM-011, the daemon MUST populate an OPTIONAL `queue_group_index` (Integer) field on `run_*` event payloads when the run is queue-dispatched. The field is absent under the same conditions as QM-011.

### 4.4 QM-013 — No reuse across daemon instances

`queue_id` values MUST NOT be reused. A daemon that loads a persisted queue at QM-002 reads the queue's existing `queue_id`; a fresh `queue-submit` mints a fresh `queue_id`. Cross-daemon-instance uniqueness is provided by UUIDv7's time-ordered random tail per EV-002.

## 5. Group State Machine

Each group transitions independently through the per-group state machine below. The queue-level lifecycle (§8) is an outer wrapper: group-state transitions are gated by the queue's `status`.

### 5.1 Transition table

| From                     | Event                                                                    | Guard                                                | To                       | Emits                                                                                                       |
|---|---|---|---|---|
| pending                  | predecessor group reaches `complete-success`                             | Queue.status == active                               | active                   | `queue_group_started`                                                                                       |
| pending                  | queue-submit accepted (this is group_index 0)                            | Queue.status == active                               | active                   | `queue_group_started`                                                                                       |
| active                   | every item terminal AND zero failed items                                | —                                                    | complete-success         | `queue_group_completed{final_status: complete-success}`                                                     |
| active                   | every item terminal AND at least one failed item                         | —                                                    | complete-with-failures   | `queue_group_completed{final_status: complete-with-failures}`, then `queue_paused{reason: group_failure}`   |
| complete-success         | —                                                                        | —                                                    | (terminal)               | —                                                                                                           |
| complete-with-failures   | —                                                                        | —                                                    | (terminal in v0.1)       | —                                                                                                           |

### 5.2 QM-030 — Group advance is all-terminal-gated

A group MUST NOT transition out of `active` until every item in the group is in a terminal `ItemStatus` (`completed` or `failed`). In-flight runs (items in `dispatched`) MUST run to their next checkpoint per [/Users/gb/github/harmonik/specs/execution-model.md §7.1]; the daemon MUST NOT interrupt them on a sibling's failure.

### 5.3 QM-031 — Pending → active gate

A group transitions `pending → active` only when (a) its immediate predecessor's status is `complete-success`, AND (b) the queue's `status` is `active`. If the queue is `paused-by-failure` or `paused-by-drain`, no group advances regardless of predecessor state.

### 5.4 QM-032 — No re-entry of terminal states

A group MUST NOT re-enter `pending` or `active` once it has reached `complete-success` or `complete-with-failures`. v0.1 ships no resume mechanism for `complete-with-failures`; recovery is daemon restart + fresh `queue-submit`. v0.2 will add `queue-resume` per §A.3.

### 5.5 QM-034 — Failed items do not interrupt sibling dispatches

Within an `active` group, an item's transition to `failed` MUST NOT cause the daemon to interrupt, cancel, or otherwise alter sibling items that are in `dispatched`. All sibling runs proceed to their next checkpoint per [/Users/gb/github/harmonik/specs/execution-model.md §7.1]. The group's terminal-status determination per §5.1 is deferred until every sibling reaches a terminal `ItemStatus`. This applies symmetrically to waves and streams.

### 5.6 QM-035 — Stream item-source semantics

For a stream group in `active`, the dispatcher MUST select the earliest-indexed item whose `status` is `pending` and whose `deferred-for-ledger-dep` status (if any) has resolved. Items appended after submit (per §7) are placed at the tail; the dispatcher's head-first selection rule ensures appended items dispatch in append order, after all earlier items have at least entered `dispatched`. A `pending` item that follows a `deferred-for-ledger-dep` head item is NOT eligible for dispatch out-of-order in v0.1; head-of-line blocking is the v0.1 behavior. Out-of-order dispatch within a stream is deferred.

### 5.7 QM-036 — Wave dispatch admission

For a wave group in `active`, the dispatcher MAY admit any `pending` item, in any order, up to the QM-062 capacity. There is no implied ordering within a wave. Waves with QM-025-deferred items still admit non-deferred siblings concurrently; the deferred items remain `deferred-for-ledger-dep` until their blockers resolve, and only then become eligible.

### 5.9 QM-033 — `queue_group_completed` is the durable landmark

The final `queue_group_completed` event of a queue (i.e., the event emitted when the last group reaches `complete-success`) is the durable landmark marking queue completion. No separate `queue_completed` event is emitted in v0.1; readers correlate by observing that no subsequent `queue_group_started` follows and that `.harmonik/queue.json` is unlinked per QM-003.

> INFORMATIVE: `queue_completed` was considered and dropped during the extqueue design pass. The final `queue_group_completed` carries enough identity (`queue_id`, `group_index`, `final_status`) to serve as the landmark.

## 6. Validation

Every `queue-submit`, `queue-append`, and `queue-dry-run` request MUST pass the validation checks in this section, evaluated in the order listed. The first failing check returns a typed JSON-RPC error and MUST NOT mutate state (no in-memory mutation, no `.harmonik/queue.json` write, no event emission). Validation failures are NOT events — they surface only on the JSON-RPC response to the caller. The JSON-RPC error code is allocated from the `-32010..-32019` range reserved for `queue-model` per [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4 PL-003a]; the error `message` field carries the typed-error shape shown in each subsection below, following the PL-003b convention (`<error_type>{"<key>":"<value>"}`).

`queue-dry-run` runs the same validation pipeline as `queue-submit`, returns the resolved plan including any QM-025 parallelism-narrowed notices on success, and MUST NOT persist state or emit events at all (success or failure).

### 6.1 QM-020 — Bead existence

Every `bead_id` in the request MUST resolve via `br show <id>` per [/Users/gb/github/harmonik/specs/beads-integration.md §4.5]. Missing beads return:

```
queue_validation_failed{
  reason: "bead_not_found",
  bead_id: "<id>"
}
```

### 6.2 QM-021 — Bead status

Every referenced bead MUST have Beads `status ∈ {open}` per [/Users/gb/github/harmonik/specs/beads-integration.md §4.3 BI-007]. Closed, in_progress, blocked, deferred, draft, pinned, or tombstone beads return:

```
queue_validation_failed{
  reason: "bead_not_open",
  bead_id: "<id>",
  actual_status: "<status>"
}
```

### 6.3 QM-022 — No double dispatch

No `bead_id` in the request MAY already be in Beads `status: in_progress` from any source (a different queue's prior submission, a non-queued direct dispatch, an external `br update`). Returns:

```
queue_validation_failed{
  reason: "bead_already_dispatched",
  bead_id: "<id>"
}
```

### 6.4 QM-023 — No cross-group or intra-group duplicates

Within a single `queue-submit` request, a `bead_id` MUST NOT appear in more than one group, AND MUST NOT appear more than once within a single group. Within a single `queue-append`, a `bead_id` MUST NOT appear more than once in the appended set, AND MUST NOT already appear as a non-terminal item in the target group. Returns:

```
queue_validation_failed{
  reason: "duplicate_bead_id",
  bead_id: "<id>"
}
```

### 6.5 QM-024 — Append target validity

`queue-append` requires `group_index` to reference an existing group whose `kind == stream` AND whose `status ∈ {pending, active}`. Append to a wave group, a completed group, or a non-existent index returns:

```
queue_validation_failed{
  reason: "append_target_invalid",
  group_index: N,
  actual_kind: "<kind> | null",
  actual_status: "<status> | null"
}
```

Append while the queue's overall `status` is `paused-by-failure` or `paused-by-drain` is rejected with:

```
queue_validation_failed{
  reason: "queue_not_advancing",
  queue_status: "<status>"
}
```

### 6.6 QM-025 — Parallelism-narrowed informational notice

If a submitted group contains two `bead_id`s X and Y where the Beads ledger declares `Y blocks-on X` (or vice versa), validation MUST NOT fail. Instead the daemon MUST emit one `queue_item_deferred_for_ledger_dep` event per blocked item at submit accept time (not at dispatch time), per [/Users/gb/github/harmonik/specs/event-model.md §8.10]:

```
queue_item_deferred_for_ledger_dep{
  queue_id: <uuid>,
  group_index: N,
  bead_id: "Y",
  blocker_bead_id: "X"
}
```

The submission proceeds and the affected item starts in `ItemStatus: deferred-for-ledger-dep`; it transitions to `pending` when its blocker closes (§2.8). The cross-reference for the `blocks` edge semantics is [/Users/gb/github/harmonik/specs/beads-integration.md §4.3 BI-006].

### 6.7 QM-026 — Persisted-size bound

After applying the proposed mutation in-memory (without persisting), the daemon MUST compute the resulting `queue.json` envelope size and reject if it exceeds 1 MiB per QM-004. Returns:

```
queue_validation_failed{
  reason: "queue_too_large",
  proposed_bytes: N,
  limit: 1048576
}
```

### 6.8 QM-027 — Single active queue

A `queue-submit` request MUST be rejected if the daemon already holds a queue object whose `status` is not `completed`. Returns:

```
queue_validation_failed{
  reason: "queue_already_active",
  existing_queue_id: <uuid>,
  existing_status: "<status>"
}
```

`queue-submit` after a queue has reached `completed` and `.harmonik/queue.json` has been unlinked per QM-003 is permitted and begins a fresh queue with a fresh `queue_id`.

### 6.9 QM-028 — Validation failures are not events

Validation failures (QM-020 through QM-027) MUST NOT emit any event. The failure surfaces exclusively on the JSON-RPC response to the caller, using the typed-error shape defined in each subsection above. This is a deliberate departure from the general "surface failures to events.jsonl" pattern: the caller (an external orchestrator agent) receives the typed error synchronously and can act on it; recording the same failure to the event log would double-publish without adding diagnostic value. Validation failures that escalate beyond the caller (e.g., repeated submit storms) are operator-NFR concerns owned by [/Users/gb/github/harmonik/specs/operator-nfr.md §4.3], not queue-model.

### 6.10 QM-029 — Validation reason enumeration

The `reason` field on the `queue_validation_failed` JSON-RPC error payload is constrained to the enum:

```
ENUM QueueValidationReason:
  bead_not_found
  bead_not_open
  bead_already_dispatched
  duplicate_bead_id
  append_target_invalid
  queue_not_advancing
  queue_too_large
  queue_already_active
  handler_paused
```

Additions to this enum require a corresponding allocation in the JSON-RPC error-code block reserved for `queue-model` (`-32010..-32019` per [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4 PL-003a]). Existing reason values are stable across the N-1 compatibility window per [/Users/gb/github/harmonik/specs/operator-nfr.md §4.5 ON-018]; the enum is a wire-level contract carried in JSON-RPC error responses.

### 6.11 QM-029a — Order of evaluation

Validation checks within a single request MUST be evaluated in the order: QM-027 (single active queue, submit-only) → QM-024 (append target validity, append-only) → QM-020 (existence) → QM-021 (status) → QM-022 (no double dispatch) → QM-052a (handler-pause gate, submit and append) → QM-023 (duplicates) → QM-026 (size). QM-025 (parallelism-narrowed) is evaluated last as an informational pass and emits its events only after the request is accepted; it never produces a validation failure. The first failing rule short-circuits and returns its typed error; the daemon MUST NOT report multiple validation failures from a single request.

### 6.11a QM-029b — Validation reason to JSON-RPC error-code mapping

Each `QueueValidationReason` enum value maps to a specific JSON-RPC error code in the `-32010..-32019` range reserved for `queue-model` per [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4 PL-003a]. The mapping follows the QM-029a evaluation sequence:

| JSON-RPC error code | `QueueValidationReason`      | Corresponding check |
|---------------------|------------------------------|---------------------|
| `-32010`            | `queue_already_active`       | QM-027              |
| `-32011`            | `append_target_invalid`      | QM-024 (target kind/status wrong) |
| `-32012`            | `queue_not_advancing`        | QM-024 (queue paused)             |
| `-32013`            | `bead_not_found`             | QM-020              |
| `-32014`            | `bead_not_open`              | QM-021              |
| `-32015`            | `bead_already_dispatched`    | QM-022              |
| `-32016`            | `duplicate_bead_id`          | QM-023              |
| `-32017`            | `queue_too_large`            | QM-026              |
| `-32018`            | `handler_paused`             | QM-052a (handler-pause gate) |

Error code `-32019` is reserved for a future `QueueValidationReason` addition within the v0.1 error-code block. Each code is a stable wire constant; additions require a spec amendment and a corresponding entry in this table. The `message` field of the JSON-RPC error carries the typed-error shape per the PL-003b `<error_type>{...}` convention; the `code` field is the numeric value in this table.

## 7. Append Semantics

### 7.1 QM-040 — Stream-only target

`queue-append` targets exactly one group, identified by `group_index`. The target MUST be a stream group per QM-024. Wave groups are immutable after submit; their `items` list never grows.

### 7.2 QM-041 — Tail-append

Appended items are placed at the tail of the target stream's `items` list, in the order supplied in the request. Each appended item starts with `status: pending`, `run_id: None`, and `appended_at` set to the request-accept timestamp (ISO 8601 with ms, UTC).

### 7.3 QM-042 — Append accept emission

After QM-001 persistence completes, the daemon MUST emit one `queue_appended` event per [/Users/gb/github/harmonik/specs/event-model.md §8.10]:

```
queue_appended{
  queue_id: <uuid>,
  group_index: N,
  appended_bead_ids: ["<id>", ...]
}
```

If any of the appended items is QM-025-deferred at accept time, the daemon MUST emit `queue_item_deferred_for_ledger_dep` events after `queue_appended`, in append order.

### 7.4 QM-043 — Append to active stream is in-flight-safe

Appending to a stream whose `status` is `active` MUST NOT block, pause, or otherwise interfere with dispatched items in the same group. The dispatch loop sees the new tail items on its next eligibility evaluation per [/Users/gb/github/harmonik/specs/execution-model.md §4.3].

### 7.5 QM-044 — Append does not extend a terminal group

A stream group reaches a terminal `GroupStatus` per §5.1 when every item in `items` is terminal. Once terminal, append is rejected per QM-024. The daemon MUST NOT re-open a terminal stream to accept new items even if the appended items would have been compatible.

## 8. Queue Lifecycle

The queue-level lifecycle is the outer wrapper around the per-group state machine. The five `QueueStatus` values are `active`, `paused-by-failure`, `paused-by-drain`, `completed`, `cancelled` (see §2.2 for semantics).

### 8.1 QM-050 — Submit (active entry)

`queue-submit` validates per §6 → mints `queue_id` per QM-010 → constructs the in-memory `Queue` envelope with `status: active` and every group at `GroupStatus: pending` → persists via QM-001 → transitions group_index 0 to `active` per QM-031 (which itself persists via QM-001) → emits in order:

1. `queue_submitted{queue_id, group_count}`
2. `queue_group_started{queue_id, group_index: 0}`
3. zero or more `queue_item_deferred_for_ledger_dep` events per QM-025.

Event payload schemas are owned by [/Users/gb/github/harmonik/specs/event-model.md §8.10].

> INFORMATIVE: `queue-submit` returning `status: active` IS the queue's "start" semantics: group_index 0 activates immediately on submit and the dispatcher picks it up at sub-poll-interval latency (per [/Users/gb/github/harmonik/specs/execution-model.md §4.11 EM-NOTE-WAKE]). There is no separate `start` method — the queue methods are `queue-submit | queue-append | queue-status | queue-dry-run` per §6. A Pi-driven dispatch flow that needs to "start processing" submits the queue; no distinct start verb exists or is required.

### 8.2 QM-051 — Advance

When the active group reaches `GroupStatus: complete-success` per §5.1 row 3, the daemon MUST:

1. Persist the group's terminal status via QM-001.
2. Emit `queue_group_completed{queue_id, group_index, final_status: complete-success}`.
3. If a successor group exists, transition it from `pending → active` per QM-031, persist via QM-001, and emit `queue_group_started{queue_id, group_index: <next>}`.
4. If no successor exists, proceed to §8.4 completion.

### 8.3 QM-052 — Pause-by-failure

When the active group reaches `GroupStatus: complete-with-failures` per §5.1 row 4, the daemon MUST:

1. Persist the group's terminal status via QM-001.
2. Transition `Queue.status` from `active → paused-by-failure` and persist via QM-001.
3. Emit `queue_group_completed{queue_id, group_index, final_status: complete-with-failures}`.
4. Emit `queue_paused{queue_id, group_index, fail_count, reason: "group_failure"}`.

No further dispatch occurs while `status == paused-by-failure`. The daemon remains running; `.harmonik/queue.json` persists with `status: paused-by-failure`. v0.1 recovery is daemon restart followed by a fresh `queue-submit` after the operator addresses the failed beads; v0.2 will add `queue-resume`.

### 8.3a QM-052a — Handler-pause gate orthogonality

Handler-type pause (per [/Users/gb/github/harmonik/specs/handler-pause.md §6 HP-025]) is **orthogonal** to the queue-level pause states (`paused-by-failure`, `paused-by-drain`). A handler pause does NOT transition `Queue.status`; it manifests only as a submission-time validation gate and as a dispatcher-level eligibility check that holds individual items without advancing queue state.

**Submission-time gate.** During `queue-submit` and `queue-append` validation (QM-052a step in the QM-029a order), the daemon MUST consult `HandlerPauseController.IsHandlerPaused(agent_type)` for each item in the request. If any item resolves to a paused handler type, the entire request MUST be rejected with `QueueValidationReason: handler_paused` (JSON-RPC error code `-32018` per §6.11a). The rejection payload MUST include the `agent_type` and the list of bead IDs that would dispatch to the paused handler. See [/Users/gb/github/harmonik/specs/handler-pause.md §6 HP-025, §7 HP-009a].

**Orthogonality.** When `Queue.status` is `paused-by-failure` or `paused-by-drain`, a concurrent handler pause has no additional effect on queue state. The queue remains in its existing pause state; the handler pause persists independently and applies when the queue eventually resumes. No `queue_paused` event is emitted for a handler pause.

### 8.4 QM-053 — Complete

When the last group (highest `group_index`) reaches `complete-success` AND no successor exists, the daemon MUST:

1. Transition `Queue.status` from `active → completed` and persist via QM-001.
2. The final `queue_group_completed` event already emitted at §8.2 step 2 is the durable landmark per QM-033.
3. Unlink `.harmonik/queue.json` and `fsync(parent_directory_fd)` per QM-003.
4. Clear the in-memory queue object.

After QM-053, `queue-status` calls return `queue_not_active`. No separate `queue_completed` event is emitted.

### 8.5 QM-054 — Pause-by-drain entry

When the daemon enters operator-pause or shutdown-drain per [/Users/gb/github/harmonik/specs/operator-nfr.md §4.7 ON-027] step (1), the queue MUST transition `Queue.status` from `active → paused-by-drain`. The drain pseudocode (which in-flight runs may complete, which are interrupted, observability obligations) is owned by ON-027 and is NOT duplicated here.

On entry to `paused-by-drain` the daemon MUST:

1. Persist the new queue status via QM-001.
2. Emit exactly one `queue_paused{queue_id, group_index, reason: "operator_drain"}` event per [/Users/gb/github/harmonik/specs/event-model.md §8.10]. The `group_index` is the currently-active group's index.

No new items are dispatched while `status == paused-by-drain`. In-flight runs continue per ON-027 step (2).

> INFORMATIVE: The `operator_pause_status{status: pausing|paused}` event that drives this `active → paused-by-drain` transition is produced in production by the operator-nfr pause/resume command verb (per [/Users/gb/github/harmonik/specs/operator-nfr.md §4.3 ON-056/ON-057]). This requirement specifies the consumer side only; the producer adds no change to the consumer semantics here. The same `operator_pause_status` is the single source of pause truth observed by both this queue transition and the execution-model br-ready fallback gate (per [/Users/gb/github/harmonik/specs/execution-model.md §7.4 EM-067]).

### 8.6 QM-055 — Persisted pause survives restart

`.harmonik/queue.json` written under QM-001 retains `status: paused-by-failure` or `status: paused-by-drain` across daemon restart. On QM-002 read, the queue loads with its persisted pause status and remains paused. v0.1 recovery from a persisted pause is daemon restart + fresh `queue-submit` after operator action; v0.2 will add `queue-resume` and `queue-clear`.

### 8.7 QM-056 — `queue_paused.reason` enumeration

The `reason` field on `queue_paused` events is constrained to the enum:

```
ENUM QueuePauseReason: group_failure, operator_drain
```

This enum is co-owned with [/Users/gb/github/harmonik/specs/event-model.md §8.10]; additions require an event-payload schema bump per [/Users/gb/github/harmonik/specs/event-model.md §6.4].

### 8.8 QM-057 — Status method

`queue-status` returns the current in-memory queue envelope (or `{queue: null}` if no queue is loaded). It MUST NOT mutate state and MUST NOT emit events. The returned envelope is a snapshot at call time; ordering against concurrent mutations is bounded by the daemon's single-writer discipline per §9.

## 9. Concurrency

### 9.1 QM-060 — Single-writer to the queue object

All mutations to the in-memory `Queue` object MUST be serialized through a single writer (the daemon's queue-control goroutine). `queue-submit`, `queue-append`, group state transitions, item state transitions, and queue lifecycle transitions all run through the same writer. Readers (`queue-status`, dispatcher capacity evaluation) MAY snapshot under read lock. v0.1 does not optimize this beyond correctness; lock-free or sharded approaches are deferred.

### 9.2 QM-061 — Single-orchestrator submission

v0.1 assumes a single orchestrator client per daemon. Multi-orchestrator submission semantics (two clients racing to enqueue, queue-ownership ACLs) are out of scope. QM-027 ensures at most one active queue exists, which is the v0.1 multi-orchestrator safeguard.

### 9.3 QM-062 — Composition with `--max-concurrent`

The queue's parallel-group concept composes with the daemon's existing `--max-concurrent N` capacity gate per [/Users/gb/github/harmonik/specs/execution-model.md §4.3]. The dispatcher dispatches up to:

```
min(group_pending_count, --max-concurrent - currently_running)
```

The queue narrows parallelism (a wave of 8 with `--max-concurrent 2` runs 2 at a time) but never widens it. The capacity gate is unchanged from its pre-queue behavior.

### 9.4 QM-063 — Persistence ordering with event emission

For any state-changing operation, the daemon MUST persist via QM-001 BEFORE emitting the corresponding event(s). This ordering mirrors the WM event-emit-after-persist discipline per [/Users/gb/github/harmonik/specs/workspace-model.md §4.4]. Event ordering within a single operation is specified per-operation in §6, §7, and §8.

### 9.5 QM-065 — Event-emission ordering across operations

Events emitted within a single operation are ordered as specified per-operation (§6.1, §7.3, §8.1, §8.2, §8.3, §8.5). Across operations the daemon's emitter ordering follows the EV-002a per-process monotonicity discipline per [/Users/gb/github/harmonik/specs/event-model.md §6.2 EV-002a]; readers tailing `events.jsonl` see queue events in a total order consistent with the queue's mutation history. The single-writer discipline per QM-060 guarantees that no two queue mutations interleave their event emissions.

### 9.6 QM-064 — No mutation during validation

The validation pipeline (§6) MUST run against an immutable snapshot of the in-memory queue. Any mutation accepted concurrently with a validation pass MUST be sequenced after that pass's snapshot via the QM-060 single-writer discipline. Failed validation MUST NOT leave any partial state, intent log, or event emission behind — per QM-028, validation failures surface only on the JSON-RPC response.

## A. Appendices

### A.1 Glossary

- **queue** — the daemon-singleton execution plan object, identified by `queue_id`, persisted at `.harmonik/queue.json`. (see §2.1)
- **group** — an ordered position within the queue containing a set or sequence of items. A group is either a `wave` or a `stream`. (see §2.3)
- **item** — a single bead reference within a group, carrying its dispatch lifecycle state. (see §2.6)
- **wave** — a `Group` of kind `wave`: a fixed closed set of items dispatched concurrently up to `--max-concurrent`, immutable after submit. (see §2.4)
- **stream** — a `Group` of kind `stream`: an ordered open-ended sequence dispatched head-first as slots open, appendable while `pending` or `active`. (see §2.4)
- **queue_id** — daemon-minted UUIDv7 identifier for one queue submission; never client-supplied; returned from `queue-submit` and carried on every queue-lifecycle event. (see §4.1)
- **group_index** — 0-based dense integer index of a group within the queue; immutable after submit. (see §2.3)
- **paused-by-failure** — queue-level lifecycle state entered when an active group reaches `complete-with-failures`; no further dispatches; survives daemon restart. (see §8.3)
- **paused-by-drain** — queue-level lifecycle state entered when the daemon enters operator-pause / shutdown drain per [operator-nfr.md §4.7 ON-027]; survives daemon restart. (see §8.5)
- **deferred-for-ledger-dep** — transient `ItemStatus` for an item whose Beads `blocks` edge is open; resolves to `pending` when the blocker closes. (see §2.7, §6.6)
- **durable landmark** — the final `queue_group_completed` event of a queue, treated as the queue-completion observable per QM-033. (see §5.5)
- **single-writer discipline** — all queue mutations serialize through one writer goroutine per QM-060. (see §9.1)

### A.2 Cross-spec impact summary

| Spec | Section | Nature of impact |
|---|---|---|
| [/Users/gb/github/harmonik/specs/event-model.md] | §8.10 (new) | Owns the six queue-lifecycle event payloads plus the new `queue_item_reconciled` event (row 8.10.7) added in v0.1.1 per QM-002a; this spec cites them by name and reason-enum but does not define payload schemas. |
| [/Users/gb/github/harmonik/specs/event-model.md] | §6.4 row 1 | OPTIONAL `queue_id` / `queue_group_index` fields added to `run_started` / `run_completed` / `run_failed` per QM-011, QM-012. |
| [/Users/gb/github/harmonik/specs/process-lifecycle.md] | §4.4 (new/extended) | Owns the `queue-submit` / `queue-append` / `queue-status` / `queue-dry-run` JSON-RPC method surface and Unix-socket transport. |
| [/Users/gb/github/harmonik/specs/process-lifecycle.md] | §4.1 PL-005 step 8a | Reads `.harmonik/queue.json` per QM-002 alongside `daemon.state` / `daemon.upgrading`. |
| [/Users/gb/github/harmonik/specs/operator-nfr.md] | §4.5 ON-018 | `.harmonik/queue.json` added to the N-1-readable artifact enumeration; this spec's `schema_version: Integer` is the QM contribution. |
| [/Users/gb/github/harmonik/specs/operator-nfr.md] | §4.6 ON-015 | ON-015 reframing: Beads is the bead-store, not the daemon's dispatch input. The daemon's dispatch input is this spec's queue. |
| [/Users/gb/github/harmonik/specs/operator-nfr.md] | §4.7 ON-027 step (1) | Entry point for `paused-by-drain` per QM-054. |
| [/Users/gb/github/harmonik/specs/execution-model.md] | §4.3 | Dispatch loop consumes the queue's `active` group; capacity gate composes per QM-062. |
| [/Users/gb/github/harmonik/specs/execution-model.md] | §7.1 | Per-run state machine layers under per-item state per §2.7 INFORMATIVE note. |
| [/Users/gb/github/harmonik/specs/beads-integration.md] | §4.3 BI-006, §4.5 | `blocks` edge consumed by QM-025; `br show` / status reads consumed by QM-020 / QM-021. |
| [/Users/gb/github/harmonik/specs/workspace-model.md] | §4.7 WM-026 | Atomic-write discipline cited by QM-001. |
| [/Users/gb/github/harmonik/specs/handler-pause.md] | §6 HP-025, §7 HP-009a | Normative dependency introduced in v0.1.2: QM-052a (§8.3a) cites HP-025 for the submission-time gate contract; `handler_paused` enum value and `-32018` error-code allocation cross-reference HP-009a. |

### A.3 v0.1 deferred surface

The following operations are explicitly out of scope for v0.1 and reserved for v0.2:

- `queue-resume` — manual transition `paused-by-failure → active` after operator addresses failed beads.
- `queue-clear` — manual transition `paused-by-drain → (deleted)` for orphan-cleanup paths.
- `queue-remove` — remove a not-yet-dispatched item from a group.
- Pause / stop / kill of in-flight runs at the queue layer.
- Auto-retry, exponential backoff, dead-letter semantics.
- Multi-orchestrator submission, queue-ownership ACLs.
- Stream priorities, weighted scheduling, fairness within `--max-concurrent`.
- Conditional ordering ("run X only if Y succeeded").
- Write coalescing across QM-001 mutations.

### A.4 Changelog

v0.1.4 — 2026-05-31 — Pi-driven dispatch & control-plane confirmations (kerf `pilot` work, A4). Three annotation-only amendments; no new requirement IDs, no new methods, no consumer-semantics change:

1. **§2.4 GroupKind (informative):** Stated that Pi-driven curated dispatch uses a `stream` group (the only appendable kind), that `harmonik run --beads`'s `wave` default is correct for closed batches but must NOT be changed to obtain appendability, and that a `stream` group is both concurrency-safe (per execution-model EM-NOTE-STREAM-CONCURRENCY) and wake-on-append (per execution-model EM-NOTE-WAKE).

2. **§8.5 QM-054 (informative):** Confirmed that the `operator_pause_status` driving the `active → paused-by-drain` transition is produced in production by the operator-nfr pause/resume command verb (ON-056/ON-057), that this changes no consumer semantics, and that the same event is the single source of pause truth observed by both the queue transition and the execution-model br-ready fallback gate (EM-067).

3. **§8.1 QM-050 (informative):** Confirmed that `queue-submit` returning `status: active` IS the queue's "start" semantics; there is no separate `start` method, and a Pi-driven flow "starts processing" by submitting the queue.

Source: kerf `pilot` 04-design/queue-model-design.md. No QM requirement IDs added, renumbered, or retired.

v0.1.3 — 2026-05-20 — QM-002b three-way reconciliation on startup (hk-nvfvj / hk-11xlj). One additive amendment documenting the implementation that landed in 15c0ad8:

1. **§3.2b (new) — QM-002b three-way reconciliation.** After QM-002a, the daemon runs a full three-way pass covering three mismatch classes: Class A (`bead_closed_queue_pending` — pending/deferred item for an already-closed bead → advance to completed + persist + emit); Class B (`bead_inprogress_queue_absent` — in_progress ledger bead with no queue item → emit only); Class C (`bead_closed_queue_inprogress` — queue terminal but ledger in_progress → emit only). Ordering: Class A mutations collected and persisted via QM-001 before any event emission (QM-063); Class B enumerated via `br list --status in_progress`; events emitted in A → C → B order. All corrections are direct (no reconciliation-investigator routing) because all three classes are fully deterministic.

2. **§4.a events-produced (additive):** Added `reconciliation_mismatch_observed` (Class O; payload schema reserved at [event-model.md §8.6.15]; emission rule §3.2b QM-002b).

3. **§4.a boundary table (additive):** Added `three_way_reconcile` (§3.2b QM-002b) row; all axes identical to `startup_cross_check`.

v0.1.2 — 2026-05-19 — QM-052a handler-pause gate amendment (hk-75rij). Three additive amendments landing `ReasonHandlerPaused` implemented at 298624d (hk-siuo2) as a normative spec requirement:

1. **§6.10 QM-029 — enum extension.** Added `handler_paused` to `QueueValidationReason` enum.

2. **§6.11 QM-029a — evaluation order.** Added QM-052a (handler-pause gate, submit and append) between QM-022 and QM-023.

3. **§6.11a QM-029b — error-code mapping.** Allocated `-32018` → `handler_paused` (QM-052a). `-32019` remains reserved.

4. **§8.3a QM-052a (new) — Handler-pause gate orthogonality.** Normative submission-time gate requirement and orthogonality clause: handler-type pause does not modify `Queue.status`; a concurrent queue-level pause and handler pause coexist independently.

v0.1.1 — 2026-05-15 — gap-closure pass (hk-089gr). Six additive amendments surfaced by 3-reviewer parallel pass on the extqueue v0.1 spec commit (e228bc3):

1. **§2.10 (new) — JSON-RPC request/response payload schemas.** Normative RECORD definitions for `QueueSubmitRequest`, `QueueSubmitResponse`, `QueueAppendRequest`, `QueueAppendResponse`, `QueueStatusResponse`, `QueueDryRunRequest`, and `QueueDryRunResponse`. Clarifies daemon-minted vs. client-supplied fields.

2. **§6.11a (new) — QM-029b validation reason to error-code mapping.** Table mapping all 8 `QueueValidationReason` enum values to specific JSON-RPC error codes in the `-32010..-32017` range; `-32018` and `-32019` reserved at this version. (Amended by v0.1.2: `-32018` allocated to `handler_paused`; stable range is now `-32010..-32018`; `-32019` is the sole remaining reserved slot.)

3. **§2.8 — Deferred-item re-evaluation trigger.** Added normative sentence requiring the dispatcher to re-evaluate `deferred-for-ledger-dep` items on every dispatch-loop tick per execution-model.md §7.4. Notes v0.2 optimization deferral.

4. **§3.2 (QM-002) — schema_version supported-read-set.** v0.1 supports `schema_version ∈ {1}`; any other value refuses startup with exit code 2.

5. **§3.2a (new) — QM-002a startup cross-check against Beads ledger.** At startup, after loading queue.json, the daemon MUST cross-check `dispatched` items against Beads. If Beads shows `open`, the item reverts to `pending` via QM-001 atomic write and emits `queue_item_reconciled{reason: claim_write_lost}`.

6. **§3.1 (QM-001) — I/O error behavior.** On any I/O error in the atomic-write sequence, the daemon MUST refuse further mutations, emit `infrastructure_unavailable{failed_prerequisite: queue_write_error}`, and transition to `degraded` state. Operator recovery is `harmonik stop` + restart.

v0.1.0 — initial publication for extqueue work; see kerf/extqueue 05-changelog.md.
