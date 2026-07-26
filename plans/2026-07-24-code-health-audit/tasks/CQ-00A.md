# CQ-00A — Inventory supported queue ingress and mutation callers

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `pi_ralph`
- Reviewer profile: `terra_high`
- Depends on: none
- Work type: mechanical source inventory

## Objective

Produce `tasks/evidence/CQ-00A.yaml` listing every supported CLI/socket ingress,
production adapter construction, queue mutation owner, and direct persistence
caller. No behavior judgment or production change.

## Exclusive lease

- Read any source/spec.
- Write only `tasks/evidence/CQ-00A.yaml`.

## Required work

1. Trace `internal/queue/cli` to `internal/queue/rpc.go` and daemon socket wiring.
2. Find every call to `Persist`, `Load`, migration, complete/unlink, and shutdown
   cancel; record file, symbol, lock owner, and lifecycle phase.
3. Mark fallback/test-only paths separately from production-reachable paths.
4. Record persist → install → wake → event ordering at each ingress.

The evidence file must contain `task_id`, `base_sha`, `searches`, `ingress`,
`mutators`, `persistence_callers`, `migration`, `shutdown`, and
`unresolved_reachability`. Every source row names `path`, `symbol`,
`production_reachable`, `lock_owner`, and `ordering`.

## Acceptance

- `rg` search terms and result counts are recorded.
- Every production `Persist` caller has a classification.
- No “safe/atomic/supported” conclusion is made.
- Evidence YAML parses.

## Escalate when

Report unresolved production reachability; do not infer or fix it.

## Verification

- `rg -n '\b(Persist|Load|MigrateFromLegacy|CompleteAndUnlink|Cancel)\b' internal/queue internal/daemon internal/lifecycle`
- `rg -n '\b(Submit|Append|Wake|queue submit|queue append)\b' internal/queue internal/daemon cmd`
- `ruby -ryaml -e 'YAML.load_file(ARGV.fetch(0)); puts "valid"' plans/2026-07-24-code-health-audit/tasks/evidence/CQ-00A.yaml`
- `git diff --exit-code -- . ':(exclude)plans/2026-07-24-code-health-audit/tasks/evidence/CQ-00A.yaml'`

## Return

Commit the accepted evidence artifact explicitly.
