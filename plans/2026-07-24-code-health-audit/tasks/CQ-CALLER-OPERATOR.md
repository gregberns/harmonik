# CQ-CALLER-OPERATOR — Migrate operator pause and resume

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-02I`
- Work type: file-disjoint queue transaction caller migration

## Objective

Migrate `QueueOperatorEventConsumer.transitionToPausedByDrain` and
`transitionToActive` to clone → mutate → persist → install. A failed durable
write produces no in-memory transition, wake, or success event.

## Evidence to verify first

Inspect `internal/queuewiring/operatorevents.go`, its tests, CQ-00A caller
evidence, and the finalized transaction/error contract.

## Exclusive lease

`internal/queuewiring/operatorevents.go` and focused tests only. No workloop,
RPC, budget, reservation, or terminal edits.

## Required work

1. Add persist-failure and retry-convergence tests for pause and resume.
2. Route both named and all-queue paths through the transaction.
3. Install/wake/emit only after durable success.
4. Delete live-pointer/fallback mutation.

## Acceptance

- Failed persist leaves disk, installed memory, wake count, and events unchanged.
- Retry applies the transition exactly once.
- Named and all-queue behavior remain equivalent.

## Verification

Fault/property/race/repeat tests, queuewiring suite, delta lint, UBS, and
`make check-fast`.

## Escalate when

Stop if operator semantics require a new status transition contract.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
