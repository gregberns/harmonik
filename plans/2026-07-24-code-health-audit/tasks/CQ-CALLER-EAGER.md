# CQ-CALLER-EAGER — Migrate eager refill to the queue transaction

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-02I`
- Work type: file-disjoint queue transaction caller migration

## Objective

Replace eager refill's live-pointer path with a complete immutable named-fleet
snapshot, all-name duplicate pre-screen, deterministic normalized-name refill
target, and reviewed transaction. Cadence/trigger ownership remains `WL-02A`.

## Evidence to verify first

Inspect `internal/daemon/eagerfill_em063.go` `eagerRefillEval`, queue transaction
contract/tests, CQ lifecycle crash cuts, and current wake/event ordering.

## Exclusive lease

`internal/daemon/eagerfill_em063.go`, the exact queue transaction adapter call,
and focused tests. No `workloop.go`, reservation, claim, or terminal edits.

## Required work

1. Add failing persistence and retry-convergence tests.
2. Snapshot/clone current queue under the reviewed lock boundary.
3. Mutate and persist the candidate before install.
4. Wake/emit only after durable install.
5. Remove live-pointer mutation and nonfatal persistence behavior.
6. Scan every named queue for a candidate duplicate; choose the first eligible
   active stream target in normalized-name order after global and per-queue
   capacity gates. Paused/full siblings do not block.

## Acceptance

- Failed persist leaves memory/disk/event state unchanged.
- Retry appends the intended items exactly once.
- No mutable queue pointer escapes the transaction.
- `WL-02A` can call eager refill as a typed maintenance effect.
- A bead present under any sibling name is excluded.

Accepted intermediate: named eager refill consumes the transaction and owns no
receipt. Roll back only `eagerRefillEval`'s fleet selection/mutation/tests.

## Verification

Fault/property/race/repeat tests, scoped daemon/queue suites, delta lint, UBS,
and `make check-fast`.

## Escalate when

Stop if eager refill requires reservation or group-terminal policy changes.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
