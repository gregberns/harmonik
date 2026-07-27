# CQ-CALLER-CREW — Migrate crew queue placeholder creation

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-02I`
- Work type: file-disjoint queue transaction caller migration

## Objective

Migrate `crewHandlerImpl.ensureQueue` placeholder creation to the reviewed queue
transaction/admission owner. Preserve the completed-placeholder semantics while
removing direct persistence.

## Evidence to verify first

Inspect `internal/daemon/crewstart.go`, `crewstart_hkvrnh3_test.go`, CQ-00A
evidence, and the deferred crew decision. This task changes only shared queue
correctness; it does not reactivate crew work.

## Exclusive lease

`crewHandlerImpl.ensureQueue` and focused placeholder tests. No crew lifecycle,
workloop, registry, or launch edits.

## Required work

1. Add persist-failure, existing-queue, and retry tests.
2. Call the supported transaction/admission owner.
3. Preserve completed placeholder status/type/identity.
4. Delete direct persistence and false-success paths.

## Acceptance

- Failed creation leaves no partial installed/disk queue.
- Retry converges to one correct placeholder.
- Existing queue is never overwritten.
- Crew remains otherwise deferred.

## Verification

Focused fault/race/repeat tests, daemon/queue suites, lint/UBS, and
`make check-fast`.

## Escalate when

Stop if placeholder semantics conflict with the queue schema/contract.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
