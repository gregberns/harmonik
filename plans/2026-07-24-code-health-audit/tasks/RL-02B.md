# RL-02B — Integrate owned review sessions and subscriptions

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `RL-02A`, `PS-02`
- Work type: serial reviewloop lifecycle integration

## Objective

Replace compatibility event taps and inline session cleanup in
`runReviewLoop` with the reviewed owned registration/subscription API and local
phase-session owner.

## Exclusive lease

Session/subscription regions of `reviewloop.go`, existing registration and
subscription owners, `PS-02` adapters, and focused tests. Sole
`reviewloop_spine` writer.

## Required work

1. Prove production uses owned subscription/registration.
2. Route implementer/reviewer session lifetime through `PS-02`.
3. Delete duplicate tap, watcher, Wait, and close paths.
4. Preserve event ordering and terminal latching.
5. Meet the exact approved structural target.

## Acceptance

- Every session/subscription closes once.
- No compatibility `Subscribe()` remains in the owned phase path.
- Lifecycle failures cannot leak a watcher, hook, or waiter.

## Verification

Mutation, immediate-event, fault/race/leak/repeat tests, real local process,
architecture gate, lint/UBS, and `make check-fast`.

## Escalate when

Stop if review mode requires changing the frozen `PS-01` contract.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
