# WL-02B — Extract the dispatch-permission decision

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `ARCH-01`, `ARCH-GATE`, `WL-01`, `WL-02A`
- Work type: serial workloop decomposition

## Objective

Extract the pure decision that permits, delays, or stops dispatch based on
shutdown, governor, capacity, dashboard, handler/operator pause, and maintenance
observations. It performs no durable, queue, claim, Run, or process effect.

## Evidence to verify first

Use `WL-01`, `WL-02A` observations, existing pause/governor/capacity decision
helpers, and exact `ARCH-01` metric targets.

## Exclusive lease

Only dispatch-gating regions of `workloop.go`, one pure decision owner, and
focused tests. Sole `dispatch_spine` writer; no shared baseline file.

## Required work

1. Define a typed allow/delay/stop decision with reason and wake condition.
2. Reuse existing pure decisions rather than wrapping them redundantly.
3. Remove inline gate branching and compatibility fallbacks.
4. Preserve wait/wake behavior using `WL-01` barriers.
5. Meet the exact approved structural target.

## Acceptance

- Gate evaluation is pure for captured inputs.
- Effects occur only after the returned decision.
- Stop/delay reasons remain observable and deterministic.
- `runWorkLoop` span/complexity meets the approved target.

## Verification

Exhaustive table/mutation tests, race/repeat composition, architecture gate,
delta lint, UBS, and `make check-fast`.

## Escalate when

Stop if a gate requires mutating queue, Bead, Run, or process state.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
