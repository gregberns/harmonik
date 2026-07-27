# WL-MUT-01 — Own non-reservation queue mutations

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `ARCH-01`, `ARCH-GATE`, `CQ-02I`, `WL-01`, `WL-02B`
- Work type: serial durable mutation extraction

## Objective

Move non-reservation queue mutations out of `runWorkLoop` into the reviewed
clone → mutate → persist → install transaction owner: pending-group activation,
deferred reevaluation, claim-skipped deferral, cross-queue duplicate failure,
and max-attempts failure.

## Evidence to verify first

Use CQ lifecycle evidence, `WL-01`, the finalized queue transaction contract,
and the exact current branches/functions named above.

## Exclusive lease

Those five mutation branches in `workloop.go`, the queue transaction owner, and
focused crash/fault tests. Sole `dispatch_spine` writer. Reservation (`CQ-03`)
and terminal/group completion (`JR-03`) are forbidden.

## Required work

1. Add failure-before-persist and failure-after-persist oracles for each branch.
2. Route each mutation through one transaction operation.
3. Emit/wake only after durable install.
4. Delete inline mutation and nonfatal persistence paths.
5. Meet the exact approved `runWorkLoop` metric target.

## Acceptance

- No listed branch mutates a live installed queue before durable success.
- Retry/restart converges without duplicate effects.
- Reservation and terminal ownership remain untouched.
- Production calls the extracted owner exclusively.

## Verification

Fault matrix, property/race/restart/repeat tests, architecture gate, delta lint,
UBS, and `make check-fast`.

## Escalate when

Stop if a branch cannot use the finalized transaction contract without a schema
or terminal-policy change.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
