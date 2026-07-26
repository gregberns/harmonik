# CQ-00B — Inventory queue crash cuts and existing proof

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `pi_ralph`
- Reviewer profile: `terra_high`
- Depends on: none
- Work type: mechanical test/crash-cut inventory

## Objective

Produce `tasks/evidence/CQ-00B.yaml` mapping existing unit, fault, race,
scenario, and restart tests to queue lifecycle cuts. No production change.

## Exclusive lease

- Read source/tests/specs.
- Write only `tasks/evidence/CQ-00B.yaml`.

## Required work

1. Inventory tests covering validation, submit/append persistence, migration,
   reservation, claim, group advance, completion/unlink, shutdown, and restart.
2. Record whether each test reaches production composition or only a helper.
3. List uncovered cuts before/after durable write and in-memory install.
4. Re-run only narrow deterministic tests needed to validate an uncertain map;
   record exact commands/results.

The evidence file must contain `task_id`, `base_sha`, `searches`,
`lifecycle_cuts`, `existing_tests`, `production_composition_tests`,
`restart_tests`, `uncovered_cuts`, and `timing_or_mock_oracles`. Each lifecycle
cut records the durable fact before and after the cut.

## Acceptance

- Existing queue happy-path and restart scenario tests are explicitly included.
- Every crash cut has a test, a named gap, or a justified not-applicable result.
- Timing-sleep or mock-only oracles are flagged.
- Evidence YAML parses.

## Escalate when

Do not repair a failing test or add a new oracle; report it for synthesis.

## Verification

- `rg -n 'scenario_queue_submit_dispatch|scenario_restart_recovery|scenario_dispatch_tracker_orphan' internal/daemon`
- `rg -n '\b(fault|restart|crash|persist|migrat|recover|unlink|claim)\b' internal/queue --glob '*_test.go'`
- `ruby -ryaml -e 'YAML.load_file(ARGV.fetch(0)); puts "valid"' plans/2026-07-24-code-health-audit/tasks/evidence/CQ-00B.yaml`
- `git diff --exit-code -- . ':(exclude)plans/2026-07-24-code-health-audit/tasks/evidence/CQ-00B.yaml'`

## Return

Commit the accepted evidence artifact explicitly.
