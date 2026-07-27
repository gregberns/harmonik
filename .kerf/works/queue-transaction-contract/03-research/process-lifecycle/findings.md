# Process-lifecycle research findings

## Scope

Process-lifecycle owns transport, startup ordering, readiness, capability
negotiation, upgrade/downgrade/rollback refusal, and daemon-down cancellation
behavior. Queue-model owns the bytes and recovery decisions for canonical
queues, intents, and receipts.

Evidence:

- `specs/process-lifecycle.md` PL-003a, PL-004, PL-005, PL-011, PL-027,
  PL-028, and PL-028c.
- `internal/queue/persistence.go` queue paths and migration.
- `internal/queue/rpc.go` `HandlerAdapter.HandleQueueCancel` and
  `HandleQueueStatus`.
- `cmd/harmonik/queue_cli.go` and daemon socket dispatch for queue operations.
- CQ-00 transport/startup writer inventory.

## PL1 — Wire inventory

The amendment must preserve and inventory:

- submit request `name` and `workers`;
- append request `name` and group selection;
- QueueStatus selector `name`/`queue_id`;
- queue-cancel's shipped JSON request `{queue, force}`, plus an optional
  compatible `queue_id` extension; an N-1 `{queue, force}` request must decode
  unchanged;
- successful and error response behavior.

QueueStatus precedence is queue-model-owned but lifecycle-visible:
nonempty name wins and ignores ID; otherwise nonempty ID selects exact ID;
otherwise `main`. Exact-ID status may be receipt-backed.

Cancel does not share status precedence: `queue` alone selects the normalized
name, `queue_id` alone selects exact identity, and both must resolve to the
same queue or return `queue_selector_conflict`. Neither selector is invalid;
cancel never silently defaults to `main`. The current CLI's
`cancelFindByID` locally resolves `--queue-id` to a name before
`tryDaemonQueueCancel` sends shipped `{queue, force}`; the migration may send
the additive `queue_id` directly only while preserving that N-1 wire.

## PL2 — Daemon-down cancellation

The CLI is a transport client, not a persistence owner. If the target daemon is
unreachable, queue cancellation must:

- exit 17;
- create, modify, rename, or unlink no canonical queue;
- write no temp, archive, replace intent, archive intent, legacy file, receipt,
  or event;
- perform no local recovery or cleanup.

Capability support cannot be interpreted as permission for a daemonless
fallback. Only the target daemon’s QueueStore/startup adapter may write.

## PL3 — Startup inventory and receipt-root establishment

Current startup inventories canonical queues and migration but has no receipt
root or protocol-record capability phase. The required queue startup subphase,
before queue readiness and before loading dispatchable queues, is:

```text
verify queue parent
-> create/validate flat .completion-receipts root
-> if first-created, fsync/close .harmonik/queues
-> verify supported replace/archive-intent and receipt capabilities
-> enumerate and classify queue-owned protocol records
-> recover replacement/archive/receipt/cleanup/migration state
-> load canonical queues
-> reconcile ledger
-> advertise queue readiness
```

`mkdir` EEXIST is success only after opening/verifying the expected directory.
A definite pre-mutation failure leaves it uncreated. An ambiguous create or
queues-parent sync requires reload/reconciliation. Readiness is forbidden
until the root and all required recovery facts are classified.

The flat receipt root must not be enumerated as a queue. Receipt GC may run
after readiness, but recovery-required cleanup and minimum-retention
eligibility must already be understood.

## PL4 — Capability contract

A target daemon must support the complete set, not isolated record parsing:

- exact replace-intent schema/version and candidate/prior classifier;
- exact archive-intent schema/version and archive/migration classifier;
- completion-receipt schema/version;
- receipt ID/basename/exact-byte binding in final replace intents;
- flat receipt-root create/EEXIST/type/parent-sync protocol;
- no-replace or exact-byte-idempotent receipt installation;
- queue-ID plus completed-digest CAS cleanup;
- exact-ID live/receipt QueueStatus semantics;
- minimum retention and crash-safe receipt GC;
- unresolved-state refusal and operator-visible diagnostics.

Unknown versions, corrupt records, missing operations, or partial capability
fail startup/upgrade closed. A binary that can parse a receipt but cannot
honor CAS cleanup or retention is not compatible.

## PL5 — Upgrade, exec, downgrade, and rollback

Before handing ownership to a target binary, lifecycle compares the target
capability set with queue-owned records and possible states on disk.

- Compatible target: may exec and recover.
- Incompatible target with no relevant queue state: policy may allow operation
  only if it cannot encounter/create unsupported state.
- Incompatible target with canonical/intents/receipts or unresolved cleanup:
  refuse before ownership handoff.
- Downgrade/rollback cannot discard, reinterpret, or locally delete newer
  records.

Operator output identifies the unsupported record type/version or missing
behavior. It does not prescribe JSONL repair or event replay.

## PL6 — Recovery ownership and readiness

The startup adapter is a bounded phase of the same daemon owner. It may:

- establish the receipt root;
- read and classify queue-owned records;
- complete an intent-selected replacement/archive/receipt/cleanup;
- install recovered canonical state into QueueStore;
- retain per-name refusal/quarantine.

It may not:

- synthesize or retry `queue_group_completed`;
- inspect JSONL to choose queue state;
- mint a replacement receipt ID;
- reconstruct receipt bytes from current time/state;
- create a local CLI fallback;
- generalize records into a repository-wide intent service.

Restart can finish receipt-governed canonical cleanup and later eligible GC.
Event append outcome never affects readiness once queue-owned facts resolve.

## PL7 — Shutdown versus operator withdrawal

Graceful shutdown drain and live operator cancellation share durable
cancelled-state/archive mechanics but have distinct semantics:

- shutdown follows PL-011 ordering and never emits
  `queue_cancelled_operator`;
- live operator cancellation attempts that audit only after durable cleanup and
  ownership release, before RPC success;
- neither path creates a completion receipt;
- failure/indeterminacy retains queue ownership/refusal until queue-owned facts
  reconcile.

PL-010 degraded vocabulary is not redefined. Pre-ready unsupported/corrupt
queue infrastructure blocks readiness; post-ready per-name mutation ambiguity
uses queue refusal plus infrastructure diagnostics.

## Conformance implications

- First-boot receipt-root creation cuts at mkdir, parent open, parent fsync, and
  parent close.
- EEXIST file/symlink/wrong-type rejection.
- Startup with every supported replace/archive/receipt state.
- Unknown/corrupt version refusal before ready.
- Upgrade to a complete-capability binary succeeds; partial capability refuses.
- Downgrade with retained receipt or unresolved intent refuses without writes.
- Daemon-down queue cancel returns 17 with byte-for-byte filesystem proof.
- Startup cleanup never emits the final completion observation.
- Graceful shutdown never emits the operator audit.

## Decision

Amend process-lifecycle only enough to expose queue-owned capability and
transport requirements. It owns when the daemon may recover and become ready;
it does not own receipt content, queue completion, event replay, or a second
writer.
