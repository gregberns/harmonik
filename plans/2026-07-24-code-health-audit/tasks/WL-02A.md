# WL-02A — Extract periodic maintenance from runWorkLoop

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `ARCH-01`, `ARCH-GATE`, `WL-01`, `CQ-CALLER-EAGER`
- Work type: serial workloop decomposition

## Objective

Extract cadenced maintenance from `runWorkLoop`: schedule tick, coordinator
reap, disk/cache checks, the trigger/call to the already-transactional eager
refill effect, and sentinel observation. It does not own eager-refill queue
mutation. Return typed observations only; dispatch permission remains `WL-02B`.

## Evidence to verify first

Use `WL-01`, the exact per-symbol targets approved by `ARCH-01`, and current
maintenance helpers/cadences.

## Exclusive lease

Only the periodic-maintenance regions of `workloop.go`, one narrow target owner,
and focused tests. Sole `dispatch_spine` writer. The architecture baseline is
coordinator-owned and not in the worker lease.

## Required work

1. Add failing fidelity tests for cadence and effect ordering.
2. Define complete narrow inputs; do not pass `workLoopDeps`.
3. Extract periodic effects without dispatch/claim/reservation policy.
4. Delete the inline path.
5. Meet the exact pre-approved span/complexity target and return measurements
   for coordinator integration.

## Acceptance

- One named maintenance operation owns all listed cadenced effects.
- No dispatch permission or queue mutation enters the owner.
- Fidelity traces remain equivalent.
- The exact `ARCH-01` metric target is met without a suppression.

## Verification

Targeted fidelity, fake-clock repeat, race, architecture gate, delta lint, UBS,
and `make check-fast`.

## Escalate when

Stop if the slice requires dispatch, reservation, claim, or terminal semantics.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
