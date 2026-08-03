# Research — Execution Model

## Questions

1. Does Step 14 change workflow selection or the durable run record?
2. Does any substrate capability create a new run lifecycle guarantee?
3. What does the specification require when a capability is unavailable?

## Findings

`specs/execution-model.md` §4.3 EM-012a resolves legacy `single` inputs to
the named no-review DOT graph. Step 14 must not change that resolution,
`WorkflowDescriptor`, selection source, or resolved mode.

The run record in §4.3 requires one sealed workflow invocation. The substrate
contract is below that record. A declared capability must not cause the daemon
to create a different workflow or re-evaluate a run.

The unavailable-capability posture in EM-012a-FLOOR is loud and honest. The
daemon must fail rather than silently change execution shape. This supports the
same distinction for Step 14: a capability that is required for a selected
mode must fail structurally. A capability that is optional must keep its
documented degraded result.

`runSessionSpawner` is a declared private interface with no production
assertion. `perRunSubstrate` calls the concrete `*tmuxSubstrate.SpawnRunSession`
method, and the current source has no production assignment to its
`runSessionID` trigger. Leaving it in a capability contract would preserve a
hidden concrete dependency.

## Pattern to Follow

Keep execution-model unchanged unless the selected design makes run-session
isolation reachable. If it does, the design must state whether that mode is
required and what structural error a missing capability returns. Otherwise,
remove the unused interface and its unreachable path from the Step 14 scope.

## Risk

Treating an unavailable run-session capability as a normal DOT fallback would
change a sealed run after resolution. That conflicts with EM-012a.
