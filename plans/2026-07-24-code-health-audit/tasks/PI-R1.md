# PI-R1 — Keep initial and resume turns in one Pi agent directory

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-A1`, `PI-A2B`
- Work type: bounded resume wiring

## Objective

Initial and resume specs must use the same run-scoped agent/session directory;
resume must not fall back to global Pi state or rewrite initial config.

## Exclusive lease

`internal/harness/pi/launchspec.go` and focused initial/resume tests. Serialize
with `PI-A1`/`PI-A2B`.

## Acceptance

Both turns carry the same effective directory; resume reuses captured session,
does not regenerate config, and cannot select a same-ID global session.

## Verification

Launch-spec tests plus controlled real Pi process if available, secret scan,
lint/UBS, review, check-fast.

## Escalate when

Stop if Pi version behavior differs from `PI-SPEC-01`.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

