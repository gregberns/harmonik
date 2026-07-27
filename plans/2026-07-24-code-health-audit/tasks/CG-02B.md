# CG-02B — Cut over per-run factory construction

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CG-02A`
- Work type: bounded composition-root migration

## Objective

Migrate immutable run resolution, placement/resource leases, phase adapter, and
mode-executor factory construction to the typed `CG-01` graph. Terminal effects
and final compatibility deletion remain excluded.

## Exclusive lease

Per-run factory/bundle construction and exact boot/call sites, plus focused
tests. Sole `composition_spine` writer.

## Required work

1. Construct complete per-run factories before dispatch.
2. Remove late nil population for migrated factories.
3. Preserve typed queued/direct variants.
4. Keep terminal/recovery adapters on their existing boundary.
5. Meet exact field-reach/import targets.

## Acceptance

- A partial runnable per-run factory cannot be constructed.
- Modes receive complete typed inputs without daemon lookup.
- One reviewed commit leaves a green intermediate state.

## Verification

Constructor-negative, cross-mode composition/mutation/race tests,
depguard/architecture gates, lint/UBS, and `make check-fast`.

## Escalate when

Stop if a mode still requires an unowned daemon-private dependency.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
