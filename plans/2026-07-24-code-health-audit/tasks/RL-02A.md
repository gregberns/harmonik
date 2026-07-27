# RL-02A — Integrate review continuity

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `ARCH-01`, `ARCH-GATE`, `RL-01`
- Work type: serial reviewloop production integration

## Objective

Wire the existing reviewed continuity/checkpoint owner into production
`runReviewLoop` and delete only the duplicate inline continuity path.

## Evidence to verify first

Use `RL-01`, the reviewed continuity contract/tests, current production
checkpoint reads/writes, and exact `ARCH-01` structural targets.

## Exclusive lease

Continuity/checkpoint regions of `reviewloop.go`, the existing continuity owner,
focused tests. Sole `reviewloop_spine` writer; no shared baseline file.

## Required work

1. Add a production-call-site mutation test.
2. Route checkpoint load/advance/restart through the existing owner.
3. Preserve durable ordering and error propagation.
4. Delete duplicate inline decisions.
5. Meet the exact approved structural target.

## Acceptance

- Continuity has a production caller and one durable owner.
- Restart/no-progress behavior is unchanged.
- `runReviewLoop` metrics meet the task target.

## Verification

Continuity fault/restart/mutation/race/repeat tests, architecture gate,
lint/UBS, and `make check-fast`.

## Escalate when

Stop if production semantics contradict the reviewed continuity contract.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
