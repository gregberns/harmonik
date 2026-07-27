# CQ-CALLER-WORKLOOP-MAINTENANCE — Migrate residual workloop mutations

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `sol_xhigh`
- Model / effort: `gpt-5.6-sol` / `xhigh`
- Reviewer profile: fresh `sol_xhigh`
- Depends on: `CQ-03`, `JR-03`
- Conflicts with: every active `dispatch_spine` card and `WL-REC-01`
- Lease family: `dispatch_spine`

## Objective and exact ownership

Migrate only the deferred reevaluation, claim-skipped dependency,
cross-queue duplicate, and max-attempt direct-persist regions in
`internal/daemon/workloop.go` `runWorkLoop` to QueueStore transactions, with
focused tests. Exact current branch anchors are:

- deferred reevaluation: the active-group loop calling
  `queue.ReevaluateDeferred`, its `queue.Persist` branch, and the
  `hasDeferredItems`/`workloopSleep` retry decision;
- claim-skipped dependency: the non-open `preClaimRecord.Status` branch that
  emits `core.BeadClaimSkippedPayload`, sets
  `queue.ItemStatusDeferredForLedgerDep`, and calls `queue.Persist`;
- cross-queue duplicate: the `crossQueueConflict` scan and the branch setting
  `LastFailureReason = "cross_queue_duplicate"` before `queue.Persist`; and
- max-attempt maintenance: the `Attempts++`/`maxAttemptsHit` branch setting
  `LastFailureReason = "max_attempts_exceeded"` before `queue.Persist`.

## Non-goals

No reservation, claim/Run, group activation, terminal/QM-053, cancellation,
recovery/adoption, selection, or scheduling refactor.

## Proof and accepted intermediate

For each exact `runWorkLoop` branch above, inject persistence failure and prove
installed state/events/wake remain unchanged; retry converges once. Mutations
restoring direct `queue.Persist` at any named call, bypassing the transaction
around the named state assignment, or reopening another owner's semantics must
fail. Accepted intermediate: all residual maintenance uses the transaction API
while `runWorkLoop` retains control scheduling.

## Completion and rollback

Targeted fault/race/repeat/mutation tests, architecture/delta lint, UBS,
check-fast, and fresh Sol xhigh review pass. Roll back only the four named
`runWorkLoop` branch regions and their focused tests. Do not start while
`WL-REC-01` is active, and do not broaden rollback into its restart
gate/adoption regions. **COMMIT EXPLICITLY.**
