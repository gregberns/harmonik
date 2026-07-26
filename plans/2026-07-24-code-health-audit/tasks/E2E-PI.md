# E2E-PI — Prove queue-to-local-Pi completion

## Dispatch metadata

- Group / priority: end-to-end / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `E2E-BASE`, `PI-E1`
- Work type: controlled local Pi scenario

## Objective

Submit an effective-Pi item to a named capped queue and prove one local Pi
process, one Run, finalized commit truth, terminal/release, and secret isolation.

## Exclusive lease

Existing Pi/queue scenario fixtures/tests only. No production or SSH edits.

## Acceptance

Queue default and/or label routing selects Pi; no remote worker is touched; the
controlled local process completes/reaps; HEAD/events/stores agree; secret scan
is clean.

## Verification

Real controlled local process, race/repeat, secret scan, review, check-fast.

## Escalate when

File a bounded production defect; never use the primary daemon as the oracle.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

