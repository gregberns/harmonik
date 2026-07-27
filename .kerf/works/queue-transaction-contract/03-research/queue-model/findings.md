# Queue-model research findings

## Scope and method

This pass reconciles CQ-00 evidence, the approved CQ-02 card, current normative
specifications, and production symbols. It answers topology, ownership,
replacement/archive recovery, authoritative completion, cleanup, exact-ID
status, and implementation decomposition. Event delivery is considered only at
the queue/event ownership boundary.

Primary evidence:

- `specs/queue-model.md` QM-001, QM-002, QM-002c, QM-003, QM-033,
  QM-050–QM-053, QM-057, QM-058, and QM-060–QM-064.
- `internal/queue/persistence.go` `Persist`, `Load`, `CompleteAndUnlink`,
  `ArchiveFailedQueue`, `Unlink`, `MigrateFromLegacy`, and `fsyncDir`.
- `internal/queue/rpc.go` `HandleQueueSubmit`, `HandleQueueAppend`,
  `HandleQueueStatus`, `findQueueByID`, and
  `HandlerAdapter.HandleQueueCancel`.
- `internal/queuewiring/store.go` `QueueStore`, `LockedQueueStore`, and
  `LockForMutation`.
- CQ-00’s writer and crash-cut inventory.

## C1 — Topology and compatibility

### Current evidence

`queuePath` writes `.harmonik/queues/<name>.json`; `legacyQueuePath` reads
`.harmonik/queue.json`; `MigrateFromLegacy` targets `main.json`.
`EnumerateQueueNames` treats JSON files in the queue directory as live queues.
The normative specification still contains singleton-era wording in completion
and startup sections.

### Finding

There must be one live-write topology:

| Path | Role |
|---|---|
| `.harmonik/queues/<normalized-name>.json` | canonical live queue |
| `.harmonik/queue.json` | migration-only legacy `main` input |
| `<name>.replace-intent` | ordinary replacement comparison evidence |
| `<name>.archive-intent` | archive/migration handoff evidence |
| `.completion-receipts/<queue_id>--<receipt_id>.json` | retained final-success receipt |

Canonical queue envelope schema remains version 1. Protocol metadata is not a
canonical queue and uses independently capability-gated schemas. The receipt
root must be excluded from live-queue enumeration.

Legacy-only migrates to `main`; canonical-only loads; equivalent dual copies
complete legacy removal; divergent or invalid copies fail closed without
overwriting either. Neither means no queue.

## C2 — Runtime transaction owner

### Current evidence

`QueueStore.LockForMutation` provides a global lock and cloned reads, but
production paths still call `Persist`, archive, and unlink directly. Some
paths mutate a live queue before persistence; append recently added clone-first
behavior locally, proving the pattern but not centralizing it.

### Finding

The daemon/project QueueStore is the sole runtime owner. A bounded startup
adapter in the same process may resolve migration/intents before QueueStore
installation; it is not a second writer.

Each transaction operates on a normalized name:

1. acquire the name domain;
2. snapshot stored state and volatile generation;
3. validate request and expected generation;
4. deep-clone or create a private candidate;
5. mutate and validate the candidate;
6. marshal exact canonical bytes and any normal-path derived payload;
7. invoke the persistence classifier;
8. install only a durable selected candidate/absence;
9. advance the process-local generation once;
10. Wake and attempt applicable observations;
11. release or retain refusal according to cleanup state.

Generation is a stale-snapshot guard only. It is never persisted or consulted
after restart. Multi-name operations sort names lexically and return
independent per-name outcomes; no cross-file rollback is implied.

## C3 — Persistence result taxonomy

The current `ErrPersistFailed` cannot distinguish failure before rename from
failure after a successful rename. The contract needs:

| Result | Disk claim | Memory action | Retry/recovery |
|---|---|---|---|
| `rejected` | no namespace I/O | retain prior generation | correct input or resnapshot |
| `not_committed` | intended namespace mutation definitely absent | retain prior; remove only unreferenced temp | bounded exact retry or refusal |
| `committed_durable` | intended namespace state parent-synced | install selected state and advance | continue remaining protocol only |
| `commit_indeterminate` | namespace mutated but durability unresolved | retain prior install; quarantine/refuse name | exact reload/compare/sync only |

Validation, conflict, stale generation, and size refusal are `rejected`.
Marshal, candidate-temp create/write/fsync/close, and derived-event
construction before durable intent are `not_committed`. Once an intent is
durable, classification uses intent/canonical/temp facts; later failures cannot
be collapsed into `not_committed`.

A directory close error after successful directory fsync is diagnostic and
does not revoke `committed_durable`.

## C4 — Replacement intent

### Required record

```yaml
schema_version: 1
transaction_id: <canonical UUIDv7>
operation_kind: <enumerated operation>
normalized_name: <name>
queue_id: <canonical UUIDv7>
canonical_basename: <name>.json
prior:
  state: absent | present
  sha256: <digest or absent>
candidate:
  sha256: <digest>
  temp_basename: <unique already-durable sibling temp>
wake_required: <boolean>
completion_receipt: null | <final-success binding>
```

For ordinary operations `completion_receipt` is null. The record contains no
event ID, effect key, replay cursor, RPC response, event payload list,
delivery acknowledgement, or pending-delivery state.

The universal replacement order is:

```text
snapshot/validate/clone/mutate/marshal
-> candidate temp create/write/fsync/close
-> replace-intent temp create/write/fsync/close/rename/queue-dir fsync
-> canonical rename/queue-dir fsync
-> install generation
-> Wake/normal-path observations
-> intent removal/queue-dir fsync when its queue work is resolved
```

Exact prior/candidate bytes equal collapses before temp or intent creation.

Recovery compares exact intent-bound facts:

- candidate canonical match: finish parent sync, promote/install candidate;
- prior canonical match or bound prior absence: classify replacement
  uncommitted and clean only intent-selected temp;
- neither/extraneous/corrupt state: quarantine and refuse;
- candidate temp match with prior canonical: retry the exact selected rename;
- any digest/identity/schema mismatch: fail closed.

## C5 — Ordinary mutation composition

Every canonical replacement consumes C2–C4: create, submit, append,
reservation, Run-ID patch, activation/advance, pause/resume, eager refill,
budget/review charge, maintenance/reconciliation, startup, inline/bootstrap,
crew-placeholder creation, and cancelled-state replacement.

Submit and group-zero activation must be one candidate. It is invalid to expose
a newly submitted active queue whose first group is still pending. A successful
install may Wake and attempt `queue_submitted` then
`queue_group_started{group_index:0}`. Their failure cannot roll back queue
state or create event-delivery recovery state.

Non-final group transition is likewise one candidate: terminal current group
plus successor activation, or terminal failure plus paused state. QM-051 owns
state transition. QM-053 alone owns final completion receipt and cleanup.

## C6 — Completion receipt identity and content

### Why a separate receipt is required

Current QM-003 removes the canonical queue after completion while QM-033 calls
the final event the durable landmark. EV-021/EV-022 prohibit observational
JSONL from reconstructing state. An event can be absent after a crash or append
failure, and same-name reuse makes name-only history ambiguous. Queue-owned
state therefore needs a retained, immutable completion fact.

### Intent-bound identity

For final `complete-success`, the owner preallocates one UUIDv7 `receipt_id`
before completed canonical installation. The durable replace intent adds:

```yaml
completion_receipt:
  receipt_id: <canonical lowercase UUIDv7>
  basename: <queue_id>--<receipt_id>.json
  schema_version: 1
  canonical_bytes_base64: <exact bytes>
  sha256: <digest of exact receipt bytes>
```

The bytes are constructed before intent durability. Recovery must use the
bound ID, basename, schema, bytes, and digest. It must not mint another ID,
rebuild timestamps/content, or select a receipt by scanning the directory.

### Receipt v1

The canonical receipt bytes bind:

```yaml
schema_version: 1
queue_id: <canonical UUIDv7>
receipt_id: <canonical UUIDv7>
transaction_id: <canonical UUIDv7>
normalized_name: <name>
final_group_index: <nonnegative integer>
final_status: complete-success
success_count: <nonnegative integer>
fail_count: 0
completed_at: <canonical timestamp>
completed_queue_sha256: <digest of exact completed canonical bytes>
```

The composite `(queue_id, receipt_id)` is authoritative. The basename repeats
both identities and must agree with content. A receipt never contains event
identity, delivery, acknowledgement, replay, or response state.

## C7 — Flat receipt root and crash cuts

Path:

```text
.harmonik/queues/.completion-receipts/<queue_id>--<receipt_id>.json
```

There is no per-queue directory.

### Root establishment

Before readiness or intent recovery:

1. `mkdir(.completion-receipts)`;
2. if EEXIST, open and verify the expected directory type;
3. after first creation, open/fsync/close `.harmonik/queues`;
4. ambiguous mkdir/parent-sync outcome requires reload/reconciliation.

Unsupported type, permissions, or capability fails startup closed.

### Receipt installation

1. create a unique temp in the already-durable root;
2. write exact intent-bound bytes, fsync, close;
3. rename without replacing a different target;
4. if target exists, accept only exact canonical bytes/digest/identity;
5. open/fsync/close the receipt root.

Receipt durability is not established before step 5. A close failure after a
successful root fsync is diagnostic. A rename/root-sync ambiguity is resolved
by exact target/temp reload; no new receipt ID is allocated.

## C8 — QM-053 completion ordering and CAS cleanup

Required final-success order:

```text
final item/group transition candidate
-> completed canonical temp durable
-> final replace intent with receipt binding durable
-> completed canonical installed and queue-dir durable
-> receipt installed and receipt-root durable
-> optional final queue_group_completed attempt
-> CAS-check canonical queue ID + exact completed digest
-> canonical unlink + queue-dir fsync
-> QueueStore clear/generation advance/ownership release
-> intent removal + queue-dir fsync
```

The implementation may order intent cleanup relative to memory release only if
an unresolved intent continues to own/refuse the name. Success cannot expose a
free name with unclassified canonical or intent state.

Cleanup cases:

| Canonical state | Action |
|---|---|
| exact queue ID and completed digest | unlink, sync queue directory |
| absent | idempotent cleanup; sync/reconcile as required |
| same name, different queue ID | newer queue; do not touch |
| same ID, different digest/corrupt | quarantine/refuse; no unlink |

Receipt durability is never rolled back by event failure. Restart completes
receipt-governed cleanup but does not emit the final observation.

## C9 — Retention and crash-safe GC

A receipt is ineligible for GC until canonical cleanup directory durability
and QueueStore ownership release are resolved. It is then retained for at
least 30 days.

GC:

1. revalidate eligibility and identity;
2. unlink exact receipt path;
3. treat absence as idempotent success;
4. open/fsync/close `.completion-receipts`;
5. on unlink or root-sync ambiguity, reload the directory before retry.

GC never touches a canonical queue, never removes a directory, and never
blocks queue admission or readiness.

## C10 — QueueStatus exact-ID authority

Selector precedence is:

1. nonempty `name`: select by name and ignore `queue_id`;
2. empty `name`, nonempty `queue_id`: select by exact ID;
3. both absent: select `main`.

Exact-ID selection first matches a live queue with that exact ID. Otherwise it
performs a read-only enumeration or equivalent indexed lookup of the flat
receipt root using the queue-ID filename prefix. The result is accepted only
when **exactly one** unexpired candidate has a filename and canonical content
that agree on queue ID, receipt ID, schema version, and canonical-byte digest.
Zero candidates is not found. Multiple valid candidates, any matching-prefix
corrupt or unsupported candidate, or any filename/content/digest disagreement
is an explicit identity-integrity error. Selection is never directory-order
first match and never falls through to a live queue sharing the old name.

This read-only status lookup does not weaken receipt recovery: recovery writes
only the one intent-bound basename and exact bytes and never scan-selects a
receipt.

Live status must expose the exact watched group’s durable status, item
outcomes, and counts. Receipt-backed status returns:

- `status=completed`;
- `final_status=complete-success`;
- final group index and counts;
- `completed_at`;
- `completion_receipt_id`.

Not found after an accepted queue ID, corrupt/unsupported/identity-inconsistent
receipt, invalid receipt group range, or transport failure is an error, not an
event-log fallback.

## C11 — Cancellation and archive handoff

Operator cancellation:

1. resolve the live QueueStore-owned queue;
2. transactionally install `cancelled` through the replace intent;
3. durably create an archive intent binding queue ID, exact canonical digest,
   and selected non-overwriting archive basename;
4. rename canonical to that destination and sync the queue directory;
5. finish applicable legacy cleanup and its parent sync;
6. remove archive intent and sync;
7. clear refusal/release ownership;
8. attempt typed operator audit;
9. return RPC success.

An archive failure retains ownership/refusal. Same-name submission is illegal
until exact archive and intent cleanup resolve. Cancellation creates no
completion receipt. Graceful shutdown uses the durable state/archive protocol
but does not emit the operator audit. Daemon-down CLI performs no write.

## C12 — Migration

Migration must preserve one valid copy:

| State | Decision |
|---|---|
| valid legacy only | copy exact bytes to durable `main`, then remove legacy durably |
| valid destination only | load destination; remove equivalent stale legacy if present |
| both valid/equal | retain destination, durably remove legacy |
| both valid/divergent | preserve both and refuse |
| invalid legacy, no destination | quarantine/refuse; never rewrite |
| invalid destination | quarantine/refuse; never overwrite from legacy |
| neither | no queue |

Every destination rename and each legacy unlink has its own parent-directory
durability classifier. CQ-MIG-01 implements these selected rules but does not
choose them.

## C13 — Implementation decomposition

### `CQ-02I`

Owns QueueStore transaction API, immutable snapshots/generations, result
taxonomy, replace/archive-intent schemas and recovery classifiers, and ordinary
replacement primitives. It owns no completion receipt or global event-log
change.

### `CQ-RECEIPT`

Owns receipt schema/store, final-intent binding, receipt-root establishment,
create/read/CAS cleanup, retention/GC, exact-ID live/receipt status, QM-053
cleanup, conditional completion payload field, and focused crash/restart
tests. It owns no event writer, segmentation, replay cursor, `ScanAfter`, or
unrelated consumer.

### `CQ-RUN-WAIT`

Exclusively owns `cmd/harmonik/run_via_daemon.go`
`viaWatchGroupCompletion` and focused tests. Both fresh-submit and shared-append
branches query exact queue ID after setup and on every heartbeat. They use live
watched-group status or the final retained receipt, never JSONL or name
fallback. Existing own-bead attribution remains when all run observations are
known; durable group/receipt outcome fills gaps.

### Required spine

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT -> JR-03
```

`CQ-01` and `CQ-RECEIPT` both edit `internal/queue/rpc.go`, so the explicit
edge serializes their same-file work. They may not run concurrently.
`CQ-RECEIPT -> CQ-04 -> JR-03` is a second mandatory branch; startup recovery
and CQ-RUN-WAIT must both land before live receipt-producing terminal calls.
No card may enable crash-optional group observation before `CQ-RUN-WAIT`.
`CQ-RECEIPT` precedes every QM-053 caller. Non-completion callers and CQ-MIG
retain their CQ-02I prerequisites.

## Patterns preserved

- Exact-byte canonical comparison and no overwrite on conflict.
- Candidate temp before intent and canonical namespace mutation.
- Immutable mutation and generation rejection.
- QueueStore as sole owner.
- Wake only after durable install.
- Archive intent for cancellation/migration handoff.
- Beads and Run ownership boundaries.
- Existing event bus as observation only.

## Active risks for independent review

1. Ensure receipt bytes can be constructed before intent durability without
   depending on a post-install clock read.
2. Ensure intent cleanup and ownership release cannot expose a free name with
   unresolved protocol state.
3. Ensure QueueStatus can distinguish corrupt receipt from true not-found.
4. Ensure non-final append waiters use live watched-group state and do not
   require a final receipt.
5. Ensure no active artifact retains effect-key, acknowledged-prefix,
   segmentation, or cursor-migration requirements.

## Decision

Adopt the event-free replace/archive transaction substrate and queue-owned
completion receipt above. The receipt is the sole durable final-success
landmark. JSONL remains observational; event delivery does not participate in
restart classification, cleanup, retention, or admission.
