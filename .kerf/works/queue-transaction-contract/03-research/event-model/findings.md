# Event-model research findings

## Scope

The event-model delta is deliberately small:

1. correlate the final successful `queue_group_completed` observation to the
   authoritative queue completion receipt; and
2. add a typed best-effort live operator-cancellation audit.

It does not change event identity, persistence, replay, framing, cursors,
writers, or consumers.

Evidence:

- `specs/event-model.md` §6.3, §8.10.3, EV-016, EV-021, EV-022, EV-047,
  and the §8.10 ordering rule.
- `specs/queue-model.md` QM-033/QM-053 and current final-landmark conflict.
- `internal/core/event.go` queue event constants and payload registry.
- `internal/queue/state.go` construction of `QueueGroupCompletedPayload`.
- `internal/queue/rpc.go` `emitOrLog` and cancellation adapter.
- `cmd/harmonik/run_via_daemon.go` `viaWatchGroupCompletion`.

## Current state

`queue_group_completed` is Class F and its payload currently contains
`queue_id`, group index, final status, counts, and completion time. The queue
spec calls the final event a durable completion landmark, but EV-021 says
observational replay must not reconstruct state and EV-022 says state
reconstruction must not walk JSONL.

The producer currently constructs the event during group transition. Event
failure is logged in several callers, and the daemon-backed run waiter uses the
observation as its completion signal.

There is no typed `queue_cancelled_operator` event.

## EV1 — Authority boundary

Queue canonical bytes, intents, and receipts decide:

- whether a transaction committed;
- whether final completion occurred;
- whether canonical cleanup is safe/durable;
- whether QueueStore ownership may clear;
- whether same-name admission may proceed;
- what restart and receipt GC may do.

JSONL decides none of these. EV-021 and EV-022 remain unchanged and
load-bearing.

Consequently, `queue_group_completed` is a derived observation even when
Class F. Class F requires the ordinary producer’s fsync-backed attempt; it
does not create a queue-owned crash-delivery protocol.

## EV2 — Conditional completion receipt correlation

Add one optional payload field:

```yaml
completion_receipt_id: <canonical UUIDv7 string> | absent
```

Producer rule:

| Group outcome | Field |
|---|---|
| non-final `complete-success` | absent |
| final `complete-success` after receipt durability | present, exact intent-bound receipt ID |
| any `complete-with-failures` | absent |

The field does not turn an event into a receipt. It lets an observer correlate
a successfully appended final observation with the queue-owned landmark.

The producer must construct the final payload before durable replace-intent
creation so construction failure remains `not_committed`. It attempts the
event once on the normal QM-053 path after receipt-root durability and before
the completion operation returns. The last item’s terminal event retains its
existing precedence over this attempt.

A process crash before or after the append, or append failure, may leave the
event absent or the outcome uncertain. Restart:

- does not retry or synthesize it;
- does not search JSONL for it;
- does not retain a queue refusal because of it;
- does not gate canonical unlink, QueueStore clear, readiness, retention/GC,
  or same-name admission on it.

No event ID, effect key, response, pending flag, delivery acknowledgement, or
replay instruction is written to the replace intent or receipt.

## EV3 — Existing event mechanics are untouched

CQ-02 makes no change to:

- envelope EventID generation;
- JSONL record shape or writer;
- F append implementation;
- `ScanAfter` or EventID watermark semantics;
- offline replay or typed decode registry;
- torn-tail handling;
- subscription buffering/gap handling;
- current event consumers/readers;
- cross-process writer inventory.

Specifically excluded are effect keys, logical exactly-once projection,
segmented storage, ReplayCursorV2, reader migration, full-log lookup,
truncation, repair CLI, event lock, and writer census.

This is compatible with additive payload rules: older readers ignore the
optional field, and new readers must accept its absence except in a final event
they know was produced under the amended producer contract.

## EV4 — Typed `queue_cancelled_operator`

### Purpose and class

This is a Class O audit of a successful live operator cancellation. It is not a
queue state landmark, completion receipt, replacement/archive-intent member,
or shutdown event.

### Payload

The event-model should define a typed payload whose validation is an XOR:

- **parseable cancellation**: canonical queue ID, normalized name, selected
  archive identity/path, cancellation/completion time, and any stable operator
  request identity available from the RPC; or
- **corrupt-source cancellation**: normalized name, selected archive identity,
  corruption classification/digest evidence, and completion time, with fields
  requiring parsed queue content absent.

Exactly one variant is valid. Both or neither is invalid. The concrete field
set must use existing event payload conventions and avoid embedding canonical
queue bytes or secrets.

### Ordering

For live operator cancellation:

```text
cancelled canonical durable
-> exact archive durable
-> legacy cleanup durable if applicable
-> archive-intent removal durable
-> QueueStore/refusal release
-> attempt queue_cancelled_operator
-> return successful RPC response
```

Append failure is diagnostic only. It cannot roll back archive success,
recreate an intent, reacquire ownership, change the RPC result, or prevent
same-name resubmission.

Graceful shutdown never emits it. Restart never replays it. Cancellation never
creates or references a completion receipt.

## EV5 — Waiter composition

The existing waiter’s dependence on observations is why event optionality
cannot land alone. `CQ-RUN-WAIT` must first give both submit and append branches
an exact-queue-ID status fallback:

- live exact-ID watched-group state while canonical exists;
- exact-ID final receipt after canonical cleanup;
- no name fallback and no JSONL query.

When every watched run observation is known, existing own-bead attribution
remains. Missing group/pause observations are filled by durable watched-group
or receipt status. This belongs to queue status/caller implementation, not a
new event consumer contract.

Required landing order:

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT -> JR-03
```

`CQ-01` precedes `CQ-RECEIPT` because both edit
`internal/queue/rpc.go`; that shared-file work is serialized.
`CQ-RECEIPT -> CQ-04 -> JR-03` is also required so capable startup recovery
joins the waiter before crash-optional live event production.
No implementation may enable crash-optional group observation before the
waiter lands.

## Conformance implications

- Final success payload contains the exact receipt ID.
- Non-final success and all failure payloads omit it.
- Final event construction failure occurs before intent and is
  `not_committed`.
- Event append failure after receipt durability does not block cleanup.
- Restart with receipt and no event emits nothing.
- Same-name reuse cannot affect old event correlation or waiter status.
- Existing JSONL/replay golden tests remain byte/behavior compatible except
  for explicitly constructed final payloads containing the additive field.
- Operator audit payload accepts exactly one parseable/corrupt variant.
- Audit failure leaves cancellation success and released ownership unchanged.
- Shutdown cancellation emits no operator audit.

## Decision

Amend only the queue completion payload’s optional receipt correlation and the
new typed operator audit. Preserve all current event-log machinery and
EV-021/EV-022. Queue-model remains the sole completion and cleanup authority.
