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
helpers, the `operator_pause_br_ready` and `capacity_gate` gap returns, and
exact `ARCH-01` metric targets.

## Exclusive lease

Only dispatch-gating regions of `workloop.go`, one pure decision owner, and
focused tests. Sole `dispatch_spine` writer; no shared baseline file.

## Required work

1. Define a typed allow/delay/stop decision with reason and wake condition.
2. Reuse existing pure decisions rather than wrapping them redundantly.
3. Remove inline gate branching and compatibility fallbacks.
4. Preserve wait/wake behavior using a deterministic gate-evaluated or
   loop-cycle rendezvous, or a synchronous extracted-step boundary. Do not
   assume that `WL-01` supplied a barrier for its returned gaps.
5. Add production-call-site traces for `operator_pause_br_ready` and
   `capacity_gate`. Each trace must observe the hold before release or
   cancellation, prove that no br-ready poll, queue selection, claim,
   registration, or spawn occurs while held, and prove reevaluation after
   release.
6. Add mutations that bypass each gate and prove that the corresponding oracle
   rejects them.
7. Meet the exact approved structural target.

## Acceptance

- Gate evaluation is pure for captured inputs.
- Effects occur only after the returned decision.
- Stop/delay reasons remain observable and deterministic.
- Operator pause and full capacity have deterministic production-composition
  oracles that cannot pass because the loop was cancelled before evaluating the
  gate.
- While either gate holds, no source, claim, registration, or spawn effect can
  occur; releasing the gate causes deterministic reevaluation.
- `runWorkLoop` span/complexity meets the approved target.

## Verification

Exhaustive table/mutation tests, named `operator_pause_br_ready` and
`capacity_gate` production-composition traces, gate-bypass mutations,
race/repeat composition, architecture gate, delta lint, UBS, and
`make check-fast`.

## Escalate when

Stop if a gate requires mutating queue, Bead, Run, or process state.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
