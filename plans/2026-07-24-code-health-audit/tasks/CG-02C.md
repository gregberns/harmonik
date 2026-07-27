# CG-02C — Cut over terminal and mode integrations

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CG-02B`
- Work type: bounded composition-root migration

## Objective

Migrate review, single, and DOT mode adapters plus terminal/recovery effects to
the immutable production graph after their Pi/process integrations are
complete. Compatibility deletion remains `CG-02D`.

## Exclusive lease

Mode/terminal/recovery construction sites and focused production-composition
tests. Sole `composition_spine` writer; mode implementation files are
read-only.

## Required work

1. Install each reviewed mode executor once.
2. Install one terminal and one recovery/adoption owner.
3. Remove migrated compatibility adapters and late population.
4. Prove every production mode resolves through the graph.
5. Meet exact field-reach/import targets.

## Acceptance

- Single, review, and DOT share the frozen lifecycle/terminal contracts.
- No mode retains an alternate production composition.
- One reviewed commit leaves a green intermediate state.

## Verification

Production-call-site mutations, cross-mode/Pi composition, race/repeat,
depguard/architecture gates, lint/UBS, and `make check-fast`.

## Escalate when

Stop if a mode requires changing a frozen owner or semantic policy.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
