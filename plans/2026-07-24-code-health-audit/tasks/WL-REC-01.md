# WL-REC-01 — Route workloop recovery through the adoption owner

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `ARCH-GATE`, `JR-04`, `BR-04`, `CQ-03`, `WL-03`
- Work type: serial workloop caller migration

## Objective

Replace inline `runWorkLoop` live-session adoption/recovery policy with the
typed, idempotent owner completed by `JR-04`. Preserve only control-loop
scheduling and effect application.

## Evidence to verify first

Use `WL-01`, `JR-04` recovery/adoption tests, `adoptLiveRunSession`, current
startup/in-loop recovery branches, and exact `ARCH-01` metric targets.

## Exclusive lease

Only the recovery/adoption call sites in `workloop.go`, narrow adapter wiring,
and focused tests. Sole `workloop_recovery_spine` writer; `JR-04`
implementation files are read-only.

## Required work

1. Add a production-call-site mutation test.
2. Translate current facts into the typed recovery input.
3. Apply returned effects through the reviewed owner.
4. Delete inline adoption/recovery policy.
5. Meet the exact approved `runWorkLoop` target.

## Acceptance

- Recovery/adoption policy exists only in the `JR-04` owner.
- Repeated loop iterations cannot adopt and retry the same Run.
- Ambiguous session/provenance fails closed.
- `runWorkLoop` retains no direct adoption policy.

## Verification

Mutation, fault/restart/race/repeat tests, architecture gate, delta lint, UBS,
and `make check-fast`.

## Escalate when

Stop if caller migration requires changing the reviewed recovery contract.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
