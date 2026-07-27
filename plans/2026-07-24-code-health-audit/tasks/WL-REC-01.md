# WL-REC-01 — Extract the restart gate and route recovery through adoption

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `ARCH-GATE`, `JR-04`, `BR-04`, `CQ-03`, `WL-03`
- Conflicts with: `CQ-CALLER-WORKLOOP-MAINTENANCE` and the other active
  `workloop.go` writers named by `TASK-INDEX.yaml`
- Work type: serial workloop caller migration

## Objective

Extract the restart-only pre-dispatch readiness gate, then replace inline
`runWorkLoop` live-session adoption/recovery policy with the typed, idempotent
owner completed by `JR-04`. Preserve only control-loop scheduling and effect
application.

## Evidence to verify first

Use `WL-01`, `JR-04` recovery/adoption tests, `adoptLiveRunSession`, current
startup/in-loop recovery branches, the `restart_spawn_gate` and
`restart_live_session` gap returns, and exact `ARCH-01` metric targets.

## Exclusive lease

Only the initial `spawnSubstrateReadyCh` gate and recovery/adoption call sites
in `workloop.go`, narrow adapter wiring, and focused tests. Sole
`workloop_recovery_spine` writer; broad boot wiring and `JR-04` implementation
files are read-only.

## Required work

1. Add production-call-site mutation tests for the restart gate and adoption
   owner.
2. Close `restart_spawn_gate` with a deterministic entered-wait
   acknowledgement before readiness is released. Prove that no source effect
   occurs before release and that selection may proceed afterward.
3. Translate current adoption facts into the typed recovery input.
4. Provide an injected or otherwise controllable monitor-tick trigger for live
   adoption. Close `restart_live_session` with a deterministic trace proving
   disappearance → ledger reopen → durable dispatched-to-pending persistence
   and wake → registry removal.
5. Apply returned effects through the reviewed owner.
6. Add mutations that bypass the readiness gate or allow duplicate
   adoption/retry; prove that the corresponding oracle rejects them.
7. Delete inline readiness and adoption/recovery policy.
8. Meet the exact approved `runWorkLoop` target.

## Acceptance

- No dispatch source is consulted before the restart readiness gate is known to
  have been entered and then released.
- Recovery/adoption policy exists only in the `JR-04` owner.
- Repeated loop iterations cannot adopt and retry the same Run.
- Ambiguous session/provenance fails closed.
- Live-session disappearance deterministically reopens the ledger, durably
  reverts and wakes the queue, and removes the registry record in that order.
- `runWorkLoop` retains no direct adoption policy.

`JR-04` production files and CQ-04 startup files are read-only. Accepted
intermediate: recovery policy exists only in JR-04 and workloop retains
scheduling/effect application. Roll back only restart gate/adoption call-site
migration and narrow adapter wiring.

## Verification

Named `restart_spawn_gate` and `restart_live_session`
production-composition traces, gate-bypass and duplicate-adoption mutations,
fault/restart/race/repeat tests, architecture gate, delta lint, UBS, and
`make check-fast`.

## Escalate when

Stop if caller migration requires changing the reviewed recovery contract.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
