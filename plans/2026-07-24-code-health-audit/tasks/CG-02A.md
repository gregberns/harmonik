# CG-02A — Cut over outer-loop service construction

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CG-01`
- Work type: bounded composition-root migration

## Objective

Migrate only the outer-loop maintenance, gate, selection, mutation, reservation,
recovery, and spawn owners to the typed constructors from `CG-01`. Per-run mode
construction and compatibility deletion are excluded.

## Exclusive lease

Daemon boot outer-loop construction, exact `runWorkLoop` constructor call, and
focused composition tests. Sole `composition_spine` writer.

## Required work

1. Construct the complete outer-loop owner set at boot.
2. Pass it through one immutable constructor boundary.
3. Delete late population for migrated outer-loop fields only.
4. Prove old per-run construction still coexists.
5. Meet the exact field-reach/import target.

## Acceptance

- Outer-loop construction is complete before start.
- No migrated outer service is late-injected or optional.
- Per-run/mode callers are untouched.
- One reviewed commit leaves a green intermediate state.

## Verification

Constructor-negative, composition/mutation/race/repeat, depguard/architecture
gates, lint/UBS, and `make check-fast`.

## Escalate when

Stop if outer-loop and per-run fields cannot migrate independently.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
