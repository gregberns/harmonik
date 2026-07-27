# CQ-CALLER-CANCEL — Migrate live and CLI cancellation

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `sol_high` (`gpt-5.6-sol`, high)
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-RECEIPT`
- Conflicts with: `CQ-RECEIPT` on `internal/queue/rpc.go`
- Lease family: `queue_cancel_rpc`

## Objective

Route operator cancellation through the live QueueStore owner and the approved
linked replace/archive transaction. Daemon-down cancellation exits 17 and
writes nothing; the typed Class-O audit is best effort after durable release.

## Exact ownership

- `internal/queue/rpc.go` `HandlerAdapter.HandleQueueCancel`;
- `internal/queue/types.go` `QueueCancelRequest`/`QueueCancelResponse`,
  preserving JSON `queue` and `force`, additively adding `queue_id`;
- `internal/queue/cli/cancel.go` `RunQueueCancel`, `tryDaemonQueueCancel`,
  `cancelFindByID`, `journalCancel`, `emitQueueCancelEvent`;
- `internal/core/eventtype.go` `EventTypeQueueCancelledOperator`;
- `internal/core/queueevents_extqueue.go`
  `QueueCancelledOperatorPayload`/`Valid`;
- `internal/core/eventreg_hqwn59.go` `registerQueueEvents`,
  `internal/core/pertypecompat_hqwn38.go`, and exact coverage tests.

## Non-goals

No local daemon-down archive/event fallback, shutdown audit, terminal
workloop, receipt design, global replay, or response replay. Both selectors
must resolve the same queue; neither-present is invalid.

## Failing-first proof

Cover N-1 `{queue,force}`, dual-selector agreement/conflict, daemon-down
byte-identical filesystem/event state, replace-only/exact-pair/archive-only/
mismatch/originless classifiers, every successor/predecessor/archive/legacy/
cleanup parent-sync cut, corrupt audit XOR, registration/compatibility, and
audit append failure preserving cancellation success.

Exercise those oracles end to end through the production CLI/socket/RPC path:
`RunQueueCancel` → `tryDaemonQueueCancel` →
`HandlerAdapter.HandleQueueCancel`, including an unavailable daemon. Then run
real-isolation mutations in a detached worktree only:

- restore any daemon-down local archive, journal, or event write and prove the
  daemon-down byte-identical-state test fails;
- reorder linked replacement/archive durability or remove an intervening
  parent sync and prove the crash-cut/restart oracle fails; and
- accept a replace-only, archive-only, mismatched, or originless linked state
  and prove the production cancellation/recovery path fails closed.

For each mutation, remove the production fix only, run the same focused
production-path test and prove it fails. Restore the exact production source,
then prove the test passes. Fixture-only mutation, changed expectations, mock-
only handler calls, and disabled assertions do not satisfy this proof.

## Accepted intermediate state

Operator cancellation is single-owner and restart-safe. Shutdown/no-audit
composition remains `JR-03`.

## Completion and rollback

Fault/restart/compatibility/race tests, the real-isolation production
CLI/socket/RPC mutation proof above, UBS/vet/lint, check-fast, and Sol xhigh
review pass. Completion evidence must include each detached-worktree failing
command/result and its restored-source passing command/result. Roll back
linked handoff, CLI/RPC, and typed audit registration as one unit.
**COMMIT EXPLICITLY.**
