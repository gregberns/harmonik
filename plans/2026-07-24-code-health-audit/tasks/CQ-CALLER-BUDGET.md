# CQ-CALLER-BUDGET — Migrate budget pause and unpause

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-02I`
- Work type: file-disjoint queue transaction caller migration

## Objective

Migrate `internal/daemon/perqueuespendmeter_tigaf11.go`
`pauseQueueByBudget` and
`unpauseBudgetPausedQueues` to the queue transaction. Failed persistence must
not expose a paused/active state, wake dispatch, or emit success.

## Evidence to verify first

Inspect `internal/daemon/perqueuespendmeter_tigaf11.go`, budget rollover tests,
CQ-00A evidence, and the finalized transaction contract.

## Exclusive lease

The per-queue spend-meter file and focused tests. No workloop, review-charge,
operator-event, reservation, or terminal edits.

## Required work

1. Add pause/unpause persistence-failure and rollover-retry tests.
2. Use clone → mutate → persist → install for each queue.
3. Define deterministic partial-batch handling from the CQ-02 contract.
4. Wake/emit only for installed transitions.

## Acceptance

- Memory and disk agree after every cut.
- Day rollover retries converge without skipping or duplicating a queue.
- Budget status never clears another pause reason.

Accepted intermediate: budget mutations are transaction-backed and other
callers remain unchanged. Roll back only the two named symbols/tests.

## Verification

Fault/table/race/repeat tests, scoped daemon/queue suites, lint/UBS, and
`make check-fast`.

## Escalate when

Stop if multi-queue batch atomicity is unspecified by CQ-02.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
