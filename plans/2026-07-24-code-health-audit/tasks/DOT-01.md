# DOT-01 — Define and implement pure DOT progression

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `ARCH-01`, `ARCH-GATE`
- Work type: graph-state contract and pure kernel

## Objective

Extract the pure graph/cascade decision state from `driveDotWorkflow`: runnable
nodes, joins, retries, completion, failure propagation, and sub-workflow
outcomes. The kernel emits decisions/effects but performs no launch, persistence,
event, or terminal action.

## Exclusive lease

One pure DOT state owner and exhaustive tests. `dot_cascade_core.go` may be read
but not edited in this task.

## Required work

1. Reconcile normative DOT specs and existing pure helpers.
2. Define complete graph state and typed decisions.
3. Cover all node types, joins, retry/cancel, nested workflow, and terminal
   absorption.
4. Prove deterministic replay and state-transition invariants.
5. Document the exact production integration sites for `DOT-02`.

## Acceptance

- Equal state/input produces equal decisions.
- Invalid transitions fail closed.
- The kernel imports no daemon, handler, tmux, queue persistence, or eventbus.
- Exhaustive/property tests cover the current production cases.

## Verification

Table/property/fuzz/mutation tests, race/repeat, depguard, lint/UBS, and
`make check-fast`.

## Escalate when

Stop for spec reconciliation if current production behavior contradicts the
normative graph semantics.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
