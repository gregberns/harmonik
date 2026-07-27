# Receipt-architecture change-design review — Round 2 response

## Disposition

`R2-1` is accepted. The design now uses one concrete queue-owned retention
anchor: immutable completion-release marker v1. No completion receipt is
mutated, and no event or JSONL fact participates in release or GC.

## Exact mechanism

The deterministic marker path is:

```text
.harmonik/queues/.completion-receipts/
  <queue_id>--<receipt_id>.release-v1.json
```

Its canonical schema binds `record_type`, schema version, queue ID, receipt ID,
final transaction ID, the SHA-256 digest of the exact receipt bytes, the
completed-canonical digest, `released_at`, and `gc_not_before`.
`gc_not_before` must equal `released_at + 720h`.

QM-053 may sample `released_at` only after the old canonical absence and final
intent absence are directory-durable and QueueStore/refusal ownership has
released. Marker installation is temp create/write/fsync/close, no-replace
rename to the deterministic basename, then receipt-root open/fsync/close. A
valid existing bound marker wins unchanged. Mismatched, corrupt, or unsupported
markers are preserved and disable GC without weakening receipt status or
same-name admission.

A crash after release but before marker durability is conservative. Startup or
queue-owned maintenance must re-prove exact old canonical absence (or a newer
queue ID), old transaction-intent absence, no surviving old owner, and receipt
integrity. Only then may it sample a later `released_at`. It never recovers an
earlier time from `completed_at`, filesystem metadata, JSONL, or events.

GC requires trusted synchronized UTC. Clock unavailability, regression,
non-canonical/overflowing arithmetic, or `now < gc_not_before` refuses GC.
Restart therefore neither resets nor shortens a valid marker interval, and a
backward step only delays deletion. If the host cannot satisfy the trusted-UTC
contract, GC remains disabled.

GC revalidates the exact receipt-marker pair, unlinks and root-syncs the receipt
first, and only after durable receipt absence unlinks and root-syncs the marker.
Every unlink/root-sync ambiguity reloads the exact entries. A marker-only
orphan remains until its own boundary and is then removed without recreating a
receipt.

## Delayed-cleanup proof

The conformance scenario now fixes completion at `T0`, holds cleanup unresolved
past `T0 + 30d`, and proves there is no marker and no GC eligibility. Cleanup
and ownership release finish at later `T1`; the durable marker anchors
`gc_not_before = T1 + 720h`. Restart and clock regression cannot make the
receipt eligible before that boundary.

## Updated surfaces

- `04-design/queue-model-design.md`: schema, ordering, namespace/fsync cuts,
  restart classifier, time semantics, GC ordering, marker-only recovery, and
  delayed-cleanup case.
- `04-design/process-lifecycle-design.md`: whole-capability negotiation,
  startup authority, and incapable downgrade refusal.
- `05-changelog.md`: planned queue and lifecycle deltas.
- `07-tasks.md`: `CQ-RECEIPT` owns the marker implementation and full fault
  proof; the receipt scenario now covers delayed cleanup, restart, clock
  regression, and receipt-first GC.
- `plans/2026-07-24-code-health-audit/tasks/evidence/CQ-02.yaml`: exact schema,
  capability rule, crash matrix, task lease, and rollback boundary.

The canonical completion receipt remains immutable completion authority.
Events remain observational, with no replay, synthesis, delivery state, or GC
role.
