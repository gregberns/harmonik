# Event-model receipt correlation and operator-audit design

## Mode

Minimal change design for `specs/event-model.md`. Queue-model remains the
completion authority; event-model owns only payload shape, class, and
normal-path ordering.

## Current state

`queue_group_completed` is Class F with queue ID, group index, final status,
counts, and completion time. The §8.10 ordering rule requires the last item
terminal event before group completion. Queue-model currently calls the final
event a durable landmark, even though EV-021/EV-022 make JSONL observational.

There is no typed `queue_cancelled_operator` event. Existing JSONL, EventID,
`ScanAfter`, torn-tail, subscription, and consumer contracts are mature and do
not need migration.

## Target state

### 1. Authority statement

Amend the queue-event cohort to state:

- canonical queue bytes, intents, and receipts decide commit, completion,
  cleanup, recovery, release, retention, and admission;
- `queue_group_completed` is a derived observation;
- Class F requires the normal producer’s existing fsync-backed append attempt,
  not persistent queue-owned delivery state;
- EV-021 and EV-022 remain unchanged.

### 2. Optional `completion_receipt_id`

Add to `QueueGroupCompletedPayload`:

```yaml
completion_receipt_id: <canonical lowercase UUIDv7 string> | absent
```

Presence table:

| Outcome | Field |
|---|---|
| non-final `complete-success` | absent |
| final `complete-success` | required; exact intent-bound receipt ID |
| `complete-with-failures` | absent |

The field is additive and optional for compatibility. Older readers ignore it;
new readers validate its canonical UUID form when present.
`CQ-RECEIPT` owns the field and
`internal/core/queueevents_extqueue.go`
`QueueGroupCompletedPayload.Valid`, including absent/additive/canonical-UUID
compatibility tests in `internal/core/queueevents_extqueue_test.go`.

### 3. Producer ordering

For final success:

```text
last item terminal event
-> authoritative completed queue state durable
-> authoritative completion receipt/root durable
-> one queue_group_completed append attempt
-> queue-model CAS cleanup
```

The payload is constructed before replace-intent durability so a construction
failure is `not_committed`. The append is attempted once on the uninterrupted
normal path. Crash before/after it or append failure may leave it absent or
uncertain.

Restart never:

- retries or synthesizes the event;
- searches JSONL to decide whether it occurred;
- retains queue refusal because it is absent.

Event outcome never gates unlink, QueueStore clear, readiness, retention/GC,
or same-name reuse.

Non-final success and failure retain existing semantic ordering:

```text
last item terminal -> durable group state -> group-completed attempt
-> successor-started attempt OR paused attempt
```

The optional receipt field is absent in both cases.

### 4. Existing event machinery unchanged

Explicitly preserve:

- envelope/EventID generation;
- JSONL record and writer;
- F-class append implementation;
- `ScanAfter` and EventID watermark behavior;
- torn-tail handling;
- subscription gap behavior;
- typed decode/replay registry;
- every current consumer/reader.

No effect key, event outbox, segmented storage, ReplayCursorV2, reader
migration, full-log lookup, truncation, repair CLI, cross-process lock, or
writer census is authorized.

### 5. New `queue_cancelled_operator`

Declare one queue-source Class O event for a successful live operator
cancellation.

Its typed payload is an XOR:

- **parseable**: canonical queue ID, normalized name, prior status, selected
  archive identity/path, cancellation time, and stable request identity if the
  RPC provides one;
- **corrupt**: normalized name, corruption classification/digest evidence,
  selected archive identity/path, and cancellation time; parsed-queue-only
  fields absent.

Exactly one variant validates. Both or neither is invalid. Payload avoids raw
canonical bytes and secrets.

`CQ-CALLER-CANCEL`, serialized after `CQ-RECEIPT`, owns the complete typed
surface: `internal/core/eventtype.go`
`EventTypeQueueCancelledOperator`,
`internal/core/queueevents_extqueue.go`
`QueueCancelledOperatorPayload` and `Valid`,
`internal/core/eventreg_hqwn59.go` `registerQueueEvents`, the per-type
compatibility row in `internal/core/pertypecompat_hqwn38.go`, and exact
constructor/decode/cohort/count/Class-O coverage in
`internal/core/queueevents_extqueue_test.go`,
`internal/core/pertypecompat_hqwn38_test.go`, and
`internal/core/eventtype_coverage_gjyks_test.go`.

### 6. Operator-audit ordering

```text
cancelled state durable
-> archive and legacy cleanup durable
-> archive intent removed durably
-> QueueStore/refusal released
-> queue_cancelled_operator attempt
-> successful RPC response
```

The event is:

- best-effort Class O;
- outside replace/archive intents and completion receipts;
- non-replayed;
- absent on graceful shutdown;
- not a cancellation success landmark.

Append failure logs a diagnostic but cannot roll back archive success, recreate
an intent, reacquire ownership, block same-name reuse, or suppress RPC success.

### 7. Waiter compatibility gate

Making queue observations crash-optional is safe only after both daemon-backed
run wait branches have exact-ID authoritative fallback. Landing order:

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT
                         |             |
                         +-> CQ-04 ----+-> JR-03
```

`CQ-01` and `CQ-RECEIPT` serialize on `internal/queue/rpc.go`.
`CQ-04` additionally requires `JR-01`; capable startup recovery and
CQ-RUN-WAIT both gate JR-03.
`CQ-RUN-WAIT` exclusively migrates `viaWatchGroupCompletion` for fresh submit
and shared append. No implementation enables crash-optional group observation
before it lands.

### 8. Conformance

- final success includes exact receipt ID;
- non-final success and all failures omit it;
- invalid UUID receipt ID fails payload validation;
- construction failure precedes durable intent;
- append failure continues receipt-governed cleanup;
- restart with receipt/no event emits nothing;
- existing log/replay golden tests remain unchanged except explicit additive
  final payload construction;
- operator audit accepts exactly one XOR variant;
- audit failure leaves successful cancellation/release unchanged;
- shutdown emits no audit.

## Rationale

Receipt correlation helps observers join an event to authoritative queue state
without elevating JSONL into recovery authority. A one-attempt producer rule
matches real crash behavior and avoids a global event-delivery subsystem.
Separating operator audit from cancellation state keeps observability useful
without weakening durable archive semantics.

## Requirements traceability

| Component | Target sections |
|---|---|
| EV1 completion observation | 1–4 |
| EV2 operator audit | 5, 6 |
| waiter composition | 7 |
| conformance | 8 |

## Explicit non-changes

All event-log, replay, cursor, writer, reader, and consumer behavior outside
the two queue surfaces above.

## Design readiness

Ready for independent change-design review.
