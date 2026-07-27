# Queue transaction and durability contract

## Summary

The queue currently has atomic-write mechanics but no single transaction
contract spanning immutable mutation, namespace ambiguity, restart recovery,
completion cleanup, and every production writer. Callers therefore combine
live-pointer mutation, persistence, events, unlink, archive, and memory release
in different orders. A failed fix in one path can contradict another.

This work settles that contract across four normative specifications:

- `specs/queue-model.md` owns queue topology, replacement/archive intents,
  transaction results, the queue-owned completion receipt, cleanup,
  retention/GC, and exact-ID status lookup.
- `specs/process-lifecycle.md` owns queue capability checks during startup,
  upgrade, downgrade, rollback, and daemon-only cancellation transport.
- `specs/event-model.md` owns the shape of the derived completion observation
  and the typed operator-cancellation audit.
- `specs/execution-model.md` receives one narrow EM-015f clarification:
  completion emission is a normal-path producer obligation, not a
  crash-surviving state landmark.

Queue-owned canonical files, intents, and receipts are authoritative. JSONL is
observational and never decides commit, cleanup, recovery, or admission.

## Goals

1. Reconcile the legacy singleton with canonical named queues.
2. Make the daemon/project QueueStore the sole mutation owner.
3. Require detached snapshot, validation, clone, mutation, persistence, and
   installation ordering with process-local generation rejection.
4. Distinguish `rejected`, `not_committed`, `committed_durable`, and
   `commit_indeterminate`, including exact memory and restart actions.
5. Put an exact, durable replace intent in front of every ordinary canonical
   replacement without turning it into an event outbox.
6. Put an exact, durable archive intent in front of cancellation/archive and
   legacy-removal namespace changes.
7. Make final successful completion authoritative through one immutable
   receipt under a flat, queue-owned receipt namespace.
8. Make canonical cleanup CAS-safe across restart and same-name reuse.
9. Preserve the final group event as a derived, normal-path-only observation
   correlated to the receipt when one exists.
10. Give `harmonik run` an authoritative exact-queue-ID waiter fallback so a
    missing observation cannot hang either submit or append.
11. Produce non-overlapping implementation leases and a completion spine that
    prevents optional event delivery from landing before the waiter fallback.

## Non-goals

- No Run schema, workflow, Bead terminal transition, or claim semantics change.
- No new canonical queue JSON fields or queue/group status values.
- No generic repository-wide durability or tombstone abstraction.
- No daemon-down queue writer or local cancellation fallback.
- No multi-process queue writer.
- No effect keys, event outbox, persistent delivery acknowledgement, event ID
  binding, replay instruction, or RPC-response replay.
- No segmented event storage, ReplayCursorV2, `ScanAfter` migration,
  event-reader migration, full-log lookup, truncation, repair CLI,
  cross-process event lock, or writer census.
- No change to EV-021/EV-022, existing JSONL framing, EventID cursors,
  torn-tail behavior, or consumers.
- No amendment to the separate `durable-state-contract` Kerf work.

## Scope

### Normative specifications changed

| Specification | Required delta |
|---|---|
| `queue-model.md` | topology, owner, transaction/result contract, replace/archive intents, completion receipt, QM-053 cleanup, exact-ID status, implementation composition |
| `process-lifecycle.md` | startup inventory and receipt-root readiness, intent/receipt capability negotiation, downgrade refusal, daemon-down cancel exit 17 |
| `event-model.md` | optional final-only `completion_receipt_id`; typed best-effort `queue_cancelled_operator` |
| `execution-model.md` | EM-015f normal-path-versus-crash-delivery clarification only |

### Normative boundaries read but not changed

- `specs/beads-integration.md`: Beads owns terminal Bead transitions.
- `specs/workspace-model.md`: workspace persistence remains independent.
- Event-model EV-021/EV-022 and every existing replay/log mechanic.
- All execution-model clauses other than EM-015f and required document
  version/history bookkeeping.

### Production evidence

- `internal/queue/persistence.go` currently provides `Persist`, `Load`,
  `CompleteAndUnlink`, `ArchiveFailedQueue`, `Unlink`, and
  `MigrateFromLegacy`, but collapses important post-rename failures into
  `ErrPersistFailed`.
- `internal/queuewiring/store.go` `QueueStore` and `LockedQueueStore` expose
  the natural in-process ownership boundary, but callers still persist outside
  a complete transaction primitive.
- `internal/queue/rpc.go` `HandleQueueStatus` selects by name, ID, or `main`
  from live canonical files only; `HandlerAdapter.HandleQueueCancel` archives
  without restart-visible receipt or intent composition.
- `cmd/harmonik/run_via_daemon.go` `viaWatchGroupCompletion` waits on
  `queue_group_completed`/`queue_paused` and heartbeats, so a healthy stream
  can wait forever when an observation is absent.
- `internal/queue/state.go` constructs group completion events during state
  transition, while `internal/daemon/workloop.go` composes terminal mutation,
  persistence, release, and cleanup.
- `specs/event-model.md` EV-021 and EV-022 already prohibit JSONL from being
  authoritative state.

## Constraints

### Queue transaction authority

- The stored/live queue is immutable transaction input.
- Validation and stale-generation rejection happen before namespace I/O and
  return `rejected`.
- The owner deep-clones, mutates, validates, marshals, and fixes any
  normal-path event payload before durable intent creation.
- Candidate temp write/fsync/close succeeds before the replace intent exists.
- Every ordinary canonical replacement durably installs one replace intent
  before canonical namespace mutation.
- Generations are volatile guards, never persisted restart identity.
- Wake is repeatable only after a durable canonical install.

### Replace intent

The intent binds transaction ID, operation, normalized name, queue ID,
canonical path, exact prior/candidate digests, already-durable candidate temp
basename, and Wake requirement. It contains no event ID, effect key, cursor,
response, payload-delivery state, or acknowledgement.

For final `complete-success` only, the same intent additionally binds the one
preallocated receipt ID, exact flat receipt basename, receipt schema version,
canonical receipt bytes, and their SHA-256 digest. Recovery reuses those exact
facts; it never mints, reconstructs, or scan-selects a receipt.

### Completion receipt

The authoritative landmark is:

```text
.harmonik/queues/.completion-receipts/<queue_id>--<receipt_id>.json
```

Both identifiers are canonical lowercase UUID strings. Receipt schema v1
canonically binds the transaction ID, normalized name, final group index and
status, success/failure counts, completion timestamp, and SHA-256 digest of
the exact completed canonical bytes.

The receipt root is startup-owned capability infrastructure. First creation
requires `mkdir`, EEXIST type verification, and `.harmonik/queues` parent
fsync. Receipt installation requires unique temp create/write/fsync/close,
no-replace or exact-byte-idempotent rename, and receipt-root fsync. Receipt
durability precedes canonical unlink.

Cleanup unlinks only when canonical queue ID and exact completed digest match
the receipt. Absence is idempotent; a newer same-name queue is untouched.
Receipts survive unlink and same-name reuse, remain until cleanup durability
and ownership release resolve, then remain at least 30 days. GC is crash-safe,
root-synced, and never blocks admission.

### Derived events

`queue_group_completed` is not completion authority. The final
`complete-success` normal path attempts it once after receipt durability and
before QM-053 returns. Only that observation contains
`completion_receipt_id`; non-final successes and every
`complete-with-failures` observation omit it. Failure is diagnostic and never
gates unlink, clear, readiness, GC, or resubmission. Restart never retries or
synthesizes it.

`queue_cancelled_operator` is a separate typed Class O audit. The live daemon
attempts it after durable archive/legacy cleanup, intent removal, and ownership
release but before returning RPC success. Failure cannot change cancellation
success. Graceful shutdown does not emit it.

## Success criteria

- Every CQ-00 cut maps to one result, memory action, restart action, and owner.
- Every ordinary replacement uses the event-free replace intent.
- Final successful completion creates exactly one intent-bound durable receipt
  before CAS-safe canonical unlink.
- Startup can classify receipt-root, receipt, canonical, temp, replace-intent,
  archive-intent, migration, and cleanup cuts without JSONL.
- Exact-ID QueueStatus first returns an exact live queue and otherwise an
  unexpired exact-ID receipt, never a newer same-name queue.
- Submit and append waiters terminate correctly when group/pause observations
  are absent.
- The completion DAG is
  `CQ-02I → CQ-01 → CQ-RECEIPT → CQ-RUN-WAIT → JR-03`.
  `CQ-01` must finish before `CQ-RECEIPT` because both edit
  `internal/queue/rpc.go`; their same-file work is serialized, not parallel.
  `CQ-RECEIPT → CQ-04 → JR-03` is an additional required startup-recovery
  branch, so live receipt-producing terminal calls cannot precede recovery.
- `CQ-RECEIPT` precedes every caller capable of executing QM-053.
- Four full-file drafts are later produced, with execution-model differences
  confined to version/date, EM-015f, and one qualifying history row.
- Historical review artifacts remain byte-identical and new reviews use new
  round-specific files.

## Preliminary component map

| Component | Question settled |
|---|---|
| Queue transaction owner | Who may mutate, persist, install, Wake, and release? |
| Replace intent | How is an ordinary replacement classified after restart? |
| Archive/migration intent | How are rename/unlink handoffs completed safely? |
| Completion receipt | What durable queue-owned fact proves final success? |
| QM-053 cleanup | How is canonical absence made durable without touching reuse? |
| Process capability | When may startup/upgrade resolve queue-owned records? |
| Derived completion event | What is attempted once, and what does its receipt ID mean? |
| Operator audit | What best-effort cancellation fact is emitted after release? |
| Exact-ID waiter | How do submit/append complete when observations are missing? |
