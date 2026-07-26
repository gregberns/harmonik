# PI-E1 — Prove the local Pi lifecycle matrix

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-A2B`, `PI-R1`, `PI-F1`, `PI-F2S`, `PI-F2D`, `PI-Q2D`, `PI-R2`
- Work type: local controlled-process scenario

## Objective

Using existing Pi/twin scenario infrastructure, prove local initial/resume,
single/review/DOT, success/no-change/failure, cancellation, finalization, queue
selection, event truth, and secret boundaries. Rate-limit behavior and
functional SSH are not dependencies.

## Exclusive lease

Pi lifecycle scenario fixtures/tests and one narrow shared helper. No production
semantics.

## Acceptance

Spawn proof, captured identity, genuine readiness, terminal signal, process
reap, HEAD truth, and queue/Run terminal truth are asserted separately; secret
scan is clean; remote case proves refusal only.

## Verification

Scenario matrix, real controlled local process, race/repeat, secret scan, review,
check-fast.

## Escalate when

Create a production defect task instead of patching it in this card.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

