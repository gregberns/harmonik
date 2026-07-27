# Queue Model

```yaml
---
title: Queue Model
spec-id: queue-model
requirement-prefix: QM
status: draft
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.1.6
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-07-27
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

This spec defines the daemon-owned execution-plan data model that an external orchestrator submits, appends to, and queries. It owns the queue envelope, the two group primitives (wave and stream), the per-item record, canonical named-queue persistence under `.harmonik/queues/`, queue-owned replace/archive intents and completion receipts, the validation contract applied at every mutation, the `queue_id` identity discipline, the group-level state machine, and the queue-level lifecycle states (`active`, `paused-by-failure`, `paused-by-drain`, `completed`).

The spec does NOT own: the CLI surface and JSON-RPC transport (owned by [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4]), event-payload field schemas for queue-lifecycle events (owned by [/Users/gb/github/harmonik/specs/event-model.md §8.10]), the dispatch loop and per-run state machine (owned by [/Users/gb/github/harmonik/specs/execution-model.md §4.3, §7.1]), bead-status semantics or the `blocks`-edge contract (owned by [/Users/gb/github/harmonik/specs/beads-integration.md §4.3, §4.5]), pause/drain pseudocode (owned by [/Users/gb/github/harmonik/specs/operator-nfr.md §4.7 ON-027]), and operator-control state transitions (owned by [/Users/gb/github/harmonik/specs/operator-nfr.md §4.3]).

### 1.1 In scope

- The `Queue` envelope record (`schema_version`, `queue_id`, `status`, `groups`).
- The `Group` record and `GroupKind ∈ {wave, stream}` / `GroupStatus` enums.
- The `Item` record and `ItemStatus` enum.
- `.harmonik/queues/<normalized-name>.json` canonical persistence; migration-only read of legacy `.harmonik/queue.json`; queue-owned transaction intents, completion receipts, release markers, startup recovery, cleanup, and retention.
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

The daemon owns a set of named queues. At most one live queue exists for each normalized name; each queue is identified by a daemon-minted `queue_id`. The queue envelope contains an ordered list of `Group` records; each group contains an ordered list of `Item` records.

### 2.1 RECORD Queue

```
RECORD Queue:
  schema_version    : Integer     -- MUST equal 1; N-1 readable per [operator-nfr.md §4.5 ON-018]
  queue_id          : UUID        -- daemon-minted UUIDv7 at queue-submit accept; never client-supplied
  submitted_at      : Timestamp   -- ISO 8601 with ms, UTC; set at queue-submit accept
  groups            : List<Group> -- ordered; at least one entry; group_index is dense 0..N-1
  status            : QueueStatus -- queue-level lifecycle state (see §2.2)
  name              : String      -- durable routing key (NQ-A1); omitted or empty = "main"; per-queue
                                  -- on-disk file is .harmonik/queues/<name>.json (NQ-A2)
  workers           : Integer     -- per-queue concurrent-dispatch ceiling (QM-066, NQ-B1); omitted/0
                                  -- defaults to --max-concurrent; may oversubscribe (global cap still wins)
```

> INFORMATIVE: **Named queues have no special semantics (N4).** The `name` field is a durable routing key — it determines which `.harmonik/queues/<name>.json` file the queue persists to and which per-queue worker pool dispatches it. The daemon assigns no special behavior to any particular name. For example, the flywheel bridge (per [/Users/gb/github/harmonik/specs/cognition-loop.md]) routes investigation beads to an 'investigate' named queue — that queue is mechanically identical to 'main'; the routing to a subscription-billed Claude worker is a property of which daemon process subscribes to it, not of the queue-model itself. There is no per-queue budget (N2): the queue-model is a mechanism-tagged subsystem with no gate/hook/budget points per §4.1(f). Any cost governance lives at the credential-isolation layer ([/Users/gb/github/harmonik/specs/credential-isolation.md]), not here.

### 2.2 ENUM QueueStatus

```
ENUM QueueStatus:
  active              -- groups are advancing per §5
  paused-by-failure   -- entered per §8.3 when a group reaches complete-with-failures
  paused-by-drain     -- entered per §8.5 when the daemon enters operator-pause / shutdown drain
  completed           -- all groups complete-success; durable landmark is the queue-owned completion receipt
  cancelled           -- operator cancelled the run (SIGINT/SIGTERM or global timeout) before all groups reached
                         a terminal state; the canonical named queue remains owned until the linked QM-007
                         cancellation archive transaction durably archives it and releases the name;
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
  bead_id           : BeadID                -- Beads ledger reference; immutable
  status            : ItemStatus            -- per-item state (see §2.7)
  run_id            : UUID | None           -- daemon-minted on transition to dispatched per [execution-model.md §4.3]
  appended_at       : Timestamp | None      -- set when appended post-submit (streams only); None for submit-time items
  workflow_mode     : String | None         -- optional per-item workflow-mode override (EM-012a)
  workflow_ref      : String | None         -- optional path to the dot workflow when workflow_mode="dot"
  context           : String | None         -- optional free-form Extra Context injected into the agent brief
  template_params   : Map<String,String> | None  -- launch-time __KEY__ substitution params for the dot workflow ([workflow-graph.md §4 WG-045])
```

**`template_params` ingestion validation (normative).** Template params are **UNTRUSTED** ([workflow-graph.md §4 WG-045]): they are settable by any local agent over the queue-submit RPC and MAY carry external data. The daemon MUST validate `template_params` at submit time and **reject** the request (typed JSON-RPC error, before persist) when any key does not match `^[A-Z][A-Z0-9_]*$` (or exceeds 128 bytes), or any value contains a NUL/newline/other ASCII or Unicode control character, or any value exceeds an 8192-byte cap. Shell metacharacters in values are NOT rejected here — neutralising them is the post-parse shell-quoting close of [workflow-graph.md §4 WG-045], not the validator's job. The same validation is re-applied at the substitution chokepoint to cover the daemon-down local-persist path that bypasses the RPC. `queue-append` carries no `template_params`, so `queue-submit` is the sole ingestion chokepoint.

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

Each `.harmonik/queues/<normalized-name>.json` canonical file is the JSON
serialization of one `RECORD Queue` envelope. Legacy `.harmonik/queue.json`
uses the same envelope but is migration-only input for `main`. Field names are
snake_case; timestamps are ISO 8601 strings with millisecond precision and UTC
`Z` suffix; UUIDs are lowercase canonical-form strings; enums are their
declared lowercase identifier strings.

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

The five queue methods (`queue-submit`, `queue-append`, `queue-status`,
`queue-dry-run`, `queue-cancel`) are carried over the daemon's Unix socket per
[/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4 PL-003a]. This
section defines the normative request and response payload shapes. The
transport framing (NDJSON, 1 MiB cap, error-code block) is owned by PL-003a;
this section owns the field-level wire contract.

#### RECORD QueueSubmitRequest

```
RECORD QueueSubmitRequest:
  groups           : List<Group>   -- one or more groups; field schemas per §2.3
  schema_version   : Integer = 1   -- MUST equal 1; forward-incompatible value refuses per QM-002
  name             : String?       -- normalized durable routing name; empty defaults to main
  workers          : Integer?      -- existing per-request worker ceiling
  spend_cap_usd    : Number?       -- existing optional queue spend ceiling
  default_harness  : AgentType?    -- existing optional default harness
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
  queue_id        : UUID?         -- exact identity selector
  name            : String?       -- normalized-name selector
  group_index     : Integer       -- 0-based index of the target stream group
  bead_ids        : List<BeadID>  -- beads to append; validated per QM-020..QM-024
```

A non-empty `queue_id` takes precedence over `name`; otherwise a non-empty
`name` selects that normalized queue, and when both are absent the selector is
`main`.

#### RECORD QueueAppendResponse

```
RECORD QueueAppendResponse:
  appended_count      : Integer         -- number of items accepted and appended
  new_tail_indices    : List<Integer>   -- 0-based item indices of the newly appended items within the target group
```

#### RECORD QueueStatusRequest

```
RECORD QueueStatusRequest:
  name                 : String?
  queue_id             : UUID?
  watched_group_index  : Integer?
```

A non-empty `name` takes precedence over `queue_id`; otherwise a non-empty
`queue_id` selects exact identity, and when both are absent the selector is
`main`. `watched_group_index`, including zero, is optional and never changes
selector precedence.

#### RECORD QueueStatusResponse

```
RECORD QueueStatusResponse:
  queue                   : Queue | null       -- exact live queue when present
  max_concurrent          : Integer | None     -- existing daemon concurrency ceiling
  completed               : Bool               -- true only for receipt-backed response
  final_status            : GroupStatus | None -- receipt-backed final status
  final_group_index       : Integer | None
  success_count           : Integer | None
  fail_count              : Integer | None
  completed_at            : Timestamp | None
  completion_receipt_id   : UUID | None
```

`queue` is null for a receipt-backed response. `queue-status` MUST NOT mutate
state or emit events per QM-057.

#### RECORD QueueCancelRequest / QueueCancelResponse

```
RECORD QueueCancelRequest:
  queue     : String?   -- shipped JSON selector; normalized queue name
  queue_id  : UUID?     -- additive exact-identity selector
  force     : Bool?     -- shipped force behavior

RECORD QueueCancelResponse:
  queue_id      : UUID?
  prior_status  : QueueStatus?
```

An N-1 request carrying non-empty `queue` and `force` remains valid unchanged.
When only `queue_id` is non-empty, the daemon selects that exact identity.
When both selectors are non-empty, the daemon resolves both and MUST reject a
different identity as `queue_selector_conflict`; matching selectors identify
the same queue and proceed. When neither selector is non-empty, the request is
invalid; queue-cancel MUST NOT silently default to `main`. `force` preserves
the shipped ability to cancel an already-terminal queue. A supplied selector
that resolves to no queue is an idempotent success with an empty response
`queue_id`.

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

The daemon/project `QueueStore` is the sole runtime mutation owner. Canonical
live queues are `.harmonik/queues/<normalized-name>.json`. Legacy
`.harmonik/queue.json` is a migration-only input for `main`, never a new-write
target. Queue-owned replace/archive intents and the flat
`.harmonik/queues/.completion-receipts/` root are protocol records, never live
queues.

### 3.1 QM-001 — Atomic write discipline

Every canonical queue replacement MUST execute inside the QueueStore
transaction domain for its normalized name. It MUST validate an immutable
detached snapshot and volatile process-local generation, deep-clone/create the
candidate, marshal exact bytes and any normal-path observation payload, make a
unique sibling candidate temp durable, make an exact v1 replace intent
directory-durable, rename the selected temp to the canonical basename, and
`fsync` the queue directory before installing memory and advancing generation
once. Stale generation MUST be rejected before I/O. Generation MUST NOT be
persisted or used as restart evidence.

The deterministic ordinary intent path is
`.harmonik/queues/<normalized-name>.replace-intent`. Canonical v1 bytes contain
exactly `schema_version`, `transaction_id`, `operation_kind`,
`normalized_name`, `queue_id`, `canonical_basename`, `prior_state`
(canonical absence or SHA-256), `candidate_sha256`,
`candidate_temp_basename`, `wake_required`, and optional
`completion_receipt_binding` or `archive_handoff_binding`. Installation uses a
unique sibling temp create/write/fsync/close followed by no-replace install at
that exact path and queue-directory fsync. Exact existing bytes are
idempotent; different, corrupt, or unsupported bytes are preserved and refuse
the name.

The replace intent MUST bind one UUIDv7 transaction ID, operation kind,
normalized name, queue ID, exact canonical basename, exact prior state
(absence or SHA-256 of prior bytes), candidate SHA-256, already-durable
candidate-temp basename, wake requirement, and any completion-receipt or
archive-handoff binding. It MUST cover create, submit, append, reservation,
Run-ID patch, activation/advance, pause/resume, eager refill, budget/review
charge, maintenance/reconciliation, startup/inline/bootstrap, crew placeholder,
and cancelled-state replacement before archive. It MUST contain no event ID,
effect key, cursor, delivery/acknowledgement state, payload batch, or RPC
response.

For an ordinary successful replacement, after canonical rename and
queue-directory fsync the QueueStore installs memory/generation and performs
the required wake/observation attempt, then unlinks the exact resolved replace
intent and fsyncs the queue directory. The name remains owned/refused until
intent absence is directory-durable. A definite pre-namespace intent or
canonical failure is `not_committed` and retains sufficient exact facts for
retry; an ambiguous rename/unlink/directory-sync reloads the exact intent,
canonical, and selected temp before classification.

Restart classification is exhaustive: prior canonical plus exact selected
candidate temp retries only the intent-selected canonical rename; exact
candidate canonical establishes parent durability then removes the intent;
exact prior/absence plus the selected abandoned temp proves replacement not
committed, removes only that temp, then removes/syncs the intent; exact
prior/absence with no selected temp removes/syncs the intent as not committed.
Any third canonical state, changed digest, wrong temp, corrupt/unsupported
intent, or binding mismatch preserves all evidence and refuses the name.
Ordinary intent-unlink definite failure retains refusal and retries the exact
unlink; unlink or parent-sync ambiguity reloads exact absence/presence and
syncs before release. Final-success and cancellation intents remain through
their later QM-053/QM-007 cleanup boundaries instead of ordinary cleanup.

Outcomes are typed: `rejected` performs no namespace I/O;
`not_committed` proves the intended namespace mutation absent;
`committed_durable` proves the selected namespace state parent-synced; and
`commit_indeterminate` means a namespace mutation occurred but parent
durability is unresolved. Indeterminate state retains the prior memory install
and refuses that name until exact intent/canonical/temp reload, byte/digest
comparison, and parent sync classify it. A close failure after successful
directory fsync is diagnostic. No event outcome changes these results.

### 3.2 QM-002 — Read on startup

At PL-005 startup, before readiness or QueueStore installation, the bounded
same-process startup adapter MUST establish and type-check the receipt root,
classify every supported replace/archive intent, completion receipt, release
marker, canonical queue, and legacy-main state, and either converge it or
retain an exact per-name refusal. It MUST NOT consult JSONL, persisted
generation, or event delivery state.

Canonical schema version 1 remains readable. For legacy `main`: valid legacy
only MUST be copied to directory-durable canonical bytes before legacy unlink
and `.harmonik` directory fsync; valid canonical only loads; byte-equivalent
copies retain canonical and durably remove legacy; divergent, corrupt, or
unsupported copies are preserved and refuse `main`. Every rename/unlink
parent-sync ambiguity reloads exact entries and MUST preserve at least one
valid copy.

Loaded paused queues remain paused. Startup emits no synthetic queue lifecycle
event and never walks event history to reconstruct queue state.

### 3.2a QM-002a — Startup cross-check against Beads ledger

After loading each canonical named queue successfully per QM-002, the daemon
MUST cross-check item statuses against the live Beads ledger. For every item
whose `status` is `dispatched` in that canonical envelope, the daemon MUST
query `br show <bead_id>` per
[/Users/gb/github/harmonik/specs/beads-integration.md §4.5]. If the Beads
ledger shows the bead as `open` (a prior queue reservation succeeded but the
corresponding Beads status write failed), the daemon MUST:

1. Revert the item's status to `pending` in the in-memory queue envelope.
2. Persist the corrected queue envelope via QM-001 atomic write before proceeding.
3. Emit a `queue_item_reconciled` event per [/Users/gb/github/harmonik/specs/event-model.md §8.10] with `reason: claim_write_lost`.

This check MUST run before the daemon reaches `ready` state and before any dispatch-loop tick that could re-dispatch an item. The reconciliation action maps to Cat 3 per [/Users/gb/github/harmonik/specs/reconciliation/spec.md §8] (claim-write-lost store disagreement). In v0.1 the daemon executes the revert directly rather than routing through the reconciliation investigator, because the correction is fully deterministic (ledger says `open` → item reverts to `pending`).

### 3.2b QM-002b — Three-way reconciliation on startup

After QM-002a completes, the daemon MUST run a full three-way reconciliation pass that covers mismatch classes not reachable by the `dispatched`-items-only scan. Four mismatch classes are defined:

**Class A — `bead_closed_queue_pending`:** A queue item has `status=pending` (or `status=deferred-for-ledger-dep`) but the Beads ledger shows the bead as `closed` or `tombstone`. The item is waiting for a bead that has already finished. The daemon MUST:

1. Advance the item's status to `completed` in the in-memory queue envelope.
2. Persist the corrected queue envelope via QM-001 atomic write (per QM-063 — persist BEFORE emit).
3. Emit `reconciliation_mismatch_observed` per [/Users/gb/github/harmonik/specs/event-model.md §8.6.15] with `mismatch_class: "bead_closed_queue_pending"`.

**Class B — `bead_inprogress_queue_absent`:** The Beads ledger reports a bead as `in_progress` but no queue item references that bead. The daemon MUST emit `reconciliation_mismatch_observed` with `mismatch_class: "bead_inprogress_queue_absent"` and log a structured warning for operator visibility. No queue mutation is applied — the orphan-sweep (hk-2ty0g) handles queue-owned remediation.

**Class C — `bead_closed_queue_inprogress`:** A queue item has `status=completed` or `status=failed` but the Beads ledger still shows the bead as `in_progress`. The queue-side terminal is already set; no queue mutation is applied. The daemon MUST emit `reconciliation_mismatch_observed` with `mismatch_class: "bead_closed_queue_inprogress"` and log a structured warning for operator visibility.

**Class D — `queue_paused_by_drain_item_stranded`:** The queue's own `status` is `paused-by-drain` (abandoned — a `paused-by-drain` queue does not auto-resume across a daemon restart in v0.1, per §3.2 QM-002) and it still carries an item with `status=pending` or `status=deferred-for-ledger-dep`. That item will never be dispatched from this queue again, but the EM-065 cross-queue occupancy guard [/Users/gb/github/harmonik/specs/execution-model.md §4.14] still counts any non-terminal item as "claimed" for its bead — permanently blocking that bead from being queued anywhere else even while the bead itself remains open. Unlike Class A, this correction does NOT depend on the Beads ledger status: the queue's own abandonment is sufficient grounds to release the item. The daemon MUST:

1. Advance the item's status to `failed` in the in-memory queue envelope (the queue-level `status` is left untouched — it remains `paused-by-drain`).
2. Persist the corrected queue envelope via QM-001 atomic write (per QM-063 — persist BEFORE emit).
3. Emit `reconciliation_mismatch_observed` per [/Users/gb/github/harmonik/specs/event-model.md §8.6.15] with `mismatch_class: "queue_paused_by_drain_item_stranded"`.

**Execution ordering (per QM-063):**

1. Scan all queue items; collect Class A/D mutations and all pending event payloads for Classes A, C, and D. Class D takes priority over Class A for the same item (a paused-by-drain queue is abandoned regardless of ledger status).
2. If any Class A/D mutations were collected: persist the corrected queue envelope via QM-001 before proceeding.
3. Enumerate in-progress Beads ledger entries (via `br list --status in_progress`); collect Class B payloads for any bead not referenced by a queue item.
4. Emit all collected events in the order: Class A/D, then Class C, then Class B.

This pass MUST complete before the daemon reaches `ready` state and before any dispatch-loop tick. In v0.1 corrections are applied directly (no reconciliation-investigator routing) because all four classes are fully deterministic given the observed store state.

### 3.3 QM-003 — Removal on completion

Final canonical removal is governed only by QM-053. The QueueStore/startup
primitive MUST unlink a canonical path only when its exact queue ID and
SHA-256 of its exact completed bytes both match the authoritative receipt.
Exact absence is idempotent after queue-directory durability is established. A
different queue ID is a newer same-name queue and MUST NOT be touched. The same
queue ID with different bytes, or corrupt bytes, MUST be preserved and the
affected identity quarantined.

### 3.4 QM-004 — Persistence size bound

Each persisted canonical named-queue envelope MUST NOT exceed 1 MiB (1048576
bytes) after any proposed mutation. This bounds atomic-write cost on every
mutation per QM-001 and bounds memory. Violations are rejected at the
validation layer per QM-026; no truncation, no auto-split.

### 3.5 QM-005 — Completion receipt and root

For final `complete-success`, the final replace intent MUST preallocate exactly
one canonical lowercase UUIDv7 `receipt_id` and bind the exact basename,
schema version, base64 canonical bytes, and SHA-256 digest of one completion
receipt before installing the completed canonical queue. Recovery MUST reuse
that binding; it MUST NOT mint, reconstruct, retimestamp, or scan-select a
receipt.

The immutable receipt path is:

```text
.harmonik/queues/.completion-receipts/<queue_id>--<receipt_id>.json
```

Canonical receipt v1 contains `schema_version`, `queue_id`, `receipt_id`,
final transaction ID, normalized name, final group index,
`final_status: complete-success`, success count, `fail_count: 0`, the one
prebound completion timestamp, and SHA-256 of the exact completed canonical
queue bytes. Filename/content identities and all bindings MUST agree.

Before queue recovery or readiness, the daemon MUST create or verify the flat
receipt root. `EEXIST` succeeds only after open/type verification. First
creation becomes durable only after `.harmonik/queues` open/fsync/close;
ambiguity reloads before recovery. Wrong type or unsupported capability fails
startup closed. The root MUST NOT be enumerated as a queue.

A receipt installs by unique root-temp create/write/fsync/close, no-replace
rename to the exact bound basename, then receipt-root open/fsync/close. An
existing target is idempotent only when exact bytes, digest, schema, filename
IDs, and content IDs agree. Conflict is preserved and fails closed.
Rename/root-sync ambiguity reloads exact target/temp before retry. Receipt
durability exists only after root fsync; later close failure is diagnostic.

Tags: mechanism

### 3.6 QM-006 — Completion-release marker, retention, and GC

Receipt completion time, filesystem times, JSONL, events, and process-local
timers MUST NOT determine retention. The immutable durable retention anchor is:

```text
.harmonik/queues/.completion-receipts/
  <queue_id>--<receipt_id>.release-v1.json
```

Marker v1 contains `record_type: completion-release`, `schema_version: 1`,
queue ID, receipt ID, final transaction ID, SHA-256 of exact receipt bytes,
the receipt's completed-queue digest, `released_at`, and `gc_not_before`.
`released_at` MUST be sampled only after old canonical and final-intent absence
are directory-durable and QueueStore/refusal ownership has released.
`gc_not_before` MUST equal `released_at + 720h` in canonical UTC arithmetic;
overflow or non-canonical timestamps are invalid. Marker bytes are immutable.

Installation uses the QM-005 temp/file-sync/no-replace/root-sync discipline at
the deterministic marker basename. A valid existing bound marker wins
unchanged. Different, corrupt, or unsupported bytes are preserved and disable
GC for that receipt without weakening receipt-backed status or same-name
admission. Rename/root-sync ambiguity reloads exact target/temp. If the target
is durably absent, a later attempt may sample a later time.

After a crash with a receipt but no marker, startup or queue-owned maintenance
may create the marker only after exact classification proves: the old
completed canonical is durably absent or the name has a different queue ID; no
unresolved intent belongs to the completed transaction; no old in-memory owner
survives; and receipt/bindings validate. It then samples a new, conservative
`released_at`; it MUST NOT infer an earlier time.

GC requires trusted synchronized UTC. Unavailable/unsynchronized or regressed
clock, `now < released_at`, invalid arithmetic, or
`now < gc_not_before` makes the receipt ineligible. A backward step therefore
only delays deletion. Once eligible, GC MUST revalidate the exact pair,
unlink and receipt-root-sync the receipt first, and only after durable receipt
absence unlink and receipt-root-sync the marker. Every unlink/root-sync
ambiguity reloads exact entries. A marker-only orphan remains through its own
boundary, then may be removed without recreating a receipt. GC never touches a
canonical queue, blocks readiness/admission, or removes the root.

Tags: mechanism

### 3.7 QM-007 — Linked cancellation archive handoff

The deterministic successor path is
`.harmonik/queues/<normalized-name>.archive-intent`. Its canonical v1 record
contains exactly `schema_version`, `archive_intent_id`,
`predecessor_transaction_id`, `archive_origin`, `archive_kind`,
`normalized_name`, `source_identity`, `source_sha256`, and
`destination_basename`. `source_identity` is either the parseable queue ID or
the exact corrupt-source pathname/digest evidence. The predecessor binds the
exact successor ID, canonical bytes, and SHA-256 digest. No digest of the
final predecessor record appears in the successor digest domain.

Before a cancelled canonical replacement becomes durable, its replace intent
MUST bind archive origin (`operator-cancel`, `graceful-shutdown`,
`inline-failure`, or `recovery-corrupt`), archive kind, exact source
identity/digest, selected non-overwriting destination basename, and one exact
successor archive-intent ID/bytes/digest. The predecessor remains until that
successor completes create/write/fsync/close, no-replace rename, and
queue-parent fsync.

The predecessor transaction ID and successor archive-intent ID MUST be
preallocated before either record's canonical bytes are constructed. A linked
pair validates only when the successor bytes equal the predecessor-bound
bytes, SHA-256 of those bytes equals the predecessor-bound digest,
`archive_intent_id` and `predecessor_transaction_id` equal the predecessor's
bound successor ID and transaction ID, and every duplicated
origin/kind/name/source/destination fact agrees.

Successor installation uses a unique sibling temp create/write/fsync/close,
no-replace rename to the exact archive-intent path, and queue-parent fsync.
Exact existing bytes are idempotent; any different/corrupt/unsupported record
refuses without replacement. Recovery exhaustively classifies:
predecessor-only creates only its prebound successor; exact linked pair first
establishes successor parent durability then unlinks/syncs the predecessor;
exact successor-only continues only after validating its complete schema and
the exact normalized path, source identity/digest, and selected destination
namespace facts; any missing binding, changed predecessor, mismatched pair,
third destination, or originless cancelled canonical preserves every fact and
refuses the name.

For a parseable source, archive selection is by exact canonical path, queue ID,
and source digest. For a corrupt recovery source, selection is by the exact
bound source pathname and digest and never depends on parsing it. Archive
destination installation is no-replace: exact existing destination bytes are
idempotent, while changed source or a different destination refuses. Definite
rename failure retains the successor; ambiguous archive rename or parent sync
reloads exact source/destination/intent facts before retry.

After archive and applicable legacy durability, the daemon unlinks the exact
successor and fsyncs the queue directory. Definite unlink failure retains
ownership; unlink or parent-sync ambiguity reloads exact successor
presence/absence before retry. For `main`, legacy absence MUST be durable
through `.harmonik` fsync before successor-intent removal. Ownership releases
only after archive, legacy absence, predecessor absence, and successor absence
are durable.
Cancellation creates no completion receipt. Graceful shutdown emits no
operator-cancellation audit. A daemon-unreachable queue CLI performs no local
queue/archive/intent/receipt/event write.

Tags: mechanism

## 4. Identity

### 4.a Subsystem envelope

#### QM-ENV-001 — Envelope declaration

Envelope for the queue-model subsystem per [/Users/gb/github/harmonik/specs/architecture.md §4.0 AR-053]. The queue-model is a daemon-owned, per-project orchestrator-side subsystem; it owns named queue envelopes, group/item records, canonical queues and queue-owned intent/receipt/marker records under `.harmonik/queues/`, validation, the group state machine, and queue lifecycle states.

(a) Events produced:
  - `queue_submitted` — emission rule §8.1; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.10.1]. Class F.
  - `queue_group_started` — emission rule §8.1, §8.2 step 3; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.10.2]. Class O.
  - `queue_group_completed` — normal-path observation rule §8.2 and §8.4; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.10.3]. Class F; never completion authority.
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

(d) Handlers implemented: none. The queue-model is a daemon/project subsystem;
it does not expose a handler. The JSON-RPC surface (`queue-submit`,
`queue-append`, `queue-status`, `queue-dry-run`, and queue cancellation owned
by process-lifecycle transport) is carried over the daemon's Unix socket per
[/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4 PL-003a] — that is a
transport surface, not a handler-contract handler.

(e) State owned:
  - named `Queue` records (§2.1) — QueueStore-installed immutable snapshots.
  - canonical queues, replace/archive intents, flat completion receipts, and completion-release markers under `.harmonik/queues/` (§2.9, §3); legacy `.harmonik/queue.json` is migration-only input.
  - `Group` and `Item` records (§2.3, §2.6) — wholly contained in the `Queue` envelope; lifecycle ownership per §5 (group state machine), §2.7 (item status), §8 (queue lifecycle).
  - `Run` records are consumed but NOT owned here ([/Users/gb/github/harmonik/specs/execution-model.md §6.1]).

(f) Control points provided: none. The queue-model is a mechanism-tagged subsystem; its operations are not gate/hook/guard/budget points per [/Users/gb/github/harmonik/specs/control-points.md §4.1]. Operator-control state transitions that affect the queue (pause/drain/resume) are inherited from [/Users/gb/github/harmonik/specs/operator-nfr.md §4.3].

(g) NFRs inherited / overridden:
  - Inherited: `ON-018` N-1 schema compatibility (§3.2 applies it to each canonical queue envelope's `schema_version`).
  - Inherited: `ON-027` graceful-shutdown ordering (§8.5 transitions the queue to `paused-by-drain` on operator pause/stop; the dispatcher drains in-flight items per [/Users/gb/github/harmonik/specs/operator-nfr.md §4.7 ON-027]).
  - Overridden: none.

(h) Boundary classification per operation:
  | Operation | `Tags:` | Axes |
  |---|---|---|
  | `queue_submit_accept` (§6, §8.1) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `queue_append_accept` (§6.5 QM-024, §7.3) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `queue_status_read` (§2.10) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `queue_dry_run` (§6.11a, §2.10) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `replace_canonical_queue` (§3.1 QM-001) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `receipt_governed_cleanup` (§3.3 QM-003, §8.4 QM-053) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `completion_receipt_gc` (§3.6 QM-006) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
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

For a stream group in `active`, the dispatcher MUST select the earliest-indexed item whose `status` is `pending`. `deferred-for-ledger-dep` items and terminal items (completed, failed) are skipped during the scan; the first `pending` item found is dispatched. Items appended after submit (per §7) are placed at the tail; the head-first scan ensures appended items dispatch in order, after all earlier non-deferred items have at least entered `dispatched`. A `deferred-for-ledger-dep` item does NOT block dep-free tail items — the scan skips past it and returns the next `pending` item regardless of position (hk-cb5ow).

### 5.7 QM-036 — Wave dispatch admission

For a wave group in `active`, the dispatcher MAY admit any `pending` item, in any order, up to the QM-062 capacity. There is no implied ordering within a wave. Waves with QM-025-deferred items still admit non-deferred siblings concurrently; the deferred items remain `deferred-for-ledger-dep` until their blockers resolve, and only then become eligible.

### 5.9 QM-033 — Completion receipt is the durable landmark

The immutable QM-005 completion receipt is the authoritative durable landmark
for final queue success. `queue_group_completed` is a derived normal-path
observation and MUST NOT govern completion, cleanup, recovery, release,
retention, admission, or exact-ID status. No separate `queue_completed` event
is emitted. Restart MUST NOT inspect JSONL, synthesize, or retry the final
observation.

## 6. Validation

Every `queue-submit`, `queue-append`, and `queue-dry-run` request MUST pass the validation checks in this section, evaluated in the order listed. The first failing check returns a typed JSON-RPC error and MUST NOT mutate state (no in-memory mutation, namespace write, or event emission). Validation failures are NOT events — they surface only on the JSON-RPC response to the caller. The JSON-RPC error code is allocated from the `-32010..-32019` range reserved for `queue-model` per [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4 PL-003a]; the error `message` field carries the typed-error shape shown in each subsection below, following the PL-003b convention (`<error_type>{"<key>":"<value>"}`).

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

After applying the proposed mutation to the detached candidate (without
persisting), the daemon MUST compute the resulting canonical named-queue
envelope size and reject if it exceeds 1 MiB per QM-004. Returns:

```
queue_validation_failed{
  reason: "queue_too_large",
  proposed_bytes: N,
  limit: 1048576
}
```

### 6.8 QM-027 — Single active queue per normalized name

A `queue-submit` request MUST be rejected if its normalized name is owned by a
live queue, unresolved intent, quarantine, or identity-integrity error.
Receipts and release markers alone MUST NOT block same-name reuse. Returns:

```
queue_validation_failed{
  reason: "queue_already_active",
  existing_queue_id: <uuid>,
  existing_status: "<status>"
}
```

`queue-submit` after old canonical absence and ownership release is permitted
and begins a fresh queue with a fresh `queue_id`, even while the old receipt is
retained.

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

`queue-submit` validates per §6, mints `queue_id`, and commits one QM-001
candidate containing both `status: active` and `group_index: 0` active. It MUST
NOT expose a separately persisted group-zero activation. After durable install
and repeatable Wake it attempts, in order:

1. `queue_submitted{queue_id, group_count}`
2. `queue_group_started{queue_id, group_index: 0}`
3. zero or more `queue_item_deferred_for_ledger_dep` events per QM-025.

Event payload schemas are owned by [/Users/gb/github/harmonik/specs/event-model.md §8.10].

> INFORMATIVE: `queue-submit` returning `status: active` IS the queue's "start" semantics: group_index 0 activates immediately on submit and the dispatcher picks it up at sub-poll-interval latency (per [/Users/gb/github/harmonik/specs/execution-model.md §4.11 EM-NOTE-WAKE]). There is no separate `start` method — the queue methods are `queue-submit | queue-append | queue-status | queue-dry-run` per §6. A Pi-driven dispatch flow that needs to "start processing" submits the queue; no distinct start verb exists or is required.

### 8.2 QM-051 — Advance

When the active group reaches `GroupStatus: complete-success` per §5.1 row 3,
the QueueStore MUST commit its terminal state together with either successor
activation or final completed state in one classified replacement:

1. If a successor exists, persist current terminal plus successor
   `pending → active`, then attempt `queue_group_completed` followed by
   `queue_group_started`.
2. If no successor exists, prebind the QM-005 receipt and persist completed
   canonical state, then delegate all receipt/event/cleanup/release work to
   QM-053.

QM-051 owns state transition only. It MUST NOT independently unlink final
canonical state, release ownership, or treat observation delivery as
authority.

### 8.3 QM-052 — Pause-by-failure

When the active group reaches `GroupStatus: complete-with-failures` per §5.1 row 4, the daemon MUST:

1. Build one detached candidate containing both the group's terminal status
   and `Queue.status: paused-by-failure`, and commit it once via QM-001.
2. After that one authoritative commit, attempt
   `queue_group_completed{queue_id, group_index, final_status: complete-with-failures}`.
3. Then attempt
   `queue_paused{queue_id, group_index, fail_count, reason: "group_failure"}`.

No further dispatch occurs while `status == paused-by-failure`. The daemon remains running; the queue's canonical named file persists with `status: paused-by-failure`. v0.1 recovery is daemon restart followed by a fresh `queue-submit` after the operator addresses the failed beads; v0.2 will add `queue-resume`.

### 8.3a QM-052a — Handler-pause gate orthogonality

Handler-type pause (per [/Users/gb/github/harmonik/specs/handler-pause.md §6 HP-025]) is **orthogonal** to the queue-level pause states (`paused-by-failure`, `paused-by-drain`). A handler pause does NOT transition `Queue.status`; it manifests only as a submission-time validation gate and as a dispatcher-level eligibility check that holds individual items without advancing queue state.

**Submission-time gate.** During `queue-submit` and `queue-append` validation (QM-052a step in the QM-029a order), the daemon MUST consult `HandlerPauseController.IsHandlerPaused(agent_type)` for each item in the request. If any item resolves to a paused handler type, the entire request MUST be rejected with `QueueValidationReason: handler_paused` (JSON-RPC error code `-32018` per §6.11a). The rejection payload MUST include the `agent_type` and the list of bead IDs that would dispatch to the paused handler. See [/Users/gb/github/harmonik/specs/handler-pause.md §6 HP-025, §7 HP-009a].

**Orthogonality.** When `Queue.status` is `paused-by-failure` or `paused-by-drain`, a concurrent handler pause has no additional effect on queue state. The queue remains in its existing pause state; the handler pause persists independently and applies when the queue eventually resumes. No `queue_paused` event is emitted for a handler pause.

### 8.4 QM-053 — Complete

When the last group (highest `group_index`) reaches `complete-success` AND no successor exists, the daemon MUST:

1. Fix exact completed canonical bytes and one QM-005 receipt binding before
   replace-intent durability.
2. Make the completed canonical candidate and replace intent durable/install
   under QM-001.
3. Install the exact receipt and receipt root durably under QM-005.
4. On the uninterrupted normal path, attempt final
   `queue_group_completed` exactly once with the bound
   `completion_receipt_id`. Append failure is diagnostic and cleanup
   continues; restart never retries or synthesizes it.
5. Apply QM-003 queue-ID plus completed-byte-digest CAS, unlink the exact old
   canonical, and make absence queue-directory-durable.
6. Remove the resolved final replace intent and make its absence
   queue-directory-durable.
7. Clear/release QueueStore/refusal ownership.
8. Install the immutable QM-006 completion-release marker.

Every unresolved canonical or intent state continues to own/refuse the name.
Marker installation failure retains the receipt and disables GC but MUST NOT
reacquire or block the released name. The retained receipt answers exact-ID
status; a newer same-name queue is never touched. No separate
`queue_completed` event is emitted.

### 8.5 QM-054 — Pause-by-drain entry

When the daemon enters operator-pause or shutdown-drain per [/Users/gb/github/harmonik/specs/operator-nfr.md §4.7 ON-027] step (1), the queue MUST transition `Queue.status` from `active → paused-by-drain`. The drain pseudocode (which in-flight runs may complete, which are interrupted, observability obligations) is owned by ON-027 and is NOT duplicated here.

On entry to `paused-by-drain` the daemon MUST:

1. Persist the new queue status via QM-001.
2. Emit exactly one `queue_paused{queue_id, group_index, reason: "operator_drain"}` event per [/Users/gb/github/harmonik/specs/event-model.md §8.10]. The `group_index` is the currently-active group's index.

No new items are dispatched while `status == paused-by-drain`. In-flight runs continue per ON-027 step (2).

> INFORMATIVE: The `operator_pause_status{status: pausing|paused}` event that drives this `active → paused-by-drain` transition is produced in production by the operator-nfr pause/resume command verb (per [/Users/gb/github/harmonik/specs/operator-nfr.md §4.3 ON-056/ON-057]). This requirement specifies the consumer side only; the producer adds no change to the consumer semantics here. The same `operator_pause_status` is the single source of pause truth observed by both this queue transition and the execution-model br-ready fallback gate (per [/Users/gb/github/harmonik/specs/execution-model.md §7.4 EM-067]).

### 8.6 QM-055 — Persisted pause survives restart

The canonical named queue written under QM-001 retains
`status: paused-by-failure` or `status: paused-by-drain` across daemon restart.
On QM-002 read, the queue loads with its persisted pause status and remains
paused. v0.1 recovery from a persisted pause is daemon restart + fresh
`queue-submit` after operator action; v0.2 will add `queue-resume` and
`queue-clear`.

### 8.7 QM-056 — `queue_paused.reason` enumeration

The `reason` field on `queue_paused` events is constrained to the enum:

```
ENUM QueuePauseReason: group_failure, operator_drain
```

This enum is co-owned with [/Users/gb/github/harmonik/specs/event-model.md §8.10]; additions require an event-payload schema bump per [/Users/gb/github/harmonik/specs/event-model.md §6.4].

### 8.8 QM-057 — Status method

`queue-status` obeys the §2.10 selector precedence. Exact live queue ID wins
and returns the exact watched-group status/items/counts. When no exact live
queue exists, exact-ID lookup MUST enumerate only exact
`<queue_id>--<receipt_id>.json` receipt names, excluding release markers and
temps. Exactly one unexpired candidate whose filename, content, schema, IDs,
and digest agree may answer completed status. Zero is not found.
Multiple/corrupt/unsupported matching candidates or identity disagreement is
an explicit identity-integrity error; directory-order first match and
same-name fallback are forbidden.

A receipt proves watched success only when its final group index covers the
watched index. The response carries completed/final status, final index,
counts, completion time, and receipt ID. Receipt recovery itself uses only the
intent-bound basename/bytes, never status enumeration. Status MUST NOT mutate
state or emit events.

## 9. Concurrency

### 9.1 QM-060 — Single-writer to the queue object

All queue mutations MUST execute through the single daemon/project QueueStore
transaction owner defined by QM-001. No RPC, workloop, lifecycle, eager,
budget, review, crew, bootstrap, inline, or operator caller may mutate a live
queue pointer or call canonical replace/unlink/archive/receipt/GC persistence
outside QueueStore or its bounded pre-install startup adapter. Each normalized
name has one transaction domain; multi-name operations acquire names in
lexical order. Readers MAY consume immutable snapshots.

### 9.2 QM-061 — Single-orchestrator submission

v0.1 assumes a single orchestrator client per daemon. Multi-orchestrator
submission semantics (two clients racing to enqueue the same normalized name,
queue-ownership ACLs) are out of scope. All submissions nevertheless serialize
through the daemon/project QueueStore transaction owner per QM-060. QM-027
ensures at most one live queue exists **per normalized name**; it does not
establish a project-wide singleton and does not prevent simultaneous live
queues under distinct normalized names.

### 9.3 QM-062 — Composition with `--max-concurrent` (two-level capacity gate)

The queue's parallel-group concept composes with the daemon's existing `--max-concurrent N` **global** capacity gate per [/Users/gb/github/harmonik/specs/execution-model.md §4.3] AND with each queue's **per-queue** worker count (QM-066). With multiple named queues active (NQ-A1), the dispatcher gate is two-level: for a given queue `Q` it dispatches up to

```
min(
  group_pending_count(Q),                       -- eligible items in Q's active group
  Q.workers       - queue_running(Q),           -- Q's per-queue ceiling (QM-066)
  --max-concurrent - global_running             -- the daemon-wide ceiling
)
```

where `queue_running(Q)` is the count of in-flight runs tagged with `Q.name` (`RunRegistry.LenForQueue(Q.name)`) and `global_running` is the count of all in-flight runs across every queue plus any `br ready`-fallback runs (`RunRegistry.Len()`). The two ceilings compose multiplicatively-bounded: a queue can never run more than `Q.workers` items at once, and the sum across all queues can never exceed `--max-concurrent`. The global ceiling always wins — even an oversubscribed queue (`Q.workers > --max-concurrent`, permitted per QM-066) is capped at `--max-concurrent` at runtime.

A single queue with `Q.workers` defaulted to `--max-concurrent` (the QM-066 default) reduces exactly to the pre-named-queues behavior `min(group_pending_count, --max-concurrent - currently_running)`. The queue narrows parallelism (a wave of 8 with `--max-concurrent 2` runs 2 at a time) but never widens it.

Cross-queue arbitration — which active queue is offered the next free global slot — is governed by QM-067.

### 9.4 QM-063 — Persistence ordering with event emission

For any state-changing operation, queue-owned state and required namespace
facts MUST become `committed_durable` before the normal producer attempts the
corresponding observation. Observation construction before intent may reject
the mutation; append failure after durable commit is diagnostic and MUST NOT
roll back state, retain/reacquire refusal, or suppress cleanup/admission.
Restart MUST NOT replay queue observations. Event ordering within one
uninterrupted operation remains as specified in §6, §7, and §8.

### 9.5 QM-065 — Event-emission ordering across operations

Events emitted within a single operation are ordered as specified per-operation (§6.1, §7.3, §8.1, §8.2, §8.3, §8.5). Across operations the daemon's emitter ordering follows the EV-002a per-process monotonicity discipline per [/Users/gb/github/harmonik/specs/event-model.md §6.2 EV-002a]; readers tailing `events.jsonl` see queue events in a total order consistent with the queue's mutation history. The single-writer discipline per QM-060 guarantees that no two queue mutations interleave their event emissions.

### 9.6 QM-064 — No mutation during validation

The validation pipeline (§6) MUST run against an immutable snapshot of the in-memory queue. Any mutation accepted concurrently with a validation pass MUST be sequenced after that pass's snapshot via the QM-060 single-writer discipline. Failed validation MUST NOT leave any partial state, intent log, or event emission behind — per QM-028, validation failures surface only on the JSON-RPC response.

### 9.7 QM-066 — Per-queue worker count

Each queue carries a `workers` field (RECORD `Queue.workers`, §2.1): the maximum number of items that queue may have in-flight simultaneously. It is the per-queue ceiling in the two-level capacity gate (QM-062).

- **Default.** When `workers` is omitted, zero, or negative at submit/load time, the daemon defaults it to the daemon-wide `--max-concurrent`. A queue with no explicit `workers` therefore behaves exactly like the single-queue daemon: bounded only by the global ceiling.
- **Bound.** A `workers` value MUST be a positive integer. It MAY exceed `--max-concurrent` (oversubscription): this is permitted but never effective, because the global ceiling (QM-062) still caps the queue at `--max-concurrent` at runtime. The daemon MUST log an oversubscription warning exactly ONCE, at queue-submit time, when `workers > --max-concurrent`.
- **Scope.** `workers` bounds only that queue's own in-flight tally (`RunRegistry.LenForQueue(name)`); it has no effect on any other queue. The reserved `main` queue is not special-cased — it obeys the same default and bound.

This is mechanism, not policy: `workers` is a static per-queue dispatch width, not a budget, quota, or cost gate (named queues have no special semantics per the §2.1 INFORMATIVE note / N2/N4). Cost governance lives at the credential-isolation layer, not here.

### 9.8 QM-067 — Cross-queue dispatch policy (name-ordered round-robin)

When more than one queue is active and a global slot is free (the QM-062 global ceiling has room), the daemon MUST choose which queue is offered the slot by **name-ordered round-robin** among the *candidate* queues. A queue is a candidate on a given dispatch tick iff:

1. its status is `active` (paused-by-failure / paused-by-drain / completed queues contribute nothing but MUST NOT block siblings),
2. it has an `active` group with at least one eligible item (§5.7), AND
3. its per-queue tally is below its `workers` ceiling (`LenForQueue(name) < workers`, QM-066).

The arbitration is:

- Candidate queue names are sorted lexicographically (the stable total order over the `[a-z0-9-]` name charset, QM-002).
- A **round-robin cursor** — daemon state that persists across dispatch ticks — selects the starting offset into the sorted candidate list (`candidates[cursor mod len(candidates)]`). The chosen queue dispatches its head-eligible item.
- The cursor MUST advance by one on **every** queue selection and MUST NOT be reset to zero each tick. Resetting it would make the lexicographically-first candidate (e.g. `investigate`) win every tick and **starve** a later one (e.g. `main`); advancing it rotates the offset so dispatch is shared fairly. Over a long run no candidate queue starves.

This is plain round-robin — every candidate queue is treated equally. **Weighted fairness** (dispatch shares proportional to `workers`, or priority tiers across queues) is explicitly OUT OF SCOPE for v0.1 and deferred to a later version. The `workers` count gates a queue's *concurrency width* (QM-066); it does NOT weight its *dispatch frequency* under this policy.

## A. Appendices

### A.1 Glossary

- **queue** — one daemon-owned named execution plan, identified by `queue_id`, persisted at `.harmonik/queues/<normalized-name>.json`. (see §2.1)
- **group** — an ordered position within the queue containing a set or sequence of items. A group is either a `wave` or a `stream`. (see §2.3)
- **item** — a single bead reference within a group, carrying its dispatch lifecycle state. (see §2.6)
- **wave** — a `Group` of kind `wave`: a fixed closed set of items dispatched concurrently up to `--max-concurrent`, immutable after submit. (see §2.4)
- **stream** — a `Group` of kind `stream`: an ordered open-ended sequence dispatched head-first as slots open, appendable while `pending` or `active`. (see §2.4)
- **queue_id** — daemon-minted UUIDv7 identifier for one queue submission; never client-supplied; returned from `queue-submit` and carried on every queue-lifecycle event. (see §4.1)
- **group_index** — 0-based dense integer index of a group within the queue; immutable after submit. (see §2.3)
- **paused-by-failure** — queue-level lifecycle state entered when an active group reaches `complete-with-failures`; no further dispatches; survives daemon restart. (see §8.3)
- **paused-by-drain** — queue-level lifecycle state entered when the daemon enters operator-pause / shutdown drain per [operator-nfr.md §4.7 ON-027]; survives daemon restart. (see §8.5)
- **cancelled** — queue-level terminal state set when cancellation begins; the
  canonical named queue remains owned until QM-007 durably archives it,
  removes the linked intents, and releases the name. It is never merely left
  for a later run to overwrite. (see §2.2, hk-ppt32)
- **deferred-for-ledger-dep** — transient `ItemStatus` for an item whose Beads `blocks` edge is open; resolves to `pending` when the blocker closes. (see §2.7, §6.6)
- **completion receipt** — immutable queue-owned exact-ID authority for final success, bound before completed canonical installation. (see QM-005)
- **completion-release marker** — immutable post-cleanup/post-release durable anchor for the receipt's 720-hour retention interval. (see QM-006)
- **durable landmark** — the authoritative completion receipt per QM-033; queue events remain observations.
- **single-writer discipline** — all queue mutations serialize through the daemon/project QueueStore transaction owner per QM-060. (see §9.1)

### A.2 Cross-spec impact summary

| Spec | Section | Nature of impact |
|---|---|---|
| [/Users/gb/github/harmonik/specs/event-model.md] | §8.10 (new) | Owns the six queue-lifecycle event payloads plus the new `queue_item_reconciled` event (row 8.10.7) added in v0.1.1 per QM-002a; this spec cites them by name and reason-enum but does not define payload schemas. |
| [/Users/gb/github/harmonik/specs/event-model.md] | §6.4 row 1 | OPTIONAL `queue_id` / `queue_group_index` fields added to `run_started` / `run_completed` / `run_failed` per QM-011, QM-012. |
| [/Users/gb/github/harmonik/specs/process-lifecycle.md] | §4.4 (new/extended) | Owns the `queue-submit` / `queue-append` / `queue-status` / `queue-dry-run` JSON-RPC method surface and Unix-socket transport. |
| [/Users/gb/github/harmonik/specs/process-lifecycle.md] | §4.1 PL-005 step 8a | Establishes the receipt root and classifies canonical queues, legacy `main`, intents, receipts, and release markers per QM-002 before readiness. |
| [/Users/gb/github/harmonik/specs/operator-nfr.md] | §4.5 ON-018 | Queue envelopes and queue-owned protocol records use explicit schema versions; this spec owns their compatibility semantics. |
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

v0.1.6 — 2026-07-27 — Queue transaction and durability contract
(`queue-transaction-contract`). Reconciles canonical named-queue topology and
legacy-main migration; makes QueueStore the sole immutable
snapshot/generation transaction owner; adds typed persistence results and
universal event-free replace/archive intents; replaces the QM-033 event
landmark with an intent-bound immutable completion receipt; adds flat-root
durability, queue-ID plus completed-byte-digest CAS cleanup, immutable
post-release retention marker v1, trusted-UTC 720-hour retention, receipt-first
GC, exact-ID QueueStatus fallback, linked cancellation handoff, and
whole-capability restart behavior. Queue observations remain normal-path only
and are never replay/recovery authority.

v0.1.5 — 2026-07-12 — QM-002b Class D: paused-by-drain strand reconciliation (hk-bl4d6). One additive amendment:

1. **§3.2b (amended) — QM-002b Class D (new).** Added a fourth mismatch class, `queue_paused_by_drain_item_stranded`: a queue whose own `status` is `paused-by-drain` (abandoned; no auto-resume in v0.1) that still carries a pending/deferred-for-ledger-dep item. Unlike Class A, this correction is keyed on the queue's own status, not the Beads ledger status — the item is advanced to `failed` regardless of whether its bead is open or closed. This releases the EM-065 cross-queue occupancy guard, which otherwise counts the stranded item as live forever, permanently blocking its bead from being queued elsewhere (`bead_already_dispatched`, -32015). Ordering: Class D mutations are collected and persisted alongside Class A (before any event emission, per QM-063); events emitted in A/D → C → B order.

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
