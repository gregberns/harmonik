# CQ-MIG-01 — Preserve the valid legacy queue during migration

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-00B`
- Work type: bounded proven durability defect

## Objective

Fix `MigrateFromLegacy` so no destination condition or migration syscall error
can silently delete the valid legacy queue, choose between conflicting queues,
or report success before the relevant directory entry is durable.

## Exclusive lease

- `internal/queue/persistence.go`: only `MigrateFromLegacy` and one narrowly
  scoped, unexported migration fault-injection helper/seam if required
- the migration section of `internal/queue/persistence_test.go` and/or one
  focused migration test file

Explicitly excluded without a coordinator amendment: `Persist`, `Load`,
`Unlink`, `CompleteAndUnlink`, `CancelQueueOnShutdown`, the general transaction
API, and any package-global mutable fault hook.

## Required work

1. Make existing-destination handling deterministic:
   - empty, corrupt JSON, or wrong-schema destination returns an error and
     preserves both destination and valid legacy unchanged;
   - valid but conflicting destination fails closed and preserves both;
   - a valid destination proven equivalent to the intended legacy migration
     result may remove legacy, but success requires `fsync(.harmonik)`;
     “equivalent” means the decoded `Queue` values are deeply equal across all
     persisted fields after the same validation/normalization used by `Load`,
     never merely both valid or sharing a `QueueID`;
   - destination `Stat` errors other than `ENOENT` fail closed and preserve
     legacy;
   - absent legacy still syncs `.harmonik` before reporting successful
     convergence, so a retry after successful removal plus failed parent sync
     cannot silently skip the durability obligation.
2. Add injected syscall cuts for the destination-absent path:
   initial legacy read; destination `Stat`; queues-directory creation; temp
   create/write/file-sync/close; destination rename; queues-directory
   open/sync/close; legacy removal; and `.harmonik` directory open/sync/close.
3. Add injected cuts for the existing-destination path:
   destination read/parse/schema/equivalence; legacy removal; and `.harmonik`
   directory open/sync/close. The removal fault test must retry after legacy
   removal succeeded but `.harmonik` sync failed and prove the absent-legacy
   retry performs the outstanding parent-directory sync before success.
4. Prove operation order. For a new destination, destination rename and
   `fsync(queues/)` precede legacy removal. On both paths, legacy removal
   precedes `fsync(.harmonik)`. No success occurs before the applicable
   directory sync and close finish.
5. Prove retry convergence after every partial result without losing the valid
   intended queue. Call these syscall-cut tests, not proof of real power-loss
   behavior.

## Acceptance

- Every failure before destination rename preserves the parseable legacy queue
  and leaves canonical state unchanged.
- Every error after destination rename leaves at least one parseable intended
  queue copy, returns a truthful error, and converges on retry.
- Conflicting valid queues are never silently resolved.
- The current existing-destination branch no longer removes legacy and returns
  success without syncing `.harmonik`.
- Race tests do not depend on package-global mutable fault state.
- No excluded persistence symbol or transaction behavior changes.

## Verification

Replace `<claim-base>` with the exact claim SHA:

```bash
go test ./internal/queue -run '^TestMigrateFromLegacy_' -count=1
go test ./internal/queue -run '^TestMigrateFromLegacy_' -count=20
go test -race ./internal/queue -run '^TestMigrateFromLegacy_' -count=10
go vet ./internal/queue
./.tools/golangci-lint run --new-from-rev=<claim-base> ./internal/queue/...
ubs $(git diff --name-only <claim-base>..HEAD -- '*.go')
git diff --check <claim-base>..HEAD
git diff --name-only <claim-base>..HEAD
git diff --function-context <claim-base>..HEAD -- internal/queue/persistence.go
make check-fast
```

The final two diff inspections must show only the leased test files and
`MigrateFromLegacy` plus its narrowly scoped unexported helper. Independent
review must inspect order assertions and retry behavior, not just final files.

## Escalate when

Stop if the fix needs a queue schema or global generation decision owned by
`CQ-02`.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
