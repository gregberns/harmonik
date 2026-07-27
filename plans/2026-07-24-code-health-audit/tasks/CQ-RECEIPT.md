# CQ-RECEIPT — Add authoritative queue completion receipts

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `sol_xhigh` (`gpt-5.6-sol`, xhigh)
- Reviewer profile: independent `sol_xhigh`
- Depends on: `CQ-02I`, `CQ-01`
- Conflicts with: `CQ-01`, `CQ-CALLER-CANCEL` on `internal/queue/rpc.go`
- Lease family: `queue_receipt`

## Objective

Implement the approved QM-053 completion receipt, release marker, exact-ID
status, retention/GC, and final-only `completion_receipt_id` contract. One
intent-bound receipt is final-success authority; events remain optional
observations and never recovery state.

## Evidence and exact ownership

Read `specs/queue-model.md` QM-053/QM-057/QM-060, `specs/event-model.md`
§8.10, `specs/execution-model.md` EM-015f, CQ-02 evidence, and the CQ-02 Kerf
integration package. Own only:

- new `internal/queue/receipt.go` and focused tests;
- receipt/root/cleanup primitives and final replace-intent binding in
  `internal/queue/persistence.go`;
- `QueueStatusRequest`/`QueueStatusResponse` additive fields in
  `internal/queue/types.go`;
- `internal/queue/rpc.go` `HandleQueueStatus`, `findQueueByID`, and
  `HandlerAdapter.HandleQueueStatus`;
- `internal/queue/cli/status.go` `RunQueueStatus` and
  `internal/queue/cli/client.go` `renderQueueStatusText`;
- `internal/core/queueevents_extqueue.go`
  `QueueGroupCompletedPayload.CompletionReceiptID`/`Valid`, focused core
  compatibility tests, and the final-only construction boundary in
  `internal/queue/state.go`.

## Non-goals

No terminal workloop call site, waiter, cancellation, global JSONL writer,
effect key, replay cursor, event retry, or unrelated status redesign. Preserve
existing live status fields and CLI selectors.

## Required work and failing-first proof

1. Fault every root mkdir/EEXIST/type/parent-sync and receipt
   temp/write/fsync/close/no-replace/rename/root-sync cut.
2. Prove restart reuses the exact intent-bound ID/path/bytes/digest and never
   mints, reconstructs, or directory-selects a receipt.
3. Prove exact-ID live status wins; exactly one valid retained receipt may
   answer; zero is not found; multiple/corrupt/unsupported/disagreeing records
   fail identity integrity; watched index zero is representable.
4. Prove queue-ID plus completed-byte-digest CAS never touches a newer
   same-name queue.
5. Fault canonical/intent unlink, ownership release, release-marker
   install/recovery, trusted-UTC `released_at + 720h`, and receipt-first
   marker-last GC. GC never gates admission.
6. Prove `completion_receipt_id` exists exactly on final
   `complete-success`; event append failure is diagnostic and cleanup proceeds.
7. Add a real-isolation mutation proof through the production status path:
   start the daemon RPC adapter, query by exact queue ID through
   `HandlerAdapter.HandleQueueStatus` and `RunQueueStatus`, and verify the
   rendered/API result comes from live state first or the one valid retained
   receipt. In a detached worktree only, remove the production fix for each
   representative mutation below, run the same focused end-to-end test and
   prove it fails. Restore the exact production source, then prove that test passes:
   - prefer a receipt over a live same-ID queue or accept a name-only match;
   - accept zero, multiple, corrupt, unsupported, or digest-disagreeing
     receipts as authoritative;
   - mint/reconstruct a receipt ID during restart instead of reusing the
     replace-intent binding;
   - emit `completion_receipt_id` on a non-final or unsuccessful event; or
   - allow release-marker/GC ordering to remove authority before the receipt
     is durable.
   Fixture-only mutation, skipped tests, altered expectations, and mock-only
   handler calls do not satisfy this gate.

Nemotron/Pi may expand the fixed fault matrix and fixtures after the Sol owner
defines the oracle. It must not choose receipt identity, durability promotion,
retention, or recovery policy.

## Accepted intermediate state

Receipt/root/status/QM-053 primitives and conditional payload exist, but no
terminal caller enables crash-optional delivery. Global event mechanics stay
unchanged.

## Completion and rollback

All fault/property/race/compatibility tests, the real-isolation production
RPC/CLI mutation proof above, scoped UBS/vet/lint, `make check-fast`, and
independent Sol xhigh review must pass. The completion evidence must record
the detached-worktree failing command/result and the restored-source passing
command/result for every representative mutation. Roll back the
receipt/status/root/release-marker/retention/GC unit together; never retain a
partial producer or GC. **COMMIT EXPLICITLY.**
