# PI-O1 — Classify Pi retry signals without global tuning

## Dispatch metadata

- Group / priority: Pi resilience / P1
- Execution profile: `pi_ralph` for pure classifier; Sol split required for integration
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-O0`, `JR-04`
- Work type: deferred pure classifier only

## Objective

Build an exhaustive pure classifier from confirmed fixtures. Per-queue delayed
retry/escalation integration is a separate Sol task created after this closes.

## Exclusive lease

Pi adapter pure classifier and tests only. No bandwidth tuner, queue, workloop,
or retry scheduling.

## Acceptance

Confirmed minute/day/unknown/404 shapes map deterministically; unconfirmed input
is not guessed; classifier has no global side effects.

## Verification

Fixture tables/fuzz, lint/UBS, Sol review.

## Escalate when

Stop before any queue/retry integration.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
