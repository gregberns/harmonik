# RL-02C — Integrate reviewcycle decisions

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `RL-02B`
- Work type: serial reviewloop policy integration

## Objective

Wire the reviewed pure `reviewcycle` decision kernel into production
`runReviewLoop` and delete its duplicate inline iteration/verdict decisions.

## Exclusive lease

Iteration/verdict decision regions of `reviewloop.go`, the existing reviewcycle
kernel, and focused tests. Sole `reviewloop_spine` writer.

## Required work

1. Add production-call-site mutation coverage.
2. Translate captured facts into kernel input once.
3. Apply typed decisions through existing effect owners.
4. Delete inline decision duplication.
5. Meet the exact approved structural target.

## Acceptance

- `reviewcycle.Decide` has a production caller.
- Verdict/iteration policy exists in one pure owner.
- The coordinator retains no shadow decision path.

## Verification

Decision tables, mutation, scenario/race/repeat tests, architecture gate,
lint/UBS, and `make check-fast`.

## Escalate when

Stop if current production behavior conflicts with normative review policy.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
