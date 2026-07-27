# PI-R2 — Fail closed before any remote Pi placement or secret materialization

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-DEF-01`, `PI-SPEC-01`, `BR-02A`
- Work type: remote placement safety fence

## Objective

Migrate the effective-Pi placement fence into the generic placement owner from
`BR-02A`. Effective Pi work remains local: it must not select/reserve a remote worker,
read/materialize a key for remote transport, call a runner, or place a secret in
SSH argv/env. Functional remote Pi remains deferred.

## Exclusive lease

The Pi policy adapter at the extracted placement owner, only the old
effective-harness/worker-selection region needed to delete the superseded
fence, routed refusal, and conformance tests. Conflicts with other workloop
tasks and must be serialized.

## Acceptance

Queue-default and label-selected Pi both bypass remote selection; explicit
remote intent returns a typed refusal; refusal has zero runner/key-read calls
and secret scan is clean.

## Verification

Placement/refusal scenarios, fake runner call count, secret scan, race, lint/UBS,
Sol review, check-fast.

## Escalate when

Stop if a secure worker-side materialization channel is proposed; that belongs
to `REMOTE-00`.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
