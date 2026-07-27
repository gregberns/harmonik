# CQ-02I — Implement the reviewed queue transaction primitive

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-02`, `CQ-MIG-01`
- Work type: durability implementation

## Objective

Implement the exact mutation/persist/install primitive approved by `CQ-02`,
with injected failures at every durable boundary. Do not migrate all callers in
this task.

## Exclusive lease

- `internal/queue/persistence.go` generic typed result/write primitives
- proposed `internal/queue/transaction.go`
- replace/archive-intent schemas and restart classifiers, including linked
  archive-handoff facts but not the cancellation caller
- QueueStore snapshot/generation API in `internal/queuewiring/store.go`
- focused fault/restart tests

RPC/workloop callers, receipt/root/status/QM-053,
`completion_receipt_id`, global event writing, and terminal call sites are
forbidden.

## Acceptance

Clone → mutate → persist → install order is enforced by API shape; failure
cannot expose memory ahead of disk; generation/lock rules match the spec; every
fault cut has a deterministic oracle.

Failing-first coverage must classify candidate/intent/canonical
rename-parent-sync cuts, prove stale snapshots perform zero I/O, and prove
restart selects only intent-bound facts. The accepted intermediate state is
an ordinary replacement/archive substrate with receipts and optional final
observations disabled. Rollback the substrate/API as one unit before caller
migrations.

## Verification

Targeted fault tests, race where applicable, vet/lint/UBS, review, check-fast.

## Escalate when

Stop on any divergence from the finalized contract or compatibility plan.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
