# SPEC — comms-test-harness

Assembled from `05-specs/*.md`. Authoritative source: `plans/2026-07-06-quality-system/12-comms-test-design.md`.

## T1 — L0 pure-projection (4 beads, parallel)
See `05-specs/t1-l0-spec.md`.

## T2 — L1 in-process bus/hub (4 beads, parallel)
See `05-specs/t2-l1-spec.md`.

## T3 — L2 socket/CLI e2e (3 beads, serial tail, depends on T1+T2)
See `05-specs/t3-l2-spec.md`.

## T4 — spec/doc reconciliation (1-2 beads, operator-in-the-loop, depends on specific T1/T2 beads)
See `05-specs/t4-spec-spec.md`.

## Gate
Assessor runs L0/L1/L2 suites on the isolated scratch clone; epic BLOCK set = open P0/P1
`found-by:comms-test-harness` beads; one human PR from `integration/comms-test` to `main` on green.
