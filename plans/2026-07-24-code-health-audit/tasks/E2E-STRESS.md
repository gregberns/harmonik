# E2E-STRESS — Repeat ownership invariants under concurrency

## Dispatch metadata

- Group / priority: end-to-end / P1
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `E2E-BASE`
- Work type: race/stress proof

## Objective

Repeat concurrent submit/dispatch/stop/recover sequences with deterministic
barriers and prove at-most-one ownership and terminal fixed points.

## Exclusive lease

Task-specific stress test and existing deterministic scenario helpers.

## Acceptance

No timing sleeps as oracle; repeat/race runs have bounded goroutines/processes
and preserve reservation/Run/terminal invariants.

## Verification

Race, high repeat count within disk budget, leak bounds, review, check-fast.

## Escalate when

File a concrete defect with durable failing trace.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

