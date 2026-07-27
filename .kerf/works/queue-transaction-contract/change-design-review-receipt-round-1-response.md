# Receipt-architecture change-design review — Round 1 writer disposition

This response does not modify
`change-design-review-receipt-round-1.md` or assert approval.

## R1 — linked cancellation handoff restored

The cancelled replacement now prebinds archive origin/kind, exact
source/candidate identity and digest, selected non-overwriting destination, and
the exact successor archive-intent ID/bytes/digest. The predecessor remains
through successor create/write/fsync/close/rename/queue-parent fsync.

Design, tasks, and evidence now classify replace-only, exact linked-pair,
archive-only, mismatched pair, and originless-cancelled states; cover
predecessor removal, archive rename/parent durability, main legacy
unlink/`.harmonik` durability, and successor removal. Recovery never guesses or
retimestamps a destination.

## R2 — startup recovery gates live completion

`CQ-04` is now a hard `JR-03` prerequisite. The intermediate and rollback
contracts prevent receipt-producing live terminal call sites before capable
startup recovery. The receipt crash/restart scenario now depends on `CQ-04`.

## R3 — waiter lease is implementable

`CQ-RUN-WAIT` now owns the
`runBeadSubcommandViaDaemon` watcher call site,
`viaWatchGroupCompletion` signature/body, and a smallest independent
`viaQueueStatusByID`/`viaStatusQuery` seam using `viaSendRequest` on a separate
RPC connection. Proof covers the immediate post-accept/setup query and every
heartbeat query for submit and append, continuing pending/active and
terminating transport failure.

## R4 — typed event symbols have exact owners

`CQ-RECEIPT` owns `QueueGroupCompletedPayload.CompletionReceiptID`,
`QueueGroupCompletedPayload.Valid`, and UUID/additive compatibility tests.
Serialized `CQ-CALLER-CANCEL` owns the cancellation event constant, payload
XOR `Valid`, `registerQueueEvents`, compatibility row, constructor/decode,
cohort/count, Class O coverage, and tests.

## R5 — GC task proof is complete

`CQ-RECEIPT` now explicitly proves eligibility boundary, identity
revalidation, unlink failure, absent idempotency, ambiguous unlink,
receipt-root open failure, fsync failure/ambiguity, reload-before-retry, close
after successful fsync, and admission non-blocking behavior. Its rollback
boundary forbids a partial GC implementation.
