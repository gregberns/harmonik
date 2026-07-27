# DOT-03 — Thin the DOT coordinator and break the sub-workflow cycle

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `DOT-01`, `DOT-02`, `DOT-GATE-01`, `ARCH-GATE`
- Work type: serial DOT coordinator decomposition

## Objective

Integrate pure progression with typed node executors, break the
DOT-core/sub-workflow symbol cycle behind the executor contract, and reduce
`driveDotWorkflow` to a thin coordinator. Package relocation is optional and
does not count as decomposition.

## Exclusive lease

`driveDotWorkflow`, `sub_workflow_runner.go`, exact executor registry/adapters,
and focused tests. Sole `dot_spine` writer; no shared baseline file.

## Required work

1. Adapt tool, gate, agentic, and sub-workflow execution to typed results.
2. Drive all state changes through the `DOT-01` kernel.
3. Remove direct cross-calls causing the core/sub-workflow cycle.
4. Delete duplicate graph/persistence/finalization decisions.
5. Meet architecture targets before any LIFT.12 move.

## Acceptance

- Graph policy is pure and node effects are adapter-owned.
- The sub-workflow cycle is absent from the symbol/dependency graph.
- `driveDotWorkflow` is a thin progression/effect loop.
- Default DOT production composition and nested workflows pass.

## Verification

DOT scenario/fault/restart/property/race/repeat tests, architecture and depguard
gates, compile probe, lint/UBS, and `make check-short`.

## Escalate when

Stop before package relocation without explicit operator approval and fresh
fleet quiescence. Do not move an unresolved cycle atom.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
