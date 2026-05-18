# queue-model — Change Design

## Current state

No existing `specs/queue-model.md`. This is a new spec. The queue concept is currently conflated with "Beads is the queue" framing in operator-nfr.md ON-015 (line 300), and dispatch input flows through `br ready` per beads-integration.md BI-013 (line 253).

## Target state

A new normative spec at `specs/queue-model.md` defining the daemon's execution-plan data model, persistence discipline, validation contract, and group lifecycle. v0.1 surface: submit + append + dry-run + status. Other Tier-B specs reference it via `[queue-model.md §X QM-NNN]` anchors.

### §1 — Scope and terms

The spec owns: the queue data structure, the group primitives (wave + stream), persistence at `.harmonik/queue.json`, the validation contract on submit/append/dry-run, the group state machine, and the `queue_id` identity discipline.

Out of scope: CLI surface (process-lifecycle owns), event payloads (event-model owns), dispatch-loop integration (execution-model owns), bead-status validation (beads-integration owns).

Term glossary: queue, group, item, wave, stream, queue_id, paused-by-failure, paused-by-drain.

### §2 — Data model

**RECORD Queue** (envelope; one per daemon at a time):

```
queue_id          : UUID         -- daemon-minted UUIDv7 at submit; never client-supplied
schema_version    : Integer      -- MUST equal 1; N-1 readable per [operator-nfr.md §4.5 ON-018]
submitted_at      : Timestamp    -- ISO 8601 with ms
groups            : List<Group>  -- ordered; 1+ entries
status            : QueueStatus  -- {active, paused-by-failure, paused-by-drain, completed}
```

**ENUM QueueStatus**: `active`, `paused-by-failure`, `paused-by-drain`, `completed`.

**RECORD Group**:

```
group_index       : Integer       -- 0-based, dense; immutable after submit
kind              : GroupKind     -- {wave, stream}
status            : GroupStatus   -- {pending, active, complete-success, complete-with-failures}
items             : List<Item>    -- waves: immutable after submit; streams: append-only
created_at        : Timestamp     -- ISO 8601 with ms
started_at        : Timestamp | None  -- set when group transitions pending → active
completed_at      : Timestamp | None  -- set on terminal status
```

**ENUM GroupKind**: `wave`, `stream`.

**ENUM GroupStatus**: `pending`, `active`, `complete-success`, `complete-with-failures`.

**RECORD Item**:

```
bead_id           : BeadID         -- ledger reference; immutable
status            : ItemStatus     -- {pending, dispatched, completed, failed, deferred-for-ledger-dep}
run_id            : UUID | None    -- populated when status transitions to dispatched
appended_at       : Timestamp | None  -- set when appended post-submit (streams only)
```

**ENUM ItemStatus**: `pending`, `dispatched`, `completed`, `failed`, `deferred-for-ledger-dep`.

(`deferred-for-ledger-dep` is a transient state: item is at head of dispatch eligibility but a ledger `blocks` edge is open. Becomes `pending` again when the blocker closes; the dispatcher re-evaluates.)

### §3 — Persistence

`.harmonik/queue.json` is the on-disk authoritative representation. The in-memory queue is the runtime authority; the file is the crash-recovery sync.

**Write discipline (QM-001):** every queue mutation MUST persist via WM-026 atomic write (temp-file + fsync(temp) + rename + fsync(parent_dir)) per `specs/workspace-model.md:557-562`. No exceptions for v0.1; write coalescing deferred.

**Read on startup (QM-002):** the daemon MUST read `.harmonik/queue.json` at PL-005 step 8a (alongside `daemon.state` and `daemon.upgrading`). If the file exists and parses, the queue is loaded with its persisted status. If the file is missing, the daemon starts with no queue (waiting for `queue-submit` over the socket). Corrupt file is treated as absent with a structured-log warning, matching PL-005 step 8a precedent.

**Removal on completion (QM-003):** when the queue reaches `status=completed`, the daemon MUST unlink `.harmonik/queue.json` and fsync(parent_dir). The in-memory queue is cleared. Next submission re-creates the file.

### §4 — Identity

**QM-010:** `queue_id` is a UUIDv7 minted by the daemon at `queue-submit` accept. Never client-supplied. Returned to the CLI in the JSON-RPC response. Carried on every queue-lifecycle event per event-model.md §8.10.

**QM-011:** `queue_id` is added as an OPTIONAL field on `run_started` / `run_completed` / `run_failed` event payloads. Populated when the run was dispatched from a queued submission; absent for direct/legacy dispatch. Cross-ref event-model.md §6.4 row 1 (additive optional field, non-breaking).

**QM-012:** `queue_group_index` similarly OPTIONAL on `run_*` events. Populated alongside `queue_id`.

### §5 — Group lifecycle (state machine)

| From | Event | Guard | To | Emits |
|---|---|---|---|---|
| pending | predecessor group reaches `complete-success` | — | active | `queue_group_started` |
| pending | (queue submitted, group_index=0 only) | — | active | `queue_group_started` |
| active | every item in group is terminal AND zero failures | — | complete-success | `queue_group_completed` |
| active | every item in group is terminal AND ≥1 failure | — | complete-with-failures | `queue_group_completed`, then `queue_paused{reason:"group_failure"}` |
| complete-success | — | — | — | (terminal; group never re-enters) |
| complete-with-failures | — | — | — | (terminal in v0.1; v0.2 will add `queue.resume` to advance past) |

`pending` → `active` is mediated by the queue's overall status: if queue status is `paused-by-failure` or `paused-by-drain`, no group advances even when its predecessor completes.

> INFORMATIVE: The §7.1 per-run state machine in execution-model.md is layered underneath the per-group state machine. An item's `status: dispatched` corresponds to its run reaching `running` per EM §7.1. An item's terminal (`completed` / `failed`) corresponds to the run's terminal event.

### §6 — Validation contract

Every `queue-submit`, `queue-append`, and `queue-dry-run` request MUST pass the following checks, in order. Failures return a typed JSON-RPC error and do NOT mutate state.

**QM-020 (existence):** every `bead_id` in the request MUST resolve via `br show <id>`. Missing → `queue_validation_failed{"reason":"bead_not_found","bead_id":"<id>"}`.

**QM-021 (status):** every referenced bead MUST have `status ∈ {open}`. Closed or blocked beads → `queue_validation_failed{"reason":"bead_not_open","bead_id":"<id>","actual_status":"<status>"}`.

**QM-022 (no-double-dispatch):** no `bead_id` in the request may already be in `in_progress` from a different queue or from a non-queued dispatch. → `queue_validation_failed{"reason":"bead_already_dispatched","bead_id":"<id>"}`.

**QM-023 (no-cross-group-duplicate):** within a single submission, no `bead_id` MAY appear in more than one group, and no `bead_id` MAY appear more than once within a single group. → `queue_validation_failed{"reason":"duplicate_bead_id","bead_id":"<id>"}`.

**QM-024 (append target validity):** `queue-append` requires `group_index` to reference an existing group whose `kind=stream` AND whose `status ∈ {pending, active}`. → `queue_validation_failed{"reason":"append_target_invalid","group_index":N,"actual_kind":"<kind>","actual_status":"<status>"}`.

**QM-025 (parallelism-narrowed informational):** if a request's group has two `bead_id`s X and Y where the ledger declares `Y blocks-on X` (or vice versa), validation does NOT fail. The daemon MUST emit `queue_item_deferred_for_ledger_dep{bead_id:Y,blocker:X}` at submit acceptance time (informational; not at dispatch time). Cross-ref beads-integration §4.3 for the `blocks` edge semantics.

**QM-026 (queue.json size):** the persisted queue MUST not exceed 1 MiB after the proposed mutation. This bounds memory and atomic-write cost. → `queue_validation_failed{"reason":"queue_too_large","proposed_bytes":N,"limit":1048576}`.

### §7 — Append semantics

`queue-append` targets a single stream group by `group_index`. Permitted iff the group's status is `pending` or `active`. Appended items land at the tail of the stream's `items` list. The daemon emits `queue_appended{queue_id, group_index, appended_bead_ids}`.

Append to a `wave` group: rejected per QM-024.
Append to a completed group: rejected per QM-024.
Append when queue status is `paused-by-failure` or `paused-by-drain`: rejected with `queue_validation_failed{"reason":"queue_not_advancing"}`.

### §8 — Queue lifecycle

**Submit:** validates → mints `queue_id` → persists → group 0 transitions `pending → active` → emits `queue_submitted` then `queue_group_started{group_index:0}`. If the queue already has an active or non-completed queue, `queue-submit` is rejected with `queue_validation_failed{"reason":"queue_already_active"}`.

**Advance:** when the active group reaches `complete-success`, the daemon advances by transitioning the next pending group to `active`, emitting `queue_group_completed` for the prior group and `queue_group_started` for the next.

**Pause-by-failure:** when the active group reaches `complete-with-failures`, the daemon transitions queue status to `paused-by-failure`, emits `queue_group_completed{final_status:complete-with-failures}` then `queue_paused{queue_id, group_index, fail_count, reason:"group_failure"}`. No further dispatches. The daemon remains running; the queue persists in `.harmonik/queue.json`. v0.1 recovery path is daemon restart + re-submit (or wait for v0.2's `queue.resume`).

**Complete:** when the last group reaches `complete-success` AND no successor exists, queue status transitions to `completed`. The final `queue_group_completed` event (F-class) is the durable landmark; no separate `queue_completed` event is emitted. `.harmonik/queue.json` is unlinked per QM-003. The `queue_completed` state is observable via `queue-status` (returns `queue_not_active` once the file is gone, matching the no-queue-loaded state).

**Pause-by-drain:** when the daemon enters operator-pause / shutdown drain per operator-nfr.md §4.7 ON-027 step (1), queue status transitions `active → paused-by-drain`. No new dispatches are issued; in-flight runs continue to their next checkpoint per ON-027 step (2). The daemon emits `queue_paused{queue_id, group_index, reason:"operator_drain"}` once at the transition (note: `queue_paused` carries a `reason ∈ {group_failure, operator_drain}` field per event-model.md §8.10). The persisted `.harmonik/queue.json` retains `status: paused-by-drain` so the queue survives the drain. On clean restart, the queue loads with `paused-by-drain` and remains so until v0.2's `queue.resume` ships; v0.1 recovery is `queue.clear` (post-v0.1) or daemon restart + fresh `queue.submit`.

### §9 — Concurrency composition

The queue's parallel-groups concept sits on top of `--max-concurrent`. The dispatcher dispatches up to `min(group_pending_count, max_concurrent - currently_running)`. A wave of 8 with `--max-concurrent 2` runs 2 at a time; a stream similarly. The capacity gate (workloop.go:382-389) is unchanged.

## Rationale

- **D1 (waves + streams):** §2's `GroupKind` + §7 append-permission rule split the two primitives cleanly. Waves' immutability lets the dispatcher know its work upfront; streams' append-only mutation lets the orchestrator extend in flight.
- **D2 (all-terminal group advance):** §5's transition table only advances on `complete-success`; failures route to `complete-with-failures` then pause.
- **D3 (failure pauses, no auto-retry):** §8's pause-by-failure path. v0.1 ships no `queue.resume`; recovery is restart-based.
- **D4 (honor ledger; surface what happens):** QM-025 + the `deferred-for-ledger-dep` item state. Validation never rejects on a ledger-blocks conflict; it surfaces.
- **D5 (Unix socket):** referenced via process-lifecycle, no socket spec here.
- **D6 (v0.1 minimal):** §5 explicitly notes `complete-with-failures → ???` is terminal in v0.1; §7 rejects v0.2-only operations.

## Requirements traceability

| 02-components §1 requirement | QM-* |
|---|---|
| Queue identity (one per daemon, mutable in place) | QM-010, §8 submit |
| Group primitives (wave + stream) | §2 ENUM GroupKind, §7 |
| Group lifecycle (transitions) | §5 table |
| Group-advance rule (all-terminal) | §5 row 3 |
| Stream append semantics | §7 |
| Validation rules | QM-020..026 |
| Persistence (in-memory + queue.json + recovery) | QM-001, QM-002, QM-003 |
| Serialization format (JSON + schema_version) | §2 RECORD Queue.schema_version |
| Relationship to ON-015 reframing | §1 scope + §8 cross-ref to operator-nfr |

## Open for spec-draft pass (not blocking design)

- Exact JSON-RPC error-code numbers for QM-020..026 (allocate from -32010..-32019; queue-model.md claims the block).
- Concrete `queue.json` example showing all RECORD fields populated.
- Schema-version bump policy when QM rules tighten (likely migration-release per event-model.md §6.4).
