# E2E-GATE — Package the bounded local recovery gate

## Dispatch metadata

- Group / priority: end-to-end / P1
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `E2E-BASE`, `E2E-PI`, `E2E-FAULT`, `E2E-STRESS`
- Work type: local test gate and runbook

## Objective

Add one bounded local command/runbook that executes the accepted queue, Run,
Pi, fault, and race evidence without the primary daemon.

## Exclusive lease

Exact test script/Make target and this plan's runbook references. No CI and no
production behavior.

## Acceptance

The command is bounded, hermetic, reports each evidence class separately, and
labels primary-daemon and functional SSH proof deferred. It is green only when
all required local tasks are integrated.

## Verification

Run the new gate from a clean tree, independent review, `make check-short` at
the milestone.

## Escalate when

Do not hide flaky or environment-dependent tests behind unconditional skips.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
