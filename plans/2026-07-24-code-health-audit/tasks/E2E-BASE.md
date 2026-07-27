# E2E-BASE — Prove the core queue without Pi

## Dispatch metadata

- Group / priority: end-to-end / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-07`, `JR-05`, `CG-03`
- Work type: hermetic baseline scenario

## Objective

Extend the `CG-03` real production-composition proof and the existing real
git/br plus twin scenario to prove submit → durable reservation → claim → Run →
terminal/release. This establishes queue recovery before the full Pi matrix.

## Exclusive lease

`scenario_queue_submit_dispatch_hksk00a_test.go`, the `CG-03` harness, and one
narrow existing helper. Do not create another composition harness. No
production edits.

## Acceptance

One traceable item closes once, durable stores agree, primary daemon/paid model
are unused, and the scenario fails if any one production wiring edge is removed.

## Verification

Scenario, race/repeat, mutation check in a real detached worktree, review,
check-fast.

## Escalate when

File a production defect; do not patch it here.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
