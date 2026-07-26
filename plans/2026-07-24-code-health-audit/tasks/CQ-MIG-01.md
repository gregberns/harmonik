# CQ-MIG-01 — Preserve the valid legacy queue during migration

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-00B`
- Work type: bounded proven durability defect

## Objective

Fix `MigrateFromLegacy` so an existing corrupt/empty destination never causes
deletion of the only parseable legacy queue.

## Exclusive lease

- `internal/queue/persistence.go`
- `internal/queue/persistence_test.go` or one focused migration test file

## Required work and acceptance

Add failing cases for corrupt, empty, conflicting, and valid destination files;
for each injected cut, require at least one parseable copy. Valid prior
migration remains idempotent. Do not introduce the general transaction API.

## Verification

Focused persistence/fault tests, repeat, vet/lint/UBS, review, `make check-fast`.

## Escalate when

Stop if the fix needs a queue schema or global generation decision owned by
`CQ-02`.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

