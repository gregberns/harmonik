# PI-Q2D — Prove Pi queue admission and dispatch routing

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-Q2B`, `CQ-DEF-01`
- Work type: production-composition scenario

## Objective

Prove label override, queue-default Pi, main/uncapped rejection, named capped
acceptance, and effective Pi dispatch through the existing scenario harness.

## Exclusive lease

One focused queue/Pi scenario test and existing helper only. No production edits.

## Acceptance

All admission matrix rows assert durable no-side-effect rejection or correct Pi
routing; no primary daemon or paid remote process.

## Verification

Scenario, race/repeat, review, check-fast.

## Escalate when

File a production defect; do not patch it here.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

