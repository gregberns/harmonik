# E2E-FAULT — Cut every durable queue/run boundary

## Dispatch metadata

- Group / priority: end-to-end / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `E2E-BASE`, `JR-04`
- Work type: fault harness

## Objective

Inject controlled cuts at admission persist/install, reservation, claim/Run
creation, launch, finalization, terminal persistence, and release; assert the
reviewed recovery oracle.

## Exclusive lease

Existing fault/scenario harness and one task-specific test file. No production
semantics.

## Acceptance

Every cut yields one parseable durable state and, after recovery, zero or one
execution according to the oracle; no leaked claim or silent ambiguity.

## Verification

Fault matrix, race/repeat, review, check-fast.

## Escalate when

Create a production recovery task for any violated invariant.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

